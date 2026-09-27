package core

import (
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
)

func TestFilePolicy_InvalidConfig(t *testing.T) {
	cases := []string{
		`{`,
		`{"version":1,"rules":[]}{"extra":true}`,
		`{"version":2,"rules":[]}`,
		`{"version":1}`,
		`{"version":1,"rules":[],"extra":1}`,
		`{"version":1,"rules":[{"pattern":"","level":"hidden"}]}`,
		`{"version":1,"rules":[{"pattern":".env","level":"nope"}]}`,
		`{"version":1,"rules":[{"pattern":"/etc/passwd","level":"hidden"}]}`,
		`{"version":1,"rules":[{"pattern":"../x","level":"hidden"}]}`,
		`{"version":1,"rules":[{"pattern":"$HOME/.env","level":"hidden"}]}`,
		`{"version":1,"rules":[{"pattern":"~/secrets","level":"hidden"}]}`,
		`{"version":1,"rules":[{"pattern":"foo[bar]","level":"hidden"}]}`,
	}
	for _, raw := range cases {
		if _, err := ParseFilePolicy([]byte(raw)); err == nil {
			t.Errorf("accepted invalid config: %s", raw)
		}
	}
}

func TestFilePolicy_PrecedenceAndInheritance(t *testing.T) {
	raw := []byte(`{
	  "version": 1,
	  "rules": [
	    {"pattern": ".env", "level": "hidden"},
	    {"pattern": ".env.*", "level": "hidden"},
	    {"pattern": "*.ini", "level": "protected"},
	    {"pattern": "settings.json", "level": "read_only"},
	    {"pattern": "**/secrets/**", "level": "hidden"}
	  ]
	}`)
	p, err := ParseFilePolicy(raw)
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(string(filepath.Separator), "proj")
	if runtime.GOOS == "windows" {
		root = `C:\proj`
	}
	level := func(rel string) ProtectionLevel {
		full := filepath.Join(root, filepath.FromSlash(rel))
		return p.Level(full, full, []string{root})
	}
	if got := level("app/.env"); got != LevelHidden {
		t.Fatalf(".env level %s", got)
	}
	if got := level("app/.env.local"); got != LevelHidden {
		t.Fatalf(".env.local level %s", got)
	}
	if got := level("cfg/app.ini"); got != LevelProtected {
		t.Fatalf("ini level %s", got)
	}
	if got := level("settings.json"); got != LevelReadOnly {
		t.Fatalf("settings level %s", got)
	}
	if got := level("secrets"); got != LevelHidden {
		t.Fatalf("secrets dir at root level %s", got)
	}
	if got := level("secrets/key.txt"); got != LevelHidden {
		t.Fatalf("secrets child level %s", got)
	}
	if got := level("app/secrets/key.txt"); got != LevelHidden {
		t.Fatalf("nested secrets level %s", got)
	}
	if got := level("main.go"); got != LevelNormal {
		t.Fatalf("main.go level %s", got)
	}
	// A less restrictive child rule cannot weaken a hidden parent.
	child := filepath.Join(root, "secrets", "settings.json")
	if got := p.Level(child, child, []string{root}); got != LevelHidden {
		t.Fatalf("child must not weaken parent, got %s", got)
	}
}

func TestFilePolicy_OverlappingRoots(t *testing.T) {
	p, err := ParseFilePolicy([]byte(`{"version":1,"rules":[{"pattern":"secrets/**","level":"protected"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	wide := filepath.Join(string(filepath.Separator), "proj")
	narrow := filepath.Join(wide, "app")
	if runtime.GOOS == "windows" {
		wide = `C:\proj`
		narrow = `C:\proj\app`
	}
	target := filepath.Join(narrow, "secrets", "k.txt")
	if got := p.Level(target, target, []string{wide}); got != LevelNormal {
		t.Fatalf("pattern secrets/** must not match via the wide root, got %s", got)
	}
	if got := p.Level(target, target, []string{wide, narrow}); got != LevelProtected {
		t.Fatalf("overlapping narrower root must still apply, got %s", got)
	}
}

func TestFilePolicy_RejectInsecureOpen(t *testing.T) {
	rel, err := ParseFilePolicy([]byte(`{"version":1,"rules":[{"pattern":"**/secrets/**","level":"hidden"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := rel.RejectInsecureOpen(); err == nil {
		t.Fatal("relative rules must reject insecure-open")
	}
	base, err := ParseFilePolicy([]byte(`{"version":1,"rules":[{"pattern":".env","level":"hidden"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := base.RejectInsecureOpen(); err != nil {
		t.Fatal(err)
	}
}

func TestFilePolicy_PinConfig(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "policy.json")
	body := []byte(`{"version":1,"rules":[]}`)
	if err := os.WriteFile(cfg, body, 0600); err != nil {
		t.Fatal(err)
	}
	p, err := LoadFilePolicy(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.PinConfig(cfg); err != nil {
		t.Fatal(err)
	}
	if got := p.Level(cfg, cfg, []string{dir}); got < LevelProtected {
		t.Fatalf("config file level %s", got)
	}
	sibling := filepath.Join(dir, "other.txt")
	if got := p.Level(sibling, sibling, []string{dir}); got != LevelNormal {
		t.Fatalf("sibling must stay normal, got %s", got)
	}
}

func TestFilePolicy_ConcurrentLevel(t *testing.T) {
	p, err := ParseFilePolicy([]byte(`{"version":1,"rules":[{"pattern":"*.ini","level":"protected"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for n := 0; n < 50; n++ {
				path := filepath.Join(root, "a.ini")
				if p.Level(path, path, []string{root}) != LevelProtected {
					t.Error("race mismatch")
					return
				}
			}
		}()
	}
	wg.Wait()
}

func TestFilePolicy_SymlinkTarget(t *testing.T) {
	dir := t.TempDir()
	secret := filepath.Join(dir, "secrets", "key.txt")
	if err := os.MkdirAll(filepath.Dir(secret), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(secret, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "alias.txt")
	if err := os.Symlink(secret, link); err != nil {
		t.Skipf("symlink not permitted in this environment: %v", err)
	}
	p, err := ParseFilePolicy([]byte(`{"version":1,"rules":[{"pattern":"**/secrets/**","level":"hidden"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	logical, canonical := ResolvePolicyPaths(link)
	if got := p.Level(logical, canonical, []string{dir}); got != LevelHidden {
		t.Fatalf("symlink to secrets must be hidden, logical=%s canonical=%s level=%s", logical, canonical, got)
	}
}

func TestDecideMatrix(t *testing.T) {
	if !Decide(LevelReadOnly, OpRead).Allow || Decide(LevelReadOnly, OpWrite).Allow {
		t.Fatal("read_only matrix")
	}
	meta := Decide(LevelProtected, OpMetadata)
	if !meta.Allow || !meta.Redact || Decide(LevelProtected, OpRead).Allow {
		t.Fatal("protected matrix")
	}
	hid := Decide(LevelHidden, OpRead)
	if hid.Allow || !hid.Hidden {
		t.Fatal("hidden matrix")
	}
}
