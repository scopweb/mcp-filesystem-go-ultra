package mcpserver

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mcp/filesystem-ultra/core"
)

type inspectByteOptions struct {
	mode, detail                                   string
	start, end, offset, byteEnd, maxBytes, maxScan int
	hasLines, hasOffset                            bool
}

func inspectByteArgs(engine *core.UltraFastEngine, args map[string]any) (inspectByteOptions, error) {
	var o inspectByteOptions
	var err error
	if o.mode, err = inspectEnum(args, "mode", "encoding", "encoding", "bytes", "all"); err != nil {
		return o, err
	}
	if o.detail, err = inspectEnum(args, "detail", "normal", "summary", "normal", "full"); err != nil {
		return o, err
	}
	maximum := engine.GetConfig().MaxInspectScanBytes
	if maximum <= 0 {
		maximum = inspectScanMaximum
	}
	if o.maxScan, err = inspectInt(args, "max_scan_bytes", min(inspectScanDefault, maximum), 1, maximum); err != nil {
		return o, err
	}
	if o.maxBytes, err = inspectInt(args, "max_bytes", 512, 1, inspectDumpLimit); err != nil {
		return o, err
	}
	_, o.hasLines = args["start_line"]
	_, o.hasOffset = args["byte_offset"]
	if _, hasEnd := args["end_line"]; hasEnd && !o.hasLines {
		return o, fmt.Errorf("end_line requires start_line")
	}
	if o.hasLines && o.hasOffset {
		return o, fmt.Errorf("byte_offset and line ranges are mutually exclusive")
	}
	_, hasByteEnd := args["end_byte_offset"]
	if o.mode == "encoding" && (o.hasLines || o.hasOffset || hasByteEnd) {
		return o, fmt.Errorf("range selectors require mode bytes or all")
	}
	if o.start, err = inspectInt(args, "start_line", 1, 1, math.MaxInt32); err != nil {
		return o, err
	}
	if o.end, err = inspectInt(args, "end_line", o.start, o.start, math.MaxInt32); err != nil {
		return o, err
	}
	if o.offset, err = inspectInt(args, "byte_offset", 0, 0, math.MaxInt); err != nil {
		return o, err
	}
	if hasByteEnd && o.hasLines {
		return o, fmt.Errorf("end_byte_offset and line ranges are mutually exclusive")
	}
	if o.byteEnd, err = inspectInt(args, "end_byte_offset", -1, 0, math.MaxInt); err != nil {
		return o, err
	}
	if hasByteEnd && o.byteEnd < o.offset {
		return o, fmt.Errorf("end_byte_offset must be >= byte_offset")
	}
	return o, nil
}

func inspectBOM(raw []byte) (string, int) {
	for _, b := range []struct {
		signature []byte
		name      string
	}{
		{[]byte{0xff, 0xfe, 0, 0}, "utf32le"}, {[]byte{0, 0, 0xfe, 0xff}, "utf32be"},
		{[]byte{0xef, 0xbb, 0xbf}, "utf8"}, {[]byte{0xff, 0xfe}, "utf16le"}, {[]byte{0xfe, 0xff}, "utf16be"},
	} {
		if bytes.HasPrefix(raw, b.signature) {
			return b.name, len(b.signature)
		}
	}
	return "none", 0
}

// Decode without replacement of invalid sequences. pending means the sample,
// rather than the file, ended inside a character; it is not an invalid byte.
func inspectDecode(raw []byte, offset int, bom string, complete bool) (rune, int, bool, bool) {
	remaining := raw[offset:]
	if bom == "utf16le" || bom == "utf16be" {
		if len(remaining) < 2 {
			return 0, len(remaining), complete, !complete
		}
		var order binary.ByteOrder = binary.LittleEndian
		if bom == "utf16be" {
			order = binary.BigEndian
		}
		a := order.Uint16(remaining)
		if a >= 0xd800 && a <= 0xdbff {
			if len(remaining) < 4 {
				return 0, len(remaining), complete, !complete
			}
			b := order.Uint16(remaining[2:])
			if b < 0xdc00 || b > 0xdfff {
				return 0, 2, true, false
			}
			return utf16.DecodeRune(rune(a), rune(b)), 4, false, false
		}
		if a >= 0xdc00 && a <= 0xdfff {
			return 0, 2, true, false
		}
		return rune(a), 2, false, false
	}
	if !complete && !utf8.FullRune(remaining) {
		return 0, len(remaining), false, true
	}
	r, n := utf8.DecodeRune(remaining)
	return r, n, r == utf8.RuneError && n == 1, false
}

type inspectPositions struct {
	Count     int              `json:"count"`
	Returned  int              `json:"returned_count"`
	Positions []map[string]any `json:"positions"`
	Truncated bool             `json:"truncated"`
}

func (p *inspectPositions) add(offset, line, col int, r rune, invalid bool) {
	p.Count++
	if len(p.Positions) < 50 {
		position := map[string]any{"byte_offset": offset, "line": line, "column": col}
		if !invalid {
			position["codepoint"] = fmt.Sprintf("U+%04X", r)
			position["char"] = string(r)
		}
		p.Positions = append(p.Positions, position)
	}
	p.Returned = len(p.Positions)
	p.Truncated = p.Count > p.Returned
}

