@echo off
REM Build script for Windows - compiles all 4 binaries
REM Run from Windows Command Prompt or PowerShell in the project root

echo ==============================================
echo   filesystem-ultra build script (v4)
echo ==============================================
echo.

set OUT_DIR=.
set GO_LDFLAGS=-ldflags="-s -w"
set GO_FLAGS=-trimpath

echo [1/4] Building filesystem-ultra-v4.exe (no embedded ripgrep)...
if exist filesystem-ultra-v4.exe del filesystem-ultra-v4.exe
go build %GO_LDFLAGS% %GO_FLAGS% -o filesystem-ultra-v4.exe .
if %ERRORLEVEL% neq 0 goto fail
echo   OK: filesystem-ultra-v4.exe

echo.
echo [2/4] Building filesystem-ultra-v4-embed_rg.exe (with embedded ripgrep)...
if exist filesystem-ultra-v4-embed_rg.exe del filesystem-ultra-v4-embed_rg.exe
go build %GO_LDFLAGS% %GO_FLAGS% -tags embed_rg -o filesystem-ultra-v4-embed_rg.exe .
if %ERRORLEVEL% neq 0 goto fail
echo   OK: filesystem-ultra-v4-embed_rg.exe

echo.
echo [3/4] Building filesystem-ultra-v4-proxy.exe (proxy variant)...
if exist filesystem-ultra-v4-proxy.exe del filesystem-ultra-v4-proxy.exe
go build %GO_LDFLAGS% %GO_FLAGS% -tags proxy -o filesystem-ultra-v4-proxy.exe .
if %ERRORLEVEL% neq 0 goto fail
echo   OK: filesystem-ultra-v4-proxy.exe

echo.
echo [4/4] Building filesystem-ultra-v4-dashboard.exe (dashboard)...
if exist filesystem-ultra-v4-dashboard.exe del filesystem-ultra-v4-dashboard.exe
go build %GO_LDFLAGS% %GO_FLAGS% -o filesystem-ultra-v4-dashboard.exe ./cmd/dashboard/
if %ERRORLEVEL% neq 0 goto fail
echo   OK: filesystem-ultra-v4-dashboard.exe

echo.
echo ==============================================
echo   All builds successful!
echo ==============================================
echo   filesystem-ultra-v4.exe         - standard build
echo   filesystem-ultra-v4-embed_rg.exe - with embedded ripgrep
echo   filesystem-ultra-v4-proxy.exe    - proxy variant
echo   filesystem-ultra-v4-dashboard.exe - dashboard
echo.
goto end

:fail
echo.
echo Build failed!
exit /b 1

:end