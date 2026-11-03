package artifacts

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Build compiles the feature's manifest from the current Go module environment
// and writes Linux/ARM64 Lambda archives to out. Shared command packages compile
// once. Each completed archive atomically replaces its previous file; a failure
// leaves earlier successful archives in place. Cancel ctx to stop compilation.
func Build(ctx context.Context, out string) error {
	return build(ctx, out, func(cmd *exec.Cmd) error { cmd.Stdout = os.Stdout; cmd.Stderr = os.Stderr; return cmd.Run() })
}

func build(ctx context.Context, out string, run func(*exec.Cmd) error) error {
	if strings.TrimSpace(out) == "" {
		return errors.New("output directory is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.MkdirAll(out, 0755); err != nil {
		return fmt.Errorf("create artifact directory: %w", err)
	}
	temp, err := os.MkdirTemp("", "capture-lambdas-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(temp)
	// Keep manifest order for predictable diagnostics and bounded compiler memory.
	packages := []string{}
	names := map[string][]string{}
	for _, lambda := range Lambdas() {
		if _, ok := names[lambda.Package]; !ok {
			packages = append(packages, lambda.Package)
		}
		names[lambda.Package] = append(names[lambda.Package], lambda.Name)
	}
	for _, pkg := range packages {
		if err := ctx.Err(); err != nil {
			return err
		}
		binary := filepath.Join(temp, "bootstrap")
		cmd := exec.CommandContext(ctx, "go", "build", "-trimpath", "-tags", "lambda.norpc", "-o", binary, pkg)
		cmd.Env = buildEnvironment(os.Environ())
		if err := run(cmd); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("build %s: %w", pkg, err)
		}
		for _, name := range names[pkg] {
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := packageLambda(binary, filepath.Join(out, name+".zip")); err != nil {
				return fmt.Errorf("package %s: %w", name, err)
			}
		}
	}
	return nil
}

func buildEnvironment(base []string) []string {
	env := make([]string, 0, len(base)+3)
	for _, entry := range base {
		key := strings.ToUpper(strings.SplitN(entry, "=", 2)[0])
		if strings.HasPrefix(key, "AWS_") || strings.HasPrefix(key, "PULUMI_") || key == "ATTESTRA_AWS_CREDENTIAL_SOURCE" || key == "GOOS" || key == "GOARCH" || key == "CGO_ENABLED" {
			continue
		}
		env = append(env, entry)
	}
	return append(env, "GOOS=linux", "GOARCH=arm64", "CGO_ENABLED=0")
}

func packageLambda(binary, archive string) error {
	in, err := os.Open(binary)
	if err != nil {
		return err
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("Lambda binary must be a regular file")
	}
	out, err := os.CreateTemp(filepath.Dir(archive), ".lambda-*.zip")
	if err != nil {
		return err
	}
	defer os.Remove(out.Name())
	writer := zip.NewWriter(out)
	header := &zip.FileHeader{Name: "bootstrap", Method: zip.Deflate}
	header.SetMode(0755)
	entry, err := writer.CreateHeader(header)
	if err == nil {
		_, err = io.Copy(entry, in)
	}
	if e := writer.Close(); err == nil {
		err = e
	}
	if e := out.Chmod(0644); err == nil {
		err = e
	}
	if e := out.Close(); err == nil {
		err = e
	}
	if err != nil {
		return err
	}
	return os.Rename(out.Name(), archive)
}
