package infrastructure

import (
	"bytes"
	"github.com/lambdawalker/go.infra.composer/adapters/pulumi/reportpreview"
	"github.com/lambdawalker/go.infra.composer/reporting"
	"strings"
	"testing"
)

func TestPreviewNativeCatalog(t *testing.T) {
	a, e := Preview(nil, "preview")
	if e != nil {
		t.Fatal(e)
	}
	n := 0
	children := 0
	for _, r := range a {
		if r.Type == "aws:lambda/function:Function" {
			n++
		}
		if r.Parent != "" {
			children++
		}
		if r.ProviderID != "" || r.Owner == "" || r.Observation.Status != reporting.Unknown {
			t.Fatal(r)
		}
	}
	if n != 3 || children == 0 {
		t.Fatal(n, children)
	}
	var b bytes.Buffer
	reporting.WriteJSON(&b, reporting.Report{Artifacts: a})
	if strings.Contains(b.String(), "proofKey") || strings.Contains(b.String(), "offline-email") {
		t.Fatal("unsafe report")
	}
}

var _ = reportpreview.Config{}

func TestPreviewServiceChildrenHaveOwnedParents(t *testing.T) {
	a, e := Preview(nil, "preview")
	if e != nil {
		t.Fatal(e)
	}
	parents := map[string]string{}
	for _, r := range a {
		parents[r.Key] = r.Type
	}
	policies, permissions := 0, 0
	for _, r := range a {
		switch r.Type {
		case "aws:iam/rolePolicy:RolePolicy", "aws:iam/rolePolicyAttachment:RolePolicyAttachment":
			if parents[r.Parent] != "aws:iam/role:Role" {
				t.Fatal("role parent missing", r.Name, r.Parent)
			}
			policies++
		case "aws:lambda/permission:Permission":
			if parents[r.Parent] != "aws:lambda/function:Function" {
				t.Fatal("function parent missing", r.Name, r.Parent)
			}
			permissions++
		}
	}
	if policies == 0 || permissions == 0 {
		t.Fatal(policies, permissions)
	}
}
