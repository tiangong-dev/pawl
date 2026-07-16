package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// sarifRegion, sarifArtifactLocation, sarifPhysicalLocation, sarifLocation,
// sarifResult, sarifRun and sarifLog mirror the SARIF log shape the sarif
// builtin parses: a top-level `runs` array, each run's `results`, and each
// result's `ruleId`/`level`/first `locations[0].physicalLocation`.
type sarifRegion struct {
	StartLine int `json:"startLine,omitempty"`
}

type sarifArtifactLocation struct {
	URI string `json:"uri"`
}

type sarifPhysicalLocation struct {
	ArtifactLocation sarifArtifactLocation `json:"artifactLocation"`
	Region           *sarifRegion          `json:"region,omitempty"`
}

type sarifLocation struct {
	PhysicalLocation *sarifPhysicalLocation `json:"physicalLocation,omitempty"`
}

type sarifResult struct {
	RuleID    string          `json:"ruleId,omitempty"`
	Level     string          `json:"level,omitempty"`
	Locations []sarifLocation `json:"locations,omitempty"`
}

type sarifRun struct {
	Results []sarifResult `json:"results"`
}

type sarifLog struct {
	Runs []sarifRun `json:"runs"`
}

// sarifResultAt builds a result with a single physicalLocation at the given
// URI/line — the common case for these fixtures.
func sarifResultAt(ruleID, level, uri string, line int) sarifResult {
	return sarifResult{
		RuleID: ruleID,
		Level:  level,
		Locations: []sarifLocation{{PhysicalLocation: &sarifPhysicalLocation{
			ArtifactLocation: sarifArtifactLocation{URI: uri},
			Region:           &sarifRegion{StartLine: line},
		}}},
	}
}

// writeSarifFixtureCommand marshals a SARIF log fixture into the config dir
// and returns a `cat` command a dimension's `command` option can run to emit
// it verbatim on stdout — the adapter owns no scanner binary, only the format.
func writeSarifFixtureCommand(t *testing.T, dir, name string, log sarifLog) string {
	t.Helper()
	b, err := json.Marshal(log)
	if err != nil {
		t.Fatalf("marshal sarif fixture: %v", err)
	}
	writeFile(t, dir, name, string(b))
	return `cat "$PAWL_ROOT/` + name + `"`
}

// sarifOptionLines builds the [dimension.options] lines for a sarif builtin
// dimension: the shared source (command and/or file) plus the optional
// rules/levels filters. Passing "" for command/file omits that option;
// passing nil for rules/levels omits the filter entirely.
func sarifOptionLines(command, file string, rules, levels []string) []string {
	var lines []string
	if command != "" {
		lines = append(lines, "command = '"+command+"'")
	}
	if file != "" {
		lines = append(lines, `file = "`+file+`"`)
	}
	if rules != nil {
		b, _ := json.Marshal(rules)
		lines = append(lines, "rules = "+string(b))
	}
	if levels != nil {
		b, _ := json.Marshal(levels)
		lines = append(lines, "levels = "+string(b))
	}
	return lines
}

// Happy path: value is the total result count across all runs, unit is
// "findings", and breakdown keys are "<path>:<line>" — two results in the
// same file at different lines get distinct keys, and a result in another
// file gets its own key.
func TestBuiltinSarifHappyPathCountsResultsAndBuildsPerFileLineBreakdown(t *testing.T) {
	dir := t.TempDir()
	log := sarifLog{Runs: []sarifRun{{Results: []sarifResult{
		sarifResultAt("R1", "error", "a.go", 5),
		sarifResultAt("R2", "warning", "a.go", 12),
		sarifResultAt("R3", "note", "b.go", 3),
	}}}}
	command := writeSarifFixtureCommand(t, dir, "sarif-out.json", log)

	writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
		id: "sarif-findings", direction: "lower-is-better",
		builtin: "sarif", optionLines: sarifOptionLines(command, "", nil, nil),
	}))

	res := runPawl(t, dir, baseEnv(), "record")
	if res.exit != 0 {
		t.Fatalf("record exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	snap := readSnapshot(t, dirJoin(dir, "pawl.snapshot.json"))
	m := snap.Metrics["sarif-findings"]

	if m.Value != 3 {
		t.Errorf("value = %v, want 3", m.Value)
	}
	if m.Unit != "findings" {
		t.Errorf("unit = %q, want %q", m.Unit, "findings")
	}
	want := map[string]float64{"a.go:5": 1, "a.go:12": 1, "b.go:3": 1}
	if len(m.Breakdown) != len(want) {
		t.Fatalf("breakdown = %v, want %v", m.Breakdown, want)
	}
	for k, v := range want {
		if m.Breakdown[k] != v {
			t.Errorf("breakdown[%q] = %v, want %v (full breakdown: %v)", k, m.Breakdown[k], v, m.Breakdown)
		}
	}
}

// A scanner that emits a well-formed SARIF log then exits non-zero is still
// a measurement — SARIF-producing scanners conventionally exit non-zero to
// signal findings, and pawl does not gate on the command's exit code for
// these report-format builtins.
func TestBuiltinSarifNonZeroExitWithValidReportStillMeasures(t *testing.T) {
	dir := t.TempDir()
	log := sarifLog{Runs: []sarifRun{{Results: []sarifResult{
		sarifResultAt("R1", "error", "a.go", 1),
	}}}}
	command := writeSarifFixtureCommand(t, dir, "sarif-out.json", log)

	writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
		id: "sarif-findings", direction: "lower-is-better",
		builtin: "sarif", optionLines: sarifOptionLines(command+"; exit 1", "", nil, nil),
	}))

	res := runPawl(t, dir, baseEnv(), "record")
	if res.exit != 0 {
		t.Fatalf("record exit = %d, want 0 (non-zero exit with a valid report is still a measurement)\nstdout=%s\nstderr=%s",
			res.exit, res.stdout, res.stderr)
	}
	snap := readSnapshot(t, dirJoin(dir, "pawl.snapshot.json"))
	if snap.Metrics["sarif-findings"].Value != 1 {
		t.Errorf("value = %v, want 1", snap.Metrics["sarif-findings"].Value)
	}
}

