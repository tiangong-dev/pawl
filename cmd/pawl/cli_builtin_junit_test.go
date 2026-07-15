package main

import (
	"fmt"
	"strings"
	"testing"
)

// junitMixedFixture is a representative JUnit XML report with a known mix:
// 5 testcases — 1 <failure>, 1 <error>, 1 <skipped>, 2 passing — so
// failures=2, tests=5, skipped=1, passing=2 by direct count of the
// testcases themselves.
const junitMixedFixture = `<?xml version="1.0" encoding="UTF-8"?>
<testsuites>
  <testsuite name="suite" tests="5" failures="1" errors="1" skipped="1">
    <testcase name="t1" classname="pkg"><failure message="boom">trace</failure></testcase>
    <testcase name="t2" classname="pkg"><error message="oops">trace</error></testcase>
    <testcase name="t3" classname="pkg"><skipped/></testcase>
    <testcase name="t4" classname="pkg"></testcase>
    <testcase name="t5" classname="pkg"></testcase>
  </testsuite>
</testsuites>
`

// junitDisagreeingSuiteFixture has the same 5-testcase mix as
// junitMixedFixture (failures=2, tests=5), but the suite-level attributes
// disagree with reality (tests="100" failures="0") — producers compute
// these inconsistently, so pawl must trust the testcases, not the attributes.
const junitDisagreeingSuiteFixture = `<?xml version="1.0" encoding="UTF-8"?>
<testsuites>
  <testsuite name="suite" tests="100" failures="0">
    <testcase name="t1"><failure/></testcase>
    <testcase name="t2"><error/></testcase>
    <testcase name="t3"><skipped/></testcase>
    <testcase name="t4"></testcase>
    <testcase name="t5"></testcase>
  </testsuite>
</testsuites>
`

// junitCommand writes a JUnit XML fixture into the config dir and returns a
// `cat` command a dimension's `command` option can run to emit it verbatim
// on stdout — the adapter owns no test-runner binary, only the format.
func junitCommand(t *testing.T, dir, name, content string) string {
	t.Helper()
	writeFile(t, dir, name, content)
	return `cat "$PAWL_ROOT/` + name + `"`
}

// junitOptionLines builds the [dimension.options] lines for a junit builtin
// dimension: the shared source (command and/or file) plus the optional
// count selector. Passing "" for command/file/count omits that option.
func junitOptionLines(command, file, count string) []string {
	var lines []string
	if command != "" {
		lines = append(lines, "command = '"+command+"'")
	}
	if file != "" {
		lines = append(lines, fmt.Sprintf("file = %q", file))
	}
	if count != "" {
		lines = append(lines, fmt.Sprintf("count = %q", count))
	}
	return lines
}

// The count option selects which quantity is measured from ONE fixture with
// a known mix: failures (the default) counts testcases with a <failure> or
// <error> child, tests counts every testcase, skipped counts <skipped>
// children, and passing is tests - failures - skipped. unit is the count
// name and breakdown is always null (junit has no per-line attribution).
func TestBuiltinJunitCountOptionSelectsWhichQuantity(t *testing.T) {
	cases := []struct {
		name      string
		count     string
		wantValue float64
		wantUnit  string
	}{
		{"default counts failures", "", 2, "failures"},
		{"tests counts every testcase", "tests", 5, "tests"},
		{"skipped counts skipped children", "skipped", 1, "skipped"},
		{"passing is tests minus failures minus skipped", "passing", 2, "passing"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			command := junitCommand(t, dir, "junit.xml", junitMixedFixture)
			writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
				id: "junit-count", direction: "lower-is-better", builtin: "junit",
				optionLines: junitOptionLines(command, "", tc.count),
			}))

			res := runPawl(t, dir, baseEnv(), "record")
			if res.exit != 0 {
				t.Fatalf("record exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
			}
			snapPath := dirJoin(dir, "pawl.snapshot.json")
			snap := readSnapshot(t, snapPath)
			m := snap.Metrics["junit-count"]
			if m.Value != tc.wantValue {
				t.Errorf("value = %v, want %v", m.Value, tc.wantValue)
			}
			if m.Unit != tc.wantUnit {
				t.Errorf("unit = %q, want %q", m.Unit, tc.wantUnit)
			}
			if m.Breakdown != nil {
				t.Errorf("breakdown = %v, want nil (junit is a scalar metric)", m.Breakdown)
			}
			if tc.count == "" {
				raw := readFile(t, snapPath)
				if !strings.Contains(raw, `"breakdown": null`) {
					t.Errorf("snapshot JSON missing literal `\"breakdown\": null`:\n%s", raw)
				}
			}
		})
	}
}

