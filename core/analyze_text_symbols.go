package core

import (
	"path/filepath"
	"regexp"
	"strings"
)

var textSymbolExts = map[string]string{
	".js": "js", ".jsx": "js", ".mjs": "js", ".cjs": "js",
	".ts": "ts", ".tsx": "ts",
	".cs":    "cs",
	".sql":   "sql",
	".razor": "razor",
}

var (
	jsFuncRe  = regexp.MustCompile(`(?m)^[ \t]*(?:export[ \t]+)?(?:default[ \t]+)?(?:async[ \t]+)?function[ \t]+([A-Za-z_$][\w$]*)`)
	jsClassRe = regexp.MustCompile(`(?m)^[ \t]*(?:export[ \t]+)?(?:default[ \t]+)?class[ \t]+([A-Za-z_$][\w$]*)`)
	jsArrowRe = regexp.MustCompile(`(?m)^[ \t]*(?:export[ \t]+)?(?:const|let|var)[ \t]+([A-Za-z_$][\w$]*)[ \t]*=[ \t]*(?:async[ \t]+)?function\b`)
	csTypeRe  = regexp.MustCompile(`(?m)^[ \t]*(?:(?:public|private|internal|protected|static|partial|abstract|sealed|readonly|unsafe|new)[ \t]+)*(class|interface|struct|enum|record)[ \t]+([A-Za-z_][\w]*)`)
	csMethRe  = regexp.MustCompile(`(?m)^[ \t]*(?:(?:public|private|internal|protected|static|virtual|override|abstract|async|extern|sealed|partial|new)[ \t]+)+([A-Za-z_][\w.<>,\[\]?]*)[ \t]+([A-Za-z_][\w]*)[ \t]*[(]`)
	sqlObjRe  = regexp.MustCompile(`(?im)^[ \t]*CREATE[ \t]+(?:OR[ \t]+ALTER[ \t]+)?(?:PROC(?:EDURE)?|FUNCTION|VIEW|TRIGGER)[ \t]+(?:\[?[A-Za-z_][\w]*\]?\.)?\[?([A-Za-z_][\w]*)\]?`)
)

func analyzeTextSymbols(e *UltraFastEngine, path, query string, maxFindings int) (AnalyzeResult, bool) {
	files, err := textSymbolFiles(e, path)
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
		if analyzeBlocked(fpath) {
			continue
		}
		b, err := analyzeRead(e, fpath)
		if err != nil {
			continue
		}
		lang := textSymbolExts[strings.ToLower(filepath.Ext(fpath))]
		text := string(b)
		scanTextSymbols(&res, fpath, text, lang, query, contentHashFNV(text))
		if len(files) == 1 {
			res.ContentHash = contentHashFNV(text)
		}
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

func textSymbolFiles(e *UltraFastEngine, path string) ([]string, error) {
	st, err := analyzeStat(e, path)
	if err != nil {
		return nil, err
	}
	if !st.IsDir() {
		if _, ok := textSymbolExts[strings.ToLower(filepath.Ext(path))]; ok {
			return []string{path}, nil
		}
		return nil, nil
	}
	entries, err := analyzeReadDir(e, path)
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

func scanTextSymbols(res *AnalyzeResult, path, content, lang, query, hash string) {
	switch lang {
	case "js", "ts":
		addRegexSymbols(res, path, content, "func", jsFuncRe, 1, query, lang, hash)
		addRegexSymbols(res, path, content, "class", jsClassRe, 1, query, lang, hash)
		addRegexSymbols(res, path, content, "func", jsArrowRe, 1, query, lang, hash)
	case "cs":
		addRegexSymbols(res, path, content, "type", csTypeRe, 2, query, lang, hash)
		addRegexSymbols(res, path, content, "method", csMethRe, 2, query, lang, hash)
	case "razor":
		scanRazorSymbols(res, path, content, query, hash)
	case "sql":
		addRegexSymbols(res, path, content, "object", sqlObjRe, 1, query, lang, hash)
	}
}

func scanRazorSymbols(res *AnalyzeResult, path, content, query, hash string) {
	for _, block := range razorCodeBlocks(content) {
		before := len(res.Findings)
		scanTextSymbols(res, path, block.text, "cs", query, hash)
		offset := lineNumberAt(content, block.start) - 1
		for i := before; i < len(res.Findings); i++ {
			f := &res.Findings[i]
			if f.Line > 0 {
				f.Line += offset
			}
			if f.StartLine > 0 {
				f.StartLine += offset
			}
			if f.EndLine > 0 {
				f.EndLine += offset
			}
		}
	}
}

func addRegexSymbols(res *AnalyzeResult, path, content, kind string, re *regexp.Regexp, nameGroup int, query, lang, hash string) {
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
		f := AnalyzeFinding{
			Path:        path,
			Line:        line,
			Symbol:      name,
			Kind:        kind,
			Message:     kind + " " + name,
			Severity:    "info",
			ContentHash: hash,
		}
		switch lang {
		case "cs":
			annotateCS(&f, content, loc[0], loc[1], kind)
		case "js", "ts":
			annotateJS(&f, content, loc[0], loc[1])
		default:
			f.StartLine = line
			f.RangeStatus = symbolRangeUnavailable
			f.RangeKind = symbolRangeDeclaration
			f.Parser = "regex"
		}
		res.Findings = append(res.Findings, f)
	}
}
