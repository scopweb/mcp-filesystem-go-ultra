package mcpserver

import (
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mcp/filesystem-ultra/core"
)

// A panic inside multi_edit must stay inside the tool: the SDK recovery
// middleware only reports "panic recovered in multi_edit tool handler", with no
// path and no hint about the file state. The handler's own recovery reports
// the path and says whether the file was touched before the failure.
func TestMultiEditPanicRecoveryReportsFileState(t *testing.T) {
	cases := []struct {
		phase string
		want  string
	}{
		{phase: "", want: "not modified"},
		{phase: "preview", want: "not modified"},
		{phase: "editing", want: "may be partially modified"},
	}
	for _, tc := range cases {
		t.Run(tc.phase, func(t *testing.T) {
			res, err := func() (response *mcp.CallToolResult, responseErr error) {
				defer recoverMultiEditPanic("C:/x/Program.cs", tc.phase, nil, &response, &responseErr)
				panic("synthetic failure")
			}()
			if err != nil || res == nil || !res.IsError {
				t.Fatalf("expected an error receipt: %v, %v", res, err)
			}
			body := resultText(t, res)
			if !strings.Contains(body, "FAILED") || !strings.Contains(body, "Program.cs") {
				t.Fatalf("missing failure context: %q", body)
			}
			if !strings.Contains(body, tc.want) {
				t.Fatalf("missing %q in %q", tc.want, body)
			}
		})
	}
}

// After the edits are on disk the recovery must keep the undo receipt: the
// caller has to be able to roll back, and must not be told the file is intact.
func TestMultiEditPanicRecoveryKeepsUndoAfterCommit(t *testing.T) {
	result := &core.MultiEditResult{SuccessfulEdits: 1, TotalEdits: 1, BackupID: "undo-after-panic"}
	res, err := func() (response *mcp.CallToolResult, responseErr error) {
		defer recoverMultiEditPanic("C:/x/Program.cs", "committed", result, &response, &responseErr)
		panic("synthetic failure")
	}()
	if err != nil || res == nil || !res.IsError {
		t.Fatalf("expected an error receipt: %v, %v", res, err)
	}
	body := resultText(t, res)
	if !strings.Contains(body, "MODIFIED") || !strings.Contains(body, "UNDO:"+result.BackupID) {
		t.Fatalf("lost committed state: %q", body)
	}
}
