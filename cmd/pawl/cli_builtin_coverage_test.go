package main

import (
	"fmt"
	"strings"
	"testing"
)

// lcovTwoRecordFixture has two SF records with known, clean-percentage
// LF/LH (lines), FNF/FNH (functions), and BRF/BRH (branches) sums:
// lines 10/8=80%, functions 5/4=80%, branches 10/7=70%.
const lcovTwoRecordFixture = `SF:src/a.go
FNF:2
FNH:2
LF:6
LH:5
BRF:6
BRH:4
end_of_record
SF:src/b.go
FNF:3
FNH:2
LF:4
LH:3
BRF:4
BRH:3
end_of_record
`

// coberturaFixture has a root <coverage> element with line-rate 0.85 and
// branch-rate 0.7 -> 85% lines, 70% branches.
const coberturaFixture = `<?xml version="1.0"?>
<coverage line-rate="0.85" branch-rate="0.7" version="1.9">
  <packages></packages>
</coverage>
`

// coverageOptionLines builds the [dimension.options] lines for a coverage
// builtin dimension. Passing "" for command/file/format/metric omits that
// option entirely, letting config-validation tests isolate one missing
// option.
func coverageOptionLines(command, file, format, metric string) []string {
	var lines []string
	if command != "" {
		lines = append(lines, "command = '"+command+"'")
	}
	if file != "" {
		lines = append(lines, fmt.Sprintf("file = %q", file))
	}
	if format != "" {
		lines = append(lines, fmt.Sprintf("format = %q", format))
	}
	if metric != "" {
		lines = append(lines, fmt.Sprintf("metric = %q", metric))
	}
	return lines
}

// lcov format: metric selects which record pair (LF/LH, FNF/FNH, BRF/BRH) is
// summed into a percentage; lines is the default. unit is "%" and breakdown
// is null.
func TestBuiltinCoverageLcovMetricSelectsWhichPercentage(t *testing.T) {
	cases := []struct {
		name   string
		metric string
		want   float64
	}{
		{"lines is the default metric", "", 80},
		{"functions metric", "functions", 80},
		{"branches metric", "branches", 70},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, dir, "coverage.info", lcovTwoRecordFixture)
			writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
				id: "coverage", direction: "higher-is-better", builtin: "coverage",
				optionLines: coverageOptionLines("", "coverage.info", "lcov", tc.metric),
			}))

			res := runPawl(t, dir, baseEnv(), "record")
			if res.exit != 0 {
				t.Fatalf("record exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
			}
			snap := readSnapshot(t, dirJoin(dir, "pawl.snapshot.json"))
			m := snap.Metrics["coverage"]
			if m.Value != tc.want {
				t.Errorf("value = %v, want %v", m.Value, tc.want)
			}
			if m.Unit != "%" {
				t.Errorf("unit = %q, want %q", m.Unit, "%")
			}
			if m.Breakdown != nil {
				t.Errorf("breakdown = %v, want nil (coverage is a scalar metric)", m.Breakdown)
			}
		})
	}
}

// cobertura format: line-rate and branch-rate attributes (0..1) become
// percentages (line-rate 0.85 -> 85%, branch-rate 0.7 -> 70%).
func TestBuiltinCoverageCoberturaRateAttributesBecomePercentages(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "coverage.xml", coberturaFixture)
	writeFile(t, dir, "pawl.yaml", buildConfig("",
		dimDef{
			id: "coverage-lines", direction: "higher-is-better", builtin: "coverage",
			optionLines: coverageOptionLines("", "coverage.xml", "cobertura", "lines"),
		},
		dimDef{
			id: "coverage-branches", direction: "higher-is-better", builtin: "coverage",
			optionLines: coverageOptionLines("", "coverage.xml", "cobertura", "branches"),
		},
	))

	res := runPawl(t, dir, baseEnv(), "record")
	if res.exit != 0 {
		t.Fatalf("record exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	snap := readSnapshot(t, dirJoin(dir, "pawl.snapshot.json"))
	if snap.Metrics["coverage-lines"].Value != 85 {
		t.Errorf("lines = %v, want 85 (line-rate 0.85 * 100)", snap.Metrics["coverage-lines"].Value)
	}
	if snap.Metrics["coverage-branches"].Value != 70 {
		t.Errorf("branches = %v, want 70 (branch-rate 0.7 * 100)", snap.Metrics["coverage-branches"].Value)
	}
}

// A test run whose tests fail (and so exits non-zero) still produces a
// valid, readable coverage report — pawl does not gate on the command's
// exit code for this builtin.
func TestBuiltinCoverageNonZeroExitWithValidReportStillMeasures(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "fixture.info", lcovTwoRecordFixture)
	writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
		id: "coverage", direction: "higher-is-better", builtin: "coverage",
		optionLines: coverageOptionLines(
			`cp "$PAWL_ROOT/fixture.info" "$PAWL_ROOT/coverage.info"; exit 1`, "coverage.info", "lcov", "",
		),
	}))

	res := runPawl(t, dir, baseEnv(), "record")
	if res.exit != 0 {
		t.Fatalf("record exit = %d, want 0 (non-zero exit with a valid report is still a measurement)\nstdout=%s\nstderr=%s",
			res.exit, res.stdout, res.stderr)
	}
	snap := readSnapshot(t, dirJoin(dir, "pawl.snapshot.json"))
	if snap.Metrics["coverage"].Value != 80 {
		t.Errorf("value = %v, want 80", snap.Metrics["coverage"].Value)
	}
}

