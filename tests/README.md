# Tests

## Test Files

- **mcp_functions_test.go** - Core MCP function tests, parameter parsing, context validation, telemetry, and search parameters
- **edit_safety_test.go** - Edit safety validation (multiline, line endings, large files)
- **coordinate_tracking_test.go** - Coordinate tracking in search results
- **large_file_processor_test.go** - Large file processing (in-memory, line-by-line, dry-run, auto-selection)
- **regex_transformer_test.go** - Regex transformation with capture groups, limits, case sensitivity
- **security/** - Security tests and fuzzing

## Run Tests

```bash
go test ./tests/... ./core/...
```

## Run with Race Detector

```bash
go test -race ./...
```

## Run Specific Test

```bash
go test ./tests/ -run TestName -v
```
