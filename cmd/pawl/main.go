// Command pawl is the CLI entry point. Behavior is specified in SPEC.md.
package main

import (
	"os"

	pawl "github.com/tiangong-dev/pawl/internal/pawl"
)

func main() {
	os.Exit(pawl.RunCLI(os.Args[1:], os.Stdout, os.Stderr))
}
