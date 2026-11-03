package feature

import (
	"strings"
	"testing"
)

func TestStorageHasNoIdentityDependency(t *testing.T) {
	f := Declare("capture")
	if len(f.Resources) != 2 || f.Resources[0].ID != "storage" || f.Resources[0].Kind != KindStorage || len(f.Resources[0].Inputs) != 0 {
		t.Fatal(f.Resources)
	}
	if f.Resources[1].ID != "backend" || f.Resources[1].Kind != Kind {
		t.Fatal(f.Resources[1])
	}
	if len(f.Requirements) != 2 || f.Offerings[0].ID != "facts" || f.Offerings[0].Contract.ID != "capture.facts" {
		t.Fatal(f)
	}
	if !strings.Contains(HostIntegrationContract().ID, "card-capture") || len(HostIntegrationContract().Fields) != 8 {
		t.Fatal(HostIntegrationContract())
	}
	if _, ok := f.Resources[1].Inputs["table_name"]; !ok {
		t.Fatal("missing storage reference")
	}
}
