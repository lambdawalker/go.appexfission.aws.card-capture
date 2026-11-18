# Deployment alignment audit

The matrix records the [alignment revision](https://github.com/lambdawalker/go.appexfission.aws.card-capture/tree/b711fe98d5809717b6ec3cbfe7a8e153a52323ba). The deployment implementation and its tests moved from `cmd/deploy` to the shared [Infra Composer CLI](https://github.com/lambdawalker/go.infra.composer/tree/cfce9564fec239531a392b37078a8ccc350c28cd/deploycli).

Audit date: 2026-11-17. Authoritative requirements: [Go/AWS](https://github.com/lambdawalker/agents.prompts/blob/main/prompts/libraries/go-aws.md), [shared](https://github.com/lambdawalker/agents.prompts/blob/main/prompts/libraries/shared.md), and the linked [Composer report protocol](https://github.com/lambdawalker/design.infra.composer/blob/main/docs/artifact-reports.md). No open repository PRs existed at the initial inspection. Other repositories were not used as compliance evidence.

Scope: this repository's Go deployment program owns the **isolated OCR benchmark**. The host-integrated card-capture infrastructure and the separately documented demo invitation stack retain their existing owners, entrypoints and identities. This change does not migrate either into a new application architecture.

Implemented means a code path exists with the listed offline evidence; it does not mean a live deployment was performed. Partial identifies a remaining limitation, not a waived requirement. No full-alignment claim is made.

| Requirement | Before | After | Evidence / remaining limitation |
|---|---|---|---|
| Root POSIX/batch launchers, same Go program, caller-independent directory, args/status | Partial | Implemented | `deploy.sh`, `deploy.bat`, `cmd/deploy/launchers_test.go`; batch now stops if directory change fails. Windows console behavior still needs native acceptance. |
| Setup/build/preview/deploy/status/reconfigure/recovery | Implemented | Implemented | `cmd/deploy/main.go`, `project.go`, `bootstrap.go`; rerun resumes durable receipts; status does not initialize stacks. |
| Tool prerequisites and supported versions | Partial | Implemented | `validation.go`, `main.go`: AWS CLI v2, Pulumi v3, module Go version; bounded checks and actionable failures. |
| First question project, then environment; valid custom names; saved environment | Implemented | Implemented | `project.go`, `main.go`, `project_test.go`; production deliberately prohibited for this existing test stack. |
| Discover/select saved environments | Partial | Implemented | Last environment reused; first selection displays saved directory names plus dev/qa and accepts a validated custom name or explicit `--environment`. |
| Admin/index, Backend, All routing and shared-first orchestration | Not applicable | Not applicable | Standalone bench has one target, no admin/index. No invented component choices. |
| Domain/deployment prefixes and unprefixed reused SES identity | Not applicable | Not applicable | This stack has no DNS, hostnames or SES. Existing `ocr-<environment>` stack and resource names preserved. |
| Central nonsecret records and ignored encrypted local vault | Implemented | Implemented | `project.go`, `main.go`, `.gitignore`; `~/.infra-composer/<project>/environments/<environment>` and `.deploy/`. |
| Consistent versioned common record schema across tools | Partial | Partial | Settings/project/stack/vault/portable/GitHub/apply records versioned; flags/bootstrap receipts now upgrade compatible legacy records to version 1. No cross-repository schema change. |
| Scoped saved-answer precedence, explicit overrides and target collisions | Partial | Implemented | `validation.go`, `project.go`, `main.go`, tests; account/region/backend overrides cannot silently retarget an existing scope. |
| Scope-safe vault encryption, unlock once, no secret regeneration | Implemented | Implemented | AES-GCM, PBKDF2/random salt/nonce in `main.go`; path-bound vault; corrupt/unlock failures preserve bytes; existing Pulumi passphrase requested, never generated. |
| Private permissions, atomic records, concurrent writers | Partial | Implemented | `private.go`, `locks.go`, atomic writes; repository workspace and artifact locks added. Windows ACL implementation cross-compiles; native ACL acceptance pending. |
| Portable nonsecret export/import and legacy vault migration | Partial | Implemented | `migration.go`, `vault_migration.go`; explicit scope check, no ownership/success transfer; legacy decrypt/re-encrypt preserves source. |
| General moved-checkout vault rebinding / secret rotation commands | Partial | Partial | Legacy layout migration supported; arbitrary path relocation and local vault rotation require recovery at the original path. No unsafe rename workaround. |
| SSO profile/session discovery, expired-session login, STS role/account | Partial | Implemented | `validation.go`, `main.go`; CLI profiles discovered; SSO configuration and STS assumed role checked; mocked expiry tests. |
| Mismatch protection before remote mutation | Partial | Implemented | `verifySavedTarget`, `stack_scope.go`, stack receipt checks; region/account/backend constraint failures cannot be accepted by countdown. |
| Explicit CI settings and OIDC, no workstation SSO | Implemented | Implemented | CI requires account/region/backend/project/environment, assumed role, secret and deploy approval; workflow mapping validated. |
| First-run S3 question, collision-safe secure bootstrap, durable retry | Implemented | Implemented | `bootstrap.go`, tests; pending creation probes exact bucket instead of silently choosing another. |
| Existing S3 owner/region/versioning/encryption/public access/TLS/access | Partial | Implemented | `main.go`, `state_policy.go`: expected owner, configuration reads, TLS deny, prefix listing. IAM write authorization remains exercised by the actual requested operation, not by a test write. |
| Authoritative stack listing; inaccessible/lost initialized stacks stop | Implemented | Implemented | `ensureStack`, `project_test.go`; stricter receipt version/stack checks. |
| Detect lost remote history when all local receipts are also gone | Missing | Partial | Successful remote listing is authoritative; local known-history receipt protects reruns. A completely lost local registry plus deleted remote state still requires operator recovery; do not treat a new checkout as migration authorization. |
| Restore encrypted config/secrets-provider metadata before edits | Implemented | Implemented | `config_refresh.go`, tests; only exact never-deployed response tolerated; missing YAML never regenerates secrets. |
| Five-second eligible nonsecret defaults, Enter and overrides | Partial | Implemented | Unified `prompts.go`; fake-clock tests; text input disables expiry while being edited. Required text has no timer. |
| EOF/Escape/Ctrl+C/errors cancel, no surviving readers or buffered approvals | Partial | Implemented | Child reader killed/joined on every exit, raw terminal restored, platform input flush, fake-clock and pipe tests. |
| Secret and destructive input must be deliberate, no timeout | Missing | Implemented | Hidden explicit secrets; destroy and destructive-plan approval require exact stack name; CI destructive plans rejected. |
| In-place terminal countdown, plain logs, NO_COLOR | Implemented | Implemented | `prompts.go`, `ui.go`, tests. Native Windows console acceptance still pending; cross-compilation is not console testing. |
| Default interactive deploy; explicit preview/build/status never apply | Implemented | Implemented | `action.go`, tests; build/preview then approval; help is non-mutating. |
| Apply only successful exact saved plan and artifact | Partial | Implemented | `main.go`: plan SHA check, artifact SHA check, artifact lock across preview/apply, `pulumi up --plan`. |
| Optional GitHub wizard, setup-only, saved/skipped choices | Partial | Implemented | `github_wizard.go`, `main.go`; local runs can skip; explicit `github-setup` reviews reconciliation. |
| Verify GitHub identity/repository; create protected environment | Missing | Implemented | `github.go`: verified repo/identity; first creation includes reviewer and default-branch policy; preserves existing protections (no existing-environment PUT). |
| OIDC provider/role/trust creation and managed policy reconciliation | Missing | Partial | Missing provider/role created after typed review. Exact repo/environment/audience trust required. New role needs operator-reviewed `--github-policy`; no universal policy generator. Incompatible existing trust is reported incomplete rather than overwritten. |
| Managed variables/secrets, unrelated settings preserved | Partial | Implemented | Paginated reads, per-variable deltas, stdin secret transfer; secret-name inventory plus scoped delivery/rotation receipts. Values are never claimed verified. |
| Canonical GitHub fingerprint, drift checks, marker last, retries | Partial | Partial | Schema/workflow/protections/identity/policy/secret-version fingerprint, readable checks, final role/policy checks, partial retry tests. External writers cannot participate in a distributed lock; final rechecks detect observed drift, not all races. |
| Insufficient permissions: exact recovery targets, incomplete outcome | Partial | Implemented | `github.go`, operator recovery instructions; no failure treated as cache hit or successful setup. Unsupported protected-environment plans remain incomplete. |
| Manual Action same program/schema, scoped permissions/OIDC/concurrency | Partial | Implemented | `.github/workflows/ocr-bench-deploy.yml`; explicit approval checkbox defaults false, protected environment, conservative target concurrency; no automatic deployment on push. |
| Source/dependency/embedded asset/toolchain/build recipe fingerprint | Partial | Implemented | `build_inputs.go`: Go dependency graph, local replacement contents, embed files, canonical SHA-256 manifest; build inputs rechecked before publication. |
| Archive content integrity, deterministic packaging, unchanged reuse | Implemented | Implemented | `main.go`, mocked compiler tests; per-target input/archive hashes; missing/corrupt/tampered artifacts rebuild. |
| Success records distinct from desired inputs and artifact hashes | Partial | Implemented | Build manifest and scoped `applied.json` after successful apply/output retrieval; saved plan fingerprint separate from artifact fingerprints. No health claim from a local success record. |
| Force rebuild/reconcile and cache cleanup controls | Partial | Implemented | `--force-rebuild`, `--force-reconcile`, `--action clean-cache`; cleanup locks and removes only this target’s archive and manifest. |
| Pulumi owns remote package/config updates; no hash skips preview | Implemented | Implemented | `pulumi.NewFileArchive`, exact-plan flow; no ad hoc Lambda update API. |
| Bounded parallel work, serialization, throttling/backoff | Partial | Partial | `--workers` bounds independent checks, single benchmark compilation, locks serialize shared writes. Feature-library multi-Lambda builder remains serial; no deployment-wide throttling scheduler. AWS CLI handles its own retries. |
| Ownership metadata, taggable/untaggable inventory, external boundaries | Implemented | Implemented | `metadata.go`, `reports.go`, `infrastructure/cmd/ocr-bench/main.go`, ownership/report tests; no renames or host ownership changes. |
| Equivalent safe JSON/Markdown/HTML, timestamps, partial failures, report-only | Implemented | Implemented | Existing Composer model/adapters retained; allowlisted status/outputs added; report-save failure now returns failure. |
| Reports for failures before authentication/stack selection | Missing | Partial | Reports start after selecting/restoring stack; early failures return explicit errors without a fabricated resource inventory. |
| Scope-checked report caches, private atomic output, safe settings | Implemented | Implemented | `reports.go`, `reports_test.go`; failed reads invalidate evidence and raw diagnostics/secrets are omitted. |
| DNS/SES staged readiness, shared email ownership | Not applicable | Not applicable | Not part of the isolated benchmark stack. |
| Health/live acceptance, backup, interruption recovery, migration docs | Partial | Implemented | `docs/ocr-bench.md`; status is explicitly output inspection, read-only report refresh verifies resources; chargeable inference acceptance remains operator-only. |
| Offline behavior tests, launcher checks, Windows build | Partial | Implemented | `cmd/deploy/*_test.go`, CI jobs; actual validation results recorded in PR, native console acceptance explicitly separate. |

Remaining partial rows are deliberately visible. In particular, automatic generation of a correct account-specific IAM policy, arbitrary vault relocation, distributed GitHub writer coordination, and native Windows acceptance must not be inferred from mocked tests or a successful compiler run.

## Validation performed

All checks below were run locally without AWS/GitHub mutations, inference calls, email or workflow dispatch:

- Root and infrastructure modules: `go test -p 1 -race ./...` and `go vet -p 1 ./...` passed using Go 1.27.3.
- `./scripts/build.sh` produced capture, capture-worker and capture-dispatcher Lambda archives.
- `./deploy.sh --action build` produced the benchmark archive; an unchanged second run reported verified archive reuse.
- `GOOS=windows GOARCH=amd64 go build -p 1 -o /tmp/card-capture-deploy.exe ./cmd/deploy` passed. This is compilation, not native console execution.
- POSIX launcher arguments/empty argument/working-directory/exit-code tests passed; `sh -n deploy.sh` passed. Batch contract tests passed; a normal CI Windows smoke job is included, but was not dispatched as a deployment test.
- Actionlint 1.7.12 passed for the CI and manual deployment workflows. `git diff --check` passed.
- Mock/fake-clock tests cover defaults/overrides/Enter/timeout/editing, EOF/input errors/Escape/Ctrl+C, explicit untimed approval semantics, expired SSO/CI isolation, target and remote identity mismatches, missing/failed stack discovery, partial bootstrap/GitHub setup, unchanged reruns, drift/rotation, tampered artifacts, concurrent writers and action-to-apply boundaries.

The initial parallel infrastructure compiler was killed by the workspace memory limit; another initial build exhausted its cache filesystem. Only caches created during this task were cleared. The successful retries used `GOMAXPROCS=2`, `GOGC=20`, `GOMEMLIMIT=1500MiB`, a scratch Go cache and serial compilation. Those resource workarounds are validation settings, not changes to deployment identities or runtime behavior.
