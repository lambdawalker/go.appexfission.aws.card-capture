package infrastructure

import "github.com/lambdawalker/go.infra.composer/adapters/pulumi/awslabels"

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/apigatewayv2"
	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/cloudwatch"
	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/dynamodb"
	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/iam"
	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/lambda"
	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/s3"
	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/sqs"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

func DeployStorage(ctx *pulumi.Context, cfg Config, opts Options) (*Storage, error) {
	if err := ValidateStorage(cfg, opts); err != nil {
		return nil, err
	}
	var err error
	ctx, _, err = awslabels.Ensure(ctx, opts.Environment, cfg.Get("appOrigin"), "card-capture", "go.appexfission.aws.card-capture")
	if err != nil {
		return nil, err
	}

	bucket, e := s3.NewBucketV2(ctx, "id-evidence", &s3.BucketV2Args{ForceDestroy: pulumi.Bool(false)}, awslabels.Option(ctx))
	if e != nil {
		return nil, e
	}
	versioning, e := s3.NewBucketVersioningV2(ctx, "id-evidence-versions", &s3.BucketVersioningV2Args{Bucket: bucket.ID(), VersioningConfiguration: &s3.BucketVersioningV2VersioningConfigurationArgs{Status: pulumi.String("Enabled")}}, awslabels.Option(ctx))
	if e != nil {
		return nil, e
	}
	_, e = s3.NewBucketLifecycleConfigurationV2(ctx, "id-evidence-orphans", &s3.BucketLifecycleConfigurationV2Args{Bucket: bucket.ID(), Rules: s3.BucketLifecycleConfigurationV2RuleArray{&s3.BucketLifecycleConfigurationV2RuleArgs{Id: pulumi.String("orphan-safety"), Status: pulumi.String("Enabled"), Filter: &s3.BucketLifecycleConfigurationV2RuleFilterArgs{Prefix: pulumi.String("")}, Expiration: &s3.BucketLifecycleConfigurationV2RuleExpirationArgs{Days: pulumi.Int(10)}, NoncurrentVersionExpiration: &s3.BucketLifecycleConfigurationV2RuleNoncurrentVersionExpirationArgs{NoncurrentDays: pulumi.Int(10)}, AbortIncompleteMultipartUpload: &s3.BucketLifecycleConfigurationV2RuleAbortIncompleteMultipartUploadArgs{DaysAfterInitiation: pulumi.Int(1)}}}}, pulumi.DependsOn([]pulumi.Resource{versioning}), awslabels.Option(ctx))
	if e != nil {
		return nil, e
	}
	_, e = s3.NewBucketOwnershipControls(ctx, "id-evidence-owner", &s3.BucketOwnershipControlsArgs{Bucket: bucket.ID(), Rule: &s3.BucketOwnershipControlsRuleArgs{ObjectOwnership: pulumi.String("BucketOwnerEnforced")}}, awslabels.Option(ctx))
	if e != nil {
		return nil, e
	}
	_, e = s3.NewBucketPublicAccessBlock(ctx, "id-evidence-private", &s3.BucketPublicAccessBlockArgs{Bucket: bucket.ID(), BlockPublicAcls: pulumi.Bool(true), BlockPublicPolicy: pulumi.Bool(true), IgnorePublicAcls: pulumi.Bool(true), RestrictPublicBuckets: pulumi.Bool(true)}, awslabels.Option(ctx))
	if e != nil {
		return nil, e
	}
	_, e = s3.NewBucketServerSideEncryptionConfigurationV2(ctx, "id-evidence-encryption", &s3.BucketServerSideEncryptionConfigurationV2Args{Bucket: bucket.ID(), Rules: s3.BucketServerSideEncryptionConfigurationV2RuleArray{&s3.BucketServerSideEncryptionConfigurationV2RuleArgs{ApplyServerSideEncryptionByDefault: &s3.BucketServerSideEncryptionConfigurationV2RuleApplyServerSideEncryptionByDefaultArgs{SseAlgorithm: pulumi.String("AES256")}}}}, awslabels.Option(ctx))
	if e != nil {
		return nil, e
	}
	_, e = s3.NewBucketPolicy(ctx, "id-evidence-tls", &s3.BucketPolicyArgs{Bucket: bucket.ID(), Policy: pulumi.Sprintf(`{"Version":"2012-10-17","Statement":[{"Effect":"Deny","Principal":"*","Action":"s3:*","Resource":[%q,%q],"Condition":{"Bool":{"aws:SecureTransport":"false"}}}]}`, bucket.Arn, pulumi.Sprintf("%s/*", bucket.Arn))}, awslabels.Option(ctx))
	if e != nil {
		return nil, e
	}
	table, e := dynamodb.NewTable(ctx, "id-captures", &dynamodb.TableArgs{BillingMode: pulumi.String("PAY_PER_REQUEST"), HashKey: pulumi.String("id"), Attributes: dynamodb.TableAttributeArray{&dynamodb.TableAttributeArgs{Name: pulumi.String("id"), Type: pulumi.String("S")}, &dynamodb.TableAttributeArgs{Name: pulumi.String("work"), Type: pulumi.String("S")}, &dynamodb.TableAttributeArgs{Name: pulumi.String("due"), Type: pulumi.String("N")}}, GlobalSecondaryIndexes: dynamodb.TableGlobalSecondaryIndexArray{&dynamodb.TableGlobalSecondaryIndexArgs{Name: pulumi.String("work-due"), HashKey: pulumi.String("work"), RangeKey: pulumi.String("due"), ProjectionType: pulumi.String("ALL")}}, PointInTimeRecovery: &dynamodb.TablePointInTimeRecoveryArgs{Enabled: pulumi.Bool(true)}, ServerSideEncryption: &dynamodb.TableServerSideEncryptionArgs{Enabled: pulumi.Bool(true)}, Ttl: &dynamodb.TableTtlArgs{AttributeName: pulumi.String("ttl"), Enabled: pulumi.Bool(true)}}, awslabels.Option(ctx))
	if e != nil {
		return nil, e
	}
	dlq, e := sqs.NewQueue(ctx, "capture-validation-dlq", &sqs.QueueArgs{MessageRetentionSeconds: pulumi.Int(1209600), SqsManagedSseEnabled: pulumi.Bool(true)}, awslabels.Option(ctx))
	if e != nil {
		return nil, e
	}
	queue, e := sqs.NewQueue(ctx, "capture-validation", &sqs.QueueArgs{VisibilityTimeoutSeconds: pulumi.Int(720), SqsManagedSseEnabled: pulumi.Bool(true), RedrivePolicy: pulumi.Sprintf(`{"deadLetterTargetArn":%q,"maxReceiveCount":5}`, dlq.Arn)}, awslabels.Option(ctx))
	if e != nil {
		return nil, e
	}
	_, e = cloudwatch.NewMetricAlarm(ctx, "capture-dlq-alarm", &cloudwatch.MetricAlarmArgs{Namespace: pulumi.String("AWS/SQS"), MetricName: pulumi.String("ApproximateNumberOfMessagesVisible"), Dimensions: pulumi.StringMap{"QueueName": dlq.Name}, Statistic: pulumi.String("Maximum"), Period: pulumi.Int(60), EvaluationPeriods: pulumi.Int(1), Threshold: pulumi.Float64(0), ComparisonOperator: pulumi.String("GreaterThanThreshold"), TreatMissingData: pulumi.String("notBreaching")}, awslabels.Option(ctx))
	if e != nil {
		return nil, e
	}
	return &Storage{Table: table, Bucket: bucket, Queue: queue, DeadLetterQueue: dlq, Versioning: versioning}, nil
}
func Deploy(ctx *pulumi.Context, cfg Config, opts Options, storage *Storage) (*Deployment, error) {
	if err := Validate(cfg, opts); err != nil {
		return nil, err
	}
	if storage == nil {
		return nil, fmt.Errorf("capture storage is required")
	}
	ctx, _, err := awslabels.Ensure(ctx, opts.Environment, cfg.Get("appOrigin"), "card-capture", "go.appexfission.aws.card-capture")
	if err != nil {
		return nil, err
	}
	reserved, workerMax, err := parseCaptureConcurrency(cfg.Get("captureReservedConcurrency"), cfg.Get("captureWorkerMaxConcurrency"))
	if err != nil {
		return nil, err
	}
	reserved, err = captureCapacity(cfg, opts.Environment)
	if err != nil {
		return nil, err
	}
	ctx.Export("captureEffectiveReservedConcurrency", pulumi.Int(reserved))
	enabled := cfg.Get("captureEnabled") != "false"
	kind := "id_card"
	table, bucket, queue, dlq, versioning := storage.Table, storage.Bucket, storage.Queue, storage.DeadLetterQueue, storage.Versioning

	trust := pulumi.String(`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":{"Service":"lambda.amazonaws.com"},"Action":"sts:AssumeRole"}]}`)
	runtimeArn := opts.RuntimeTableARN
	runtimeRead := pulumi.Sprintf(`{"Effect":"Allow","Action":"dynamodb:GetItem","Resource":%q,"Condition":{"ForAllValues:StringEquals":{"dynamodb:LeadingKeys":[%q]}}}`, runtimeArn, "ENV#"+opts.Environment)
	dbStatement := pulumi.Sprintf(`{"Effect":"Allow","Action":["dynamodb:GetItem","dynamodb:PutItem"],"Resource":%q}`, table.Arn)
	workerDB := table.Arn.ApplyT(captureWorkerDatabaseStatements).(pulumi.StringOutput)
	policies := map[string]pulumi.StringOutput{
		"capture":            pulumi.Sprintf(`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":"dynamodb:GetItem","Resource":%q},%s,%s,{"Effect":"Allow","Action":["s3:PutObject","s3:GetObject"],"Resource":%q}]}`, opts.Integration["reset_table_arn"], dbStatement, runtimeRead, pulumi.Sprintf("%s/uploads/*", bucket.Arn)),
		"capture-worker":     pulumi.Sprintf(`{"Version":"2012-10-17","Statement":[%s,{"Effect":"Allow","Action":["s3:GetObjectVersion","s3:DeleteObjectVersion"],"Resource":%q},{"Effect":"Allow","Action":["s3:PutObject","s3:GetObjectVersion","s3:DeleteObjectVersion"],"Resource":%q},{"Effect":"Allow","Action":"s3:ListBucketVersions","Resource":%q,"Condition":{"StringLike":{"s3:prefix":["uploads/*","processed/*"]}}},{"Effect":"Allow","Action":["sqs:ReceiveMessage","sqs:DeleteMessage","sqs:GetQueueAttributes"],"Resource":%q}]}`, workerDB, pulumi.Sprintf("%s/uploads/*", bucket.Arn), pulumi.Sprintf("%s/processed/*", bucket.Arn), bucket.Arn, queue.Arn),
		"capture-dispatcher": pulumi.Sprintf(`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":"dynamodb:Query","Resource":%q},{"Effect":"Allow","Action":"sqs:SendMessage","Resource":%q}]}`, pulumi.Sprintf("%s/index/work-due", table.Arn), queue.Arn),
	}
	functions := map[string]*lambda.Function{}
	for _, name := range []string{"capture", "capture-worker", "capture-dispatcher"} {
		role, e := iam.NewRole(ctx, name+"-role", &iam.RoleArgs{AssumeRolePolicy: trust}, awslabels.Option(ctx))
		if e != nil {
			return nil, e
		}
		_, e = iam.NewRolePolicyAttachment(ctx, name+"-logs", &iam.RolePolicyAttachmentArgs{Role: role.Name, PolicyArn: pulumi.String("arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole")}, awslabels.Option(ctx))
		if e != nil {
			return nil, e
		}
		if name == "capture" || name == "capture-worker" {
			_, e = iam.NewRolePolicy(ctx, name+"-dispatch-grant", &iam.RolePolicyArgs{Role: role.ID(), Policy: pulumi.Sprintf(`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":"sqs:SendMessage","Resource":%q}]}`, queue.Arn)}, awslabels.Option(ctx))
			if e != nil {
				return nil, e
			}
		}
		grant, e := iam.NewRolePolicy(ctx, name+"-grant", &iam.RolePolicyArgs{Role: role.ID(), Policy: policies[name]}, awslabels.Option(ctx))
		if e != nil {
			return nil, e
		}
		if name == "capture-worker" {
			_, e = iam.NewRolePolicy(ctx, "capture-extraction-grant", &iam.RolePolicyArgs{Role: role.ID(), Policy: pulumi.String(`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":"bedrock-mantle:CreateInference","Resource":"*","Condition":{"StringEquals":{"bedrock-mantle:Model":"google.gemma-4-e2b"}}}]}`)}, awslabels.Option(ctx))
			if e != nil {
				return nil, e
			}
		}
		archive, _ := filepath.Abs(filepath.Join(opts.ArtifactDir, name+".zip"))
		timeout, memory := 25, 256
		if name != "capture" {
			timeout, memory = 90, 1024
		}
		fn, e := lambda.NewFunction(ctx, name, &lambda.FunctionArgs{Runtime: pulumi.String("provided.al2023"), Handler: pulumi.String("bootstrap"), Architectures: pulumi.StringArray{pulumi.String("arm64")}, Role: role.Arn, Code: pulumi.NewFileArchive(archive), Timeout: pulumi.Int(timeout), MemorySize: pulumi.Int(memory), ReservedConcurrentExecutions: pulumi.Int(reserved), Environment: &lambda.FunctionEnvironmentArgs{Variables: pulumi.StringMap{"RESET_TABLE": opts.Integration["reset_table_name"], "RUNTIME_TABLE": opts.RuntimeTableName, "ENVIRONMENT": pulumi.String(opts.Environment), "CAPTURE_TABLE": table.Name, "CAPTURE_BUCKET": bucket.ID(), "CAPTURE_QUEUE_URL": queue.Url, "CAPTURE_ENABLED": pulumi.String(fmt.Sprint(enabled)), "CAPTURE_DOCUMENT_TYPE": pulumi.String(kind), "CAPTURE_PURPOSE": pulumi.String(cfg.Get("capturePurpose")), "CAPTURE_JURISDICTION": pulumi.String(cfg.Get("captureJurisdiction"))}}}, pulumi.DependsOn([]pulumi.Resource{grant, versioning}), awslabels.Option(ctx))
		if e != nil {
			return nil, e
		}
		functions[name] = fn
	}
	_, e := lambda.NewEventSourceMapping(ctx, "capture-queue-worker", &lambda.EventSourceMappingArgs{EventSourceArn: queue.Arn, FunctionName: functions["capture-worker"].Arn, BatchSize: pulumi.Int(1), ScalingConfig: &lambda.EventSourceMappingScalingConfigArgs{MaximumConcurrency: pulumi.Int(workerMax)}, FunctionResponseTypes: pulumi.StringArray{pulumi.String("ReportBatchItemFailures")}}, awslabels.Option(ctx))
	if e != nil {
		return nil, e
	}
	rule, e := cloudwatch.NewEventRule(ctx, "capture-sweep", &cloudwatch.EventRuleArgs{ScheduleExpression: pulumi.String("rate(1 minute)")}, awslabels.Option(ctx))
	if e != nil {
		return nil, e
	}
	_, e = cloudwatch.NewEventTarget(ctx, "capture-sweep-target", &cloudwatch.EventTargetArgs{Rule: rule.Name, Arn: functions["capture-dispatcher"].Arn}, awslabels.Option(ctx))
	if e != nil {
		return nil, e
	}
	_, e = lambda.NewPermission(ctx, "capture-sweep-invoke", &lambda.PermissionArgs{Action: pulumi.String("lambda:InvokeFunction"), Function: functions["capture-dispatcher"].Name, Principal: pulumi.String("events.amazonaws.com"), SourceArn: rule.Arn}, awslabels.Option(ctx))
	if e != nil {
		return nil, e
	}
	authorizer, e := apigatewayv2.NewAuthorizer(ctx, "capture-jwt", &apigatewayv2.AuthorizerArgs{ApiId: opts.Integration["api_id"], AuthorizerType: pulumi.String("JWT"), IdentitySources: pulumi.StringArray{pulumi.String("$request.header.Authorization")}, JwtConfiguration: &apigatewayv2.AuthorizerJwtConfigurationArgs{Audiences: pulumi.StringArray{opts.Integration["client_id"]}, Issuer: opts.Integration["issuer"]}}, awslabels.Option(ctx))
	if e != nil {
		return nil, e
	}
	integration, e := apigatewayv2.NewIntegration(ctx, "capture-integration", &apigatewayv2.IntegrationArgs{ApiId: opts.Integration["api_id"], IntegrationType: pulumi.String("AWS_PROXY"), IntegrationUri: functions["capture"].InvokeArn, PayloadFormatVersion: pulumi.String("2.0")}, awslabels.Option(ctx))
	if e != nil {
		return nil, e
	}
	for i, route := range []string{"GET /onboarding/id/document-policy", "GET /onboarding/id/status", "POST /onboarding/id/captures", "GET /onboarding/id/captures/{capture_id}", "POST /onboarding/id/captures/{capture_id}/uploads", "POST /onboarding/id/captures/{capture_id}/finalize", "POST /onboarding/id/captures/{capture_id}/retry-finalization", "POST /onboarding/id/captures/{capture_id}/cancel", "POST /onboarding/id/captures/{capture_id}/review", "POST /onboarding/id/captures/{capture_id}/retake", "POST /onboarding/id/captures/{capture_id}/confirm-front"} {
		_, e = apigatewayv2.NewRoute(ctx, fmt.Sprintf("capture-route-%d", i), &apigatewayv2.RouteArgs{ApiId: opts.Integration["api_id"], RouteKey: pulumi.String(route), Target: pulumi.Sprintf("integrations/%s", integration.ID()), AuthorizationType: pulumi.String("JWT"), AuthorizerId: authorizer.ID()}, awslabels.Option(ctx))
		if e != nil {
			return nil, e
		}
	}
	_, e = lambda.NewPermission(ctx, "capture-api-invoke", &lambda.PermissionArgs{Action: pulumi.String("lambda:InvokeFunction"), Function: functions["capture"].Name, Principal: pulumi.String("apigateway.amazonaws.com"), SourceArn: pulumi.Sprintf("%s/*/*/onboarding/id/*", opts.Integration["api_execution_arn"])}, awslabels.Option(ctx))
	if e != nil {
		return nil, e
	}
	ctx.Export("captureEnabled", pulumi.Bool(enabled))
	ctx.Export("captureBucket", bucket.ID())
	ctx.Export("captureDeadLetterQueue", dlq.Url)
	return &Deployment{Storage: storage, Functions: functions}, nil
}

