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

## Live integration

See [capture API, retakes and timing](docs/agents/index.md) and the [Attestra system flow](https://raw.githubusercontent.com/lambdawalker/design.attestra/main/docs/agents/journeys.md).

## Isolated OCR speed bench

See [console benchmark and deployment guide](docs/ocr-bench.md) for private, standalone OCR testing from front/back JPEGs, without Android or onboarding services.

### Document details and issuing jurisdiction

Development OCR/review data now includes optional string keys `document_type`,
`document_name`, `issuing_region`, `issuing_city`, and `issuing_authority`, alongside
`issuing_country`. These are transcription/correction data, not verification and
not the capture policy's `document_type`. Presets are `national_id`, `passport`,
`drivers_license`, `residence_permit`, `voter_id`, and `other`; unfamiliar titles
belong in `document_name`. Old records decode with empty new fields. All fields
retain the 512 UTF-8 byte bound and existing control-character validation.

The issuing jurisdiction describes the issuing government, separately from the
holder's residence, birthplace, nationality and reviewed `origin_country`.
The model transcribes only supported issuing information and leaves unknown
jurisdiction levels blank; it must not infer a country/region from a city or
residential address. Values remain editable, and issuing authority is separate.
All new fields persist with reviewed data and participate in comparison reports.
Deploy this feature revision through the consuming host before using the new
Android review fields; older servers reject unknown review keys. No deployment
or publication is implied by these source changes.
