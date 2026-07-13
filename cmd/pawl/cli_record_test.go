package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// record writes the snapshot with an exact, pinned byte shape: 2-space
// indent, trailing newline, per-metric field order
// direction/value/unit/breakdown/tolerance, metric ids sorted, breakdown
// null when the adapter omitted one, and tolerance present ONLY when the
// dimension declared it (undeclared tolerance is omitted, not zero).
func TestRecordSnapshotExactByteShape(t *testing.T) {
	dir := t.TempDir()
	tol := 1.5
	writeFile(t, dir, "pawl.yaml", buildConfig("",
		// declared second in the config, but must sort before "b" in the snapshot
		dimDef{
			id: "b", direction: "lower-is-better",
			command: `echo '{"value": 2}'`,
		},
		dimDef{
			id: "a", direction: "higher-is-better", tolerance: &tol,
			command: `echo '{"value": 5, "unit": "widgets", "breakdown": {"x.go": 1}}'`,
		},
	))

	res := runPawl(t, dir, baseEnv(), "record")
	if res.exit != 0 {
		t.Fatalf("record exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}

	got := readFile(t, filepath.Join(dir, "pawl.snapshot.json"))
	want := `{
  "metrics": {
    "a": {
      "direction": "higher-is-better",
      "value": 5,
      "unit": "widgets",
      "breakdown": {
        "x.go": 1
      },
      "tolerance": 1.5
    },
    "b": {
      "direction": "lower-is-better",
      "value": 2,
      "unit": "count",
      "breakdown": null
    }
  }
}
`
	if got != want {
		t.Errorf("snapshot bytes mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// record prints its confirmation line after writing the snapshot.
func TestRecordPrintsWrittenLine(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
		id: "m", direction: "lower-is-better", command: `echo '{"value": 1}'`,
	}))
	res := runPawl(t, dir, baseEnv(), "record")
	if res.exit != 0 {
		t.Fatalf("record exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	if !strings.Contains(res.stdout, "📸 snapshot written to") {
		t.Errorf("stdout missing snapshot-written line: %s", res.stdout)
	}
}

// A custom `snapshot = "sub/dir/snap.json"` path, resolved relative to the
// config file's directory, is honored (including creating the subdirectory).
func TestRecordHonorsCustomSnapshotPath(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "pawl.yaml", buildConfig("sub/dir/snap.json", dimDef{
		id: "m", direction: "lower-is-better", command: `echo '{"value": 7}'`,
	}))
	res := runPawl(t, dir, baseEnv(), "record")
	if res.exit != 0 {
		t.Fatalf("record exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	custom := filepath.Join(dir, "sub", "dir", "snap.json")
	if _, err := os.Stat(custom); err != nil {
		t.Fatalf("expected snapshot at custom path %s: %v", custom, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "pawl.snapshot.json")); err == nil {
		t.Errorf("default-named snapshot should not exist when a custom path is configured")
	}
	snap := readSnapshot(t, custom)
	if snap.Metrics["m"].Value != 7 {
		t.Errorf("value at custom snapshot path = %v, want 7", snap.Metrics["m"].Value)
	}
}
