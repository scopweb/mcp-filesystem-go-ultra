package core

import "sync"

type mutationBudget struct {
	mu    sync.Mutex
	used  int
	limit int
}

func (e *UltraFastEngine) SetMutationBudget(n int) {
	if e == nil {
		return
	}
	e.mutationBudget.mu.Lock()
	e.mutationBudget.limit = n
	e.mutationBudget.mu.Unlock()
}

func (e *UltraFastEngine) MutationBudgetSnapshot() (used, limit int) {
	if e == nil {
		return 0, 0
	}
	e.mutationBudget.mu.Lock()
	defer e.mutationBudget.mu.Unlock()
	return e.mutationBudget.used, e.mutationBudget.limit
}

func (e *UltraFastEngine) CheckMutationBudget() (used, limit int, ok bool) {
	if e == nil {
		return 0, 0, true
	}
	e.mutationBudget.mu.Lock()
	defer e.mutationBudget.mu.Unlock()
	used, limit = e.mutationBudget.used, e.mutationBudget.limit
	if limit <= 0 || used < limit {
		return used, limit, true
	}
	return used, limit, false
}

func (e *UltraFastEngine) RecordMutation() {
	if e == nil {
		return
	}
	e.mutationBudget.mu.Lock()
	if e.mutationBudget.limit > 0 {
		e.mutationBudget.used++
	}
	e.mutationBudget.mu.Unlock()
}
