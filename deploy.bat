@echo off
setlocal
pushd "%~dp0"
go run ./cmd/deploy %*
set "result=%errorlevel%"
popd
exit /b %result%
