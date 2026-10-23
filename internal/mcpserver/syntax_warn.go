package mcpserver

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"errors"
	"io"
	"path/filepath"
	"strconv"
	"strings"
)

// syntaxWarning reports ill-formed JSON or XML after an edit. It validates the
// bytes already in memory and never rewrites the file. A UTF-8 BOM is stripped
// before the JSON check only. If the file was already invalid, the edit is not
// blamed. Comments and trailing commas stay invalid: that is "strict JSON
// failed", not corruption.
func syntaxWarning(path string, before, after []byte) string {
	kind := syntaxKind(path)
	if kind == "" {
		return ""
	}
	if len(bytes.TrimSpace(stripUTF8BOM(after))) == 0 {
		return ""
	}
	switch kind {
	case "json":
		return jsonEditWarning(before, after)
	case "xml":
		return xmlSyntaxWarning(after)
	}
	return ""
}

func jsonEditWarning(before, after []byte) string {
	checked := stripUTF8BOM(after)
	problem := strictJSONProblem(checked)
	if problem == nil {
		return ""
	}
	if jsonAlreadyInvalid(before) {
		return ""
	}
	line, col := lineColAt(checked, problem.index)
	if problem.relaxed {
		return formatJSONWarn("strict JSON failed after this edit", line, col)
	}
	return formatJSONWarn("JSON is not valid after this edit", line, col)
}

func formatJSONWarn(kind string, line, col int) string {
	return "\n⚠ syntax: " + kind + " (line " + strconv.Itoa(line) + ", column " + strconv.Itoa(col) + ")"
}

func jsonAlreadyInvalid(before []byte) bool {
	before = stripUTF8BOM(before)
	if len(bytes.TrimSpace(before)) == 0 {
		return false
	}
	return strictJSONProblem(before) != nil
}

type jsonProblem struct {
	index   int
	relaxed bool
}

func strictJSONProblem(b []byte) *jsonProblem {
	dec := json.NewDecoder(bytes.NewReader(b))
	var v any
	err := dec.Decode(&v)
	if err == nil {
		restStart := int(dec.InputOffset())
		if restStart < 0 {
			restStart = 0
		}
		if restStart > len(b) {
			restStart = len(b)
		}
		rest := b[restStart:]
		trimmed := bytes.TrimLeft(rest, " \t\r\n")
		if len(trimmed) == 0 {
			return nil
		}
		idx := restStart + (len(rest) - len(trimmed))
		return &jsonProblem{index: idx, relaxed: looksLikeCommentOrTrailingComma(b, idx)}
	}
	idx := 0
	var syn *json.SyntaxError
	if errors.As(err, &syn) && syn.Offset > 0 {
		idx = int(syn.Offset) - 1
	} else if off := int(dec.InputOffset()); off > 0 {
		idx = off - 1
	}
	if idx < 0 {
		idx = 0
	}
	if idx >= len(b) && len(b) > 0 {
		idx = len(b) - 1
	}
	return &jsonProblem{index: idx, relaxed: looksLikeCommentOrTrailingComma(b, idx)}
}

func looksLikeCommentOrTrailingComma(b []byte, idx int) bool {
	if len(b) == 0 {
		return false
	}
	if idx < 0 {
		idx = 0
	}
	if idx >= len(b) {
		idx = len(b) - 1
	}
	start := idx - 1
	if start < 0 {
		start = 0
	}
	end := idx + 2
	if end > len(b) {
		end = len(b)
	}
	win := b[start:end]
	if bytes.Contains(win, []byte("//")) || bytes.Contains(win, []byte("/*")) || bytes.Contains(win, []byte("*/")) {
		return true
	}
	if b[idx] == '/' {
		return true
	}
	if b[idx] == ',' {
		rest := bytes.TrimLeft(b[idx+1:], " \t\r\n")
		if len(rest) > 0 && (rest[0] == '}' || rest[0] == ']') {
			return true
		}
	}
	if b[idx] == '}' || b[idx] == ']' {
		prev := bytes.TrimRight(b[:idx], " \t\r\n")
		if len(prev) > 0 && prev[len(prev)-1] == ',' {
			return true
		}
	}
	return false
}

func lineColAt(b []byte, idx int) (int, int) {
	if idx < 0 {
		idx = 0
	}
	if idx > len(b) {
		idx = len(b)
	}
	line, col := 1, 1
	for i := 0; i < idx; i++ {
		if b[i] == '\n' {
			line++
			col = 1
		} else {
			col++
		}
	}
	return line, col
}

func stripUTF8BOM(b []byte) []byte {
	return bytes.TrimPrefix(b, []byte{0xEF, 0xBB, 0xBF})
}

func xmlSyntaxWarning(b []byte) string {
	dec := xml.NewDecoder(bytes.NewReader(b))
	for {
		_, err := dec.Token()
		if err == io.EOF {
			return ""
		}
		if err != nil {
			msg := err.Error()
			if len(msg) > 160 {
				msg = msg[:160]
			}
			return "\n⚠ syntax: XML is not well-formed after this edit: " + msg
		}
	}
}

func syntaxKind(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".json":
		return "json"
	case ".xml", ".csproj", ".config", ".resx", ".xaml", ".xsd":
		return "xml"
	}
	low := strings.ToLower(path)
	if strings.HasSuffix(low, ".csproj") {
		return "xml"
	}
	return ""
}
