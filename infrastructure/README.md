# Native capture infrastructure

`Options` accepts `ArtifactDir`, `Environment`, `RuntimeTableName`, `RuntimeTableARN`, and `Integration`. Integration fields are `api_id`, `api_execution_arn`, `pool_id`, `client_id`, `issuer`, `region`, `reset_table_name`, and `reset_table_arn`.

Call `ValidateStorage` before any resources are registered. It validates configuration and all three archives without requiring host bindings. `DeployStorage` creates the private versioned bucket, capture table, queue and DLQ with their original logical names. `Validate` additionally requires runtime and identity bindings. `Deploy` adds Lambdas, IAM, scheduling and existing `/onboarding/id/*` routes to the supplied API.

Storage is independent of identity. `Storage.Table` supplies identity's read-only capture facts. `Deployment.Functions` and `Deployment.Storage` let the application compose its environment-reset manifest without capture importing authentication or the reset engine.
