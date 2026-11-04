package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"github.com/aws/aws-lambda-go/lambda"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/lambdawalker/go.appexfission.aws.card-capture/awscapture"
	"github.com/lambdawalker/go.appexfission.aws.card-capture/capture"
	"github.com/lambdawalker/go.appexfission.aws.card-capture/ocrbench"
	"io"
	"net/http"
	"net/url"
	"os"
	"sync/atomic"
	"time"
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
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	read := func(ctx context.Context, key string) ([]byte, error) {
		target := fmt.Sprintf("https://%s.s3.%s.amazonaws.com/%s", bucket, cfg.Region, url.PathEscape(key))
		req, e := http.NewRequestWithContext(ctx, "GET", target, nil)
		if e != nil {
			return nil, ocrbench.ErrInput
		}
		creds, e := cfg.Credentials.Retrieve(ctx)
		if e != nil {
			return nil, ocrbench.ErrInput
		}
		sum := sha256.Sum256(nil)
		if v4.NewSigner().SignHTTP(ctx, creds, req, hex.EncodeToString(sum[:]), "s3", cfg.Region, time.Now()) != nil {
			return nil, ocrbench.ErrInput
		}
		resp, e := client.Do(req)
		if e != nil {
			return nil, ocrbench.ErrInput
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			return nil, ocrbench.ErrInput
		}
		b, e := io.ReadAll(io.LimitReader(resp.Body, capture.MaxBytes+1))
		if e != nil || int64(len(b)) > capture.MaxBytes {
			clear(b)
			return nil, ocrbench.ErrInput
		}
		return b, nil
	}

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
