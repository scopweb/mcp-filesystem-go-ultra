package core

import (
	"fmt"
	"strings"
	"unicode"
)

// Optional caller-trace fields for orchestrators. Logged and echoed on success.
// They do not authorize paths, bypass OCC, or change security_policy.

const (
	AuthIDMaxBytes        = 128
	AuthScopeNoteMaxBytes = 256

	AuthStrengthExplicit = "explicit"
	AuthStrengthInferred = "inferred"

	// Receipt advice only. The write still runs.
	AuthNoticeInferredDryRun = "strength=inferred is trace only and does not authorize this write; prefer dry_run:true, then verify with git diff --stat"
	AuthNoticeInferred       = "strength=inferred is trace only and does not authorize this write; verify with git diff --stat"
)

// AuthTrace is optional authorization metadata supplied by an orchestrator.
// The zero value means the caller did not pass any field.
type AuthTrace struct {
	MandateID     string
	PolicyVersion string
	Strength      string
	TicketID      string
	ScopeNote     string
}

// Empty reports whether the caller omitted every trace field.
func (a AuthTrace) Empty() bool {
	return a == AuthTrace{}
}

// ParseAuthTrace reads already-validated arguments. Empty strings are omitted.
func ParseAuthTrace(args map[string]interface{}) AuthTrace {
	if args == nil {
		return AuthTrace{}
	}
	return AuthTrace{
		MandateID:     authString(args, "mandate_id"),
		PolicyVersion: authString(args, "policy_version"),
		Strength:      authString(args, "strength"),
		TicketID:      authString(args, "ticket_id"),
		ScopeNote:     authString(args, "scope_note"),
	}
}

func authString(args map[string]interface{}, key string) string {
	s, _ := args[key].(string)
	return s
}

// Apply copies the trace onto an audit entry. Empty fields stay empty so
// omitempty drops them from operations.jsonl.
func (a AuthTrace) Apply(entry *AuditEntry) {
	if entry == nil {
		return
	}
	entry.MandateID = a.MandateID
	entry.PolicyVersion = a.PolicyVersion
	entry.Strength = a.Strength
	entry.TicketID = a.TicketID
	entry.ScopeNote = a.ScopeNote
}

// Overlay returns a copy of base with every non-empty call field replacing it.
func (base AuthTrace) Overlay(call AuthTrace) AuthTrace {
	if call.MandateID != "" {
		base.MandateID = call.MandateID
	}
	if call.PolicyVersion != "" {
		base.PolicyVersion = call.PolicyVersion
	}
	if call.Strength != "" {
		base.Strength = call.Strength
	}
	if call.TicketID != "" {
		base.TicketID = call.TicketID
	}
	if call.ScopeNote != "" {
		base.ScopeNote = call.ScopeNote
	}
	return base
}

// SetAuthTrace replaces the process-wide trace. The zero value clears it.
// It does not authorize paths. A process is not a conversation.
func (e *UltraFastEngine) SetAuthTrace(trace AuthTrace) {
	if e == nil {
		return
	}
	e.authTraceMu.Lock()
	e.authTrace = trace
	e.authTraceMu.Unlock()
}

// AuthTrace returns the process-wide trace. The zero value means none is set.
func (e *UltraFastEngine) AuthTrace() AuthTrace {
	if e == nil {
		return AuthTrace{}
	}
	e.authTraceMu.RLock()
	defer e.authTraceMu.RUnlock()
	return e.authTrace
}

// AuthNotice is receipt advice only. It does not change whether the write runs.
// A dry-run already followed the inferred-strength recommendation, so it is silent.
func AuthNotice(tool string, dryRun bool, strength string) string {
	if strength != AuthStrengthInferred || dryRun {
		return ""
	}
	switch tool {
	case "edit_file", "multi_edit", "apply_patch":
		return AuthNoticeInferredDryRun
	default:
		return AuthNoticeInferred
	}
}

// validateAuthArgs checks length and printable text for trace fields the tool
// schema accepts. Type and the strength enum are checked by ValidateToolParams.
func validateAuthArgs(schema ToolParamSchema, args map[string]interface{}) []string {
	if schema == nil || args == nil {
		return nil
	}
	var errs []string
	for _, name := range []string{"mandate_id", "policy_version", "strength", "ticket_id", "scope_note"} {
		if _, known := schema[name]; !known {
			continue
		}
		v, ok := args[name]
		if !ok || v == nil {
			continue
		}
		s, ok := v.(string)
		if !ok || s == "" {
			continue
		}
		max := AuthIDMaxBytes
		switch name {
		case "scope_note":
			max = AuthScopeNoteMaxBytes
		case "strength":
			max = 16
		}
		if len(s) > max {
			errs = append(errs, fmt.Sprintf("parameter %q: must be at most %d bytes, got %d", name, max, len(s)))
			continue
		}
		if strings.TrimSpace(s) != s {
			errs = append(errs, fmt.Sprintf("parameter %q: must not have leading or trailing whitespace", name))
			continue
		}
		if !printableAuth(s) {
			errs = append(errs, fmt.Sprintf("parameter %q: must be a printable string", name))
		}
	}
	return errs
}

func printableAuth(s string) bool {
	for _, r := range s {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}
