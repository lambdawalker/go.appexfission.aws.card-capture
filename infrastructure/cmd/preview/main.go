package main

import (
	"fmt"
	infra "github.com/lambdawalker/go.appexfission.aws.card-capture/infrastructure"
	"github.com/lambdawalker/go.infra.composer/adapters/pulumi/reportpreview"
	"os"
)

func main() {
	if err := reportpreview.RunCLI(os.Args[1:], "card-capture", "go.appexfission.aws.card-capture", infra.Preview, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
