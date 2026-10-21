package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"unicode/utf8"

	"github.com/jmespath/go-jmespath"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mcp/filesystem-ultra/core"
)

const (
	inspectJSONNodes     = 200000
	inspectJSONDepth     = 64
	inspectQueryLength   = 2048
	inspectExpandedLimit = 64 << 20
	inspectQueryWork     = 100000000
)

type jsonQueryOptions struct {
	path, query, format, detail, expected string
	columns                               []string
	limit, offset, budgetMultiplier       int
}

func inspectJSONOptions(args map[string]any) (jsonQueryOptions, error) {
	var o jsonQueryOptions
	var err error
	o.path = stringArg(args, "path")
	o.query = stringArg(args, "query")
	o.expected = stringArg(args, "expected_hash")
	if o.format, err = inspectEnum(args, "output_format", "json", "json", "table", "csv", "text"); err != nil {
		return o, err
	}
	if o.detail, err = inspectEnum(args, "detail", "normal", "summary", "normal", "full"); err != nil {
		return o, err
	}
	if o.limit, err = inspectInt(args, "limit", 100, 1, 1000); err != nil {
		return o, err
	}
	if o.offset, err = inspectInt(args, "offset", 0, 0, math.MaxInt32); err != nil {
		return o, err
	}
	if o.budgetMultiplier, err = inspectInt(args, "budget_multiplier", 1, 1, 4); err != nil {
		return o, err
	}
	if o.columns, err = inspectStrings(args, "columns", 128); err != nil {
		return o, err
	}
	if o.columns != nil && o.format != "table" && o.format != "csv" {
		return o, fmt.Errorf("columns is only supported for table/csv")
	}
	seen := map[string]bool{}
	columnBytes := 0
	for _, column := range o.columns {
		columnBytes += len(column)
		if len(column) > 1024 || columnBytes > 8192 {
			return o, fmt.Errorf("column paths exceed 1024 bytes each or 8192 bytes total")
		}
		if seen[column] {
			return o, fmt.Errorf("duplicate column %q", column)
		}
		seen[column] = true
		for _, part := range strings.Split(column, ".") {
			if part == "" {
				return o, fmt.Errorf("invalid column path %q", column)
			}
		}
	}
	if len(o.query) > inspectQueryLength {
		return o, fmt.Errorf("query exceeds %d bytes", inspectQueryLength)
	}
	return o, nil
}

