package main

// --format codeclimate makes record/check/diff print a Code Climate issue
// array to stdout — the format GitLab renders as its MR Code Quality widget —
// instead of the text/json report. It is FINDINGS MODE: it lists every
// current per-file-count offender the gate can locate to a path:line,
// independent of the snapshot delta. stderr and the exit code are unchanged
// from text mode. See SPEC.md § Machine-readable Output.

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"
)

// parseCodeclimate asserts stdout is exactly one JSON array (nothing else —
// no leading table, no trailing emoji line) and decodes it.
func parseCodeclimate(t *testing.T, out string) []map[string]any {
	t.Helper()
	var entries []map[string]any
	if err := json.Unmarshal([]byte(out), &entries); err != nil {
		t.Fatalf("stdout is not a pure JSON array: %v\nstdout=%s", err, out)
	}
	return entries
}

// entryLocation extracts location.path and location.lines.begin, failing
// loudly if either is absent or the wrong type — a Code Quality entry without
// a location is not actionable by GitLab's widget.
func entryLocation(t *testing.T, e map[string]any) (path string, line int) {
	t.Helper()
	loc, ok := e["location"].(map[string]any)
	if !ok {
		t.Fatalf("entry missing location object: %+v", e)
	}
	path, ok = loc["path"].(string)
	if !ok {
		t.Fatalf("location.path missing or not a string: %+v", loc)
	}
	lines, ok := loc["lines"].(map[string]any)
	if !ok {
		t.Fatalf("location.lines missing: %+v", loc)
	}
	begin, ok := lines["begin"].(float64)
	if !ok {
		t.Fatalf("location.lines.begin missing or not a number: %+v", lines)
	}
	return path, int(begin)
}

// A per-file-count dimension's breakdown produces one entry per key, with
// check_name = dimension id, description = dimension title (no suffix at
// count 1), severity always "major", and location parsed from "path:line".
func TestCodeclimatePerFileCountEntryFields(t *testing.T) {
	dir := t.TempDir()
	config := buildConfig("", dimDef{
		id: "lint-issues", title: "Lint issues",
		direction: "lower-is-better", gate: "per-file-count",
		command: `echo '{"value": 2, "breakdown": {"src/a.ts:5": 1, "src/b.ts:11": 1}}'`,
	})
	mustRecord(t, dir, config)

	res := runPawl(t, dir, baseEnv(), "check", "--format", "codeclimate")
	entries := parseCodeclimate(t, res.stdout)
	if len(entries) != 2 {
		t.Fatalf("entries = %+v, want 2 (one per breakdown key)", entries)
	}
	for _, e := range entries {
		if e["check_name"] != "lint-issues" {
			t.Errorf("check_name = %v, want lint-issues", e["check_name"])
		}
		if e["description"] != "Lint issues" {
			t.Errorf("description = %v, want the bare dimension title (count==1, no suffix)", e["description"])
		}
		if e["severity"] != "major" {
			t.Errorf("severity = %v, want major", e["severity"])
		}
	}
	got := map[string]int{}
	for _, e := range entries {
		path, line := entryLocation(t, e)
		got[path] = line
	}
	if got["src/a.ts"] != 5 || got["src/b.ts"] != 11 {
		t.Errorf("locations = %+v, want src/a.ts:5 and src/b.ts:11", got)
	}
}

// description gets " ×<n>" appended only when the breakdown count at that
// location exceeds 1; a count of exactly 1 stays unsuffixed.
func TestCodeclimateDescriptionSuffixWhenCountExceedsOne(t *testing.T) {
	dir := t.TempDir()
	config := buildConfig("", dimDef{
		id: "m", title: "Sample Marker",
		direction: "lower-is-better", gate: "per-file-count",
		command: `echo '{"value": 5, "breakdown": {"a.go:5": 1, "b.go:9": 4}}'`,
	})
	mustRecord(t, dir, config)

	res := runPawl(t, dir, baseEnv(), "check", "--format", "codeclimate")
	entries := parseCodeclimate(t, res.stdout)
	descByPath := map[string]string{}
	for _, e := range entries {
		path, _ := entryLocation(t, e)
		descByPath[path], _ = e["description"].(string)
	}
	if descByPath["a.go"] != "Sample Marker" {
		t.Errorf("description for count==1 offender = %q, want %q (no suffix)", descByPath["a.go"], "Sample Marker")
	}
	if descByPath["b.go"] != "Sample Marker ×4" {
		t.Errorf("description for count==4 offender = %q, want %q", descByPath["b.go"], "Sample Marker ×4")
	}
}

