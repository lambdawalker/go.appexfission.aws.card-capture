#!/usr/bin/env sh
set -eu
ROOT=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
cd "$ROOT"
GOWORK=off exec go run -modfile=tools/deploy/go.mod -mod=readonly github.com/lambdawalker/go.infra.composer/deploycli/cmd/infra-deploy "$@"
