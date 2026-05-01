@echo off
title MCP Filesystem Ultra - Dashboard

set LOG_DIR=C:\temp\mcp-ultra-logs
set PROXY_LOG_DIR=C:\temp\mcp-proxy-logs
set BACKUP_DIR=C:\backups\filesystem-ultra
set PORT=9100

REM Kill any previous dashboard instance so the port is free
taskkill /F /IM dashboard.exe >nul 2>&1

echo Starting Dashboard on http://localhost:%PORT%
echo Log dir:       %LOG_DIR%
echo Proxy log dir: %PROXY_LOG_DIR%
echo Backup dir:    %BACKUP_DIR%
echo.

"%~dp0dashboard.exe" --log-dir="%LOG_DIR%" --proxy-log-dir="%PROXY_LOG_DIR%" --backup-dir="%BACKUP_DIR%" --port=%PORT%

pause
