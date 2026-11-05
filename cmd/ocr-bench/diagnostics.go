package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/lambdawalker/go.appexfission.aws.card-capture/ocrbench"
	"os/exec"
	"regexp"
	"strings"
)

type diagnostic struct {
	Stage      string `json:"stage"`
	Code       string `json:"code"`
	Message    string `json:"message"`
	HTTPStatus int    `json:"http_status,omitempty"`
	RequestID  string `json:"request_id,omitempty"`
	LogGroup   string `json:"log_group,omitempty"`
}

func invokeAWS(ctx context.Context, args ...string) ([]byte, []byte, error) {
	c := exec.CommandContext(ctx, "aws", args...)
	var stderr strings.Builder
	c.Stderr = &stderr
	b, e := c.Output()
	return b, []byte(stderr.String()), e
}
func diagnoseInvoke(meta, body, stderr []byte, invokeErr error, fn string) (ocrbench.Result, *diagnostic) {
	d := &diagnostic{LogGroup: "/aws/lambda/" + fn}
	fail := func(stage, code, message string) (ocrbench.Result, *diagnostic) {
		d.Stage = stage
		d.Code = code
		d.Message = message
		return ocrbench.Result{Error: code}, d
	}
	if invokeErr != nil {
		code := "aws_cli_failed"
		message := "AWS CLI invocation failed. Check AWS credentials, region and Lambda permissions."
		match := regexp.MustCompile(`An error occurred \(([A-Za-z0-9.]+)\)`).FindSubmatch(stderr)
		if len(match) > 1 {
			switch string(match[1]) {
			case "AccessDeniedException":
				code = "access_denied"
				message = "The signed-in identity cannot invoke this Lambda. Check lambda:InvokeFunction permissions."
			case "ResourceNotFoundException":
				code = "function_not_found"
				message = "Lambda was not found in the selected region/account. Refresh saved deployment outputs."
			case "TooManyRequestsException":
				code = "throttled"
				message = "Lambda invocation was throttled. Check concurrency limits."
			case "ExpiredTokenException", "UnrecognizedClientException":
				code = "authentication_failed"
				message = "AWS credentials expired or are invalid. Run aws sso login for the selected profile."
			}
		}
		return fail("aws_cli", code, message)
	}
	var envelope struct {
		FunctionError   string
		StatusCode      int
		LogResult       string
		ExecutedVersion string
	}
	if json.Unmarshal(meta, &envelope) != nil {
		return fail("invoke_metadata", "invalid_invoke_metadata", "AWS CLI returned non-JSON or malformed invocation metadata; invocation must use --output json.")
	}
	d.HTTPStatus = envelope.StatusCode
	logs, _ := base64.StdEncoding.DecodeString(envelope.LogResult)
	defer clear(logs)
	if m := regexp.MustCompile(`RequestId: ([a-fA-F0-9-]{36})`).FindSubmatch(logs); len(m) > 1 {
		d.RequestID = string(m[1])
	}
	if envelope.StatusCode != 200 {
		return fail("lambda_service", "unexpected_invoke_status", fmt.Sprintf("Lambda invocation returned HTTP status %d.", envelope.StatusCode))
	}
	if envelope.FunctionError != "" {
		var runtimeError struct{ ErrorType, ErrorMessage string }
		_ = json.Unmarshal(body, &runtimeError)
		switch runtimeError.ErrorType {
		case "Runtime.InvalidEntrypoint":
			return fail("lambda_runtime", "invalid_entrypoint", "Lambda could not start bootstrap. Check that the archive contains an executable Linux ARM64 bootstrap at its root.")
		case "Runtime.ExitError":
			return fail("lambda_runtime", "runtime_exited", "Lambda exited before returning a result. Check the function's CloudWatch logs for startup failure.")
		case "Runtime.ImportModuleError":
			return fail("lambda_runtime", "runtime_import_failed", "Lambda could not load its handler. Check the deployed runtime/handler/package.")
		case "Sandbox.Timedout":
			return fail("lambda_runtime", "lambda_timeout", "Lambda exceeded its execution timeout. Check CloudWatch timing logs.")
		}
		if strings.Contains(runtimeError.ErrorMessage, "Task timed out") {
			return fail("lambda_runtime", "lambda_timeout", "Lambda exceeded its execution timeout.")
		}
		return fail("lambda_runtime", "function_error", "Lambda returned a runtime or handler error. Inspect the log group and request ID; raw error text is omitted from reports.")
	}
	var result ocrbench.Result
	// Require the application's success field; arbitrary valid JSON is not a benchmark result.
	var fields map[string]json.RawMessage
	if json.Unmarshal(body, &fields) != nil || fields["success"] == nil || json.Unmarshal(body, &result) != nil {
		return fail("lambda_response", "invalid_response", "Lambda returned an unreadable or unexpected benchmark response.")
	}
	if !result.Success {
		switch result.Error {
		case "read_failed":
			return result, &diagnostic{Stage: "s3_read", Code: result.Error, Message: "Lambda could not read the uploaded images. Check its S3 permissions and input bucket configuration.", LogGroup: d.LogGroup, RequestID: d.RequestID}
		case "extraction_failed":
			return result, &diagnostic{Stage: "extraction", Code: result.Error, Message: "Image preparation or Bedrock extraction failed. Check capture.inference_failed and capture.inference_response events in CloudWatch.", LogGroup: d.LogGroup, RequestID: d.RequestID}
		default:
			return fail("benchmark", "benchmark_failed", "The benchmark handler rejected the request; inspect CloudWatch logs.")
		}
	}
	return result, nil
}