func inspectDiagnose(ctx context.Context, raw []byte, bom string, bomSize int, complete bool) (map[string]any, error) {
	nonASCII := inspectPositions{Positions: []map[string]any{}}
	invisible := inspectPositions{Positions: []map[string]any{}}
	invalid := inspectPositions{Positions: []map[string]any{}}
	encoding := map[string]any{"bom": bom, "charset_guess": "unknown", "confidence": "unknown", "decoding_supported": true}
	result := map[string]any{"encoding": encoding, "non_ascii": &nonASCII, "invisible": &invisible, "invalid_sequences": &invalid, "bytes_scanned": len(raw), "scan_complete": complete, "scan_scope": "prefix", "sample_boundary_pending": false}
	if complete {
		result["scan_scope"] = "file"
	}
	if !complete && len(raw) < bomSize {
		result["sample_boundary_pending"] = true
	}
	if bom == "utf32le" || bom == "utf32be" {
		encoding["charset_guess"] = bom
		encoding["confidence"] = "bom"
		encoding["decoding_supported"] = false
		result["non_ascii"], result["invisible"], result["invalid_sequences"] = nil, nil, nil
		return result, nil
	}
	charset := "utf8"
	if bom == "utf16le" || bom == "utf16be" {
		charset = bom
	}
	encoding["charset_guess"] = charset
	if bom != "none" {
		encoding["confidence"] = "bom"
	} else if complete {
		encoding["confidence"] = "validated"
	} else {
		encoding["confidence"] = "validated_prefix"
	}
	crlf, lf, cr := 0, 0, 0
	line, col := 1, 1
	for offset := min(bomSize, len(raw)); offset < len(raw); {
		if offset%4096 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		r, n, bad, pending := inspectDecode(raw, offset, bom, complete)
		if pending {
			result["sample_boundary_pending"] = true
			break
		}
		if bad {
			invalid.add(offset, line, col, 0, true)
			offset += n
			col++
			continue
		}
		if r > 127 {
			nonASCII.add(offset, line, col, r, false)
		}
		if (unicode.IsControl(r) && r != '\r' && r != '\n' && r != '\t') || unicode.Is(unicode.Cf, r) || r == 0x00a0 || r == 0x202f || r == 0x2007 {
			invisible.add(offset, line, col, r, false)
		}
		if r == '\r' {
			if offset+n == len(raw) && !complete {
				result["sample_boundary_pending"] = true
				break
			}
			if offset+n < len(raw) {
				next, width, bad, pending := inspectDecode(raw, offset+n, bom, complete)
				if pending {
					result["sample_boundary_pending"] = true
					break
				}
				if !bad && next == '\n' {
					crlf++
					offset += n + width
					line++
					col = 1
					continue
				}
			}
			cr++
			line++
			col = 1
		} else if r == '\n' {
			lf++
			line++
			col = 1
		} else {
			col++
		}
		offset += n
	}
	encoding["crlf_count"], encoding["lf_count"], encoding["cr_count"] = crlf, lf, cr
	eol := "none"
	kinds := 0
	for _, v := range []struct {
		name  string
		count int
	}{{"crlf", crlf}, {"lf", lf}, {"cr", cr}} {
		if v.count > 0 {
			kinds++
			eol = v.name
		}
	}
	if kinds > 1 {
		eol = "mixed"
	}
	encoding["eol"] = eol
	encoding["valid"] = invalid.Count == 0
	if charset == "utf8" {
		encoding["utf8_valid"] = invalid.Count == 0
	}
	if invalid.Count > 0 && bom == "none" {
		encoding["charset_guess"] = "unknown"
		encoding["confidence"] = "invalid_utf8"
	}
	return result, nil
}

// Line lookup is deliberately bounded by max_scan_bytes. It never silently
// scans the rest of a huge file to resolve a remote line number.
func inspectLineRange(ctx context.Context, raw []byte, bom string, bomSize int, complete bool, start, end int) (int, int, error) {
	if bom == "utf32le" || bom == "utf32be" {
		return 0, 0, fmt.Errorf("line ranges require UTF-8 or UTF-16 with BOM; use byte_offset")
	}
	line, begin := 1, 0
	selected := -1
	if start == 1 {
		selected = 0
	}
	lastWasEOL := false
	for offset := min(bomSize, len(raw)); offset < len(raw); {
		if err := ctx.Err(); err != nil {
			return 0, 0, err
		}
		r, n, bad, pending := inspectDecode(raw, offset, bom, complete)
		if bad {
			return 0, 0, fmt.Errorf("invalid encoding in scanned range; use byte_offset")
		}
		if pending {
			break
		}
		next := offset + n
		lastWasEOL = r == '\r' || r == '\n'
		if r == '\r' {
			if next == len(raw) && !complete {
				break
			}
			if next < len(raw) {
				r2, n2, bad2, pending2 := inspectDecode(raw, next, bom, complete)
				if pending2 {
					break
				}
				if !bad2 && r2 == '\n' {
					next += n2
				}
			}
		}
		if lastWasEOL {
			if line == end && selected >= 0 {
				return selected, next, nil
			}
			line++
			begin = next
			if line == start {
				selected = begin
			}
		}
		offset = next
	}
	if !complete {
		return 0, 0, fmt.Errorf("line range extends beyond diagnostic prefix; raise max_scan_bytes or use byte_offset")
	}
	if selected < 0 || (selected == len(raw) && lastWasEOL) {
		return 0, 0, fmt.Errorf("start_line exceeds file lines")
	}
	if line < end {
		return 0, 0, fmt.Errorf("end_line exceeds file lines")
	}
	return selected, len(raw), nil
}

