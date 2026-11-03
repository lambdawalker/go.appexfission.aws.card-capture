@echo off
setlocal
pushd "%~dp0infrastructure"
go run ./cmd/preview -output-dir ../deployment-reports/preview %*
set "PREVIEW_EXIT=%ERRORLEVEL%"
popd
exit /b %PREVIEW_EXIT%