// File-mode stale-artifact protection: a pre-existing report is deleted
// before the command runs, so a command that does not rewrite it (`true`)
// leaves no report — a measurement failure, and the prior snapshot value
// must not be clobbered with a stale or zero value.
func TestBuiltinSarifFileModeStaleReportNotRewrittenIsMeasurementFailure(t *testing.T) {
	dir := t.TempDir()
	log := sarifLog{Runs: []sarifRun{{Results: []sarifResult{
		sarifResultAt("R1", "error", "a.go", 1),
	}}}}
	b, err := json.Marshal(log)
	if err != nil {
		t.Fatalf("marshal sarif fixture: %v", err)
	}
	writeFile(t, dir, "fixture-report.sarif", string(b))

	goodConfig := buildConfig("", dimDef{
		id: "sarif-findings", direction: "lower-is-better", builtin: "sarif",
		optionLines: sarifOptionLines(
			`cp "$PAWL_ROOT/fixture-report.sarif" "$PAWL_ROOT/out.sarif"`, "out.sarif", nil, nil,
		),
	})
	mustRecord(t, dir, goodConfig)
	snapPath := dirJoin(dir, "pawl.snapshot.json")
	before := readSnapshot(t, snapPath)
	if before.Metrics["sarif-findings"].Value != 1 {
		t.Fatalf("precondition: initial value = %v, want 1", before.Metrics["sarif-findings"].Value)
	}

	badConfig := buildConfig("", dimDef{
		id: "sarif-findings", direction: "lower-is-better", builtin: "sarif",
		optionLines: sarifOptionLines("true", "out.sarif", nil, nil),
	})
	writeFile(t, dir, "pawl.yaml", badConfig)
	res := runPawl(t, dir, baseEnv(), "record")
	if res.exit != 2 {
		t.Fatalf("record exit = %d, want 2 (report never rewritten after stale-artifact deletion)\nstdout=%s\nstderr=%s",
			res.exit, res.stdout, res.stderr)
	}

	after := readSnapshot(t, snapPath)
	if after.Metrics["sarif-findings"].Value != 1 {
		t.Errorf("snapshot value after failed measurement = %v, want unchanged 1 (not clobbered)", after.Metrics["sarif-findings"].Value)
	}
}

