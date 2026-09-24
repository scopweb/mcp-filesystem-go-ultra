package mcpserver

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func syntaxWarning(path string) string {
	kind := syntaxKind(path)
	if kind == "" {
		return ""
	}
	b, err := os.ReadFile(path)
	if err != nil || len(bytes.TrimSpace(b)) == 0 {
		return ""
	}
	switch kind {
	case "json":
		if json.Valid(b) {
			return ""
		}
		return "\n⚠ syntax: JSON is not valid after this edit"
	case "xml":
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
	return ""
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
