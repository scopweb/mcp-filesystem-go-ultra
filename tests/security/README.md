# Security Test Suite - MCP Filesystem Ultra v3.1.0

Complete security testing framework for Go dependencies and vulnerability detection.

## Overview

This test suite provides comprehensive security scanning including:

- **Dependency Vulnerability Scanning** - Check for known CVEs in dependencies
- **Code Security Analysis** - Detect unsafe patterns and potential vulnerabilities
- **Module Integrity Verification** - Ensure go.mod and go.sum haven't been tampered
- **Static Analysis** - Code quality and security issues (requires gosec)
- **Race Condition Detection** - Identify data races with `-race` flag
- **Coverage Analysis** - Security test coverage metrics

## Test Files

### 1. `security_tests.go`
Core security unit tests for the MCP project.

**Tests included:**
- `TestDependencyVersions` - Verify all dependencies are current
- `TestGoModuleIntegrity` - Check go.mod for suspicious patterns
- `TestGoSumIntegrity` - Validate go.sum file structure
- `TestMainDependencies` - Track critical dependencies
- `TestNoPrivateKeyCommitted` - Detect accidentally committed secrets
- `TestNoDangerousImports` - Check for unsafe/syscall imports
- `TestInputValidation` - Verify path/input validation patterns
- `TestErrorHandling` - Check error handling coverage
- `TestLogSanitization` - Ensure logs don't leak sensitive data
- `TestGoVersion` - Verify Go version compatibility

### 2. `cves_test.go`
Known CVE detection and security pattern analysis.

**Tests included:**
- `TestKnownCVEs` - Check for known vulnerabilities
- `TestPathTraversalVulnerability` - CWE-22 path traversal detection
- `TestCommandInjectionVulnerability` - CWE-78 command injection detection
- `TestRACEVulnerabilities` - Race condition patterns
- `TestMemorySafetyVulnerabilities` - Memory safety assessment
- `TestCryptographyVulnerabilities` - Crypto algorithm review
- `TestDependencySupplyChainRisk` - Supply chain risk assessment
- `TestSoftwareCompositionAnalysis` - SCA tool recommendations
- `TestRegexVulnerabilities` - ReDoS (Regular Expression DoS) detection
- `TestSecurityConfigurationBaseline` - Establish baseline
- `TestFuzzingRecommendations` - Fuzzing guidance
- `TestSecurityAuditLog` - Audit documentation

## Running Tests

### Quick Start (Windows)
```batch
cd c:\MCPs\clone\mcp-filesystem-go-ultra

REM Run all security tests
scripts\security\run_all_security_tests.bat

REM Run with detailed output
scripts\security\run_all_security_tests.bat --verbose

REM Generate coverage report
scripts\security\run_all_security_tests.bat --coverage

REM Generate security report
scripts\security\run_all_security_tests.bat --report
```

### Individual Test Runs

```batch
REM Run only Go security tests
go test ./tests/security -v

REM Run with race detection
go test ./tests/security -race -v

REM Run with coverage
go test ./tests/security -coverprofile=coverage.out

REM Run specific test
go test ./tests/security -run TestPathTraversalVulnerability -v

REM Run benchmarks
go test ./tests/security -bench=. -benchmem
```

### Vulnerability Scanning

```batch
REM Run vulnerability scan
scripts\security\vulnerability_scan.bat

REM Run with verbose output
scripts\security\vulnerability_scan.bat --verbose

REM Generate security report
scripts\security\vulnerability_scan.bat --report

REM Attempt to fix issues
scripts\security\vulnerability_scan.bat --fix
```

## Batch Script Guide

### `run_all_security_tests.bat`
Master script orchestrating complete security assessment.

**Phases:**
1. Environment Verification
2. Module Verification
3. Vulnerability Scanning
4. Security Unit Tests
5. Static Analysis (gosec)
6. Code Coverage (optional)
7. Security Benchmarks (optional)
8. Race Condition Detection
9. Summary & Reporting

**Usage:**
```batch
run_all_security_tests.bat [options]

Options:
  --verbose    Show detailed output for all phases
  --bench      Include benchmark performance tests
  --coverage   Generate code coverage metrics
  --report     Generate comprehensive security report
  --fix        Attempt to fix detected issues
```

### `vulnerability_scan.bat`
Dependency vulnerability scanner with multiple checking methods.

**Features:**
- go mod verify
- Outdated package detection
- gosec static analysis (if installed)
- nancy CVE scanning (if installed)
- go-licenses compliance (if installed)
- Manual credential detection
- Unsafe import checking

**Usage:**
```batch
vulnerability_scan.bat [options]

Options:
  --verbose    Show detailed output
  --fix        Run 'go mod tidy' to fix
  --report     Generate detailed report
```

### `run_security_tests.bat`
Focused security test runner with optional features.

**Features:**
- Go security unit test execution
- Dependency vulnerability scanning
- Optional benchmarking
- Optional code coverage generation
- Test result summarization

**Usage:**
```batch
run_security_tests.bat [options]

Options:
  --verbose    Show detailed test output
  --bench      Include benchmark tests
  --coverage   Generate coverage report
```

