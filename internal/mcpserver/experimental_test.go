package mcpserver

// Tests for the Fase 3 stability-tier policy (experimental.go + addTool guard).

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

var semverRe = regexp.MustCompile(`^\d+\.\d+\.\d+$`)

// TestExperimental_EntriesWellFormed validates the registry hygiene: every
// entry has a semver "since", and no entry is STALE — an entry introduced in
// a version older than the current serverVersion should have graduated
// (sweep coverage + optional outputSchema) and been removed by now.
func TestExperimental_EntriesWellFormed(t *testing.T) {
	for key, since := range experimentalFeatures {
		if !semverRe.MatchString(since) {
			t.Errorf("experimental feature %q: since %q is not semver (X.Y.Z)", key, since)
		}
		if semverLessThan(since, serverVersion) {
			t.Errorf("experimental feature %q is STALE (since v%s, current v%s): graduate it and remove the entry",
				key, since, serverVersion)
		}
	}
}

// TestGraduated_NotInExperimentalRegistry keeps the two registries disjoint:
// a graduated feature must not sit in experimentalFeatures again.
func TestGraduated_NotInExperimentalRegistry(t *testing.T) {
	for key := range graduatedFeatures {
		if _, ok := experimentalFeatures[key]; ok {
			t.Errorf("feature %q is graduated and experimental at the same time", key)
		}
	}
}

// TestGraduated_ToolEntriesAreRegistered ensures the graduated tool-level
// keys (keys without ':') still name a registered tool — a typo would hide a
// graduation from the checks below.
func TestGraduated_ToolEntriesAreRegistered(t *testing.T) {
	reg := newHelpTestRegistry(t, t.TempDir())
	for key := range graduatedFeatures {
		if strings.Contains(key, ":") {
			continue // mode-level entries are informational
		}
		if _, ok := reg.handlers[key]; !ok {
			t.Errorf("graduated feature %q does not match any registered tool", key)
		}
	}
}

// TestGraduated_NoExperimentalPrefix verifies that a graduated tool lost the
// [EXPERIMENTAL ...] description prefix. The prefix promises graduation in
// the next release cycle; keeping it after graduation would be a lie.
func TestGraduated_NoExperimentalPrefix(t *testing.T) {
	reg := newHelpTestRegistry(t, t.TempDir())
	tools := reg.server.ListTools()
	for key := range graduatedFeatures {
		if strings.Contains(key, ":") {
			continue // mode-level entries are informational
		}
		st, ok := tools[key]
		if !ok {
			continue // profile without this tool; registration is checked above
		}
		if strings.Contains(st.Tool.Description, "[EXPERIMENTAL") {
			t.Errorf("graduated tool %q still carries the experimental prefix: %q", key, st.Tool.Description)
		}
	}
}

// TestGraduated_PendingSchemaToolsDeclareNoSchema keeps the "a schema is
// earned by the conformance sweep" rule alive for the graduated cohort. A
// graduated tool listed in pendingSchema must declare NO outputSchema yet:
// adding one is a separate change that also adds the sweep case and drops the
// key from pendingSchema.
func TestGraduated_PendingSchemaToolsDeclareNoSchema(t *testing.T) {
	reg := newHelpTestRegistry(t, t.TempDir())
	tools := reg.server.ListTools()
	for key := range pendingSchema {
		if _, experimental := experimentalFeatures[key]; experimental {
			t.Errorf("feature %q is still experimental; it cannot be in pendingSchema", key)
		}
		st, ok := tools[key]
		if !ok {
			continue // profile without this tool
		}
		if st.Tool.RawOutputSchema != nil || st.Tool.OutputSchema.Type != "" {
			t.Errorf("tool %q is in pendingSchema and must not declare an outputSchema yet", key)
		}
	}
}

// TestExperimental_ToolEntriesAreRegistered ensures tool-level entries (keys
// without ':') name a tool that is actually registered — a typo in the map
// would silently disable enforcement for that feature.
func TestExperimental_ToolEntriesAreRegistered(t *testing.T) {
	reg := newHelpTestRegistry(t, t.TempDir())
	for key := range experimentalFeatures {
		if strings.Contains(key, ":") {
			continue // mode-level entries are informational
		}
		if _, ok := reg.handlers[key]; !ok {
			t.Errorf("experimental feature %q does not match any registered tool", key)
		}
	}
}

// TestExperimental_SchemaPanics verifies the addTool guard: an experimental
// tool declaring an outputSchema must panic at registration.
func TestExperimental_SchemaPanics(t *testing.T) {
	experimentalFeatures["zz_experimental_tool"] = serverVersion
	defer delete(experimentalFeatures, "zz_experimental_tool")

	reg := newHelpTestRegistry(t, t.TempDir())
	tool := mcp.NewTool("zz_experimental_tool",
		mcp.WithDescription("test"),
		mcp.WithRawOutputSchema(json.RawMessage(`{"type":"object"}`)),
	)

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic when registering an experimental tool WITH outputSchema")
		}
	}()
	reg.addTool(tool, reg.handlers["read_file"])
}

// TestExperimental_DescriptionPrefixed verifies that an experimental tool
// without a schema registers fine and gets the EXPERIMENTAL prefix.
func TestExperimental_DescriptionPrefixed(t *testing.T) {
	experimentalFeatures["zz_experimental_tool"] = serverVersion
	defer delete(experimentalFeatures, "zz_experimental_tool")

	tool := mcp.NewTool("zz_experimental_tool", mcp.WithDescription("does things"))

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("unexpected panic registering schema-less experimental tool: %v", r)
		}
	}()
	tool = applyExperimentalPolicy(tool)
	if !strings.HasPrefix(tool.Description, "[EXPERIMENTAL since v") {
		t.Errorf("description not prefixed: %q", tool.Description)
	}
}

// TestExperimental_StableToolsUntouched verifies that tools NOT in the
// experimental map pass through the policy without any mutation.
func TestExperimental_StableToolsUntouched(t *testing.T) {
	tool := mcp.NewTool("zz_stable_tool", mcp.WithDescription("plain"))
	tool = applyExperimentalPolicy(tool)
	if tool.Description != "plain" {
		t.Errorf("stable tool description mutated: %q", tool.Description)
	}
}

// semverLessThan compares X.Y.Z versions numerically.
func semverLessThan(a, b string) bool {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	if len(pa) != 3 || len(pb) != 3 {
		return false
	}
	for i := 0; i < 3; i++ {
		var na, nb int
		fmt.Sscanf(pa[i], "%d", &na)
		fmt.Sscanf(pb[i], "%d", &nb)
		if na != nb {
			return na < nb
		}
	}
	return false
}
