# Pipeline Guide — Parallel Execution with DAG

The `batch_operations` tool accepts a `pipeline_json` payload that executes multi-step file transformations with:

- **12 actions**: `search`, `read_ranges`, `count_occurrences`, `edit`, `multi_edit`, `regex_transform`, `copy`, `rename`, `delete`, `aggregate`, `diff`, `merge`
- **Conditional steps** (9 types): `has_matches`, `no_matches`, `count_gt`, `count_lt`, `count_eq`, `file_exists`, `file_not_exists`, `step_succeeded`, `step_failed`
- **Template variables**: `{{step_id.field}}` resolved at runtime
- **Parallel execution**: DAG-based scheduling; destructive ops within the same level are serialized for safety

This guide walks through a realistic parallel pipeline end-to-end.

---

## Scenario — Unify TODO markers across Go and JS code

**Goal.** Replace every `TODO:` comment with `FIXME:` across two language subtrees, but:

1. Do nothing if no TODOs exist (saves a backup round-trip).
2. Count occurrences per language for a summary.
3. Run the two language-specific scans and edits **in parallel** where safe.
4. Take an atomic backup first; roll back on any failure.
5. Emit a final report with counts resolved from prior steps via templates.

## Full pipeline

```json
{
  "name": "todo-to-fixme",
  "parallel": true,
  "stop_on_error": true,
  "create_backup": true,
  "steps": [
    {
      "id": "scan-go",
      "action": "search",
      "params": {"path": "src", "pattern": "TODO:", "file_types": [".go"]}
    },
    {
      "id": "scan-js",
      "action": "search",
      "params": {"path": "web", "pattern": "TODO:", "file_types": [".js", ".ts"]}
    },
    {
      "id": "count-go",
      "action": "count_occurrences",
      "input_from": "scan-go",
      "params": {"pattern": "TODO:"}
    },
    {
      "id": "count-js",
      "action": "count_occurrences",
      "input_from": "scan-js",
      "params": {"pattern": "TODO:"}
    },
    {
      "id": "all-files",
      "action": "merge",
      "input_from_all": ["scan-go", "scan-js"],
      "params": {"mode": "union"}
    },
    {
      "id": "replace-go",
      "action": "edit",
      "input_from": "scan-go",
      "condition": {"type": "count_gt", "step_ref": "count-go", "value": "0"},
      "params": {"old_text": "TODO:", "new_text": "FIXME:"}
    },
    {
      "id": "replace-js",
      "action": "edit",
      "input_from": "scan-js",
      "condition": {"type": "count_gt", "step_ref": "count-js", "value": "0"},
      "params": {"old_text": "TODO:", "new_text": "FIXME:"}
    },
    {
      "id": "summary",
      "action": "aggregate",
      "input_from_all": ["replace-go", "replace-js"],
      "params": {
        "message": "Replaced {{count-go.count}} Go TODOs and {{count-js.count}} JS/TS TODOs across {{all-files.files_count}} files."
      }
    }
  ]
}
```

## How the DAG executes

The scheduler inspects `input_from`, `input_from_all`, and `condition.step_ref` to build dependencies, then groups independent steps into levels:

```
Level 0 (parallel):        scan-go │ scan-js
Level 1 (parallel):        count-go │ count-js │ all-files
Level 2 (destructive):     replace-go → replace-js   (serialized: both edit)
Level 3:                   summary
```

Notes:
- `scan-go` and `scan-js` have no dependencies, so they run concurrently.
- `count-go`, `count-js`, and `all-files` each depend only on the scans, so they all start as soon as their input is ready.
- `replace-go` and `replace-js` would normally run in parallel, but both are destructive (`edit`), so the scheduler serializes them into sub-levels to keep rollback semantics clean.
- `summary` waits for both replaces and pulls fields from earlier steps via templates.

## Key mechanics illustrated

### 1. `input_from` vs `input_from_all`

| Field | Type | Used by |
|-------|------|---------|
| `input_from` | single step id | Consumes prior step's `files_matched` as input (e.g. `edit` operates on every file that `search` found) |
| `input_from_all` | array of step ids | `aggregate` and `merge`: combine outputs from multiple branches |

### 2. Conditional skipping

```json
{"condition": {"type": "count_gt", "step_ref": "count-go", "value": "0"}}
```

- If the condition is false, the step reports `success: true, skipped: true` — it does not fail the pipeline.
- Other condition types useful in practice:
  - `has_matches` / `no_matches` — cheap guard for "only proceed if a prior search hit"
  - `step_succeeded` / `step_failed` — recover-or-continue flows
  - `file_exists` / `file_not_exists` — guard by disk state

### 3. Template variables

Templates resolve after a dependency finishes, so `summary` can reference:

```
{{count-go.count}}      → integer from count-go
{{count-js.count}}      → integer from count-js
{{all-files.files_count}} → length of the merged file list
```

Available fields: `count`, `files_count`, `files` (comma-separated), `risk`, `edits`. Unknown keys are left as literal text, which makes misspellings easy to spot in the output.

### 4. Destructive serialization

Within a level, the scheduler splits destructive actions (`edit`, `multi_edit`, `regex_transform`, `delete`, `rename`) into sub-levels so two writers never race on overlapping files. Read-only actions in the same level (search, count_occurrences, aggregate, merge, diff) still run fully parallel.

### 5. Rollback

Because the top-level payload has `stop_on_error: true` and `create_backup: true`:

- A backup is captured **before** the first destructive step runs.
- If any step fails, all completed writes are rolled back from that backup.
- The failure message includes the offending step id and a hint from `PipelineStepError`.

## Running the pipeline

```text
batch_operations(pipeline_json: "<payload from above>")
```

Recommended first call with `dry_run: true` to preview without writing:

```json
{
  "name": "todo-to-fixme",
  "parallel": true,
  "dry_run": true,
  "steps": [ /* ... */ ]
}
```

With `--log-dir` configured, every step emits its own audit entry tagged `sub_op: "step:N/M:<id>:<action>"`, so the dashboard **Operations** page surfaces per-step progress in real time.

## Risk gates

| Level | Files | Edits |
|-------|-------|-------|
| MEDIUM | ≥ 30 | ≥ 100 |
| HIGH | ≥ 50 | ≥ 500 |
| CRITICAL | ≥ 80 | ≥ 1000 |

HIGH and CRITICAL pipelines are blocked unless the payload sets `force: true`. Run `analyze_operation` first if you're not sure where a pipeline will land.

## Related

- [BATCH_OPERATIONS_GUIDE.md](BATCH_OPERATIONS_GUIDE.md) — atomic single-level batches
- [HOOKS.md](HOOKS.md) — pre/post hooks fire for every destructive pipeline step
- [BENCHMARKS.md](BENCHMARKS.md) — measure pipeline cost locally before shipping
- See `core/pipeline_scheduler.go` for the DAG builder and `core/pipeline.go` for action dispatch.