func captureCapacity(cfg Config, environment string) (int, error) {
	reserved, _, err := parseCaptureConcurrency(cfg.Get("captureReservedConcurrency"), cfg.Get("captureWorkerMaxConcurrency"))
	if err != nil {
		return 0, err
	}
	class := cfg.Get("environmentClass")
	if class != "" && class != "test" && class != "production" {
		return 0, fmt.Errorf("environmentClass must be test or production")
	}
	production := environment == "prod" || class == "production"
	if environment == "prod" && class == "test" {
		return 0, fmt.Errorf("prod requires production capacity")
	}
	if production && (cfg.Get("captureReservedConcurrency") == "" || cfg.Get("captureWorkerMaxConcurrency") == "" || reserved < 1) {
		return 0, fmt.Errorf("production requires explicit reserved and worker concurrency")
	}
	requested := reserved
	reserved, err = effectiveCaptureReservation(reserved, os.Getenv("ATTESTRA_CAPTURE_RESERVED_CONCURRENCY"))
	if err != nil {
		return 0, err
	}
	if production && (reserved != requested || os.Getenv("ATTESTRA_CAPTURE_RESERVED_CONCURRENCY") == "") {
		return 0, fmt.Errorf("production requires a successful quota check; deploy using the deployment tool or GitHub action")
	}

	return reserved, nil
}
