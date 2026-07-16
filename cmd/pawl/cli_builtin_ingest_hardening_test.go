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

// lcov summary counters must be paired WITHIN the same SF:...end_of_record
// record, not merely appear the same number of times across the whole
// report. Two records that each supply half of a pair (LF in one, LH in
// another) sum to a matching total and must not fabricate a percentage from
// counters that were never actually co-located in one record. A counter
// appearing after the last end_of_record is not inside any record either.
func TestBuiltinCoverageLcovCountersMustPairWithinSameRecord(t *testing.T) {
	cases := map[string]string{
		"LF and LH split across two different records": "SF:a.go\nLF:10\nend_of_record\n" +
			"SF:b.go\nLH:10\nend_of_record\n",
		"LH counter appears after the last end_of_record": "SF:a.go\nLF:10\nLH:5\nend_of_record\n" +
			"LH:5\n",
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
				t.Fatalf("record exit = %d, want 2 (LF/LH must pair within one SF record, not merely balance in total across the report)\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
			}
		})
	}
}

// A trailing record cut off mid-parse — SF and LF present, EOF before LH or
// end_of_record — is truncated input, not a genuine zero-hit record.
func TestBuiltinCoverageLcovTruncatedTrailingRecordIsMeasurementFailure(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "cov.info", "SF:a.go\nLF:10\n")
	writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
		id: "cov", direction: "higher-is-better", builtin: "coverage",
		optionLines: coverageOptionLines("", "cov.info", "lcov", "lines"),
	}))
	res := runPawl(t, dir, baseEnv(), "record")
	if res.exit != 2 {
		t.Fatalf("record exit = %d, want 2 (a record truncated before LH/end_of_record is not a measured report)\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
}

// lcov counters must live inside an SF:...end_of_record record. A pair of
// counters outside any record — even a balanced LF/LH pair that would sum to
// a plausible-looking total — is not attached to a file and must not be
// folded into the measurement. This holds whether the stray pair trails the
// last record or precedes the first one.
func TestBuiltinCoverageLcovCountersOutsideAnyRecordAreMeasurementFailure(t *testing.T) {
	cases := map[string]string{
		"balanced pair trails the last end_of_record": "SF:a.go\nLF:10\nLH:5\nend_of_record\n" +
			"LF:90\nLH:90\n",
		"balanced pair precedes the first SF": "LF:90\nLH:90\n" +
			"SF:a.go\nLF:10\nLH:5\nend_of_record\n",
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
				t.Fatalf("record exit = %d, want 2 (a counter pair outside any SF record must not be folded into the measurement, however balanced it looks)\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
			}
		})
	}
}

