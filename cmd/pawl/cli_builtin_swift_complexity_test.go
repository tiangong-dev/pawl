package main

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

// swiftLocation, swiftFunction and swiftFileResult mirror the shape of
// swift-complexity's `--format json` output that the swift-complexity
// builtin parses. Complexity fields are pointers so a nil field marshals to
// an absent JSON key, letting fixtures precisely distinguish "field present
// with value 0" from "field missing" (the latter must be a measurement
// failure, never a silent zero).
type swiftLocation struct {
	Line   int `json:"line"`
	Column int `json:"column,omitempty"`
}

type swiftFunction struct {
	CognitiveComplexity  *int          `json:"cognitiveComplexity,omitempty"`
	CyclomaticComplexity *int          `json:"cyclomaticComplexity,omitempty"`
	Name                 string        `json:"name"`
	EnclosingTypeName    string        `json:"enclosingTypeName,omitempty"`
	Signature            string        `json:"signature,omitempty"`
	Location             swiftLocation `json:"location"`
}

type swiftSummary struct {
	TotalFunctions int `json:"totalFunctions"`
}

type swiftFileResult struct {
	FilePath  string          `json:"filePath"`
	Functions []swiftFunction `json:"functions"`
	Summary   *swiftSummary   `json:"summary,omitempty"`
}

type swiftComplexityOutput struct {
	Files []swiftFileResult `json:"files"`
}

func intPtr(v int) *int { return &v }

// writeSwiftComplexityFixture marshals a swift-complexity JSON fixture into
// the config dir and returns a `cat` command a dimension's `command` option
// can run to emit it verbatim on stdout — the adapter owns no
// swift-complexity binary, only the format.
func writeSwiftComplexityFixture(t *testing.T, dir, name string, files []swiftFileResult) string {
	t.Helper()
	b, err := json.Marshal(swiftComplexityOutput{Files: files})
	if err != nil {
		t.Fatalf("marshal swift-complexity fixture: %v", err)
	}
	writeFile(t, dir, name, string(b))
	return `cat "$PAWL_ROOT/` + name + `"`
}

// swiftComplexityOptionLines builds the [dimension.options] lines for a
// swift-complexity builtin dimension. An empty command or nil threshold
// omits that option entirely, letting config-validation tests isolate a
// single missing option.
func swiftComplexityOptionLines(command string, threshold *float64, metric string) []string {
	var lines []string
	if command != "" {
		lines = append(lines, "command = '"+command+"'")
	}
	if threshold != nil {
		lines = append(lines, fmt.Sprintf("threshold = %v", *threshold))
	}
	if metric != "" {
		lines = append(lines, fmt.Sprintf("metric = %q", metric))
	}
	return lines
}

// swiftComplexityMetricFixtureFiles returns a two-file, four-function
// fixture whose cognitive and cyclomatic values are deliberately
// interleaved: at threshold 10, gating on cognitive selects funcA+funcC,
// while gating on cyclomatic selects funcB+funcD — a disjoint set. This
// proves the `metric` option actually switches which field is read, rather
// than the builtin always reading cognitiveComplexity. funcA's cognitive
// value sits exactly at the threshold to pin the >= boundary. Extra fields
// (enclosingTypeName, signature, location.column, summary) are populated on
// funcA/a.swift to prove the parser tolerates fields it doesn't need.
func swiftComplexityMetricFixtureFiles() []swiftFileResult {
	return []swiftFileResult{
		{
			FilePath: "a.swift",
			Functions: []swiftFunction{
				{
					CognitiveComplexity: intPtr(10), CyclomaticComplexity: intPtr(2),
					Name: "funcA", EnclosingTypeName: "A", Signature: "func funcA()",
					Location: swiftLocation{Line: 10, Column: 5},
				},
				{
					CognitiveComplexity: intPtr(9), CyclomaticComplexity: intPtr(20),
					Name: "funcB", Location: swiftLocation{Line: 20},
				},
			},
			Summary: &swiftSummary{TotalFunctions: 2},
		},
		{
			FilePath: "b.swift",
			Functions: []swiftFunction{
				{
					CognitiveComplexity: intPtr(15), CyclomaticComplexity: intPtr(1),
					Name: "funcC", Location: swiftLocation{Line: 5},
				},
				{
					CognitiveComplexity: intPtr(3), CyclomaticComplexity: intPtr(20),
					Name: "funcD", Location: swiftLocation{Line: 15},
				},
			},
		},
	}
}