func inspectJSONQuery(ctx context.Context, engine *core.UltraFastEngine, args map[string]any) *mcp.CallToolResult {
	ctx, cancel := context.WithTimeout(ctx, core.DefaultOperationTimeout)
	defer cancel()
	o, err := inspectJSONOptions(args)
	if err != nil {
		return validationResult(err.Error(), "", "valid json_query arguments")
	}
	f, path, denied := inspectOpen(ctx, engine, o.path)
	if denied != nil {
		return denied
	}
	defer f.Close()
	before, err := f.Stat()
	if err != nil {
		return inspectIOError(path, err)
	}
	if before.Size() > inspectJSONInputLimit {
		return inspectFailure("LIMIT_EXCEEDED", path, "JSON input exceeds 16 MiB; select a smaller source file")
	}
	raw, err := io.ReadAll(io.LimitReader(inspectContextReader{ctx, f}, inspectJSONInputLimit+1))
	if err != nil {
		return inspectIOError(path, err)
	}
	if len(raw) > inspectJSONInputLimit {
		return inspectFailure("LIMIT_EXCEEDED", path, "JSON input exceeds 16 MiB")
	}
	if err := inspectStable(f, before, path); err != nil {
		return inspectFailure("FILE_CHANGED", path, err.Error())
	}
	hash := contentHashBytes(raw)
	if o.expected != "" && o.expected != hash {
		conflict := core.BuildOCCConflict(o.expected, raw, nil)
		return occMismatchResult("JSON changed since the previous page; restart the query without expected_hash", path, o.expected, hash, conflict)
	}
	core.RecordReadHash(path, hash)
	input := raw
	bomSize := 0
	if bytes.HasPrefix(input, []byte{0xef, 0xbb, 0xbf}) {
		input = input[3:]
		bomSize = 3
	}
	if !utf8.Valid(input) {
		offset := 0
		for offset < len(input) {
			r, n := utf8.DecodeRune(input[offset:])
			if r == utf8.RuneError && n == 1 {
				break
			}
			offset += n
		}
		return inspectJSONInvalid(path, raw, int64(offset+bomSize), "invalid UTF-8")
	}
	decoder := json.NewDecoder(bytes.NewReader(input))
	decoder.UseNumber()
	nodes := 0
	document, err := inspectDecodeJSON(ctx, decoder, 0, &nodes)
	if err != nil {
		return inspectJSONDecodeError(path, raw, bomSize, err)
	}
	if err := inspectJSONEnd(decoder); err != nil {
		return inspectJSONDecodeError(path, raw, bomSize, err)
	}
	multiplier, syntaxWork, err := inspectQueryBudget(ctx, o.query)
	if err != nil {
		return validationResult(err.Error(), "query", "bounded standard JMESPath")
	}
	expansion, work := int64(len(raw))*multiplier, int64(nodes)*syntaxWork*multiplier
	expansionLimit, workLimit := int64(inspectExpandedLimit)*int64(o.budgetMultiplier), int64(inspectQueryWork)*int64(o.budgetMultiplier)
	if expansion > expansionLimit || work > workLimit {
		return inspectFailure("LIMIT_EXCEEDED", path, fmt.Sprintf("query exceeds conservative evaluation/expansion budget: estimated expansion=%d bytes (limit=%d), work=%d (limit=%d); budget_multiplier=%d (allowed 1..4). Simplify the expression, use a smaller input, or increase budget_multiplier", expansion, expansionLimit, work, workLimit, o.budgetMultiplier))
	}
	selected := document
	if o.query != "" {
		compiled, err := jmespath.Compile(o.query)
		if err != nil {
			return validationResult(err.Error(), "query", "standard JMESPath, not jq")
		}
		if err := ctx.Err(); err != nil {
			return inspectFailure("CANCELLED", path, err.Error())
		}
		selected, err = compiled.Search(document)
		if err != nil {
			return validationResult(err.Error(), "query", "valid JMESPath types and functions")
		}
	}
	if err := ctx.Err(); err != nil {
		return inspectFailure("CANCELLED", path, err.Error())
	}
	columns := o.columns
	nodes = 0
	if _, err := inspectJSONNumbers(ctx, selected, 0, &nodes); err != nil {
		return inspectFailure("VALIDATION", path, "query result: "+err.Error())
	}
	if o.format == "table" || o.format == "csv" {
		columns, err = inspectJSONColumns(ctx, selected, columns)
		if err != nil {
			return validationResult(err.Error(), "columns", "homogeneous rows and at most 128 columns")
		}
	}
	array, isArray := selected.([]any)
	if !isArray && o.offset != 0 {
		return validationResult("non-array results require offset:0", "offset", "0")
	}
	if isArray && o.offset > len(array) {
		return validationResult("offset exceeds result length", "offset", "offset <= total_count")
	}
	count := 1
	if isArray {
		count = min(o.limit, len(array)-o.offset)
	}
	// Reduce whole rows until the actual encoded MCP response fits. Never slice
	// serialized JSON or a single oversized scalar/object into invalid fragments.
	for {
		if err := ctx.Err(); err != nil {
			return inspectFailure("CANCELLED", path, err.Error())
		}
		if isArray && inspectQueryUnordered(o.query) && (o.offset > 0 || count < len(array)) {
			return validationResult("object iteration has no stable page order; project scalar values and append | sort(@), or return the complete result in one page", "query", "deterministically ordered result for pagination")
		}
		page := selected
		if isArray {
			page = array[o.offset : o.offset+count]
		}
		payload := map[string]any{"path": path, "query": o.query, "content_hash": hash, "status": "success", "type": inspectJSONType(selected), "result": page, "truncated": false}
		if isArray {
			payload["total_count"], payload["returned_count"] = len(array), count
			if len(array) == 0 {
				payload["status"] = "empty"
			}
			if o.offset+count < len(array) {
				payload["truncated"] = true
				reason := "pagination"
				if count < min(o.limit, len(array)-o.offset) {
					reason = "response_bytes"
				}
				payload["truncation_reason"] = reason
				next := map[string]any{"path": path, "query": o.query, "output_format": o.format, "limit": o.limit, "offset": o.offset + count, "detail": o.detail, "expected_hash": hash, "budget_multiplier": o.budgetMultiplier}
				if o.columns != nil {
					next["columns"] = o.columns
				}
				payload["continuation"] = next
			}
		}
		remaining := inspectResponseLimit(engine)
		if !inspectJSONFits(page, &remaining) {
			if !isArray || count <= 1 {
				return inspectFailure("LIMIT_EXCEEDED", path, "one result value exceeds the response limit; use a smaller JMESPath projection")
			}
			count = max(1, count/2)
			continue
		}
		body, err := inspectJSONRender(page, o.format, columns, o.columns != nil, payload)
		if err != nil {
			return inspectFailure("VALIDATION", path, err.Error())
		}
		summary := fmt.Sprintf("json_query %s | %s | hash:%s | truncated:%v | result in structured content", path, payload["type"], hash, payload["truncated"])
		if isArray {
			summary = fmt.Sprintf("json_query %s | %d/%d items at %d | hash:%s | truncated:%v | result in structured content", path, count, len(array), o.offset, hash, payload["truncated"])
		}
		result, fits := inspectResponse(engine, payload, body, summary, o.detail)
		if fits {
			return result
		}
		if !isArray || count <= 1 {
			return inspectFailure("LIMIT_EXCEEDED", path, "one result value or its metadata exceeds the response limit; use a smaller JMESPath projection")
		}
		count = max(1, count/2)
	}
}

