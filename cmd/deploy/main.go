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
	if len(os.Args) > 1 && strings.HasPrefix(os.Args[1], "--confirm-child") {
		os.Exit(readApproval(os.Args[1] != "--confirm-child-no"))
	}

	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, "\n  "+color("Stopped", "1;31")+"  "+e.Error())
		os.Exit(1)
	}
}

// The child owns terminal mode and input; the parent can cancel it without leaked readers.
func readApproval(defaultYes bool) int {
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
			raw := exec.Command("stty", "-echo", "-icanon", "min", "1", "time", "0")
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
	if !defaultYes && (value == '\n' || value == '\r') {
		return 3
	}
	if value == 'n' || value == 'N' {
		return 3
	}
	if value == 'y' || value == 'Y' || value == '\n' || value == '\r' {
		return 0
	}
	return 2
}

func prompt(label, def string) string {
	fmt.Printf("  %s %s: ", color(label, "1"), color("["+def+"]", "2"))
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
	c := toolCommand(ctx, args...)
	c.Dir = dir
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	c.Stdin = os.Stdin
	return c.Run()
}
func output(ctx context.Context, args ...string) ([]byte, error) {
	c := toolCommand(ctx, args...)
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
	heading("Deploy the previewed changes? [Y/n]")
	yes, err := timedChoice(true)
	if err != nil {
		return err
	}
	if !yes {
		return errors.New("deployment cancelled")
	}
	return nil
}

var countdownStep = time.Second

