package core

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var textSymbolExts = map[string]string{
	".js": "js", ".jsx": "js", ".mjs": "js", ".cjs": "js",
	".ts": "ts", ".tsx": "ts",
	".cs":  "cs",
	".sql": "sql",
}

var (
	jsFuncRe  = regexp.MustCompile(`(?m)^[ \t]*(?:export[ \t]+)?(?:default[ \t]+)?(?:async[ \t]+)?function[ \t]+([A-Za-z_$][\w$]*)`)
	jsClassRe = regexp.MustCompile(`(?m)^[ \t]*(?:export[ \t]+)?(?:default[ \t]+)?class[ \t]+([A-Za-z_$][\w$]*)`)
	jsArrowRe = regexp.MustCompile(`(?m)^[ \t]*(?:export[ \t]+)?(?:const|let|var)[ \t]+([A-Za-z_$][\w$]*)[ \t]*=[ \t]*(?:async[ \t]+)?function\b`)
	csTypeRe  = regexp.MustCompile(`(?m)^[ \t]*(?:(?:public|private|internal|protected|static|partial|abstract|sealed|readonly|unsafe|new)[ \t]+)*(class|interface|struct|enum|record)[ \t]+([A-Za-z_][\w]*)`)
	csMethRe  = regexp.MustCompile(`(?m)^[ \t]*(?:(?:public|private|internal|protected|static|virtual|override|abstract|async|extern|sealed|partial|new)[ \t]+)+([A-Za-z_][\w.<>,\[\]?]*)[ \t]+([A-Za-z_][\w]*)[ \t]*[(]`)
	sqlObjRe  = regexp.MustCompile(`(?im)^[ \t]*CREATE[ \t]+(?:OR[ \t]+ALTER[ \t]+)?(?:PROC(?:EDURE)?|FUNCTION|VIEW|TRIGGER)[ \t]+(?:\[?[A-Za-z_][\w]*\]?\.)?\[?([A-Za-z_][\w]*)\]?`)
)

func analyzeTextSymbols(path, query string, maxFindings int) (AnalyzeResult, bool) {
	files, err := textSymbolFiles(path)
	res := AnalyzeResult{Action: "symbols", Tool: "regex", Findings: []AnalyzeFinding{}, Retryable: false}
	if err != nil {
		res.Status = "error"
		res.Message = err.Error()
		return res, true
	}
	if len(files) == 0 {
		return res, false
	}
	for _, fpath := range files {
		b, err := os.ReadFile(fpath)
		if err != nil {
			continue
		}
		lang := textSymbolExts[strings.ToLower(filepath.Ext(fpath))]
		scanTextSymbols(&res, fpath, string(b), lang, query)
	}
	res.Findings, res.Truncated = capFindings(res.Findings, maxFindings)
	if len(res.Findings) == 0 {
		res.Status = "empty"
	} else {
		res.Status = "ok"
	}
	res.Message = formatAnalyzeText(res)
	return res, true
}

func textSymbolFiles(path string) ([]string, error) {
	st, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !st.IsDir() {
		if _, ok := textSymbolExts[strings.ToLower(filepath.Ext(path))]; ok {
			return []string{path}, nil
		}
		return nil, nil
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if _, ok := textSymbolExts[strings.ToLower(filepath.Ext(e.Name()))]; ok {
			out = append(out, filepath.Join(path, e.Name()))
		}
	}
	return out, nil
}

func scanTextSymbols(res *AnalyzeResult, path, content, lang, query string) {
	switch lang {
	case "js", "ts":
		addRegexSymbols(res, path, content, "func", jsFuncRe, 1, query)
		addRegexSymbols(res, path, content, "class", jsClassRe, 1, query)
		addRegexSymbols(res, path, content, "func", jsArrowRe, 1, query)
	case "cs":
		addRegexSymbols(res, path, content, "type", csTypeRe, 2, query)
		addRegexSymbols(res, path, content, "method", csMethRe, 2, query)
	case "sql":
		addRegexSymbols(res, path, content, "object", sqlObjRe, 1, query)
	}
}

func addRegexSymbols(res *AnalyzeResult, path, content, kind string, re *regexp.Regexp, nameGroup int, query string) {
	for _, loc := range re.FindAllStringSubmatchIndex(content, -1) {
		if len(loc) < (nameGroup+1)*2 || loc[nameGroup*2] < 0 {
			continue
		}
		name := content[loc[nameGroup*2]:loc[nameGroup*2+1]]
		if query != "" && !strings.Contains(strings.ToLower(name), strings.ToLower(query)) {
			continue
		}
		if kind == "method" && (name == "class" || name == "interface" || name == "struct" || name == "enum" || name == "record" || name == "if" || name == "for" || name == "while" || name == "switch" || name == "catch") {
			continue
		}
		line := strings.Count(content[:loc[0]], "\n") + 1
		res.Findings = append(res.Findings, AnalyzeFinding{
			Path:     path,
			Line:     line,
			Symbol:   name,
			Kind:     kind,
			Message:  kind + " " + name,
			Severity: "info",
		})
	}
}