func inspectJSONDecodeError(path string, raw []byte, bom int, err error) *mcp.CallToolResult {
	var resource *inspectJSONResourceError
	if errors.As(err, &resource) {
		return inspectFailure("VALIDATION", path, err.Error())
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return inspectFailure("CANCELLED", path, err.Error())
	}
	offset := int64(len(raw))
	var syntax *json.SyntaxError
	if errors.As(err, &syntax) {
		offset = int64(bom + max(0, int(syntax.Offset)-1))
	}
	return inspectJSONInvalid(path, raw, offset, err.Error())
}

func inspectJSONInvalid(path string, raw []byte, offset int64, message string) *mcp.CallToolResult {
	if offset < 0 {
		offset = 0
	}
	if offset > int64(len(raw)) {
		offset = int64(len(raw))
	}
	prefix := raw[:offset]
	line := bytes.Count(prefix, []byte{'\n'}) + 1
	last := bytes.LastIndexByte(prefix, '\n') + 1
	col := utf8.RuneCount(prefix[last:]) + 1
	if line == 1 && bytes.HasPrefix(prefix, []byte{0xef, 0xbb, 0xbf}) {
		col--
	}
	return pathErrorResultDetails("INVALID_JSON", message, path, map[string]any{"line": line, "column": col, "byte_offset": offset}, "Correct the JSON/UTF-8 input")
}

