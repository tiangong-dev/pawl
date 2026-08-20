package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

var (
	definitionMarkerA = "TO" + "DO"
	definitionMarkerB = "FIX" + "ME"
)

func readSnapshotDocument(t *testing.T, document string) snapshotFile {
	t.Helper()
	var snapshot snapshotFile
	if err := json.Unmarshal([]byte(document), &snapshot); err != nil {
		t.Fatalf("measurement document is not JSON: %v", err)
	}
	return snapshot
}

func withoutDefinitions(t *testing.T, document string, ids ...string) string {
	t.Helper()
	var parsed map[string]any
	if err := json.Unmarshal([]byte(document), &parsed); err != nil {
		t.Fatalf("measurement document is not JSON: %v", err)
	}
	metrics, ok := parsed["metrics"].(map[string]any)
	if !ok {
		t.Fatal("measurement document has no metrics object")
	}
	for _, id := range ids {
		metric, ok := metrics[id].(map[string]any)
		if !ok {
			t.Fatalf("measurement document has no metric %q", id)
		}
		delete(metric, "definition")
	}
	out, err := json.MarshalIndent(parsed, "", "  ")
	if err != nil {
		t.Fatalf("marshal legacy measurement document: %v", err)
	}
	return string(out) + "\n"
}

func markerDefinitionConfig(pattern string) string {
	return `snapshot: "pawl.snapshot.json"
dimensions:
  - id: "markers"
    title: "markers"
    direction: "lower-is-better"
    gate: "per-file-count"
    builtin: "pattern-count"
    options:
      pattern: "` + pattern + `"
      include: ["*.txt"]
`
}

// A snapshot number is comparable only under the measurement definition that
// produced it. Changing the pattern must not turn a different measurement into
// a reported improvement against the old number.
func TestCheckRefusesChangedMeasurementDefinition(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "sample.txt", definitionMarkerA+"\n")
	mustRecord(t, dir, markerDefinitionConfig(definitionMarkerA))

	snapPath := filepath.Join(dir, "pawl.snapshot.json")
	before := readSnapshot(t, snapPath).Metrics["markers"].Definition
	if !strings.HasPrefix(before, "v1:sha256:") {
		t.Fatalf("recorded definition = %q, want versioned sha256 fingerprint", before)
	}

	writeFile(t, dir, "pawl.yaml", markerDefinitionConfig(definitionMarkerB))
	res := runPawl(t, dir, baseEnv(), "check", "--format", "json")
	if res.exit != 2 {
		t.Fatalf("check after definition change exit = %d, want 2\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	if !strings.Contains(res.stdout+res.stderr, "measurement definition") || !strings.Contains(res.stdout+res.stderr, "markers") {
		t.Errorf("definition mismatch does not name its cause and metric: stdout=%s stderr=%s", res.stdout, res.stderr)
	}

	// A full record is the explicit re-baseline operation. It replaces the
	// incompatible definition without calling 1 -> 0 a comparable improvement.
	record := runPawl(t, dir, baseEnv(), "record", "--format", "json")
	if record.exit != 0 {
		t.Fatalf("full record after definition change exit = %d\nstdout=%s\nstderr=%s", record.exit, record.stdout, record.stderr)
	}
	after := readSnapshot(t, snapPath).Metrics["markers"].Definition
	if after == before || !strings.HasPrefix(after, "v1:sha256:") {
		t.Errorf("definition was not replaced: before=%q after=%q", before, after)
	}
	if final := runPawl(t, dir, baseEnv(), "check"); final.exit != 0 {
		t.Fatalf("check after explicit re-baseline exit = %d\nstdout=%s\nstderr=%s", final.exit, final.stdout, final.stderr)
	}
}

