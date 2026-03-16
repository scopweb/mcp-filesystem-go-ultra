package core

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// RuleType defines the type of normalization rule
type RuleType string

const (
	RuleParamAlias     RuleType = "param_alias"      // Rename top-level param: From → To
	RuleParamDefault   RuleType = "param_default"     // Set default if param missing
	RuleTypeCoerce     RuleType = "type_coerce"       // Coerce type (string "true" → bool true)
	RuleNestedAlias    RuleType = "nested_alias"      // Rename field inside JSON payload
	RuleNestedDefault  RuleType = "nested_default"    // Set default inside JSON payload array items
	RuleJSONAcceptBoth RuleType = "json_accept_both"  // Accept param as string OR raw JSON value
)

// NormalizationRule defines a single data-driven normalization rule
type NormalizationRule struct {
	ID        string   `json:"id"`                    // Unique rule identifier
	Tools     []string `json:"tools"`                 // Tool names ("*" = all tools)
	Type      RuleType `json:"type"`                  // Rule type
	From      string   `json:"from"`                  // Source param or field name
	To        string   `json:"to,omitempty"`          // Target param or field name (alias types)
	CoerceTo  string   `json:"coerce_to,omitempty"`   // Target type: "bool", "int", "float"
	InPayload string   `json:"in_payload,omitempty"`  // JSON payload param name (nested_* types)
	ArrayPath string   `json:"array_path,omitempty"`  // Array path: "[]", "steps[]"
	Value     string   `json:"value,omitempty"`       // Default value template: "step-{{index}}"
}

// NormalizationApplied records a single normalization that was applied
type NormalizationApplied struct {
	RuleID string `json:"rule_id"`
	Type   string `json:"type"`
	Param  string `json:"param"`
	From   string `json:"from,omitempty"`
	To     string `json:"to,omitempty"`
}

// NormalizeResult is returned by Normalize()
type NormalizeResult struct {
	Args        map[string]interface{}
	Applied     []NormalizationApplied
	WasModified bool
}

// NormalizerStats tracks aggregate normalization statistics
type NormalizerStats struct {
	mu              sync.RWMutex           `json:"-"`
	TotalProcessed  int64                  `json:"total_processed"`
	TotalNormalized int64                  `json:"total_normalized"`
	LastUpdated     time.Time              `json:"last_updated"`
	ByTool          map[string]*ToolNormStats `json:"by_tool"`
	ByRule          map[string]*RuleNormStats `json:"by_rule"`
	RecentNorms     []RecentNormalization  `json:"recent_normalizations"`
}

// ToolNormStats tracks per-tool normalization counts
type ToolNormStats struct {
	Processed  int64 `json:"processed"`
	Normalized int64 `json:"normalized"`
}

// RuleNormStats tracks per-rule hit counts
type RuleNormStats struct {
	RuleID string   `json:"rule_id"`
	Type   string   `json:"type"`
	Hits   int64    `json:"hits"`
	Tools  []string `json:"tools"`
}

// RecentNormalization records a recent normalization event
type RecentNormalization struct {
	Timestamp time.Time              `json:"ts"`
	Tool      string                 `json:"tool"`
	Applied   []NormalizationApplied `json:"applied"`
}

// Normalizer is the central request normalization engine
type Normalizer struct {
	rules     []NormalizationRule
	ruleIndex map[string][]int // tool name → indices into rules; "*" for wildcard
	stats     NormalizerStats
	logDir    string
}

// NewNormalizer creates a new normalizer with built-in rules.
// If rulesPath is non-empty, external rules are loaded and merged after built-in rules.
func NewNormalizer(rulesPath string, logDir string) (*Normalizer, error) {
	n := &Normalizer{
		ruleIndex: make(map[string][]int),
		logDir:    logDir,
	}
	n.stats.ByTool = make(map[string]*ToolNormStats)
	n.stats.ByRule = make(map[string]*RuleNormStats)

	// Load built-in rules
	n.rules = builtinRules()

	// Load external rules (optional)
	if rulesPath != "" {
		external, err := loadRulesFromFile(rulesPath)
		if err != nil {
			return nil, fmt.Errorf("load normalizer rules: %w", err)
		}
		n.rules = append(n.rules, external...)
		slog.Info("Loaded external normalizer rules", "path", rulesPath, "count", len(external))
	}

	// Build lookup index
	n.buildIndex()

	// Start stats persistence (if log directory configured)
	if logDir != "" {
		go n.statsPersistLoop()
	}

	return n, nil
}

