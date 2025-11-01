# Security Testing Scripts - MCP Filesystem Ultra

Batch scripts for comprehensive security testing and vulnerability scanning.

## Quick Start

```batch
REM Run all security tests
cd c:\MCPs\clone\mcp-filesystem-go-ultra
scripts\security\run_all_security_tests.bat
```

## Scripts Overview

### 1. `run_all_security_tests.bat` (Master Script)
Complete security testing orchestration with 9 phases.

**Size:** ~600 lines
**Execution time:** ~5-10 minutes (depending on options)
**Purpose:** One-stop security validation for CI/CD

**Phases:**
```
1. Environment Verification      - Check Go, project structure
2. Module Verification           - Validate go.mod/go.sum
3. Vulnerability Scanning        - Dependency CVE checks
4. Security Unit Tests           - Run security_tests.go + cves_test.go
5. Static Analysis               - gosec (if installed)
6. Code Coverage                 - Optional (-–coverage flag)
7. Security Benchmarks           - Optional (--bench flag)
8. Race Condition Detection      - go test -race
9. Summary & Report Generation   - Detailed security report
```

**Usage:**
```batch
run_all_security_tests.bat
run_all_security_tests.bat --verbose
run_all_security_tests.bat --coverage --report
run_all_security_tests.bat --bench --verbose
```

**Output:**
- Console summary with emoji status indicators
- Optional: `security_report_*.txt` with detailed findings
- Optional: `coverage_security.out` with coverage metrics
- Optional: `gosec_report.txt` with static analysis results

---

### 2. `vulnerability_scan.bat` (Dependency Scanner)
Focused dependency vulnerability scanner.

**Size:** ~350 lines
**Execution time:** ~30-60 seconds
**Purpose:** Quick dependency security check

**Features:**
- ✅ go.mod/go.sum integrity verification
- ✅ Outdated package detection
- ✅ gosec static analysis (optional)
- ✅ nancy CVE scanning (optional)
- ✅ go-licenses compliance (optional)
- ✅ Hardcoded secret detection
- ✅ Unsafe import checking

**Usage:**
```batch
vulnerability_scan.bat
vulnerability_scan.bat --verbose
vulnerability_scan.bat --report
vulnerability_scan.bat --fix
vulnerability_scan.bat --verbose --report --fix
```

**Key Checks:**
```batch
REM Step 1: Module file validation
if not exist go.mod → ERROR

REM Step 2: Dependency list
go list -m all

REM Step 3: Outdated packages
go list -u -m all | findstr "["

REM Step 4: Module verification
go mod verify

REM Step 5: gosec analysis (if installed)
gosec ./...

REM Step 6: nancy CVE scan (if installed)
go list -json -m all | nancy sleuth

REM Step 7: Manual security checks
REM - Unsafe imports
REM - Hardcoded credentials
REM - Error handling patterns
```

---

### 3. `run_security_tests.bat` (Test Runner)
Focused security test execution with optional analysis.

**Size:** ~250 lines
**Execution time:** ~2-5 minutes (depending on options)
**Purpose:** Run security unit tests + optional features

**Phases:**
1. Environment verification
2. Go security unit tests
3. Optional: Dependency scanning
4. Optional: Benchmarks
5. Optional: Coverage report

**Usage:**
```batch
run_security_tests.bat
run_security_tests.bat --verbose
run_security_tests.bat --coverage
run_security_tests.bat --bench
run_security_tests.bat --verbose --coverage --bench
```

**Test Execution:**
```batch
go test ./tests/security -v -timeout 5m [options]
```

---

## Script Structure

All scripts follow consistent structure:

```batch
@echo off
REM Header with documentation
REM Parse command-line arguments
REM Main execution phases
REM Generate reports
REM Print summary
REM Exit with appropriate code
```

## Features

### Error Handling
- ✅ Check command existence: `where command`
- ✅ Verify error codes: `if !errorlevel!`
- ✅ Graceful failure messages
- ✅ Exit with non-zero on failures

### User Feedback
- ✅ Emoji indicators (✅ ❌ ⚠️ ⓘ)
- ✅ Progress indicators
- ✅ Organized output with headers
- ✅ Timing information
- ✅ Recommendations

### Report Generation
- ✅ Date-stamped filenames
- ✅ Detailed findings
- ✅ Machine-readable format
- ✅ HTML coverage reports

### Cross-Platform Considerations
- Uses DOS batch compatible commands
- Handles Windows path separators
- Tests for tool installation
- Works with or without optional tools

## Key Commands Used

| Command | Purpose | Fallback |
|---------|---------|----------|
| `go mod verify` | Check integrity | Exit on fail |
| `go list -m all` | List dependencies | Manual check |
| `go test` | Run unit tests | Fail build |
| `go test -race` | Race detection | Optional |
| `gosec ./...` | Static analysis | Skip if not installed |
| `go tool cover` | Coverage analysis | Optional |
| `nancy` | CVE detection | Skip if not installed |
| `go-licenses` | License check | Skip if not installed |

## Integration Examples

