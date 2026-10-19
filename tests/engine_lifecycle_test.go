package tests

import (
	"testing"

	"github.com/mcp/filesystem-ultra/core"
)

// newManagedEngine releases cached os.Root handles before TempDir cleanup.
// Register at construction so fatal assertions and subtests also release them.
func newManagedEngine(t testing.TB, config *core.Config) (*core.UltraFastEngine, error) {
	t.Helper()
	engine, err := core.NewUltraFastEngine(config)
	if err != nil {
		return nil, err
	}
	t.Cleanup(func() {
		if err := engine.Close(); err != nil {
			t.Errorf("close test engine: %v", err)
		}
	})
	return engine, nil
}
