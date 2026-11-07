// Package infrastructure deploys capture-owned AWS resources separately from identity.
package infrastructure

import (
	"fmt"
	"github.com/lambdawalker/go.appexfission.aws.card-capture/artifacts"
	"github.com/lambdawalker/go.appexfission.aws.card-capture/runtimepolicy"
	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/dynamodb"
	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/lambda"
	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/s3"
	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/sqs"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type Config interface {
	Get(string) string
	Require(string) string
	RequireSecret(string) pulumi.StringOutput
	GetObject(string, interface{}) error
	GetBool(string) bool
}
type Options struct {
	ArtifactDir                       string
	Environment                       string
	RuntimeTableName, RuntimeTableARN pulumi.StringInput
	Integration                       map[string]pulumi.StringInput
}
type Storage struct {
	Table, ReportsTable    *dynamodb.Table
	Bucket                 *s3.BucketV2
	Queue, DeadLetterQueue *sqs.Queue
	Versioning             *s3.BucketVersioningV2
}
type Deployment struct {
	Storage   *Storage
	Functions map[string]*lambda.Function
}

// Validate checks all archives and configuration before registering runtime resources.
func Validate(cfg Config, opts Options) error {
	if err := ValidateStorage(cfg, opts); err != nil {
		return err
	}

	bindings := map[string]pulumi.StringInput{"runtime_table_name": opts.RuntimeTableName, "runtime_table_arn": opts.RuntimeTableARN}
	for _, k := range []string{"api_id", "api_execution_arn", "pool_id", "client_id", "issuer", "region", "reset_table_name", "reset_table_arn"} {
		bindings[k] = opts.Integration[k]
	}
	for k, v := range bindings {
		if v == nil {
			return fmt.Errorf("%s binding is required", k)
		}
		if x, ok := v.(pulumi.String); ok && strings.TrimSpace(string(x)) == "" {
			return fmt.Errorf("%s binding must not be empty", k)
		}
	}
	return nil
}

// ValidateStorage checks all capture configuration and archives without identity bindings.
// Applications call this before registering any native resources.
func ValidateStorage(cfg Config, opts Options) error {
	if cfg == nil {
		return fmt.Errorf("configuration reader is required")
	}
	if !runtimepolicy.ValidEnvironment(opts.Environment) {
		return fmt.Errorf("valid Environment is required")
	}

	if _, e := captureCapacity(cfg, opts.Environment); e != nil {
		return e
	}
	if v := cfg.Get("captureEnabled"); v != "" {
		if _, e := strconv.ParseBool(v); e != nil {
			return fmt.Errorf("captureEnabled must be boolean")
		}
	}
	if opts.ArtifactDir == "" {
		return fmt.Errorf("ArtifactDir is required")
	}
	for _, a := range artifacts.Lambdas() {
		p := filepath.Join(opts.ArtifactDir, a.Name+".zip")
		i, e := os.Stat(p)
		if e != nil {
			return fmt.Errorf("build Lambda archive %s before deployment: %w", p, e)
		}
		if !i.Mode().IsRegular() {
			return fmt.Errorf("Lambda archive %s must be regular", p)
		}
	}
	return nil
}