// Only per-file-count dimensions produce findings: a total dimension and a
// per-key-value dimension (even one whose breakdown keys look like
// "path:line") contribute zero entries — their gate is still the exit code,
// not stdout.
func TestCodeclimateTotalAndPerKeyValueDimensionsProduceNoEntries(t *testing.T) {
	dir := t.TempDir()
	config := buildConfig("",
		dimDef{
			id: "file-length", title: "File length", direction: "lower-is-better", gate: "total",
			command: `echo '{"value": 8}'`,
		},
		dimDef{
			id: "pkv", title: "Per key value dim", direction: "lower-is-better", gate: "per-key-value",
			command: `echo '{"value": 5, "breakdown": {"x.go:1": 5}}'`,
		},
		dimDef{
			id: "pfc", title: "Per file count dim", direction: "lower-is-better", gate: "per-file-count",
			command: `echo '{"value": 1, "breakdown": {"y.go:2": 1}}'`,
		},
	)
	mustRecord(t, dir, config)

	res := runPawl(t, dir, baseEnv(), "check", "--format", "codeclimate")
	entries := parseCodeclimate(t, res.stdout)
	if len(entries) != 1 {
		t.Fatalf("entries = %+v, want exactly 1 (only the per-file-count dimension contributes)", entries)
	}
	if entries[0]["check_name"] != "pfc" {
		t.Errorf("check_name = %v, want pfc (total/per-key-value dims must not appear)", entries[0]["check_name"])
	}
}

// A check with no per-file-count offenders prints exactly "[]" — a valid
// empty Code Quality report, not an empty string or omitted output.
func TestCodeclimateNoOffendersEmptyArray(t *testing.T) {
	dir := t.TempDir()
	config := buildConfig("", dimDef{
		id: "m", direction: "lower-is-better", gate: "per-file-count",
		command: `echo '{"value": 0, "breakdown": {}}'`,
	})
	mustRecord(t, dir, config)

	res := runPawl(t, dir, baseEnv(), "check", "--format", "codeclimate")
	if strings.TrimSpace(res.stdout) != "[]" {
		t.Errorf("stdout = %q, want exactly []", res.stdout)
	}
}

// Entries are sorted by path, then line, then check_name — deterministic
// across dimensions that happen to share a location.
func TestCodeclimateEntriesSortedByPathLineCheckName(t *testing.T) {
	dir := t.TempDir()
	// Declared zeta-before-alpha so the check_name tie-break is observable.
	config := buildConfig("",
		dimDef{
			id: "zeta", title: "Zeta dim", direction: "lower-is-better", gate: "per-file-count",
			command: `echo '{"value": 1, "breakdown": {"m.go:5": 1}}'`,
		},
		dimDef{
			id: "alpha", title: "Alpha dim", direction: "lower-is-better", gate: "per-file-count",
			command: `echo '{"value": 3, "breakdown": {"m.go:5": 1, "m.go:2": 1, "a.go:9": 1}}'`,
		},
	)
	mustRecord(t, dir, config)

	res := runPawl(t, dir, baseEnv(), "check", "--format", "codeclimate")
	entries := parseCodeclimate(t, res.stdout)
	if len(entries) != 4 {
		t.Fatalf("entries = %+v, want 4", entries)
	}
	type key struct {
		path, check string
		line        int
	}
	var got []key
	for _, e := range entries {
		path, line := entryLocation(t, e)
		check, _ := e["check_name"].(string)
		got = append(got, key{path, check, line})
	}
	want := []key{
		{"a.go", "alpha", 9},
		{"m.go", "alpha", 2},
		{"m.go", "alpha", 5},
		{"m.go", "zeta", 5},
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("order = %+v, want %+v (mismatch at index %d)", got, want, i)
			break
		}
	}
}

