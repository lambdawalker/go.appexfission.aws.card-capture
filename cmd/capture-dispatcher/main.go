package main

import (
	"context"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/lambdawalker/go.appexfission.aws.card-capture/awscapture"
	"github.com/lambdawalker/go.appexfission.aws.card-capture/capture"
	"log"
	"time"
)

func main() {
	_, store, transport, e := awscapture.New(context.Background())
	if e != nil {
		log.Fatal("capture initialization failed")
	}
	lambda.Start(func(ctx context.Context) error {
		return store.Pending(ctx, time.Now().Unix(), func(job capture.Job) error {
			return transport.Send(ctx, job)
		})
	})
}
