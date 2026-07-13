package pawl

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// runBaselineGuard compares the working tree's snapshot against the version
// committed at ref (in CI: the PR's base branch). This is what stops a
// hand-edited snapshot from faking a pass — check alone only verifies
// consistency between the snapshot on disk and a fresh measurement, not that
// the file's history is honest.
func runBaselineGuard(cfg *Config, ref string, stdout, stderr io.Writer) int {
	if ref == "" {
		fmt.Fprintln(stderr, "baseline-guard requires a git ref, e.g. `pawl baseline-guard origin/main`")
		return 2
	}

	toplevel, code, gitErr := gitOutput(cfg.Dir, "rev-parse", "--show-toplevel")
	if code != 0 {
		fmt.Fprintf(stderr, "baseline-guard: %s is not inside a git repository: %s\n", cfg.Dir, gitErr)
		return 2
	}
	relPath, err := filepath.Rel(toplevel, cfg.SnapshotPath)
	if err != nil || strings.HasPrefix(relPath, "..") {
		fmt.Fprintf(stderr, "baseline-guard: snapshot %s is outside the git repository %s\n", cfg.SnapshotPath, toplevel)
		return 2
	}
	relPath = filepath.ToSlash(relPath)

	// Two-stage lookup, the stages deliberately separate: rev-parse --verify
	// fails only when ref itself doesn't resolve — the one honest signal for
	// "error". git show fails both for a bad ref AND for a valid ref missing
	// the path; conflating those would let a typo'd ref silently disable the
	// anti-tamper gate.
	if _, code, gitErr := gitOutput(cfg.Dir, "rev-parse", "--verify", ref); code != 0 {
		fmt.Fprintf(stderr, "baseline-guard: could not resolve ref %q: %s\n", ref, gitErr)
		return 2
	}
	shown, code, _ := gitOutput(cfg.Dir, "show", ref+":"+relPath)
	if code != 0 {
		fmt.Fprintf(stdout, "baseline-guard: no %s found at %s — nothing to compare against, skipping.\n", relPath, ref)
		return 0
	}

	baseSnap, baseParsed, err := ParseSnapshot([]byte(shown))
	if err != nil {
		fmt.Fprintf(stderr, "baseline-guard: %s at %s is %v\n", relPath, ref, err)
		return 2
	}

	currentData, err := os.ReadFile(cfg.SnapshotPath)
	if os.IsNotExist(err) {
		fmt.Fprintf(stderr, "no %s in the working tree — run `pawl record` first.\n", cfg.SnapshotPath)
		return 2
	}
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	currentSnap, currentParsed, err := ParseSnapshot(currentData)
	if err != nil {
		fmt.Fprintf(stderr, "baseline-guard: working tree %s is %v\n", cfg.SnapshotPath, err)
		return 2
	}

	var shapeErrors []string
	for _, e := range SnapshotShapeErrors(baseParsed) {
		shapeErrors = append(shapeErrors, fmt.Sprintf("%s: %s", ref, e))
	}
	for _, e := range SnapshotShapeErrors(currentParsed) {
		shapeErrors = append(shapeErrors, fmt.Sprintf("working tree: %s", e))
	}
	if len(shapeErrors) > 0 {
		fmt.Fprintln(stderr, "baseline-guard: malformed snapshot shape:")
		for _, e := range shapeErrors {
			fmt.Fprintf(stderr, "  • %s\n", e)
		}
		return 2
	}

	violations, removed := BaselineGuardViolations(baseSnap.Metrics, currentSnap.Metrics)

	if len(removed) > 0 {
		message := fmt.Sprintf(
			"baseline-guard: metric(s) present at %s are missing from the current snapshot: %s — confirm the dimension was deleted deliberately.",
			ref, strings.Join(removed, ", "))
		if onCI() {
			fmt.Fprintf(stdout, "::warning::%s\n", message)
		} else {
			fmt.Fprintf(stdout, "⚠️  %s\n", message)
		}
	}

	if len(violations) > 0 {
		fmt.Fprintf(stdout, "baseline-guard: snapshot regressed against %s:\n", ref)
		for _, v := range violations {
			fmt.Fprintf(stdout, "  • %s\n", v)
		}
		return 1
	}

	fmt.Fprintf(stdout, "baseline-guard: snapshot is consistent with %s.\n", ref)
	return 0
}

// gitOutput runs one git command against dir and returns trimmed stdout, the
// exit code, and trimmed stderr — callers branch on the exit code, so git
// failing must never look like empty-but-successful output.
func gitOutput(dir string, args ...string) (string, int, string) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	code := 0
	if err != nil {
		code = cmd.ProcessState.ExitCode()
		if code <= 0 {
			code = 1
		}
	}
	return strings.TrimSpace(stdout.String()), code, strings.TrimSpace(stderr.String())
}
