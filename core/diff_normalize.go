package core

import (
	"fmt"
	"regexp"
	"strings"
)

const (
	maxNormalizeRules   = 8
	maxNormalizePattern = 256
	maxNormalizeReplace = 256
	maxNormalizeGrowth  = 1 << 20
)

// NormalizeRule rewrites both sides of a comparison in memory.
// Pattern is a Go RE2 expression. Replacement may use $1 capture groups.
type NormalizeRule struct {
	Pattern     string `json:"pattern"`
	Replacement string `json:"replacement"`
}

// DiffCompare is the result of a literal or normalized text comparison.
// Identical is true only when the original bytes match. Equivalent is true
// only when rules were applied and the normalized texts match. A normalized
// diff is not a patch for the original files.
type DiffCompare struct {
	Identical      bool
	Equivalent     bool
	Basis          string
	Added          int
	Removed        int
	RulesApplied   int
	MatchesA       int
	MatchesB       int
	Diff           string
	NormalizedDiff bool
}

// ApplyNormalizeRules applies rules in order and returns the match count.
// It does not touch the filesystem. Go RE2 keeps each replacement linear.
func ApplyNormalizeRules(content string, rules []NormalizeRule) (string, int, error) {
	if err := validateNormalizeRules(rules); err != nil {
		return "", 0, err
	}
	total := 0
	cur := content
	for _, rule := range rules {
		re, err := regexp.Compile(rule.Pattern)
		if err != nil {
			return "", 0, fmt.Errorf("normalize pattern %q: %w", rule.Pattern, err)
		}
		n := len(re.FindAllStringIndex(cur, -1))
		total += n
		next := re.ReplaceAllString(cur, rule.Replacement)
		if grewTooFar(len(content), len(next)) {
			return "", 0, fmt.Errorf("normalize expanded input from %d to %d bytes", len(content), len(next))
		}
		cur = next
	}
	return cur, total, nil
}

func validateNormalizeRules(rules []NormalizeRule) error {
	if len(rules) > maxNormalizeRules {
		return fmt.Errorf("normalize accepts at most %d rules", maxNormalizeRules)
	}
	for i, rule := range rules {
		if strings.TrimSpace(rule.Pattern) == "" {
			return fmt.Errorf("normalize rule %d: pattern is empty", i+1)
		}
		if len(rule.Pattern) > maxNormalizePattern {
			return fmt.Errorf("normalize rule %d: pattern exceeds %d bytes", i+1, maxNormalizePattern)
		}
		if len(rule.Replacement) > maxNormalizeReplace {
			return fmt.Errorf("normalize rule %d: replacement exceeds %d bytes", i+1, maxNormalizeReplace)
		}
	}
	return nil
}

func grewTooFar(original, next int) bool {
	if next <= original+maxNormalizeGrowth {
		return false
	}
	return next > original*4
}

// CompareTexts compares a and b. With no rules the result matches a literal
// unified diff. With rules, both sides are rewritten the same way first.
func CompareTexts(a, b string, rules []NormalizeRule) (DiffCompare, error) {
	out := DiffCompare{Basis: "literal"}
	if len(rules) == 0 {
		if a == b {
			out.Identical = true
			return out, nil
		}
		out.Added, out.Removed, _ = DiffCounts(a, b)
		out.Diff = UnifiedDiff(a, b, "b")
		return out, nil
	}
	if err := validateNormalizeRules(rules); err != nil {
		return DiffCompare{}, err
	}
	out.RulesApplied = len(rules)
	out.Basis = "normalized"
	if a == b {
		out.Identical = true
		return out, nil
	}
	na, ma, err := ApplyNormalizeRules(a, rules)
	if err != nil {
		return DiffCompare{}, err
	}
	nb, mb, err := ApplyNormalizeRules(b, rules)
	if err != nil {
		return DiffCompare{}, err
	}
	out.MatchesA = ma
	out.MatchesB = mb
	if na == nb {
		out.Equivalent = true
		return out, nil
	}
	out.Added, out.Removed, _ = DiffCounts(na, nb)
	out.Diff = UnifiedDiff(na, nb, "b")
	out.NormalizedDiff = true
	return out, nil
}
