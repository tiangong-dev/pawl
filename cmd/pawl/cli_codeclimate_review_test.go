package main

// Regression coverage for the Code Quality output's location + fingerprint
// contract: a stable fingerprint must survive a run-varying offender count, the
// path:line split must tolerate a colon inside the path, and an "unknown" line
// (≤ 0) must not become a bogus Code Quality entry.

import "testing"

func ccEntriesFor(t *testing.T, breakdownJSON string) []map[string]any {
	t.Helper()
	dir := t.TempDir()
	config := buildConfig("", dimDef{
		id: "lint-issues", title: "Lint issues",
		direction: "lower-is-better", gate: "per-file-count",
		command: "echo '" + breakdownJSON + "'",
	})
	mustRecord(t, dir, config)
	res := runPawl(t, dir, baseEnv(), "check", "--format", "codeclimate")
	return parseCodeclimate(t, res.stdout)
}

// The fingerprint keys the location only, so the same offender keeps its id even
// when its count (and thus the ×n description) changes between runs.
func TestCodeclimateFingerprintStableWhenCountChanges(t *testing.T) {
	one := ccEntriesFor(t, `{"value": 1, "breakdown": {"a.go:3": 1}}`)
	two := ccEntriesFor(t, `{"value": 2, "breakdown": {"a.go:3": 2}}`)
	if len(one) != 1 || len(two) != 1 {
		t.Fatalf("want one entry each, got %d and %d", len(one), len(two))
	}
	if one[0]["fingerprint"] != two[0]["fingerprint"] {
		t.Errorf("fingerprint changed with count: %v vs %v", one[0]["fingerprint"], two[0]["fingerprint"])
	}
	// Guard that the descriptions really did differ — otherwise the test would
	// pass trivially without exercising the count-varying path.
	if one[0]["description"] == two[0]["description"] {
		t.Errorf("descriptions should differ (×2 suffix): both %v", one[0]["description"])
	}
	if two[0]["description"] != "Lint issues ×2" {
		t.Errorf("count-2 description = %v, want \"Lint issues ×2\"", two[0]["description"])
	}
}

// The path:line split is on the LAST colon, so a path containing a colon keeps
// its line rather than being dropped.
func TestCodeclimatePathWithColonSplitsOnLastColon(t *testing.T) {
	entries := ccEntriesFor(t, `{"value": 1, "breakdown": {"pkg:a.go:10": 1}}`)
	if len(entries) != 1 {
		t.Fatalf("want 1 entry, got %d: %+v", len(entries), entries)
	}
	path, line := entryLocation(t, entries[0])
	if path != "pkg:a.go" || line != 10 {
		t.Errorf("location = %q:%d, want pkg:a.go:10", path, line)
	}
}

// A line ≤ 0 is the adapter's "unknown line"; it must be skipped, not emitted as
// an invalid begin:0 entry.
func TestCodeclimateSkipsUnknownLineZero(t *testing.T) {
	entries := ccEntriesFor(t, `{"value": 2, "breakdown": {"a.go:0": 1, "b.go:5": 1}}`)
	if len(entries) != 1 {
		t.Fatalf("want only the b.go:5 entry, got %d: %+v", len(entries), entries)
	}
	path, line := entryLocation(t, entries[0])
	if path != "b.go" || line != 5 {
		t.Errorf("surviving entry = %q:%d, want b.go:5", path, line)
	}
}
