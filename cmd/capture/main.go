package main

import (
	"context"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/lambdawalker/go.appexfission.aws.card-capture/awscapture"
	"github.com/lambdawalker/go.appexfission.aws.card-capture/captureapi"
	"github.com/lambdawalker/go.appexfission.aws.card-capture/resetgate"
	"log"
	"os"
)

func main() {
	s, store, _, e := awscapture.New(context.Background())
	if e != nil {
		log.Fatal("capture initialization failed")
	}
	if os.Getenv("RESET_TABLE") == "" {
		log.Fatal("missing reset gate configuration")
	}
	gate := resetgate.Store{DB: store.DB, Table: os.Getenv("RESET_TABLE")}
	lambda.Start(captureapi.Handler{Service: s, CheckToken: gate.CheckToken}.Handle)
}
