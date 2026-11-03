// Command build-lambdas builds all three feature-owned Lambda archives.
package main

import (
	"context"
	"flag"
	"github.com/lambdawalker/go.appexfission.aws.card-capture/artifacts"
	"log"
)

func main() {
	out := flag.String("out", "dist", "archive output directory")
	flag.Parse()
	if err := artifacts.Build(context.Background(), *out); err != nil {
		log.Fatal(err)
	}
}
