package main

import (
	"fmt"
	"strings"
	"testing"
)

// jscpdReportFixture is the shape of jscpd's `--reporters json` report that
// the jscpd builtin reads: only the field the SPEC promises to read.
const jscpdReportFixture = `{"statistics":{"total":{"duplicatedLines": 123}}}`

// jscpdOptionLines builds the option lines for a jscpd builtin dimension:
// `command` as a single-quoted string (avoids shell escaping pain) and the
// `report` path.
func jscpdOptionLines(command, report string) []string {
	return []string{
		"command = '" + command + "'",
		fmt.Sprintf("report = %q", report),
	}
}

// Happy path: the command writes the jscpd report at the configured path;
// value is statistics.total.duplicatedLines, unit is "duplicated lines",
// and breakdown is null — jscpd offers no per-file/line breakdown.
func TestBuiltinJscpdHappyPath(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "fixture-report.json", jscpdReportFixture)

	writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
		id: "dup-lines", direction: "lower-is-better",
		builtin: "jscpd",
		optionLines: jscpdOptionLines(
			`cp "$PAWL_ROOT/fixture-report.json" "$PAWL_ROOT/jscpd-report.json"`,
			"jscpd-report.json",
		),
	}))

	res := runPawl(t, dir, baseEnv(), "record")
	if res.exit != 0 {
		t.Fatalf("record exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	snapPath := dirJoin(dir, "pawl.snapshot.json")
	snap := readSnapshot(t, snapPath)
	m := snap.Metrics["dup-lines"]

	if m.Value != 123 {
		t.Errorf("value = %v, want 123", m.Value)
	}
	if m.Unit != "duplicated lines" {
		t.Errorf("unit = %q, want %q", m.Unit, "duplicated lines")
	}
	if m.Breakdown != nil {
		t.Errorf("breakdown = %v, want nil (jscpd has no per-file breakdown)", m.Breakdown)
	}
	raw := readFile(t, snapPath)
	if !strings.Contains(raw, `"breakdown": null`) {
		t.Errorf("snapshot JSON missing literal `\"breakdown\": null`:\n%s", raw)
	}
}

// A command that exits 0 but never writes the report file is a measurement
// failure, not a zero — an existing snapshot value must not be clobbered,
// mirroring TestExecAdapterNonZeroExitAbortsAndDoesNotRecordZero.
func TestBuiltinJscpdMissingReportDoesNotRecordZero(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "fixture-report.json", jscpdReportFixture)

	goodConfig := buildConfig("", dimDef{
		id: "dup-lines", direction: "lower-is-better", builtin: "jscpd",
		optionLines: jscpdOptionLines(
			`cp "$PAWL_ROOT/fixture-report.json" "$PAWL_ROOT/jscpd-report.json"`,
			"jscpd-report.json",
		),
	})
	mustRecord(t, dir, goodConfig)
	snapPath := dirJoin(dir, "pawl.snapshot.json")
	before := readSnapshot(t, snapPath)
	if before.Metrics["dup-lines"].Value != 123 {
		t.Fatalf("precondition: initial value = %v, want 123", before.Metrics["dup-lines"].Value)
	}

	noReportConfig := buildConfig("", dimDef{
		id: "dup-lines", direction: "lower-is-better", builtin: "jscpd",
		optionLines: jscpdOptionLines("true", "jscpd-report.json"),
	})
	writeFile(t, dir, "pawl.yaml", noReportConfig)
	res := runPawl(t, dir, baseEnv(), "record")
	if res.exit != 2 {
		t.Fatalf("record exit = %d, want 2 (report file never produced)\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}

	after := readSnapshot(t, snapPath)
	if after.Metrics["dup-lines"].Value != 123 {
		t.Errorf("snapshot value after failed measurement = %v, want unchanged 123 (not recorded as 0)", after.Metrics["dup-lines"].Value)
	}
}

// A report that exists but doesn't parse as JSON, or parses without
// statistics.total.duplicatedLines, is a measurement failure.
func TestBuiltinJscpdMalformedReportIsMeasurementFailure(t *testing.T) {
	cases := []struct {
		name    string
		content string
	}{
		{"not valid JSON", "not json at all"},
		{"missing statistics.total.duplicatedLines", `{"statistics":{"total":{}}}`},
		{"missing statistics entirely", `{"foo": 1}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, dir, "fixture-report.json", tc.content)
			writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
				id: "dup-lines", direction: "lower-is-better", builtin: "jscpd",
				optionLines: jscpdOptionLines(
					`cp "$PAWL_ROOT/fixture-report.json" "$PAWL_ROOT/jscpd-report.json"`,
					"jscpd-report.json",
				),
			}))
			res := runPawl(t, dir, baseEnv(), "record")
			if res.exit != 2 {
				t.Fatalf("record exit = %d, want 2\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
			}
			if !strings.Contains(res.stderr, "measuring dup-lines…") {
				t.Errorf("stderr missing progress line: the dimension must actually run (config-valid) and fail reading the report, not be rejected up front: %s", res.stderr)
			}
		})
	}
}

// A non-zero command exit is a measurement failure regardless of what the
// report contains — pawl does not use jscpd's own --threshold gating, but
// it does require jscpd's own exit 0.
func TestBuiltinJscpdNonZeroExitIsMeasurementFailure(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "fixture-report.json", jscpdReportFixture)
	writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
		id: "dup-lines", direction: "lower-is-better", builtin: "jscpd",
		optionLines: jscpdOptionLines(
			`cp "$PAWL_ROOT/fixture-report.json" "$PAWL_ROOT/jscpd-report.json"; exit 1`,
			"jscpd-report.json",
		),
	}))
	res := runPawl(t, dir, baseEnv(), "record")
	if res.exit != 2 {
		t.Fatalf("record exit = %d, want 2 (jscpd exit 1 is a measurement failure, even with a valid report written)\nstdout=%s\nstderr=%s",
			res.exit, res.stdout, res.stderr)
	}
	if !strings.Contains(res.stderr, "measuring dup-lines…") {
		t.Errorf("stderr missing progress line: the dimension must actually run (config-valid) and fail on the command's exit code, not be rejected up front: %s", res.stderr)
	}
}

// jscpd builtin requires both command and report; either missing is a
// config error — the rejection must name the missing option, not fall
// through to "unknown builtin" (which would mean jscpd itself was never
// recognized).
func TestBuiltinJscpdMissingOptionsAreConfigErrors(t *testing.T) {
	cases := []struct {
		name        string
		optionLines []string
	}{
		{"missing command", []string{`report = "jscpd-report.json"`}},
		{"missing report", []string{"command = 'true'"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
				id: "dup-lines", direction: "lower-is-better", builtin: "jscpd",
				optionLines: tc.optionLines,
			}))
			res := runPawl(t, dir, baseEnv(), "check")
			if res.exit != 2 {
				t.Fatalf("exit = %d, want 2\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
			}
			if strings.Contains(res.stderr, "unknown builtin") {
				t.Errorf("stderr reports \"unknown builtin\" — jscpd must be a recognized builtin whose %s option is what's missing: %s", tc.name, res.stderr)
			}
		})
	}
}

// report is resolved relative to the config dir, including through a
// subdirectory the command creates on the fly.
func TestBuiltinJscpdReportPathRelativeToConfigDirSubdirectory(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "fixture-report.json", jscpdReportFixture)
	writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
		id: "dup-lines", direction: "lower-is-better", builtin: "jscpd",
		optionLines: jscpdOptionLines(
			`mkdir -p "$PAWL_ROOT/.pawl/jscpd" && cp "$PAWL_ROOT/fixture-report.json" "$PAWL_ROOT/.pawl/jscpd/jscpd-report.json"`,
			".pawl/jscpd/jscpd-report.json",
		),
	}))
	res := runPawl(t, dir, baseEnv(), "record")
	if res.exit != 0 {
		t.Fatalf("record exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	snap := readSnapshot(t, dirJoin(dir, "pawl.snapshot.json"))
	if snap.Metrics["dup-lines"].Value != 123 {
		t.Errorf("value = %v, want 123 (report resolved relative to config dir via a subdirectory)", snap.Metrics["dup-lines"].Value)
	}
}
