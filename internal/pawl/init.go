package pawl

import (
	_ "embed"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// starterConfig is what `pawl init` scaffolds: a valid, non-empty config using
// only zero-dependency primitive builtins, so `pawl record` succeeds right away
// with no external tool installed. It lives as an embedded YAML asset rather
// than a Go string literal so its example debt-marker text is not itself
// scanned by pawl's own `**/*.go` dimensions.
//
//go:embed starter.yaml
var starterConfig string

// detectedStarterConfig is the same starter with placeholders for the include
// globs, the shared exclude globs, and the detected stack labels. It is used
// when marker files identify the project's stacks; starter.yaml is the fallback
// for a directory with no marker. See init_detect.go.
//
//go:embed starter_detected.yaml
var detectedStarterConfig string

// runInit scaffolds a starter config at configPath. It refuses to overwrite an
// existing file — a scaffolder that clobbered a hand-tuned config would be worse
// than useless. See spec/commands/init.md.
func runInit(configPath string, stdout, stderr io.Writer) int {
	abs, err := filepath.Abs(configPath)
	if err != nil {
		fmt.Fprintf(stderr, "init: resolving %s: %v\n", configPath, err)
		return 2
	}
	// Detection reads marker files next to the config, so the generated include
	// and exclude globs resolve the way every other config glob does: relative to
	// the config file's directory.
	include, exclude, detected := detectProject(filepath.Dir(abs))
	content := starterConfig
	if len(detected) > 0 {
		content = renderDetectedConfig(include, exclude, detected)
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
	if _, err := f.WriteString(content); err != nil {
		f.Close()
		fmt.Fprintf(stderr, "init: writing %s: %v\n", displayPath(abs), err)
		return 2
	}
	if err := f.Close(); err != nil {
		fmt.Fprintf(stderr, "init: writing %s: %v\n", displayPath(abs), err)
		return 2
	}
	fmt.Fprintf(stdout, "✅ wrote %s — edit it, then run `pawl record` to snapshot your baseline.\n", displayPath(abs))
	if len(detected) > 0 {
		fmt.Fprintf(stdout, "   detected %s — generated dimensions scan %s\n", strings.Join(detected, ", "), strings.Join(include, ", "))
	}
	fmt.Fprintln(stdout, "   More ready-to-paste dimensions: https://github.com/tiangong-dev/pawl/blob/main/RECIPES.md")
	// The scaffold is only half of adoption: a repo whose agent doesn't know
	// this gate exists is a repo where the gate runs in CI and nowhere else.
	fmt.Fprintln(stdout, "   Working with a coding agent? `pawl agent` teaches it this gate.")
	return 0
}