// rules filters which results count: only results whose ruleId is listed
// contribute to value and breakdown.
func TestBuiltinSarifRulesFilterCountsOnlyListedRuleIDs(t *testing.T) {
	dir := t.TempDir()
	log := sarifLog{Runs: []sarifRun{{Results: []sarifResult{
		sarifResultAt("keep-me", "error", "a.go", 1),
		sarifResultAt("ignore-me", "error", "a.go", 2),
		sarifResultAt("keep-me", "error", "a.go", 5),
	}}}}
	command := writeSarifFixtureCommand(t, dir, "sarif-out.json", log)

	writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
		id: "sarif-findings", direction: "lower-is-better",
		builtin: "sarif", optionLines: sarifOptionLines(command, "", []string{"keep-me"}, nil),
	}))

	res := runPawl(t, dir, baseEnv(), "record")
	if res.exit != 0 {
		t.Fatalf("record exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	snap := readSnapshot(t, dirJoin(dir, "pawl.snapshot.json"))
	m := snap.Metrics["sarif-findings"]

	if m.Value != 2 {
		t.Errorf("value = %v, want 2 (only the two keep-me results)", m.Value)
	}
	want := map[string]float64{"a.go:1": 1, "a.go:5": 1}
	if len(m.Breakdown) != len(want) {
		t.Fatalf("breakdown = %v, want %v (the ignore-me result must not appear)", m.Breakdown, want)
	}
	for k, v := range want {
		if m.Breakdown[k] != v {
			t.Errorf("breakdown[%q] = %v, want %v", k, m.Breakdown[k], v)
		}
	}
}

// levels filters which results count, and a result with no level is treated
// as "warning" (the SARIF default) — so it is counted when "warning" is in
// the filter list, and a result with an explicit level not in the list is
// excluded.
func TestBuiltinSarifLevelsFilterTreatsMissingLevelAsWarning(t *testing.T) {
	dir := t.TempDir()
	log := sarifLog{Runs: []sarifRun{{Results: []sarifResult{
		sarifResultAt("R1", "", "a.go", 1),        // no level -> treated as warning, kept
		sarifResultAt("R2", "error", "a.go", 2),   // explicit error, filtered out
		sarifResultAt("R3", "warning", "b.go", 3), // explicit warning, kept
	}}}}
	command := writeSarifFixtureCommand(t, dir, "sarif-out.json", log)

	writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
		id: "sarif-findings", direction: "lower-is-better",
		builtin: "sarif", optionLines: sarifOptionLines(command, "", nil, []string{"warning"}),
	}))

	res := runPawl(t, dir, baseEnv(), "record")
	if res.exit != 0 {
		t.Fatalf("record exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	snap := readSnapshot(t, dirJoin(dir, "pawl.snapshot.json"))
	m := snap.Metrics["sarif-findings"]

	if m.Value != 2 {
		t.Errorf("value = %v, want 2 (the no-level result counts as warning, the explicit-error result is filtered out)", m.Value)
	}
	want := map[string]float64{"a.go:1": 1, "b.go:3": 1}
	if len(m.Breakdown) != len(want) {
		t.Fatalf("breakdown = %v, want %v", m.Breakdown, want)
	}
	for k, v := range want {
		if m.Breakdown[k] != v {
			t.Errorf("breakdown[%q] = %v, want %v", k, m.Breakdown[k], v)
		}
	}
}

// A file:// scheme prefix on artifactLocation.uri is stripped, then the
// remaining path is relativized against the config dir in the breakdown key.
func TestBuiltinSarifFileSchemeURIStrippedAndRelativized(t *testing.T) {
	dir := t.TempDir()
	absURI := "file://" + filepath.Join(dir, "src", "a.go")
	log := sarifLog{Runs: []sarifRun{{Results: []sarifResult{
		sarifResultAt("R1", "error", absURI, 12),
	}}}}
	command := writeSarifFixtureCommand(t, dir, "sarif-out.json", log)

	writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
		id: "sarif-findings", direction: "lower-is-better",
		builtin: "sarif", optionLines: sarifOptionLines(command, "", nil, nil),
	}))

	res := runPawl(t, dir, baseEnv(), "record")
	if res.exit != 0 {
		t.Fatalf("record exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	snap := readSnapshot(t, dirJoin(dir, "pawl.snapshot.json"))
	m := snap.Metrics["sarif-findings"]
	want := "src/a.go:12"
	if _, ok := m.Breakdown[want]; !ok {
		t.Errorf("breakdown = %v, want key %q (file:// prefix stripped, path relativized to config dir)", m.Breakdown, want)
	}
}

// A result with no physicalLocation still counts toward value but is
// omitted from the breakdown — there is nothing to attribute it to.
func TestBuiltinSarifResultWithNoPhysicalLocationCountsButOmittedFromBreakdown(t *testing.T) {
	dir := t.TempDir()
	log := sarifLog{Runs: []sarifRun{{Results: []sarifResult{
		{RuleID: "R1", Level: "error"}, // no Locations at all
		sarifResultAt("R2", "error", "a.go", 1),
	}}}}
	command := writeSarifFixtureCommand(t, dir, "sarif-out.json", log)

	writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
		id: "sarif-findings", direction: "lower-is-better",
		builtin: "sarif", optionLines: sarifOptionLines(command, "", nil, nil),
	}))

	res := runPawl(t, dir, baseEnv(), "record")
	if res.exit != 0 {
		t.Fatalf("record exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	snap := readSnapshot(t, dirJoin(dir, "pawl.snapshot.json"))
	m := snap.Metrics["sarif-findings"]

	if m.Value != 2 {
		t.Errorf("value = %v, want 2 (the location-less result still counts toward value)", m.Value)
	}
	if len(m.Breakdown) != 1 {
		t.Errorf("breakdown = %v, want exactly 1 key (the location-less result must be absent)", m.Breakdown)
	}
	if m.Breakdown["a.go:1"] != 1 {
		t.Errorf(`breakdown["a.go:1"] = %v, want 1`, m.Breakdown["a.go:1"])
	}
}

// A document that parses as JSON but lacks a top-level `runs` array is the
// wrong shape (an empty run is {"runs":[]}, not {}) and a measurement
// failure — as is stdout that isn't JSON at all. Both must fail while
// actually running the measurement, not be rejected as invalid config.
func TestBuiltinSarifWrongShapeIsMeasurementFailure(t *testing.T) {
	cases := []struct {
		name    string
		content string
	}{
		{"no runs array", `{}`},
		{"non-JSON stdout", "not json at all"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, dir, "sarif-out.json", tc.content)
			writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
				id: "sarif-findings", direction: "lower-is-better",
				builtin:     "sarif",
				optionLines: sarifOptionLines(`cat "$PAWL_ROOT/sarif-out.json"`, "", nil, nil),
			}))
			res := runPawl(t, dir, baseEnv(), "record")
			if res.exit != 2 {
				t.Fatalf("record exit = %d, want 2\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
			}
			if !strings.Contains(res.stderr, "measuring sarif-findings…") {
				t.Errorf("stderr missing progress line: the dimension must actually run (config-valid) and fail reading the report, not be rejected up front: %s", res.stderr)
			}
		})
	}
}

