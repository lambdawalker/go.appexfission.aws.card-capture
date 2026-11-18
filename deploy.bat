@echo off
setlocal
pushd "%~dp0" || exit /b 1
set "GOWORK=off"
go run -modfile=tools/deploy/go.mod -mod=readonly github.com/lambdawalker/go.infra.composer/deploycli/cmd/infra-deploy %*
set "result=%errorlevel%"
popd
exit /b %result%
