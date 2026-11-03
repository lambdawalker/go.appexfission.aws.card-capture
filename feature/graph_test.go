package feature

import (
	composer "github.com/lambdawalker/go.infra.composer"
	"testing"
)

func TestFactsCanFeedIdentityWithoutCycle(t *testing.T) {
	capture := Declare("capture")
	hostInputs := map[string]composer.Value{"capture_table": composer.RequirementRef("identity", "capture", "table_name")}
	hostOutputs := HostIntegrationContract().Fields
	values := map[string]composer.Value{}
	for k := range hostOutputs {
		values[k] = composer.ResourceRef("identity.backend", k)
	}
	host := &composer.Feature{ID: "identity", Requirements: []composer.Requirement{{ID: "capture", Contract: FactsContract()}}, Resources: []composer.Resource{{ID: "backend", Kind: "test:Identity", Inputs: hostInputs, Outputs: hostOutputs}}, Offerings: []composer.Offering{{ID: "capture_integration", Contract: HostIntegrationContract(), Values: values}}}
	runtime := &composer.Feature{ID: "runtime", Offerings: []composer.Offering{{ID: "config", Contract: RuntimeContract(), Values: map[string]composer.Value{"table_name": composer.Literal("runtime"), "table_arn": composer.Literal("arn:runtime")}}}}
	app := composer.NewApplication("test", capture, host, runtime)
	for _, b := range [][4]string{{"identity", "capture", "capture", "facts"}, {"capture", IntegrationRequirement, "identity", "capture_integration"}, {"capture", RuntimeRequirement, "runtime", "config"}} {
		if e := app.Inject(b[0], b[1], b[2], b[3]); e != nil {
			t.Fatal(e)
		}
	}
	p, e := app.Plan(composer.Environment{Name: "dev"})
	if e != nil {
		t.Fatal(e)
	}
	if len(p.Resources()) != 3 {
		t.Fatal(p.Resources())
	}
}
