@echo off
setlocal
pushd "%~dp0" || exit /b 1
go run ./cmd/deploy %*
set "result=%errorlevel%"
popd
exit /b %result%