// TestBuiltinCoverageLcovMultiRecordReportMeasuresCorrectly is the control:
// two genuinely separate SF records, each internally balanced, sum to the
// same 95% a stray trailing pair would fabricate — but here every counter is
// inside a record, so the measurement is honest and must succeed.
func TestBuiltinCoverageLcovMultiRecordReportMeasuresCorrectly(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "cov.info", "SF:a.go\nLF:10\nLH:5\nend_of_record\n"+
		"SF:b.go\nLF:90\nLH:90\nend_of_record\n")
	writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
		id: "cov", direction: "higher-is-better", builtin: "coverage",
		optionLines: coverageOptionLines("", "cov.info", "lcov", "lines"),
	}))
	res := runPawl(t, dir, baseEnv(), "record")
	if res.exit != 0 {
		t.Fatalf("record exit = %d, want 0 (every counter is inside an SF record; this is a genuine measurement)\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	snap := readSnapshot(t, dirJoin(dir, "pawl.snapshot.json"))
	if snap.Metrics["cov"].Value != 95 {
		t.Errorf("value = %v, want 95 ((5+90)/(10+90) across two well-formed records)", snap.Metrics["cov"].Value)
	}
}

// lcov hit <= found must hold per record, not merely across the report's
// global totals. A record with LH:2 LF:1 is impossible on its own, but here a
// second, honest record (LF:99 LH:98) pads the totals to (2+98)/(1+99) = 100
// found = 100 hit — a globally "consistent" 100% that masks the impossible
// record. The per-record violation must still fail measurement.
func TestBuiltinCoverageLcovImpossibleRecordMaskedByGlobalTotalsIsMeasurementFailure(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "cov.info", "SF:a.go\nLF:1\nLH:2\nend_of_record\n"+
		"SF:b.go\nLF:99\nLH:98\nend_of_record\n")
	writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
		id: "cov", direction: "higher-is-better", builtin: "coverage",
		optionLines: coverageOptionLines("", "cov.info", "lcov", "lines"),
	}))
	res := runPawl(t, dir, baseEnv(), "record")
	if res.exit != 2 {
		t.Fatalf("record exit = %d, want 2 (a.go alone has LH>LF; a second well-formed record must not launder it into a plausible global total)\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
}

// TestBuiltinCoverageLcovSingleRecordHitExceedsFoundIsMeasurementFailure is
// the control: a lone record with LH>LF is impossible both per-record and
// globally (there are no other records to average it against), and must
// already fail measurement.
func TestBuiltinCoverageLcovSingleRecordHitExceedsFoundIsMeasurementFailure(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "cov.info", "SF:a.go\nLF:1\nLH:2\nend_of_record\n")
	writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
		id: "cov", direction: "higher-is-better", builtin: "coverage",
		optionLines: coverageOptionLines("", "cov.info", "lcov", "lines"),
	}))
	res := runPawl(t, dir, baseEnv(), "record")
	if res.exit != 2 {
		t.Fatalf("record exit = %d, want 2 (a lone LH>LF record is impossible with nothing to average against)\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
}

// TestBuiltinCoverageLcovTwoWellFormedRecordsMeasureCorrectly is the control:
// both records individually satisfy hit <= found, so the report measures
// honestly and must succeed.
func TestBuiltinCoverageLcovTwoWellFormedRecordsMeasureCorrectly(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "cov.info", "SF:a.go\nLF:1\nLH:1\nend_of_record\n"+
		"SF:b.go\nLF:99\nLH:98\nend_of_record\n")
	writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
		id: "cov", direction: "higher-is-better", builtin: "coverage",
		optionLines: coverageOptionLines("", "cov.info", "lcov", "lines"),
	}))
	res := runPawl(t, dir, baseEnv(), "record")
	if res.exit != 0 {
		t.Fatalf("record exit = %d, want 0 (both records satisfy hit <= found; this is a genuine measurement)\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	snap := readSnapshot(t, dirJoin(dir, "pawl.snapshot.json"))
	if snap.Metrics["cov"].Value != 99 {
		t.Errorf("value = %v, want 99 ((1+98)/(1+99) across two well-formed records)", snap.Metrics["cov"].Value)
	}
}

// A well-formed lcov record carries at most one summary pair per metric (one
// LF + one LH for lines). A record with two LF/LH pairs is malformed: naively
// summing hits and founds across the duplicated pairs fabricates a low
// percentage — here (1+0)/(1+99) = 1% — that the record never actually
// reported.
func TestBuiltinCoverageLcovDuplicateSummaryPairInRecordIsMeasurementFailure(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "cov.info", "SF:a.go\nLF:1\nLH:1\nLF:99\nLH:0\nend_of_record\n")
	writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
		id: "cov", direction: "higher-is-better", builtin: "coverage",
		optionLines: coverageOptionLines("", "cov.info", "lcov", "lines"),
	}))
	res := runPawl(t, dir, baseEnv(), "record")
	if res.exit != 2 {
		t.Fatalf("record exit = %d, want 2 (a record with two LF/LH pairs is malformed, not a 1%% measurement)\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
}

// TestBuiltinCoverageLcovSingleSummaryPairPlusSecondRecordMeasuresCorrectly is
// a control: one record carrying exactly one LF+LH pair, followed by a
// second, independently well-formed record, is ordinary honest input and must
// measure correctly.
func TestBuiltinCoverageLcovSingleSummaryPairPlusSecondRecordMeasuresCorrectly(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "cov.info", "SF:a.go\nLF:1\nLH:1\nend_of_record\n"+
		"SF:b.go\nLF:99\nLH:99\nend_of_record\n")
	writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
		id: "cov", direction: "higher-is-better", builtin: "coverage",
		optionLines: coverageOptionLines("", "cov.info", "lcov", "lines"),
	}))
	res := runPawl(t, dir, baseEnv(), "record")
	if res.exit != 0 {
		t.Fatalf("record exit = %d, want 0 (two records, each with a single LF/LH pair, are a genuine measurement)\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	snap := readSnapshot(t, dirJoin(dir, "pawl.snapshot.json"))
	if snap.Metrics["cov"].Value != 100 {
		t.Errorf("value = %v, want 100 ((1+99)/(1+99) across two well-formed records)", snap.Metrics["cov"].Value)
	}
}

// TestBuiltinCoverageLcovDuplicateFoundCounterWithSingleHitIsMeasurementFailure
// is a control locking existing behavior: two LF counters against a single LH
// is already an unequal-count violation and must already fail measurement,
// independent of the duplicate-pair fix above.
func TestBuiltinCoverageLcovDuplicateFoundCounterWithSingleHitIsMeasurementFailure(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "cov.info", "SF:a.go\nLF:1\nLH:1\nLF:99\nend_of_record\n")
	writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
		id: "cov", direction: "higher-is-better", builtin: "coverage",
		optionLines: coverageOptionLines("", "cov.info", "lcov", "lines"),
	}))
	res := runPawl(t, dir, baseEnv(), "record")
	if res.exit != 2 {
		t.Fatalf("record exit = %d, want 2 (two LF counters against one LH is an unequal-count violation)\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
}

// Per-record counters can each be finite and individually satisfy hit <=
// found, yet still overflow float64 once summed across the whole report.
// Record a.go's found total (1e308) plus b.go's found total (1e308) exceeds
// float64's finite range and overflows to +Inf, while the hit total (1e308)
// stays finite — so hit/Inf silently reads as a "measured" 0% instead of the
// measurement failure it actually is.
func TestBuiltinCoverageLcovAggregateOverflowIsMeasurementFailure(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "cov.info", "SF:a.go\nLF:1e308\nLH:1e308\nend_of_record\n"+
		"SF:b.go\nLF:1e308\nLH:0\nend_of_record\n")
	writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
		id: "cov", direction: "higher-is-better", builtin: "coverage",
		optionLines: coverageOptionLines("", "cov.info", "lcov", "lines"),
	}))
	res := runPawl(t, dir, baseEnv(), "record")
	if res.exit != 2 {
		t.Fatalf("record exit = %d, want 2 (found totals overflow float64 to +Inf; hit/Inf must not fabricate a percentage)\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
}

// TestBuiltinCoverageLcovLargeButFiniteAggregateMeasuresCorrectly is the
// control: counters large enough to matter but nowhere near float64's range
// limit sum without overflowing, so the report measures honestly.
func TestBuiltinCoverageLcovLargeButFiniteAggregateMeasuresCorrectly(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "cov.info", "SF:a.go\nLF:1e15\nLH:5e14\nend_of_record\n"+
		"SF:b.go\nLF:1e15\nLH:5e14\nend_of_record\n")
	writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
		id: "cov", direction: "higher-is-better", builtin: "coverage",
		optionLines: coverageOptionLines("", "cov.info", "lcov", "lines"),
	}))
	res := runPawl(t, dir, baseEnv(), "record")
	if res.exit != 0 {
		t.Fatalf("record exit = %d, want 0 (counters are large but well within float64's finite range)\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	snap := readSnapshot(t, dirJoin(dir, "pawl.snapshot.json"))
	if snap.Metrics["cov"].Value != 50 {
		t.Errorf("value = %v, want 50 ((5e14+5e14)/(1e15+1e15) across two well-formed records)", snap.Metrics["cov"].Value)
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
