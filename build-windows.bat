@echo off
REM ============================================================
REM  Build script for Windows - MCP Filesystem Ultra (v4+)
REM  Compiles all production binaries cleanly.
REM
REM  IMPORTANT: Run this from the project root.
REM  Requires: Go 1.26.3 or newer
REM ============================================================

echo.
echo ==============================================
echo   MCP Filesystem Ultra - Windows Build
echo ==============================================
echo.

set GO_LDFLAGS=-ldflags=-s -w
set GO_FLAGS=-trimpath

REM Output directory - keeps the project root clean
set OUT_DIR=bin
if not exist %OUT_DIR% mkdir %OUT_DIR%

REM ------------------------------------------------------------
REM 1. Main server (standard, no embedded ripgrep)
REM ------------------------------------------------------------
echo [1/4] Building %OUT_DIR%\filesystem-ultra-v4.exe ...
if exist %OUT_DIR%\filesystem-ultra-v4.exe del %OUT_DIR%\filesystem-ultra-v4.exe
go build "%GO_LDFLAGS%" %GO_FLAGS% -o %OUT_DIR%\filesystem-ultra-v4.exe .
if %ERRORLEVEL% neq 0 goto fail
echo   OK: %OUT_DIR%\filesystem-ultra-v4.exe

REM ------------------------------------------------------------
REM 2. Main server with embedded ripgrep (recommended for Claude)
REM ------------------------------------------------------------
echo.
echo [2/4] Building %OUT_DIR%\filesystem-ultra-v4-embed_rg.exe (with embedded ripgrep)...
if exist %OUT_DIR%\filesystem-ultra-v4-embed_rg.exe del %OUT_DIR%\filesystem-ultra-v4-embed_rg.exe
go build "%GO_LDFLAGS%" %GO_FLAGS% -tags embed_rg -o %OUT_DIR%\filesystem-ultra-v4-embed_rg.exe .
if %ERRORLEVEL% neq 0 goto fail
echo   OK: %OUT_DIR%\filesystem-ultra-v4-embed_rg.exe

REM ------------------------------------------------------------
REM 3. MCP Proxy (stdio logging proxy with --model / --log-dir support)
REM    This is the CORRECT way. The old -tags proxy build is dead.
REM    Output name matches documentation (MCP-PROXY.md).
REM ------------------------------------------------------------
echo.
echo [3/4] Building %OUT_DIR%\mcp-proxy.exe (logging proxy)...
if exist %OUT_DIR%\mcp-proxy.exe del %OUT_DIR%\mcp-proxy.exe
go build "%GO_LDFLAGS%" %GO_FLAGS% -o %OUT_DIR%\mcp-proxy.exe ./cmd/proxy
if %ERRORLEVEL% neq 0 goto fail
echo   OK: %OUT_DIR%\mcp-proxy.exe

REM ------------------------------------------------------------
REM 4. Dashboard (separate binary for logs/metrics/backups)
REM ------------------------------------------------------------
echo.
echo [4/4] Building %OUT_DIR%\filesystem-ultra-v4-dashboard.exe ...
if exist %OUT_DIR%\filesystem-ultra-v4-dashboard.exe del %OUT_DIR%\filesystem-ultra-v4-dashboard.exe
go build "%GO_LDFLAGS%" %GO_FLAGS% -o %OUT_DIR%\filesystem-ultra-v4-dashboard.exe ./cmd/dashboard/
if %ERRORLEVEL% neq 0 goto fail
echo   OK: %OUT_DIR%\filesystem-ultra-v4-dashboard.exe

echo.
echo ==============================================
echo   BUILD SUCCESSFUL
echo ==============================================
echo.
echo   All binaries created inside: %OUT_DIR%\
echo.
echo     %OUT_DIR%\filesystem-ultra-v4.exe
echo     %OUT_DIR%\filesystem-ultra-v4-embed_rg.exe   (recommended for Claude)
echo     %OUT_DIR%\mcp-proxy.exe                      (use this in Claude Desktop)
echo     %OUT_DIR%\filesystem-ultra-v4-dashboard.exe
echo.
echo   IMPORTANT - Claude Desktop configuration:
echo     Use "%OUT_DIR%\mcp-proxy.exe"
echo.
echo   The project root now stays clean after builds.
echo.
goto end

:fail
echo.
echo [ERROR] Build failed!
exit /b 1

:end
echo.
echo Done. You can now update your Claude Desktop config to use mcp-proxy.exe
echo.