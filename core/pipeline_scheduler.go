package core

import (
	"fmt"
)

// PipelineScheduler builds a DAG from pipeline steps and groups them into execution levels
type PipelineScheduler struct{}

// NewPipelineScheduler creates a new scheduler
func NewPipelineScheduler() *PipelineScheduler {
	return &PipelineScheduler{}
}

// BuildExecutionPlan analyzes step dependencies and returns execution levels
// Each level contains step indices that can be run in parallel
// Steps within a level have no mutual dependencies
func (s *PipelineScheduler) BuildExecutionPlan(steps []PipelineStep) ([][]int, error) {
	n := len(steps)
	if n == 0 {
		return nil, nil
	}

	// Build step ID to index map
	idToIdx := make(map[string]int, n)
	for i, step := range steps {
		idToIdx[step.ID] = i
	}

	// Build adjacency list (dependencies: who does each step depend on?)
	deps := make([][]int, n)     // deps[i] = list of step indices that step i depends on
	depCount := make([]int, n)   // in-degree for topological sort
	dependents := make([][]int, n) // dependents[i] = steps that depend on step i

	for i, step := range steps {
		deps[i] = []int{}

		// input_from dependency
		if step.InputFrom != "" {
			if depIdx, ok := idToIdx[step.InputFrom]; ok {
				deps[i] = append(deps[i], depIdx)
				dependents[depIdx] = append(dependents[depIdx], i)
				depCount[i]++
			}
		}

		// input_from_all dependencies
		for _, ref := range step.InputFromAll {
			if depIdx, ok := idToIdx[ref]; ok {
				alreadyDep := false
				for _, d := range deps[i] {
					if d == depIdx {
						alreadyDep = true
						break
					}
				}
				if !alreadyDep {
					deps[i] = append(deps[i], depIdx)
					dependents[depIdx] = append(dependents[depIdx], i)
					depCount[i]++
				}
			}
		}

		// condition.step_ref dependency
		if step.Condition != nil && step.Condition.StepRef != "" {
			if depIdx, ok := idToIdx[step.Condition.StepRef]; ok {
				// Avoid duplicate dependency
				alreadyDep := false
				for _, d := range deps[i] {
					if d == depIdx {
						alreadyDep = true
						break
					}
				}
				if !alreadyDep {
					deps[i] = append(deps[i], depIdx)
					dependents[depIdx] = append(dependents[depIdx], i)
					depCount[i]++
				}
			}
		}
	}

	// Topological sort using Kahn's algorithm, grouping into levels
	var levels [][]int
	queue := make([]int, 0)

	// Start with steps that have no dependencies
	for i := 0; i < n; i++ {
		if depCount[i] == 0 {
			queue = append(queue, i)
		}
	}

	visited := 0
	for len(queue) > 0 {
		// Current queue forms one level (all can run in parallel)
		level := make([]int, len(queue))
		copy(level, queue)
		levels = append(levels, level)

		nextQueue := make([]int, 0)
		for _, idx := range queue {
			visited++
			for _, dep := range dependents[idx] {
				depCount[dep]--
				if depCount[dep] == 0 {
					nextQueue = append(nextQueue, dep)
				}
			}
		}
		queue = nextQueue
	}

	// Cycle detection
	if visited != n {
		return nil, fmt.Errorf("pipeline has dependency cycle (visited %d of %d steps)", visited, n)
	}

	// Sub-level splitting: separate destructive actions on the same file within a level
	levels = s.splitDestructiveLevels(levels, steps)

	return levels, nil
}

// destructiveActions are actions that modify files
var destructiveActions = map[string]bool{
	"edit":            true,
	"multi_edit":      true,
	"regex_transform": true,
	"delete":          true,
	"rename":          true,
}

// splitDestructiveLevels splits levels where multiple destructive steps might target the same files
// Read-only steps are always safe to parallelize
func (s *PipelineScheduler) splitDestructiveLevels(levels [][]int, steps []PipelineStep) [][]int {
	var result [][]int

	for _, level := range levels {
		if len(level) <= 1 {
			result = append(result, level)
			continue
		}

		// Separate read-only from destructive steps
		var readOnly []int
		var destructive []int
		for _, idx := range level {
			if destructiveActions[steps[idx].Action] {
				destructive = append(destructive, idx)
			} else {
				readOnly = append(readOnly, idx)
			}
		}

		// Read-only steps can all run together
		if len(destructive) == 0 {
			result = append(result, level)
			continue
		}

		// If there are both read-only and destructive, group read-only together
		// and serialize destructive steps
		if len(readOnly) > 0 {
			result = append(result, readOnly)
		}

		// Each destructive step gets its own sub-level for safety
		for _, idx := range destructive {
			result = append(result, []int{idx})
		}
	}

	return result
}

// GetDependencies returns the direct dependencies of a step (for debugging/display)
func (s *PipelineScheduler) GetDependencies(steps []PipelineStep) map[string][]string {
	result := make(map[string][]string, len(steps))
	for _, step := range steps {
		seen := make(map[string]bool)
		var deps []string
		if step.InputFrom != "" {
			deps = append(deps, step.InputFrom)
			seen[step.InputFrom] = true
		}
		for _, ref := range step.InputFromAll {
			if !seen[ref] {
				deps = append(deps, ref)
				seen[ref] = true
			}
		}
		if step.Condition != nil && step.Condition.StepRef != "" && !seen[step.Condition.StepRef] {
			deps = append(deps, step.Condition.StepRef)
		}
		result[step.ID] = deps
	}
	return result
}
