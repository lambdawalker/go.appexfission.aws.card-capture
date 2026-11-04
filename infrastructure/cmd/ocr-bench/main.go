package main

import (
	"fmt"
	"github.com/lambdawalker/go.appexfission.aws.card-capture/awscapture"
	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/cloudwatch"
	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/iam"
	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/lambda"
	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/s3"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi/config"
	"os"
	"strings"
)

func main() {
	pulumi.Run(func(ctx *pulumi.Context) error {
		cfg := config.New(ctx, "")
		project := cfg.Get("deploymentProject")
		if project == "" {
			project = ctx.Project()
		}
		environment := cfg.Get("deploymentEnvironment")
		if environment == "" {
			environment = strings.TrimPrefix(ctx.Stack(), "ocr-")
		}
		identity := project + "-" + environment
		tags := func(purpose string) pulumi.StringMap {
			return pulumi.StringMap{"Name": pulumi.String(identity), "Project": pulumi.String(project), "Environment": pulumi.String(environment), "ManagedBy": pulumi.String("infra-composer"), "Component": pulumi.String("ocr-bench"), "Purpose": pulumi.String(purpose), "Description": pulumi.String(identity + ": " + purpose)}
		}
		archive := cfg.Require("archive")
		if _, e := os.Stat(archive); e != nil {
			return e
		}
		memory := cfg.GetInt("memory")
		if memory == 0 {
			memory = 1024
		}
		if memory < 128 || memory > 10240 {
			return fmt.Errorf("invalid Lambda memory")
		}
		bucket, e := s3.NewBucketV2(ctx, "ocr-bench-inputs", &s3.BucketV2Args{Tags: tags("Private front/back images for OCR speed tests"), ForceDestroy: pulumi.Bool(false)}, pulumi.Protect(true))
		if e != nil {
			return e
		}
		block, e := s3.NewBucketPublicAccessBlock(ctx, "ocr-bench-private", &s3.BucketPublicAccessBlockArgs{Bucket: bucket.ID(), BlockPublicAcls: pulumi.Bool(true), BlockPublicPolicy: pulumi.Bool(true), IgnorePublicAcls: pulumi.Bool(true), RestrictPublicBuckets: pulumi.Bool(true)})
		if e != nil {
			return e
		}
		_, e = s3.NewBucketServerSideEncryptionConfigurationV2(ctx, "ocr-bench-encryption", &s3.BucketServerSideEncryptionConfigurationV2Args{Bucket: bucket.ID(), Rules: s3.BucketServerSideEncryptionConfigurationV2RuleArray{&s3.BucketServerSideEncryptionConfigurationV2RuleArgs{ApplyServerSideEncryptionByDefault: &s3.BucketServerSideEncryptionConfigurationV2RuleApplyServerSideEncryptionByDefaultArgs{SseAlgorithm: pulumi.String("AES256")}}}})
		if e != nil {
			return e
		}
		_, e = s3.NewBucketLifecycleConfigurationV2(ctx, "ocr-bench-expiry", &s3.BucketLifecycleConfigurationV2Args{Bucket: bucket.ID(), Rules: s3.BucketLifecycleConfigurationV2RuleArray{&s3.BucketLifecycleConfigurationV2RuleArgs{Id: pulumi.String("test-inputs"), Status: pulumi.String("Enabled"), Filter: &s3.BucketLifecycleConfigurationV2RuleFilterArgs{Prefix: pulumi.String("inputs/")}, Expiration: &s3.BucketLifecycleConfigurationV2RuleExpirationArgs{Days: pulumi.Int(1)}}}})
		if e != nil {
			return e
		}
		_, e = s3.NewBucketPolicy(ctx, "ocr-bench-tls", &s3.BucketPolicyArgs{Bucket: bucket.ID(), Policy: pulumi.Sprintf(`{"Version":"2012-10-17","Statement":[{"Effect":"Deny","Principal":"*","Action":"s3:*","Resource":[%q,%q],"Condition":{"Bool":{"aws:SecureTransport":"false"}}}]}`, bucket.Arn, pulumi.Sprintf("%s/*", bucket.Arn))})
		if e != nil {
			return e
		}
		role, e := iam.NewRole(ctx, "ocr-bench-role", &iam.RoleArgs{Description: pulumi.String(identity + ": OCR benchmark execution role"), Tags: tags("OCR benchmark execution role"), AssumeRolePolicy: pulumi.String(`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":{"Service":"lambda.amazonaws.com"},"Action":"sts:AssumeRole"}]}`)})
		if e != nil {
			return e
		}
		logs, e := iam.NewRolePolicyAttachment(ctx, "ocr-bench-logs", &iam.RolePolicyAttachmentArgs{Role: role.Name, PolicyArn: pulumi.String("arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole")})
		if e != nil {
			return e
		}
		grant, e := iam.NewRolePolicy(ctx, "ocr-bench-inference", &iam.RolePolicyArgs{Role: role.ID(), Policy: pulumi.Sprintf(`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":"s3:GetObject","Resource":%q},{"Effect":"Allow","Action":"bedrock-mantle:CreateInference","Resource":"*","Condition":{"StringEquals":{"bedrock-mantle:Model":%q}}}]}`, pulumi.Sprintf("%s/inputs/*", bucket.Arn), awscapture.ExtractionModel)})
		if e != nil {
			return e
		}
		fn, e := lambda.NewFunction(ctx, "ocr-bench", &lambda.FunctionArgs{Description: pulumi.String(identity + ": isolated card OCR latency benchmark"), Tags: tags("Isolated card OCR latency benchmark"), Runtime: pulumi.String("provided.al2023"), Handler: pulumi.String("bootstrap"), Architectures: pulumi.StringArray{pulumi.String("arm64")}, Role: role.Arn, Code: pulumi.NewFileArchive(archive), MemorySize: pulumi.Int(memory), Timeout: pulumi.Int(90), Environment: &lambda.FunctionEnvironmentArgs{Variables: pulumi.StringMap{"BENCH_BUCKET": bucket.ID()}}}, pulumi.DependsOn([]pulumi.Resource{grant, logs, block}))
		if e != nil {
			return e
		}
		_, e = cloudwatch.NewLogGroup(ctx, "ocr-bench-log-retention", &cloudwatch.LogGroupArgs{Tags: tags("OCR benchmark timing and error logs"), Name: pulumi.Sprintf("/aws/lambda/%s", fn.Name), RetentionInDays: pulumi.Int(7)})
		if e != nil {
			return e
		}
		ctx.Export("deploymentIdentity", pulumi.String(identity))
		ctx.Export("resourceMetadata", pulumi.Map{
			"project": pulumi.String(project), "environment": pulumi.String(environment), "name": pulumi.String(identity),
			"bucket":   pulumi.String("Private test image storage; owns encryption, public-access block, TLS policy and expiry lifecycle"),
			"role":     pulumi.String("Execution role; owns log policy attachment and inline S3/Bedrock access policy"),
			"function": pulumi.String("IAM-invoked OCR benchmark; uses production extractor"),
			"logs":     pulumi.String("Seven-day timing/error logs"),
		})
		ctx.Export("bucket", bucket.ID())
		ctx.Export("function", fn.Name)
		ctx.Export("model", pulumi.String(awscapture.ExtractionModel))
		return nil
	})
}
