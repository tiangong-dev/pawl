package pawl

import (
	_ "embed"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// starterConfig is what `pawl init` scaffolds: a valid, non-empty config using
// only zero-dependency primitive builtins, so `pawl record` succeeds right away
// with no external tool installed. It lives as an embedded YAML asset rather
// than a Go string literal so its example debt-marker text is not itself
// scanned by pawl's own `**/*.go` dimensions.
//
//go:embed starter.yaml
var starterConfig string

// runInit scaffolds a starter config at configPath. It refuses to overwrite an
// existing file — a scaffolder that clobbered a hand-tuned config would be worse
// than useless. See SPEC.md § init.
func runInit(configPath string, stdout, stderr io.Writer) int {
	abs, err := filepath.Abs(configPath)
	if err != nil {
		fmt.Fprintf(stderr, "init: resolving %s: %v\n", configPath, err)
		return 2
	}
	if _, err := os.Stat(abs); err == nil {
		fmt.Fprintf(stderr, "init: %s already exists — edit it, or remove it first.\n", displayPath(abs))
		return 2
	} else if !os.IsNotExist(err) {
		fmt.Fprintf(stderr, "init: checking %s: %v\n", displayPath(abs), err)
		return 2
	}
	if err := os.WriteFile(abs, []byte(starterConfig), 0o644); err != nil {
		fmt.Fprintf(stderr, "init: writing %s: %v\n", displayPath(abs), err)
		return 2
	}
	fmt.Fprintf(stdout, "✅ wrote %s — edit it, then run `pawl record` to snapshot your baseline.\n", displayPath(abs))
	fmt.Fprintln(stdout, "   More ready-to-paste dimensions: RECIPES.md")
	return 0
}
