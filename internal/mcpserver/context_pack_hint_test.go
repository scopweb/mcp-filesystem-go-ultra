package mcpserver

import (
	"strings"
	"testing"
)

// The hint names a tool that the client must be able to call. In strict and
// in agent there is no context_pack, so the hint must not be there.
func TestContextPackHintOnlyWhereTheToolExists(t *testing.T) {
	if h := contextPackHint(profileUltra); !strings.Contains(h, "context_pack") {
		t.Fatalf("ultra must point at context_pack: %q", h)
	}
	if h := contextPackHint(profileAgent); h != "" {
		t.Fatalf("agent has no context_pack, the hint must be empty: %q", h)
	}
	if h := contextPackHint(profileStrict); h != "" {
		t.Fatalf("strict has no context_pack, the hint must be empty: %q", h)
	}
}

// The description of read_file must name context_pack only in the profile
// that registers it.
func TestReadFileDescriptionNamesContextPackOnlyInUltra(t *testing.T) {
	for _, tc := range []struct {
		profile toolProfile
		want    bool
	}{
		{profileUltra, true},
		{profileAgent, false},
		{profileStrict, false},
	} {
		t.Run(string(tc.profile), func(t *testing.T) {
			dir := t.TempDir()
			reg := newProfileRegistry(t, dir, registerOpts{Profile: tc.profile})
			tool, ok := reg.server.ListTools()["read_file"]
			if !ok {
				t.Fatal("read_file not registered")
			}
			has := strings.Contains(tool.Tool.Description, "context_pack")
			if has != tc.want {
				t.Fatalf("read_file description mentions context_pack: %v, want %v", has, tc.want)
			}
		})
	}
}
