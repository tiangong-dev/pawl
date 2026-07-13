package main

import (
	"testing"
)

// The pattern-count builtin: value is the total match count across included
// files, breakdown keys are "<path>:<line>", multiple matches on one line
// both count toward value, and include/exclude globs are honored.
func TestBuiltinPatternCount(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.go", "package a\nx := 1 //nolint\ny := 2\nz := 3 //nolint\n")
	writeFile(t, dir, "b.go", "package b\nq := 1 //nolint //nolint\n")
	writeFile(t, dir, "excluded/c.go", "//nolint\n")
	writeFile(t, dir, "d.md", "//nolint\n")

	writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
		id: "nolint", direction: "lower-is-better", gate: "per-file-count", builtin: "pattern-count",
		optionLines: []string{
			`pattern = "//nolint"`,
			`include = ["**/*.go"]`,
			`exclude = ["excluded/**"]`,
		},
	}))

	res := runPawl(t, dir, baseEnv(), "record")
	if res.exit != 0 {
		t.Fatalf("record exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	snap := readSnapshot(t, dirJoin(dir, "pawl.snapshot.json"))
	m := snap.Metrics["nolint"]

	if m.Value != 4 {
		t.Errorf("value = %v, want 4 (2 in a.go + 2 on one line in b.go; excluded/ and .md must not count)", m.Value)
	}
	if m.Unit != "matches" {
		t.Errorf("unit = %q, want %q", m.Unit, "matches")
	}
	want := map[string]float64{"a.go:2": 1, "a.go:4": 1, "b.go:2": 2}
	if len(m.Breakdown) != len(want) {
		t.Fatalf("breakdown = %v, want %v", m.Breakdown, want)
	}
	for k, v := range want {
		if m.Breakdown[k] != v {
			t.Errorf("breakdown[%q] = %v, want %v (full breakdown: %v)", k, m.Breakdown[k], v, m.Breakdown)
		}
	}
}