// Presentation and execution-budget edits do not change what a dimension
// measures. They must not force a baseline reset.
func TestDefinitionFingerprintIgnoresTitleAndTimeout(t *testing.T) {
	dir := t.TempDir()
	mustRecord(t, dir, buildConfig("", dimDef{
		id: "m", title: "old title", direction: "lower-is-better", timeout: "1m", command: `echo '{"value": 1}'`,
	}))
	before := readSnapshot(t, filepath.Join(dir, "pawl.snapshot.json")).Metrics["m"].Definition

	writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
		id: "m", title: "new title", direction: "lower-is-better", timeout: "2m", command: `echo '{"value": 1}'`,
	}))
	if res := runPawl(t, dir, baseEnv(), "check"); res.exit != 0 {
		t.Fatalf("title/timeout-only change exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	measure := runPawl(t, dir, baseEnv(), "measure")
	if measure.exit != 0 {
		t.Fatalf("measure exit = %d: %s", measure.exit, measure.stderr)
	}
	writeFile(t, dir, "current.json", measure.stdout)
	current := readSnapshot(t, filepath.Join(dir, "current.json")).Metrics["m"].Definition
	if current != before {
		t.Errorf("non-semantic edit changed fingerprint: before=%q current=%q", before, current)
	}
}

func TestDefinitionFingerprintNormalizesExplicitBuiltinDefaults(t *testing.T) {
	dir := t.TempDir()
	mustRecord(t, dir, buildConfig("", dimDef{
		id: "length", direction: "lower-is-better", builtin: "file-length",
		optionLines: []string{`include = ["*.txt"]`},
	}))
	writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
		id: "length", direction: "lower-is-better", builtin: "file-length",
		optionLines: []string{`include = ["*.txt"]`, `threshold = 500`, `exclude = []`},
	}))
	if res := runPawl(t, dir, baseEnv(), "check"); res.exit != 0 {
		t.Fatalf("explicit defaults changed definition: exit=%d\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
}

func TestCurrentDocumentRejectsDifferentDefinition(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "sample.txt", definitionMarkerA+"\n")
	writeFile(t, dir, "pawl.yaml", markerDefinitionConfig(definitionMarkerA))
	measure := runPawl(t, dir, baseEnv(), "measure")
	if measure.exit != 0 {
		t.Fatalf("measure exit = %d: %s", measure.exit, measure.stderr)
	}
	writeFile(t, dir, "current.json", measure.stdout)
	writeFile(t, dir, "pawl.yaml", markerDefinitionConfig(definitionMarkerB))

	res := runPawl(t, dir, baseEnv(), "record", "--current", "current.json")
	if res.exit != 2 {
		t.Fatalf("record with stale --current exit = %d, want 2\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	if !strings.Contains(res.stderr, "supplied measurement uses a different measurement definition") {
		t.Errorf("stale --current diagnostic missing: %s", res.stderr)
	}
}

func TestRecordCurrentUpgradesLegacyMeasurementDefinition(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "sample.txt", definitionMarkerA+"\n")
	writeFile(t, dir, "pawl.yaml", markerDefinitionConfig(definitionMarkerA))
	measure := runPawl(t, dir, baseEnv(), "measure")
	if measure.exit != 0 {
		t.Fatalf("measure exit = %d: %s", measure.exit, measure.stderr)
	}
	expected := readSnapshotDocument(t, measure.stdout).Metrics["markers"].Definition
	writeFile(t, dir, "current.json", withoutDefinitions(t, measure.stdout, "markers"))

	res := runPawl(t, dir, baseEnv(), "record", "--current", "current.json")
	if res.exit != 0 {
		t.Fatalf("record legacy --current exit = %d\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	got := readSnapshot(t, filepath.Join(dir, "pawl.snapshot.json")).Metrics["markers"].Definition
	if got != expected || !strings.HasPrefix(got, "v1:sha256:") {
		t.Fatalf("recorded definition = %q, want current %q", got, expected)
	}
}

func TestRecordOnlyCurrentUpgradesSelectedAndPreservesUnselectedLegacyDefinition(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "sample.txt", definitionMarkerA+"\n")
	writeFile(t, dir, "pawl.yaml", twoMarkerDefinitions(definitionMarkerA, definitionMarkerA))
	initial := runPawl(t, dir, baseEnv(), "record")
	if initial.exit != 0 {
		t.Fatalf("initial record exit = %d: %s", initial.exit, initial.stderr)
	}
	snapshotPath := filepath.Join(dir, "pawl.snapshot.json")
	writeFile(t, dir, "pawl.snapshot.json", withoutDefinitions(t, readFile(t, snapshotPath), "a", "b"))

	measure := runPawl(t, dir, baseEnv(), "measure", "--only", "a")
	if measure.exit != 0 {
		t.Fatalf("scoped measure exit = %d: %s", measure.exit, measure.stderr)
	}
	expected := readSnapshotDocument(t, measure.stdout).Metrics["a"].Definition
	writeFile(t, dir, "current.json", withoutDefinitions(t, measure.stdout, "a"))
	res := runPawl(t, dir, baseEnv(), "record", "--only", "a", "--current", "current.json")
	if res.exit != 0 {
		t.Fatalf("scoped legacy --current exit = %d\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	after := readSnapshot(t, snapshotPath)
	if after.Metrics["a"].Definition != expected {
		t.Fatalf("selected definition = %q, want %q", after.Metrics["a"].Definition, expected)
	}
	if after.Metrics["b"].Definition != "" {
		t.Fatalf("unselected legacy definition changed to %q", after.Metrics["b"].Definition)
	}
}

// record --only may explicitly redefine a selected dimension, but it must not
// preserve an incompatible baseline for an unselected dimension and leave a
// snapshot that the next check cannot honestly compare.
func twoMarkerDefinitions(patternA, patternB string) string {
	return `dimensions:
  - id: "a"
    title: "a"
    direction: "lower-is-better"
    builtin: "pattern-count"
    options:
      pattern: "` + patternA + `"
      include: ["*.txt"]
  - id: "b"
    title: "b"
    direction: "lower-is-better"
    builtin: "pattern-count"
    options:
      pattern: "` + patternB + `"
      include: ["*.txt"]
`
}

func TestRecordOnlyRefusesUnselectedDefinitionMismatch(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "sample.txt", definitionMarkerA+"\n")
	mustRecord(t, dir, twoMarkerDefinitions(definitionMarkerA, definitionMarkerA))
	snapPath := filepath.Join(dir, "pawl.snapshot.json")
	before := readFile(t, snapPath)

	writeFile(t, dir, "sample.txt", "clean\n")
	writeFile(t, dir, "pawl.yaml", twoMarkerDefinitions(definitionMarkerA, definitionMarkerB))
	res := runPawl(t, dir, baseEnv(), "record", "--only", "a")
	if res.exit != 2 {
		t.Fatalf("record --only preserving mismatched b exit = %d, want 2\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	if !strings.Contains(res.stdout+res.stderr, "b") || !strings.Contains(res.stdout+res.stderr, "full `pawl record`") {
		t.Errorf("refusal does not name b and the migration path: stdout=%s stderr=%s", res.stdout, res.stderr)
	}
	if got := readFile(t, snapPath); got != before {
		t.Error("refused record --only modified the snapshot")
	}
}

func TestDefinitionFingerprintIncludesNamedAnalyzerAndIgnoresSetOrder(t *testing.T) {
	dir := t.TempDir()
	config := func(command, regex, rules string) string {
		return `analyzers:
  - id: "shared"
    builtin: "lines"
    options:
      command: "` + command + `"
      regex: "` + regex + `"
      valid_exit_codes: [0, 1]
dimensions:
  - id: "findings"
    title: "findings"
    direction: "lower-is-better"
    source: "shared"
    options:
      rules: ` + rules + `
      levels: ["warning", "error"]
`
	}
	regex := `^(?P<rule>[^:]+):(?P<level>[^:]+)$`
	mustRecord(t, dir, config("printf 'R:warning\\n'", regex, `["S", "R"]`))

	// Commands are replaceable adapter wiring, and selector/valid-exit lists
	// are sets, so these changes remain comparable.
	writeFile(t, dir, "pawl.yaml", strings.ReplaceAll(config("printf 'R:warning\\n'; true", regex, `["R", "S"]`), "[0, 1]", "[1, 0]"))
	if res := runPawl(t, dir, baseEnv(), "check"); res.exit != 0 {
		t.Fatalf("set reordering exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}

	// Changing how the analyzer decodes that report changes the metric.
	changedRegex := `^(?P<rule>R):(?P<level>warning)$`
	writeFile(t, dir, "pawl.yaml", config("printf 'R:warning\\n'", changedRegex, `["R", "S"]`))
	if res := runPawl(t, dir, baseEnv(), "check"); res.exit != 2 {
		t.Fatalf("named analyzer regex change exit = %d, want 2\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
}

func TestRecordOnlyCanExplicitlyRedefineSelectedDimension(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "sample.txt", definitionMarkerA+"\n")
	mustRecord(t, dir, twoMarkerDefinitions(definitionMarkerA, definitionMarkerA))
	before := readSnapshot(t, filepath.Join(dir, "pawl.snapshot.json"))
	writeFile(t, dir, "pawl.yaml", twoMarkerDefinitions(definitionMarkerB, definitionMarkerA))

	res := runPawl(t, dir, baseEnv(), "record", "--only", "a", "--format", "json")
	if res.exit != 0 {
		t.Fatalf("record --only selected redefinition exit = %d\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	after := readSnapshot(t, filepath.Join(dir, "pawl.snapshot.json"))
	if after.Metrics["a"].Definition == before.Metrics["a"].Definition {
		t.Error("selected dimension kept its old definition")
	}
	if after.Metrics["b"].Definition != before.Metrics["b"].Definition || after.Metrics["b"].Value != 1 {
		t.Error("unselected compatible dimension was not preserved verbatim")
	}
}