// Default metric is cognitive: offenders are functions whose
// cognitiveComplexity is >= threshold. funcA sits exactly at the threshold
// (10) and must count — pinning the >= boundary, not >.
func TestBuiltinSwiftComplexityDefaultMetricCognitive(t *testing.T) {
	dir := t.TempDir()
	command := writeSwiftComplexityFixture(t, dir, "swift-cc.json", swiftComplexityMetricFixtureFiles())
	threshold := 10.0

	writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
		id: "swift-cc", direction: "lower-is-better",
		builtin:     "swift-complexity",
		optionLines: swiftComplexityOptionLines(command, &threshold, ""),
	}))

	res := runPawl(t, dir, baseEnv(), "record")
	if res.exit != 0 {
		t.Fatalf("record exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	snap := readSnapshot(t, dirJoin(dir, "pawl.snapshot.json"))
	m := snap.Metrics["swift-cc"]

	if m.Value != 2 {
		t.Errorf("value = %v, want 2 (funcA at threshold + funcC over threshold)", m.Value)
	}
	if m.Unit != "functions" {
		t.Errorf("unit = %q, want %q", m.Unit, "functions")
	}
	want := map[string]float64{"a.swift:10": 1, "b.swift:5": 1}
	if len(m.Breakdown) != len(want) {
		t.Fatalf("breakdown = %v, want %v", m.Breakdown, want)
	}
	for k, v := range want {
		if m.Breakdown[k] != v {
			t.Errorf("breakdown[%q] = %v, want %v (full breakdown: %v)", k, m.Breakdown[k], v, m.Breakdown)
		}
	}
}

// metric = "cyclomatic" gates on cyclomaticComplexity instead of
// cognitiveComplexity, selecting a disjoint offender set on the same
// fixture — proving the field switch is real, not hard-coded to cognitive.
func TestBuiltinSwiftComplexityMetricCyclomatic(t *testing.T) {
	dir := t.TempDir()
	command := writeSwiftComplexityFixture(t, dir, "swift-cc.json", swiftComplexityMetricFixtureFiles())
	threshold := 10.0

	writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
		id: "swift-cc", direction: "lower-is-better",
		builtin:     "swift-complexity",
		optionLines: swiftComplexityOptionLines(command, &threshold, "cyclomatic"),
	}))

	res := runPawl(t, dir, baseEnv(), "record")
	if res.exit != 0 {
		t.Fatalf("record exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	snap := readSnapshot(t, dirJoin(dir, "pawl.snapshot.json"))
	m := snap.Metrics["swift-cc"]

	if m.Value != 2 {
		t.Errorf("value = %v, want 2 (funcB + funcD over threshold on cyclomaticComplexity)", m.Value)
	}
	want := map[string]float64{"a.swift:20": 1, "b.swift:15": 1}
	if len(m.Breakdown) != len(want) {
		t.Fatalf("breakdown = %v, want %v (must not be the cognitive-metric offender set)", m.Breakdown, want)
	}
	for k, v := range want {
		if m.Breakdown[k] != v {
			t.Errorf("breakdown[%q] = %v, want %v", k, m.Breakdown[k], v)
		}
	}
}

// An absolute filePath from swift-complexity is relativized against the
// config dir in the breakdown key, even through nested directories.
func TestBuiltinSwiftComplexityRelativizesAbsoluteFilePath(t *testing.T) {
	dir := t.TempDir()
	absNested := filepath.Join(dir, "src", "nested", "Deep.swift")
	command := writeSwiftComplexityFixture(t, dir, "swift-cc.json", []swiftFileResult{
		{
			FilePath: absNested,
			Functions: []swiftFunction{
				{CognitiveComplexity: intPtr(20), Name: "deep", Location: swiftLocation{Line: 36}},
			},
		},
	})
	threshold := 10.0

	writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
		id: "swift-cc", direction: "lower-is-better",
		builtin:     "swift-complexity",
		optionLines: swiftComplexityOptionLines(command, &threshold, ""),
	}))

	res := runPawl(t, dir, baseEnv(), "record")
	if res.exit != 0 {
		t.Fatalf("record exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	snap := readSnapshot(t, dirJoin(dir, "pawl.snapshot.json"))
	m := snap.Metrics["swift-cc"]
	want := "src/nested/Deep.swift:36"
	if _, ok := m.Breakdown[want]; !ok {
		t.Errorf("breakdown = %v, want key %q (absolute filePath relativized against config dir)", m.Breakdown, want)
	}
}

// The value proper's key contribution: per-file-count gate must catch a
// regression that a total-count gate would miss. One file's offender count
// rises (1 -> 2) while another's falls (1 -> 0); the grand total stays at 2
// either way, so `check` must still fail with the per-file detail line.
func TestBuiltinSwiftComplexityPerFileCountGateEndToEnd(t *testing.T) {
	dir := t.TempDir()
	threshold := 10.0

	baseCommand := writeSwiftComplexityFixture(t, dir, "swift-cc-base.json", []swiftFileResult{
		{FilePath: "a.swift", Functions: []swiftFunction{
			{CognitiveComplexity: intPtr(20), Name: "a1", Location: swiftLocation{Line: 10}},
		}},
		{FilePath: "b.swift", Functions: []swiftFunction{
			{CognitiveComplexity: intPtr(20), Name: "b1", Location: swiftLocation{Line: 5}},
		}},
	})
	mustRecord(t, dir, buildConfig("", dimDef{
		id: "swift-cc", direction: "lower-is-better", gate: "per-file-count",
		builtin:     "swift-complexity",
		optionLines: swiftComplexityOptionLines(baseCommand, &threshold, ""),
	}))

	worseCommand := writeSwiftComplexityFixture(t, dir, "swift-cc-worse.json", []swiftFileResult{
		{FilePath: "a.swift", Functions: []swiftFunction{
			{CognitiveComplexity: intPtr(20), Name: "a1", Location: swiftLocation{Line: 10}},
			{CognitiveComplexity: intPtr(20), Name: "a2", Location: swiftLocation{Line: 11}},
		}},
		{FilePath: "b.swift", Functions: []swiftFunction{
			{CognitiveComplexity: intPtr(1), Name: "b1", Location: swiftLocation{Line: 5}},
		}},
	})
	writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
		id: "swift-cc", direction: "lower-is-better", gate: "per-file-count",
		builtin:     "swift-complexity",
		optionLines: swiftComplexityOptionLines(worseCommand, &threshold, ""),
	}))

	res := runPawl(t, dir, baseEnv(), "check")
	if res.exit != 1 {
		t.Fatalf("check exit = %d, want 1 (a.swift's offender count rose 1 -> 2 even though the total is unchanged)\nstdout=%s\nstderr=%s",
			res.exit, res.stdout, res.stderr)
	}
	if !strings.Contains(res.stdout, "a.swift  1 → 2") {
		t.Errorf("stdout missing per-file detail line %q: %s", "a.swift  1 → 2", res.stdout)
	}
}

