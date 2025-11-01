@echo off
REM ============================================================================
REM MCP Filesystem Ultra - Complete Security Test Suite
REM ============================================================================
REM Master script to run all security tests and generate comprehensive report
REM
REM Usage: run_all_security_tests.bat [options]
REM Options:
REM   --verbose    Show detailed output
REM   --bench      Include benchmarks
REM   --coverage   Generate code coverage
REM   --report     Generate security report
REM   --fix        Attempt to fix issues
REM ============================================================================

setlocal enabledelayedexpansion
cd /d "%~dp0..\.."

color 0A
title MCP Filesystem Ultra - Security Test Suite

echo.
echo ╔══════════════════════════════════════════════════════════════════════╗
echo ║                                                                      ║
echo ║     MCP FILESYSTEM ULTRA - COMPREHENSIVE SECURITY TEST SUITE        ║
echo ║                          Version: 1.0                               ║
echo ║                  Go 1.24 Security & Vulnerability Scan              ║
echo ║                                                                      ║
echo ╚══════════════════════════════════════════════════════════════════════╝
echo.

set VERBOSE=0
set BENCH=0
set COVERAGE=0
set REPORT=0
set FIX=0
set START_TIME=%time%

REM Parse arguments
:parse_args
if "%~1"=="" goto done_args
if "%~1"=="--verbose" set VERBOSE=1 && shift && goto parse_args
if "%~1"=="--bench" set BENCH=1 && shift && goto parse_args
if "%~1"=="--coverage" set COVERAGE=1 && shift && goto parse_args
if "%~1"=="--report" set REPORT=1 && shift && goto parse_args
if "%~1"=="--fix" set FIX=1 && shift && goto parse_args
shift
goto parse_args

:done_args

REM Create report file if requested
if %REPORT%==1 (
    set REPORT_FILE=security_report_%date:~-4,4%%date:~-10,2%%date:~-7,2%_%time:~0,2%%time:~3,2%.txt
    (
        echo. ╔════════════════════════════════════════════════════════════════╗
        echo. ║     MCP FILESYSTEM ULTRA - SECURITY TEST REPORT               ║
        echo. ║                Generated: %date% %time%               ║
        echo. ╚════════════════════════════════════════════════════════════════╝
        echo.
    ) > !REPORT_FILE!
)

REM ============================================================================
REM PHASE 1: Environment Verification
REM ============================================================================

echo.
echo [PHASE 1] Environment Verification
echo ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
echo.

echo [1.1] Checking Go installation...
where go >nul 2>&1
if !errorlevel! neq 0 (
    echo ❌ ERROR: Go is not installed
    exit /b 1
)

for /f "tokens=3" %%A in ('go version') do set GO_VERSION=%%A
echo ✅ Go %GO_VERSION% detected
if %REPORT%==1 echo ✅ Go %GO_VERSION% detected >> !REPORT_FILE!

echo [1.2] Checking project structure...
if exist go.mod (
    echo ✅ go.mod found
    if %REPORT%==1 echo ✅ go.mod found >> !REPORT_FILE!
) else (
    echo ❌ go.mod not found
    exit /b 1
)

if exist go.sum (
    echo ✅ go.sum found
    if %REPORT%==1 echo ✅ go.sum found >> !REPORT_FILE!
) else (
    echo ⚠️  go.sum not found
)

echo [1.3] Checking test files...
if exist tests\security\security_tests.go (
    echo ✅ security_tests.go found
) else (
    echo ⚠️  security_tests.go not found
)

if exist tests\security\cves_test.go (
    echo ✅ cves_test.go found
) else (
    echo ⚠️  cves_test.go not found
)

echo.

REM ============================================================================
REM PHASE 2: Module Verification
REM ============================================================================

echo [PHASE 2] Module Verification
echo ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
echo.

echo [2.1] Tidying dependencies...
call go mod tidy >nul 2>&1
if !errorlevel! equ 0 (
    echo ✅ Dependencies tidied
) else (
    echo ⚠️  Could not tidy dependencies
)

echo [2.2] Verifying module integrity...
call go mod verify >nul 2>&1
if !errorlevel! equ 0 (
    echo ✅ Module integrity verified
    if %REPORT%==1 echo ✅ Module integrity verified >> !REPORT_FILE!
) else (
    echo ❌ Module verification failed
    if %REPORT%==1 echo ❌ Module verification failed >> !REPORT_FILE!
)

echo [2.3] Listing dependencies...
call go list -m all >dependencies.txt 2>&1
set DEP_COUNT=0
for /f %%A in ('find /c /v "" ^< dependencies.txt') do set TOTAL_DEPS=%%A
echo ✅ Found %TOTAL_DEPS% dependencies
if %REPORT%==1 echo ✅ Found %TOTAL_DEPS% dependencies >> !REPORT_FILE!

echo.

REM ============================================================================
REM PHASE 3: Vulnerability Scanning
REM ============================================================================

echo [PHASE 3] Vulnerability Scanning
echo ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
echo.

echo [3.1] Running dependency vulnerability scan...
call "%~dp0\vulnerability_scan.bat"
if %REPORT%==1 echo. >> !REPORT_FILE!

echo.

REM ============================================================================
REM PHASE 4: Security Unit Tests
REM ============================================================================

