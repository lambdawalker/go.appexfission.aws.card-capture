package main

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestAtomicWrite(t *testing.T) {
	p := filepath.Join(t.TempDir(), "settings")
	if e := atomicWrite(p, []byte("first")); e != nil {
		t.Fatal(e)
	}
	if e := atomicWrite(p, []byte("second")); e != nil {
		t.Fatal(e)
	}
	b, e := os.ReadFile(p)
	if e != nil || string(b) != "second" {
		t.Fatal(string(b), e)
	}
}

func TestBuildCacheDetectsTampering(t *testing.T) {
	// Use a fake compiler to exercise publication/cache behavior without cloud calls.
	if runtime.GOOS == "windows" {
		t.Skip("POSIX fake compiler")
	}
	root := t.TempDir()
	bin := t.TempDir()
	fake := filepath.Join(bin, "go")
	script := `#!/bin/sh
if [ "$1" = version ]; then echo 'go version go1.27.3 linux/amd64'; exit; fi
while [ $# -gt 0 ]; do if [ "$1" = -o ]; then shift; printf binary > "$1"; exit; fi; shift; done
exit 1
`
	if e := os.WriteFile(fake, []byte(script), 0700); e != nil {
		t.Fatal(e)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example"), 0600)
	archive, e := build(context.Background(), root, false)
	if e != nil {
		t.Fatal(e)
	}
	before, _ := os.Stat(archive)
	if _, e = build(context.Background(), root, false); e != nil {
		t.Fatal(e)
	}
	after, _ := os.Stat(archive)
	if !before.ModTime().Equal(after.ModTime()) {
		t.Fatal("unchanged rebuilt")
	}
	os.WriteFile(archive, []byte("tampered"), 0600)
	if _, e = build(context.Background(), root, false); e != nil {
		t.Fatal(e)
	}
	b, _ := os.ReadFile(archive)
	if string(b) == "tampered" {
		t.Fatal("tampered archive reused")
	}
	os.WriteFile(filepath.Join(root, "source.go"), []byte("package example"), 0600)
	old, _ := os.ReadFile(archive + ".json")
	if _, e = build(context.Background(), root, false); e != nil {
		t.Fatal(e)
	}
	next, _ := os.ReadFile(archive + ".json")
	if string(old) == string(next) {
		t.Fatal("source fingerprint unchanged")
	}
}
