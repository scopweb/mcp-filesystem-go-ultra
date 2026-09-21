package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const reliabilityFileCount = 30

func seedReliability(root string) error {
	searchDir := filepath.Join(root, "rel-search")
	if err := os.MkdirAll(searchDir, 0700); err != nil {
		return err
	}
	for i := 1; i <= reliabilityFileCount; i++ {
		p := filepath.Join(searchDir, fmt.Sprintf("hit-%02d.txt", i))
		if err := os.WriteFile(p, []byte(fmt.Sprintf("TOKEN %d\n", i)), 0600); err != nil {
			return err
		}
	}
	prDir := filepath.Join(root, "rel-pr")
	if err := os.MkdirAll(prDir, 0700); err != nil {
		return err
	}
	for _, name := range []string{"a.txt", "b.txt", "c.txt"} {
		if err := os.WriteFile(filepath.Join(prDir, name), []byte("needle\n"), 0600); err != nil {
			return err
		}
	}
	mix := filepath.Join(root, "rel-mix.txt")
	if err := os.WriteFile(mix, []byte("foo\nbar\nfoo\n"), 0600); err != nil {
		return err
	}
	if err := exec.Command("git", "init", "-q", root).Run(); err != nil {
		return fmt.Errorf("git init: %w", err)
	}
	add := exec.Command("git", "-C", root, "remote", "add", "origin", "https://github.com/example/eval.git")
	if out, err := add.CombinedOutput(); err != nil {
		return fmt.Errorf("git remote add: %w (%s)", err, out)
	}
	return nil
}

func scenarioSearchPages(c *rpcClient, root string) error {
	dir := filepath.Join(root, "rel-search")
	seen := map[string]bool{}
	offset := 0
	page := 10
	for pageNum := 0; pageNum < 4; pageNum++ {
		args := map[string]any{
			"path": dir, "pattern": "TOKEN", "include_content": true,
			"max_results": page, "offset": offset,
		}
		res, err := c.tool("search_files", args)
		if err != nil {
			return err
		}
		if isError(res) {
			return fmt.Errorf("search_files: %s", resultText(res))
		}
		sc := structured(res)
		hits := matchPaths(sc["matches"])
		if len(hits) == 0 {
			return fmt.Errorf("empty page offset=%d", offset)
		}
		for _, p := range hits {
			if seen[p] {
				return fmt.Errorf("duplicate %s", p)
			}
			seen[p] = true
		}
		trunc, _ := sc["truncated"].(bool)
		if !trunc {
			break
		}
		cont, _ := sc["continuation"].(string)
		next := offset + len(hits)
		if !strings.Contains(cont, fmt.Sprintf("offset:%d", next)) {
			return fmt.Errorf("continuation want offset:%d got %q", next, cont)
		}
		offset = next
	}
	if len(seen) != reliabilityFileCount {
		return fmt.Errorf("got %d unique files, want %d", len(seen), reliabilityFileCount)
	}
	return nil
}

func scenarioMultiEditAmbiguous(c *rpcClient, root string) error {
	path := filepath.Join(root, "rel-mix.txt")
	original, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	res, err := c.tool("multi_edit", map[string]any{
		"path": path,
		"edits": []any{
			map[string]any{"old_text": "bar", "new_text": "baz"},
			map[string]any{"old_text": "foo", "new_text": "qux"},
		},
	})
	if err != nil {
		return err
	}
	text := resultText(res)
	if !isError(res) {
		return fmt.Errorf("expected rollback, got %q", text)
	}
	if strings.Contains(text, "Failed: .") {
		return fmt.Errorf("empty failed list: %s", text)
	}
	low := strings.ToLower(text)
	if !strings.Contains(low, "ambiguous") {
		return fmt.Errorf("expected ambiguous: %s", text)
	}
	if !strings.Contains(low, "edit 2") {
		return fmt.Errorf("expected edit 2: %s", text)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if string(got) != string(original) {
		return fmt.Errorf("file changed: %q", got)
	}
	return nil
}

func scenarioProjectReplaceFiles(c *rpcClient, root string) error {
	dir := filepath.Join(root, "rel-pr")
	base := map[string]any{"path": dir, "find": "needle", "replace": "pin", "preview": true}
	plain, err := c.tool("project_replace", base)
	if err != nil {
		return err
	}
	if isError(plain) {
		return fmt.Errorf("preview: %s", resultText(plain))
	}
	text := resultText(plain)
	if strings.Contains(text, "a.txt") {
		return fmt.Errorf("compact default leaked file list: %s", text)
	}
	base["detail"] = "full"
	full, err := c.tool("project_replace", base)
	if err != nil {
		return err
	}
	if isError(full) {
		return fmt.Errorf("detail full: %s", resultText(full))
	}
	ft := resultText(full)
	for _, name := range []string{"a.txt", "b.txt", "c.txt"} {
		if !strings.Contains(ft, name) {
			return fmt.Errorf("detail=full missing %s: %s", name, ft)
		}
	}
	return nil
}

func scenarioGitRemote(c *rpcClient, root string) error {
	res, err := c.tool("git", map[string]any{"action": "remote", "path": root})
	if err != nil {
		return err
	}
	text := resultText(res)
	if isError(res) {
		return fmt.Errorf("git remote: %s", text)
	}
	if strings.Contains(text, "s3cret") {
		return fmt.Errorf("credential leaked: %s", text)
	}
	if !strings.Contains(text, "origin") || !strings.Contains(text, "github.com/example/eval") {
		return fmt.Errorf("expected origin URL: %s", text)
	}
	return nil
}

func matchPaths(raw any) []string {
	arr, _ := raw.([]any)
	var out []string
	for _, item := range arr {
		m, _ := item.(map[string]any)
		p, _ := m["path"].(string)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

var reliabilityScenarios = []string{
	"search_pages",
	"multi_edit_ambiguous",
	"project_replace_files",
	"git_remote",
}

var pr0Scenarios = []string{
	"explore_2k",
	"occ_clash",
	"patch_fail",
	"help_catalog",
}