// builtinRules returns the default normalization rules based on known Bug #22 patterns
func builtinRules() []NormalizationRule {
	return []NormalizationRule{
		// edit_file: old_str/new_str → old_text/new_text (Claude Desktop convention)
		{ID: "edit-old_str", Tools: []string{"edit_file"}, Type: RuleParamAlias, From: "old_str", To: "old_text"},
		{ID: "edit-new_str", Tools: []string{"edit_file"}, Type: RuleParamAlias, From: "new_str", To: "new_text"},

		// multi_edit: accept edits_json as raw JSON array (not just string)
		{ID: "multi_edit-edits-coerce", Tools: []string{"multi_edit"}, Type: RuleJSONAcceptBoth, From: "edits_json"},

		// pipeline: "type" → "action" in pipeline_json steps
		{ID: "pipeline-type-alias", Tools: []string{"batch_operations"}, Type: RuleNestedAlias,
			InPayload: "pipeline_json", ArrayPath: "steps[]", From: "type", To: "action"},

		// pipeline: auto-generate missing step IDs
		{ID: "pipeline-auto-id", Tools: []string{"batch_operations"}, Type: RuleNestedDefault,
			InPayload: "pipeline_json", ArrayPath: "steps[]", From: "id", Value: "step-{{index}}"},

		// Boolean coercions: string "true"/"false" → bool (all tools)
		{ID: "force-bool-coerce", Tools: []string{"*"}, Type: RuleTypeCoerce, From: "force", CoerceTo: "bool"},
		{ID: "dry_run-bool-coerce", Tools: []string{"*"}, Type: RuleTypeCoerce, From: "dry_run", CoerceTo: "bool"},

		// Boolean coercions: tool-specific
		{ID: "count_only-bool-coerce", Tools: []string{"search_files"}, Type: RuleTypeCoerce, From: "count_only", CoerceTo: "bool"},
		{ID: "permanent-bool-coerce", Tools: []string{"delete_file"}, Type: RuleTypeCoerce, From: "permanent", CoerceTo: "bool"},
		{ID: "whole_word-bool-coerce", Tools: []string{"edit_file", "search_files"}, Type: RuleTypeCoerce, From: "whole_word", CoerceTo: "bool"},
		{ID: "case_sensitive-bool-coerce", Tools: []string{"search_files", "edit_file"}, Type: RuleTypeCoerce, From: "case_sensitive", CoerceTo: "bool"},
		{ID: "include_content-bool-coerce", Tools: []string{"search_files"}, Type: RuleTypeCoerce, From: "include_content", CoerceTo: "bool"},
		{ID: "include_context-bool-coerce", Tools: []string{"search_files"}, Type: RuleTypeCoerce, From: "include_context", CoerceTo: "bool"},
		{ID: "recursive-bool-coerce", Tools: []string{"list_directory", "search_files"}, Type: RuleTypeCoerce, From: "recursive", CoerceTo: "bool"},
	}
}

// buildIndex creates a lookup map from tool name to rule indices
func (n *Normalizer) buildIndex() {
	n.ruleIndex = make(map[string][]int)
	for i, rule := range n.rules {
		for _, tool := range rule.Tools {
			n.ruleIndex[tool] = append(n.ruleIndex[tool], i)
		}
	}
}

