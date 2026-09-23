package core

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const receiptSchemaVersion = 1

type diskReceipt struct {
	V         int    `json:"v"`
	Status    string `json:"status"`
	Args      string `json:"args"`
	Completed string `json:"completed,omitempty"`
	Result    string `json:"result,omitempty"`
	Error     string `json:"error,omitempty"`
}

func (e *UltraFastEngine) initReceiptStore(root string) error {
	root = filepath.Clean(strings.TrimSpace(root))
	if root == "" || root == "." {
		return fmt.Errorf("receipt-dir is empty")
	}
	store := filepath.Join(root, receiptProjectID(e.config.AllowedPaths))
	recDir := filepath.Join(store, "r")
	if err := os.MkdirAll(recDir, 0700); err != nil {
		return err
	}
	epoch, err := loadOrCreateEpoch(filepath.Join(store, "epoch"))
	if err != nil {
		return err
	}
	e.operationRetries.dir = recDir
	e.operationRetries.epoch = epoch
	e.operationRetries.entries = make(map[string]*operationReceipt)
	return nil
}

func receiptProjectID(paths []string) string {
	canon := make([]string, 0, len(paths))
	for _, p := range paths {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		canon = append(canon, CanonicalOCCKey(p))
	}
	sort.Strings(canon)
	sum := sha256.Sum256([]byte(strings.Join(canon, "\n")))
	return hex.EncodeToString(sum[:8])
}

func loadOrCreateEpoch(path string) (string, error) {
	if b, err := os.ReadFile(path); err == nil {
		ep := strings.TrimSpace(string(b))
		if epochValid(ep) {
			return ep, nil
		}
	} else if !os.IsNotExist(err) {
		return "", err
	}
	ep := secureRandomSuffix()
	if err := atomicWriteFile(path, []byte(ep+"\n"), 0600); err != nil {
		return "", err
	}
	return ep, nil
}

func epochValid(ep string) bool {
	if len(ep) < 16 || len(ep) > 64 {
		return false
	}
	for _, c := range ep {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
			return false
		}
	}
	return true
}

func (r *operationRetries) receiptFile(key string) string {
	sum := sha256.Sum256([]byte(key))
	return filepath.Join(r.dir, hex.EncodeToString(sum[:])+".json")
}

func (r *operationRetries) writeDiskLocked(key string, rec *operationReceipt) error {
	if r.dir == "" {
		return nil
	}
	d := diskReceipt{V: receiptSchemaVersion, Args: hex.EncodeToString(rec.args[:])}
	switch {
	case rec.unknown:
		d.Status = "unknown"
		if !rec.completed.IsZero() {
			d.Completed = rec.completed.UTC().Format(time.RFC3339Nano)
		}
		if rec.err != nil {
			d.Error = rec.err.Error()
		}
	case rec.completed.IsZero():
		d.Status = "pending"
	default:
		d.Status = "done"
		d.Completed = rec.completed.UTC().Format(time.RFC3339Nano)
		if len(rec.result) > 0 {
			d.Result = base64.StdEncoding.EncodeToString(rec.result)
		}
		if rec.err != nil {
			d.Error = rec.err.Error()
		}
	}
	raw, err := json.Marshal(d)
	if err != nil {
		return err
	}
	return atomicWriteFile(r.receiptFile(key), raw, 0600)
}

func (r *operationRetries) loadDiskLocked(key string) *operationReceipt {
	if r.dir == "" {
		return nil
	}
	raw, err := os.ReadFile(r.receiptFile(key))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		rec := unreadableReceipt()
		r.entries[key] = rec
		return rec
	}
	rec := parseDiskReceipt(raw)
	r.entries[key] = rec
	return rec
}

func parseDiskReceipt(raw []byte) *operationReceipt {
	var d diskReceipt
	if err := json.Unmarshal(raw, &d); err != nil {
		return unreadableReceipt()
	}
	if d.V != receiptSchemaVersion || (d.Status != "pending" && d.Status != "done" && d.Status != "unknown") {
		return unreadableReceipt()
	}
	args, err := hex.DecodeString(d.Args)
	if err != nil || len(args) != 32 {
		return unreadableReceipt()
	}
	rec := &operationReceipt{done: make(chan struct{})}
	copy(rec.args[:], args)
	close(rec.done)
	switch d.Status {
	case "pending", "unknown":
		rec.unknown = true
		rec.err = errOutcomeUnknown
		rec.completed = time.Now()
		return rec
	}
	completed, err := time.Parse(time.RFC3339Nano, d.Completed)
	if err != nil {
		return unreadableReceipt()
	}
	rec.completed = completed
	if d.Result != "" {
		result, err := base64.StdEncoding.DecodeString(d.Result)
		if err != nil {
			return unreadableReceipt()
		}
		rec.result = result
	}
	if d.Error != "" {
		rec.err = errors.New(d.Error)
	}
	return rec
}

func unreadableReceipt() *operationReceipt {
	rec := &operationReceipt{
		done:      make(chan struct{}),
		unknown:   true,
		err:       errReceiptUnreadable,
		completed: time.Now(),
	}
	close(rec.done)
	return rec
}

func (r *operationRetries) receiptCountLocked() int {
	if r.dir == "" {
		return len(r.entries)
	}
	ents, err := os.ReadDir(r.dir)
	if err != nil {
		return 4096
	}
	n := 0
	for _, e := range ents {
		if isReceiptName(e.Name()) {
			n++
		}
	}
	return n
}

func isReceiptName(name string) bool {
	if len(name) != 69 || !strings.HasSuffix(name, ".json") {
		return false
	}
	for i := 0; i < 64; i++ {
		c := name[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

var (
	errOutcomeUnknown    = fmt.Errorf("operation interrupted; outcome unknown, reconcile before retrying")
	errReceiptUnreadable = fmt.Errorf("operation receipt unreadable; outcome unknown, reconcile before retrying")
	errReceiptArgsReuse  = fmt.Errorf("operation_id reused with different arguments")
	errReceiptExpired    = fmt.Errorf("operation_id expired; outcome must be reconciled before a new operation")
	errReceiptCapacity   = fmt.Errorf("operation receipt capacity reached; no mutation executed")
)
