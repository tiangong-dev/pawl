package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

// extractConfig renders a single-dimension pawl.yaml carrying an `extract`
// key. The shared dimDef/buildConfig harness deliberately does not model
// `extract`, so extract dimensions are authored here: extractLines are emitted
// verbatim inside the dimension body (pass "extract: number" for a scalar
// form, or "extract:" followed by "  regex: ..." for an object form).
func extractConfig(id, direction, gate, command string, extractLines ...string) string {
	var b strings.Builder
	b.WriteString("dimensions:\n")
	fmt.Fprintf(&b, "  - id: %q\n", id)
	fmt.Fprintf(&b, "    title: %q\n", id)
	fmt.Fprintf(&b, "    direction: %q\n", direction)
	if gate != "" {
		fmt.Fprintf(&b, "    gate: %q\n", gate)
	}
	fmt.Fprintf(&b, "    command: %q\n", command)
	for _, l := range extractLines {
		fmt.Fprintf(&b, "    %s\n", l)
	}
	return b.String()
}

// extract: number takes the command's trimmed stdout as the value — proving
// pawl derives a measurement from raw numeric output with no wrapper JSON, and
// that surrounding whitespace is stripped rather than read as a parse failure.
func TestExtractNumberRecordsTrimmedNumericValue(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "pawl.yaml", extractConfig("n", "lower-is-better", "", "echo '  42  '", "extract: number"))

	res := runPawl(t, dir, baseEnv(), "record")
	if res.exit != 0 {
		t.Fatalf("record exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	snap := readSnapshot(t, filepath.Join(dir, "pawl.snapshot.json"))
	m := snap.Metrics["n"]
	if m.Value != 42 {
		t.Errorf("value = %v, want 42 (trimmed numeric stdout)", m.Value)
	}
	if len(m.Breakdown) != 0 {
		t.Errorf("breakdown = %v, want none for extract: number", m.Breakdown)
	}
}

// extract: number must fail loud (exit 2) rather than fabricate a value when
// the stdout is not exactly one finite number — non-numeric, multi-token,
// empty, or produced by a command that itself failed. "Could not measure"
// must never be conflated with "measured zero".
func TestExtractNumberMeasurementFailures(t *testing.T) {
	cases := []struct {
		name    string
		command string
	}{
		// Well-formed exec JSON is still not a bare number under extract: number.
		{"json object is not a bare number", `echo '{"value": 5}'`},
		{"non-numeric text", "echo hello"},
		{"multiple tokens", "echo '42 43'"},
		{"empty stdout", "printf ''"},
		{"command exits non-zero", "echo 42; exit 1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, dir, "pawl.yaml", extractConfig("n", "lower-is-better", "", tc.command, "extract: number"))
			res := runPawl(t, dir, baseEnv(), "record")
			if res.exit != 2 {
				t.Fatalf("exit = %d, want 2\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
			}
		})
	}
}

// extract: lines counts non-empty lines, treating blank and whitespace-only
// lines as noise — the "count the matches this command printed" case with no
// reformatting wrapper.
func TestExtractLinesCountsNonEmptyLines(t *testing.T) {
	dir := t.TempDir()
	// Three findings, plus a blank line and a whitespace-only line that must
	// not inflate the count.
	writeFile(t, dir, "pawl.yaml", extractConfig("l", "lower-is-better", "", `printf 'a\nb\n\n  \nc\n'`, "extract: lines"))

	res := runPawl(t, dir, baseEnv(), "record")
	if res.exit != 0 {
		t.Fatalf("record exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	snap := readSnapshot(t, filepath.Join(dir, "pawl.snapshot.json"))
	if got := snap.Metrics["l"].Value; got != 3 {
		t.Errorf("value = %v, want 3 (blank/whitespace-only lines ignored)", got)
	}
}

// extract: lines aborts (exit 2) when the command exits non-zero — the count
// of a failed command's partial output is not an honest measurement.
func TestExtractLinesCommandNonZeroExitIsMeasurementFailure(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "pawl.yaml", extractConfig("l", "lower-is-better", "", `printf 'a\nb\n'; exit 1`, "extract: lines"))
	res := runPawl(t, dir, baseEnv(), "record")
	if res.exit != 2 {
		t.Fatalf("exit = %d, want 2\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
}

// extract: {regex} without a `path` capture group produces a scalar count of
// matching lines and a null breakdown — the summary-only case.
func TestExtractRegexCountsMatchingLinesNoBreakdown(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "pawl.yaml", extractConfig("r", "lower-is-better", "",
		`printf 'ISSUE one\nISSUE two\n'`,
		"extract:", `  regex: '^ISSUE'`))

	res := runPawl(t, dir, baseEnv(), "record")
	if res.exit != 0 {
		t.Fatalf("record exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	snap := readSnapshot(t, filepath.Join(dir, "pawl.snapshot.json"))
	m := snap.Metrics["r"]
	if m.Value != 2 {
		t.Errorf("value = %v, want 2 (matching line count)", m.Value)
	}
	if len(m.Breakdown) != 0 {
		t.Errorf("breakdown = %v, want null with no path capture group", m.Breakdown)
	}
}

// extract: {regex} with named `path`+`line` groups builds a per-file-count
// breakdown keyed "<path>:<line>", each matching line +1 to its key — the
// golangci-lint-style adapter with no wrapper script.
func TestExtractRegexPathLineBuildsBreakdown(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "pawl.yaml", extractConfig("r", "lower-is-better", "per-file-count",
		`printf 'src/a.go:5: bad\nsrc/a.go:5: worse\nsrc/b.go:10: x\n'`,
		"extract:", `  regex: '^(?P<path>[^:]+):(?P<line>\d+):'`))

	res := runPawl(t, dir, baseEnv(), "record")
	if res.exit != 0 {
		t.Fatalf("record exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	snap := readSnapshot(t, filepath.Join(dir, "pawl.snapshot.json"))
	m := snap.Metrics["r"]
	if m.Value != 3 {
		t.Errorf("value = %v, want 3 (matching lines)", m.Value)
	}
	want := map[string]float64{"src/a.go:5": 2, "src/b.go:10": 1}
	if len(m.Breakdown) != len(want) {
		t.Fatalf("breakdown = %v, want %v", m.Breakdown, want)
	}
	for k, v := range want {
		if m.Breakdown[k] != v {
			t.Errorf("breakdown[%q] = %v, want %v (full: %v)", k, m.Breakdown[k], v, m.Breakdown)
		}
	}
}

// extract: {regex} honesty guard — a non-empty stdout line that does NOT match
// the regexp is a measurement failure (exit 2), not a silent value=0. A
// mistyped regexp that matches nothing must never read as "clean".
func TestExtractRegexUnmatchedLineIsMeasurementFailure(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "pawl.yaml", extractConfig("r", "lower-is-better", "",
		`printf 'ISSUE one\nnot-a-finding\n'`,
		"extract:", `  regex: '^ISSUE'`))
	res := runPawl(t, dir, baseEnv(), "record")
	if res.exit != 2 {
		t.Fatalf("exit = %d, want 2\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
}

// An uncompilable extract.regex is a config-load error (exit 2), caught before
// any measurement runs — the command emits valid exec JSON so the failure can
// only come from validating the regexp.
func TestExtractRegexUncompilableIsConfigError(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "pawl.yaml", extractConfig("r", "lower-is-better", "",
		`echo '{"value": 1}'`,
		"extract:", `  regex: '(unclosed'`))
	res := runPawl(t, dir, baseEnv(), "record")
	if res.exit != 2 {
		t.Fatalf("exit = %d, want 2\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
}

// extract: {json_path} reads one finite number at a dotted path from the
// command's stdout JSON — the "one number in a tool's JSON report" case.
func TestExtractJSONPathReadsFiniteNumber(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "pawl.yaml", extractConfig("c", "higher-is-better", "",
		`echo '{"total":{"lines":{"pct":72.41}}}'`,
		"extract:", `  json_path: "total.lines.pct"`))

	res := runPawl(t, dir, baseEnv(), "record")
	if res.exit != 0 {
		t.Fatalf("record exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	if got := snapshotValue(t, dir, "c"); got != 72.41 {
		t.Errorf("value = %v, want 72.41", got)
	}
}

// extract: {json_path} fails loud (exit 2) for a missing key, a non-numeric
// leaf, or malformed JSON — never a silent zero.
func TestExtractJSONPathMeasurementFailures(t *testing.T) {
	cases := []struct {
		name    string
		command string
	}{
		// Command emits valid JSON with a resolvable value elsewhere, but the
		// declared path is absent — still a failure, not a fabricated zero.
		{"missing key", `echo '{"value":1,"total":{"lines":{}}}'`},
		{"non-numeric leaf", `echo '{"total":{"lines":{"pct":"high"}}}'`},
		{"malformed json", "echo not-json"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, dir, "pawl.yaml", extractConfig("c", "higher-is-better", "", tc.command,
				"extract:", `  json_path: "total.lines.pct"`))
			res := runPawl(t, dir, baseEnv(), "record")
			if res.exit != 2 {
				t.Fatalf("exit = %d, want 2\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
			}
		})
	}
}

// An optional `unit` on an object extract form lands in the snapshot verbatim.
func TestExtractUnitLandsInSnapshot(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "pawl.yaml", extractConfig("c", "higher-is-better", "",
		`echo '{"total":{"lines":{"pct":90}}}'`,
		"extract:", `  json_path: "total.lines.pct"`, `  unit: "%"`))

	res := runPawl(t, dir, baseEnv(), "record")
	if res.exit != 0 {
		t.Fatalf("record exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	if got := readSnapshot(t, filepath.Join(dir, "pawl.snapshot.json")).Metrics["c"].Unit; got != "%" {
		t.Errorf("unit = %q, want %q", got, "%")
	}
}

// Every malformed extract configuration is rejected at config load (exit 2).
// Each command emits valid exec JSON, so without extract validation the run
// would exit 0 — the exit-2 requirement is purely the validation contract.
func TestExtractConfigValidationErrorsExitTwo(t *testing.T) {
	validCmd := `echo '{"value": 1}'`
	builtinWithExtract := `dimensions:
  - id: "a"
    title: "a"
    direction: "lower-is-better"
    builtin: "file-length"
    options:
      threshold: 500
      include: ["**/*.go"]
    extract: number
`
	cases := []struct {
		name   string
		config string
	}{
		{"extract on a builtin dimension", builtinWithExtract},
		{"object with both regex and json_path",
			extractConfig("a", "lower-is-better", "", validCmd, "extract:", `  regex: '^x'`, `  json_path: "a.b"`)},
		{"object with neither regex nor json_path",
			extractConfig("a", "lower-is-better", "", validCmd, "extract:", `  unit: "things"`)},
		{"unknown scalar form", extractConfig("a", "lower-is-better", "", validCmd, "extract: bogus")},
		{"empty json_path", extractConfig("a", "lower-is-better", "", validCmd, "extract:", `  json_path: ""`)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, dir, "pawl.yaml", tc.config)
			res := runPawl(t, dir, baseEnv(), "record")
			if res.exit != 2 {
				t.Fatalf("exit = %d, want 2\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
			}
		})
	}
}

// End-to-end: an extract: number dimension feeds the normal gate — a
// recorded baseline passes when equal and fails (exit 1) with the pinned
// scalar detail line when the value worsens.
func TestExtractNumberFeedsGateEndToEnd(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "pawl.yaml", extractConfig("n", "lower-is-better", "", "echo 5", "extract: number"))
	rec := runPawl(t, dir, baseEnv(), "record")
	if rec.exit != 0 {
		t.Fatalf("record exit = %d, want 0\nstdout=%s\nstderr=%s", rec.exit, rec.stdout, rec.stderr)
	}

	same := runPawl(t, dir, baseEnv(), "check")
	if same.exit != 0 {
		t.Fatalf("check (equal) exit = %d, want 0\nstdout=%s\nstderr=%s", same.exit, same.stdout, same.stderr)
	}

	writeFile(t, dir, "pawl.yaml", extractConfig("n", "lower-is-better", "", "echo 8", "extract: number"))
	worse := runPawl(t, dir, baseEnv(), "check")
	if worse.exit != 1 {
		t.Fatalf("check (worse) exit = %d, want 1\nstdout=%s\nstderr=%s", worse.exit, worse.stdout, worse.stderr)
	}
	if !strings.Contains(worse.stdout, "total 5 → 8") {
		t.Errorf("stdout missing scalar detail line: %s", worse.stdout)
	}
}

// snapshotValue reads one metric's recorded value.
func snapshotValue(t *testing.T, dir, id string) float64 {
	t.Helper()
	return readSnapshot(t, filepath.Join(dir, "pawl.snapshot.json")).Metrics[id].Value
}
