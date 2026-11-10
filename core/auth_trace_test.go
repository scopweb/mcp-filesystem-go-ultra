package core

import (
	"strings"
	"testing"
)

func TestAuthTrace_AcceptsOptionalFields(t *testing.T) {
	args := map[string]interface{}{
		"path":           "x",
		"content":        "y",
		"mandate_id":     "m-1",
		"policy_version": "pol-2026.11",
		"strength":       AuthStrengthExplicit,
		"ticket_id":      "tkt-9",
		"scope_note":     "worker scope",
	}
	if errs := ValidateToolParams("write_file", args); len(errs) != 0 {
		t.Fatal(errs)
	}
	got := ParseAuthTrace(args)
	if got.Empty() || got.MandateID != "m-1" || got.Strength != AuthStrengthExplicit || got.TicketID != "tkt-9" {
		t.Fatalf("%+v", got)
	}
	if errs := ValidateToolParams("write_file", map[string]interface{}{"path": "x", "content": "y"}); len(errs) != 0 {
		t.Fatalf("omitted trace must stay valid: %v", errs)
	}
}

func TestAuthTrace_RejectsBadStrengthLengthAndControls(t *testing.T) {
	cases := []struct {
		name string
		args map[string]interface{}
		want string
	}{
		{"strength", map[string]interface{}{"path": "x", "strength": "guessed"}, "invalid value"},
		{"length", map[string]interface{}{"path": "x", "mandate_id": strings.Repeat("a", AuthIDMaxBytes+1)}, "at most"},
		{"scope", map[string]interface{}{"path": "x", "scope_note": strings.Repeat("n", AuthScopeNoteMaxBytes+1)}, "at most"},
		{"space", map[string]interface{}{"path": "x", "ticket_id": " tkt"}, "whitespace"},
		{"control", map[string]interface{}{"path": "x", "scope_note": "note\nmore"}, "printable"},
	}
	for _, tc := range cases {
		errs := ValidateToolParams("write_file", tc.args)
		if len(errs) == 0 || !strings.Contains(strings.Join(errs, ";"), tc.want) {
			t.Errorf("%s: got %v", tc.name, errs)
		}
	}
	if errs := ValidateToolParams("read_file", map[string]interface{}{"path": "x", "mandate_id": "m"}); len(errs) == 0 {
		t.Fatal("read_file must reject mandate_id")
	}
	if errs := ValidateToolParams("edit_file", map[string]interface{}{"path": "x", "strength": AuthStrengthInferred}); len(errs) != 0 {
		t.Fatal(errs)
	}
	if errs := ValidateToolParams("apply_patch", map[string]interface{}{"path": "x", "patch": "p", "strength": "explicit"}); len(errs) != 0 {
		t.Fatal(errs)
	}
	if errs := ValidateToolParams("batch_operations", map[string]interface{}{"ticket_id": "t"}); len(errs) != 0 {
		t.Fatal(errs)
	}
}

func TestAuthNotice_OnlyInferredApply(t *testing.T) {
	if AuthNotice("edit_file", true, AuthStrengthInferred) != "" {
		t.Fatal("dry_run must not nag")
	}
	if AuthNotice("edit_file", false, AuthStrengthExplicit) != "" {
		t.Fatal("explicit must not nag")
	}
	if AuthNotice("edit_file", false, AuthStrengthInferred) != AuthNoticeInferredDryRun {
		t.Fatal("edit_file should recommend dry_run")
	}
	if AuthNotice("write_file", false, AuthStrengthInferred) != AuthNoticeInferred {
		t.Fatal("write_file has no dry_run")
	}
	if !ParseAuthTrace(nil).Empty() {
		t.Fatal("nil args")
	}
}

func TestAuthTrace_OverlayCallWins(t *testing.T) {
	base := AuthTrace{MandateID: "m", TicketID: "t", Strength: AuthStrengthExplicit}
	got := base.Overlay(AuthTrace{TicketID: "t2"})
	if got.TicketID != "t2" || got.MandateID != "m" || got.Strength != AuthStrengthExplicit {
		t.Fatalf("%+v", got)
	}
	if got = base.Overlay(AuthTrace{}); got != base {
		t.Fatalf("empty overlay changed base: %+v", got)
	}
}
