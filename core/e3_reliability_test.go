package core

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func e3File(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}
func e3Content(t *testing.T, path, want string) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil || string(b) != want {
		t.Fatalf("%s: got %q, %v; want %q", path, b, err, want)
	}
}

func TestE3BatchRollbackRestoresOverwrittenCopy(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")
	e3File(t, src, "source")
	e3File(t, dst, "original destination")
	m := NewBatchOperationManager(t.TempDir(), 10)
	result := m.ExecuteBatchContext(context.Background(), BatchRequest{Atomic: true, Operations: []FileOperation{
		{Type: "copy", Source: src, Destination: dst}, {Type: "edit", Path: src, OldText: "missing", NewText: "x"},
	}})
	if result.Success || result.RollbackStatus != "complete" || !result.RollbackDone {
		t.Fatalf("%+v", result)
	}
	e3Content(t, dst, "original destination")
	e3Content(t, src, "source")
}

func TestE3JournalPreservesLaterWriter(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a")
	b := filepath.Join(dir, "b")
	e3File(t, a, "original a")
	e3File(t, b, "original b")
	e := newTestEngine(dir)
	defer e.Close()
	j := &mutationJournal{}
	ctx := context.WithValue(context.Background(), journalKey{}, j)
	for _, p := range []string{a, b} {
		txCtx, txn, err := e.BeginFileTxn(ctx, p, false)
		if err != nil {
			t.Fatal(err)
		}
		_, err = txn.Commit(txCtx, []byte("ours"), false, "test", "")
		txn.Release()
		if err != nil {
			t.Fatal(err)
		}
	}
	// Another participating writer commits after our transaction released its lock.
	if err := e.WriteFileContent(context.Background(), a, "other agent"); err != nil {
		t.Fatal(err)
	}
	status, errors := j.rollback(ctx, e)
	if status != "partial" || len(errors) != 1 || !strings.Contains(errors[0], "conflict") {
		t.Fatalf("%s: %v", status, errors)
	}
	e3Content(t, a, "other agent")
	e3Content(t, b, "original b")
}

func TestE3PipelineFailureAndCancellation(t *testing.T) {
	for _, parallel := range []bool{false, true} {
		t.Run(map[bool]string{false: "sequential", true: "parallel"}[parallel], func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "f.txt")
			e3File(t, path, "old")
			e := newTestEngine(dir)
			defer e.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			pe := NewPipelineExecutor(e)
			if !parallel {
				pe.OnProgress = func(i, total int, r StepResult) {
					if i == 0 {
						cancel()
					}
				}
			}
			req := PipelineRequest{Name: "recover", Parallel: parallel, StopOnError: true, CreateBackup: true, Steps: []PipelineStep{
				{ID: "change", Action: "edit", Params: map[string]interface{}{"files": []string{path}, "old_text": "old", "new_text": "new"}},
				{ID: "fail", Action: "edit", InputFrom: "change", Params: map[string]interface{}{"old_text": "missing", "new_text": "x"}},
			}}
			result, _ := pe.Execute(ctx, req)
			if result == nil || result.Success || result.RollbackStatus != "complete" {
				t.Fatalf("%+v", result)
			}
			e3Content(t, path, "old")
		})
	}
}

func TestE3PipelinePartialIsFailure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	e3File(t, path, "old")
	e := newTestEngine(dir)
	defer e.Close()
	req := PipelineRequest{Name: "partial", Steps: []PipelineStep{
		{ID: "ok", Action: "edit", Params: map[string]interface{}{"files": []string{path}, "old_text": "old", "new_text": "new"}},
		{ID: "fail", Action: "edit", Params: map[string]interface{}{"files": []string{path}, "old_text": "missing", "new_text": "x"}},
	}}
	for _, parallel := range []bool{false, true} {
		e3File(t, path, "old")
		req.Parallel = parallel
		result, _ := NewPipelineExecutor(e).Execute(context.Background(), req)
		if result == nil || result.Success {
			t.Fatalf("partial pipeline claimed success: %+v", result)
		}
	}
}

func TestE3PipelineCopyRecovery(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dstDir := filepath.Join(dir, "out")
	if err := os.Mkdir(dstDir, 0755); err != nil {
		t.Fatal(err)
	}
	e3File(t, src, "data")
	e := newTestEngine(dir)
	defer e.Close()
	result, _ := NewPipelineExecutor(e).Execute(context.Background(), PipelineRequest{Name: "copy", CreateBackup: true, StopOnError: true, Steps: []PipelineStep{
		{ID: "copy", Action: "copy", Params: map[string]interface{}{"files": []string{src}, "destination": dstDir}},
		{ID: "fail", Action: "edit", InputFrom: "copy", Params: map[string]interface{}{"old_text": "missing", "new_text": "x"}},
	}})
	if result == nil || result.Success || result.RollbackStatus != "complete" {
		t.Fatalf("%+v", result)
	}
	if _, err := os.Stat(filepath.Join(dstDir, "src")); !os.IsNotExist(err) {
		t.Fatalf("copy not removed: %v", err)
	}
	e3Content(t, src, "data")
}

