package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
)

// setupGitHub reconciles only benchmark variables and explicitly delivers the unlocked secret.
// The deployment role/provider are supplied by the account operator, never broadened here.
func setupGitHub(ctx context.Context, s Settings, role, repository, environment, project string) error {
	if !strings.HasPrefix(role, "arn:aws:iam::"+s.Account+":role/") {
		return fmt.Errorf("--github-role must reference a deployment role in target account; see OIDC setup in docs/ocr-bench.md")
	}
	if repository != "lambdawalker/go.appexfission.aws.card-capture" || environment == "" {
		return fmt.Errorf("explicit --github-repository lambdawalker/go.appexfission.aws.card-capture and --github-environment required")
	}
	if _, e := output(ctx, "gh", "api", "user", "--jq", ".login"); e != nil {
		return fmt.Errorf("authenticate gh with repository environment administration permission: %w", e)
	}
	if _, e := output(ctx, "gh", "repo", "view", repository, "--json", "nameWithOwner"); e != nil {
		return e
	}
	base := "repos/" + repository + "/environments/" + environment
	// GET first; do not PUT over existing protection settings.
	if _, e := output(ctx, "gh", "api", base); e != nil {
		return fmt.Errorf("environment unavailable: create/verify it in repository Settings > Environments; setup incomplete: %w", e)
	}
	desired := map[string]string{"BENCH_PROJECT": project, "BENCH_ACCOUNT": s.Account, "BENCH_REGION": s.Region, "BENCH_BACKEND": s.Backend, "BENCH_ENVIRONMENT": s.Environment, "BENCH_ROLE_ARN": role}
	b, _ := json.Marshal(desired)
	sum := sha256.Sum256(b)
	marker := hex.EncodeToString(sum[:])
	currentBytes, e := output(ctx, "gh", "api", base+"/variables", "--paginate")
	if e != nil {
		return e
	}
	var current struct {
		Variables []struct{ Name, Value string }
	}
	if json.Unmarshal(currentBytes, &current) != nil {
		return fmt.Errorf("cannot inspect GitHub variables")
	}
	values := map[string]string{}
	for _, v := range current.Variables {
		values[v.Name] = v.Value
	}
	names := []string{}
	for name := range desired {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if values[name] == desired[name] {
			continue
		}
		method := "POST"
		url := base + "/variables"
		if _, ok := values[name]; ok {
			method = "PATCH"
			url += "/" + name
		}
		if _, e = output(ctx, "gh", "api", "--method", method, url, "-f", "name="+name, "-f", "value="+desired[name]); e != nil {
			return fmt.Errorf("variable reconciliation incomplete: %w", e)
		}
	}
	// Secret value cannot be read back. Explicit delivery each setup invocation is intentional.
	c := exec.CommandContext(ctx, "gh", "secret", "set", "PULUMI_CONFIG_PASSPHRASE", "--repo", repository, "--env", environment)
	c.Stdin = strings.NewReader(os.Getenv("PULUMI_CONFIG_PASSPHRASE"))
	c.Stderr = os.Stderr
	if e = c.Run(); e != nil {
		return fmt.Errorf("secret delivery incomplete: %w", e)
	}
	method := "POST"
	url := base + "/variables"
	if _, ok := values["DEPLOY_CONFIG_SHA256"]; ok {
		method = "PATCH"
		url += "/DEPLOY_CONFIG_SHA256"
	}
	if _, e = output(ctx, "gh", "api", "--method", method, url, "-f", "name=DEPLOY_CONFIG_SHA256", "-f", "value="+marker); e != nil {
		return e
	}
	fmt.Println("GitHub variables reconciled; Pulumi secret delivered (value cannot be verified). Existing protections preserved.")
	fmt.Printf("Verify OIDC trust subject repo:%s:environment:%s and audience sts.amazonaws.com before dispatch. Workflow: https://github.com/%s/actions/workflows/ocr-bench-deploy.yml\n", repository, environment, repository)
	return nil
}
