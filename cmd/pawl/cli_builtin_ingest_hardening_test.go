package main

import (
	"strings"
	"testing"
)

// Regression tests hardening the ingest builtins against inputs that could make
// "could not measure" read as "measured zero / perfect" — pawl's core honesty
// contract. Each guards a specific hole.

// A malformed lcov report must never yield a bogus percentage. Negative
// counters would make -1/-1 read as 100%; NaN/Inf counters would store a
// non-finite value that never compares "worse"; hit > found is impossible.
func TestBuiltinCoverageLcovMalformedCountersAreMeasurementFailure(t *testing.T) {
	cases := map[string]string{
		"negative counters": "SF:a.go\nLF:-1\nLH:-1\nend_of_record\n",
		"NaN hit":           "SF:a.go\nLF:1\nLH:NaN\nend_of_record\n",
		"Inf found":         "SF:a.go\nLF:Inf\nLH:1\nend_of_record\n",
		"hit exceeds found": "SF:a.go\nLF:2\nLH:5\nend_of_record\n",
	}
	for name, fixture := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, dir, "cov.info", fixture)
			writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
				id: "cov", direction: "higher-is-better", builtin: "coverage",
				optionLines: coverageOptionLines("", "cov.info", "lcov", "lines"),
			}))
			res := runPawl(t, dir, baseEnv(), "record")
			if res.exit != 2 {
				t.Fatalf("record exit = %d, want 2 (malformed lcov must fail, not read as a percentage)\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
			}
		})
	}
}

// A cobertura report is the root <coverage> element; some other XML that merely
// carries a line-rate attribute, or a rate outside [0,1], must fail loud.
func TestBuiltinCoverageCoberturaShapeAndRangeAreMeasurementFailure(t *testing.T) {
	cases := map[string]string{
		"non-coverage root": `<not-coverage line-rate="1.0"/>`,
		"rate above one":    `<coverage line-rate="1.5"/>`,
		"NaN rate":          `<coverage line-rate="NaN"/>`,
		"negative rate":     `<coverage line-rate="-0.1"/>`,
	}
	for name, fixture := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, dir, "cov.xml", fixture)
			writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
				id: "cov", direction: "higher-is-better", builtin: "coverage",
				optionLines: coverageOptionLines("", "cov.xml", "cobertura", "lines"),
			}))
			res := runPawl(t, dir, baseEnv(), "record")
			if res.exit != 2 {
				t.Fatalf("record exit = %d, want 2 (%s must fail, not read as coverage)\nstdout=%s\nstderr=%s", res.exit, name, res.stdout, res.stderr)
			}
		})
	}
}

// XML that parses but is not rooted at <testsuites>/<testsuite> is not a JUnit
// report, even if it happens to contain a <testcase>.
func TestBuiltinJUnitNonJUnitRootIsMeasurementFailure(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "j.xml", `<not-junit><testcase name="looks-passing"/></not-junit>`)
	writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
		id: "tests", direction: "higher-is-better", builtin: "junit",
		optionLines: junitOptionLines(`cat "$PAWL_ROOT/j.xml"`, "", "passing"),
	}))
	res := runPawl(t, dir, baseEnv(), "record")
	if res.exit != 2 {
		t.Fatalf("record exit = %d, want 2 (a <not-junit> root must not measure as 1 passing test)\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	if !strings.Contains(res.stderr, "measuring tests…") {
		t.Errorf("stderr missing progress line — the dimension must run and fail on shape, not be rejected at config load: %s", res.stderr)
	}
}

