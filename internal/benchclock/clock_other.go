//go:build !windows

package benchclock

import "time"

var origin = time.Now()

func Now() int64                      { return time.Since(origin).Nanoseconds() }
func Since(start int64) time.Duration { return time.Duration(Now() - start) }
