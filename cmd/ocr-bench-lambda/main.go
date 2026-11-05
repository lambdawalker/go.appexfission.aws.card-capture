package main

import (
	"context"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/lambdawalker/go.appexfission.aws.card-capture/awscapture"
	"github.com/lambdawalker/go.appexfission.aws.card-capture/ocrbench"
	"os"
	"sync/atomic"
)

func main() {
	cfg, e := config.LoadDefaultConfig(context.Background())
	if e != nil {
		panic("AWS configuration unavailable")
	}
	bucket := os.Getenv("BENCH_BUCKET")
	if bucket == "" {
		panic("BENCH_BUCKET required")
	}
	transport := &awscapture.Transport{Config: cfg, Bucket: bucket}
	read := transport.ReadInput

	var warm atomic.Bool
	lambda.Start(func(ctx context.Context, r ocrbench.Request) (ocrbench.Result, error) {
		cold := !warm.Swap(true)
		var prepareMS, inferenceMS int64
		extractor := awscapture.BedrockExtractor{Transport: &awscapture.Transport{Config: cfg}, Observe: func(stage string, ms int64) {
			if stage == "prepare" {
				prepareMS = ms
			}
			if stage == "inference" {
				inferenceMS = ms
			}
		}}
		result := ocrbench.Run(ctx, r, read, extractor)
		result.PrepareMS = prepareMS
		result.InferenceMS = inferenceMS
		result.ColdStart = cold
		return result, nil
	})
}
