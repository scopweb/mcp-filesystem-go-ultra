package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// makeTrashEntry creates a synthetic soft-deleted entry under
// <backupDir>/filesdelete/<sdID>/ with a metadata.json + a small file body.
// Returns the full SDID and the original_path used. Invalidates the
// package-level trash cache so the test sees the new entry on next handler call.
func makeTrashEntry(t *testing.T, backupDir, sdID, originalPath, content string) {
	t.Helper()
	invalidateTrashCache()
	sdDir := filepath.Join(backupDir, "filesdelete", sdID)
	if err := os.MkdirAll(sdDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sdDir, filepath.Base(originalPath)), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	destPath := filepath.Join(sdDir, filepath.Base(originalPath))
	meta := TrashEntry{
		SDID:         sdID,
		OriginalPath: originalPath,
		DestPath:     destPath,
		Size:         int64(len(content)),
		Hash:         "deadbeef",
		Timestamp:    time.Now(),
		Kind:         "soft_delete",
	}
	data, _ := json.MarshalIndent(meta, "", "  ")
	if err := os.WriteFile(filepath.Join(sdDir, "metadata.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
}

// TestTrashListEmpty verifies that the list endpoint returns an empty slice
// (not nil, not 404) when the trash dir doesn't exist.
func TestTrashListEmpty(t *testing.T) {
	backupDir := t.TempDir()

	h := trashListHandler(backupDir)
	req := httptest.NewRequest("GET", "/api/trash", nil)
	w := httptest.NewRecorder()
	h(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var got []TrashEntry
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %d entries, want 0", len(got))
	}
}

// TestTrashListWithoutBackupDir verifies graceful degradation when the
// dashboard was started without --backup-dir.
func TestTrashListWithoutBackupDir(t *testing.T) {
	h := trashListHandler("")
	req := httptest.NewRequest("GET", "/api/trash", nil)
	w := httptest.NewRecorder()
	h(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (graceful degradation)", w.Code)
	}
	var got map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if !strings.Contains(strings.ToLower(got["error"]), "backup-dir") {
		t.Errorf("error = %q, want it to mention --backup-dir", got["error"])
	}
}

// TestTrashListReturnsEntries verifies the happy path: create 2 entries, see 2.
func TestTrashListReturnsEntries(t *testing.T) {
	backupDir := t.TempDir()
	makeTrashEntry(t, backupDir, "sd-20260611-150455-aaaaaaaaaaaa", "/tmp/a.txt", "hello")
	makeTrashEntry(t, backupDir, "sd-20260611-150456-bbbbbbbbbbbb", "/tmp/b.txt", "world!!")

	h := trashListHandler(backupDir)
	req := httptest.NewRequest("GET", "/api/trash", nil)
	w := httptest.NewRecorder()
	h(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	var got []TrashEntry
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d entries, want 2", len(got))
	}
	for _, e := range got {
		if e.SDID == "" {
			t.Error("entry has empty SDID")
		}
		if e.FileName == "" {
			t.Error("entry has empty file_name")
		}
		if e.ViewURL == "" {
			t.Error("entry has empty view_url")
		}
		if !e.CanRestore {
			t.Errorf("entry %s CanRestore=false; original paths don't exist", e.SDID)
		}
	}
}

// TestTrashSearchFilter verifies the q parameter filters by substring.
func TestTrashSearchFilter(t *testing.T) {
	backupDir := t.TempDir()
	makeTrashEntry(t, backupDir, "sd-20260611-150455-aaaaaaaaaaaa", "/tmp/alpha.txt", "alpha")
	makeTrashEntry(t, backupDir, "sd-20260611-150456-bbbbbbbbbbbb", "/tmp/beta.txt", "beta")

	h := trashSearchHandler(backupDir)
	req := httptest.NewRequest("GET", "/api/trash/search?q=alpha", nil)
	w := httptest.NewRecorder()
	h(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	var resp TrashSearchResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Total != 1 {
		t.Errorf("total = %d, want 1", resp.Total)
	}
	if len(resp.Results) != 1 {
		t.Errorf("results = %d, want 1", len(resp.Results))
	}
	if !strings.Contains(resp.Results[0].OriginalPath, "alpha") {
		t.Errorf("result original_path = %q, want 'alpha'", resp.Results[0].OriginalPath)
	}
}

// TestTrashSearchPagination verifies limit + offset.
func TestTrashSearchPagination(t *testing.T) {
	backupDir := t.TempDir()
	for i := 0; i < 5; i++ {
		makeTrashEntry(t, backupDir, fmt.Sprintf("sd-20260611-15045%d-aaaaaaaaaaaa", i),
			fmt.Sprintf("/tmp/file%d.txt", i), fmt.Sprintf("content-%d", i))
	}

	h := trashSearchHandler(backupDir)
	// limit=2 offset=0
	req := httptest.NewRequest("GET", "/api/trash/search?limit=2&offset=0", nil)
	w := httptest.NewRecorder()
	h(w, req)
	var page1 TrashSearchResponse
	json.NewDecoder(w.Body).Decode(&page1)
	if page1.Total != 5 {
		t.Errorf("page1 total = %d, want 5", page1.Total)
	}
	if len(page1.Results) != 2 {
		t.Errorf("page1 results = %d, want 2", len(page1.Results))
	}

	// limit=2 offset=2
	req = httptest.NewRequest("GET", "/api/trash/search?limit=2&offset=2", nil)
	w = httptest.NewRecorder()
	h(w, req)
	var page2 TrashSearchResponse
	json.NewDecoder(w.Body).Decode(&page2)
	if page2.Total != 5 {
		t.Errorf("page2 total = %d, want 5", page2.Total)
	}
	if len(page2.Results) != 2 {
		t.Errorf("page2 results = %d, want 2", len(page2.Results))
	}

	// limit=2 offset=4 (last page, only 1 result)
	req = httptest.NewRequest("GET", "/api/trash/search?limit=2&offset=4", nil)
	w = httptest.NewRecorder()
	h(w, req)
	var page3 TrashSearchResponse
	json.NewDecoder(w.Body).Decode(&page3)
	if len(page3.Results) != 1 {
		t.Errorf("page3 results = %d, want 1", len(page3.Results))
	}
}

// TestTrashSearchRejectsPathTraversal verifies the SD-ID regex.
func TestTrashSearchRejectsPathTraversal(t *testing.T) {
	backupDir := t.TempDir()

	for _, badID := range []string{
		"../../etc/passwd",
		"..\\..\\windows",
		"foo/bar",
		"foo\\bar",
		"sd-../escape",
		"",
	} {
		req := httptest.NewRequest("GET", "/api/trash/detail/"+badID, nil)
		h := trashDetailHandler(backupDir)
		w := httptest.NewRecorder()
		h(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("trashDetail(%q) status = %d, want 400", badID, w.Code)
		}
	}
}

// TestTrashRestoreSuccess — happy path: file goes back to original_path.
func TestTrashRestoreSuccess(t *testing.T) {
	backupDir := t.TempDir()
	originalDir := t.TempDir()
	originalPath := filepath.Join(originalDir, "important.txt")
	sdID := "sd-20260611-150455-aaaaaaaaaaaa"
	makeTrashEntry(t, backupDir, sdID, originalPath, "critical content")

	h := trashRestoreHandler(backupDir)
	body, _ := json.Marshal(trashRestoreRequest{SDID: sdID})
	req := httptest.NewRequest("POST", "/api/trash/restore", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var resp trashRestoreResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if !resp.OK {
		t.Error("resp.OK is false")
	}
	if resp.RestoredTo != originalPath {
		t.Errorf("restored_to = %q, want %q", resp.RestoredTo, originalPath)
	}
	got, err := os.ReadFile(originalPath)
	if err != nil {
		t.Fatalf("restored file missing: %v", err)
	}
	if string(got) != "critical content" {
		t.Errorf("content = %q, want %q", got, "critical content")
	}
	// Trash subdir gone
	if _, err := os.Stat(filepath.Join(backupDir, "filesdelete", sdID)); !os.IsNotExist(err) {
		t.Errorf("trash subdir still exists after restore: %v", err)
	}
}

// TestTrashRestoreRefusesIfOriginalExists — refuse to overwrite.
func TestTrashRestoreRefusesIfOriginalExists(t *testing.T) {
	backupDir := t.TempDir()
	originalDir := t.TempDir()
	originalPath := filepath.Join(originalDir, "occupied.txt")
	if err := os.WriteFile(originalPath, []byte("user's new content"), 0644); err != nil {
		t.Fatal(err)
	}
	sdID := "sd-20260611-150455-aaaaaaaaaaaa"
	makeTrashEntry(t, backupDir, sdID, originalPath, "old trashed content")

	h := trashRestoreHandler(backupDir)
	body, _ := json.Marshal(trashRestoreRequest{SDID: sdID})
	req := httptest.NewRequest("POST", "/api/trash/restore", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h(w, req)

	if w.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409 (Conflict)", w.Code)
	}
	// Original file is unchanged
	got, _ := os.ReadFile(originalPath)
	if string(got) != "user's new content" {
		t.Errorf("user's file was modified: got %q", got)
	}
	// Trash entry is still there
	if _, err := os.Stat(filepath.Join(backupDir, "filesdelete", sdID, "metadata.json")); err != nil {
		t.Errorf("trash entry was deleted despite failed restore: %v", err)
	}
}

// TestTrashRestoreRejectsInvalidSDID — POST with a bad SD-ID returns 400.
func TestTrashRestoreRejectsInvalidSDID(t *testing.T) {
	backupDir := t.TempDir()
	h := trashRestoreHandler(backupDir)
	body, _ := json.Marshal(trashRestoreRequest{SDID: "../../etc/passwd"})
	req := httptest.NewRequest("POST", "/api/trash/restore", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

// TestTrashRestoreRejectsNonPOST — method not allowed.
func TestTrashRestoreRejectsNonPOST(t *testing.T) {
	h := trashRestoreHandler(t.TempDir())
	req := httptest.NewRequest("GET", "/api/trash/restore", nil)
	w := httptest.NewRecorder()
	h(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", w.Code)
	}
}

// TestTrashPurgeDryRun — dry_run=true counts but doesn't delete.
func TestTrashPurgeDryRun(t *testing.T) {
	backupDir := t.TempDir()
	sdID := "sd-20260611-150455-aaaaaaaaaaaa"
	makeTrashEntry(t, backupDir, sdID, "/tmp/dryrun.txt", "data")

	h := trashPurgeHandler(backupDir)
	body, _ := json.Marshal(trashPurgeRequest{SDID: sdID, DryRun: true})
	req := httptest.NewRequest("POST", "/api/trash/purge", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var resp trashPurgeResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if !resp.OK {
		t.Error("OK is false")
	}
	if !resp.DryRun {
		t.Error("DryRun is false; expected true")
	}
	if resp.DeletedCount != 1 {
		t.Errorf("deleted_count = %d, want 1", resp.DeletedCount)
	}
	// File still there (dry run)
	if _, err := os.Stat(filepath.Join(backupDir, "filesdelete", sdID, "metadata.json")); err != nil {
		t.Errorf("dry-run deleted the file: %v", err)
	}
}

// TestTrashPurgeReal — actually deletes.
func TestTrashPurgeReal(t *testing.T) {
	backupDir := t.TempDir()
	sdID := "sd-20260611-150455-aaaaaaaaaaaa"
	makeTrashEntry(t, backupDir, sdID, "/tmp/realdelete.txt", "data")

	h := trashPurgeHandler(backupDir)
	body, _ := json.Marshal(trashPurgeRequest{SDID: sdID, DryRun: false})
	req := httptest.NewRequest("POST", "/api/trash/purge", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	var resp trashPurgeResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.DryRun {
		t.Error("DryRun is true; expected false")
	}
	if resp.DeletedCount != 1 {
		t.Errorf("deleted_count = %d, want 1", resp.DeletedCount)
	}
	if _, err := os.Stat(filepath.Join(backupDir, "filesdelete", sdID)); !os.IsNotExist(err) {
		t.Errorf("real-run did not delete: %v", err)
	}
}

// TestTrashPurgeBulkByAge — older_than_days purges multiple entries.
func TestTrashPurgeBulkByAge(t *testing.T) {
	backupDir := t.TempDir()
	oldID := "sd-20260101-000000-aaaaaaaaaaaa"
	recentID := "sd-20260611-150455-bbbbbbbbbbbb"
	makeTrashEntry(t, backupDir, oldID, "/tmp/old.txt", "old")
	makeTrashEntry(t, backupDir, recentID, "/tmp/recent.txt", "recent")

	// Backdate the old one
	oldMetaPath := filepath.Join(backupDir, "filesdelete", oldID, "metadata.json")
	data, _ := os.ReadFile(oldMetaPath)
	var oldMeta TrashEntry
	json.Unmarshal(data, &oldMeta)
	oldMeta.Timestamp = time.Now().AddDate(0, 0, -30) // 30 days old
	back, _ := json.MarshalIndent(oldMeta, "", "  ")
	os.WriteFile(oldMetaPath, back, 0600)

	// Purge >7 days
	h := trashPurgeHandler(backupDir)
	body, _ := json.Marshal(trashPurgeRequest{OlderThanDays: 7, DryRun: false})
	req := httptest.NewRequest("POST", "/api/trash/purge", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	var resp trashPurgeResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.DeletedCount != 1 {
		t.Errorf("deleted_count = %d, want 1 (only the old one)", resp.DeletedCount)
	}
	// Old gone, recent still there
	if _, err := os.Stat(filepath.Join(backupDir, "filesdelete", oldID)); !os.IsNotExist(err) {
		t.Errorf("old entry not deleted: %v", err)
	}
	if _, err := os.Stat(filepath.Join(backupDir, "filesdelete", recentID)); err != nil {
		t.Errorf("recent entry was incorrectly purged: %v", err)
	}
}

// TestTrashPurgeRequiresMode — must provide sd_id or older_than_days.
func TestTrashPurgeRequiresMode(t *testing.T) {
	h := trashPurgeHandler(t.TempDir())
	body, _ := json.Marshal(trashPurgeRequest{DryRun: false}) // no sd_id, no older_than_days
	req := httptest.NewRequest("POST", "/api/trash/purge", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

// TestTrashDetailHandlerReturnsEntry — happy path.
func TestTrashDetailHandlerReturnsEntry(t *testing.T) {
	backupDir := t.TempDir()
	sdID := "sd-20260611-150455-aaaaaaaaaaaa"
	makeTrashEntry(t, backupDir, sdID, "/tmp/detail.txt", "x")

	h := trashDetailHandler(backupDir)
	req := httptest.NewRequest("GET", "/api/trash/detail/"+sdID, nil)
	w := httptest.NewRecorder()
	h(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	var e TrashEntry
	if err := json.Unmarshal(w.Body.Bytes(), &e); err != nil {
		t.Fatal(err)
	}
	if e.SDID != sdID {
		t.Errorf("SDID = %q, want %q", e.SDID, sdID)
	}
	if e.ViewURL == "" {
		t.Error("ViewURL is empty")
	}
	if e.FileName != "detail.txt" {
		t.Errorf("FileName = %q, want %q", e.FileName, "detail.txt")
	}
}

// TestTrashFileHandlerStreamsContent — GET /api/trash/file/<id>/<filename>
func TestTrashFileHandlerStreamsContent(t *testing.T) {
	backupDir := t.TempDir()
	sdID := "sd-20260611-150455-aaaaaaaaaaaa"
	makeTrashEntry(t, backupDir, sdID, "/tmp/file-content.txt", "streaming body")

	h := trashFileHandler(backupDir)
	req := httptest.NewRequest("GET", "/api/trash/file/"+sdID+"/file-content.txt", nil)
	w := httptest.NewRecorder()
	h(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "streaming body") {
		t.Errorf("body = %q, want it to contain 'streaming body'", w.Body.String())
	}
}

// TestTrashFileHandlerRejectsPathTraversal — the file path must stay under
// the trash root.
func TestTrashFileHandlerRejectsPathTraversal(t *testing.T) {
	backupDir := t.TempDir()
	h := trashFileHandler(backupDir)
	for _, bad := range []string{
		"/api/trash/file/sd-20260611-150455-aaaaaaaaaaaa/..%2F..%2Fetc%2Fpasswd",
		"/api/trash/file/..%2F..%2Fetc/passwd",
		"/api/trash/file/sd-20260611-150455-aaaaaaaaaaaa/foo/bar",
	} {
		req := httptest.NewRequest("GET", bad, nil)
		w := httptest.NewRecorder()
		h(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("path %q: status = %d, want 400", bad, w.Code)
		}
	}
}

// TestRejectCrossSite verifies CSRF protection on state-changing endpoints.
// A malicious website can make the user's browser POST to the localhost
// dashboard via a plain HTML form (CORS does not block sending, only reading).
// The guard rejects cross-site browser requests while allowing the real
// (same-origin) UI and non-browser clients such as curl.
func TestRejectCrossSite(t *testing.T) {
	cases := []struct {
		name   string
		sfs    string // Sec-Fetch-Site header value ("" = header absent)
		origin string // Origin header value ("" = header absent)
		want   int
	}{
		{"cross-site fetch", "cross-site", "", http.StatusForbidden},
		{"same-site fetch", "same-site", "", http.StatusForbidden},
		{"same-origin fetch", "same-origin", "", http.StatusOK},
		{"direct navigation", "none", "", http.StatusOK},
		{"no headers (curl)", "", "", http.StatusOK},
		{"matching origin", "", "http://example.com", http.StatusOK},
		{"foreign origin", "", "http://evil.example.com", http.StatusForbidden},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := strings.NewReader(`{"dry_run":true,"older_than_days":1}`)
			req := httptest.NewRequest("POST", "/api/trash/purge", body)
			req.Host = "example.com"
			if tc.sfs != "" {
				req.Header.Set("Sec-Fetch-Site", tc.sfs)
			}
			if tc.origin != "" {
				req.Header.Set("Origin", tc.origin)
			}
			w := httptest.NewRecorder()
			trashPurgeHandler(t.TempDir())(w, req)
			if w.Code != tc.want {
				t.Errorf("status = %d, want %d", w.Code, tc.want)
			}
		})
	}
}

// TestTrashRestoreRejectsCrossSite — the restore endpoint must apply the
// same CSRF guard as purge.
func TestTrashRestoreRejectsCrossSite(t *testing.T) {
	h := trashRestoreHandler(t.TempDir())
	req := httptest.NewRequest("POST", "/api/trash/restore",
		strings.NewReader(`{"sd_id":"sd-20260611-150455-aaaaaaaaaaaa"}`))
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	w := httptest.NewRecorder()
	h(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", w.Code)
	}
}
