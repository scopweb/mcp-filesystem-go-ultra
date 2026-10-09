package mcpserver

import (
	"testing"
)

func TestAnnotations_OpenWorldMatchesBehavior(t *testing.T) {
	dir := t.TempDir()
	s, _ := newIncidentFixServer(t, dir)
	alwaysOpen := map[string]bool{
		"github_issues": true,
		"gitlab_issues": true,
	}
	for name, tool := range s.ListTools() {
		hint := tool.Tool.Annotations.OpenWorldHint
		if name == "git" {
			if hint != nil && *hint {
				t.Fatal("git must not be open-world without --git-network")
			}
			continue
		}
		want := alwaysOpen[name]
		if hint == nil || *hint != want {
			t.Fatalf("%s openWorldHint=%v want %v", name, hint, want)
		}
	}
	info := s.ListTools()["server_info"]
	if info.Tool.Annotations.ReadOnlyHint == nil || *info.Tool.Annotations.ReadOnlyHint {
		t.Fatal("server_info must stay writable: artifact/write")
	}
	if info.Tool.Annotations.DestructiveHint == nil || !*info.Tool.Annotations.DestructiveHint {
		t.Fatal("server_info artifact/write can overwrite a file")
	}
}
