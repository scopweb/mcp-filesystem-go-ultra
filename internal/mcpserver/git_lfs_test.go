package mcpserver

import "testing"

// The `git lfs version` output of the real binaries.
func TestParseGitLFSVersion(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
		ok   bool
	}{
		{"windows", "git-lfs/3.8.1 (GitHub; windows amd64; go 1.27.3; git a662a6db)\n", "3.8.1", true},
		{"linux", "git-lfs/3.5.1 (GitHub; linux amd64; go 1.21.5; git 8e35099d2)\n", "3.5.1", true},
		{"bare", "git-lfs/3.8.1\n", "3.8.1", true},
		{"no newline", "git-lfs/3.4.0 (GitHub; darwin arm64; go 1.20.7; git 857e4f4e1)", "3.4.0", true},
		{"empty", "", "", false},
		{"other tool", "git version 2.47.1.windows.1\n", "", false},
		{"no version", "git-lfs/\n", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parseGitLFSVersion(tc.in)
			if got != tc.want || ok != tc.ok {
				t.Fatalf("parseGitLFSVersion(%q) = %q,%v; want %q,%v",
					tc.in, got, ok, tc.want, tc.ok)
			}
		})
	}
}

func TestVersionOlder(t *testing.T) {
	cases := []struct {
		v, min string
		want   bool
	}{
		{"3.8.1", "3.8.1", false},
		{"3.8.0", "3.8.1", true},
		{"3.7.9", "3.8.1", true},
		{"2.9.9", "3.8.1", true},
		{"3.9.0", "3.8.1", false},
		{"4.0.0", "3.8.1", false},
		{"3.8", "3.8.1", true},
		{"3.8.2", "3.8.1", false},
		{"3.8.1-rc1", "3.8.1", false},
		{"3.8.0-rc1", "3.8.1", true},
	}
	for _, tc := range cases {
		t.Run(tc.v+"|"+tc.min, func(t *testing.T) {
			if got := versionOlder(tc.v, tc.min); got != tc.want {
				t.Fatalf("versionOlder(%q, %q) = %v; want %v", tc.v, tc.min, got, tc.want)
			}
		})
	}
}
