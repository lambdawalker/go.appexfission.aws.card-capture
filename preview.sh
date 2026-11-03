#!/usr/bin/env bash
set -euo pipefail
cd -- "$(dirname -- "${BASH_SOURCE[0]}")"
go -C infrastructure run ./cmd/preview -output-dir ../deployment-reports/preview "$@"