func TestE3RetryConcurrentMismatchExpiryAndRestart(t *testing.T) {
	e := newTestEngine(t.TempDir())
	defer e.Close()
	ctx := context.Background()
	_, err := e.runOnce(ctx, "test", "probe", nil, func() ([]byte, error) { t.Fatal("invalid namespace executed"); return nil, nil })
	if err == nil {
		t.Fatal("namespace required")
	}
	id := e.operationRetries.epoch + ":one"
	entered := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	run := func() ([]byte, error) { calls.Add(1); close(entered); <-release; return []byte("result"), nil }
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		if b, err := e.runOnce(ctx, "test", id, "args", run); err != nil || string(b) != "result" {
			t.Errorf("%q %v", b, err)
		}
	}()
	<-entered
	go func() {
		defer wg.Done()
		if b, err := e.runOnce(ctx, "test", id, "args", run); err != nil || string(b) != "result" {
			t.Errorf("%q %v", b, err)
		}
	}()
	close(release)
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatalf("executions: %d", calls.Load())
	}
	if _, err := e.runOnce(ctx, "test", id, "different", run); err == nil {
		t.Fatal("mismatched retry accepted")
	}
	e.operationRetries.mu.Lock()
	for _, r := range e.operationRetries.entries {
		r.completed = time.Now().Add(-25 * time.Hour)
	}
	e.operationRetries.mu.Unlock()
	if _, err := e.runOnce(ctx, "test", id, "args", run); err == nil {
		t.Fatal("expired retry accepted")
	}
	other := newTestEngine(t.TempDir())
	defer other.Close()
	if _, err := other.runOnce(ctx, "test", id, "args", run); err == nil {
		t.Fatal("old process namespace accepted")
	}
}

func TestE3BatchRetryDoesNotAppendTwice(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")
	e3File(t, src, "first\nsecond\n")
	e3File(t, dst, "prefix\n")
	e := newTestEngine(dir)
	defer e.Close()
	m := NewBatchOperationManager(t.TempDir(), 10)
	m.SetEngine(e)
	req := BatchRequest{RetryContract: "e3-v1", OperationID: "probe", Atomic: true, Operations: []FileOperation{{Type: "extract", Source: src, Destination: dst, StartLine: 1, EndLine: 1, Append: true}}}
	if r := m.ExecuteBatchContext(context.Background(), req); r.Success {
		t.Fatal("probe executed")
	}
	req.OperationID = e.operationRetries.epoch + ":append-once"
	for i := 0; i < 2; i++ {
		if r := m.ExecuteBatchContext(context.Background(), req); !r.Success {
			t.Fatalf("%+v", r)
		}
	}
	e3Content(t, src, "second\n")
	e3Content(t, dst, "prefix\nfirst\n")
}

func TestE3PipelineRollbackWithoutBackup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")
	e3File(t, path, "old")
	e := newTestEngine(dir)
	defer e.Close()
	result, _ := NewPipelineExecutor(e).Execute(context.Background(), PipelineRequest{Name: "nobackup", StopOnError: true, Steps: []PipelineStep{
		{ID: "change", Action: "edit", Params: map[string]interface{}{"files": []string{path}, "old_text": "old", "new_text": "new"}},
		{ID: "fail", Action: "edit", Params: map[string]interface{}{"files": []string{path}, "old_text": "missing", "new_text": "x"}},
	}})
	if result == nil || result.Success || result.RollbackStatus != "complete" {
		t.Fatalf("%+v", result)
	}
	e3Content(t, path, "old")
}

func TestE3BatchRenameStopsAndRestores(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.txt")
	b := filepath.Join(dir, "b.txt")
	e3File(t, a, "A")
	e3File(t, b, "B")
	e := newTestEngine(dir)
	defer e.Close()
	journal := &mutationJournal{}
	ctx := context.WithValue(context.Background(), journalKey{}, journal)
	ctx, unlock, err := e.coordinateFiles(ctx, []string{a, filepath.Join(dir, "a2.txt"), filepath.Join(dir, "missing.txt"), filepath.Join(dir, "c.txt")})
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	result := &BatchRenameResult{}
	e.executeRenameOperations(ctx, []RenameOperation{
		{OldPath: a, NewPath: filepath.Join(dir, "a2.txt"), OldName: "a.txt", NewName: "a2.txt"},
		{OldPath: filepath.Join(dir, "missing.txt"), NewPath: filepath.Join(dir, "c.txt"), OldName: "missing.txt", NewName: "c.txt"},
	}, result)
	if result.Success || result.ErrorCount == 0 {
		t.Fatalf("%+v", result)
	}
	status, failures := journal.rollback(ctx, e)
	if status != "complete" {
		t.Fatalf("%s %v", status, failures)
	}
	e3Content(t, a, "A")
	e3Content(t, b, "B")
	if _, err := os.Stat(filepath.Join(dir, "a2.txt")); !os.IsNotExist(err) {
		t.Fatal("rename not restored")
	}
}

func TestE3AtomicCreateDirRejected(t *testing.T) {
	dir := t.TempDir()
	m := NewBatchOperationManager(t.TempDir(), 10)
	result := m.ExecuteBatchContext(context.Background(), BatchRequest{Atomic: true, Operations: []FileOperation{{Type: "create_dir", Path: filepath.Join(dir, "n")}}})
	if result.Success || len(result.Errors) == 0 {
		t.Fatalf("%+v", result)
	}
}