func inspectJSONNumbers(ctx context.Context, value any, depth int, nodes *int) (any, error) {
	*nodes++
	if *nodes > inspectJSONNodes || depth > inspectJSONDepth {
		return nil, fmt.Errorf("JSON exceeds %d nodes or depth %d", inspectJSONNodes, inspectJSONDepth)
	}
	if *nodes%256 == 0 {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}
	switch v := value.(type) {
	case json.Number:
		n, err := strconv.ParseFloat(string(v), 64)
		if err != nil || math.IsNaN(n) || math.IsInf(n, 0) {
			return nil, fmt.Errorf("number %q overflows binary64", inspectNumberPreview(string(v)))
		}
		if math.Trunc(n) == n && math.Abs(n) > 9007199254740991 {
			return nil, fmt.Errorf("integer %q exceeds safe binary64 range; encode identifiers as strings", inspectNumberPreview(string(v)))
		}
		return n, nil
	case float64:
		if math.IsNaN(v) || math.IsInf(v, 0) || (math.Trunc(v) == v && math.Abs(v) > 9007199254740991) {
			return nil, fmt.Errorf("result number is outside safe finite binary64 range")
		}
	case []any:
		for i := range v {
			n, err := inspectJSONNumbers(ctx, v[i], depth+1, nodes)
			if err != nil {
				return nil, err
			}
			v[i] = n
		}
	case map[string]any:
		for key := range v {
			n, err := inspectJSONNumbers(ctx, v[key], depth+1, nodes)
			if err != nil {
				return nil, err
			}
			v[key] = n
		}
	}
	return value, nil
}

// Conservative admission bounds for the non-context-aware standard evaluator.
// Each comma-bearing container can replicate its input. Multiplying all arities
// overestimates normal projections, but prevents chained multi-select expansion
// before evaluating. Strings/literals do not contribute punctuation to the bound.
func inspectQueryBudget(ctx context.Context, query string) (int64, int64, error) {
	stack := []int{1}
	multiplier, work := int64(1), int64(1)
	for i := 0; i < len(query); i++ {
		c := query[i]
		if c == '\'' || c == '"' || c == '`' {
			quote, start := c, i+1
			i++
			for i < len(query) {
				if query[i] == '\\' {
					i += 2
					continue
				}
				if query[i] == quote {
					break
				}
				i++
			}
			if i >= len(query) {
				return 0, 0, fmt.Errorf("unterminated query literal")
			}
			if quote == '`' {
				literal := strings.ReplaceAll(query[start:i], "\\`", "`")
				d := json.NewDecoder(strings.NewReader(literal))
				d.UseNumber()
				nodes := 0
				if _, err := inspectDecodeJSON(ctx, d, 0, &nodes); err != nil {
					return 0, 0, fmt.Errorf("invalid JSON query literal: %w", err)
				}
				if err := inspectJSONEnd(d); err != nil {
					return 0, 0, err
				}
			}
			continue
		}
		// Nested serialization can amplify escaped strings without commas.
		if strings.HasPrefix(query[i:], "to_string") {
			multiplier *= 2
			if multiplier > 4096 {
				return 0, 0, fmt.Errorf("query serialization exceeds expansion budget")
			}
		}
		switch c {
		case '[', '{', '(':
			stack = append(stack, 1)
			work++
			if len(stack) > 33 {
				return 0, 0, fmt.Errorf("query nesting exceeds 32")
			}
		case ']', '}', ')':
			if len(stack) > 1 {
				multiplier *= int64(stack[len(stack)-1])
				stack = stack[:len(stack)-1]
				if multiplier > 4096 {
					return 0, 0, fmt.Errorf("query expansion exceeds budget")
				}
			}
		case ',':
			stack[len(stack)-1]++
			work++
		case '.', '|', '&', '?', '*':
			work++
		}
	}
	return multiplier, work, nil
}

func inspectJSONType(value any) string {
	switch value.(type) {
	case nil:
		return "null"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	case string:
		return "string"
	case bool:
		return "boolean"
	default:
		return "number"
	}
}

