package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestPOSIXLauncherArgumentsAndStatus(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX launcher")
	}
	root, e := filepath.Abs("../..")
	if e != nil {
		t.Fatal(e)
	}
	bin := t.TempDir()
	capture := filepath.Join(bin, "args")
	script := "#!/bin/sh\nprintf '%s\\n' \"$PWD\" \"$@\" > \"$LAUNCHER_CAPTURE\"\nexit 23\n"
	os.WriteFile(filepath.Join(bin, "go"), []byte(script), 0700)
	cmd := exec.Command("sh", filepath.Join(root, "deploy.sh"), "--project", "with spaces", "", "--action", "build")
	cmd.Dir = t.TempDir()
	cmd.Env = append(os.Environ(), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"), "LAUNCHER_CAPTURE="+capture)
	e = cmd.Run()
	if x, ok := e.(*exec.ExitError); !ok || x.ExitCode() != 23 {
		t.Fatal(e)
	}
	b, e := os.ReadFile(capture)
	want := root + "\nrun\n./cmd/deploy\n--project\nwith spaces\n\n--action\nbuild\n"
	if e != nil || string(b) != want {
		t.Fatal(string(b), e)
	}
}
func TestBatchLauncherContract(t *testing.T) {
	b, e := os.ReadFile("../../deploy.bat")
	if e != nil {
		t.Fatal(e)
	}
	for _, part := range []string{`pushd "%~dp0" || exit /b 1`, `go run ./cmd/deploy %*`, `set "result=%errorlevel%"`, `popd`, `exit /b %result%`} {
		if !strings.Contains(string(b), part) {
			t.Fatal("missing batch contract", part)
		}
	}
}
