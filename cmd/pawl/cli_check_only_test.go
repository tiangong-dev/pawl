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

func TestDiffOnlyAccepted(t *testing.T) {
	dir := t.TempDir()
	mustRecord(t, dir, twoDimConfig(`echo '{"value": 1}'`, `echo '{"value": 1}'`))
	writeFile(t, dir, "pawl.yaml", twoDimConfig(`echo '{"value": 1}'`, `echo '{"value": 9}'`))

	res := runPawl(t, dir, baseEnv(), "diff", "--only", "a")
	if res.exit != 0 {
		t.Fatalf("diff --only a exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
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

	for _, command := range []string{"check", "diff", "record"} {
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
