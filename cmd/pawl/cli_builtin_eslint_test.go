package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// eslintMessage and eslintFileResult mirror the shape of ESLint's
// `--format json` output that the eslint builtin parses: an array of
// per-file results, each with a filePath and a list of messages.
type eslintMessage struct {
	RuleID string `json:"ruleId,omitempty"`
	Line   int    `json:"line,omitempty"`
}

type eslintFileResult struct {
	FilePath string          `json:"filePath"`
	Messages []eslintMessage `json:"messages"`
}

// writeEslintFixture marshals an ESLint JSON array fixture into the config
// dir and returns a `cat` command a dimension's `command` option can run to
// emit it verbatim on stdout — the adapter owns no eslint binary, only the
// format.
func writeEslintFixture(t *testing.T, dir, name string, results []eslintFileResult) string {
	t.Helper()
	b, err := json.Marshal(results)
	if err != nil {
		t.Fatalf("marshal eslint fixture: %v", err)
	}
	writeFile(t, dir, name, string(b))
	return `cat "$PAWL_ROOT/` + name + `"`
}

// eslintOptionLines builds the option lines for an eslint builtin dimension:
// `command` as a single-quoted string (avoids shell escaping pain) and an
// optional `rules` filter.
func eslintOptionLines(command string, rules []string) []string {
	lines := []string{"command = '" + command + "'"}
	if rules != nil {
		b, _ := json.Marshal(rules)
		lines = append(lines, "rules = "+string(b))
	}
	return lines
}

