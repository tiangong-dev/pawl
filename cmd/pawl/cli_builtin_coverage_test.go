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

func TestBuiltinCoverageCompletenessFloors(t *testing.T) {
	t.Run("min_found rejects fractional counters in each lcov record", func(t *testing.T) {
		dir := t.TempDir()
		report := "SF:src/a.go\nLF:0.5\nLH:0\nend_of_record\nSF:src/b.go\nLF:0.5\nLH:0\nend_of_record\n"
		writeFile(t, dir, "coverage.info", report)
		writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
			id: "coverage", direction: "higher-is-better", builtin: "coverage",
			optionLines: append(coverageOptionLines("", "coverage.info", "lcov", "lines"), "min_found = 1"),
		}))
		res := runPawl(t, dir, baseEnv(), "record")
		if res.exit != 2 || !strings.Contains(res.stderr+res.stdout, "integer") {
			t.Fatalf("record exit=%d, want 2 for fractional per-record counters\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
		}
	})
	t.Run("fractional counters remain compatible without min_found", func(t *testing.T) {
		dir := t.TempDir()
		report := "SF:src/a.go\nLF:0.5\nLH:0\nend_of_record\nSF:src/b.go\nLF:0.5\nLH:0\nend_of_record\n"
		writeFile(t, dir, "coverage.info", report)
		writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
			id: "coverage", direction: "higher-is-better", builtin: "coverage",
			optionLines: coverageOptionLines("", "coverage.info", "lcov", "lines"),
		}))
		res := runPawl(t, dir, baseEnv(), "record")
		if res.exit != 0 {
			t.Fatalf("record exit=%d, want legacy compatibility\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
		}
	})

	t.Run("lcov counts distinct normalized files and selected found units", func(t *testing.T) {
		dir := t.TempDir()
		absA := dirJoin(dir, "src/a.go")
		writeFile(t, dir, "coverage.info", "SF:src/a.go\nLF:4\nLH:3\nend_of_record\nSF:"+absA+"\nLF:4\nLH:3\nend_of_record\nSF:src/b.go\nLF:4\nLH:4\nend_of_record\n")
		writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
			id: "coverage", direction: "higher-is-better", builtin: "coverage",
			optionLines: append(coverageOptionLines("", "coverage.info", "lcov", "lines"), "min_files = 2", "min_found = 12"),
		}))
		res := runPawl(t, dir, baseEnv(), "record")
		if res.exit != 0 {
			t.Fatalf("record exit = %d\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
		}
		if got := readSnapshot(t, dirJoin(dir, "pawl.snapshot.json")).Metrics["coverage"].Value; got != 83.33333333333334 {
			t.Errorf("value = %v, want 83.33333333333334 (duplicate path records remain in percentage denominator)", got)
		}
	})
	for _, tc := range []struct {
		metric, minFound string
		want             float64
	}{
		{"functions", "5", 80},
		{"branches", "10", 70},
	} {
		t.Run("lcov selected denominator "+tc.metric, func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, dir, "coverage.info", lcovTwoRecordFixture)
			writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
				id: "coverage", direction: "higher-is-better", builtin: "coverage",
				optionLines: append(coverageOptionLines("", "coverage.info", "lcov", tc.metric), "min_found = "+tc.minFound),
			}))
			res := runPawl(t, dir, baseEnv(), "record")
			if res.exit != 0 {
				t.Fatalf("record exit = %d\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
			}
			if got := readSnapshot(t, dirJoin(dir, "pawl.snapshot.json")).Metrics["coverage"].Value; got != tc.want {
				t.Errorf("value = %v, want %v", got, tc.want)
			}
		})
	}
	t.Run("cobertura counts distinct filenames and root denominator", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, "coverage.xml", `<coverage line-rate="0.8" lines-valid="10"><packages><package><classes><class filename="a.c"/><class filename="a.c"/><class filename="b.c"/></classes></package></packages></coverage>`)
		writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
			id: "coverage", direction: "higher-is-better", builtin: "coverage",
			optionLines: append(coverageOptionLines("", "coverage.xml", "cobertura", "lines"), "min_files = 2", "min_found = 10"),
		}))
		res := runPawl(t, dir, baseEnv(), "record")
		if res.exit != 0 {
			t.Fatalf("record exit = %d\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
		}
		if got := readSnapshot(t, dirJoin(dir, "pawl.snapshot.json")).Metrics["coverage"].Value; got != 80 {
			t.Errorf("value = %v, want 80", got)
		}
	})
	t.Run("cobertura uses selected branches denominator", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, "coverage.xml", `<coverage line-rate="0.8" branch-rate="0.7" lines-valid="12" branches-valid="10"><packages/></coverage>`)
		writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
			id: "coverage", direction: "higher-is-better", builtin: "coverage",
			optionLines: append(coverageOptionLines("", "coverage.xml", "cobertura", "branches"), "min_found = 10"),
		}))
		res := runPawl(t, dir, baseEnv(), "record")
		if res.exit != 0 {
			t.Fatalf("record exit = %d\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
		}
		if got := readSnapshot(t, dirJoin(dir, "pawl.snapshot.json")).Metrics["coverage"].Value; got != 70 {
			t.Errorf("value = %v, want 70", got)
		}
	})
	t.Run("cobertura source roots resolve and deduplicate class filenames", func(t *testing.T) {
		dir := t.TempDir()
		absoluteSource := strings.ReplaceAll(dirJoin(dir, "src"), "\\", "/")
		writeFile(t, dir, "coverage.xml", fmt.Sprintf(`<coverage line-rate="0.8" lines-valid="10"><packages><package><sources><source>src</source></sources><classes><class filename="a.c"/></classes></package><package><sources><source>%s</source></sources><classes><class filename="a.c"/></classes></package></packages></coverage>`, absoluteSource))
		writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
			id: "coverage", direction: "higher-is-better", builtin: "coverage",
			optionLines: append(coverageOptionLines("", "coverage.xml", "cobertura", "lines"), "min_files = 1"),
		}))
		res := runPawl(t, dir, baseEnv(), "record")
		if res.exit != 0 {
			t.Fatalf("record exit = %d\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
		}
	})
	t.Run("cobertura root sources deduplicate relative and absolute class paths", func(t *testing.T) {
		dir := t.TempDir()
		absoluteClass := strings.ReplaceAll(dirJoin(dir, "src/a.c"), "\\", "/")
		writeFile(t, dir, "coverage.xml", fmt.Sprintf(`<coverage line-rate="0.8" lines-valid="10"><sources><source>src</source></sources><packages><package><classes><class filename="a.c"/><class filename="%s"/></classes></package></packages></coverage>`, absoluteClass))
		writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
			id: "coverage", direction: "higher-is-better", builtin: "coverage",
			optionLines: append(coverageOptionLines("", "coverage.xml", "cobertura", "lines"), "min_files = 2"),
		}))
		res := runPawl(t, dir, baseEnv(), "record")
		if res.exit != 2 || !strings.Contains(res.stderr, "distinct valid source file") {
			t.Fatalf("record exit = %d, want 2 due to one normalized file\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
		}
	})
	t.Run("legacy cobertura rate remains valid with unrelated denominator text", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, "coverage.xml", `<coverage line-rate="0.8" lines-valid="unknown"><packages/></coverage>`)
		writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
			id: "coverage", direction: "higher-is-better", builtin: "coverage",
			optionLines: coverageOptionLines("", "coverage.xml", "cobertura", "lines"),
		}))
		res := runPawl(t, dir, baseEnv(), "record")
		if res.exit != 0 {
			t.Fatalf("record exit = %d\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
		}
	})

	for _, tc := range []struct {
		name, format, report, options string
		wantExit                      int
	}{
		{"lcov file floor", "lcov", "SF:a.c\nLF:3\nLH:2\nend_of_record\n", "min_files = 2", 2},
		{"lcov found floor", "lcov", "SF:a.c\nLF:3\nLH:2\nend_of_record\n", "min_found = 4", 2},
		{"lcov missing source filename", "lcov", "SF:\nLF:3\nLH:2\nend_of_record\n", "min_files = 0", 2},
		{"lcov outside config root", "lcov", "SF:../outside.c\nLF:3\nLH:2\nend_of_record\n", "min_files = 1", 2},
		{"cobertura file floor", "cobertura", `<coverage line-rate="0.8" lines-valid="10"><packages><package><classes><class filename="a.c"/><class filename="a.c"/><class filename="b.c"/></classes></package></packages></coverage>`, "min_files = 3", 2},
		{"cobertura found floor", "cobertura", `<coverage line-rate="0.8" lines-valid="10"><packages><package><classes><class filename="a.c"/></classes></package></packages></coverage>`, "min_found = 11", 2},
		{"cobertura missing filenames", "cobertura", `<coverage line-rate="0.8" lines-valid="10"><packages><package><classes><class/></classes></package></packages></coverage>`, "min_files = 0", 2},
		{"cobertura source outside config root", "cobertura", `<coverage line-rate="0.8" lines-valid="10"><packages><package><sources><source>src</source></sources><classes><class filename="inside.c"/></classes></package><package><sources><source>../outside</source></sources><classes><class filename="outside.c"/></classes></package></packages></coverage>`, "min_files = 2", 2},
		{"cobertura root source outside config root", "cobertura", `<coverage line-rate="0.8" lines-valid="10"><sources><source>../outside</source></sources><packages><package><classes><class filename="outside.c"/></classes></package></packages></coverage>`, "min_files = 1", 2},
		{"cobertura ambiguous source roots", "cobertura", `<coverage line-rate="0.8" lines-valid="10"><packages><package><sources><source>src</source><source>lib</source></sources><classes><class filename="same.c"/></classes></package></packages></coverage>`, "min_files = 1", 2},
		{"cobertura missing denominator", "cobertura", `<coverage line-rate="0.8"><packages><package><classes><class filename="a.c"/></classes></package></packages></coverage>`, "min_found = 1", 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			ext := "info"
			if tc.format == "cobertura" {
				ext = "xml"
			}
			writeFile(t, dir, "coverage."+ext, tc.report)
			options := append(coverageOptionLines("", "coverage."+ext, tc.format, "lines"), tc.options)
			writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
				id: "coverage", direction: "higher-is-better", builtin: "coverage", optionLines: options,
			}))
			res := runPawl(t, dir, baseEnv(), "record")
			if res.exit != tc.wantExit {
				t.Fatalf("record exit = %d, want %d\nstdout=%s\nstderr=%s", res.exit, tc.wantExit, res.stdout, res.stderr)
			}
		})
	}
}

func TestBuiltinCoverageRejectsInvalidCompletenessFloors(t *testing.T) {
	for _, option := range []string{"min_files = -1", "min_files = 1.5", `min_files = "2"`, "min_found = -1", "min_found = 1.5"} {
		t.Run(option, func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, dir, "coverage.info", lcovTwoRecordFixture)
			options := append(coverageOptionLines("", "coverage.info", "lcov", ""), option)
			writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
				id: "coverage", direction: "higher-is-better", builtin: "coverage", optionLines: options,
			}))
			res := runPawl(t, dir, baseEnv(), "validate")
			if res.exit != 2 || !strings.Contains(res.stderr+res.stdout, "non-negative integer") {
				t.Fatalf("exit=%d stdout=%s stderr=%s; want config error about non-negative integer", res.exit, res.stdout, res.stderr)
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
