package artifacts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestManifestOwnsOwnedFeatureArchives(t *testing.T) {
	entries := Lambdas()
	if len(entries) != 3 {
		t.Fatalf("expected3archives,got%d", len(entries))
	}
	seen := map[string]bool{}
	signin := 0
	for _, e := range entries {
		if seen[e.Name] {
			t.Fatal("duplicate", e.Name)
		}
		seen[e.Name] = true
		if e.Name == "admin" || e.Name == "index" {
			t.Fatal("application command leaked into feature")
		}
		if !strings.HasPrefix(e.Package, Module+"/cmd/") {
			t.Fatal(e.Package)
		}
		if _, err := os.Stat(filepath.Join("..", strings.TrimPrefix(e.Package, Module+"/"), "main.go")); err != nil {
			t.Fatal(err)
		}
		if strings.HasSuffix(e.Package, "/signin") {
			signin++
		}
	}
	if signin != 0 {
		t.Fatal("route archives mustshareone signinbuild", signin)
	}
	entries[0].Name = "mutated"
	if Lambdas()[0].Name == "mutated" {
		t.Fatal("manifestaliasesinternalstate")
	}
}

func TestVerificationCommandsAreNotAuthenticationArtifacts(t *testing.T) {
	for _, a := range Lambdas() {
		if a.Name == "signup" || a.Name == "resend" || a.Name == "confirm" {
			t.Fatalf("verification artifact still owned by auth: %s", a.Name)
		}
	}
}
