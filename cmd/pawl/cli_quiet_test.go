package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func quietConfig(t *testing.T, dir string, lines int) {
	t.Helper()
	writeFile(t, dir, "a.txt", strings.Repeat("x\n", lines))
	writeFile(t, dir, "pawl.yaml", buildConfig("",
		dimDef{id: "a", direction: "lower-is-better", command: `printf '{"value": %s}' "$(wc -l < a.txt | tr -d ' ')"`},
	))
}

// A gate that passes has nothing to say the exit code does not already say.
func TestQuietCheckSaysNothingWhenEverythingHolds(t *testing.T) {
	dir := t.TempDir()
	quietConfig(t, dir, 3)
	if res := runPawl(t, dir, baseEnv(), "record"); res.exit != 0 {
		t.Fatalf("record exit = %d\nstderr=%s", res.exit, res.stderr)
	}

	loud := runPawl(t, dir, baseEnv(), "check")
	if loud.stdout == "" && loud.stderr == "" {
		t.Fatal("the non-quiet run printed nothing; this test cannot show a difference")
	}

	res := runPawl(t, dir, baseEnv(), "check", "-q")
	if res.exit != 0 {
		t.Fatalf("check -q exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	if res.stdout != "" || res.stderr != "" {
		t.Fatalf("check -q on a clean gate should be silent\nstdout=%q\nstderr=%q", res.stdout, res.stderr)
	}
}

// Quiet is not silent: a regression is exactly the information the exit code
// cannot carry, so it still prints.
func TestQuietCheckStillReportsARegression(t *testing.T) {
	dir := t.TempDir()
	quietConfig(t, dir, 3)
	if res := runPawl(t, dir, baseEnv(), "record"); res.exit != 0 {
		t.Fatalf("record exit = %d\nstderr=%s", res.exit, res.stderr)
	}
	writeFile(t, dir, "a.txt", strings.Repeat("x\n", 9))

	res := runPawl(t, dir, baseEnv(), "check", "--quiet")
	if res.exit != 1 {
		t.Fatalf("check --quiet exit = %d, want 1\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	if !strings.Contains(res.stdout, "3") || !strings.Contains(res.stdout, "9") {
		t.Fatalf("the regression detail is missing from a quiet run:\n%s", res.stdout)
	}
}

// A run that could not measure must never be quieted into looking like a pass.
func TestQuietCheckStillReportsAMeasurementFailure(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "pawl.yaml", buildConfig("",
		dimDef{id: "a", direction: "lower-is-better", command: `echo '{"value": 1}'`}))
	if res := runPawl(t, dir, baseEnv(), "record"); res.exit != 0 {
		t.Fatalf("record exit = %d\nstderr=%s", res.exit, res.stderr)
	}
	writeFile(t, dir, "pawl.yaml", buildConfig("",
		dimDef{id: "a", direction: "lower-is-better", command: `echo nope; exit 7`}))

	res := runPawl(t, dir, baseEnv(), "check", "-q")
	if res.exit != 2 {
		t.Fatalf("check -q exit = %d, want 2\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	if !strings.Contains(res.stderr, "exit status 7") {
		t.Fatalf("the failure reason is missing from a quiet run:\n%s", res.stderr)
	}
}

// Quiet suppresses pawl's chatter, not the diagnosis of a tool that is failing
// — losing an adapter's own stderr is losing the only clue it gave.
func TestQuietKeepsTheAdaptersOwnStderr(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "pawl.yaml", buildConfig("",
		dimDef{id: "a", direction: "lower-is-better", command: `echo "cannot reach the registry" >&2; exit 4`}))

	res := runPawl(t, dir, baseEnv(), "record", "-q")
	if res.exit != 2 {
		t.Fatalf("record -q exit = %d, want 2\nstderr=%s", res.exit, res.stderr)
	}
	if !strings.Contains(res.stderr, "cannot reach the registry") {
		t.Fatalf("the adapter's own stderr was swallowed by -q:\n%s", res.stderr)
	}
	if strings.Contains(res.stderr, "measuring a…") {
		t.Fatalf("-q did not suppress pawl's progress lines:\n%s", res.stderr)
	}
}

// A caller parsing the verdict must always receive one; quiet governs chatter,
// not the machine-readable output.
func TestQuietStillEmitsTheJSONVerdict(t *testing.T) {
	dir := t.TempDir()
	quietConfig(t, dir, 3)
	if res := runPawl(t, dir, baseEnv(), "record"); res.exit != 0 {
		t.Fatalf("record exit = %d\nstderr=%s", res.exit, res.stderr)
	}

	res := runPawl(t, dir, baseEnv(), "check", "-q", "--format", "json")
	if res.exit != 0 {
		t.Fatalf("check -q --format json exit = %d, want 0\nstderr=%s", res.exit, res.stderr)
	}
	var verdict struct {
		Command  string `json:"command"`
		ExitCode int    `json:"exit_code"`
	}
	if err := json.Unmarshal([]byte(res.stdout), &verdict); err != nil {
		t.Fatalf("quiet swallowed the JSON verdict: %v\nstdout=%q", err, res.stdout)
	}
	if verdict.Command != "check" || verdict.ExitCode != 0 {
		t.Fatalf("verdict = %+v", verdict)
	}
	if res.stderr != "" {
		t.Fatalf("quiet should still silence stderr chatter, got:\n%s", res.stderr)
	}
}

// measure's document is its output, so quiet only silences the progress lines.
func TestQuietMeasureKeepsTheDocument(t *testing.T) {
	dir := t.TempDir()
	quietConfig(t, dir, 3)

	res := runPawl(t, dir, baseEnv(), "measure", "-q")
	if res.exit != 0 {
		t.Fatalf("measure -q exit = %d\nstderr=%s", res.exit, res.stderr)
	}
	if !strings.Contains(res.stdout, `"value": 3`) {
		t.Fatalf("measure -q dropped the document:\n%s", res.stdout)
	}
	if res.stderr != "" {
		t.Fatalf("measure -q should silence progress, got:\n%s", res.stderr)
	}
}

// A quiet record that writes nothing because it refused must still say so.
func TestQuietRecordStillReportsARefusal(t *testing.T) {
	dir := t.TempDir()
	quietConfig(t, dir, 3)
	if res := runPawl(t, dir, baseEnv(), "record"); res.exit != 0 {
		t.Fatalf("record exit = %d\nstderr=%s", res.exit, res.stderr)
	}
	writeFile(t, dir, "a.txt", strings.Repeat("x\n", 9))

	res := runPawl(t, dir, baseEnv(), "record", "-q")
	if res.exit != 1 {
		t.Fatalf("record -q exit = %d, want 1\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	if !strings.Contains(res.stdout, "--accept-worse") {
		t.Fatalf("the refusal and its remedy are missing from a quiet run:\n%s", res.stdout)
	}
}

// A successful quiet record is silent; the snapshot on disk is the result.
func TestQuietRecordIsSilentWhenItWrites(t *testing.T) {
	dir := t.TempDir()
	quietConfig(t, dir, 3)

	res := runPawl(t, dir, baseEnv(), "record", "-q")
	if res.exit != 0 {
		t.Fatalf("record -q exit = %d\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	if res.stdout != "" || res.stderr != "" {
		t.Fatalf("record -q should be silent on success\nstdout=%q\nstderr=%q", res.stdout, res.stderr)
	}
	if got := readSnapshot(t, dirJoin(dir, "pawl.snapshot.json")).Metrics["a"].Value; got != 3 {
		t.Fatalf("record -q wrote %v, want 3", got)
	}
}

func TestQuietRejectedOnCommandsWithNoChatter(t *testing.T) {
	dir := t.TempDir()
	quietConfig(t, dir, 3)
	for _, command := range []string{"rank", "trend", "init", "agent", "version"} {
		res := runPawl(t, dir, baseEnv(), command, "-q")
		if res.exit != 2 {
			t.Errorf("%s -q exit = %d, want 2\nstdout=%s", command, res.exit, res.stdout)
		}
		if !strings.Contains(res.stderr, "--quiet is only valid") {
			t.Errorf("%s -q: stderr should reject the flag, got:\n%s", command, res.stderr)
		}
	}
}
