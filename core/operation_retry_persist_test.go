package core

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mcp/filesystem-ultra/cache"
)

func newReceiptEngine(t *testing.T, allowed, receiptDir string) *UltraFastEngine {
	t.Helper()
	cacheInstance, err := cache.NewIntelligentCache(50 * 1024 * 1024)
	if err != nil {
		t.Fatal(err)
	}
	e, err := NewUltraFastEngine(&Config{
		Cache:        cacheInstance,
		AllowedPaths: []string{allowed},
		ParallelOps:  2,
		ReceiptDir:   receiptDir,
	})
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func e3ReceiptFile(receiptDir, allowed, kind, id string) string {
	key := "\x00" + kind + "\x00" + id
	sum := sha256.Sum256([]byte(key))
	return filepath.Join(receiptDir, receiptProjectID([]string{allowed}), "r", hex.EncodeToString(sum[:])+".json")
}

func TestE3ReceiptsSurviveProcessRestart(t *testing.T) {
	if os.Getenv("E3_RECEIPT_HELPER") != "" {
		e3ReceiptHelper(t)
		return
	}
	work := t.TempDir()
	receiptDir := t.TempDir()
	src := filepath.Join(work, "src")
	dst := filepath.Join(work, "dst")
	e3File(t, src, "first\nsecond\n")
	e3File(t, dst, "prefix\n")
	run := func(mode string) {
		t.Helper()
		cmd := exec.Command(os.Args[0], "-test.run=^TestE3ReceiptsSurviveProcessRestart$", "-test.count=1")
		cmd.Env = append(os.Environ(),
			"E3_RECEIPT_HELPER=1",
			"E3_MODE="+mode,
			"E3_WORK="+work,
			"E3_RECEIPT="+receiptDir,
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("mode %s: %v\n%s", mode, err, out)
		}
	}
	run("apply")
	e3Content(t, src, "second\n")
	e3Content(t, dst, "prefix\nfirst\n")
	run("retry")
	e3Content(t, src, "second\n")
	e3Content(t, dst, "prefix\nfirst\n")
	run("mismatch")
	run("foreign")
	id, err := os.ReadFile(filepath.Join(work, "id.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(e3ReceiptFile(receiptDir, work, "batch", string(id)), []byte("{broken"), 0600); err != nil {
		t.Fatal(err)
	}
	run("truncated")
	e3Content(t, src, "second\n")
	e3Content(t, dst, "prefix\nfirst\n")
}

func e3ReceiptHelper(t *testing.T) {
	t.Helper()
	work := os.Getenv("E3_WORK")
	receiptDir := os.Getenv("E3_RECEIPT")
	mode := os.Getenv("E3_MODE")
	src := filepath.Join(work, "src")
	dst := filepath.Join(work, "dst")
	e := newReceiptEngine(t, work, receiptDir)
	defer e.Close()
	m := NewBatchOperationManager(filepath.Join(work, "backups"), 10)
	m.SetEngine(e)
	ops := []FileOperation{{Type: "extract", Source: src, Destination: dst, StartLine: 1, EndLine: 1, Append: true}}
	req := BatchRequest{RetryContract: "e3-v1", Atomic: true, Operations: ops}
	idPath := filepath.Join(work, "id.txt")
	switch mode {
	case "apply":
		req.OperationID = e.operationRetries.epoch + ":append-once"
		if r := m.ExecuteBatchContext(context.Background(), req); !r.Success {
			t.Fatalf("%+v", r)
		}
		if err := os.WriteFile(idPath, []byte(req.OperationID), 0600); err != nil {
			t.Fatal(err)
		}
	case "retry", "truncated":
		id, err := os.ReadFile(idPath)
		if err != nil {
			t.Fatal(err)
		}
		req.OperationID = string(id)
		r := m.ExecuteBatchContext(context.Background(), req)
		if mode == "retry" {
			if !r.Success {
				t.Fatalf("%+v", r)
			}
		} else if r.Success {
			t.Fatal("truncated receipt executed")
		}
	case "mismatch":
		id, err := os.ReadFile(idPath)
		if err != nil {
			t.Fatal(err)
		}
		req.OperationID = string(id)
		req.Operations = []FileOperation{{Type: "extract", Source: src, Destination: dst, StartLine: 1, EndLine: 1, Append: false}}
		if r := m.ExecuteBatchContext(context.Background(), req); r.Success {
			t.Fatal("mismatch executed")
		}
	case "foreign":
		req.OperationID = "00000000000000000000000000000000:once"
		if r := m.ExecuteBatchContext(context.Background(), req); r.Success {
			t.Fatal("foreign epoch executed")
		}
	default:
		t.Fatalf("unknown mode %q", mode)
	}
	e3Content(t, src, "second\n")
	e3Content(t, dst, "prefix\nfirst\n")
}

func TestE3ReceiptStoreReplaysErrorWithoutRerun(t *testing.T) {
	dir := t.TempDir()
	receiptDir := t.TempDir()
	e := newReceiptEngine(t, dir, receiptDir)
	id := e.operationRetries.epoch + ":err"
	var calls atomic.Int32
	_, err := e.runOnce(context.Background(), "test", id, "args", func() ([]byte, error) {
		calls.Add(1)
		return nil, errBoom
	})
	if err == nil || err.Error() != "boom" || calls.Load() != 1 {
		t.Fatalf("%v calls=%d", err, calls.Load())
	}
	e.Close()
	e2 := newReceiptEngine(t, dir, receiptDir)
	defer e2.Close()
	_, err = e2.runOnce(context.Background(), "test", id, "args", func() ([]byte, error) {
		calls.Add(1)
		return []byte("nope"), nil
	})
	if err == nil || err.Error() != "boom" || calls.Load() != 1 {
		t.Fatalf("%v calls=%d", err, calls.Load())
	}
}

func TestE3ReceiptStorePendingExpiredAndCapacity(t *testing.T) {
	dir := t.TempDir()
	receiptDir := t.TempDir()
	e := newReceiptEngine(t, dir, receiptDir)
	id := e.operationRetries.epoch + ":cap"
	if _, err := e.runOnce(context.Background(), "test", id, "args", func() ([]byte, error) { return []byte("ok"), nil }); err != nil {
		t.Fatal(err)
	}
	store := e.operationRetries.dir
	epoch := e.operationRetries.epoch
	e.Close()

	pendingID := epoch + ":pending"
	pendingReq := "pending-args"
	sum := sha256.Sum256(mustJSON(pendingReq))
	pendingRaw, _ := json.Marshal(diskReceipt{V: 1, Status: "pending", Args: hex.EncodeToString(sum[:])})
	if err := os.WriteFile(e3ReceiptFile(receiptDir, dir, "test", pendingID), pendingRaw, 0600); err != nil {
		t.Fatal(err)
	}

	expiredID := epoch + ":old"
	old := time.Now().Add(-25 * time.Hour).UTC().Format(time.RFC3339Nano)
	args := sha256.Sum256(mustJSON("args"))
	expiredRaw, _ := json.Marshal(diskReceipt{
		V: 1, Status: "done", Args: hex.EncodeToString(args[:]), Completed: old, Result: "b2s=",
	})
	if err := os.WriteFile(e3ReceiptFile(receiptDir, dir, "test", expiredID), expiredRaw, 0600); err != nil {
		t.Fatal(err)
	}

	e2 := newReceiptEngine(t, dir, receiptDir)
	defer e2.Close()
	var pendingCalls atomic.Int32
	if _, err := e2.runOnce(context.Background(), "test", pendingID, pendingReq, func() ([]byte, error) {
		pendingCalls.Add(1)
		return []byte("nope"), nil
	}); err == nil || pendingCalls.Load() != 0 {
		t.Fatalf("pending reran: %v calls=%d", err, pendingCalls.Load())
	}
	var expiredCalls atomic.Int32
	if _, err := e2.runOnce(context.Background(), "test", expiredID, "args", func() ([]byte, error) {
		expiredCalls.Add(1)
		return []byte("nope"), nil
	}); err == nil || expiredCalls.Load() != 0 {
		t.Fatalf("expired reran: %v calls=%d", err, expiredCalls.Load())
	}

	zero := hex.EncodeToString(make([]byte, 32))
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for i := 0; countReceiptFiles(t, store) < 4096; i++ {
		name := filepath.Join(store, hex.EncodeToString(padReceiptName(i))+".json")
		body := `{"v":1,"status":"done","args":"` + zero + `","completed":"` + now + `"}`
		if err := os.WriteFile(name, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		if i > 10000 {
			t.Fatal("could not fill receipt store")
		}
	}
	var capCalls atomic.Int32
	if _, err := e2.runOnce(context.Background(), "test", epoch+":full", "new", func() ([]byte, error) {
		capCalls.Add(1)
		return []byte("nope"), nil
	}); err == nil || capCalls.Load() != 0 {
		t.Fatalf("capacity reran: %v calls=%d", err, capCalls.Load())
	}
}

var errBoom = fmt.Errorf("boom")

func countReceiptFiles(t *testing.T, dir string) int {
	t.Helper()
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, e := range ents {
		if isReceiptName(e.Name()) {
			n++
		}
	}
	return n
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

func padReceiptName(i int) []byte {
	b := make([]byte, 32)
	b[31] = byte(i)
	b[30] = byte(i >> 8)
	b[29] = byte(i >> 16)
	return b
}