## Security Analysis Results

### Threat Model
MCP Filesystem Ultra is a file operations service with these primary attack surfaces:

1. **Path Traversal (CWE-22)** - Accessing files outside allowed directories
2. **Command Injection (CWE-78)** - Injecting shell commands
3. **Race Conditions** - Concurrent file access issues
4. **Dependency Vulnerabilities** - Third-party package exploits

### Current Status

| Category | Status | Notes |
|----------|--------|-------|
| Unit Tests | ✅ PASS | All security tests passing |
| Module Integrity | ✅ OK | go.mod/go.sum verified |
| Dependencies | ✅ OK | 8 direct, ~20+ transitive |
| Unsafe Code | ✅ OK | No unsafe imports |
| Secrets | ✅ OK | No hardcoded credentials |
| Path Validation | ✅ OK | Implemented in core/edit_operations.go |
| Error Handling | ✅ OK | Consistent error returns |
| Input Validation | ✅ OK | RequireString + path checks |

## Continuous Integration

For CI/CD pipelines, run:

```yaml
# GitHub Actions example
- name: Security Tests
  run: |
    cd scripts\security
    run_all_security_tests.bat --coverage --report

- name: Upload Coverage
  uses: codecov/codecov-action@v3
  with:
    files: coverage_security.out
```

## Installing Security Tools

Optional tools for enhanced analysis:

```bash
# Static analysis
go install github.com/securego/gosec/v2/cmd/gosec@latest

# CVE detection
go install github.com/sonatype-nexus-oss/nancy@latest

# License compliance
go install github.com/google/go-licenses@latest

# SBOM generation
go install github.com/anchore/syft/cmd/syft@latest
```

## Key Vulnerabilities Tested

### CWE-22: Path Traversal
Tests detect:
- `../../../etc/passwd`
- `..\..\windows\system32`
- `/etc/shadow` (absolute paths)
- URL-encoded variations: `%2e%2e/`

### CWE-78: OS Command Injection
Tests detect:
- Shell metacharacters: `;` `|` `&` `` ` ``
- Command substitution: `$(...)` `$(...)`
- Pipe chains: `file.txt | cat /etc/passwd`

### CWE-190: Integer Overflow
- Buffer size validation
- Line range boundary checks

### CWE-416: Use After Free
- Go's garbage collection prevents this

### CWE-269: Improper Access Control
- File permission checks
- Path restriction validation

## Go 1.24 Security Features

This codebase leverages:
- ✅ Memory safety (automatic)
- ✅ Type safety (compile-time)
- ✅ Bounds checking (automatic)
- ✅ Race detection flag (`-race`)
- ✅ Fuzzing support (`-fuzz`)
- ✅ Go vulnerability database (Go 1.21+)

## Security Best Practices

1. **Always run tests before deploying:**
   ```batch
   scripts\security\run_all_security_tests.bat
   ```

2. **Keep dependencies updated:**
   ```batch
   go get -u ./...
   ```

3. **Review security reports monthly:**
   ```batch
   scripts\security\run_all_security_tests.bat --report
   ```

4. **Use race detector during development:**
   ```batch
   go test -race ./...
   ```

5. **Check for new vulnerabilities:**
   ```batch
   go list -m all | nancy sleuth
   ```

## Troubleshooting

### "Module verification failed"
```batch
go mod tidy
go mod verify
```

### "Unknown test package"
Ensure you're in the project root directory:
```batch
cd c:\MCPs\clone\mcp-filesystem-go-ultra
go test ./tests/security
```

### "Command 'gosec' not found"
Install gosec for static analysis:
```bash
go install github.com/securego/gosec/v2/cmd/gosec@latest
```

### Tests timeout
Increase timeout:
```batch
go test ./tests/security -timeout 10m
```

## Generated Reports

Scripts can generate multiple reports:

- `security_report_YYYYMMDD_HHMM.txt` - Complete security audit
- `coverage_security.out` - Code coverage data
- `coverage_security.txt` - Coverage report
- `gosec_report.txt` - Static analysis findings
- `dependencies.txt` - Full dependency list

View coverage:
```batch
go tool cover -html=coverage_security.out
```

## Security Test Metrics

Baseline metrics for v3.1.0:

- **Tests:** 20+ security-focused tests
- **Coverage:** Pending (run with --coverage)
- **Critical Issues:** 0
- **High Issues:** 0
- **Dependencies:** 8 direct, ~20+ transitive
- **Review Frequency:** Monthly recommended

## License

Same as parent project (see LICENSE file)

## Support

For security issues:
1. **DO NOT** create public GitHub issues
2. Use GitHub's private security advisory feature
3. Email security team if available

## References

- [OWASP Top 10](https://owasp.org/www-project-top-ten/)
- [Go Security Best Practices](https://golang.org/doc/security)
- [CWE/SANS Top 25](https://cwe.mitre.org/top25/)
- [CVE Database](https://cve.mitre.org/)
- [Go Vulnerability Database](https://vuln.go.dev/)
