package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	proxy := flag.String("proxy", "", "Path to mcp-proxy binary")
	server := flag.String("server", "", "Path to filesystem-ultra binary")
	out := flag.String("out", "sample-report.json", "Output report path")
	keep := flag.Bool("keep-workdir", false, "Keep the generated workspace")
	flag.Parse()
	if *proxy == "" || *server == "" {
		fmt.Fprintln(os.Stderr, "Usage: go run . -proxy <mcp-proxy> -server <filesystem-ultra> [-out report.json]")
		os.Exit(2)
	}

	workDir, err := os.MkdirTemp("", "fs-ultra-pr0-")
	if err != nil {
		fail(err)
	}
	if !*keep {
		defer os.RemoveAll(workDir)
	}
	if err := seedTree(workDir, 2000); err != nil {
		fail(err)
	}
	logDir := filepath.Join(workDir, "proxy-log")
	if err := os.MkdirAll(logDir, 0700); err != nil {
		fail(err)
	}
	client, err := startRPC(*proxy, logDir, *server, workDir)
	if err != nil {
		fail(err)
	}
	defer client.close()

	slices := make(map[string][2]int)
	run := func(name string, scenario func() error) {
		before, err := loadProxyLog(filepath.Join(logDir, "proxy.jsonl"))
		if err != nil && !os.IsNotExist(err) {
			fail(err)
		}
		if err := scenario(); err != nil {
			fail(fmt.Errorf("%s: %w", name, err))
		}
		after, err := loadProxyLog(filepath.Join(logDir, "proxy.jsonl"))
		if err != nil {
			fail(err)
		}
		slices[name] = [2]int{len(before), len(after)}
	}

	run("explore_2k", func() error {
		if _, err := client.tool("list_allowed_directories", map[string]any{}); err != nil {
			return err
		}
		if _, err := client.tool("directory_tree", map[string]any{"path": workDir, "max_depth": 2, "max_nodes": 500}); err != nil {
			return err
		}
		_, err := client.tool("search_files", map[string]any{"path": workDir, "pattern": "needle", "include_content": true, "file_types": ".go", "max_results": 20})
		return err
	})

	occPath := filepath.Join(workDir, "occ.txt")
	run("occ_clash", func() error {
		if _, err := client.tool("write_file", map[string]any{"path": occPath, "content": "original\n"}); err != nil {
			return err
		}
		read, err := client.tool("read_file", map[string]any{"path": occPath})
		if err != nil {
			return err
		}
		hash, _ := structured(read)["content_hash"].(string)
		if hash == "" {
			return fmt.Errorf("read_file returned no content_hash")
		}
		if err := os.WriteFile(occPath, []byte("external change\n"), 0600); err != nil {
			return err
		}
		res, err := client.tool("edit_file", map[string]any{"path": occPath, "old_text": "original", "new_text": "agent", "expected_hash": hash})
		if err != nil {
			return err
		}
		if !isError(res) || !strings.Contains(strings.ToLower(resultText(res)), "stale edit") {
			return fmt.Errorf("expected stale OCC edit, got %q", resultText(res))
		}
		return nil
	})

	patchPath := filepath.Join(workDir, "patch.txt")
	if err := os.WriteFile(patchPath, []byte("actual context\n"), 0600); err != nil {
		fail(err)
	}
	run("patch_fail", func() error {
		patch := "--- a/patch.txt\n+++ b/patch.txt\n@@ -1 +1 @@\n-wrong context\n+patched\n"
		res, err := client.tool("apply_patch", map[string]any{"path": patchPath, "patch": patch})
		if err != nil {
			return err
		}
		if !isError(res) || !strings.Contains(resultText(res), "PATCH_APPLY_FAILED") {
			return fmt.Errorf("expected PATCH_APPLY_FAILED, got %q", resultText(res))
		}
		return nil
	})

	run("help_catalog", func() error {
		_, err := client.tool("help", map[string]any{})
		return err
	})

	entries, err := loadProxyLog(filepath.Join(logDir, "proxy.jsonl"))
	if err != nil {
		fail(err)
	}
	report := summarize(entries, slices)
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		fail(err)
	}
	if err := os.WriteFile(*out, append(data, '\n'), 0644); err != nil {
		fail(err)
	}
	fmt.Printf("PR-0 report: %s\n", *out)
	fmt.Println(report.Decision)
	if *keep {
		fmt.Printf("Workspace retained: %s\n", workDir)
	}
}

func seedTree(root string, count int) error {
	for i := 0; i < count; i++ {
		dir := filepath.Join(root, fmt.Sprintf("pkg-%03d", i%100))
		if err := os.MkdirAll(dir, 0700); err != nil {
			return err
		}
		body := "package fixture\n\nconst Value = \"ordinary\"\n"
		if i%100 == 0 {
			body = "package fixture\n\nconst Value = \"needle\"\n"
		}
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("file-%04d.go", i)), []byte(body), 0600); err != nil {
			return err
		}
	}
	return nil
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "pr0-baseline:", err)
	os.Exit(1)
}
