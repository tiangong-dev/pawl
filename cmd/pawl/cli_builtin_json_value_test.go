package main

import (
	"fmt"
	"strings"
	"testing"
)

// jsonValueOptionLines builds the option lines for a json-value builtin
// dimension in buildConfig's `key = value` form (which it re-emits as YAML).
// command is written as a double-quoted string (matching how buildConfig
// renders the top-level `command` field via %q) since these commands embed
// literal `"` characters (JSON). Pass "" to omit any option; unit "" omits
// the option so the builtin's own default ("count") applies.
func jsonValueOptionLines(command, file, path, unit string) []string {
	var lines []string
	if command != "" {
		lines = append(lines, fmt.Sprintf("command = %q", command))
	}
	if file != "" {
		lines = append(lines, "file = "+quoteScalar(file))
	}
	if path != "" {
		lines = append(lines, "path = "+quoteScalar(path))
	}
	if unit != "" {
		lines = append(lines, "unit = "+quoteScalar(unit))
	}
	return lines
}

func quoteScalar(s string) string {
	return `"` + s + `"`
}

// command-stdout source: the command's stdout IS the JSON, no file
// involved. Value is an integer read via a dotted path.
func TestBuiltinJsonValueCommandStdoutInteger(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
		id: "widgets", direction: "lower-is-better", builtin: "json-value",
		optionLines: jsonValueOptionLines(`printf '%s' '{"a":{"b":42}}'`, "", "a.b", ""),
	}))

	res := runPawl(t, dir, baseEnv(), "record")
	if res.exit != 0 {
		t.Fatalf("record exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	snap := readSnapshot(t, dirJoin(dir, "pawl.snapshot.json"))
	m := snap.Metrics["widgets"]
	if m.Value != 42 {
		t.Errorf("value = %v, want 42", m.Value)
	}
	if m.Unit != "count" {
		t.Errorf("unit = %q, want default %q", m.Unit, "count")
	}
	if m.Breakdown != nil {
		t.Errorf("breakdown = %v, want nil (json-value is a scalar metric)", m.Breakdown)
	}
}

// file source: no command, path already exists as a pre-written file. Value
// is a float and unit is overridden from its "count" default.
func TestBuiltinJsonValueFileSourceFloatCustomUnit(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "coverage.json", `{"total":{"lines":{"pct":87.5}}}`)
	writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
		id: "coverage", direction: "higher-is-better", builtin: "json-value",
		optionLines: jsonValueOptionLines("", "coverage.json", "total.lines.pct", "%"),
	}))

	res := runPawl(t, dir, baseEnv(), "record")
	if res.exit != 0 {
		t.Fatalf("record exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	snap := readSnapshot(t, dirJoin(dir, "pawl.snapshot.json"))
	m := snap.Metrics["coverage"]
	if m.Value != 87.5 {
		t.Errorf("value = %v, want 87.5", m.Value)
	}
	if m.Unit != "%" {
		t.Errorf("unit = %q, want %q", m.Unit, "%")
	}
}

// command+file source with stale-artifact protection: a WRONG pre-existing
// out.json must be cleared before the command runs, mirroring the jscpd
// builtin's stale-report guard — otherwise a command that silently no-ops
// (or fails to overwrite) would report a fossil value from a prior run.
func TestBuiltinJsonValueCommandFileStaleArtifactProtection(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "out.json", `{"n":999}`)
	writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
		id: "n", direction: "lower-is-better", builtin: "json-value",
		optionLines: jsonValueOptionLines(
			`printf '%s' '{"n":5}' > "$PAWL_ROOT/out.json"`, "out.json", "n", "",
		),
	}))

	res := runPawl(t, dir, baseEnv(), "record")
	if res.exit != 0 {
		t.Fatalf("record exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	snap := readSnapshot(t, dirJoin(dir, "pawl.snapshot.json"))
	if snap.Metrics["n"].Value != 5 {
		t.Errorf("value = %v, want 5 (fresh command output, not the stale pre-existing 999)", snap.Metrics["n"].Value)
	}
}

// higher-is-better gating: a coverage DROP must fail check (exit 1), and an
// improvement over the baseline must pass (exit 0) — this is the whole
// point of json-value existing (it's the intended home for
// higher-is-better dimensions like coverage/type-coverage).
func TestBuiltinJsonValueHigherIsBetterGating(t *testing.T) {
	dir := t.TempDir()
	config := buildConfig("", dimDef{
		id: "coverage", direction: "higher-is-better", builtin: "json-value",
		optionLines: jsonValueOptionLines("", "coverage.json", "total.lines.pct", "%"),
	})

	writeFile(t, dir, "coverage.json", `{"total":{"lines":{"pct":90.0}}}`)
	mustRecord(t, dir, config)

	writeFile(t, dir, "coverage.json", `{"total":{"lines":{"pct":85.0}}}`)
	writeFile(t, dir, "pawl.yaml", config)
	drop := runPawl(t, dir, baseEnv(), "check")
	if drop.exit != 1 {
		t.Fatalf("check exit after coverage drop 90->85 = %d, want 1 (regression)\nstdout=%s\nstderr=%s", drop.exit, drop.stdout, drop.stderr)
	}

	writeFile(t, dir, "coverage.json", `{"total":{"lines":{"pct":95.0}}}`)
	writeFile(t, dir, "pawl.yaml", config)
	improve := runPawl(t, dir, baseEnv(), "check")
	if improve.exit != 0 {
		t.Fatalf("check exit after coverage rise 90->95 = %d, want 0 (improvement)\nstdout=%s\nstderr=%s", improve.exit, improve.stdout, improve.stderr)
	}
}

// A non-zero command exit is a measurement failure, even for a
// command-only (stdout) source that emits valid JSON before failing.
func TestBuiltinJsonValueCommandNonZeroExitIsMeasurementFailure(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
		id: "n", direction: "lower-is-better", builtin: "json-value",
		optionLines: jsonValueOptionLines(`printf '%s' '{"n":1}'; exit 3`, "", "n", ""),
	}))
	res := runPawl(t, dir, baseEnv(), "record")
	if res.exit != 2 {
		t.Fatalf("record exit = %d, want 2 (command exited non-zero)\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
}

// A missing key along the path is a measurement failure, not a silent zero.
func TestBuiltinJsonValueMissingKeyAtPathIsMeasurementFailure(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
		id: "n", direction: "lower-is-better", builtin: "json-value",
		optionLines: jsonValueOptionLines(`printf '%s' '{"a":{}}'`, "", "a.b", ""),
	}))
	res := runPawl(t, dir, baseEnv(), "record")
	if res.exit != 2 {
		t.Fatalf("record exit = %d, want 2 (path a.b does not resolve)\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
}

// A leaf that is not a number is a measurement failure.
func TestBuiltinJsonValueNonNumericLeafIsMeasurementFailure(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
		id: "n", direction: "lower-is-better", builtin: "json-value",
		optionLines: jsonValueOptionLines(`printf '%s' '{"a":"hello"}'`, "", "a", ""),
	}))
	res := runPawl(t, dir, baseEnv(), "record")
	if res.exit != 2 {
		t.Fatalf("record exit = %d, want 2 (leaf at path is a string, not a number)\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
}

// Encountering a non-object while still descending the path (there are
// more path segments left but the current node is a number) is a
// measurement failure, not a silent nil-safe zero.
func TestBuiltinJsonValueNonObjectMidwayIsMeasurementFailure(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
		id: "n", direction: "lower-is-better", builtin: "json-value",
		optionLines: jsonValueOptionLines(`printf '%s' '{"a":5}'`, "", "a.b.c", ""),
	}))
	res := runPawl(t, dir, baseEnv(), "record")
	if res.exit != 2 {
		t.Fatalf("record exit = %d, want 2 (a is a number, cannot descend into .b.c)\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
}

// A file source pointing at a file that doesn't exist is a measurement
// failure, not a config error at load time (the builtin is fully
// configured; the file is just missing at measurement time, e.g. an
// upstream tool step wasn't run).
func TestBuiltinJsonValueMissingFileIsMeasurementFailure(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
		id: "n", direction: "lower-is-better", builtin: "json-value",
		optionLines: jsonValueOptionLines("", "does-not-exist.json", "n", ""),
	}))
	res := runPawl(t, dir, baseEnv(), "record")
	if res.exit != 2 {
		t.Fatalf("record exit = %d, want 2 (file source does not exist)\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
}

// Unparseable JSON on stdout is a measurement failure.
func TestBuiltinJsonValueUnparseableJSONStdoutIsMeasurementFailure(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
		id: "n", direction: "lower-is-better", builtin: "json-value",
		optionLines: jsonValueOptionLines(`printf '%s' 'not json at all'`, "", "n", ""),
	}))
	res := runPawl(t, dir, baseEnv(), "record")
	if res.exit != 2 {
		t.Fatalf("record exit = %d, want 2 (stdout is not valid JSON)\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
}

// Config validation at load time (exit 2, before any measurement runs):
// path is always required, and at least one of command/file must be given.
// The rejection must name json-value as the recognized builtin (not fall
// through to "unknown builtin"), matching the jscpd builtin's contract.
func TestBuiltinJsonValueMissingOptionsAreConfigErrors(t *testing.T) {
	cases := []struct {
		name        string
		optionLines []string
	}{
		{"missing path", jsonValueOptionLines(`printf '%s' '{"n":1}'`, "", "", "")},
		{"missing command and file", jsonValueOptionLines("", "", "n", "")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
				id: "n", direction: "lower-is-better", builtin: "json-value",
				optionLines: tc.optionLines,
			}))
			res := runPawl(t, dir, baseEnv(), "check")
			if res.exit != 2 {
				t.Fatalf("exit = %d, want 2\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
			}
			if strings.Contains(res.stderr, "unknown builtin") {
				t.Errorf("stderr reports \"unknown builtin\" — json-value must be a recognized builtin whose %s option is what's missing: %s", tc.name, res.stderr)
			}
		})
	}
}
