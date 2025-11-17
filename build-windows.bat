@echo off
REM Build script for Windows executable
REM Run this from Windows Command Prompt or PowerShell

echo Building filesystem-ultra.exe for Windows...
echo.

REM Clean old builds
if exist filesystem-ultra.exe del filesystem-ultra.exe

REM Build for Windows with optimizations
go build -ldflags="-s -w" -trimpath -o filesystem-ultra.exe .

if %ERRORLEVEL% == 0 (
    echo.
    echo Build successful!
    echo Output: filesystem-ultra.exe
    echo.
    echo This .exe is compiled for Windows and will:
    echo   - Correctly recognize Windows paths (C:\..., D:\...)
    echo   - Run natively on Windows (not through WSL^)
    echo   - Use Windows path separators (\^)
) else (
    echo.
    echo Build failed!
    exit /b 1
)
