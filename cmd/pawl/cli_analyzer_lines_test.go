package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A stand-in for any line-oriented tool pawl has never heard of. It prints
// ruff-style `path:line:col: RULE message`, counts its own invocations into a
// file so a test can prove sharing, and exits 1 because it found something.
const lineToolScript = `#!/bin/sh
echo run >> "$PAWL_ROOT/runs.log"
cat <<'OUT'
src/a.py:3:1: F401 imported but unused
src/a.py:9:5: E501 line too long
src/b.py:2:1: F401 imported but unused
OUT
exit 1
`

// linesConfig wires the fake tool to a lines analyzer plus the given dimension
// bodies, each already carrying its own selector.
func linesConfig(dims string) string {
	return `analyzers:
  - id: "tool"
    builtin: "lines"
    options:
      command: "sh ./tool.sh"
      valid_exit_codes: [0, 1]
      regex: '^(?P<path>[^:]+):(?P<line>\d+):\d+: (?P<rule>\S+) .*$'

dimensions:
` + dims
}

func linesDim(id, rules string) string {
	d := `  - id: "` + id + `"
    title: "` + id + `"
    direction: "lower-is-better"
    gate: "per-file-count"
    source: "tool"
`
	if rules != "" {
		d += "    options:\n      rules: [" + rules + "]\n"
	}
	return d
}

func writeLineTool(t *testing.T, dir string) {
	t.Helper()
	writeFile(t, dir, "tool.sh", lineToolScript)
}

// The capability this analyzer exists for: several dimensions select different
// rules out of one tool run. Without it, a tool with no built-in support had to
// be invoked once per dimension.
func TestLinesAnalyzerRunsOnceForSeveralDimensions(t *testing.T) {
	dir := t.TempDir()
	writeLineTool(t, dir)
	writeFile(t, dir, "pawl.yaml", linesConfig(
		linesDim("unused-imports", `"F401"`)+"\n"+linesDim("long-lines", `"E501"`)))

	res := runPawl(t, dir, baseEnv(), "record")
	if res.exit != 0 {
		t.Fatalf("record exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	if runs := strings.Count(readFile(t, filepath.Join(dir, "runs.log")), "run"); runs != 1 {
		t.Fatalf("tool ran %d times, want 1 — the analyzer must be shared, not re-run per dimension", runs)
	}

	snap := readSnapshot(t, filepath.Join(dir, "pawl.snapshot.json"))
	if got := snap.Metrics["unused-imports"].Value; got != 2 {
		t.Fatalf("unused-imports = %v, want 2", got)
	}
	if got := snap.Metrics["long-lines"].Value; got != 1 {
		t.Fatalf("long-lines = %v, want 1", got)
	}
}

// The `path` and `line` groups build the per-file breakdown, so a lines
// analyzer supports per-file-count gating exactly like a SARIF one.
func TestLinesAnalyzerBuildsPerFileBreakdown(t *testing.T) {
	dir := t.TempDir()
	writeLineTool(t, dir)
	writeFile(t, dir, "pawl.yaml", linesConfig(linesDim("unused-imports", `"F401"`)))

	if res := runPawl(t, dir, baseEnv(), "record"); res.exit != 0 {
		t.Fatalf("record exit = %d, want 0\nstderr=%s", res.exit, res.stderr)
	}
	got := readSnapshot(t, filepath.Join(dir, "pawl.snapshot.json")).Metrics["unused-imports"].Breakdown
	if got["src/a.py:3"] != 1 || got["src/b.py:2"] != 1 || len(got) != 2 {
		t.Fatalf("breakdown = %v, want one entry per offending location", got)
	}
}

// No selector means every finding counts — the "one number for the whole tool"
// case, which must not require naming rules that do not exist yet.
func TestLinesAnalyzerWithoutSelectorCountsEveryFinding(t *testing.T) {
	dir := t.TempDir()
	writeLineTool(t, dir)
	writeFile(t, dir, "pawl.yaml", linesConfig(linesDim("all", "")))

	if res := runPawl(t, dir, baseEnv(), "record"); res.exit != 0 {
		t.Fatalf("record exit = %d, want 0\nstderr=%s", res.exit, res.stderr)
	}
	if got := readSnapshot(t, filepath.Join(dir, "pawl.snapshot.json")).Metrics["all"].Value; got != 3 {
		t.Fatalf("value = %v, want 3", got)
	}
}

// Levels are whatever the regex captures. pawl must not police them against a
// fixed vocabulary, because severity names belong to the tool.
func TestLinesAnalyzerAcceptsToolSpecificLevelNames(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "tool.sh", `#!/bin/sh
printf 'a.rs:1:1: nag: x\na.rs:2:1: shout: y\n'
`)
	writeFile(t, dir, "pawl.yaml", `analyzers:
  - id: "tool"
    builtin: "lines"
    options:
      command: "sh ./tool.sh"
      regex: '^(?P<path>[^:]+):(?P<line>\d+):\d+: (?P<level>\w+): .*$'

dimensions:
  - id: "shouts"
    title: "shouts"
    direction: "lower-is-better"
    source: "tool"
    options:
      levels: ["shout"]
`)
	if res := runPawl(t, dir, baseEnv(), "record"); res.exit != 0 {
		t.Fatalf("record exit = %d, want 0\nstderr=%s", res.exit, res.stderr)
	}
	if got := readSnapshot(t, filepath.Join(dir, "pawl.snapshot.json")).Metrics["shouts"].Value; got != 1 {
		t.Fatalf("value = %v, want 1", got)
	}
}

