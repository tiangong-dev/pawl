package main

import (
	"testing"
)

// The file-length builtin's line-count semantics: a trailing newline does
// not add a phantom line, so a file at exactly the threshold is not "over"
// and one line past it is. include/exclude globs are honored, and the
// breakdown reports relative paths with per-file line counts.
func TestBuiltinFileLength(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "exactly500.txt", nLines(500))
	writeFile(t, dir, "exactly501.txt", nLines(501))
	writeFile(t, dir, "small.txt", nLines(5))
	writeFile(t, dir, "excluded/big-but-excluded.txt", nLines(600))
	writeFile(t, dir, "other.md", nLines(600))

	writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
		id: "fl", direction: "lower-is-better", builtin: "file-length",
		optionLines: []string{
			"threshold = 500",
			`include = ["**/*.txt"]`,
			`exclude = ["excluded/**"]`,
		},
	}))

	res := runPawl(t, dir, baseEnv(), "record")
	if res.exit != 0 {
		t.Fatalf("record exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	snap := readSnapshot(t, dirJoin(dir, "pawl.snapshot.json"))
	m := snap.Metrics["fl"]

	if m.Value != 1 {
		t.Errorf("value = %v, want 1 (only exactly501.txt is over threshold)", m.Value)
	}
	if m.Unit != "files > 500 lines" {
		t.Errorf("unit = %q, want %q", m.Unit, "files > 500 lines")
	}
	wantBreakdown := map[string]float64{"exactly501.txt": 501}
	if len(m.Breakdown) != len(wantBreakdown) || m.Breakdown["exactly501.txt"] != 501 {
		t.Errorf("breakdown = %v, want %v", m.Breakdown, wantBreakdown)
	}
	for path := range m.Breakdown {
		if path != "exactly501.txt" {
			t.Errorf("breakdown has unexpected key %q (500-line file, excluded file, and .md file must all be absent)", path)
		}
	}
}

// The threshold is read from config, not hardcoded to 500.
func TestBuiltinFileLengthCustomThreshold(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.txt", nLines(2))
	writeFile(t, dir, "b.txt", nLines(4))

	writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
		id: "fl", direction: "lower-is-better", builtin: "file-length",
		optionLines: []string{
			"threshold = 3",
			`include = ["**/*.txt"]`,
		},
	}))

	res := runPawl(t, dir, baseEnv(), "record")
	if res.exit != 0 {
		t.Fatalf("record exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	snap := readSnapshot(t, dirJoin(dir, "pawl.snapshot.json"))
	m := snap.Metrics["fl"]
	if m.Value != 1 {
		t.Errorf("value = %v, want 1", m.Value)
	}
	if m.Unit != "files > 3 lines" {
		t.Errorf("unit = %q, want %q", m.Unit, "files > 3 lines")
	}
	if m.Breakdown["b.txt"] != 4 {
		t.Errorf("breakdown[b.txt] = %v, want 4", m.Breakdown["b.txt"])
	}
}

// An empty file is 0 lines and never counts as an offender.
func TestBuiltinFileLengthEmptyFileIsZeroLines(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "empty.txt", "")

	writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
		id: "fl", direction: "lower-is-better", builtin: "file-length",
		optionLines: []string{
			"threshold = 0",
			`include = ["**/*.txt"]`,
		},
	}))

	res := runPawl(t, dir, baseEnv(), "record")
	if res.exit != 0 {
		t.Fatalf("record exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	snap := readSnapshot(t, dirJoin(dir, "pawl.snapshot.json"))
	m := snap.Metrics["fl"]
	if m.Value != 0 {
		t.Errorf("value = %v, want 0 (an empty file has 0 lines, even with threshold 0)", m.Value)
	}
}
