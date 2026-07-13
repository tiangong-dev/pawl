package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type snapshotFile struct {
	Metrics map[string]struct {
		Direction string             `json:"direction"`
		Value     float64            `json:"value"`
		Unit      string             `json:"unit"`
		Breakdown map[string]float64 `json:"breakdown"`
		Tolerance *float64           `json:"tolerance"`
	} `json:"metrics"`
}

func readSnapshot(t *testing.T, path string) snapshotFile {
	t.Helper()
	var snap snapshotFile
	if err := json.Unmarshal([]byte(readFile(t, path)), &snap); err != nil {
		t.Fatalf("snapshot at %s is not valid JSON: %v\ncontent:\n%s", path, err, readFile(t, path))
	}
	return snap
}

// exit 0 + valid JSON on stdout is a measurement; unit defaults to "count"
// when the adapter omits it.
func TestExecAdapterMeasuresValueAndDefaultsUnit(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
		id: "m", direction: "lower-is-better", command: `echo '{"value": 3}'`,
	}))
	res := runPawl(t, dir, baseEnv(), "record")
	if res.exit != 0 {
		t.Fatalf("record exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	snap := readSnapshot(t, filepath.Join(dir, "pawl.snapshot.json"))
	m, ok := snap.Metrics["m"]
	if !ok {
		t.Fatalf("snapshot missing metric m: %+v", snap)
	}
	if m.Value != 3 {
		t.Errorf("value = %v, want 3", m.Value)
	}
	if m.Unit != "count" {
		t.Errorf("unit = %q, want default \"count\"", m.Unit)
	}
}

// The adapter runs with PAWL_ROOT set to the absolute config directory.
func TestExecAdapterSeesPawlRootEnv(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
		id:        "m",
		direction: "lower-is-better",
		command:   `cd "$PAWL_ROOT" && pwd > pawl_root_seen.txt; echo '{"value": 1}'`,
	}))
	res := runPawl(t, dir, baseEnv(), "record")
	if res.exit != 0 {
		t.Fatalf("record exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	seen := strings.TrimSpace(readFile(t, filepath.Join(dir, "pawl_root_seen.txt")))
	wantInfo, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat config dir: %v", err)
	}
	gotInfo, err := os.Stat(seen)
	if err != nil {
		t.Fatalf("PAWL_ROOT %q does not resolve to a real directory: %v", seen, err)
	}
	if !os.SameFile(wantInfo, gotInfo) {
		t.Errorf("PAWL_ROOT resolved to %q, want the config directory %q", seen, dir)
	}
}

// The adapter's stderr is passed through to pawl's own stderr for humans to
// read, even though only stdout is parsed as the measurement.
func TestExecAdapterStderrPassesThrough(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
		id: "m", direction: "lower-is-better",
		command: `echo diag-message-xyz 1>&2; echo '{"value": 1}'`,
	}))
	res := runPawl(t, dir, baseEnv(), "record")
	if res.exit != 0 {
		t.Fatalf("record exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	if !strings.Contains(res.stderr, "diag-message-xyz") {
		t.Errorf("stderr does not contain adapter diagnostic output: %s", res.stderr)
	}
}

// A non-zero adapter exit is a measurement FAILURE, not a measured zero — the
// whole run aborts with exit 2 and an existing snapshot must not be
// clobbered with a fabricated zero.
func TestExecAdapterNonZeroExitAbortsAndDoesNotRecordZero(t *testing.T) {
	dir := t.TempDir()
	goodConfig := buildConfig("", dimDef{
		id: "m", direction: "lower-is-better", command: `echo '{"value": 5}'`,
	})
	writeFile(t, dir, "pawl.yaml", goodConfig)
	res := runPawl(t, dir, baseEnv(), "record")
	if res.exit != 0 {
		t.Fatalf("initial record exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	snapPath := filepath.Join(dir, "pawl.snapshot.json")
	before := readSnapshot(t, snapPath)
	if before.Metrics["m"].Value != 5 {
		t.Fatalf("precondition: initial value = %v, want 5", before.Metrics["m"].Value)
	}

	failingConfig := buildConfig("", dimDef{
		id: "m", direction: "lower-is-better",
		command: `echo '{"value": 5}'; exit 1`,
	})
	writeFile(t, dir, "pawl.yaml", failingConfig)
	res = runPawl(t, dir, baseEnv(), "record")
	if res.exit != 2 {
		t.Fatalf("record with failing adapter exit = %d, want 2\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}

	after := readSnapshot(t, snapPath)
	if after.Metrics["m"].Value != 5 {
		t.Errorf("snapshot value after failed measurement = %v, want unchanged 5 (not recorded as 0)", after.Metrics["m"].Value)
	}
}

// stdout that is not JSON at all is a measurement failure.
func TestExecAdapterNonJSONStdoutExitsTwo(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
		id: "m", direction: "lower-is-better", command: `echo 'not json at all'`,
	}))
	res := runPawl(t, dir, baseEnv(), "record")
	if res.exit != 2 {
		t.Fatalf("exit = %d, want 2\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
}

// stdout JSON with no numeric value — value missing or wrong type — is a
// measurement failure, checked the same way SnapshotShapeErrors checks it.
func TestExecAdapterMissingOrNonNumericValueExitsTwo(t *testing.T) {
	cases := []struct {
		name    string
		command string
	}{
		{"value field missing", `echo '{"unit":"count"}'`},
		{"value field is a string", `echo '{"value":"nope"}'`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
				id: "m", direction: "lower-is-better", command: tc.command,
			}))
			res := runPawl(t, dir, baseEnv(), "record")
			if res.exit != 2 {
				t.Fatalf("exit = %d, want 2\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
			}
		})
	}
}

// A dimension timeout is enforced: a command that outlives it is a
// measurement failure (exit 2), and pawl does not wait out the full sleep.
func TestExecAdapterTimeoutExitsTwo(t *testing.T) {
	dir := t.TempDir()
	timeout := "100ms"
	writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
		id: "m", direction: "lower-is-better", timeout: timeout, command: "sleep 5",
	}))
	start := time.Now()
	res := runPawl(t, dir, baseEnv(), "record")
	elapsed := time.Since(start)
	if res.exit != 2 {
		t.Fatalf("exit = %d, want 2\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	if elapsed > 4*time.Second {
		t.Errorf("run took %v — the 100ms timeout does not appear to have been enforced (waited near the full 5s sleep)", elapsed)
	}
}

// Every dimension prints a "measuring <id>…" progress line to stderr as its
// measurement starts.
func TestExecAdapterPrintsProgressLinesPerDimension(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "pawl.yaml", buildConfig("",
		dimDef{id: "alpha", direction: "lower-is-better", command: `echo '{"value": 1}'`},
		dimDef{id: "beta", direction: "lower-is-better", command: `echo '{"value": 2}'`},
	))
	res := runPawl(t, dir, baseEnv(), "record")
	if res.exit != 0 {
		t.Fatalf("record exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	for _, want := range []string{"measuring alpha…", "measuring beta…"} {
		if !strings.Contains(res.stderr, want) {
			t.Errorf("stderr missing progress line %q: %s", want, res.stderr)
		}
	}
}
