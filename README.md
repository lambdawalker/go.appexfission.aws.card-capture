# Card capture

An independent Go feature for private identity-card upload, image processing, OCR extraction and user review. This module is `github.com/lambdawalker/go.appexfission.aws.card-capture` (the repository spelling is intentional).

The root module owns `capture`, `captureapi`, `awscapture`, and three Lambda commands. `contracts` contains only standard-library consent types and policy hashing for host interoperability. `runtimepolicy` reads and validates the existing DynamoDB `ENV#<environment>` / `CONFIG` document without administering it. `resetgate` reads the reset control token fence without owning reset execution.

Build Linux ARM64 archives with `./scripts/build.sh` or `go run ./cmd/build-lambdas -out dist`. `artifacts.Lambdas()` and `artifacts.Build` support application-controlled dependency builds.

The optional `infrastructure` module exposes `ValidateStorage`, `Validate`, `DeployStorage`, and `Deploy`. Preflight all configuration and archives before registering resources. Deploy storage first; provide its table as the `capture.facts` offering to identity, then bind identity's `identity.card-capture` integration and the shared `auth.runtime-policy` contract to the capture backend. `feature.Declare` expresses that order using independent `storage` and `backend` native resources.

The application composes reset inventory using returned storage resources and Lambda functions. No live AWS operations are part of tests.

```sh
go test -race ./...
go vet ./...
(cd infrastructure && go test -race ./... && go vet ./...)
```

## Offline artifact preview

From the repository root run `./preview.sh` or `.\preview.bat`. No AWS credentials, built archives or deployment are needed. Outputs default to repository-root `deployment-reports/preview/report.{md,json,html}`. Direct `go run ./cmd/preview` from infrastructure uses that process working directory instead.

Flags: `-environment preview`, `-config nonsecret.json` (JSON string map), `-output-dir DIR`. Native optional settings use their existing config names; auth additionally accepts `"suppliedRuntime":"true"`. Defaults are illustrative `https://app.example.test`, `example.test`, and `verify@example.test`. Host bindings are external fixtures.

To read an explicitly selected cached app report use `-cache FILE -project PROJECT -account ACCOUNT -backend BACKEND` with the matching environment; optional `-application`, `-region`, `-stack` further constrain identity. Unique exact feature/type/logical-name matches preserve physical identity and original evidence timestamps. Unknown and ambiguous evidence stays unknown. Cache does not perform a live check. The full selected source scope appears in the report; account identity is caller supplied.

Checked-in `docs/artifacts/` samples are deterministic illustrative defaults, with unknown presence and no deployed IDs.

## Isolated OCR speed bench

See [console benchmark and deployment guide](docs/ocr-bench.md) for private, standalone OCR testing from front/back JPEGs, without Android or onboarding services.