// Happy path: value is the total counted messages across all files, unit is
// "issues", and breakdown keys are "<path>:<line>" — two messages on the
// same file+line merge into one key whose count is 2.
func TestBuiltinEslintHappyPath(t *testing.T) {
	dir := t.TempDir()
	absA := filepath.Join(dir, "a.js")
	command := writeEslintFixture(t, dir, "eslint-out.json", []eslintFileResult{
		{FilePath: absA, Messages: []eslintMessage{
			{RuleID: "no-unused-vars", Line: 3},
			{RuleID: "no-console", Line: 3},
			{RuleID: "no-console", Line: 7},
		}},
		{FilePath: "b.js", Messages: []eslintMessage{
			{RuleID: "no-unused-vars", Line: 1},
		}},
	})

	writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
		id: "eslint-issues", direction: "lower-is-better", gate: "per-file-count",
		builtin: "eslint", optionLines: eslintOptionLines(command, nil),
	}))

	res := runPawl(t, dir, baseEnv(), "record")
	if res.exit != 0 {
		t.Fatalf("record exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	snap := readSnapshot(t, dirJoin(dir, "pawl.snapshot.json"))
	m := snap.Metrics["eslint-issues"]

	if m.Value != 4 {
		t.Errorf("value = %v, want 4 (total messages across both files)", m.Value)
	}
	if m.Unit != "issues" {
		t.Errorf("unit = %q, want %q", m.Unit, "issues")
	}
	want := map[string]float64{"a.js:3": 2, "a.js:7": 1, "b.js:1": 1}
	if len(m.Breakdown) != len(want) {
		t.Fatalf("breakdown = %v, want %v", m.Breakdown, want)
	}
	for k, v := range want {
		if m.Breakdown[k] != v {
			t.Errorf("breakdown[%q] = %v, want %v (full breakdown: %v)", k, m.Breakdown[k], v, m.Breakdown)
		}
	}
}

// rules filters which messages count: only messages whose ruleId is listed
// contribute to value and breakdown. An empty/omitted rules list counts
// every message, as exercised by the happy-path test above.
func TestBuiltinEslintRulesFilter(t *testing.T) {
	dir := t.TempDir()
	command := writeEslintFixture(t, dir, "eslint-out.json", []eslintFileResult{
		{FilePath: "a.js", Messages: []eslintMessage{
			{RuleID: "keep-me", Line: 1},
			{RuleID: "ignore-me", Line: 2},
			{RuleID: "keep-me", Line: 5},
		}},
	})

	writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
		id: "eslint-issues", direction: "lower-is-better",
		builtin: "eslint", optionLines: eslintOptionLines(command, []string{"keep-me"}),
	}))

	res := runPawl(t, dir, baseEnv(), "record")
	if res.exit != 0 {
		t.Fatalf("record exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	snap := readSnapshot(t, dirJoin(dir, "pawl.snapshot.json"))
	m := snap.Metrics["eslint-issues"]

	if m.Value != 2 {
		t.Errorf("value = %v, want 2 (only the two keep-me messages)", m.Value)
	}
	want := map[string]float64{"a.js:1": 1, "a.js:5": 1}
	if len(m.Breakdown) != len(want) {
		t.Fatalf("breakdown = %v, want %v (the ignore-me message must not appear)", m.Breakdown, want)
	}
	for k, v := range want {
		if m.Breakdown[k] != v {
			t.Errorf("breakdown[%q] = %v, want %v", k, m.Breakdown[k], v)
		}
	}
}

// An absolute filePath from ESLint is relativized against the config dir in
// the breakdown key, even through nested directories.
func TestBuiltinEslintRelativizesAbsoluteFilePath(t *testing.T) {
	dir := t.TempDir()
	absNested := filepath.Join(dir, "src", "nested", "deep.js")
	command := writeEslintFixture(t, dir, "eslint-out.json", []eslintFileResult{
		{FilePath: absNested, Messages: []eslintMessage{{RuleID: "r", Line: 9}}},
	})

	writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
		id: "eslint-issues", direction: "lower-is-better",
		builtin: "eslint", optionLines: eslintOptionLines(command, nil),
	}))

	res := runPawl(t, dir, baseEnv(), "record")
	if res.exit != 0 {
		t.Fatalf("record exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	snap := readSnapshot(t, dirJoin(dir, "pawl.snapshot.json"))
	m := snap.Metrics["eslint-issues"]
	want := "src/nested/deep.js:9"
	if _, ok := m.Breakdown[want]; !ok {
		t.Errorf("breakdown = %v, want key %q (absolute filePath relativized against config dir)", m.Breakdown, want)
	}
}

// A message with no line uses line 0 in its breakdown key.
func TestBuiltinEslintMessageWithNoLineUsesZero(t *testing.T) {
	dir := t.TempDir()
	command := writeEslintFixture(t, dir, "eslint-out.json", []eslintFileResult{
		{FilePath: "c.js", Messages: []eslintMessage{{RuleID: "r"}}}, // Line omitted
	})

	writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
		id: "eslint-issues", direction: "lower-is-better",
		builtin: "eslint", optionLines: eslintOptionLines(command, nil),
	}))

	res := runPawl(t, dir, baseEnv(), "record")
	if res.exit != 0 {
		t.Fatalf("record exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	snap := readSnapshot(t, dirJoin(dir, "pawl.snapshot.json"))
	m := snap.Metrics["eslint-issues"]
	if m.Breakdown["c.js:0"] != 1 {
		t.Errorf(`breakdown = %v, want {"c.js:0": 1}`, m.Breakdown)
	}
}

// ESLint's own exit codes are the adapter's contract, not the raw exec
// contract's: exit 1 ("problems found") with valid JSON is still a valid
// measurement — this is the whole point of shipping the adapter instead of
// a raw exec command needing `|| true`.
func TestBuiltinEslintExitOneWithValidJSONIsAMeasurement(t *testing.T) {
	dir := t.TempDir()
	command := writeEslintFixture(t, dir, "eslint-out.json", []eslintFileResult{
		{FilePath: "a.js", Messages: []eslintMessage{{RuleID: "r", Line: 1}}},
	})

	writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
		id: "eslint-issues", direction: "lower-is-better",
		builtin: "eslint", optionLines: eslintOptionLines(command+"; exit 1", nil),
	}))

	res := runPawl(t, dir, baseEnv(), "record")
	if res.exit != 0 {
		t.Fatalf("record exit = %d, want 0 (eslint exit 1 = problems found, still a valid measurement)\nstdout=%s\nstderr=%s",
			res.exit, res.stdout, res.stderr)
	}
	snap := readSnapshot(t, dirJoin(dir, "pawl.snapshot.json"))
	if snap.Metrics["eslint-issues"].Value != 1 {
		t.Errorf("value = %v, want 1", snap.Metrics["eslint-issues"].Value)
	}
}

