// deploy orchestrates the isolated OCR stack. It never deploys during build/status.
package main

import (
	"archive/zip"
	"bufio"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

type Settings struct {
	Version                                        int
	Environment, Profile, Region, Account, Backend string
}

var input = bufio.NewReader(os.Stdin)

func main() {
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintln(os.Stderr, r)
			os.Exit(1)
		}
	}()
	if len(os.Args) > 1 && os.Args[1] == "--confirm-child" {
		os.Exit(readApproval())
	}

	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}

// The child owns terminal mode and input; the parent can cancel it without leaked readers.
func readApproval() int {
	var value byte
	if runtime.GOOS == "windows" {
		c := exec.Command("powershell", "-NoProfile", "-Command", `if ([Console]::IsInputRedirected) { $v=[Console]::Read(); if($v -lt 0){exit 2}; [Console]::Write([char]$v) } else { [Console]::Write([Console]::ReadKey($true).KeyChar) }`)
		c.Stdin = os.Stdin
		b, e := c.Output()
		if e != nil || len(b) == 0 {
			return 2
		}
		value = b[0]
	} else {
		state := exec.Command("stty", "-g")
		state.Stdin = os.Stdin
		old, e := state.Output()
		if e == nil {
			raw := exec.Command("stty", "-icanon", "min", "1", "time", "0")
			raw.Stdin = os.Stdin
			if raw.Run() != nil {
				return 2
			}
			defer func() {
				restore := exec.Command("stty", strings.TrimSpace(string(old)))
				restore.Stdin = os.Stdin
				_ = restore.Run()
			}()
		}
		var one [1]byte
		n, e := os.Stdin.Read(one[:])
		if e != nil || n != 1 {
			return 2
		}
		value = one[0]
	}
	if value == 'y' || value == 'Y' || value == '\n' || value == '\r' {
		return 0
	}
	return 2
}