// A line the pattern does not match fails the measurement instead of being
// skipped. Skipping is how a tool that changed its output format silently turns
// every dimension sourcing it into a clean zero.
func TestLinesAnalyzerFailsOnAnUnmatchedLine(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "tool.sh", `#!/bin/sh
printf 'src/a.py:3:1: F401 unused\nFound 1 error.\n'
`)
	writeFile(t, dir, "pawl.yaml", linesConfig(linesDim("findings", "")))

	res := runPawl(t, dir, baseEnv(), "record")
	if res.exit != 2 {
		t.Fatalf("record exit = %d, want 2\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	if !strings.Contains(res.stderr, "Found 1 error.") {
		t.Fatalf("stderr should quote the line that did not match, got:\n%s", res.stderr)
	}
}

// The tool's own exit code is judged by the same contract every other command
// dimension uses.
func TestLinesAnalyzerRejectsAnUnlistedExitCode(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "tool.sh", `#!/bin/sh
printf 'src/a.py:3:1: F401 unused\n'
exit 3
`)
	writeFile(t, dir, "pawl.yaml", linesConfig(linesDim("findings", "")))

	res := runPawl(t, dir, baseEnv(), "record")
	if res.exit != 2 {
		t.Fatalf("record exit = %d, want 2\nstderr=%s", res.exit, res.stderr)
	}
	if !strings.Contains(res.stderr, "exited with 3") {
		t.Fatalf("stderr should name the exit code, got:\n%s", res.stderr)
	}
}

// min_files counts files *scanned*. Line output only reveals files that had
// findings, so honouring the option would let a completeness floor pass on a
// scan that covered nothing.
func TestLinesAnalyzerRefusesMinFiles(t *testing.T) {
	dir := t.TempDir()
	writeLineTool(t, dir)
	writeFile(t, dir, "pawl.yaml", strings.Replace(linesConfig(linesDim("all", "")),
		"      valid_exit_codes: [0, 1]", "      valid_exit_codes: [0, 1]\n      min_files: 1", 1))

	res := runPawl(t, dir, baseEnv(), "check")
	if res.exit != 2 {
		t.Fatalf("check exit = %d, want 2\nstderr=%s", res.exit, res.stderr)
	}
	if !strings.Contains(res.stderr, "min_files") || !strings.Contains(res.stderr, "scanned") {
		t.Fatalf("stderr should explain why min_files cannot be honoured, got:\n%s", res.stderr)
	}
}

// verify probes a tool's own config for a rule catalog. Line output has none,
// so accepting the option would promise a guarantee pawl cannot keep.
func TestLinesAnalyzerRefusesVerify(t *testing.T) {
	dir := t.TempDir()
	writeLineTool(t, dir)
	writeFile(t, dir, "pawl.yaml", strings.Replace(linesConfig(linesDim("all", "")),
		"    builtin: \"lines\"", "    builtin: \"lines\"\n    verify:\n      - \"echo hi\"", 1))

	res := runPawl(t, dir, baseEnv(), "check")
	if res.exit != 2 {
		t.Fatalf("check exit = %d, want 2\nstderr=%s", res.exit, res.stderr)
	}
	if !strings.Contains(res.stderr, "verify") || !strings.Contains(res.stderr, "rule catalog") {
		t.Fatalf("stderr should explain why verify cannot be honoured, got:\n%s", res.stderr)
	}
}

// An uncompilable pattern must abort at config load, before any tool runs.
func TestLinesAnalyzerRejectsAnUncompilableRegex(t *testing.T) {
	dir := t.TempDir()
	writeLineTool(t, dir)
	writeFile(t, dir, "pawl.yaml", strings.Replace(linesConfig(linesDim("all", "")),
		`regex: '^(?P<path>[^:]+):(?P<line>\d+):\d+: (?P<rule>\S+) .*$'`, `regex: '([unclosed'`, 1))

	res := runPawl(t, dir, baseEnv(), "check")
	if res.exit != 2 {
		t.Fatalf("check exit = %d, want 2\nstderr=%s", res.exit, res.stderr)
	}
	if !strings.Contains(res.stderr, "regex") {
		t.Fatalf("stderr should name the bad regex, got:\n%s", res.stderr)
	}
	if _, err := os.Stat(filepath.Join(dir, "runs.log")); err == nil {
		t.Fatal("the tool ran despite an invalid config — validation must precede execution")
	}
}

// `lines` is a sharing boundary, so it belongs under analyzers:. On a dimension
// it would have nothing to share, and the message must say where to put it.
func TestLinesRejectedAsADimensionBuiltin(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "pawl.yaml", `dimensions:
  - id: "x"
    title: "x"
    direction: "lower-is-better"
    builtin: "lines"
`)
	res := runPawl(t, dir, baseEnv(), "check")
	if res.exit != 2 {
		t.Fatalf("check exit = %d, want 2\nstderr=%s", res.exit, res.stderr)
	}
	if !strings.Contains(res.stderr, "analyzers:") || !strings.Contains(res.stderr, "source:") {
		t.Fatalf("stderr should point at the analyzer form, got:\n%s", res.stderr)
	}
}