// Fingerprint stability: two separate invocations against the same offender
// yield the identical fingerprint, so GitLab tracks the same issue run over run.
func TestCodeclimateFingerprintStableAcrossRuns(t *testing.T) {
	dir := t.TempDir()
	config := buildConfig("", dimDef{
		id: "m", title: "Stable marker", direction: "lower-is-better", gate: "per-file-count",
		command: `echo '{"value": 1, "breakdown": {"a.go:3": 1}}'`,
	})
	mustRecord(t, dir, config)

	first := parseCodeclimate(t, runPawl(t, dir, baseEnv(), "check", "--format", "codeclimate").stdout)
	second := parseCodeclimate(t, runPawl(t, dir, baseEnv(), "check", "--format", "codeclimate").stdout)
	if len(first) != 1 || len(second) != 1 {
		t.Fatalf("first=%+v second=%+v, want exactly 1 entry in each run", first, second)
	}
	fp1, _ := first[0]["fingerprint"].(string)
	fp2, _ := second[0]["fingerprint"].(string)
	if fp1 == "" || fp2 == "" {
		t.Fatalf("empty fingerprint: fp1=%q fp2=%q", fp1, fp2)
	}
	if fp1 != fp2 {
		t.Errorf("fingerprint changed across runs for the same offender: %q vs %q", fp1, fp2)
	}
}

// Fingerprint uniqueness: two distinct offenders in the same run never share
// a fingerprint.
func TestCodeclimateFingerprintUniqueForDifferentOffenders(t *testing.T) {
	dir := t.TempDir()
	config := buildConfig("", dimDef{
		id: "m", title: "Marker", direction: "lower-is-better", gate: "per-file-count",
		command: `echo '{"value": 2, "breakdown": {"a.go:3": 1, "b.go:7": 1}}'`,
	})
	mustRecord(t, dir, config)

	res := runPawl(t, dir, baseEnv(), "check", "--format", "codeclimate")
	entries := parseCodeclimate(t, res.stdout)
	if len(entries) != 2 {
		t.Fatalf("entries = %+v, want 2", entries)
	}
	seen := map[string]bool{}
	for _, e := range entries {
		fp, _ := e["fingerprint"].(string)
		if fp == "" {
			t.Fatalf("empty fingerprint in %+v", e)
		}
		if seen[fp] {
			t.Errorf("duplicate fingerprint %q across distinct offenders: %+v", fp, entries)
		}
		seen[fp] = true
	}
}

var hexFingerprintRE = regexp.MustCompile(`^[0-9a-f]+$`)

// Fingerprint is a non-empty lowercase hex digest — not a raw description
// string, not uppercase, not empty.
func TestCodeclimateFingerprintIsNonEmptyLowercaseHex(t *testing.T) {
	dir := t.TempDir()
	config := buildConfig("", dimDef{
		id: "m", direction: "lower-is-better", gate: "per-file-count",
		command: `echo '{"value": 1, "breakdown": {"a.go:1": 1}}'`,
	})
	mustRecord(t, dir, config)

	res := runPawl(t, dir, baseEnv(), "check", "--format", "codeclimate")
	entries := parseCodeclimate(t, res.stdout)
	if len(entries) != 1 {
		t.Fatalf("entries = %+v, want 1", entries)
	}
	fp, _ := entries[0]["fingerprint"].(string)
	if fp == "" || !hexFingerprintRE.MatchString(fp) {
		t.Errorf("fingerprint = %q, want non-empty lowercase hex", fp)
	}
}

