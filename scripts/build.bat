@echo off
:: ============================================================================
::  Compile Script for MCP Filesystem Ultra-Fast Server
:: ============================================================================

:: Set the window title
TITLE Compiling MCP Filesystem Ultra-Fast Server

:: Clear the screen
cls

echo.
echo [INFO] Starting Go project compilation...
echo [INFO] Setting up environment...

:: Set the name of the output executable
SET OUTPUT_EXE=mcp-filesystem-ultra.exe

:: The main package is the current directory
SET MAIN_PACKAGE=.

echo [INFO] Output executable: %OUTPUT_EXE%
echo [INFO] Main package: %MAIN_PACKAGE%
echo.

echo =================================================
echo           Cleaning and tidying modules...
echo =================================================
echo.
go mod tidy

echo.
echo =================================================
echo           Attempting to build...
echo =================================================
echo.

:: Run the Go build command with flags to create a smaller executable
go build -v -ldflags="-s -w" -o %OUTPUT_EXE% %MAIN_PACKAGE%

:: Check the exit code of the go build command
IF %ERRORLEVEL% == 0 (
    echo.
    echo =================================================
    echo.
    echo  [SUCCESS] Compilation successful!
    echo  Executable created: %OUTPUT_EXE%
    echo.
    echo =================================================
) ELSE (
    echo.
    echo =================================================
    echo.
    echo  [ERROR] Compilation failed.
    echo  Please check the errors above.
    echo.
    echo =================================================
)

echo.
echo Script finished. Press any key to exit.
pause >nul
