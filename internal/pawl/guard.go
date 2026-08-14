package pawl

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
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

	var accepted, remaining []GuardViolation
	if len(violations) > 0 {
		trailers, err := acceptedTrailers(cfg.Dir, ref)
		if err != nil {
			fmt.Fprintf(stderr, "baseline-guard: could not read commit trailers between %s and HEAD: %v\n", ref, err)
			return 2
		}
		for _, v := range violations {
			if declared, ok := trailers[v.ID]; ok && trailerAccepts(v, declared) {
				accepted = append(accepted, v)
			} else {
				remaining = append(remaining, v)
			}
		}
	}

	if len(accepted) > 0 {
		lines := make([]string, 0, len(accepted))
		for _, v := range accepted {
			lines = append(lines, v.String())
		}
		message := fmt.Sprintf("baseline-guard: %d accepted regression(s) (Pawl-Accept trailer found): %s",
			len(accepted), strings.Join(lines, "; "))
		if onCI() {
			// The whole message goes in the notice (not just the header) — a
			// GitHub annotation is one line, and the bulleted detail below is
			// only visible in the raw log, not the PR-level annotation.
			fmt.Fprintf(stdout, "::notice::%s\n", message)
		} else {
			fmt.Fprintln(stdout, message)
		}
		for _, v := range accepted {
			fmt.Fprintf(stdout, "  • %s\n", v)
		}
	}

	if len(remaining) > 0 {
		fmt.Fprintf(stdout, "baseline-guard: snapshot regressed against %s:\n", ref)
		for _, v := range remaining {
			fmt.Fprintf(stdout, "  • %s\n", v)
		}
		return 1
	}

	if len(accepted) > 0 {
		// Not "consistent" — it isn't, some metric(s) regressed; they were
		// just explicitly authorized. Saying so avoids printing an accepted
		// regression immediately followed by a claim that nothing changed.
		fmt.Fprintf(stdout, "baseline-guard: no unauthorized regression against %s.\n", ref)
		return 0
	}

	fmt.Fprintf(stdout, "baseline-guard: snapshot is consistent with %s.\n", ref)
	return 0
}

// acceptedTrailers scans the commits in ref..HEAD for `Pawl-Accept: <id>
// <value>` trailer lines and groups the declared values by dimension id. A
// line that doesn't parse (no numeric value) is skipped rather than treated
// as an error — a malformed trailer must fall back to "not accepted", never
// silently disable the gate.
func acceptedTrailers(dir, ref string) (map[string][]float64, error) {
	out, code, gitErr := gitOutput(dir, "log", "--format=%B%x00", ref+"..HEAD")
	if code != 0 {
		return nil, fmt.Errorf("git log %s..HEAD: %s", ref, gitErr)
	}
	declared := map[string][]float64{}
	for _, msg := range strings.Split(out, "\x00") {
		for _, line := range strings.Split(msg, "\n") {
			rest, ok := strings.CutPrefix(strings.TrimSpace(line), "Pawl-Accept:")
			if !ok {
				continue
			}
			rest = strings.TrimSpace(rest)
			sep := strings.LastIndex(rest, " ")
			if sep < 0 {
				continue
			}
			id := strings.TrimSpace(rest[:sep])
			value, err := strconv.ParseFloat(strings.TrimSpace(rest[sep+1:]), 64)
			if id == "" || err != nil {
				continue
			}
			declared[id] = append(declared[id], value)
		}
	}
	return declared, nil
}

// trailerAccepts reports whether v's current value is no worse than the
// worst value declared for it — the trailer author knowingly accepted debt up
// to that point, and the current snapshot must not have moved past it.
// Multiple trailers for the same id (accumulated across a branch's commits)
// take the single most-permissive declared value. v.Tolerance is the
// metric's own recorded slack (same as the rest of this file's honoring of
// the committed baseline's tolerance), so the effective ceiling is the
// declared value plus that tolerance, not the declared value exactly — a
// trailer is a debt ceiling, not a promise of the precise recorded number.
func trailerAccepts(v GuardViolation, declared []float64) bool {
	worst := declared[0]
	for _, d := range declared[1:] {
		if Worse(v.Direction, worst, d, 0) {
			worst = d
		}
	}
	return !Worse(v.Direction, worst, v.Current, v.Tolerance)
}

// gitOutput runs one git command against dir and returns trimmed stdout, the
// exit code, and trimmed stderr — callers branch on the exit code, so git
// failing must never look like empty-but-successful output.
//
// core.quotePath is pinned off for every invocation: it defaults to on, which
// octal-escapes every non-ASCII byte in a path. A path pawl cannot match drops
// out of the changed-file set silently, and a gate that quietly stops seeing a
// file is worse than one that fails.
func gitOutput(dir string, args ...string) (string, int, string) {
	fixed := []string{"-C", dir, "-c", "core.quotePath=false"}
	cmd := exec.Command("git", append(fixed, args...)...)
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