// Any non-zero exit from the command is a measurement failure: unlike
// eslint (which conflates exit 1 = "problems found"), swift-complexity uses
// exit 1 for both violations and bad-path errors, so pawl must forbid
// non-zero entirely rather than trying to disambiguate.
func TestBuiltinSwiftComplexityNonZeroExitIsMeasurementFailure(t *testing.T) {
	dir := t.TempDir()
	command := writeSwiftComplexityFixture(t, dir, "swift-cc.json", []swiftFileResult{
		{FilePath: "a.swift", Functions: []swiftFunction{
			{CognitiveComplexity: intPtr(20), Name: "a1", Location: swiftLocation{Line: 1}},
		}},
	})
	threshold := 10.0

	writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
		id: "swift-cc", direction: "lower-is-better",
		builtin:     "swift-complexity",
		optionLines: swiftComplexityOptionLines(command+"; exit 1", &threshold, ""),
	}))

	res := runPawl(t, dir, baseEnv(), "record")
	if res.exit != 2 {
		t.Fatalf("record exit = %d, want 2 (non-zero exit must never be treated as \"found violations\")\nstdout=%s\nstderr=%s",
			res.exit, res.stdout, res.stderr)
	}
	if !strings.Contains(res.stderr, "measuring swift-cc…") {
		t.Errorf("stderr missing progress line: the dimension must actually run (config-valid) and fail as a measurement: %s", res.stderr)
	}
}