func inspectJSONColumns(ctx context.Context, selected any, requested []string) ([]string, error) {
	values, array := selected.([]any)
	if !array {
		values = []any{selected}
	}
	objects, scalars := false, false
	keys := map[string]bool{}
	for i, value := range values {
		if i%256 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		if object, ok := value.(map[string]any); ok {
			objects = true
			if requested == nil {
				for key := range object {
					keys[key] = true
					if len(keys) > 128 {
						return nil, fmt.Errorf("inferred columns exceed 128; supply columns explicitly")
					}
				}
			}
		} else if _, ok := value.([]any); ok {
			return nil, fmt.Errorf("array-valued rows require a JMESPath object projection")
		} else {
			scalars = true
		}
	}
	if objects && scalars {
		return nil, fmt.Errorf("mixed object/scalar rows are not supported for table/csv")
	}
	if requested != nil {
		if scalars && (len(requested) != 1 || requested[0] != "value") {
			return nil, fmt.Errorf("scalar rows use the column value")
		}
		return requested, nil
	}
	if scalars {
		return []string{"value"}, nil
	}
	columns := make([]string, 0, len(keys))
	for key := range keys {
		columns = append(columns, key)
	}
	sort.Strings(columns)
	return columns, nil
}

func inspectJSONCell(value any) string {
	if value == nil {
		return ""
	}
	if s, ok := value.(string); ok {
		return s
	}
	raw, _ := json.Marshal(value)
	return string(raw)
}

func inspectJSONRender(page any, format string, columns []string, columnPaths bool, payload map[string]any) (string, error) {
	if format == "json" {
		raw, err := json.MarshalIndent(page, "", "  ")
		return string(raw), err
	}
	if format == "text" {
		if s, ok := page.(string); ok {
			return s, nil
		}
		if values, ok := page.([]any); ok {
			texts := make([]string, 0, len(values))
			all := true
			for _, v := range values {
				s, ok := v.(string)
				if !ok {
					all = false
					break
				}
				texts = append(texts, s)
			}
			if all {
				return strings.Join(texts, "\n"), nil
			}
		}
		raw, err := json.Marshal(page)
		return string(raw), err
	}
	values, ok := page.([]any)
	if !ok {
		values = []any{page}
	}
	rows := make([]map[string]any, 0, len(values))
	cells := make([][]string, 0, len(values))
	for _, value := range values {
		row := map[string]any{}
		record := make([]string, len(columns))
		for i, column := range columns {
			cell := value
			present := true
			if object, ok := value.(map[string]any); ok {
				// Inferred keys are literal top-level keys; explicit columns are paths.
				if !columnPaths {
					cell, present = object[column]
				} else {
					cell = any(object)
					for _, part := range strings.Split(column, ".") {
						m, ok := cell.(map[string]any)
						if !ok {
							present = false
							break
						}
						cell, present = m[part]
						if !present {
							break
						}
					}
				}
			}
			if present {
				row[column] = cell
				record[i] = inspectJSONCell(cell)
			}
		}
		rows = append(rows, row)
		cells = append(cells, record)
	}
	payload["columns"], payload["rows"] = columns, rows
	var b strings.Builder
	if format == "csv" {
		// CRLF record separators without normalizing embedded CR/LF values.
		writeRecord := func(record []string) {
			for i, cell := range record {
				if i > 0 {
					b.WriteByte(',')
				}
				if strings.ContainsAny(cell, ",\"\r\n") {
					b.WriteByte('"')
					b.WriteString(strings.ReplaceAll(cell, "\"", "\"\""))
					b.WriteByte('"')
				} else {
					b.WriteString(cell)
				}
			}
			b.WriteString("\r\n")
		}
		writeRecord(columns)
		for _, record := range cells {
			writeRecord(record)
		}
		return b.String(), nil
	}
	writer := tabwriter.NewWriter(&b, 0, 4, 2, ' ', 0)
	escape := func(s string) string {
		s = strings.ReplaceAll(s, "\\", "\\\\")
		s = strings.ReplaceAll(s, "\r", "\\r")
		s = strings.ReplaceAll(s, "\n", "\\n")
		return strings.ReplaceAll(s, "\t", "\\t")
	}
	headers := make([]string, len(columns))
	for i, c := range columns {
		headers[i] = escape(c)
	}
	fmt.Fprintln(writer, strings.Join(headers, "\t"))
	for _, record := range cells {
		for i := range record {
			record[i] = escape(record[i])
		}
		fmt.Fprintln(writer, strings.Join(record, "\t"))
	}
	writer.Flush()
	return b.String(), nil
}
