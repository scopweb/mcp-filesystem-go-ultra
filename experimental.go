package main

// experimental.go — Feature stability tiers (Fase 3 del plan de endurecimiento).
//
// Policy:
//
//  1. New tools (or tool modes) ship as EXPERIMENTAL for exactly one release
//     cycle. Their description is automatically prefixed at registration time.
//  2. Experimental features MUST NOT declare an MCP outputSchema. The schema
//     is the interop contract; it is earned only when the feature passes the
//     handler-level conformance sweep (output_schema_sweep_test.go). The
//     registration chokepoint (toolRegistry.addTool) PANICS on violation —
//     a programmer error that must fail in dev/CI, never in a release.
//  3. An entry graduates — is removed from experimentalFeatures — in the
//     release after it was introduced, once the sweep covers it. Graduation
//     is when the outputSchema (if any) may be added.
//  4. The 17 core tools are frozen except bug fixes.
//
// Enforcement: toolRegistry.addTool (tools_core.go) consults
// applyExperimentalPolicy for every registered tool. Guarded by
// experimental_test.go.

import (
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
)

// experimentalFeatures maps a feature key to the version in which it was
// introduced. Tool-level keys are the tool name ("my_tool"); mode-level keys
// use "tool:mode" and are informational (enforcement is tool-level).
//
// KEEP THIS LIST SHORT: entries older than one release cycle are a policy
// violation — graduate them (sweep coverage + optional outputSchema) and
// remove the entry.
var experimentalFeatures = map[string]string{
	// Example (graduated — remove after one release):
	// "git:implicit-pathspec": "4.5.31",
}

// isExperimental reports whether featureKey is currently experimental and
// the version since which it has been.
func isExperimental(featureKey string) (since string, ok bool) {
	since, ok = experimentalFeatures[featureKey]
	return
}

// experimentalNotice is the description prefix applied to experimental tools.
func experimentalNotice(since string) string {
	return fmt.Sprintf("[EXPERIMENTAL since v%s — API may change; graduates next release cycle] ", since)
}

// applyExperimentalPolicy enforces the stability-tier rules on a tool about to
// be registered (Fase 3): experimental tools get the EXPERIMENTAL description
// prefix and MUST NOT declare an outputSchema — the schema is the interop
// contract, earned only when the feature passes the handler-level conformance
// sweep. A violation is a programmer error: panic so it fails in dev/CI,
// never in a shipped release.
func applyExperimentalPolicy(tool mcp.Tool) mcp.Tool {
	since, experimental := isExperimental(tool.Name)
	if !experimental {
		return tool
	}
	if tool.RawOutputSchema != nil || tool.OutputSchema.Type != "" {
		panic(fmt.Sprintf("experimental tool %q must not declare an outputSchema (graduate it first — see experimental.go)", tool.Name))
	}
	tool.Description = experimentalNotice(since) + tool.Description
	return tool
}
