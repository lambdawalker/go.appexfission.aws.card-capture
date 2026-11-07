package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

func authenticate(profile string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	args := []string{"sts", "get-caller-identity", "--output", "json", "--no-cli-pager"}
	identity, stderr, err := invokeAWS(ctx, args...)
	if err == nil {
		return identity, nil
	}

	message := strings.ToLower(string(stderr))
	expiredSSO := strings.Contains(message, "sso") &&
		(strings.Contains(message, "expired") ||
			strings.Contains(message, "refresh failed") ||
			strings.Contains(message, "otherwise invalid") ||
			strings.Contains(message, "does not exist"))
	if !expiredSSO {
		fmt.Fprint(os.Stderr, string(stderr))
		return nil, fmt.Errorf("verify AWS identity: %w", err)
	}

	if profile == "" {
		profile = os.Getenv("AWS_DEFAULT_PROFILE")
		if profile == "" {
			profile = "default"
		}
	}
	fmt.Fprintf(os.Stderr, "Renewing AWS SSO session for %q. Complete sign-in in your browser.\n", profile)
	login := exec.CommandContext(ctx, "aws", "sso", "login", "--profile", profile, "--no-cli-pager")
	login.Stdin, login.Stdout, login.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := login.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("AWS SSO sign-in for %q timed out", profile)
		}
		return nil, fmt.Errorf("AWS SSO sign-in for %q: %w", profile, err)
	}

	identity, err = aws(ctx, args...)
	if err != nil {
		return nil, fmt.Errorf("verify AWS identity after SSO sign-in: %w", err)
	}
	return identity, nil
}
