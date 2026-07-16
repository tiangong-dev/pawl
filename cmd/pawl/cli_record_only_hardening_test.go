package main

import (
	"strings"
	"testing"
)

// A configured dimension that is neither in `--only` nor in the existing
// snapshot stays absent from the written snapshot — and the rendered output
// (text/json/codeclimate) must not invent a measured-looking 0 for it. Guards
// pawl's honesty contract on the partial-record rendering path.
func TestRecordOnlyOmitsUnlistedAbsentDimFromOutput(t *testing.T) {
	dir := t.TempDir()

	// Baseline with only `a`.
	writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
		id: "a", direction: "lower-is-better", command: `echo '{"value": 1}'`,
	}))
	if res := runPawl(t, dir, baseEnv(), "record"); res.exit != 0 {
		t.Fatalf("seed record exit = %d\nstderr=%s", res.exit, res.stderr)
	}

	// Add a brand-new dimension `b`, then re-record only `a`.
	writeFile(t, dir, "pawl.yaml", buildConfig("",
		dimDef{id: "a", direction: "lower-is-better", command: `echo '{"value": 1}'`},
		dimDef{id: "b", direction: "lower-is-better", command: `echo '{"value": 9}'`},
	))

	jsonRes := runPawl(t, dir, baseEnv(), "record", "--only", "a", "--format", "json")
	if jsonRes.exit != 0 {
		t.Fatalf("record --only a --format json exit = %d\nstderr=%s", jsonRes.exit, jsonRes.stderr)
	}
	if strings.Contains(jsonRes.stdout, `"id": "b"`) {
		t.Errorf("json output invents an absent dimension b (must be omitted, not shown as current 0):\n%s", jsonRes.stdout)
	}
	if !strings.Contains(jsonRes.stdout, `"id": "a"`) {
		t.Errorf("json output is missing the re-recorded dimension a:\n%s", jsonRes.stdout)
	}

	textRes := runPawl(t, dir, baseEnv(), "record", "--only", "a")
	if textRes.exit != 0 {
		t.Fatalf("record --only a exit = %d\nstderr=%s", textRes.exit, textRes.stderr)
	}
	for _, line := range strings.Split(textRes.stdout, "\n") {
		fields := strings.Fields(line)
		if len(fields) > 0 && fields[0] == "b" {
			t.Errorf("text table lists absent dimension b as a metric row:\n%s", textRes.stdout)
		}
	}

	// The snapshot itself must still lack b.
	snap := readSnapshot(t, dirJoin(dir, "pawl.snapshot.json"))
	if _, ok := snap.Metrics["b"]; ok {
		t.Errorf("snapshot contains absent dimension b: %v", snap.Metrics)
	}
}

// --only is a record-only flag; it is a usage error on `version` too, not a
// silent version print (the version short-circuit must not swallow it).
func TestRecordOnlyRejectedOnVersion(t *testing.T) {
	dir := t.TempDir()
	for _, args := range [][]string{{"version", "--only", "a"}, {"--version", "--only", "a"}} {
		res := runPawl(t, dir, baseEnv(), args...)
		if res.exit != 2 {
			t.Errorf("pawl %v exit = %d, want 2 (--only is only valid on record)\nstdout=%s\nstderr=%s", args, res.exit, res.stdout, res.stderr)
		}
	}
}
