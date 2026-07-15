package pawl

import (
	_ "embed"
	"errors"
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
	// Atomic create-exclusive rather than stat-then-write: O_CREATE|O_EXCL fails
	// if anything already exists at the path (including a symlink, dangling or
	// not), which closes both the TOCTOU race and the "write through a symlink to
	// an outside file" hole a stat pre-check would leave open.
	f, err := os.OpenFile(abs, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			fmt.Fprintf(stderr, "init: %s already exists — edit it, or remove it first.\n", displayPath(abs))
			return 2
		}
		fmt.Fprintf(stderr, "init: writing %s: %v\n", displayPath(abs), err)
		return 2
	}
	if _, err := f.WriteString(starterConfig); err != nil {
		f.Close()
		fmt.Fprintf(stderr, "init: writing %s: %v\n", displayPath(abs), err)
		return 2
	}
	if err := f.Close(); err != nil {
		fmt.Fprintf(stderr, "init: writing %s: %v\n", displayPath(abs), err)
		return 2
	}
	fmt.Fprintf(stdout, "✅ wrote %s — edit it, then run `pawl record` to snapshot your baseline.\n", displayPath(abs))
	fmt.Fprintln(stdout, "   More ready-to-paste dimensions: RECIPES.md")
	return 0
}