// Exit 2+ (fatal ESLint error: config error, crash) is a measurement
// failure — unlike exit 1, which is a legitimate "problems found" result.
func TestBuiltinEslintExitTwoIsMeasurementFailure(t *testing.T) {
	dir := t.TempDir()
	command := writeEslintFixture(t, dir, "eslint-out.json", []eslintFileResult{
		{FilePath: "a.js", Messages: []eslintMessage{{RuleID: "r", Line: 1}}},
	})

	writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
		id: "eslint-issues", direction: "lower-is-better",
		builtin: "eslint", optionLines: eslintOptionLines(command+"; exit 2", nil),
	}))

	res := runPawl(t, dir, baseEnv(), "record")
	if res.exit != 2 {
		t.Fatalf("record exit = %d, want 2 (eslint exit 2 is fatal, not problems-found)\nstdout=%s\nstderr=%s",
			res.exit, res.stdout, res.stderr)
	}
	if !strings.Contains(res.stderr, "measuring eslint-issues…") {
		t.Errorf("stderr missing progress line: the dimension must actually run (config-valid) and fail as a measurement, not be rejected up front: %s", res.stderr)
	}
}

// stdout that does not parse as the ESLint JSON array — a bare object or
// garbage — is a measurement failure.
func TestBuiltinEslintNonArrayStdoutIsMeasurementFailure(t *testing.T) {
	cases := []struct {
		name    string
		content string
	}{
		{"bare object", `{"filePath": "a.js", "messages": []}`},
		{"garbage", "not json at all"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, dir, "eslint-out.json", tc.content)
			writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
				id: "eslint-issues", direction: "lower-is-better",
				builtin:     "eslint",
				optionLines: eslintOptionLines(`cat "$PAWL_ROOT/eslint-out.json"`, nil),
			}))
			res := runPawl(t, dir, baseEnv(), "record")
			if res.exit != 2 {
				t.Fatalf("record exit = %d, want 2\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
			}
			if !strings.Contains(res.stderr, "measuring eslint-issues…") {
				t.Errorf("stderr missing progress line: the dimension must actually run (config-valid) and fail parsing stdout, not be rejected up front: %s", res.stderr)
			}
		})
	}
}

// eslint builtin requires the command option; without it the config is
// invalid — the rejection must name the missing option, not fall through to
// "unknown builtin" (which would mean the builtin itself was never recognized).
func TestBuiltinEslintMissingCommandIsConfigError(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
		id: "eslint-issues", direction: "lower-is-better", builtin: "eslint",
	}))
	res := runPawl(t, dir, baseEnv(), "check")
	if res.exit != 2 {
		t.Fatalf("exit = %d, want 2\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	if strings.Contains(res.stderr, "unknown builtin") {
		t.Errorf("stderr reports \"unknown builtin\" — eslint must be a recognized builtin whose command option is what's missing: %s", res.stderr)
	}
}

// Works end-to-end as a per-file-count gate: a message in a file absent
// from the baseline is a fresh offender (regresses from 0) and must fail
// check with the per-file detail line.
func TestBuiltinEslintPerFileCountGateEndToEnd(t *testing.T) {
	dir := t.TempDir()
	baseCommand := writeEslintFixture(t, dir, "eslint-base.json", []eslintFileResult{
		{FilePath: "a.js", Messages: []eslintMessage{{RuleID: "r", Line: 1}}},
	})
	mustRecord(t, dir, buildConfig("", dimDef{
		id: "eslint-issues", direction: "lower-is-better", gate: "per-file-count",
		builtin: "eslint", optionLines: eslintOptionLines(baseCommand, nil),
	}))

	worseCommand := writeEslintFixture(t, dir, "eslint-worse.json", []eslintFileResult{
		{FilePath: "a.js", Messages: []eslintMessage{{RuleID: "r", Line: 1}}},
		{FilePath: "b.js", Messages: []eslintMessage{{RuleID: "r", Line: 1}}},
	})
	writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
		id: "eslint-issues", direction: "lower-is-better", gate: "per-file-count",
		builtin: "eslint", optionLines: eslintOptionLines(worseCommand, nil),
	}))

	res := runPawl(t, dir, baseEnv(), "check")
	if res.exit != 1 {
		t.Fatalf("check exit = %d, want 1 (a new file's offender is a fresh regression)\nstdout=%s\nstderr=%s",
			res.exit, res.stdout, res.stderr)
	}
	if !strings.Contains(res.stdout, "b.js  0 → 1") {
		t.Errorf("stdout missing per-file detail line %q: %s", "b.js  0 → 1", res.stdout)
	}
}
