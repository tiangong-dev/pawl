package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// sharedExcludeMarker is the literal the shared-exclude fixtures count. It is
// chosen so it never collides with the debt markers other fixtures use.
const sharedExcludeMarker = "needle"

// sharedExcludeScanConfig builds a two-dimension config over the fixture tree:
// a pattern-count dimension and a file-length dimension, both scanning *.txt.
// options is appended to the pattern-count dimension, so a caller can give it
// its own exclude; the same string is appended to the file-length dimension.
func sharedExcludeScanConfig(options string) string {
	dimension := options
	return `dimensions:
  - id: "markers"
    title: "Markers"
    direction: "lower-is-better"
    gate: "per-file-count"
    builtin: "pattern-count"
    options:
      pattern: "` + sharedExcludeMarker + `"
      include: ["**/*.txt"]
` + dimension + `
  - id: "length"
    title: "Length"
    direction: "lower-is-better"
    builtin: "file-length"
    options:
      threshold: 1
      include: ["**/*.txt"]
` + dimension
}

// sharedExcludeTree writes a tree with one counted file, one file under the
// shared exclude, and one under a dimension-local exclude.
func sharedExcludeTree(t *testing.T, dir string) {
	t.Helper()
	writeFile(t, dir, "keep.txt", sharedExcludeMarker+"\n"+sharedExcludeMarker+"\n")
	writeFile(t, dir, "vendor/dep.txt", sharedExcludeMarker+"\n")
	writeFile(t, dir, "skip/extra.txt", sharedExcludeMarker+"\n")
}