func timedChoice(defaultYes bool) (bool, error) {
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

	terminal := interactiveOutput()
	defer finishCountdown(os.Stdout, terminal)
	exe, e := os.Executable()
	if e != nil {
		return false, e
	}
	childArg := "--confirm-child"
	if !defaultYes {
		childArg = "--confirm-child-no"
	}
	c := exec.Command(exe, childArg)
	c.Stdin = os.Stdin
	if e = c.Start(); e != nil {
		return false, e
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
		writeCountdown(os.Stdout, i, defaultYes, terminal)
		select {
		case e := <-done:
			if e != nil {
				if exit, ok := e.(*exec.ExitError); ok && exit.ExitCode() == 3 {
					return false, nil
				}
				return false, errors.New("input closed or cancelled")
			}
			return true, nil
		case <-signals:
			return false, errors.New("deployment cancelled")
		case <-time.After(countdownStep):
		}
	}
	_ = c.Process.Kill()
	e = <-done
	_ = e
	return defaultYes, nil
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
			note("Build: reusing verified OCR Lambda archive")
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
func run() (runErr error) {
	ctx := context.Background()
	fs := flag.NewFlagSet("deploy", flag.ContinueOnError)
	env := fs.String("environment", "", "isolated environment")
	action := fs.String("action", "", "setup, build, preview, deploy, status, destroy, github-setup, report (default: deploy interactively, preview in CI)")
	profile := fs.String("profile", "", "AWS SSO profile (omit in CI)")
	region := fs.String("region", "", "AWS region")
	account := fs.String("account", "", "expected AWS account")
	backend := fs.String("backend", "", "s3://bucket/dedicated-prefix")
	ci := fs.Bool("non-interactive", false, "complete explicit inputs required")
	approve := fs.Bool("approve", false, "explicit apply approval")
	project := fs.String("project", "", "deployment project name (saved after first run)")
	_ = fs.Bool("init-stack", false, "deprecated: stacks are detected automatically")
	force := fs.Bool("force-rebuild", false, "ignore build cache")
	reconfigure := fs.Bool("reconfigure", false, "ask for saved settings again")
	refreshReport := fs.Bool("refresh-report", false, "verify report presence with read-only refresh")
	memory := fs.Int("memory", 1024, "Lambda memory MB")
	githubRole := fs.String("github-role", "", "existing scoped OIDC deployment role")
	githubRepo := fs.String("github-repository", "", "verified repository")
	githubEnv := fs.String("github-environment", "", "existing protected environment")
	if e := fs.Parse(os.Args[1:]); e != nil {
		return e
	}
	*action = defaultAction(*action, *ci)
	if *refreshReport && *action != "report" {
		return errors.New("--refresh-report requires --action report")
	}
	allowedActions := map[string]bool{"setup": true, "build": true, "preview": true, "deploy": true, "status": true, "destroy": true, "github-setup": true, "report": true}
	if !allowedActions[*action] {
		return errors.New("unknown action")
	}
	banner(*action, *env)
	if *action == "build" {
		root, e := os.Getwd()
		if e != nil {
			return e
		}
		_, e = build(ctx, root, *force)
		return e
	}
	root, e := os.Getwd()
	if e != nil {
		return e
	}
	projectName, local, central, e := projectPaths(root, *project, *ci)
	if e != nil {
		return e
	}
	detail("Project", projectName)
	var projectFlags struct{ Environment string }
	projectFlagsPath := filepath.Join(local, "flags.json")
	if b, err := os.ReadFile(projectFlagsPath); err == nil {
		if json.Unmarshal(b, &projectFlags) != nil {
			return errors.New("invalid .deploy/flags.json")
		}
		if *env == "" {
			*env = projectFlags.Environment
		}
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
	if _, e = os.Stat(filepath.Join(root, "go.mod")); e != nil {
		return errors.New("run from repository root or use deploy launcher")
	}
	if *action == "build" {
		_, e := build(ctx, root, *force)
		return e
	}
	projectFlags.Environment = *env
	flagsBytes, _ := json.MarshalIndent(projectFlags, "", "  ")
	if e = atomicWrite(projectFlagsPath, flagsBytes); e != nil {
		return e
	}
	private := filepath.Join(central, "environments", *env)
	localEnv := filepath.Join(local, "environments", *env)
	if e = os.MkdirAll(localEnv, 0700); e != nil {
		return e
	}

	if e = os.MkdirAll(private, 0700); e != nil {
		return e
	}
	lock, e := os.OpenFile(filepath.Join(private, "operation.lock"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return errors.New("operation lock exists; verify no active process before manually removing it")
	}
	lock.Close()
	defer os.Remove(lock.Name())
	var deploymentFlags struct{ Memory int }
	memoryExplicit := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "memory" {
			memoryExplicit = true
		}
	})
	deploymentFlagsPath := filepath.Join(private, "flags.json")
	if b, err := os.ReadFile(deploymentFlagsPath); err == nil {
		if json.Unmarshal(b, &deploymentFlags) != nil {
			return errors.New("invalid deployment flags")
		}
		if !memoryExplicit && deploymentFlags.Memory >= 128 && deploymentFlags.Memory <= 10240 {
			*memory = deploymentFlags.Memory
		}
	}
	if *memory < 128 || *memory > 10240 {
		return errors.New("memory must be 128..10240 MB")
	}
	deploymentFlags.Memory = *memory
	fb, _ := json.MarshalIndent(deploymentFlags, "", "  ")
	if e = atomicWrite(deploymentFlagsPath, fb); e != nil {
		return e
	}
	if e = atomicWrite(filepath.Join(localEnv, "flags.json"), fb); e != nil {
		return e
	}
	saved := Settings{Version: 1, Environment: *env}
	path := filepath.Join(private, "settings.json")
	legacyHome, _ := os.UserConfigDir()
	legacy := filepath.Join(legacyHome, "attestra", "card-capture-ocr-bench", *env)
	if _, err := os.Stat(path); os.IsNotExist(err) && projectName == "card-capture-ocr-bench" {
		if previous, err := os.ReadFile(filepath.Join(legacy, "settings.json")); err == nil {
			if e = atomicWrite(path, previous); e != nil {
				return e
			}
			note("Imported previous deployment settings")
		}
	}

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
			note(label + ": using saved setting")
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
	}{{region, saved.Region, "region", "us-east-2"}} {
		if e = choose(v.p, v.old, v.label, v.def); e != nil {
			return e
		}
	}
	if !regexp.MustCompile(`^[a-z]{2}(-[a-z]+)+-\d$`).MatchString(*region) {
		return errors.New("valid region required")
	}
	if *ci && (*account == "" || *backend == "") {
		return errors.New("--account and --backend required in CI")
	}
	if *account == "" && !*reconfigure {
		*account = saved.Account
	}
	if *backend == "" && !*reconfigure {
		*backend = saved.Backend
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
	if json.Unmarshal(identity, &who) != nil || !regexp.MustCompile(`^\d{12}$`).MatchString(who.Account) {
		return errors.New("invalid AWS identity")
	}
	if *account != "" && *account != who.Account {
		return errors.New("AWS account differs from saved/explicit target; use --reconfigure to deliberately change it")
	}
	*account = who.Account
	heading("AWS account")
	detail("Account", *account)
	detail("Identity", who.Arn)
	detail("Region", *region)
	detail("Environment", *env)
	detail("Stack", "ocr-"+*env)

	if !*ci {
		heading("Proceed with this AWS account? [Y/n]")
		yes, err := timedChoice(true)
		if err != nil {
			return err
		}
		if !yes {
			return errors.New("account selection cancelled")
		}
	}
	if *backend == "" {
		if *ci {
			return errors.New("--backend required in CI")
		}
		heading("Do you have an S3 bucket for deployment state? [y/N]")
		note("No: create a private, encrypted, versioned bucket automatically.")
		existing, err := timedChoice(false)
		if err != nil {
			return err
		}
		if existing {
			name := prompt("S3 bucket name", "")
			*backend = "s3://" + name + "/infra-composer/" + projectName
		} else {
			if *action == "status" || *action == "destroy" || *action == "report" {
				return errors.New("select an existing state bucket for status/destroy")
			}
			name, err := bootstrapBucket(ctx, private, *account, *region)
			if err != nil {
				return err
			}
			*backend = "s3://" + name + "/infra-composer/" + projectName
		}
	}
	if !regexp.MustCompile(`^s3://[a-z0-9.-]+/[^?\s]+$`).MatchString(*backend) {
		return errors.New("valid S3 state location required")
	}
	heading("Deployment state")
	detail("S3 location", *backend)

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
	// Recover a legacy bootstrap receipt only when it matches the verified target.
	if _, err := os.Stat(filepath.Join(private, "state-bucket.json")); os.IsNotExist(err) && projectName == "card-capture-ocr-bench" && (*action == "setup" || *action == "preview" || *action == "deploy") {
		if prior, err := os.ReadFile(filepath.Join(legacy, "state-bucket.json")); err == nil {
			var r bucketReceipt
			if json.Unmarshal(prior, &r) == nil && r.Created && r.Name == bucket && r.Account == *account && r.Region == *region {
				if e = atomicWrite(filepath.Join(private, "state-bucket.json"), prior); e != nil {
					return e
				}
			}
		}
	}
	// Only annotate a state bucket this wizard actually bootstrapped; preserve shared bucket ownership.
	if receiptBytes, err := os.ReadFile(filepath.Join(private, "state-bucket.json")); err == nil {
		var receipt bucketReceipt
		if json.Unmarshal(receiptBytes, &receipt) != nil {
			return errors.New("invalid state bucket receipt")
		}
		if (*action == "setup" || *action == "preview" || *action == "deploy") && receipt.Created && receipt.Name == bucket && receipt.Account == *account && receipt.Region == *region {
			if e = tagManagedStateBucket(ctx, bucket, *account, projectName, *env); e != nil {
				return e
			}
		}
	}
	b, _ := json.MarshalIndent(Settings{1, *env, *profile, *region, *account, *backend}, "", "  ")
	if e = atomicWrite(path, b); e != nil {
		return e
	}
	if e = atomicWrite(filepath.Join(localEnv, "settings.json"), b); e != nil {
		return e
	}

	if os.Getenv("PULUMI_CONFIG_PASSPHRASE") == "" {
		if *ci {
			return errors.New("PULUMI_CONFIG_PASSPHRASE secret required")
		}
		vaultPath := filepath.Join(localEnv, "vault.bin")
		legacyVault := ""
		if projectName == "card-capture-ocr-bench" {
			legacyVault = filepath.Join(legacy, "vault.bin")
		}
		pass, e := loadProjectVault(vaultPath, legacyVault)
		if e != nil {
			return e
		}
		os.Setenv("PULUMI_CONFIG_PASSPHRASE", pass)
		defer os.Unsetenv("PULUMI_CONFIG_PASSPHRASE")
	}
	if *action == "github-setup" {
		return setupGitHub(ctx, Settings{1, *env, *profile, *region, *account, *backend}, *githubRole, *githubRepo, *githubEnv, projectName)
	}
	dir := filepath.Join(root, "infrastructure", "cmd", "ocr-bench")
	stack := "ocr-" + *env
	if e = command(ctx, dir, "pulumi", "login", *backend, "--non-interactive"); e != nil {
		return e
	}
	if e = ensureStack(ctx, dir, stack, filepath.Join(private, "stack.json"), stackRecord{Account: *account, Region: *region, Backend: *backend}, *action == "setup" || *action == "preview" || *action == "deploy",
		func(args ...string) error { return command(ctx, dir, append([]string{"pulumi"}, args...)...) },
		func() ([]byte, error) {
			return outputIn(ctx, dir, "pulumi", "stack", "ls", "--json", "--non-interactive")
		}); e != nil {
		return e
	}

	// Restore authoritative encrypted configuration before setting nonsecret values.
	if e = restoreStackConfig(func() ([]byte, error) {
		cmd := exec.CommandContext(ctx, "pulumi", "config", "refresh", "--force", "--stack", stack, "--non-interactive")
		cmd.Dir = dir
		return cmd.CombinedOutput()
	}); e != nil {
		return e
	}
	var activeFeatureReport *featureReport
	if *action == "report" || *action == "preview" || *action == "deploy" {
		attempt := newFeatureReport(ctx, root, dir, stack, private, local, Settings{1, *env, *profile, *region, *account, *backend}, projectName, *action)
		if *action == "report" {
			return attempt.finish(ctx, nil, *refreshReport)
		}
		eventFile, err := os.CreateTemp(private, "preview-events-*.json")
		if err != nil {
			return err
		}
		attempt.previewLog = eventFile.Name()
		eventFile.Close()
		defer func() {
			if err := attempt.finish(ctx, runErr, attempt.applied); err != nil {
				fmt.Fprintln(os.Stderr, "Feature report could not be saved:", err)
			}
		}()
		activeFeatureReport = attempt

	}
	if *action == "status" {
		return command(ctx, dir, "pulumi", "stack", "output", "--stack", stack, "--json")
	}
	if *action == "destroy" {
		if *ci {
			return errors.New("interactive teardown confirmation required")
		}
		heading("Destroy this stack? [Y/n]")
		detail("Stack", stack)
		note("This removes stack resources; protected resources can still block deletion.")
		yes, err := timedChoice(true)
		if err != nil {
			return err
		}
		if !yes {
			return errors.New("destroy cancelled")
		}
		return command(ctx, dir, "pulumi", "destroy", "--stack", stack, "--yes")
	}

	if *action != "preview" && *action != "deploy" && *action != "setup" {
		return errors.New("unknown action")
	}
	heading("Build OCR Lambda")
	archive, e := build(ctx, root, *force)
	if e != nil {
		return e
	}
	for _, pair := range [][2]string{{"aws:region", *region}, {"archive", archive}, {"memory", fmt.Sprint(*memory)}, {"deploymentProject", projectName}, {"deploymentEnvironment", *env}} {
		if e = command(ctx, dir, "pulumi", "config", "set", pair[0], pair[1], "--stack", stack, "--non-interactive"); e != nil {
			return e
		}
	}
	if *action == "setup" {
		return nil
	}
	plan := filepath.Join(private, "plan.json")
	defer os.Remove(plan)
	heading("Preview changes")
	if e = command(ctx, dir, "pulumi", "preview", "--stack", stack, "--save-plan", plan, "--event-log", activeFeatureReport.previewLog, "--non-interactive"); e != nil {
		return e
	}
	if *action == "preview" {
		return nil
	}
	planBytes, e := os.ReadFile(plan)
	if e != nil {
		return e
	}
	destructive, e := destructivePlan(planBytes)
	if e != nil {
		return e
	}
	if destructive {
		if *ci {
			return errors.New("destructive plan requires separate operator review")
		}
		heading("This plan includes deletion or replacement")
		detail("Stack", stack)
		note("The following default-yes countdown approves these previewed operations.")
	}

	if *ci {
		if !*approve {
			return errors.New("--approve required for noninteractive deploy")
		}
	} else if e = confirm(); e != nil {
		return e
	}
	activeFeatureReport.applied = true
	heading("Apply previewed changes")
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
	heading("Deployment complete")
	fmt.Println(string(outputs))
	return nil
}
func outputIn(ctx context.Context, dir string, args ...string) ([]byte, error) {
	c := toolCommand(ctx, args...)
	c.Dir = dir
	c.Stderr = os.Stderr
	return c.Output()
}