### GitHub Actions
```yaml
- name: Security Tests
  run: |
    cd scripts\security
    run_all_security_tests.bat --coverage --report
```

### Azure Pipelines
```yaml
- script: |
    cd scripts\security
    call run_all_security_tests.bat --report
  displayName: 'Run Security Tests'
```

### GitLab CI
```yaml
security_tests:
  script:
    - cd scripts\security
    - run_all_security_tests.bat --verbose
```

### AppVeyor
```yaml
build_script:
  - cd scripts\security
  - run_all_security_tests.bat
```

## Recommended Usage Patterns

### Development
```batch
REM Quick check before committing
scripts\security\vulnerability_scan.bat
```

### Pre-Release
```batch
REM Full assessment before release
scripts\security\run_all_security_tests.bat --coverage --report
```

### Continuous Monitoring
```batch
REM Weekly cron job
scripts\security\run_all_security_tests.bat --report
REM Check for new reports in scheduled task
```

### CI/CD Pipeline
```batch
REM As part of test suite
scripts\security\run_security_tests.bat --coverage
```

## Output Examples

### Console Output
```
╔════════════════════════════════════════════════════════════════╗
║     MCP FILESYSTEM ULTRA - SECURITY TEST SUITE                ║
║                          Version: 1.0                         ║
╚════════════════════════════════════════════════════════════════╝

[PHASE 1] Environment Verification
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
[1.1] Checking Go installation...
✅ Go go1.24.6 detected

[1.2] Checking project structure...
✅ go.mod found
✅ go.sum found

[1.3] Checking test files...
✅ security_tests.go found
✅ cves_test.go found

[PHASE 2] Module Verification
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
[2.1] Tidying dependencies...
✅ Dependencies tidied

[2.2] Verifying module integrity...
✅ Module integrity verified

[2.3] Listing dependencies...
✅ Found 23 dependencies

...

Security Test Results:
┌─────────────────────────────────────────────────────────┐
│ ✅ Unit Tests:              PASSED                      │
│ ✅ Module Verification:     PASSED                      │
│ ✅ Dependency Check:        COMPLETED                   │
│ ✅ Static Analysis:         COMPLETED                   │
│ ✅ Race Detection:          COMPLETED                   │
└─────────────────────────────────────────────────────────┘

╔════════════════════════════════════════════════════════════════╗
║  Security Test Suite Complete                                 ║
║  Start: 13:45:32.23                                            ║
║  End:   13:52:15.87                                            ║
╚════════════════════════════════════════════════════════════════╝
```

### Report File
```
════════════════════════════════════════════════════════════════
    MCP FILESYSTEM ULTRA - SECURITY TEST REPORT
    Generated: 11/01/2024 13:52:15.87
════════════════════════════════════════════════════════════════

✅ Go go1.24.6 detected
✅ go.mod found
✅ Module integrity verified
✅ Found 23 dependencies

[Details of all tests, findings, and recommendations]
```

## Troubleshooting

### Script won't run
```batch
REM May need to enable batch execution
powershell -Command "Set-ExecutionPolicy -ExecutionPolicy RemoteSigned -Scope CurrentUser"
```

### "Command not found"
```batch
REM Make sure you're in correct directory
cd c:\MCPs\clone\mcp-filesystem-go-ultra
REM Or use full path
c:\MCPs\clone\mcp-filesystem-go-ultra\scripts\security\run_all_security_tests.bat
```

### Tests timeout
```batch
REM Increase timeout in the script or:
go test ./tests/security -timeout 15m
```

### Report not generated
```batch
REM Check write permissions and use --report flag
scripts\security\run_all_security_tests.bat --report --verbose
```

## Performance Characteristics

| Operation | Time | Notes |
|-----------|------|-------|
| Module verify | ~100ms | Usually instant |
| List deps | ~500ms | Network might affect |
| Run unit tests | ~3-5s | Depends on test count |
| gosec analysis | ~2-5s | Scans all Go files |
| nancy CVE scan | ~5-10s | Requires network |
| Race detection | ~10-30s | Must rebuild tests |
| Coverage report | ~5-10s | Depends on test count |

**Total execution:** ~30-60 seconds (without optional features)

## Contributing

To add new security tests:

1. Add Go test function to `tests/security/security_tests.go` or `cves_test.go`
2. Name test: `TestSecurityFeature`
3. Add phase to master script if needed
4. Update this README with new test description
5. Commit and push

## Security Reporting

For security vulnerabilities found during testing:

1. **DO NOT** report publicly
2. Use GitHub's private security advisory
3. Include:
   - Description
   - Test case
   - Impact assessment
   - Suggested fix

## References

- Go Security Best Practices: https://golang.org/doc/security
- OWASP Testing Guide: https://owasp.org/www-project-web-security-testing-guide/
- CWE/SANS Top 25: https://cwe.mitre.org/top25/
- Go Vulnerability Database: https://vuln.go.dev/

## Version History

- **v1.0** (2024-11-01)
  - Initial release
  - 3 batch scripts
  - 20+ security tests
  - Full CI/CD integration