// check --format codeclimate still exits 1 on a regression vs the snapshot —
// codeclimate only changes stdout, never the gate verdict — while stdout
// remains the findings array (current offenders), not empty and not a table.
func TestCodeclimateCheckExitsOneOnRegressionButStdoutIsFindings(t *testing.T) {
	dir := t.TempDir()
	base := buildConfig("", dimDef{
		id: "m", direction: "lower-is-better", gate: "per-file-count",
		command: `echo '{"value": 1, "breakdown": {"a.go:1": 1}}'`,
	})
	mustRecord(t, dir, base)
	worse := buildConfig("", dimDef{
		id: "m", direction: "lower-is-better", gate: "per-file-count",
		command: `echo '{"value": 2, "breakdown": {"a.go:1": 1, "b.go:2": 1}}'`,
	})
	writeFile(t, dir, "pawl.yaml", worse)

	res := runPawl(t, dir, baseEnv(), "check", "--format", "codeclimate")
	if res.exit != 1 {
		t.Fatalf("exit = %d, want 1 (regression vs snapshot)\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	entries := parseCodeclimate(t, res.stdout)
	if len(entries) != 2 {
		t.Fatalf("entries = %+v, want 2 (current offenders, independent of exit code)", entries)
	}
}

// A clean check --format codeclimate exits 0.
func TestCodeclimateCheckCleanRunExitsZero(t *testing.T) {
	dir := t.TempDir()
	config := buildConfig("", dimDef{
		id: "m", direction: "lower-is-better", gate: "per-file-count",
		command: `echo '{"value": 1, "breakdown": {"a.go:1": 1}}'`,
	})
	mustRecord(t, dir, config)

	res := runPawl(t, dir, baseEnv(), "check", "--format", "codeclimate")
	if res.exit != 0 {
		t.Fatalf("exit = %d, want 0 (no regression)\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	parseCodeclimate(t, res.stdout)
}

// diff --format codeclimate always exits 0, even with offenders present.
func TestCodeclimateDiffAlwaysExitsZero(t *testing.T) {
	dir := t.TempDir()
	mustRecord(t, dir, buildConfig("", dimDef{
		id: "m", direction: "lower-is-better", gate: "per-file-count",
		command: `echo '{"value": 1, "breakdown": {"a.go:1": 1}}'`,
	}))
	writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
		id: "m", direction: "lower-is-better", gate: "per-file-count",
		command: `echo '{"value": 2, "breakdown": {"a.go:1": 1, "b.go:2": 1}}'`,
	}))

	res := runPawl(t, dir, baseEnv(), "diff", "--format", "codeclimate")
	if res.exit != 0 {
		t.Fatalf("diff exit = %d, want 0 (diff never fails)\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	entries := parseCodeclimate(t, res.stdout)
	if len(entries) == 0 {
		t.Errorf("entries empty, want the current offenders listed even though diff always exits 0")
	}
}

// stdout is pure JSON: it must unmarshal cleanly with nothing else mixed in,
// and never carry the "measuring <id>…" progress line or emoji — those stay
// on stderr / are suppressed entirely, same as --format json.
func TestCodeclimateStdoutPureJSONStderrHasMeasuringLines(t *testing.T) {
	dir := t.TempDir()
	config := buildConfig("", dimDef{
		id: "m", direction: "lower-is-better", gate: "per-file-count",
		command: `echo '{"value": 1, "breakdown": {"a.go:1": 1}}'`,
	})
	mustRecord(t, dir, config)

	res := runPawl(t, dir, baseEnv(), "check", "--format", "codeclimate")
	parseCodeclimate(t, res.stdout) // must parse as pure JSON array, nothing else
	if strings.Contains(res.stdout, "measuring") {
		t.Errorf("stdout carries the measuring progress line, want it confined to stderr: %s", res.stdout)
	}
	if !strings.Contains(res.stderr, "measuring m") {
		t.Errorf("stderr missing the measuring progress line: %s", res.stderr)
	}
}

// A breakdown key with a bare path and no numeric ":line" is skipped — a Code
// Quality entry with no line is not actionable by GitLab's widget.
func TestCodeclimateBreakdownKeyWithoutLineIsSkipped(t *testing.T) {
	dir := t.TempDir()
	config := buildConfig("", dimDef{
		id: "m", direction: "lower-is-better", gate: "per-file-count",
		command: `echo '{"value": 2, "breakdown": {"bare-path-no-line": 1, "a.go:3": 1}}'`,
	})
	mustRecord(t, dir, config)

	res := runPawl(t, dir, baseEnv(), "check", "--format", "codeclimate")
	entries := parseCodeclimate(t, res.stdout)
	if len(entries) != 1 {
		t.Fatalf("entries = %+v, want exactly 1 (the bare-path key has no line and must be skipped)", entries)
	}
	path, line := entryLocation(t, entries[0])
	if path != "a.go" || line != 3 {
		t.Errorf("surviving entry location = %s:%d, want a.go:3", path, line)
	}
}