// Counts are derived from the <testcase> elements themselves, not the
// suite-level tests=/failures= attributes: a fixture whose suite attributes
// disagree with the actual testcases must still report the testcase-derived
// counts.
func TestBuiltinJunitCountsDeriveFromTestcasesNotSuiteAttributes(t *testing.T) {
	dir := t.TempDir()
	command := junitCommand(t, dir, "junit.xml", junitDisagreeingSuiteFixture)
	writeFile(t, dir, "pawl.yaml", buildConfig("",
		dimDef{
			id: "junit-failures", direction: "lower-is-better", builtin: "junit",
			optionLines: junitOptionLines(command, "", "failures"),
		},
		dimDef{
			id: "junit-tests", direction: "lower-is-better", builtin: "junit",
			optionLines: junitOptionLines(command, "", "tests"),
		},
	))

	res := runPawl(t, dir, baseEnv(), "record")
	if res.exit != 0 {
		t.Fatalf("record exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	snap := readSnapshot(t, dirJoin(dir, "pawl.snapshot.json"))
	if snap.Metrics["junit-failures"].Value != 2 {
		t.Errorf("failures = %v, want 2 (2 testcases with failure/error children, despite suite failures=\"0\")", snap.Metrics["junit-failures"].Value)
	}
	if snap.Metrics["junit-tests"].Value != 5 {
		t.Errorf("tests = %v, want 5 (5 actual testcases, despite suite tests=\"100\")", snap.Metrics["junit-tests"].Value)
	}
}

// A test run that fails (and so exits non-zero) still produces a valid,
// readable JUnit report — pawl does not gate on the command's exit code for
// this builtin.
func TestBuiltinJunitNonZeroExitWithValidXMLStillMeasures(t *testing.T) {
	dir := t.TempDir()
	command := junitCommand(t, dir, "junit.xml", junitMixedFixture)
	writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
		id: "junit-count", direction: "lower-is-better", builtin: "junit",
		optionLines: junitOptionLines(command+"; exit 1", "", "tests"),
	}))

	res := runPawl(t, dir, baseEnv(), "record")
	if res.exit != 0 {
		t.Fatalf("record exit = %d, want 0 (non-zero exit with a valid report is still a measurement)\nstdout=%s\nstderr=%s",
			res.exit, res.stdout, res.stderr)
	}
	snap := readSnapshot(t, dirJoin(dir, "pawl.snapshot.json"))
	if snap.Metrics["junit-count"].Value != 5 {
		t.Errorf("value = %v, want 5", snap.Metrics["junit-count"].Value)
	}
}

// command+file source with stale-artifact protection: a pre-existing report
// at the configured file path is deleted before the command runs, so a
// fresh command output — not the stale fossil — is what gets measured.
func TestBuiltinJunitCommandFileStaleArtifactProtection(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "fixture.xml", junitMixedFixture)
	writeFile(t, dir, "out.xml", `<testsuites><testsuite><testcase name="stale"><failure/></testcase></testsuite></testsuites>`)
	writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
		id: "junit-count", direction: "lower-is-better", builtin: "junit",
		optionLines: junitOptionLines(
			`cp "$PAWL_ROOT/fixture.xml" "$PAWL_ROOT/out.xml"`, "out.xml", "tests",
		),
	}))

	res := runPawl(t, dir, baseEnv(), "record")
	if res.exit != 0 {
		t.Fatalf("record exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	snap := readSnapshot(t, dirJoin(dir, "pawl.snapshot.json"))
	if snap.Metrics["junit-count"].Value != 5 {
		t.Errorf("value = %v, want 5 (fresh command output, not the stale 1-testcase fossil)", snap.Metrics["junit-count"].Value)
	}
}

// A document that does not parse as XML, or one that parses but has no
// <testcase> at all, is a measurement failure — both must fail while
// actually running the measurement, not be rejected as invalid config.
func TestBuiltinJunitMalformedReportIsMeasurementFailure(t *testing.T) {
	cases := []struct {
		name    string
		content string
	}{
		{"non-XML", "not xml at all"},
		{"zero testcases", `<testsuites><testsuite name="empty"></testsuite></testsuites>`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			command := junitCommand(t, dir, "junit.xml", tc.content)
			writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
				id: "junit-count", direction: "lower-is-better", builtin: "junit",
				optionLines: junitOptionLines(command, "", ""),
			}))
			res := runPawl(t, dir, baseEnv(), "record")
			if res.exit != 2 {
				t.Fatalf("record exit = %d, want 2\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
			}
			if !strings.Contains(res.stderr, "measuring junit-count…") {
				t.Errorf("stderr missing progress line: the dimension must actually run (config-valid) and fail reading the report, not be rejected up front: %s", res.stderr)
			}
		})
	}
}

// Config errors are rejected at load time, before any measurement runs, and
// must name junit as the recognized builtin (not fall through to "unknown
// builtin"): neither command nor file given, and an unrecognized count value.
func TestBuiltinJunitConfigErrorsAreRejectedAtLoadNotUnknownBuiltin(t *testing.T) {
	cases := []struct {
		name        string
		optionLines []string
		needsFile   bool
	}{
		{"neither command nor file", junitOptionLines("", "", "tests"), false},
		{"invalid count value", junitOptionLines(`cat "$PAWL_ROOT/junit.xml"`, "", "bogus"), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if tc.needsFile {
				writeFile(t, dir, "junit.xml", junitMixedFixture)
			}
			writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
				id: "junit-count", direction: "lower-is-better", builtin: "junit",
				optionLines: tc.optionLines,
			}))
			res := runPawl(t, dir, baseEnv(), "check")
			if res.exit != 2 {
				t.Fatalf("exit = %d, want 2\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
			}
			if strings.Contains(res.stderr, "unknown builtin") {
				t.Errorf("stderr reports \"unknown builtin\" — junit must be a recognized builtin whose %s is what's invalid: %s", tc.name, res.stderr)
			}
		})
	}
}