// A function missing the selected metric's JSON field is a measurement
// failure, never a silent zero — checked for both metric settings so
// neither field has a hidden default.
func TestBuiltinSwiftComplexityMissingMetricFieldIsMeasurementFailure(t *testing.T) {
	cases := []struct {
		name    string
		metric  string
		content string
	}{
		{
			name:   "cognitive missing",
			metric: "",
			content: `{"files":[{"filePath":"a.swift","functions":[` +
				`{"cyclomaticComplexity":5,"name":"foo","location":{"line":1}}]}]}`,
		},
		{
			name:   "cyclomatic missing",
			metric: "cyclomatic",
			content: `{"files":[{"filePath":"a.swift","functions":[` +
				`{"cognitiveComplexity":5,"name":"foo","location":{"line":1}}]}]}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, dir, "swift-cc.json", tc.content)
			threshold := 1.0
			writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
				id: "swift-cc", direction: "lower-is-better",
				builtin:     "swift-complexity",
				optionLines: swiftComplexityOptionLines(`cat "$PAWL_ROOT/swift-cc.json"`, &threshold, tc.metric),
			}))
			res := runPawl(t, dir, baseEnv(), "record")
			if res.exit != 2 {
				t.Fatalf("record exit = %d, want 2 (function missing the selected metric field must not silently read as 0)\nstdout=%s\nstderr=%s",
					res.exit, res.stdout, res.stderr)
			}
			if !strings.Contains(res.stderr, "measuring swift-cc…") {
				t.Errorf("stderr missing progress line: must fail while measuring, not be rejected up front: %s", res.stderr)
			}
		})
	}
}

// stdout that does not parse as swift-complexity's `--format json` shape —
// a bare unrelated object or garbage — is a measurement failure.
func TestBuiltinSwiftComplexityNonJSONStdoutIsMeasurementFailure(t *testing.T) {
	cases := []struct {
		name    string
		content string
	}{
		{"bare object without files key", `{"filePath": "a.swift", "functions": []}`},
		{"garbage", "not json at all"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, dir, "swift-cc.json", tc.content)
			threshold := 10.0
			writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
				id: "swift-cc", direction: "lower-is-better",
				builtin:     "swift-complexity",
				optionLines: swiftComplexityOptionLines(`cat "$PAWL_ROOT/swift-cc.json"`, &threshold, ""),
			}))
			res := runPawl(t, dir, baseEnv(), "record")
			if res.exit != 2 {
				t.Fatalf("record exit = %d, want 2\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
			}
			if !strings.Contains(res.stderr, "measuring swift-cc…") {
				t.Errorf("stderr missing progress line: must fail while measuring, not be rejected up front: %s", res.stderr)
			}
		})
	}
}

// swift-complexity builtin requires the command option; without it the
// config is invalid at load time (exit 2, before any measurement) — the
// rejection must name the missing option, not fall through to "unknown
// builtin" (which would mean the builtin itself was never recognized).
func TestBuiltinSwiftComplexityMissingCommandIsConfigError(t *testing.T) {
	dir := t.TempDir()
	threshold := 10.0
	writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
		id: "swift-cc", direction: "lower-is-better",
		builtin:     "swift-complexity",
		optionLines: swiftComplexityOptionLines("", &threshold, ""),
	}))
	res := runPawl(t, dir, baseEnv(), "check")
	if res.exit != 2 {
		t.Fatalf("exit = %d, want 2\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	if strings.Contains(res.stderr, "unknown builtin") {
		t.Errorf("stderr reports \"unknown builtin\" — swift-complexity must be a recognized builtin whose command option is what's missing: %s", res.stderr)
	}
	if strings.Contains(res.stderr, "measuring") {
		t.Errorf("stderr shows a measuring progress line — a missing required option must be rejected at config load, before any measurement runs: %s", res.stderr)
	}
}

// threshold is required; without it the config is invalid at load time.
func TestBuiltinSwiftComplexityMissingThresholdIsConfigError(t *testing.T) {
	dir := t.TempDir()
	command := writeSwiftComplexityFixture(t, dir, "swift-cc.json", []swiftFileResult{
		{FilePath: "a.swift", Functions: []swiftFunction{
			{CognitiveComplexity: intPtr(20), Name: "a1", Location: swiftLocation{Line: 1}},
		}},
	})
	writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
		id: "swift-cc", direction: "lower-is-better",
		builtin:     "swift-complexity",
		optionLines: swiftComplexityOptionLines(command, nil, ""),
	}))
	res := runPawl(t, dir, baseEnv(), "check")
	if res.exit != 2 {
		t.Fatalf("exit = %d, want 2\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	if strings.Contains(res.stderr, "unknown builtin") {
		t.Errorf("stderr reports \"unknown builtin\" — swift-complexity must be a recognized builtin whose threshold option is what's missing: %s", res.stderr)
	}
	if strings.Contains(res.stderr, "measuring") {
		t.Errorf("stderr shows a measuring progress line — a missing required option must be rejected at config load, before any measurement runs: %s", res.stderr)
	}
}

// metric must be "cognitive" or "cyclomatic"; any other value is a config
// error at load time, not a runtime surprise discovered mid-measurement.
func TestBuiltinSwiftComplexityInvalidMetricIsConfigError(t *testing.T) {
	dir := t.TempDir()
	command := writeSwiftComplexityFixture(t, dir, "swift-cc.json", []swiftFileResult{
		{FilePath: "a.swift", Functions: []swiftFunction{
			{CognitiveComplexity: intPtr(20), Name: "a1", Location: swiftLocation{Line: 1}},
		}},
	})
	threshold := 10.0
	writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
		id: "swift-cc", direction: "lower-is-better",
		builtin:     "swift-complexity",
		optionLines: swiftComplexityOptionLines(command, &threshold, "bogus"),
	}))
	res := runPawl(t, dir, baseEnv(), "check")
	if res.exit != 2 {
		t.Fatalf("exit = %d, want 2\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	if strings.Contains(res.stderr, "measuring") {
		t.Errorf("stderr shows a measuring progress line — an invalid metric value must be rejected at config load, before any measurement runs: %s", res.stderr)
	}
}
