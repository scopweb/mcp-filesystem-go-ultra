package core

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
)

// ExecuteBatchContext is the cancelable batch entry point. Retry support is
// explicitly versioned and opt-in inside request_json.
func (m *BatchOperationManager) ExecuteBatchContext(ctx context.Context, request BatchRequest) BatchResult {
	fail := func(err error) BatchResult {
		return BatchResult{TotalOps: len(request.Operations), Errors: []string{err.Error()}}
	}
	if err := ctx.Err(); err != nil {
		return fail(err)
	}
	if request.OperationID == "" && request.RetryContract == "" {
		return m.executeBatchContext(ctx, request)
	}
	if request.OperationID == "" || request.RetryContract != "e3-v1" {
		return fail(fmt.Errorf("operation_id requires retry_contract: e3-v1"))
	}
	if m.engine == nil {
		return fail(fmt.Errorf("operation_id requires a server engine"))
	}
	if m.engine.IsReadOnly() && !request.ValidateOnly {
		return fail(fmt.Errorf("readonly: batch mutation denied"))
	}
	data, err := m.engine.runOnce(ctx, "batch", request.OperationID, request, func() ([]byte, error) { return json.Marshal(m.executeBatchContext(ctx, request)) })
	if err != nil {
		return fail(err)
	}
	var result BatchResult
	if err := json.Unmarshal(data, &result); err != nil {
		return fail(err)
	}
	return result
}

// Execute returns the original result on a recognized retry, including failures
// and partial recovery. It never starts a second execution for that identifier.
func (pe *PipelineExecutor) Execute(ctx context.Context, request PipelineRequest) (*PipelineResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if request.OperationID == "" && request.RetryContract == "" {
		return pe.execute(ctx, request)
	}
	if request.OperationID == "" || request.RetryContract != "e3-v1" {
		return nil, fmt.Errorf("operation_id requires retry_contract: e3-v1")
	}
	if pe.engine.IsReadOnly() && !request.DryRun && pe.hasDestructiveSteps(request.Steps) {
		return nil, fmt.Errorf("readonly: pipeline mutation denied")
	}
	data, runErr := pe.engine.runOnce(ctx, "pipeline", request.OperationID, request, func() ([]byte, error) {
		result, err := pe.execute(ctx, request)
		data, marshalErr := json.Marshal(result)
		if marshalErr != nil {
			return nil, marshalErr
		}
		return data, err
	})
	if len(data) == 0 {
		return nil, runErr
	}
	var result *PipelineResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return result, runErr
}

// E3 retry contract: 24-hour results, no eviction/re-execution. With
// --receipt-dir the epoch and receipts survive restart; without it the
// namespace is process-local and a prior lifetime's id is rejected.
type operationRetries struct {
	mu      sync.Mutex
	epoch   string
	dir     string
	entries map[string]*operationReceipt
}
type operationReceipt struct {
	args      [32]byte
	done      chan struct{}
	completed time.Time
	result    []byte
	err       error
	unknown   bool
}

func (e *UltraFastEngine) runOnce(ctx context.Context, kind, id string, args any, run func() ([]byte, error)) ([]byte, error) {
	encoded, err := json.Marshal(args)
	if err != nil {
		return nil, err
	}
	hash := sha256.Sum256(encoded)
	r := &e.operationRetries
	r.mu.Lock()
	if r.epoch == "" {
		r.epoch = secureRandomSuffix()
		r.entries = make(map[string]*operationReceipt)
	}
	if r.entries == nil {
		r.entries = make(map[string]*operationReceipt)
	}
	if !strings.HasPrefix(id, r.epoch+":") || len(id) <= len(r.epoch)+1 || len(id) > 200 {
		prefix := r.epoch
		r.mu.Unlock()
		return nil, fmt.Errorf("operation_id must use this receipt namespace's prefix %s:<unique-id>; an ID from an unknown epoch cannot be retried safely", prefix)
	}
	key := sessionIDFrom(ctx) + "\x00" + kind + "\x00" + id
	receipt := r.entries[key]
	if receipt == nil {
		receipt = r.loadDiskLocked(key)
	}
	if receipt != nil {
		if receipt.args != hash {
			err := receipt.err
			r.mu.Unlock()
			if receipt.unknown || receipt.args == ([32]byte{}) {
				if err == nil {
					err = errReceiptUnreadable
				}
				return nil, err
			}
			return nil, errReceiptArgsReuse
		}
		r.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-receipt.done:
		}
		r.mu.Lock()
		defer r.mu.Unlock()
		if receipt.unknown {
			return nil, receipt.err
		}
		if time.Since(receipt.completed) > 24*time.Hour {
			receipt.result = nil
			return nil, errReceiptExpired
		}
		return append([]byte(nil), receipt.result...), receipt.err
	}
	if r.receiptCountLocked() >= 4096 {
		r.mu.Unlock()
		return nil, errReceiptCapacity
	}
	receipt = &operationReceipt{args: hash, done: make(chan struct{})}
	r.entries[key] = receipt
	if err := r.writeDiskLocked(key, receipt); err != nil {
		delete(r.entries, key)
		r.mu.Unlock()
		return nil, err
	}
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		if receipt.completed.IsZero() {
			receipt.err = errOutcomeUnknown
			receipt.unknown = true
			receipt.completed = time.Now()
			_ = r.writeDiskLocked(key, receipt)
			close(receipt.done)
		}
		r.mu.Unlock()
	}()
	result, runErr := run()
	r.mu.Lock()
	receipt.result = append([]byte(nil), result...)
	receipt.err = runErr
	receipt.completed = time.Now()
	_ = r.writeDiskLocked(key, receipt)
	close(receipt.done)
	r.mu.Unlock()
	return result, runErr
}
