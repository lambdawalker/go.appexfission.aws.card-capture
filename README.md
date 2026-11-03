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
