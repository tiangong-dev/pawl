package main

import (
	"encoding/json"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// The pawl --format json verdict schema. Pointer fields distinguish JSON null
// (a new dimension's base, a total regression's key/path/line) from a zero
// value. Shared by the --format json and --since json tests.
type jsonReport struct {
	SchemaVersion int          `json:"schema_version"`
	Command       string       `json:"command"`
	Mode          string       `json:"mode"`
	Since         *string      `json:"since"`
	ExitCode      int          `json:"exit_code"`
	Metrics       []jsonMetric `json:"metrics"`
}

type jsonMetric struct {
	ID          string           `json:"id"`
	Title       string           `json:"title"`
	Direction   string           `json:"direction"`
	Gate        string           `json:"gate"`
	Unit        string           `json:"unit"`
	Base        *float64         `json:"base"`
	Current     float64          `json:"current"`
	Status      string           `json:"status"`
	Improved    bool             `json:"improved"`
	Regressions []jsonRegression `json:"regressions"`
}

type jsonRegression struct {
	Kind       string  `json:"kind"`
	Key        *string `json:"key"`
	Path       *string `json:"path"`
	Line       *int    `json:"line"`
	Base       float64 `json:"base"`
	Current    float64 `json:"current"`
	Message    string  `json:"message"`
	Suppressed bool    `json:"suppressed"`
}

// parseReport asserts stdout is exactly one JSON object (nothing else) and
// decodes it — the core "stdout stays pure machine output" contract.
func parseReport(t *testing.T, out string) jsonReport {
	t.Helper()
	var r jsonReport
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatalf("stdout is not one JSON object: %v\nstdout=%s", err, out)
	}
	return r
}

func metricByID(r jsonReport, id string) (jsonMetric, bool) {
	for _, m := range r.Metrics {
		if m.ID == id {
			return m, true
		}
	}
	return jsonMetric{}, false
}

// check --format json emits one JSON object with the fixed top-level shape:
// schema_version 1, command/mode/since set, exit_code mirroring the process
// exit, and metrics sorted by id.
func TestCheckFormatJSONStructure(t *testing.T) {
	dir := t.TempDir()
	// Declared b-before-a so the sorted-by-id contract is observable.
	config := buildConfig("",
		dimDef{id: "b", direction: "lower-is-better", command: `echo '{"value": 1}'`},
		dimDef{id: "a", direction: "lower-is-better", command: `echo '{"value": 1}'`},
	)
	mustRecord(t, dir, config)

	res := runPawl(t, dir, baseEnv(), "check", "--format", "json")
	r := parseReport(t, res.stdout)
	if r.SchemaVersion != 1 {
		t.Errorf("schema_version = %d, want 1", r.SchemaVersion)
	}
	if r.Command != "check" {
		t.Errorf("command = %q, want %q", r.Command, "check")
	}
	if r.Mode != "full" {
		t.Errorf("mode = %q, want %q", r.Mode, "full")
	}
	if r.Since != nil {
		t.Errorf("since = %v, want null", *r.Since)
	}
	if r.ExitCode != res.exit {
		t.Errorf("exit_code = %d, but process exit = %d", r.ExitCode, res.exit)
	}
	ids := make([]string, len(r.Metrics))
	for i, m := range r.Metrics {
		ids[i] = m.ID
	}
	if !sort.StringsAreSorted(ids) {
		t.Errorf("metrics not sorted by id: %v", ids)
	}
}

