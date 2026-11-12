// Command atlas-ai is the Atlas AI command line interface.
package main

import (
	"os"
	"runtime"

	"github.com/FelipePepe/atlas-ai/internal/cli"
)

// Version is overridden at link time for releases.
var Version = "dev"

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr, cli.Env{
		Version: Version,
		GOOS:    runtime.GOOS,
		GOARCH:  runtime.GOARCH,
		Getenv:  os.Getenv,
		Stdin:   os.Stdin,
	}))
}
