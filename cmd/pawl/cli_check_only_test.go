package main

import (
	"strings"
	"testing"
)

func twoDimConfig(aCmd, bCmd string) string {
	return buildConfig("",
		dimDef{id: "a", direction: "lower-is-better", command: aCmd},
		dimDef{id: "b", direction: "lower-is-better", command: bCmd},
	)
}

// check --only measures just the named dimension: a regression in an unlisted
// dimension must not fail the inner loop, and a broken unlisted adapter must
// not block it.
func TestCheckOnlyIgnoresUnlistedRegression(t *testing.T) {
	dir := t.TempDir()
	mustRecord(t, dir, twoDimConfig(`echo '{"value": 1}'`, `echo '{"value": 1}'`))
	writeFile(t, dir, "pawl.yaml", twoDimConfig(`echo '{"value": 1}'`, `echo '{"value": 9}'`))

	full := runPawl(t, dir, baseEnv(), "check")
	if full.exit != 1 {
		t.Fatalf("full check exit = %d, want 1 (b regressed)\nstdout=%s\nstderr=%s", full.exit, full.stdout, full.stderr)
	}

	only := runPawl(t, dir, baseEnv(), "check", "--only", "a", "--format", "json")
	if only.exit != 0 {
		t.Fatalf("check --only a exit = %d, want 0\nstdout=%s\nstderr=%s", only.exit, only.stdout, only.stderr)
	}
	r := parseReport(t, only.stdout)
	if _, ok := metricByID(r, "b"); ok {
		t.Errorf("check --only a report includes unlisted metric b: %+v", r.Metrics)
	}
	if _, ok := metricByID(r, "a"); !ok {
		t.Errorf("check --only a report missing metric a")
	}
}