// A per-file-count regression is fully described in JSON: status "worse",
// improved false, and a regressions[] entry with kind/key/path/line/base/
// current/message and suppressed false; process exit and exit_code both 1.
func TestCheckFormatJSONRegression(t *testing.T) {
	dir := t.TempDir()
	// Net-zero scalar (total 2 → 2) isolates the per-file-count regression:
	// b.go leaves, c.go arrives as a new offender.
	base := buildConfig("", dimDef{
		id: "m", direction: "lower-is-better", gate: "per-file-count",
		command: `echo '{"value": 2, "breakdown": {"a.go:1": 1, "b.go:1": 1}}'`,
	})
	mustRecord(t, dir, base)
	worse := buildConfig("", dimDef{
		id: "m", direction: "lower-is-better", gate: "per-file-count",
		command: `echo '{"value": 2, "breakdown": {"a.go:1": 1, "c.go:1": 1}}'`,
	})
	writeFile(t, dir, "pawl.yaml", worse)

	res := runPawl(t, dir, baseEnv(), "check", "--format", "json")
	if res.exit != 1 {
		t.Fatalf("process exit = %d, want 1\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	r := parseReport(t, res.stdout)
	if r.ExitCode != 1 {
		t.Errorf("exit_code = %d, want 1", r.ExitCode)
	}
	m, ok := metricByID(r, "m")
	if !ok {
		t.Fatalf("metric m absent: %+v", r)
	}
	if m.Status != "worse" || m.Improved {
		t.Errorf("status/improved = %q/%v, want worse/false", m.Status, m.Improved)
	}
	if len(m.Regressions) != 1 {
		t.Fatalf("regressions = %+v, want exactly 1 (new offender c.go)", m.Regressions)
	}
	reg := m.Regressions[0]
	if reg.Kind != "per-file-count" {
		t.Errorf("kind = %q, want per-file-count", reg.Kind)
	}
	if reg.Key == nil || *reg.Key != "c.go:1" {
		t.Errorf("key = %v, want c.go:1", reg.Key)
	}
	if reg.Path == nil || *reg.Path != "c.go" {
		t.Errorf("path = %v, want c.go", reg.Path)
	}
	if reg.Line == nil || *reg.Line != 1 {
		t.Errorf("line = %v, want 1", reg.Line)
	}
	if reg.Base != 0 || reg.Current != 1 {
		t.Errorf("base/current = %v/%v, want 0/1 (offender counts)", reg.Base, reg.Current)
	}
	if reg.Message == "" {
		t.Errorf("message is empty, want the text-mode detail line")
	}
	if reg.Suppressed {
		t.Errorf("suppressed = true, want false in full mode")
	}
}

// A clean run reports empty regressions, a non-worse status, exit 0.
func TestCheckFormatJSONCleanRun(t *testing.T) {
	dir := t.TempDir()
	config := buildConfig("", dimDef{id: "m", direction: "lower-is-better", command: `echo '{"value": 5}'`})
	mustRecord(t, dir, config)

	res := runPawl(t, dir, baseEnv(), "check", "--format", "json")
	if res.exit != 0 {
		t.Fatalf("process exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	r := parseReport(t, res.stdout)
	m, _ := metricByID(r, "m")
	if len(m.Regressions) != 0 {
		t.Errorf("regressions = %+v, want empty", m.Regressions)
	}
	if m.Status != "same" && m.Status != "better" {
		t.Errorf("status = %q, want same or better", m.Status)
	}
}

// A dimension present in config but absent from the snapshot reports base null,
// status "new".
func TestCheckFormatJSONNewDimension(t *testing.T) {
	dir := t.TempDir()
	mustRecord(t, dir, buildConfig("", dimDef{id: "a", direction: "lower-is-better", command: `echo '{"value": 1}'`}))
	withNew := buildConfig("",
		dimDef{id: "a", direction: "lower-is-better", command: `echo '{"value": 1}'`},
		dimDef{id: "b", direction: "lower-is-better", command: `echo '{"value": 9}'`},
	)
	writeFile(t, dir, "pawl.yaml", withNew)

	res := runPawl(t, dir, baseEnv(), "check", "--format", "json")
	r := parseReport(t, res.stdout)
	m, ok := metricByID(r, "b")
	if !ok {
		t.Fatalf("metric b absent: %+v", r)
	}
	if m.Base != nil {
		t.Errorf("base = %v, want null for a new dimension", *m.Base)
	}
	if m.Status != "new" {
		t.Errorf("status = %q, want new", m.Status)
	}
}

// An improved dimension reports improved true, status "better".
func TestCheckFormatJSONImprovedDimension(t *testing.T) {
	dir := t.TempDir()
	mustRecord(t, dir, buildConfig("", dimDef{id: "m", direction: "lower-is-better", command: `echo '{"value": 10}'`}))
	writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{id: "m", direction: "lower-is-better", command: `echo '{"value": 5}'`}))

	res := runPawl(t, dir, baseEnv(), "check", "--format", "json")
	r := parseReport(t, res.stdout)
	m, _ := metricByID(r, "m")
	if !m.Improved {
		t.Errorf("improved = false, want true")
	}
	if m.Status != "better" {
		t.Errorf("status = %q, want better", m.Status)
	}
}

// stdout stays pure JSON even under GITHUB_ACTIONS: no ::error:: annotation for
// the regression and no ::notice:: for the improvement — both are suppressed in
// json mode so the object is machine-parseable.
func TestCheckFormatJSONStdoutPurityUnderCI(t *testing.T) {
	dir := t.TempDir()
	base := buildConfig("",
		dimDef{id: "reg", direction: "lower-is-better", command: `echo '{"value": 5}'`},
		dimDef{id: "imp", direction: "lower-is-better", command: `echo '{"value": 10}'`},
	)
	mustRecord(t, dir, base)
	changed := buildConfig("",
		dimDef{id: "reg", direction: "lower-is-better", command: `echo '{"value": 8}'`},
		dimDef{id: "imp", direction: "lower-is-better", command: `echo '{"value": 5}'`},
	)
	writeFile(t, dir, "pawl.yaml", changed)

	res := runPawl(t, dir, baseEnv("GITHUB_ACTIONS=true"), "check", "--format", "json")
	if strings.Contains(res.stdout, "::error::") || strings.Contains(res.stdout, "::notice::") {
		t.Errorf("stdout carries GitHub annotations in json mode: %s", res.stdout)
	}
	parseReport(t, res.stdout) // must still be one clean JSON object
}

// A total-gate regression's JSON entry has kind "total" and key/path/line all
// null — a scalar has no line to attribute.
func TestCheckFormatJSONTotalRegression(t *testing.T) {
	dir := t.TempDir()
	mustRecord(t, dir, buildConfig("", dimDef{id: "m", direction: "lower-is-better", command: `echo '{"value": 5}'`}))
	writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{id: "m", direction: "lower-is-better", command: `echo '{"value": 8}'`}))

	res := runPawl(t, dir, baseEnv(), "check", "--format", "json")
	if res.exit != 1 {
		t.Fatalf("process exit = %d, want 1\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	r := parseReport(t, res.stdout)
	m, _ := metricByID(r, "m")
	if len(m.Regressions) != 1 {
		t.Fatalf("regressions = %+v, want 1", m.Regressions)
	}
	reg := m.Regressions[0]
	if reg.Kind != "total" {
		t.Errorf("kind = %q, want total", reg.Kind)
	}
	if reg.Key != nil || reg.Path != nil || reg.Line != nil {
		t.Errorf("key/path/line = %v/%v/%v, want all null for a total regression", reg.Key, reg.Path, reg.Line)
	}
}

// record --format json emits the verdict object AND writes the snapshot.
func TestRecordFormatJSONEmitsObjectAndWritesSnapshot(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{id: "m", direction: "lower-is-better", command: `echo '{"value": 3}'`}))

	res := runPawl(t, dir, baseEnv(), "record", "--format", "json")
	if res.exit != 0 {
		t.Fatalf("record exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	r := parseReport(t, res.stdout)
	if r.Command != "record" {
		t.Errorf("command = %q, want record", r.Command)
	}
	if got := readSnapshot(t, filepath.Join(dir, "pawl.snapshot.json")).Metrics["m"].Value; got != 3 {
		t.Errorf("snapshot value = %v, want 3 (record must still write the snapshot)", got)
	}
}

// diff --format json lists regressions but reports exit_code 0 — diff never
// fails, and its JSON must say so.
func TestDiffFormatJSONListsRegressionsButExitZero(t *testing.T) {
	dir := t.TempDir()
	mustRecord(t, dir, buildConfig("", dimDef{id: "m", direction: "lower-is-better", command: `echo '{"value": 5}'`}))
	writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{id: "m", direction: "lower-is-better", command: `echo '{"value": 8}'`}))

	res := runPawl(t, dir, baseEnv(), "diff", "--format", "json")
	if res.exit != 0 {
		t.Fatalf("diff exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	r := parseReport(t, res.stdout)
	if r.Command != "diff" {
		t.Errorf("command = %q, want diff", r.Command)
	}
	if r.ExitCode != 0 {
		t.Errorf("exit_code = %d, want 0 (diff never fails)", r.ExitCode)
	}
	m, _ := metricByID(r, "m")
	if len(m.Regressions) == 0 {
		t.Errorf("regressions empty, want the scalar regression listed even though diff exits 0")
	}
}
