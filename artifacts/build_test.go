package artifacts

import (
	"archive/zip"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildGroupsManifestAndPackagesExecutableBootstrap(t *testing.T) {
	out := filepath.Join(t.TempDir(), "dist")
	t.Setenv("AWS_ACCESS_KEY_ID", "sensitive")
	t.Setenv("AWS_PROFILE", "sensitive")
	t.Setenv("PULUMI_ACCESS_TOKEN", "sensitive")
	t.Setenv("GOOS", "wrong")
	counts := map[string]int{}
	err := build(context.Background(), out, func(cmd *exec.Cmd) error {
		args := cmd.Args
		pkg := args[len(args)-1]
		counts[pkg]++
		if !strings.HasPrefix(pkg, Module+"/cmd/") {
			t.Fatalf("nonmodule build: %s", pkg)
		}
		if strings.Join(args[1:len(args)-3], " ") != "build -trimpath -tags lambda.norpc" {
			t.Fatalf("wrong build options %v", args)
		}
		env := strings.Join(cmd.Env, "\n")
		if strings.Contains(env, "sensitive") || !strings.Contains(env, "GOOS=linux\nGOARCH=arm64\nCGO_ENABLED=0") {
			t.Fatalf("invalid build environment")
		}
		return os.WriteFile(args[len(args)-2], []byte(pkg), 0700)
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(counts) != 3 {
		t.Fatalf("manifest builds not deduplicated: %v", counts)
	}
	files, err := os.ReadDir(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 3 {
		t.Fatalf("got %d archives", len(files))
	}
	for _, lambda := range Lambdas() {
		filename := filepath.Join(out, lambda.Name+".zip")
		info, err := os.Stat(filename)
		if err != nil {
			t.Fatal(err)
		}
		if !info.Mode().IsRegular() || info.Mode().Perm() != 0644 {
			t.Fatalf("invalid archive mode %v", info.Mode())
		}
		z, err := zip.OpenReader(filename)
		if err != nil {
			t.Fatal(err)
		}
		if len(z.File) != 1 || z.File[0].Name != "bootstrap" || z.File[0].Mode().Perm() != 0755 {
			t.Fatalf("invalid bootstrap entries: %v", z.File)
		}
		r, err := z.File[0].Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(r)
		r.Close()
		z.Close()
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != lambda.Package {
			t.Fatalf("wrong binary in %s", lambda.Name)
		}
	}
}
func TestBuildRejectsMissingOutputAndCancellation(t *testing.T) {
	calls := 0
	runner := func(*exec.Cmd) error { calls++; return nil }
	if err := build(context.Background(), "", runner); err == nil {
		t.Fatal("accepted empty output")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := build(ctx, filepath.Join(t.TempDir(), "dist"), runner); !errors.Is(err, context.Canceled) {
		t.Fatalf("want cancellation, got %v", err)
	}
	if calls != 0 {
		t.Fatal("invalid request started compiler")
	}
}
func TestBuildStopsOnCompilerFailure(t *testing.T) {
	out := filepath.Join(t.TempDir(), "dist")
	calls := 0
	err := build(context.Background(), out, func(*exec.Cmd) error { calls++; return errors.New("compiler failed") })
	if err == nil || calls != 1 {
		t.Fatalf("compiler failure ignored: %v, %d calls", err, calls)
	}
	files, err := os.ReadDir(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 0 {
		t.Fatal("failed compiler published artifacts")
	}
}
func TestPackageFailurePreservesExistingArchive(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "lambda.zip")
	if err := os.WriteFile(archive, []byte("previous"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := packageLambda(filepath.Join(dir, "missing"), archive); err == nil {
		t.Fatal("accepted missing binary")
	}
	data, err := os.ReadFile(archive)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "previous" {
		t.Fatal("failed packaging changed archive")
	}
}
