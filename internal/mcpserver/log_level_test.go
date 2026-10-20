package mcpserver

import (
	"context"
	"log/slog"
	"testing"
)

func TestParseLogLevel(t *testing.T) {
	cases := []struct {
		in      string
		want    slog.Level
		wantErr bool
	}{
		{"", slog.LevelInfo, false},
		{"info", slog.LevelInfo, false},
		{"DEBUG", slog.LevelDebug, false},
		{"warn", slog.LevelWarn, false},
		{"warning", slog.LevelWarn, false},
		{" error ", slog.LevelError, false},
		{"verbose", 0, true},
	}
	for _, tc := range cases {
		got, err := parseLogLevel(tc.in)
		if (err != nil) != tc.wantErr {
			t.Errorf("parseLogLevel(%q) err=%v wantErr=%v", tc.in, err, tc.wantErr)
			continue
		}
		if !tc.wantErr && got != tc.want {
			t.Errorf("parseLogLevel(%q)=%v want %v", tc.in, got, tc.want)
		}
	}
}

func TestSetupLogging_LevelAndDebugAlias(t *testing.T) {
	defer slog.SetLogLoggerLevel(slog.LevelInfo)
	ctx := context.Background()

	cfg := &Configuration{LogLevel: "error"}
	if err := setupLogging(cfg); err != nil {
		t.Fatal(err)
	}
	if slog.Default().Enabled(ctx, slog.LevelInfo) {
		t.Error("--log-level error must suppress info")
	}
	if !slog.Default().Enabled(ctx, slog.LevelError) {
		t.Error("--log-level error must keep errors")
	}

	// --debug wins over a higher --log-level.
	cfg = &Configuration{LogLevel: "error", DebugMode: true}
	if err := setupLogging(cfg); err != nil {
		t.Fatal(err)
	}
	if !slog.Default().Enabled(ctx, slog.LevelDebug) {
		t.Error("--debug must enable debug level")
	}

	// --log-level debug turns on DebugMode (hooks debug, file:line).
	cfg = &Configuration{LogLevel: "debug"}
	if err := setupLogging(cfg); err != nil {
		t.Fatal(err)
	}
	if !cfg.DebugMode {
		t.Error("--log-level debug must set DebugMode")
	}

	if err := setupLogging(&Configuration{LogLevel: "loud"}); err == nil {
		t.Error("invalid level must be rejected")
	}
}
