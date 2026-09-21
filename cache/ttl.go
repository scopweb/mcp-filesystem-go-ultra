package cache

import (
	"fmt"
	"strings"
	"time"
)

func (c *IntelligentCache) FileTTL() time.Duration {
	return c.fileTTL
}

func ParseFileTTL(s string) (time.Duration, error) {
	d, err := time.ParseDuration(strings.TrimSpace(s))
	if err != nil {
		return 0, fmt.Errorf("invalid cache TTL %q: %w", s, err)
	}
	if d < time.Second {
		return 0, fmt.Errorf("cache TTL must be at least 1s")
	}
	return d, nil
}
