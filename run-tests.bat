@echo off
echo Running core tests...
go test ./core/... -v
echo.
echo Running all tests...
go test ./tests/... -v
echo.
echo Running security tests...
go test ./tests/security/... -v
pause
