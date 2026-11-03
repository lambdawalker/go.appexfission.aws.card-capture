// Package feature exposes side-effect-free capture composition contracts.
package feature

import (
	composer "github.com/lambdawalker/go.infra.composer"
	"github.com/lambdawalker/go.infra.composer/configuration"
)

const (
	KindStorage            = "apexfission:capture:Storage"
	Kind                   = "apexfission:capture:Backend"
	RuntimeRequirement     = "runtime_configuration"
	IntegrationRequirement = "identity_integration"
)

func RuntimeContract() composer.Contract {
	return composer.Contract{ID: "auth.runtime-policy", Major: 1, Fields: fields("table_name", "table_arn")}
}
func FactsContract() composer.Contract {
	return composer.Contract{ID: "capture.facts", Major: 1, Fields: fields("table_name", "table_arn")}
}
func HostIntegrationContract() composer.Contract {
	return composer.Contract{ID: "identity.card-capture", Major: 1, Fields: fields("api_id", "api_execution_arn", "pool_id", "client_id", "issuer", "region", "reset_table_name", "reset_table_arn")}
}
func fields(keys ...string) map[string]composer.Field {
	m := map[string]composer.Field{}
	for _, k := range keys {
		m[k] = composer.Field{Type: composer.String}
	}
	return m
}
func Declare(id string) *composer.Feature {
	storageFields := fields("table_name", "table_arn", "bucket_name", "bucket_arn", "queue_url", "queue_arn", "dead_letter_queue_url", "dead_letter_queue_arn")
	inputs := map[string]composer.Value{}
	for k := range storageFields {
		inputs[k] = composer.ResourceRef(id+".storage", k)
	}
	for k := range HostIntegrationContract().Fields {
		inputs[k] = composer.RequirementRef(id, IntegrationRequirement, k)
	}
	inputs["runtime_table_name"] = composer.RequirementRef(id, RuntimeRequirement, "table_name")
	inputs["runtime_table_arn"] = composer.RequirementRef(id, RuntimeRequirement, "table_arn")
	return &composer.Feature{ID: id, Source: composer.Origin(), Requirements: []composer.Requirement{{ID: RuntimeRequirement, Contract: RuntimeContract(), Source: composer.Origin()}, {ID: IntegrationRequirement, Contract: HostIntegrationContract(), Source: composer.Origin()}}, Resources: []composer.Resource{{ID: "storage", Kind: KindStorage, Source: composer.Origin(), Outputs: storageFields}, {ID: "backend", Kind: Kind, Source: composer.Origin(), Inputs: inputs, Outputs: fields("capture_function_name", "worker_function_name", "dispatcher_function_name")}}, Offerings: []composer.Offering{{ID: "facts", Contract: FactsContract(), Values: map[string]composer.Value{"table_name": composer.ResourceRef(id+".storage", "table_name"), "table_arn": composer.ResourceRef(id+".storage", "table_arn")}}}}
}

type Configurable struct {
	Name, NativeKey, Risk, Reader string
	Type                          composer.Type
	Category                      configuration.Category
	Sensitivity                   configuration.Sensitivity
}

func ConfigurableProperties() []Configurable {
	return []Configurable{
		{"capture_enabled", "CAPTURE_ENABLED", "onboarding-consent-policy", "env", composer.Boolean, configuration.Application, configuration.Public},
	}
}
func ConfigurationContract(id, environment string) configuration.Declaration {
	d := configuration.Declaration{Environment: environment, PolicyVersion: "capture-configuration-v1"}
	for _, p := range ConfigurableProperties() {
		controls := []string{"scoped-access"}
		if p.Sensitivity == configuration.Confidential {
			controls = append(controls, "encrypted")
		}
		d.Properties = append(d.Properties, configuration.Property{Name: p.Name, Type: p.Type, Category: p.Category, Sensitivity: p.Sensitivity, Required: true, RequiredControls: controls})
		d.Consumers = append(d.Consumers, configuration.Consumer{Artifact: id + ".backend", Property: p.Name, Readers: []string{p.Reader}})
	}
	return d
}
