package core

import (
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func testAutoSync(t *testing.T) *AutoSyncManager {
	t.Helper()
	return &AutoSyncManager{
		config:     DefaultAutoSyncConfig(),
		configPath: filepath.Join(t.TempDir(), "autosync.json"),
	}
}

func TestUpdateConfigWithMappingsDoesNotDeadlock(t *testing.T) {
	dir := t.TempDir()
	m := testAutoSync(t)
	m.SetAllowedPaths([]string{dir})

	done := make(chan error, 1)
	go func() {
		done <- m.UpdateConfig(&AutoSyncConfig{
			Enabled:       true,
			SyncOnWrite:   true,
			Silent:        true,
			TargetMapping: map[string]string{"/mnt/c/src": filepath.Join(dir, "child.txt")},
			ConfigVersion: "1.0",
		})
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("UpdateConfig deadlocked")
	}
}

func TestUpdateConfigRejectsMappingOutsideAllowedRoot(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "repo")
	m := testAutoSync(t)
	m.SetAllowedPaths([]string{root})

	err := m.UpdateConfig(&AutoSyncConfig{
		TargetMapping: map[string]string{"/mnt/c/src": filepath.Join(dir, "repo-evil", "x.txt")},
	})
	if err == nil {
		t.Fatal("sibling prefix was accepted")
	}
}

func TestTargetWithinAllowedRejectsSiblingPrefix(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "repo")
	if targetWithinAllowed(filepath.Join(dir, "repo-evil", "x.txt"), []string{root}) {
		t.Fatal("sibling path allowed")
	}
	if !targetWithinAllowed(filepath.Join(root, "file.txt"), []string{root}) {
		t.Fatal("child path denied")
	}
	if !targetWithinAllowed(root, []string{root}) {
		t.Fatal("root itself denied")
	}
}

func TestGetConfigCopyDoesNotAliasMaps(t *testing.T) {
	m := testAutoSync(t)
	m.config.TargetMapping["/mnt/c/a"] = `C:\allowed`
	got := m.GetConfig()
	got.TargetMapping["/mnt/c/a"] = `C:\evil`
	if m.config.TargetMapping["/mnt/c/a"] != `C:\allowed` {
		t.Fatal("GetConfig returned an aliased map")
	}
}

func TestAutoSyncConfigConcurrentAccess(t *testing.T) {
	dir := t.TempDir()
	m := testAutoSync(t)
	m.SetAllowedPaths([]string{dir})
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			_ = m.UpdateConfig(&AutoSyncConfig{
				Enabled:       true,
				SyncOnWrite:   true,
				SyncOnEdit:    true,
				SyncOnDelete:  true,
				Silent:        true,
				TargetMapping: map[string]string{"/mnt/c/a": dir},
				ConfigVersion: "1.0",
			})
		}()
		go func() {
			defer wg.Done()
			_ = m.AfterWrite("/mnt/c/a/file.txt")
			_ = m.AfterEdit("/mnt/c/a/file.txt")
			_ = m.AfterDelete("/mnt/c/a/file.txt")
			_ = m.GetConfig()
			_ = m.GetStatus()
			_ = m.IsEnabled()
		}()
	}
	wg.Wait()
}
