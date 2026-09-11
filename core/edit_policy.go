package core

import (
	"context"
	"fmt"
	"strings"
)

type ctxEditPolicyKey struct{}

// EditPolicy is the opt-in E4 strict-matching contract. Default (zero value)
// preserves historical fallback behaviour.
type EditPolicy struct {
	Strict          bool
	ExpectedMatches *int
}

// WithEditPolicy attaches matching policy to ctx for EditFile/MultiEdit.
func WithEditPolicy(ctx context.Context, p EditPolicy) context.Context {
	if !p.Strict && p.ExpectedMatches == nil {
		return ctx
	}
	return context.WithValue(ctx, ctxEditPolicyKey{}, p)
}

func editPolicyFrom(ctx context.Context) EditPolicy {
	p, _ := ctx.Value(ctxEditPolicyKey{}).(EditPolicy)
	return p
}

// MatchLineNumbers returns 1-based line numbers of non-overlapping needle matches.
func MatchLineNumbers(content, needle string) []int {
	if needle == "" {
		return nil
	}
	var lines []int
	start := 0
	for {
		i := strings.Index(content[start:], needle)
		if i < 0 {
			break
		}
		abs := start + i
		lines = append(lines, strings.Count(content[:abs], "\n")+1)
		start = abs + len(needle)
	}
	return lines
}

func formatMatchCountError(content, needle string, got, want int) error {
	locs := MatchLineNumbers(content, needle)
	if len(locs) > 12 {
		locs = locs[:12]
	}
	where := "none"
	if len(locs) > 0 {
		parts := make([]string, len(locs))
		for i, n := range locs {
			parts[i] = fmt.Sprintf("%d", n)
		}
		where = strings.Join(parts, ", ")
	}
	return fmt.Errorf("old_text matched %d time(s) (expected %d). Candidates at lines: %s. Quote more context, split the edit, or set expected_matches", got, want, where)
}

func (p EditPolicy) checkMatchCount(content, needle string, got int) error {
	want := 0
	if p.ExpectedMatches != nil {
		want = *p.ExpectedMatches
		if want < 1 {
			return fmt.Errorf(`parameter "expected_matches": must be an integer >= 1`)
		}
	} else if p.Strict {
		want = 1
	} else {
		return nil
	}
	if got != want {
		return formatMatchCountError(content, needle, got, want)
	}
	return nil
}
