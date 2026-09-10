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

// E3 retry contract: process-local, 24-hour results, no eviction/re-execution.
// Expired entries become tombstones. Restart changes the namespace, so an old
// identifier is rejected instead of silently executing an uncertain write.
type operationRetries struct {
	mu      sync.Mutex
	epoch   string
	entries map[string]*operationReceipt
}
type operationReceipt struct {
	args      [32]byte
	done      chan struct{}
	completed time.Time
	result    []byte
	err       error
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
	if !strings.HasPrefix(id, r.epoch+":") || len(id) <= len(r.epoch)+1 || len(id) > 200 {
		prefix := r.epoch
		r.mu.Unlock()
		return nil, fmt.Errorf("operation_id must use this server lifetime's prefix %s:<unique-id>; an ID from a prior server lifetime cannot be retried safely", prefix)
	}
	key := sessionIDFrom(ctx) + "\x00" + kind + "\x00" + id
	if receipt := r.entries[key]; receipt != nil {
		if receipt.args != hash {
			r.mu.Unlock()
			return nil, fmt.Errorf("operation_id reused with different arguments")
		}
		r.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-receipt.done:
		}
		r.mu.Lock()
		defer r.mu.Unlock()
		if time.Since(receipt.completed) > 24*time.Hour {
			receipt.result = nil
			return nil, fmt.Errorf("operation_id expired; outcome must be reconciled before a new operation")
		}
		return append([]byte(nil), receipt.result...), receipt.err
	}
	if len(r.entries) >= 4096 {
		r.mu.Unlock()
		return nil, fmt.Errorf("operation receipt capacity reached; no mutation executed")
	}
	receipt := &operationReceipt{args: hash, done: make(chan struct{})}
	r.entries[key] = receipt
	r.mu.Unlock()
	// Always resolve waiters, even if a downstream panic interrupts the action.
	defer func() {
		r.mu.Lock()
		if receipt.completed.IsZero() {
			receipt.err = fmt.Errorf("operation interrupted; outcome unknown, reconcile before retrying")
			receipt.completed = time.Now()
			close(receipt.done)
		}
		r.mu.Unlock()
	}()
	result, runErr := run()
	r.mu.Lock()
	receipt.result = append([]byte(nil), result...)
	receipt.err = runErr
	receipt.completed = time.Now()
	close(receipt.done)
	r.mu.Unlock()
	return result, runErr
}
