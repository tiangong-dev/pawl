package main

import (
	"os"
	"path/filepath"
	"strings"
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

// Pairing total + per-key-value on file-length: a new file crossing the
// threshold moves the total; growing an already-long file is caught only by
// per-key-value. Together they close the "inflate a grandfathered file" hole.
func TestFileLengthTotalPlusPerKeyValuePair(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "long.txt", nLines(12))
	cfg := buildConfig("",
		dimDef{
			id: "file-length", direction: "lower-is-better", gate: "total",
			builtin:     "file-length",
			optionLines: []string{"threshold = 10", `include = ["**/*.txt"]`},
		},
		dimDef{
			id: "file-length-growth", direction: "lower-is-better", gate: "per-key-value",
			builtin:     "file-length",
			optionLines: []string{"threshold = 10", `include = ["**/*.txt"]`},
		},
	)
	mustRecord(t, dir, cfg)

	writeFile(t, dir, "long.txt", nLines(20))
	res := runPawl(t, dir, baseEnv(), "check", "--format", "json")
	if res.exit != 1 {
		t.Fatalf("growing a long file exit = %d, want 1\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	r := parseReport(t, res.stdout)
	total, _ := metricByID(r, "file-length")
	if total.Status == "worse" {
		t.Errorf("total gate status = %q, want same (count of over-limit files did not move)", total.Status)
	}
	growth, _ := metricByID(r, "file-length-growth")
	if growth.Status != "worse" {
		t.Errorf("per-key-value status = %q, want worse", growth.Status)
	}

	writeFile(t, dir, "long.txt", nLines(12))
	writeFile(t, dir, "new.txt", nLines(15))
	res = runPawl(t, dir, baseEnv(), "check", "--format", "json")
	if res.exit != 1 {
		t.Fatalf("new over-limit file exit = %d, want 1\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	r = parseReport(t, res.stdout)
	total, _ = metricByID(r, "file-length")
	if total.Status != "worse" {
		t.Errorf("total gate status = %q, want worse (new over-limit file)", total.Status)
	}
}

func TestFileLengthStrictPerKeyRejectsNewPathDespiteToleranceAndSince(t *testing.T) {
	dir := t.TempDir()
	homeDir := initGitRepo(t, dir)
	writeFile(t, dir, "old.txt", nLines(11))
	tolerance := 100.0
	config := buildConfig("", dimDef{
		id: "length", direction: "lower-is-better", gate: "per-key-value-strict", tolerance: &tolerance,
		builtin: "file-length", optionLines: []string{"threshold = 10", `include = ["*.txt"]`},
	})
	writeFile(t, dir, "pawl.yaml", config)
	if rec := runPawl(t, dir, gitEnv(homeDir), "record"); rec.exit != 0 {
		t.Fatalf("record exit=%d\nstdout=%s\nstderr=%s", rec.exit, rec.stdout, rec.stderr)
	}
	base := gitCommitAll(t, dir, homeDir, "strict per-key baseline")
	if err := os.Remove(filepath.Join(dir, "old.txt")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, dir, "new.txt", nLines(11))
	for _, args := range [][]string{{"check"}, {"check", "--since", base}} {
		res := runPawl(t, dir, gitEnv(homeDir), args...)
		if res.exit != 1 || !strings.Contains(res.stdout, "new key: new.txt") {
			t.Fatalf("%v exit=%d, want strict new-key regression despite tolerance\nstdout=%s\nstderr=%s", args, res.exit, res.stdout, res.stderr)
		}
	}
}

func TestFileLengthStrictPerKeyRejectsCopiedPath(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "old.txt", nLines(11))
	tolerance := 100.0
	config := buildConfig("", dimDef{
		id: "length", direction: "lower-is-better", gate: "per-key-value-strict", tolerance: &tolerance,
		builtin: "file-length", optionLines: []string{"threshold = 10", `include = ["*.txt"]`},
	})
	mustRecord(t, dir, config)
	writeFile(t, dir, "copy.txt", nLines(11))
	res := runPawl(t, dir, baseEnv(), "check")
	if res.exit != 1 || !strings.Contains(res.stdout, "copy.txt") {
		t.Fatalf("copy exit=%d, want strict new-key regression despite scalar tolerance\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
}

func TestGuardStrictPerKeyTreatsRenamedPathAsNew(t *testing.T) {
	dir := t.TempDir()
	homeDir := initGitRepo(t, dir)
	writeFile(t, dir, "old.txt", nLines(11))
	config := buildConfig("", dimDef{
		id: "length", direction: "lower-is-better", gate: "per-key-value-strict",
		builtin: "file-length", optionLines: []string{"threshold = 10", `include = ["*.txt"]`},
	})
	writeFile(t, dir, "pawl.yaml", config)
	if rec := runPawl(t, dir, gitEnv(homeDir), "record"); rec.exit != 0 {
		t.Fatalf("record exit=%d\nstdout=%s\nstderr=%s", rec.exit, rec.stdout, rec.stderr)
	}
	base := gitCommitAll(t, dir, homeDir, "strict per-key baseline")
	snapshotPath := filepath.Join(dir, "pawl.snapshot.json")
	snapshot, err := os.ReadFile(snapshotPath)
	if err != nil {
		t.Fatal(err)
	}
	updated := strings.Replace(string(snapshot), "old.txt", "new.txt", 1)
	if updated == string(snapshot) {
		t.Fatal("baseline snapshot had no old path key")
	}
	updated = strings.Replace(updated, `"new.txt": 11`, `"new.txt": 12`, 1)
	if err := os.WriteFile(snapshotPath, []byte(updated), 0o644); err != nil {
		t.Fatal(err)
	}
	res := runPawl(t, dir, gitEnv(homeDir), "guard", base)
	if res.exit != 1 || !strings.Contains(res.stdout, "new key: new.txt 0 → 12") {
		t.Fatalf("renamed key exit=%d, want strict new-key guard failure\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
}