func TestCheckOnlyStillFailsListedRegression(t *testing.T) {
	dir := t.TempDir()
	mustRecord(t, dir, twoDimConfig(`echo '{"value": 1}'`, `echo '{"value": 1}'`))
	writeFile(t, dir, "pawl.yaml", twoDimConfig(`echo '{"value": 9}'`, `echo '{"value": 1}'`))

	res := runPawl(t, dir, baseEnv(), "check", "--only", "a")
	if res.exit != 1 {
		t.Fatalf("check --only a exit = %d, want 1\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
}

func TestCheckOnlySkipsBrokenUnlistedAdapter(t *testing.T) {
	dir := t.TempDir()
	mustRecord(t, dir, twoDimConfig(`echo '{"value": 1}'`, `echo '{"value": 1}'`))
	writeFile(t, dir, "pawl.yaml", twoDimConfig(
		`echo '{"value": 1}'`,
		`sh -c 'echo broken >&2; exit 1'`,
	))

	full := runPawl(t, dir, baseEnv(), "check")
	if full.exit != 2 {
		t.Fatalf("full check exit = %d, want 2 (b adapter broken)\nstderr=%s", full.exit, full.stderr)
	}
	only := runPawl(t, dir, baseEnv(), "check", "--only", "a")
	if only.exit != 0 {
		t.Fatalf("check --only a exit = %d, want 0 (broken b must not block)\nstdout=%s\nstderr=%s",
			only.exit, only.stdout, only.stderr)
	}
}

func TestCheckOnlyUnknownIDExitsTwo(t *testing.T) {
	dir := t.TempDir()
	mustRecord(t, dir, twoDimConfig(`echo '{"value": 1}'`, `echo '{"value": 1}'`))

	res := runPawl(t, dir, baseEnv(), "check", "--only", "nope")
	if res.exit != 2 {
		t.Fatalf("exit = %d, want 2\nstderr=%s", res.exit, res.stderr)
	}
	if !strings.Contains(res.stderr, "nope") {
		t.Errorf("stderr should name the unknown id: %s", res.stderr)
	}
}

func TestCheckOnlyCodeclimateExitsTwo(t *testing.T) {
	dir := t.TempDir()
	mustRecord(t, dir, twoDimConfig(`echo '{"value": 1}'`, `echo '{"value": 1}'`))

	res := runPawl(t, dir, baseEnv(), "check", "--only", "a", "--format", "codeclimate")
	if res.exit != 2 {
		t.Fatalf("exit = %d, want 2\nstderr=%s", res.exit, res.stderr)
	}
	if !strings.Contains(res.stderr, "removed") || !strings.Contains(res.stderr, "format json") {
		t.Errorf("stderr should explain the migration from codeclimate: %s", res.stderr)
	}
}

func TestUnknownCommandWinsOverCodeclimateMigration(t *testing.T) {

	res := runPawl(t, t.TempDir(), baseEnv(), "not-a-command", "--format", "codeclimate")
	if res.exit != 2 {
		t.Fatalf("exit = %d, want 2\nstderr=%s", res.exit, res.stderr)
	}
	if !strings.Contains(res.stderr, `unknown command "not-a-command"`) {
		t.Fatalf("stderr should report the unknown command first: %s", res.stderr)
	}
	if strings.Contains(res.stderr, "codeclimate was removed") {
		t.Fatalf("unknown command should not be rewritten as a format migration: %s", res.stderr)
	}
}

// A --only verdict must say so in the JSON itself. Without it, `check --only a`
// (exit 0, one metric) is indistinguishable from a green full gate once the
// object leaves the invocation that produced it — a CI aggregator, a PR
// comment, a subagent handed the blob.
func TestOnlyJSONNamesNarrowedScope(t *testing.T) {
	dir := t.TempDir()
	config := twoDimConfig(`echo '{"value": 1}'`, `echo '{"value": 1}'`)
	mustRecord(t, dir, config)

	full := runPawl(t, dir, baseEnv(), "check", "--format", "json")
	if r := parseReport(t, full.stdout); r.Only != nil {
		t.Errorf("full check reported only = %v, want it omitted", r.Only)
	}

	for _, command := range []string{"check", "record"} {
		t.Run(command, func(t *testing.T) {
			res := runPawl(t, dir, baseEnv(), command, "--only", "b,a", "--format", "json")
			r := parseReport(t, res.stdout)
			if len(r.Only) != 2 || r.Only[0] != "a" || r.Only[1] != "b" {
				t.Errorf("%s --only b,a reported only = %v, want [a b] (sorted)", command, r.Only)
			}
		})
	}
}

// The same scope has to survive onto the exit-2 object, which is the one an
// agent reads when it cannot trust the verdict.
func TestOnlyJSONNamesScopeOnCouldNotMeasure(t *testing.T) {
	dir := t.TempDir()
	mustRecord(t, dir, twoDimConfig(`echo '{"value": 1}'`, `echo '{"value": 1}'`))
	writeFile(t, dir, "pawl.yaml", twoDimConfig(`sh -c 'echo broken >&2; exit 1'`, `echo '{"value": 1}'`))

	res := runPawl(t, dir, baseEnv(), "check", "--only", "a", "--format", "json")
	if res.exit != 2 {
		t.Fatalf("exit = %d, want 2 (a is broken)\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	r := parseReport(t, res.stdout)
	if len(r.Only) != 1 || r.Only[0] != "a" {
		t.Errorf("only = %v, want [a]", r.Only)
	}
}

// check --only must say what it left unmeasured, not just what it measured —
// an agent that scopes down to fix one broken dimension needs a way to
// notice the others still exist rather than a --only habit silently dropping
// a dimension from view forever. A real eval run (see demo/README.md for the
// harness) surfaced exactly this: an agent that never once looked back at
// the dimensions it had scoped out.
func TestOnlyJSONNamesExcludedDimensions(t *testing.T) {
	dir := t.TempDir()
	config := buildConfig("",
		dimDef{id: "a", direction: "lower-is-better", command: `echo '{"value": 1}'`},
		dimDef{id: "b", direction: "lower-is-better", command: `echo '{"value": 1}'`},
		dimDef{id: "c", direction: "lower-is-better", command: `echo '{"value": 1}'`},
	)
	mustRecord(t, dir, config)

	full := runPawl(t, dir, baseEnv(), "check", "--format", "json")
	if r := parseReport(t, full.stdout); r.Excluded != nil {
		t.Errorf("full check reported excluded = %v, want nil", r.Excluded)
	}

	res := runPawl(t, dir, baseEnv(), "check", "--only", "b", "--format", "json")
	if res.exit != 0 {
		t.Fatalf("check --only b exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	r := parseReport(t, res.stdout)
	if len(r.Excluded) != 2 || r.Excluded[0] != "a" || r.Excluded[1] != "c" {
		t.Errorf("excluded = %v, want [a c] (sorted)", r.Excluded)
	}
}

// The excluded scope has to survive onto the exit-2 could-not-measure
// object too — the same reasoning as TestOnlyJSONNamesScopeOnCouldNotMeasure,
// for the new field.
func TestOnlyJSONNamesExcludedDimensionsOnCouldNotMeasure(t *testing.T) {
	dir := t.TempDir()
	mustRecord(t, dir, twoDimConfig(`echo '{"value": 1}'`, `echo '{"value": 1}'`))
	writeFile(t, dir, "pawl.yaml", twoDimConfig(`sh -c 'echo broken >&2; exit 1'`, `echo '{"value": 1}'`))

	res := runPawl(t, dir, baseEnv(), "check", "--only", "a", "--format", "json")
	if res.exit != 2 {
		t.Fatalf("exit = %d, want 2 (a is broken)\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	r := parseReport(t, res.stdout)
	if len(r.Excluded) != 1 || r.Excluded[0] != "b" {
		t.Errorf("excluded = %v, want [b]", r.Excluded)
	}
}

// The text default must carry the same information as the JSON field, since
// most real agent runs never touch --format json at all.
func TestCheckOnlyTextAdvisesExcludedDimensions(t *testing.T) {
	dir := t.TempDir()
	config := buildConfig("",
		dimDef{id: "a", direction: "lower-is-better", command: `echo '{"value": 1}'`},
		dimDef{id: "b", direction: "lower-is-better", command: `echo '{"value": 1}'`},
	)
	mustRecord(t, dir, config)

	res := runPawl(t, dir, baseEnv(), "check", "--only", "a")
	if res.exit != 0 {
		t.Fatalf("check --only a exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	if !strings.Contains(res.stdout, "1 dimension(s) not measured this run (--only scope): b") {
		t.Errorf("stdout missing excluded-dimension advisory: %s", res.stdout)
	}

	full := runPawl(t, dir, baseEnv(), "check")
	if strings.Contains(full.stdout, "not measured this run") {
		t.Errorf("full check (no --only) printed an excluded-dimension advisory: %s", full.stdout)
	}
}

func TestCheckOnlyStillCatchesOrphansAgainstFullConfig(t *testing.T) {
	dir := t.TempDir()
	mustRecord(t, dir, twoDimConfig(`echo '{"value": 1}'`, `echo '{"value": 1}'`))
	writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{id: "a", direction: "lower-is-better", command: `echo '{"value": 1}'`}))

	res := runPawl(t, dir, baseEnv(), "check", "--only", "a")
	if res.exit != 2 {
		t.Fatalf("exit = %d, want 2 (orphan b must still fail)\nstderr=%s", res.exit, res.stderr)
	}
	if !strings.Contains(res.stderr, "orphaned") {
		t.Errorf("stderr should report the orphan: %s", res.stderr)
	}
}
