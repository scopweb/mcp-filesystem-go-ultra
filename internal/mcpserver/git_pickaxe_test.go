package mcpserver

import (
	"strings"
	"testing"
)

func TestGitLog_PickaxeFindsIntroducingCommit(t *testing.T) {
	dir, engine := setupRepoWithFile(t)
	defer engine.Close()
	writeAndCommit(t, dir, "a.txt", "alpha\nunique-pickaxe-token\n", "introduces token")
	writeAndCommit(t, dir, "a.txt", "alpha\nunique-pickaxe-token\nbeta\n", "later")
	writeAndCommit(t, dir, "b.txt", "alpha\n-led-token\n", "introduces dash")

	handler := newRegisteredGitHandler(engine)
	res := callRegisteredGit(t, handler, map[string]interface{}{
		"action":  "log",
		"path":    dir,
		"pickaxe": "unique-pickaxe-token",
	})
	if res.IsError {
		t.Fatal(mcpText(t, res))
	}
	text := mcpText(t, res)
	if !strings.Contains(text, "introduces token") {
		t.Fatalf("pickaxe missed the introducing commit:\n%s", text)
	}
	if strings.Contains(text, "later") || strings.Contains(text, "init") {
		t.Fatalf("pickaxe listed a commit that did not change the string count:\n%s", text)
	}

	dashed := callRegisteredGit(t, handler, map[string]interface{}{
		"action":  "log",
		"path":    dir,
		"pickaxe": "-led-token",
	})
	if dashed.IsError {
		t.Fatal(mcpText(t, dashed))
	}
	if !strings.Contains(mcpText(t, dashed), "introduces dash") {
		t.Fatalf("leading-dash pickaxe was not data:\n%s", mcpText(t, dashed))
	}

	bad := callRegisteredGit(t, handler, map[string]interface{}{
		"action": "log",
		"path":   dir,
		"rev":    "-Sfoo",
	})
	if !bad.IsError || !strings.Contains(mcpText(t, bad), "must not start with") {
		t.Fatalf("rev:-Sfoo must stay rejected, got: %s", mcpText(t, bad))
	}
}

func TestGitLog_PickaxeRejectsBadPatterns(t *testing.T) {
	dir, engine := setupRepoWithFile(t)
	defer engine.Close()
	handler := newRegisteredGitHandler(engine)
	cases := []struct {
		name string
		args map[string]interface{}
		want string
	}{
		{"empty", map[string]interface{}{"action": "log", "path": dir, "pickaxe": ""}, "empty"},
		{"newline", map[string]interface{}{"action": "log", "path": dir, "pickaxe": "a\nb"}, "newline"},
		{"nul", map[string]interface{}{"action": "log", "path": dir, "pickaxe": "a\x00b"}, "NUL"},
		{"long", map[string]interface{}{"action": "log", "path": dir, "pickaxe": strings.Repeat("x", 513)}, "512"},
		{"not log", map[string]interface{}{"action": "status", "path": dir, "pickaxe": "token"}, `action:"log"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := callRegisteredGit(t, handler, tc.args)
			if !res.IsError || !strings.Contains(mcpText(t, res), tc.want) {
				t.Fatalf("want error containing %q, got: %s", tc.want, mcpText(t, res))
			}
		})
	}
}