func prompt(label, def string) string {
	fmt.Printf("%s [%s]: ", label, def)
	s, e := input.ReadString('\n')
	if e != nil {
		panic("input closed; cancelled")
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return def
	}
	return s
}
func secret(label string) (string, error) {
	fmt.Fprintln(os.Stderr, label)
	var c *exec.Cmd
	if runtime.GOOS == "windows" {
		c = exec.Command("powershell", "-NoProfile", "-Command", `$s=Read-Host -AsSecureString; $p=[Runtime.InteropServices.Marshal]::SecureStringToBSTR($s); try {[Runtime.InteropServices.Marshal]::PtrToStringBSTR($p)} finally {[Runtime.InteropServices.Marshal]::ZeroFreeBSTR($p)}`)
	} else {
		c = exec.Command("sh", "-c", `old=$(stty -g </dev/tty) || exit 1; trap 'stty "$old" </dev/tty' EXIT INT TERM; stty -echo </dev/tty; IFS= read -r value </dev/tty; printf '%s' "$value"`)
	}
	c.Stderr = os.Stderr
	b, e := c.Output()
	fmt.Fprintln(os.Stderr)
	return strings.TrimRight(string(b), "\r\n"), e
}
func command(ctx context.Context, dir string, args ...string) error {
	c := exec.CommandContext(ctx, args[0], args[1:]...)
	c.Dir = dir
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	c.Stdin = os.Stdin
	return c.Run()
}
func output(ctx context.Context, args ...string) ([]byte, error) {
	c := exec.CommandContext(ctx, args[0], args[1:]...)
	c.Stderr = os.Stderr
	return c.Output()
}
func atomicWrite(path string, b []byte) error {
	f, e := os.CreateTemp(filepath.Dir(path), ".pending-")
	if e != nil {
		return e
	}
	name := f.Name()
	defer os.Remove(name)
	if e = f.Chmod(0600); e == nil {
		_, e = f.Write(b)
	}
	if closeErr := f.Close(); e == nil {
		e = closeErr
	}
	if e != nil {
		return e
	}
	return os.Rename(name, path)
}
func vault(path string) (string, error) {
	b, e := os.ReadFile(path)
	fresh := os.IsNotExist(e)
	if e != nil && !fresh {
		return "", e
	}
	pass, e := secret("Local vault unlock passphrase:")
	if e != nil || pass == "" {
		return "", errors.New("vault unlock cancelled")
	}
	if fresh {
		again, e := secret("Confirm vault passphrase:")
		if e != nil || again != pass {
			return "", errors.New("passphrases differ")
		}
		b = make([]byte, 20)
		copy(b, []byte("OCV1"))
		if _, e = rand.Read(b[4:]); e != nil {
			return "", e
		}
	}
	if len(b) < 20 || string(b[:4]) != "OCV1" {
		return "", errors.New("invalid vault; restore backup")
	}
	key, e := pbkdf2.Key(sha256.New, pass, b[4:20], 600000, 32)
	if e != nil {
		return "", e
	}
	defer clear(key)
	block, e := aes.NewCipher(key)
	if e != nil {
		return "", e
	}
	g, e := cipher.NewGCM(block)
	if e != nil {
		return "", e
	}
	aad := []byte(path)
	if !fresh {
		if len(b) < 20+g.NonceSize() {
			return "", errors.New("corrupt vault")
		}
		plain, e := g.Open(nil, b[20:20+g.NonceSize()], b[20+g.NonceSize():], aad)
		if e != nil {
			return "", errors.New("vault unlock failed; existing file preserved")
		}
		defer clear(plain)
		return string(plain), nil
	}
	pulumiPass, e := secret("Pulumi passphrase (use the existing passphrase for an existing stack):")
	if e != nil || pulumiPass == "" {
		return "", errors.New("Pulumi passphrase required")
	}
	again, e := secret("Confirm Pulumi passphrase:")
	if e != nil || again != pulumiPass {
		return "", errors.New("passphrases differ")
	}
	nonce := make([]byte, g.NonceSize())
	if _, e = rand.Read(nonce); e != nil {
		return "", e
	}
	b = append(b, nonce...)
	b = g.Seal(b, nonce, []byte(pulumiPass), aad)
	return pulumiPass, atomicWrite(path, b)
}
func confirm() error {
	if runtime.GOOS != "windows" {
		probe := exec.Command("stty", "-g")
		probe.Stdin = os.Stdin
		if old, e := probe.Output(); e == nil {
			defer func() {
				restore := exec.Command("stty", strings.TrimSpace(string(old)))
				restore.Stdin = os.Stdin
				_ = restore.Run()
			}()
		}
	}

	fmt.Println("Deploy the previewed changes? [Y/n] Continuing in 5 seconds; press n or Ctrl+C to cancel.")
	exe, e := os.Executable()
	if e != nil {
		return e
	}
	c := exec.Command(exe, "--confirm-child")
	c.Stdin = os.Stdin
	if e = c.Start(); e != nil {
		return e
	}
	done := make(chan error, 1)
	go func() { done <- c.Wait() }()
	defer func() {
		if c.Process != nil {
			_ = c.Process.Kill()
		}
	}()
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt)
	defer signal.Stop(signals)
	for i := 5; i > 0; i-- {
		fmt.Printf("%d...\n", i)
		select {
		case e := <-done:
			if e != nil {
				return errors.New("deployment cancelled")
			}
			return nil
		case <-signals:
			return errors.New("deployment cancelled")
		case <-time.After(time.Second):
		}
	}
	_ = c.Process.Kill()
	e = <-done
	_ = e
	return nil
}
func build(ctx context.Context, root string, force bool) (string, error) {
	out := filepath.Join(root, "dist", "ocr-bench")
	if e := os.MkdirAll(out, 0700); e != nil {
		return "", e
	}
	version, e := output(ctx, "go", "version")
	if e != nil {
		return "", e
	}
	h := sha256.New()
	h.Write(version)
	buildEnv, _ := json.Marshal(map[string]string{"GOFLAGS": os.Getenv("GOFLAGS"), "GOTOOLCHAIN": os.Getenv("GOTOOLCHAIN"), "GOEXPERIMENT": os.Getenv("GOEXPERIMENT")})
	h.Write(buildEnv)
	h.Write([]byte("ocr-bench-build-v1/linux/arm64/cgo0/trimpath/lambda.norpc/zip"))
	e = filepath.WalkDir(root, func(path string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if d.IsDir() {
			if d.Name() == ".git" || d.Name() == "dist" || d.Name() == "deployment-reports" {
				return filepath.SkipDir
			}
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		if strings.HasSuffix(path, ".go") || d.Name() == "go.mod" || d.Name() == "go.sum" {
			b, e := os.ReadFile(path)
			if e != nil {
				return e
			}
			manifest, _ := json.Marshal([]string{filepath.ToSlash(rel), string(b)})
			h.Write(manifest)
		}
		return nil
	})
	if e != nil {
		return "", e
	}
	fingerprint := hex.EncodeToString(h.Sum(nil))
	archive := filepath.Join(out, "lambda.zip")
	marker := archive + ".json"
	var old struct{ Input, Artifact string }
	if b, e := os.ReadFile(marker); e == nil {
		_ = json.Unmarshal(b, &old)
	}
	if b, e := os.ReadFile(archive); e == nil && !force {
		sum := sha256.Sum256(b)
		if old.Input == fingerprint && old.Artifact == hex.EncodeToString(sum[:]) {
			fmt.Println("Reusing verified OCR Lambda archive")
			return archive, nil
		}
	}
	temp, e := os.MkdirTemp(out, "build-")
	if e != nil {
		return "", e
	}
	defer os.RemoveAll(temp)
	binary := filepath.Join(temp, "bootstrap")
	c := exec.CommandContext(ctx, "go", "build", "-trimpath", "-tags", "lambda.norpc", "-o", binary, "./cmd/ocr-bench-lambda")
	c.Dir = root
	c.Env = append(os.Environ(), "GOOS=linux", "GOARCH=arm64", "CGO_ENABLED=0")
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	if e = c.Run(); e != nil {
		return "", e
	}
	f, e := os.Create(filepath.Join(temp, "lambda.zip"))
	if e != nil {
		return "", e
	}
	z := zip.NewWriter(f)
	header := &zip.FileHeader{Name: "bootstrap", Method: zip.Deflate}
	header.SetMode(0755)
	w, e := z.CreateHeader(header)
	if e != nil {
		return "", e
	}
	b, e := os.ReadFile(binary)
	if e != nil {
		return "", e
	}
	if _, e = w.Write(b); e != nil {
		return "", e
	}
	if e = z.Close(); e != nil {
		return "", e
	}
	if e = f.Close(); e != nil {
		return "", e
	}
	b, e = os.ReadFile(f.Name())
	if e != nil {
		return "", e
	}
	sum := sha256.Sum256(b)
	if e = atomicWrite(archive, b); e != nil {
		return "", e
	}
	manifest, _ := json.Marshal(struct{ Input, Artifact string }{fingerprint, hex.EncodeToString(sum[:])})
	return archive, atomicWrite(marker, manifest)
}
func run() error {
	ctx := context.Background()
	fs := flag.NewFlagSet("deploy", flag.ContinueOnError)
	env := fs.String("environment", "", "isolated environment")
	action := fs.String("action", "preview", "setup, build, preview, deploy, status, destroy, github-setup")
	profile := fs.String("profile", "", "AWS SSO profile (omit in CI)")
	region := fs.String("region", "", "AWS region")
	account := fs.String("account", "", "expected AWS account")
	backend := fs.String("backend", "", "s3://bucket/dedicated-prefix")
	ci := fs.Bool("non-interactive", false, "complete explicit inputs required")
	approve := fs.Bool("approve", false, "explicit apply approval")
	init := fs.Bool("init-stack", false, "explicitly initialize a new stack")
	force := fs.Bool("force-rebuild", false, "ignore build cache")
	reconfigure := fs.Bool("reconfigure", false, "ask for saved settings again")
	memory := fs.Int("memory", 1024, "Lambda memory MB")
	githubRole := fs.String("github-role", "", "existing scoped OIDC deployment role")
	githubRepo := fs.String("github-repository", "", "verified repository")
	githubEnv := fs.String("github-environment", "", "existing protected environment")
	if e := fs.Parse(os.Args[1:]); e != nil {
		return e
	}
	if *action == "build" {
		root, e := os.Getwd()
		if e != nil {
			return e
		}
		_, e = build(ctx, root, *force)
		return e
	}
	if *env == "" {
		if *ci {
			return errors.New("--environment required")
		}
		*env = prompt("Environment (dev, qa, or custom test name)", "dev")
	}
	if !regexp.MustCompile(`^[a-z][a-z0-9-]{0,30}$`).MatchString(*env) || *env == "prod" {
		return errors.New("use a validated non-production benchmark environment")
	}
	root, e := os.Getwd()
	if e != nil {
		return e
	}
	if _, e = os.Stat(filepath.Join(root, "go.mod")); e != nil {
		return errors.New("run from repository root or use deploy launcher")
	}
	if *action == "build" {
		_, e := build(ctx, root, *force)
		return e
	}
	home, e := os.UserConfigDir()
	if e != nil {
		return e
	}
	private := filepath.Join(home, "attestra", "card-capture-ocr-bench", *env)
	if e = os.MkdirAll(private, 0700); e != nil {
		return e
	}
	lock, e := os.OpenFile(filepath.Join(private, "operation.lock"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return errors.New("operation lock exists; verify no active process before manually removing it")
	}
	lock.Close()
	defer os.Remove(lock.Name())
	saved := Settings{Version: 1, Environment: *env}
	path := filepath.Join(private, "settings.json")
	if b, e := os.ReadFile(path); e == nil {
		if json.Unmarshal(b, &saved) != nil || saved.Version != 1 || saved.Environment != *env {
			return errors.New("invalid settings")
		}
	}
	choose := func(v *string, prior, label, def string) error {
		if *v != "" {
			return nil
		}
		if !*ci && prior != "" && !*reconfigure {
			*v = prior
			fmt.Println(label + ": saved configuration")
			return nil
		}
		if *ci {
			return fmt.Errorf("%s required in CI", label)
		}
		if prior != "" {
			def = prior
		}
		*v = prompt(label, def)
		return nil
	}
	if !*ci {
		if e = choose(profile, saved.Profile, "AWS SSO profile", "default"); e != nil {
			return e
		}
		os.Setenv("AWS_PROFILE", *profile)
	}
	for _, v := range []struct {
		p               *string
		old, label, def string
	}{{region, saved.Region, "region", "us-east-2"}, {account, saved.Account, "account", ""}, {backend, saved.Backend, "backend", ""}} {
		if e = choose(v.p, v.old, v.label, v.def); e != nil {
			return e
		}
	}
	if !regexp.MustCompile(`^\d{12}$`).MatchString(*account) || !regexp.MustCompile(`^[a-z]{2}(-[a-z]+)+-\d$`).MatchString(*region) || !regexp.MustCompile(`^s3://[a-z0-9.-]+/[^?\s]+$`).MatchString(*backend) {
		return errors.New("account, region and dedicated S3 backend prefix required")
	}
	os.Setenv("AWS_REGION", *region)
	os.Setenv("AWS_DEFAULT_REGION", *region)
	for _, tool := range []string{"aws", "pulumi", "go"} {
		if _, e = exec.LookPath(tool); e != nil {
			return fmt.Errorf("install %s and add it to PATH", tool)
		}
	}
	awsVersion, err := output(ctx, "aws", "--version")
	if err != nil || !strings.Contains(string(awsVersion), "aws-cli/2.") {
		return errors.New("AWS CLI v2 required")
	}
	pulumiVersion, err := output(ctx, "pulumi", "version")
	if err != nil || !strings.HasPrefix(strings.TrimSpace(string(pulumiVersion)), "v3.") {
		return errors.New("Pulumi CLI v3 required")
	}
	identity, e := output(ctx, "aws", "sts", "get-caller-identity", "--output", "json")
	if e != nil && !*ci {
		if e = command(ctx, root, "aws", "sso", "login", "--profile", *profile); e != nil {
			return e
		}
		identity, e = output(ctx, "aws", "sts", "get-caller-identity", "--output", "json")
	}
	if e != nil {
		return e
	}
	var who struct{ Account, Arn string }
	if json.Unmarshal(identity, &who) != nil || who.Account != *account {
		return errors.New("AWS account differs from expected target")
	}
	fmt.Printf("Account %s • role %s • region %s • environment %s • stack ocr-%s • backend %s\n", *account, who.Arn, *region, *env, *env, *backend)
	// Verify the existing state bucket before using it. Bootstrap is deliberately separate.
	bucket := strings.Split(strings.TrimPrefix(*backend, "s3://"), "/")[0]
	if e = command(ctx, root, "aws", "s3api", "head-bucket", "--bucket", bucket, "--expected-bucket-owner", *account); e != nil {
		return e
	}
	for _, check := range []struct{ cmd, field, want string }{{"get-bucket-versioning", "Status", "Enabled"}, {"get-bucket-encryption", "ServerSideEncryptionConfiguration.Rules[0].ApplyServerSideEncryptionByDefault.SSEAlgorithm", ""}, {"get-public-access-block", "PublicAccessBlockConfiguration", ""}, {"get-bucket-location", "LocationConstraint", ""}} {
		b, e := output(ctx, "aws", "s3api", check.cmd, "--bucket", bucket, "--expected-bucket-owner", *account, "--query", check.field, "--output", "json")
		if e != nil {
			return e
		}
		if check.cmd == "get-bucket-versioning" && strings.TrimSpace(string(b)) != `"Enabled"` {
			return errors.New("state bucket must have versioning enabled")
		}
		if check.cmd == "get-bucket-encryption" && !strings.Contains(string(b), "AES256") && !strings.Contains(string(b), "aws:kms") {
			return errors.New("state bucket encryption unavailable")
		}
		if check.cmd == "get-public-access-block" {
			var fields map[string]bool
			if json.Unmarshal(b, &fields) != nil || !fields["BlockPublicAcls"] || !fields["BlockPublicPolicy"] || !fields["IgnorePublicAcls"] || !fields["RestrictPublicBuckets"] {
				return errors.New("state bucket must block all public access")
			}
		}
		if check.cmd == "get-bucket-location" {
			loc := strings.Trim(strings.TrimSpace(string(b)), `"`)
			if loc == "null" {
				loc = "us-east-1"
			}
			if loc != *region {
				return errors.New("state bucket region differs from selected region")
			}
		}
	}
	b, _ := json.MarshalIndent(Settings{1, *env, *profile, *region, *account, *backend}, "", "  ")
	if e = atomicWrite(path, b); e != nil {
		return e
	}

	if os.Getenv("PULUMI_CONFIG_PASSPHRASE") == "" {
		if *ci {
			return errors.New("PULUMI_CONFIG_PASSPHRASE secret required")
		}
		pass, e := vault(filepath.Join(private, "vault.bin"))
		if e != nil {
			return e
		}
		os.Setenv("PULUMI_CONFIG_PASSPHRASE", pass)
		defer os.Unsetenv("PULUMI_CONFIG_PASSPHRASE")
	}
	if *action == "github-setup" {
		return setupGitHub(ctx, Settings{1, *env, *profile, *region, *account, *backend}, *githubRole, *githubRepo, *githubEnv)
	}
	dir := filepath.Join(root, "infrastructure", "cmd", "ocr-bench")
	stack := "ocr-" + *env
	if e = command(ctx, dir, "pulumi", "login", *backend, "--non-interactive"); e != nil {
		return e
	}
	if *init {
		if e = command(ctx, dir, "pulumi", "stack", "init", stack, "--secrets-provider", "passphrase", "--non-interactive"); e != nil {
			return e
		}
	} else if e = command(ctx, dir, "pulumi", "stack", "select", stack, "--non-interactive"); e != nil {
		return errors.New("select failed; for a NEW stack explicitly pass --init-stack, otherwise recover backend/access")
	}
	// Restore authoritative encrypted configuration before setting nonsecret values.
	if e = command(ctx, dir, "pulumi", "config", "refresh", "--force", "--stack", stack, "--non-interactive"); e != nil {
		return e
	}
	if *action == "status" {
		return command(ctx, dir, "pulumi", "stack", "output", "--stack", stack, "--json")
	}
	if *action == "destroy" {
		if *ci || prompt("Type the complete stack name to destroy (no timeout)", "") != stack {
			return errors.New("destroy cancelled; protected bucket must be emptied/unprotected explicitly")
		}
		return command(ctx, dir, "pulumi", "destroy", "--stack", stack, "--yes")
	}
	if *action != "preview" && *action != "deploy" && *action != "setup" {
		return errors.New("unknown action")
	}
	archive, e := build(ctx, root, *force)
	if e != nil {
		return e
	}
	for _, pair := range [][2]string{{"aws:region", *region}, {"archive", archive}, {"memory", fmt.Sprint(*memory)}} {
		if e = command(ctx, dir, "pulumi", "config", "set", pair[0], pair[1], "--stack", stack, "--non-interactive"); e != nil {
			return e
		}
	}
	if *action == "setup" {
		return nil
	}
	plan := filepath.Join(private, "plan.json")
	defer os.Remove(plan)
	if e = command(ctx, dir, "pulumi", "preview", "--stack", stack, "--save-plan", plan, "--non-interactive"); e != nil {
		return e
	}
	if *action == "preview" {
		return nil
	}
	// Do not allow automatic approval of deletions/replacements.
	planBytes, e := os.ReadFile(plan)
	if e != nil {
		return e
	}
	if regexp.MustCompile(`"[^"\n]*(delete|replace)[^"\n]*"`).Match(planBytes) {
		if *ci {
			return errors.New("destructive plan requires separate operator review")
		}
		if prompt("Destructive plan: type stack name to approve", "") != stack {
			return errors.New("cancelled")
		}
	}
	if *ci {
		if !*approve {
			return errors.New("--approve required for noninteractive deploy")
		}
	} else if e = confirm(); e != nil {
		return e
	}
	if e = command(ctx, dir, "pulumi", "up", "--stack", stack, "--plan", plan, "--yes", "--non-interactive"); e != nil {
		return e
	}
	outputs, e := outputIn(ctx, dir, "pulumi", "stack", "output", "--stack", stack, "--json")
	if e != nil {
		return e
	}
	if e = atomicWrite(filepath.Join(private, "outputs.json"), outputs); e != nil {
		return e
	}
	fmt.Println(string(outputs))
	return nil
}
func outputIn(ctx context.Context, dir string, args ...string) ([]byte, error) {
	c := exec.CommandContext(ctx, args[0], args[1:]...)
	c.Dir = dir
	c.Stderr = os.Stderr
	return c.Output()
}