// Normalize applies all matching rules to the tool's arguments.
// Returns the (possibly modified) args and a list of applied normalizations.
func (n *Normalizer) Normalize(tool string, args map[string]interface{}) NormalizeResult {
	if args == nil {
		args = make(map[string]interface{})
	}

	result := NormalizeResult{Args: args}

	// Get applicable rules: tool-specific + wildcard
	indices := make([]int, 0, 8)
	if toolRules, ok := n.ruleIndex[tool]; ok {
		indices = append(indices, toolRules...)
	}
	if wildcardRules, ok := n.ruleIndex["*"]; ok {
		indices = append(indices, wildcardRules...)
	}

	for _, idx := range indices {
		rule := n.rules[idx]
		if applied := n.applyRule(rule, args); applied != nil {
			result.Applied = append(result.Applied, *applied)
			result.WasModified = true
		}
	}

	// Record stats
	n.recordStats(tool, result.Applied)

	return result
}

// applyRule dispatches to the correct rule application function
func (n *Normalizer) applyRule(rule NormalizationRule, args map[string]interface{}) *NormalizationApplied {
	switch rule.Type {
	case RuleParamAlias:
		return applyParamAlias(rule, args)
	case RuleParamDefault:
		return applyParamDefault(rule, args)
	case RuleTypeCoerce:
		return applyTypeCoerce(rule, args)
	case RuleNestedAlias:
		return applyNestedAlias(rule, args)
	case RuleNestedDefault:
		return applyNestedDefault(rule, args)
	case RuleJSONAcceptBoth:
		return applyJSONAcceptBoth(rule, args)
	default:
		return nil
	}
}

// applyParamAlias renames a parameter if source exists and target doesn't
func applyParamAlias(rule NormalizationRule, args map[string]interface{}) *NormalizationApplied {
	fromVal, fromOK := args[rule.From]
	_, toOK := args[rule.To]
	if fromOK && !toOK {
		args[rule.To] = fromVal
		delete(args, rule.From)
		return &NormalizationApplied{
			RuleID: rule.ID, Type: string(rule.Type),
			Param: rule.From, From: rule.From, To: rule.To,
		}
	}
	return nil
}

// applyParamDefault sets a default value if the parameter is missing
func applyParamDefault(rule NormalizationRule, args map[string]interface{}) *NormalizationApplied {
	if _, exists := args[rule.From]; !exists {
		args[rule.From] = rule.Value
		return &NormalizationApplied{
			RuleID: rule.ID, Type: string(rule.Type),
			Param: rule.From, To: rule.Value,
		}
	}
	return nil
}

// applyTypeCoerce coerces string values to the target type
func applyTypeCoerce(rule NormalizationRule, args map[string]interface{}) *NormalizationApplied {
	val, exists := args[rule.From]
	if !exists {
		return nil
	}

	switch rule.CoerceTo {
	case "bool":
		s, ok := val.(string)
		if !ok {
			return nil
		}
		switch strings.ToLower(s) {
		case "true", "1", "yes":
			args[rule.From] = true
			return &NormalizationApplied{
				RuleID: rule.ID, Type: string(rule.Type),
				Param: rule.From, From: s, To: "true",
			}
		case "false", "0", "no":
			args[rule.From] = false
			return &NormalizationApplied{
				RuleID: rule.ID, Type: string(rule.Type),
				Param: rule.From, From: s, To: "false",
			}
		}
	case "int":
		if s, ok := val.(string); ok {
			if i, err := strconv.Atoi(s); err == nil {
				args[rule.From] = float64(i) // JSON numbers are float64
				return &NormalizationApplied{
					RuleID: rule.ID, Type: string(rule.Type),
					Param: rule.From, From: s, To: fmt.Sprintf("%d", i),
				}
			}
		}
	case "float":
		if s, ok := val.(string); ok {
			if f, err := strconv.ParseFloat(s, 64); err == nil {
				args[rule.From] = f
				return &NormalizationApplied{
					RuleID: rule.ID, Type: string(rule.Type),
					Param: rule.From, From: s, To: fmt.Sprintf("%g", f),
				}
			}
		}
	}
	return nil
}