func inspectFileBytes(ctx context.Context, engine *core.UltraFastEngine, args map[string]any) *mcp.CallToolResult {
	ctx, cancel := context.WithTimeout(ctx, core.DefaultOperationTimeout)
	defer cancel()
	o, err := inspectByteArgs(engine, args)
	if err != nil {
		return validationResult(err.Error(), "", "valid file_bytes arguments")
	}
	f, path, denied := inspectOpen(ctx, engine, stringArg(args, "path"))
	if denied != nil {
		return denied
	}
	defer f.Close()
	before, err := f.Stat()
	if err != nil {
		return inspectIOError(path, err)
	}
	payload := map[string]any{"path": path, "status": "success", "truncated": false}
	var raw []byte
	var bom string
	var bomSize int
	var complete bool
	if o.mode != "bytes" || o.hasLines {
		raw, err = io.ReadAll(io.LimitReader(inspectContextReader{ctx, f}, int64(o.maxScan)+1))
		if err != nil {
			return inspectIOError(path, err)
		}
		complete = len(raw) <= o.maxScan
		if !complete {
			raw = raw[:o.maxScan]
		}
		// Constant-size BOM probe is independent of the statistical scan limit.
		probe := make([]byte, 4)
		n, readErr := f.ReadAt(probe, 0)
		if readErr != nil && readErr != io.EOF {
			return inspectIOError(path, readErr)
		}
		bom, bomSize = inspectBOM(probe[:n])
		if o.mode != "bytes" {
			diagnostic, err := inspectDiagnose(ctx, raw, bom, bomSize, complete)
			if err != nil {
				return inspectIOError(path, err)
			}
			for key, value := range diagnostic {
				payload[key] = value
			}
		}
	}
	var dump []byte
	var offset int64
	if o.mode != "encoding" {
		offset = int64(o.offset)
		end := before.Size()
		if o.byteEnd >= 0 {
			end = int64(o.byteEnd)
		}
		if o.hasLines {
			begin, finish, err := inspectLineRange(ctx, raw, bom, bomSize, complete, o.start, o.end)
			if err != nil {
				return validationResult(err.Error(), "start_line", "range within supported diagnostic prefix")
			}
			offset, end = int64(begin), int64(finish)
		}
		if end > before.Size() {
			return validationResult("end_byte_offset exceeds file size", "end_byte_offset", "end <= size")
		}
		if offset > before.Size() {
			return validationResult("byte_offset exceeds file size", "byte_offset", "offset <= size")
		}
		dumpSize := end - offset
		if dumpSize > int64(o.maxBytes) {
			dumpSize = int64(o.maxBytes)
		}
		dump = make([]byte, int(dumpSize))
		n, err := f.ReadAt(dump, offset)
		if err != nil && err != io.EOF {
			return inspectIOError(path, err)
		}
		dump = dump[:n]
		payload["byte_offset"], payload["bytes_returned"], payload["hex"] = offset, len(dump), hex.EncodeToString(dump)
		payload["range_end_offset"] = end
		if offset+int64(len(dump)) < end {
			payload["truncated"] = true
			payload["truncation_reason"] = "max_bytes"
			// Keep the exclusive range end so line-range continuations cannot spill.
			payload["continuation"] = map[string]any{"path": path, "mode": "bytes", "byte_offset": offset + int64(len(dump)), "end_byte_offset": end, "max_bytes": o.maxBytes, "detail": o.detail}
		}
	}
	if err := ctx.Err(); err != nil {
		return inspectFailure("CANCELLED", path, err.Error())
	}
	if err := inspectStable(f, before, path); err != nil {
		return inspectFailure("FILE_CHANGED", path, err.Error())
	}
	summary := fmt.Sprintf("file_bytes %s | mode:%s | dump:%d bytes | truncated:%v", path, o.mode, len(dump), payload["truncated"])
	if o.mode != "bytes" {
		summary += fmt.Sprintf(" | scanned:%d | scan_complete:%v", len(raw), complete)
	}
	metadata, _ := json.MarshalIndent(payload, "", "  ")
	body := string(metadata)
	if dump != nil {
		body += "\n" + inspectHexDump(dump, offset)
	}
	result, fits := inspectResponse(engine, payload, body, summary, o.detail)
	if !fits {
		return inspectFailure("LIMIT_EXCEEDED", path, "diagnostic response exceeds server response limit; use detail:summary, mode:bytes or a smaller max_bytes")
	}
	return result
}
