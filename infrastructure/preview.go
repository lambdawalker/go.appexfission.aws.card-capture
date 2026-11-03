package infrastructure

import (
	"github.com/lambdawalker/go.appexfission.aws.card-capture/artifacts"
	"github.com/lambdawalker/go.infra.composer/adapters/pulumi/reportpreview"
	"github.com/lambdawalker/go.infra.composer/reporting"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// Preview constructs the actual native feature offline. External host bindings
// and private empty archives are fixtures; no provider or deployment is invoked.
func Preview(config reportpreview.Config, environment string) (out []reporting.Artifact, err error) {
	cfg := reportpreview.Defaults(config)
	names := []string{}
	for _, a := range artifacts.Lambdas() {
		names = append(names, a.Name)
	}
	err = reportpreview.Archives(names, func(dir string) error {
		var e error
		out, e = reportpreview.Catalog(environment, "card-capture", "go.appexfission.aws.card-capture", func(ctx *pulumi.Context) error {
			integration := map[string]pulumi.StringInput{}
			for _, k := range []string{"api_id", "api_execution_arn", "pool_id", "client_id", "issuer", "region", "reset_table_name", "reset_table_arn"} {
				integration[k] = pulumi.String("external-" + k)
			}
			opts := Options{ArtifactDir: dir, Environment: environment, Integration: integration, RuntimeTableName: pulumi.String("external-runtime"), RuntimeTableARN: pulumi.String("arn:aws:dynamodb:us-east-1:000000000000:table/external-runtime")}
			storage, err := DeployStorage(ctx, cfg, opts)
			if err != nil {
				return err
			}
			_, err = Deploy(ctx, cfg, opts, storage)
			return err
		})
		return e
	})
	return
}