// A <testcase> that is simultaneously failed and skipped is contradictory; the
// counts (failures/skipped/passing) can't all be honest, so it fails loud.
func TestBuiltinJUnitContradictoryTestcaseIsMeasurementFailure(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "j.xml", `<testsuite><testcase name="x"><failure/><skipped/></testcase></testsuite>`)
	writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
		id: "tests", direction: "lower-is-better", builtin: "junit",
		optionLines: junitOptionLines(`cat "$PAWL_ROOT/j.xml"`, "", "skipped"),
	}))
	res := runPawl(t, dir, baseEnv(), "record")
	if res.exit != 2 {
		t.Fatalf("record exit = %d, want 2 (a testcase both failed and skipped is contradictory)\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
}

// lcov summary counters come in pairs per file record (LF+LH for lines,
// FNF+FNH for functions, BRF+BRH for branches). A report where the requested
// metric's found-counters and hit-counters appear a different number of
// times — one counter present without its partner, on one record or across
// several — is truncated or otherwise malformed and must fail measurement
// rather than read as a (dishonestly low) percentage.
func TestBuiltinCoverageLcovUnpairedCountersAreMeasurementFailure(t *testing.T) {
	cases := map[string]string{
		"LF present, no LH anywhere": "SF:a.go\nLF:10\nend_of_record\n",
		"one of two records missing LH": "SF:a.go\nLF:6\nLH:5\nend_of_record\n" +
			"SF:b.go\nLF:4\nend_of_record\n",
	}
	for name, fixture := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, dir, "cov.info", fixture)
			writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
				id: "cov", direction: "higher-is-better", builtin: "coverage",
				optionLines: coverageOptionLines("", "cov.info", "lcov", "lines"),
			}))
			res := runPawl(t, dir, baseEnv(), "record")
			if res.exit != 2 {
				t.Fatalf("record exit = %d, want 2 (LF/LH must appear the same number of times, or this is a truncated report, not a measured percentage)\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
			}
		})
	}
}

// TestBuiltinCoverageLcovExplicitZeroHitIsHonestMeasurement is the control:
// LH:0 is an explicit hit-counter (present, just zero), not a missing one —
// LF and LH still appear the same number of times, so the record is a
// genuinely honest zero, not a measurement failure.
func TestBuiltinCoverageLcovExplicitZeroHitIsHonestMeasurement(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "cov.info", "SF:a.go\nLF:10\nLH:0\nend_of_record\n")
	writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
		id: "cov", direction: "higher-is-better", builtin: "coverage",
		optionLines: coverageOptionLines("", "cov.info", "lcov", "lines"),
	}))
	res := runPawl(t, dir, baseEnv(), "record")
	if res.exit != 0 {
		t.Fatalf("record exit = %d, want 0 (LH:0 is an explicit, honest zero, not a missing counter)\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	snap := readSnapshot(t, dirJoin(dir, "pawl.snapshot.json"))
	if snap.Metrics["cov"].Value != 0 {
		t.Errorf("value = %v, want 0", snap.Metrics["cov"].Value)
	}
}

// SARIF filter lists are string lists with constrained values; a non-string
// entry must be a config error, not silently dropped into "no filter" (which
// would broaden the gate behind the user's back).
func TestBuiltinSarifNonStringFilterListsAreConfigErrors(t *testing.T) {
	cases := map[string][]string{
		"non-string levels": {`command = 'cat "$PAWL_ROOT/out.sarif"'`, "levels = [123]"},
		"non-string rules":  {`command = 'cat "$PAWL_ROOT/out.sarif"'`, "rules = [123]"},
	}
	for name, optionLines := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
				id: "scan", direction: "lower-is-better", gate: "per-file-count",
				builtin: "sarif", optionLines: optionLines,
			}))
			res := runPawl(t, dir, baseEnv(), "check")
			if res.exit != 2 {
				t.Fatalf("exit = %d, want 2 (%s must be rejected, not silently ignored)\nstdout=%s\nstderr=%s", res.exit, name, res.stdout, res.stderr)
			}
			if strings.Contains(res.stderr, "unknown builtin") {
				t.Errorf("stderr says \"unknown builtin\" — sarif must be recognized and the %s option is what's invalid: %s", name, res.stderr)
			}
		})
	}
}
