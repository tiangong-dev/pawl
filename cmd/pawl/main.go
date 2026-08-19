// Command pawl is the CLI entry point. The behavioral contract lives under
// spec/ (SPEC.md is the index).
package main

import (
	"os"

	pawl "github.com/tiangong-dev/pawl/internal/pawl"
)

func main() {
	os.Exit(pawl.RunCLI(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}
