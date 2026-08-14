package main

import "testing"

func TestBuiltinFileBytes(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "small.txt", "abc")
	writeFile(t, dir, "big.txt", stringsRepeatBytes(20))
	writeFile(t, dir, "excluded/huge.txt", stringsRepeatBytes(50))
	writeFile(t, dir, "other.md", stringsRepeatBytes(50))

	writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
		id: "fb", direction: "lower-is-better", builtin: "file-bytes",
		optionLines: []string{
			"threshold = 10",
			`include = ["**/*.txt"]`,
			`exclude = ["excluded/**"]`,
		},
	}))

	res := runPawl(t, dir, baseEnv(), "record")
	if res.exit != 0 {
		t.Fatalf("record exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	snap := readSnapshot(t, dirJoin(dir, "pawl.snapshot.json"))
	m := snap.Metrics["fb"]
	if m.Value != 1 {
		t.Errorf("value = %v, want 1 (only big.txt is over 10 bytes)", m.Value)
	}
	if m.Unit != "files > 10 bytes" {
		t.Errorf("unit = %q, want %q", m.Unit, "files > 10 bytes")
	}
	if m.Breakdown["big.txt"] != 20 {
		t.Errorf("breakdown[big.txt] = %v, want 20", m.Breakdown["big.txt"])
	}
	if _, ok := m.Breakdown["small.txt"]; ok {
		t.Errorf("small.txt must not be in the breakdown")
	}
	if _, ok := m.Breakdown["excluded/huge.txt"]; ok {
		t.Errorf("excluded file must not be in the breakdown")
	}
}

func TestBuiltinFileBytesMissingIncludeExitsTwo(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
		id: "fb", direction: "lower-is-better", builtin: "file-bytes",
		optionLines: []string{"threshold = 10"},
	}))
	res := runPawl(t, dir, baseEnv(), "record")
	if res.exit != 2 {
		t.Fatalf("exit = %d, want 2\nstderr=%s", res.exit, res.stderr)
	}
}

func stringsRepeatBytes(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = 'x'
	}
	return string(b)
}