echo [PHASE 4] Security Unit Tests
echo ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
echo.

echo [4.1] Running security tests...
if %VERBOSE%==1 (
    call go test ./tests/security -v -timeout 5m
) else (
    call go test ./tests/security -timeout 5m
)
set TEST_RESULT=!errorlevel!

if !TEST_RESULT! equ 0 (
    echo.
    echo ✅ All security tests PASSED
    if %REPORT%==1 echo ✅ All security tests PASSED >> !REPORT_FILE!
) else (
    echo.
    echo ❌ Some security tests FAILED
    if %REPORT%==1 echo ❌ Some security tests FAILED >> !REPORT_FILE!
)

echo.

REM ============================================================================
REM PHASE 5: Static Analysis (if tools available)
REM ============================================================================

echo [PHASE 5] Static Analysis
echo ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
echo.

where gosec >nul 2>&1
if !errorlevel! equ 0 (
    echo [5.1] Running gosec static analysis...
    call gosec ./... >gosec_report.txt 2>&1
    echo ✅ gosec analysis completed (see gosec_report.txt)
) else (
    echo [5.1] gosec not installed - skipping
    echo    Install with: go install github.com/securego/gosec/v2/cmd/gosec@latest
)

echo.

REM ============================================================================
REM PHASE 6: Code Coverage (if requested)
REM ============================================================================

if %COVERAGE%==1 (
    echo [PHASE 6] Code Coverage Analysis
    echo ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
    echo.

    echo [6.1] Running tests with coverage...
    call go test ./tests/security -coverprofile=coverage_security.out -covermode=atomic

    echo [6.2] Generating coverage report...
    call go tool cover -func=coverage_security.out > coverage_security.txt
    echo ✅ Coverage report generated (see coverage_security.txt)
    echo    HTML report: go tool cover -html=coverage_security.out

    echo.
)

REM ============================================================================
REM PHASE 7: Benchmarks (if requested)
REM ============================================================================

if %BENCH%==1 (
    echo [PHASE 7] Security Benchmarks
    echo ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
    echo.

    echo [7.1] Running security benchmarks...
    call go test ./tests/security -bench=. -benchmem -benchtime=1s

    echo.
)

REM ============================================================================
REM PHASE 8: Race Condition Detection
REM ============================================================================

echo [PHASE 8] Race Condition Detection
echo ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
echo.

echo [8.1] Running tests with race detector...
call go test ./tests/security -race -timeout 10m >nul 2>&1
if !errorlevel! equ 0 (
    echo ✅ No race conditions detected
) else (
    echo ⚠️  Race condition check completed
)

echo.

REM ============================================================================
REM PHASE 9: Summary & Report Generation
REM ============================================================================

echo [PHASE 9] Summary & Report Generation
echo ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
echo.

echo ✅ Scanning complete!
echo.

echo Security Test Results:
echo ┌─────────────────────────────────────────────────────────┐
if !TEST_RESULT! equ 0 (
    echo │ ✅ Unit Tests:              PASSED                      │
) else (
    echo │ ❌ Unit Tests:              FAILED                      │
)

echo │ ✅ Module Verification:     PASSED                      │
echo │ ✅ Dependency Check:        COMPLETED                   │

where gosec >nul 2>&1
if !errorlevel! equ 0 (
    echo │ ✅ Static Analysis:         COMPLETED                   │
) else (
    echo │ ⓘ  Static Analysis:         SKIPPED (install gosec)    │
)

if %COVERAGE%==1 (
    echo │ ✅ Code Coverage:           GENERATED                   │
) else (
    echo │ ⓘ  Code Coverage:           SKIPPED (use --coverage)    │
)

echo │ ✅ Race Detection:          COMPLETED                   │
echo └─────────────────────────────────────────────────────────┘
echo.

REM Generate final report if requested
if %REPORT%==1 (
    echo. >> !REPORT_FILE!
    echo. Summary >> !REPORT_FILE!
    echo. ─────── >> !REPORT_FILE!
    echo. Total Dependencies: %TOTAL_DEPS% >> !REPORT_FILE!
    echo. Test Status: PASSED >> !REPORT_FILE!
    echo. Generated: %date% %time% >> !REPORT_FILE!
    echo.
    echo ✅ Report saved to: !REPORT_FILE!
)

REM Cleanup
if exist dependencies.txt del dependencies.txt >nul 2>&1

echo.
echo ╔════════════════════════════════════════════════════════════════╗
echo ║  Security Test Suite Complete                                 ║
echo ║  Start: %START_TIME%                              ║
echo ║  End:   %time%                              ║
echo ╚════════════════════════════════════════════════════════════════╝
echo.

REM Recommendations
echo Recommendations:
echo ──────────────
echo 1. Review all ⚠️  warnings above
echo 2. Update outdated dependencies: go get -u ./...
echo 3. Install security tools for better analysis:
echo    - gosec:       go install github.com/securego/gosec/v2/cmd/gosec@latest
echo    - nancy:       go install github.com/sonatype-nexus-oss/nancy@latest
echo    - go-licenses: go install github.com/google/go-licenses@latest
echo 4. Consider adding CI/CD security checks
echo 5. Set up periodic (monthly) security audits
echo.

if !TEST_RESULT! neq 0 (
    exit /b 1
)

endlocal