// Config errors are rejected at load time, before any measurement runs, and
// must name sarif as the recognized builtin (not fall through to "unknown
// builtin"): neither command nor file given, and a levels entry outside the
// error/warning/note/none vocabulary.
func TestBuiltinSarifConfigErrorsAreRejectedAtLoadNotUnknownBuiltin(t *testing.T) {
	cases := []struct {
		name        string
		optionLines []string
	}{
		{"neither command nor file", sarifOptionLines("", "", nil, nil)},
		{"invalid levels entry", sarifOptionLines(`cat "$PAWL_ROOT/sarif-out.json"`, "", nil, []string{"bogus"})},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
				id: "sarif-findings", direction: "lower-is-better", builtin: "sarif",
				optionLines: tc.optionLines,
			}))
			res := runPawl(t, dir, baseEnv(), "check")
			if res.exit != 2 {
				t.Fatalf("exit = %d, want 2\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
			}
			if strings.Contains(res.stderr, "unknown builtin") {
				t.Errorf("stderr reports \"unknown builtin\" — sarif must be a recognized builtin whose %s is what's invalid: %s", tc.name, res.stderr)
			}
		})
	}
}

// Works end-to-end as a per-file-count gate: one file's offender count rises
// while another's falls to zero, leaving the scalar total unchanged — the
// per-file check must still fail because the net-zero total hides a real
// regression in the first file.
func TestBuiltinSarifPerFileCountGateEndToEnd(t *testing.T) {
	dir := t.TempDir()
	baseLog := sarifLog{Runs: []sarifRun{{Results: []sarifResult{
		sarifResultAt("R1", "error", "a.go", 1),
		sarifResultAt("R1", "error", "b.go", 2),
	}}}}
	baseCommand := writeSarifFixtureCommand(t, dir, "sarif-base.json", baseLog)
	mustRecord(t, dir, buildConfig("", dimDef{
		id: "sarif-findings", direction: "lower-is-better", gate: "per-file-count",
		builtin: "sarif", optionLines: sarifOptionLines(baseCommand, "", nil, nil),
	}))

	worseLog := sarifLog{Runs: []sarifRun{{Results: []sarifResult{
		sarifResultAt("R1", "error", "a.go", 1),
		sarifResultAt("R1", "error", "a.go", 2), // a.go gains a second offender
		// b.go's offender is gone: total stays 2, but a.go's per-file count rose
	}}}}
	worseCommand := writeSarifFixtureCommand(t, dir, "sarif-worse.json", worseLog)
	writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
		id: "sarif-findings", direction: "lower-is-better", gate: "per-file-count",
		builtin: "sarif", optionLines: sarifOptionLines(worseCommand, "", nil, nil),
	}))

	res := runPawl(t, dir, baseEnv(), "check")
	if res.exit != 1 {
		t.Fatalf("check exit = %d, want 1 (a.go's offender count rose 1 -> 2 even though the total is unchanged)\nstdout=%s\nstderr=%s",
			res.exit, res.stdout, res.stderr)
	}
	if !strings.Contains(res.stdout, "a.go  1 → 2") {
		t.Errorf("stdout missing per-file detail line %q: %s", "a.go  1 → 2", res.stdout)
	}
}
