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
)

func main() {
	pulumi.Run(func(ctx *pulumi.Context) error {
		cfg := config.New(ctx, "")
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
		bucket, e := s3.NewBucketV2(ctx, "ocr-bench-inputs", &s3.BucketV2Args{ForceDestroy: pulumi.Bool(false)}, pulumi.Protect(true))
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
		role, e := iam.NewRole(ctx, "ocr-bench-role", &iam.RoleArgs{AssumeRolePolicy: pulumi.String(`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":{"Service":"lambda.amazonaws.com"},"Action":"sts:AssumeRole"}]}`)})
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
		fn, e := lambda.NewFunction(ctx, "ocr-bench", &lambda.FunctionArgs{Runtime: pulumi.String("provided.al2023"), Handler: pulumi.String("bootstrap"), Architectures: pulumi.StringArray{pulumi.String("arm64")}, Role: role.Arn, Code: pulumi.NewFileArchive(archive), MemorySize: pulumi.Int(memory), Timeout: pulumi.Int(90), Environment: &lambda.FunctionEnvironmentArgs{Variables: pulumi.StringMap{"BENCH_BUCKET": bucket.ID()}}}, pulumi.DependsOn([]pulumi.Resource{grant, logs, block}))
		if e != nil {
			return e
		}
		_, e = cloudwatch.NewLogGroup(ctx, "ocr-bench-log-retention", &cloudwatch.LogGroupArgs{Name: pulumi.Sprintf("/aws/lambda/%s", fn.Name), RetentionInDays: pulumi.Int(7)})
		if e != nil {
			return e
		}
		ctx.Export("bucket", bucket.ID())
		ctx.Export("function", fn.Name)
		ctx.Export("model", pulumi.String(awscapture.ExtractionModel))
		return nil
	})
}