// measureAll records nothing and returns every dimension's measurement for the
// config in dir.
func measureAll(t *testing.T, dir, config string) map[string]float64 {
	t.Helper()
	writeFile(t, dir, "pawl.yaml", config)
	res := runPawl(t, dir, baseEnv(), "measure", "--format", "json")
	if res.exit != 0 {
		t.Fatalf("measure exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	values := map[string]float64{}
	for id, metric := range readSnapshotDocument(t, res.stdout).Metrics {
		values[id] = metric.Value
	}
	return values
}

// A shared top-level exclude and the same glob repeated in the dimension must
// measure exactly the same files: the top level is one place to write the list,
// not a second, differently-applied one.
func TestSharedExcludeMeasuresSameValuesAsRepeatedExcludes(t *testing.T) {
	dir := t.TempDir()
	sharedExcludeTree(t, dir)

	repeated := sharedExcludeScanConfig(`      exclude: ["vendor/**"]
`)
	repeatedValues := measureAll(t, dir, repeated)
	if repeatedValues["markers"] != 3 || repeatedValues["length"] != 1 {
		t.Fatalf("repeated exclude measured %v, want markers=3 (vendor/dep.txt excluded; keep.txt twice and skip/extra.txt once) length=1 (only keep.txt is longer than 1 line)", repeatedValues)
	}

	shared := `exclude: ["vendor/**"]
` + sharedExcludeScanConfig("")
	sharedValues := measureAll(t, dir, shared)
	for id, want := range repeatedValues {
		if sharedValues[id] != want {
			t.Errorf("dimension %q measured %v under the top-level exclude, %v under the repeated one; the effective set must be identical", id, sharedValues[id], want)
		}
	}
}

// The dimension's own exclude keeps applying next to the shared list: moving a
// common glob to the top level must not drop a glob the dimension still lists.
func TestDimensionOwnExcludeStillAppliesAlongsideSharedExclude(t *testing.T) {
	dir := t.TempDir()
	sharedExcludeTree(t, dir)

	own := `exclude: ["vendor/**"]
` + sharedExcludeScanConfig(`      exclude: ["skip/**"]
`)
	ownValues := measureAll(t, dir, own)
	if ownValues["markers"] != 2 {
		t.Errorf("dimension own exclude + shared exclude measured markers = %v, want 2 (both vendor/dep.txt and skip/extra.txt excluded)", ownValues["markers"])
	}

	widened := `exclude: ["vendor/**", "skip/**"]
` + sharedExcludeScanConfig("")
	sharedValues := measureAll(t, dir, widened)
	for id, want := range ownValues {
		if sharedValues[id] != want {
			t.Errorf("dimension %q measured %v with skip/** at the top level, %v with it in the dimension", id, sharedValues[id], want)
		}
	}
}

// check compares recorded numbers only under the definition that produced them.
// Lifting the repeated globs to the top level keeps the definition, so the
// baseline stays valid; a genuinely different effective set is a redefinition.
func TestSharedExcludeMigrationKeepsRecordedDefinitionComparable(t *testing.T) {
	dir := t.TempDir()
	sharedExcludeTree(t, dir)

	repeated := sharedExcludeScanConfig(`      exclude: ["vendor/**"]
`)
	mustRecord(t, dir, repeated)

	shared := `exclude: ["vendor/**"]
` + sharedExcludeScanConfig("")
	writeFile(t, dir, "pawl.yaml", shared)
	if res := runPawl(t, dir, baseEnv(), "check"); res.exit != 0 {
		t.Fatalf("check after moving excludes to the top level exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	} else if strings.Contains(res.stdout+res.stderr, "measurement definition") {
		t.Errorf("moving the excludes reported a definition change:\nstdout=%s\nstderr=%s", res.stdout, res.stderr)
	}

	widened := `exclude: ["vendor/**", "skip/**"]
` + sharedExcludeScanConfig("")
	writeFile(t, dir, "pawl.yaml", widened)
	res := runPawl(t, dir, baseEnv(), "check")
	if res.exit != 2 {
		t.Fatalf("check after widening the effective exclude exit = %d, want 2\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	if !strings.Contains(res.stdout+res.stderr, "measurement definition") || !strings.Contains(res.stdout+res.stderr, "markers") {
		t.Errorf("widened exclude does not report a definition change for markers: stdout=%s stderr=%s", res.stdout, res.stderr)
	}
}

// guard reads the definitions recorded in the two snapshots, so the same
// migration must not read as redefined there either, while a changed effective
// set must.
func TestGuardTreatsSharedExcludeMigrationAsTheSameDefinition(t *testing.T) {
	dir := t.TempDir()
	homeDir := initGitRepo(t, dir)
	sharedExcludeTree(t, dir)

	repeated := sharedExcludeScanConfig(`      exclude: ["vendor/**"]
`)
	writeFile(t, dir, "pawl.yaml", repeated)
	if res := runPawl(t, dir, gitEnv(homeDir), "record"); res.exit != 0 {
		t.Fatalf("record exit = %d\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	base := gitCommitAll(t, dir, homeDir, "initial baseline with repeated excludes")

	shared := `exclude: ["vendor/**"]
` + sharedExcludeScanConfig("")
	writeFile(t, dir, "pawl.yaml", shared)
	if res := runPawl(t, dir, gitEnv(homeDir), "record"); res.exit != 0 {
		t.Fatalf("record after migration exit = %d\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	res := runPawl(t, dir, gitEnv(homeDir), "guard", base)
	if res.exit != 0 {
		t.Fatalf("guard exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	if strings.Contains(res.stdout+res.stderr, "measurement definition changed") {
		t.Errorf("guard reported the migration as a redefinition:\nstdout=%s\nstderr=%s", res.stdout, res.stderr)
	}

	widened := `exclude: ["vendor/**", "skip/**"]
` + sharedExcludeScanConfig("")
	writeFile(t, dir, "pawl.yaml", widened)
	if res := runPawl(t, dir, gitEnv(homeDir), "record"); res.exit != 0 {
		t.Fatalf("record after widening exit = %d\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	// Widening the effective exclude changes the recorded definition, so guard
	// compares it only once the commit that changes the config carries a
	// Pawl-Redefine trailer naming the affected dimensions.
	gitCommitAll(t, dir, homeDir, "widen the shared exclude\n\nPawl-Redefine: length, markers\n")
	res = runPawl(t, dir, gitEnv(homeDir), "guard", base)
	if res.exit != 0 {
		t.Fatalf("guard after widening exit = %d, want 0 (redefinition is reported, not failed)\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	if !strings.Contains(res.stdout+res.stderr, "measurement definition changed") {
		t.Errorf("guard did not report the widened effective exclude as a redefinition:\nstdout=%s\nstderr=%s", res.stdout, res.stderr)
	}
}

// The migration must not be forced through a full re-baseline: the recorded
// fingerprint is what check trusts, so it has to be identical.
func TestSharedExcludeMigrationKeepsRecordedFingerprint(t *testing.T) {
	dir := t.TempDir()
	sharedExcludeTree(t, dir)

	repeated := sharedExcludeScanConfig(`      exclude: ["vendor/**"]
`)
	mustRecord(t, dir, repeated)
	before := readSnapshot(t, filepath.Join(dir, "pawl.snapshot.json")).Metrics["markers"].Definition

	shared := `exclude: ["vendor/**"]
` + sharedExcludeScanConfig("")
	writeFile(t, dir, "pawl.yaml", shared)
	if res := runPawl(t, dir, baseEnv(), "record"); res.exit != 0 {
		t.Fatalf("record after migration exit = %d\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	after := readSnapshot(t, filepath.Join(dir, "pawl.snapshot.json")).Metrics["markers"].Definition
	if before == "" || after != before {
		t.Errorf("definition = %q after migration, want the recorded %q", after, before)
	}
}
