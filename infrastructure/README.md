# Native capture infrastructure

`Options` accepts `ArtifactDir`, `Environment`, `RuntimeTableName`, `RuntimeTableARN`, and `Integration`. Integration fields are `api_id`, `api_execution_arn`, `pool_id`, `client_id`, `issuer`, `region`, `reset_table_name`, and `reset_table_arn`.

Call `ValidateStorage` before any resources are registered. It validates configuration and all three archives without requiring host bindings. `DeployStorage` creates the private versioned bucket, capture table, queue and DLQ with their original logical names. `Validate` additionally requires runtime and identity bindings. `Deploy` adds Lambdas, IAM, scheduling and existing `/onboarding/id/*` routes to the supplied API.

Storage is independent of identity. `Storage.Table` supplies identity's read-only capture facts. `Deployment.Functions` and `Deployment.Storage` let the application compose its environment-reset manifest without capture importing authentication or the reset engine.

## Offline artifact preview

From the repository root run `./preview.sh` or `.\preview.bat`. No AWS credentials, built archives or deployment are needed. Outputs default to repository-root `deployment-reports/preview/report.{md,json,html}`. Direct `go run ./cmd/preview` from infrastructure uses that process working directory instead.

Flags: `-environment preview`, `-config nonsecret.json` (JSON string map), `-output-dir DIR`. Native optional settings use their existing config names; auth additionally accepts `"suppliedRuntime":"true"`. Defaults are illustrative `https://app.example.test`, `example.test`, and `verify@example.test`. Host bindings are external fixtures.

To read an explicitly selected cached app report use `-cache FILE -project PROJECT -account ACCOUNT -backend BACKEND` with the matching environment; optional `-application`, `-region`, `-stack` further constrain identity. Unique exact feature/type/logical-name matches preserve physical identity and original evidence timestamps. Unknown and ambiguous evidence stays unknown. Cache does not perform a live check. The full selected source scope appears in the report; account identity is caller supplied.

Checked-in `docs/artifacts/` samples are deterministic illustrative defaults, with unknown presence and no deployed IDs.
