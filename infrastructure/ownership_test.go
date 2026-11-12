package infrastructure

import (
	"strings"
	"testing"

	"github.com/lambdawalker/go.infra.composer/adapters/pulumi/awslabels"
	"github.com/lambdawalker/go.infra.composer/ownership"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi/config"
)

func TestStorageInventoryIncludesEveryNativeResource(t *testing.T) {
	t.Setenv("PULUMI_CONFIG", `{"test:appOrigin":"https://dev.example.com"}`)
	o := options(t)
	m := &nativeMocks{resources: map[string]pulumi.MockResourceArgs{}}
	var rows []any
	err := pulumi.RunErr(func(ctx *pulumi.Context) error {
		ctx, tracker, err := awslabels.Install(ctx, ownership.Metadata{
			Owner: "host", Application: "test", Environment: "test", Domain: "dev.example.com",
			Feature: "card-capture", Deployment: "test-test",
		})
		if err != nil {
			return err
		}
		if _, err := DeployStorage(ctx, config.New(ctx, "test"), o); err != nil {
			return err
		}
		if err := tracker.Export(ctx, "resourceInventory"); err != nil {
			return err
		}
		pulumi.All(ctx.GetCurrentExportMap()["resourceInventory"]).ApplyT(func(v []any) error {
			rows = v[0].([]any)
			return nil
		})
		return nil
	}, pulumi.WithMocks("test", "test", m))
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, value := range rows {
		row := value.(map[string]any)
		key := row["type"].(string) + "::" + row["logicalName"].(string)
		if seen[key] {
			t.Fatalf("duplicate inventory entry: %s", key)
		}
		seen[key] = true
		if row["owner"] != "go.appexfission.aws.card-capture" {
			t.Errorf("incorrect resource owner for %s: %v", key, row["owner"])
		}
	}
	want := 0
	for _, resource := range m.resources {
		if !strings.HasPrefix(resource.TypeToken, "aws:") {
			continue
		}
		want++
		key := resource.TypeToken + "::" + resource.Name
		if !seen[key] {
			t.Errorf("native resource absent from inventory: %s", key)
		}
	}
	if len(rows) != want {
		t.Errorf("inventory has %d entries for %d native resources", len(rows), want)
	}
	policy := "aws:dynamodb/resourcePolicy:ResourcePolicy::capture-comparison-access"
	if !seen[policy] {
		t.Errorf("comparison policy missing from inventory: %s", policy)
	}
}