// applyJSONAcceptBoth handles params that can be either a JSON string or raw JSON value
func applyJSONAcceptBoth(rule NormalizationRule, args map[string]interface{}) *NormalizationApplied {
	val, exists := args[rule.From]
	if !exists {
		return nil
	}
	// If already a string, no normalization needed
	if _, isStr := val.(string); isStr {
		return nil
	}
	// It's a raw JSON value (array or object) — marshal to string
	bytes, err := json.Marshal(val)
	if err != nil {
		return nil
	}
	args[rule.From] = string(bytes)
	return &NormalizationApplied{
		RuleID: rule.ID, Type: string(rule.Type),
		Param: rule.From, From: fmt.Sprintf("(%T)", val), To: "string",
	}
}

// applyNestedAlias renames fields inside array items of a JSON payload parameter
func applyNestedAlias(rule NormalizationRule, args map[string]interface{}) *NormalizationApplied {
	payload, modified := parseAndModifyPayload(rule, args, func(item map[string]interface{}, _ int) bool {
		fromVal, fromOK := item[rule.From]
		_, toOK := item[rule.To]
		if fromOK && !toOK {
			item[rule.To] = fromVal
			delete(item, rule.From)
			return true
		}
		return false
	})
	if !modified {
		return nil
	}
	return reserializePayload(rule, args, payload)
}

// applyNestedDefault sets default values for missing fields in array items of a JSON payload
func applyNestedDefault(rule NormalizationRule, args map[string]interface{}) *NormalizationApplied {
	payload, modified := parseAndModifyPayload(rule, args, func(item map[string]interface{}, index int) bool {
		existing, exists := item[rule.From]
		if exists {
			// If it's a non-empty string, don't overwrite
			if s, ok := existing.(string); ok && s != "" {
				return false
			}
			// If it's not a string but has a value, don't overwrite
			if _, ok := existing.(string); !ok && existing != nil {
				return false
			}
		}
		value := strings.ReplaceAll(rule.Value, "{{index}}", fmt.Sprintf("%d", index))
		item[rule.From] = value
		return true
	})
	if !modified {
		return nil
	}
	return reserializePayload(rule, args, payload)
}

// parseAndModifyPayload parses a JSON payload param, navigates to the array, and applies a modifier
func parseAndModifyPayload(rule NormalizationRule, args map[string]interface{}, modifier func(item map[string]interface{}, index int) bool) (interface{}, bool) {
	jsonStr, ok := args[rule.InPayload].(string)
	if !ok || jsonStr == "" {
		return nil, false
	}

	var payload interface{}
	if err := json.Unmarshal([]byte(jsonStr), &payload); err != nil {
		return nil, false
	}

	items := navigateToArray(payload, rule.ArrayPath)
	if items == nil {
		return nil, false
	}

	modified := false
	for i, item := range items {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		if modifier(m, i) {
			modified = true
		}
	}

	return payload, modified
}

// reserializePayload re-marshals the modified payload back into the args map
func reserializePayload(rule NormalizationRule, args map[string]interface{}, payload interface{}) *NormalizationApplied {
	newBytes, err := json.Marshal(payload)
	if err != nil {
		return nil
	}
	args[rule.InPayload] = string(newBytes)
	return &NormalizationApplied{
		RuleID: rule.ID, Type: string(rule.Type),
		Param: rule.InPayload + "." + rule.From, From: rule.From, To: rule.To,
	}
}

// navigateToArray navigates a parsed JSON structure to find the target array.
// Supports: "[]" (root is array), "steps[]" (field.array), "data.steps[]" (nested.field.array)
func navigateToArray(payload interface{}, arrayPath string) []interface{} {
	if arrayPath == "[]" {
		if arr, ok := payload.([]interface{}); ok {
			return arr
		}
		return nil
	}

	parts := strings.Split(strings.TrimSuffix(arrayPath, "[]"), ".")
	current := payload
	for _, part := range parts {
		if part == "" {
			continue
		}
		m, ok := current.(map[string]interface{})
		if !ok {
			return nil
		}
		current = m[part]
	}

	if arr, ok := current.([]interface{}); ok {
		return arr
	}
	return nil
}