// command+file source with stale-artifact protection: a pre-existing report
// at the configured file path is deleted before the command runs, so a
// fresh command output — not the stale fossil — is what gets measured.
func TestBuiltinCoverageCommandFileStaleArtifactProtection(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "fixture.info", lcovTwoRecordFixture)
	writeFile(t, dir, "coverage.info", "SF:stale.go\nLF:10\nLH:1\nend_of_record\n") // stale: would read as 10%
	writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
		id: "coverage", direction: "higher-is-better", builtin: "coverage",
		optionLines: coverageOptionLines(
			`cp "$PAWL_ROOT/fixture.info" "$PAWL_ROOT/coverage.info"`, "coverage.info", "lcov", "",
		),
	}))

	res := runPawl(t, dir, baseEnv(), "record")
	if res.exit != 0 {
		t.Fatalf("record exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	snap := readSnapshot(t, dirJoin(dir, "pawl.snapshot.json"))
	if snap.Metrics["coverage"].Value != 80 {
		t.Errorf("value = %v, want 80 (fresh command output, not the stale 10%% fossil)", snap.Metrics["coverage"].Value)
	}
}

// A report with zero of the requested unit found is a measurement failure,
// never a silent 0 or 100: lcov with a zero (or entirely missing) LF total,
// and cobertura missing the rate attribute for the requested metric.
func TestBuiltinCoverageZeroFoundDataIsMeasurementFailureNotSilentZeroOrHundred(t *testing.T) {
	cases := []struct {
		name    string
		ext     string
		format  string
		content string
	}{
		{"lcov LF zero", "info", "lcov", "SF:a.go\nLF:0\nLH:0\nend_of_record\n"},
		{"lcov missing LF entirely", "info", "lcov", "SF:a.go\nend_of_record\n"},
		{"cobertura missing line-rate", "xml", "cobertura", `<?xml version="1.0"?><coverage branch-rate="0.5"></coverage>`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			name := "coverage." + tc.ext
			writeFile(t, dir, name, tc.content)
			writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
				id: "coverage", direction: "higher-is-better", builtin: "coverage",
				optionLines: coverageOptionLines("", name, tc.format, "lines"),
			}))
			res := runPawl(t, dir, baseEnv(), "record")
			if res.exit != 2 {
				t.Fatalf("record exit = %d, want 2 (zero coverage data must never read as a silent 0%% or 100%%)\nstdout=%s\nstderr=%s",
					res.exit, res.stdout, res.stderr)
			}
			if !strings.Contains(res.stderr, "measuring coverage…") {
				t.Errorf("stderr missing progress line: the dimension must actually run (config-valid) and fail reading the report, not be rejected up front: %s", res.stderr)
			}
		})
	}
}

// Config errors are rejected at load time, before any measurement runs, and
// must name coverage as the recognized builtin (not fall through to
// "unknown builtin"): missing file, missing/invalid format, and metric
// "functions" combined with format "cobertura" (functions is lcov-only).
func TestBuiltinCoverageConfigErrorsAreRejectedAtLoadNotUnknownBuiltin(t *testing.T) {
	cases := []struct {
		name        string
		optionLines []string
	}{
		{"missing file", coverageOptionLines("", "", "lcov", "")},
		{"missing format", coverageOptionLines("", "coverage.info", "", "")},
		{"invalid format", coverageOptionLines("", "coverage.info", "bogus", "")},
		{"functions metric with cobertura format", coverageOptionLines("", "coverage.xml", "cobertura", "functions")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, dir, "coverage.info", lcovTwoRecordFixture)
			writeFile(t, dir, "coverage.xml", coberturaFixture)
			writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
				id: "coverage", direction: "higher-is-better", builtin: "coverage",
				optionLines: tc.optionLines,
			}))
			res := runPawl(t, dir, baseEnv(), "check")
			if res.exit != 2 {
				t.Fatalf("exit = %d, want 2\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
			}
			if strings.Contains(res.stderr, "unknown builtin") {
				t.Errorf("stderr reports \"unknown builtin\" — coverage must be a recognized builtin whose %s is what's invalid: %s", tc.name, res.stderr)
			}
		})
	}
}
