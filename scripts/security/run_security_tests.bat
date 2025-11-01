@echo off
REM ============================================================================
REM MCP Filesystem Ultra - Run Security Tests
REM ============================================================================
REM Executes all security-related tests
REM
REM Usage: run_security_tests.bat [options]
REM Options:
REM   --verbose    Show detailed test output
REM   --bench      Include benchmark tests
REM   --coverage   Generate coverage report
REM ============================================================================

setlocal enabledelayedexpansion
cd /d "%~dp0..\.."

echo.
echo ╔════════════════════════════════════════════════════════════════╗
echo ║  MCP Filesystem Ultra - Security Test Suite                    ║
echo ║  Version: 1.0 | Go Testing Framework                           ║
echo ╚════════════════════════════════════════════════════════════════╝
echo.

set VERBOSE=0
set BENCH=0
set COVERAGE=0

REM Parse arguments
:parse_args
if "%~1"=="" goto done_args
if "%~1"=="--verbose" set VERBOSE=1 && shift && goto parse_args
if "%~1"=="--bench" set BENCH=1 && shift && goto parse_args
if "%~1"=="--coverage" set COVERAGE=1 && shift && goto parse_args
shift
goto parse_args

:done_args

echo [*] Preparing security test environment...
echo.

REM Check if Go is installed
where go >nul 2>&1
if !errorlevel! neq 0 (
    echo ❌ ERROR: Go is not installed or not in PATH
    exit /b 1
)

echo ✅ Go environment verified
echo.

REM Build flags
set BUILD_FLAGS=-v

if %COVERAGE%==1 (
    set BUILD_FLAGS=!BUILD_FLAGS! -coverprofile=coverage_security.out -covermode=atomic
)

if %VERBOSE%==1 (
    set BUILD_FLAGS=!BUILD_FLAGS! -v -run=".*"
)

REM Run tests
echo [STEP 1] Running Go security unit tests...
if %VERBOSE%==1 (
    call go test ./tests/security !BUILD_FLAGS! -timeout 5m
) else (
    call go test ./tests/security !BUILD_FLAGS! -timeout 5m 2>&1 | findstr /v "^    "
)

set TEST_RESULT=!errorlevel!

echo.
if !TEST_RESULT! equ 0 (
    echo ✅ Security unit tests PASSED
) else (
    echo ❌ Security unit tests FAILED (exit code: !TEST_RESULT!)
)
echo.

REM Run benchmarks if requested
if %BENCH%==1 (
    echo [STEP 2] Running security benchmarks...
    call go test ./tests/security -bench=. -benchmem -benchtime=1s
    echo.
)

REM Generate coverage report if requested
if %COVERAGE%==1 (
    echo [STEP 3] Generating coverage report...
    if exist coverage_security.out (
        call go tool cover -func=coverage_security.out | tail -5
        echo.
        echo 📊 Coverage report saved to: coverage_security.out
        echo   View HTML: go tool cover -html=coverage_security.out
    )
    echo.
)

REM Run vulnerability scan
echo [STEP 2] Running dependency vulnerability scan...
call "%~dp0\vulnerability_scan.bat" --verbose
set SCAN_RESULT=!errorlevel!

echo.

REM Summary
echo.
echo ╔════════════════════════════════════════════════════════════════╗
echo ║  Security Test Summary                                         ║
echo ╚════════════════════════════════════════════════════════════════╝

if !TEST_RESULT! equ 0 (
    echo ✅ Unit Tests:       PASSED
) else (
    echo ❌ Unit Tests:       FAILED
)

if !SCAN_RESULT! equ 0 (
    echo ✅ Vulnerability Scan: PASSED
) else (
    echo ⚠️  Vulnerability Scan: Warnings found
)

if %COVERAGE%==1 (
    echo ✅ Coverage Report:  Generated
)

echo.
echo Test Execution Time: %time%
echo.

REM Exit with appropriate code
if !TEST_RESULT! neq 0 (
    exit /b 1
)

exit /b 0