// recordStats updates aggregate statistics (thread-safe)
func (n *Normalizer) recordStats(tool string, applied []NormalizationApplied) {
	n.stats.mu.Lock()
	defer n.stats.mu.Unlock()

	n.stats.TotalProcessed++
	n.stats.LastUpdated = time.Now()

	if _, ok := n.stats.ByTool[tool]; !ok {
		n.stats.ByTool[tool] = &ToolNormStats{}
	}
	n.stats.ByTool[tool].Processed++

	if len(applied) > 0 {
		n.stats.TotalNormalized++
		n.stats.ByTool[tool].Normalized++

		for _, a := range applied {
			rs, ok := n.stats.ByRule[a.RuleID]
			if !ok {
				rs = &RuleNormStats{RuleID: a.RuleID, Type: a.Type}
				n.stats.ByRule[a.RuleID] = rs
			}
			rs.Hits++
			// Track which tools triggered this rule
			found := false
			for _, t := range rs.Tools {
				if t == tool {
					found = true
					break
				}
			}
			if !found {
				rs.Tools = append(rs.Tools, tool)
			}
		}

		// Ring buffer of recent normalizations (keep last 50)
		n.stats.RecentNorms = append(n.stats.RecentNorms, RecentNormalization{
			Timestamp: time.Now(),
			Tool:      tool,
			Applied:   applied,
		})
		if len(n.stats.RecentNorms) > 50 {
			n.stats.RecentNorms = n.stats.RecentNorms[len(n.stats.RecentNorms)-50:]
		}
	}
}

// GetStats returns a thread-safe snapshot of current stats
func (n *Normalizer) GetStats() NormalizerStats {
	n.stats.mu.RLock()
	defer n.stats.mu.RUnlock()

	snapshot := NormalizerStats{
		TotalProcessed:  n.stats.TotalProcessed,
		TotalNormalized: n.stats.TotalNormalized,
		LastUpdated:     n.stats.LastUpdated,
		ByTool:          make(map[string]*ToolNormStats, len(n.stats.ByTool)),
		ByRule:          make(map[string]*RuleNormStats, len(n.stats.ByRule)),
		RecentNorms:     make([]RecentNormalization, len(n.stats.RecentNorms)),
	}
	for k, v := range n.stats.ByTool {
		c := *v
		snapshot.ByTool[k] = &c
	}
	for k, v := range n.stats.ByRule {
		c := *v
		toolsCopy := make([]string, len(v.Tools))
		copy(toolsCopy, v.Tools)
		c.Tools = toolsCopy
		snapshot.ByRule[k] = &c
	}
	copy(snapshot.RecentNorms, n.stats.RecentNorms)
	return snapshot
}

// RulesCount returns the total number of loaded rules
func (n *Normalizer) RulesCount() int {
	return len(n.rules)
}

// statsPersistLoop writes normalizer_stats.json every 30 seconds
func (n *Normalizer) statsPersistLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		snapshot := n.GetStats()
		data, err := json.MarshalIndent(snapshot, "", "  ")
		if err != nil {
			continue
		}
		path := filepath.Join(n.logDir, "normalizer_stats.json")
		tmpPath := path + ".tmp"
		if err := os.WriteFile(tmpPath, data, 0600); err != nil {
			continue
		}
		os.Rename(tmpPath, path)
	}
}

// loadRulesFromFile loads normalization rules from a JSON file
func loadRulesFromFile(path string) ([]NormalizationRule, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var rules []NormalizationRule
	if err := json.Unmarshal(data, &rules); err != nil {
		return nil, fmt.Errorf("parse rules file: %w", err)
	}
	for i, r := range rules {
		if r.ID == "" {
			return nil, fmt.Errorf("rule %d: id is required", i)
		}
		if len(r.Tools) == 0 {
			return nil, fmt.Errorf("rule %d (%s): tools is required", i, r.ID)
		}
		if r.Type == "" {
			return nil, fmt.Errorf("rule %d (%s): type is required", i, r.ID)
		}
	}
	return rules, nil
}
