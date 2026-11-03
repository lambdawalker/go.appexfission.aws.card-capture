package infrastructure

import (
	"fmt"
	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi/config"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

type nativeMocks struct {
	mu        sync.Mutex
	resources map[string]pulumi.MockResourceArgs
}

func (m *nativeMocks) Call(a pulumi.MockCallArgs) (resource.PropertyMap, error) {
	return nil, fmt.Errorf("unexpected invoke %s", a.Token)
}
func (m *nativeMocks) NewResource(a pulumi.MockResourceArgs) (string, resource.PropertyMap, error) {
	m.mu.Lock()
	m.resources[a.Name] = a
	m.mu.Unlock()
	out := a.Inputs.Copy()
	for k, v := range map[string]string{"arn": "arn:example:" + a.Name, "name": a.Name, "url": "https://sqs.example/" + a.Name, "invokeArn": "arn:invoke:" + a.Name} {
		if !out[resource.PropertyKey(k)].HasValue() {
			out[resource.PropertyKey(k)] = resource.NewStringProperty(v)
		}
	}
	return a.Name + "-id", out, nil
}
func options(t *testing.T) Options {
	t.Helper()
	dir := t.TempDir()
	for _, n := range []string{"capture", "capture-worker", "capture-dispatcher"} {
		if e := os.WriteFile(filepath.Join(dir, n+".zip"), []byte("archive"), 0600); e != nil {
			t.Fatal(e)
		}
	}
	i := map[string]pulumi.StringInput{}
	for _, k := range []string{"api_id", "api_execution_arn", "pool_id", "client_id", "issuer", "region", "reset_table_name", "reset_table_arn"} {
		i[k] = pulumi.String("host-" + k)
	}
	return Options{ArtifactDir: dir, Environment: "test", RuntimeTableName: pulumi.String("runtime"), RuntimeTableARN: pulumi.String("arn:runtime"), Integration: i}
}
func TestMissingArchiveRegistersNothing(t *testing.T) {
	o := options(t)
	os.Remove(filepath.Join(o.ArtifactDir, "capture-dispatcher.zip"))
	m := &nativeMocks{resources: map[string]pulumi.MockResourceArgs{}}
	e := pulumi.RunErr(func(ctx *pulumi.Context) error { _, e := Deploy(ctx, config.New(ctx, "test"), o, nil); return e }, pulumi.WithMocks("test", "test", m))
	if e == nil || !strings.Contains(e.Error(), "capture-dispatcher.zip") {
		t.Fatal(e)
	}
	for _, r := range m.resources {
		if strings.HasPrefix(r.TypeToken, "aws:") {
			t.Fatal("resource before preflight", r.Name)
		}
	}
}
func TestNativeCaptureStorageRuntimeAndIAM(t *testing.T) {
	t.Setenv("PULUMI_CONFIG", `{"test:appOrigin":"https://dev.example.com"}`)
	o := options(t)
	m := &nativeMocks{resources: map[string]pulumi.MockResourceArgs{}}
	e := pulumi.RunErr(func(ctx *pulumi.Context) error {
		storage, e := DeployStorage(ctx, config.New(ctx, "test"), o)
		if e != nil {
			return e
		}
		d, e := Deploy(ctx, config.New(ctx, "test"), o, storage)
		if e == nil && (d.Storage != storage || len(d.Functions) != 3) {
			return fmt.Errorf("incomplete deployment")
		}
		return e
	}, pulumi.WithMocks("test", "test", m))
	if e != nil {
		t.Fatal(e)
	}
	for _, n := range []string{"id-evidence", "id-captures", "capture-validation", "capture-validation-dlq", "capture-integration", "capture-route-8", "capture-extraction-grant", "capture-dispatch-grant", "capture-worker-dispatch-grant"} {
		if _, ok := m.resources[n]; !ok {
			t.Fatal("missing original logical name", n)
		}
	}
	n := 0
	for _, r := range m.resources {
		if r.TypeToken == "aws:lambda/function:Function" {
			n++
			v := r.Inputs["environment"].ObjectValue()["variables"].ObjectValue()
			if v["RUNTIME_TABLE"].StringValue() != "runtime" || v["RESET_TABLE"].StringValue() != "host-reset_table_name" {
				t.Fatal(v)
			}
		}
	}
	if n != 3 {
		t.Fatal(n)
	}
	for _, tc := range []struct{ name, content string }{{"capture-grant", "ENV#test"}, {"capture-grant", "arn:runtime"}, {"capture-worker-grant", "ACCOUNT#*"}, {"capture-extraction-grant", "google.gemma-4-e2b"}, {"capture-api-invoke", "host-api_execution_arn"}} {
		key := "policy"
		if tc.name == "capture-api-invoke" {
			key = "sourceArn"
		}
		if !strings.Contains(m.resources[tc.name].Inputs[resource.PropertyKey(key)].StringValue(), tc.content) {
			t.Fatal(tc)
		}
	}
}

func TestStoragePreflightNeedsNoHostBindings(t *testing.T) {
	o := options(t)
	o.Integration = nil
	o.RuntimeTableName = nil
	o.RuntimeTableARN = nil
	m := &nativeMocks{resources: map[string]pulumi.MockResourceArgs{}}
	err := pulumi.RunErr(func(ctx *pulumi.Context) error {
		cfg := config.New(ctx, "test")
		if e := ValidateStorage(cfg, o); e != nil {
			return e
		}
		if e := Validate(cfg, o); e == nil {
			return fmt.Errorf("runtime preflight accepted missing host bindings")
		}
		return nil
	}, pulumi.WithMocks("test", "test", m))
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range m.resources {
		if strings.HasPrefix(r.TypeToken, "aws:") {
			t.Fatal("preflight registered resource", r.Name)
		}
	}
}
