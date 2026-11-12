package main

import (
	"strings"
	"testing"
)

const guardPerFileConfig = `dimensions:
  - id: "markers"
    title: "MARKER comments"
    direction: "lower-is-better"
    gate: "per-file-count"
    builtin: "pattern-count"
    options:
      pattern: "MARKER"
      include: ["**/*.go"]
`

// 3 offenders in a.go, none in b.go.
const guardFlatTotalBaseSnapshot = `{"metrics":{"markers":{"direction":"lower-is-better","value":3,"unit":"offenders","breakdown":{"a.go:1":1,"a.go:2":1,"a.go:3":1}}}}` + "\n"

// Same scalar total, but one offender moved into b.go.
const guardFlatTotalPRSnapshot = `{"metrics":{"markers":{"direction":"lower-is-better","value":3,"unit":"offenders","breakdown":{"a.go:1":1,"a.go:2":1,"b.go:1":1}}}}` + "\n"

// On a per-file-count gate guard compares the breakdown too: a scalar total that
// held still cannot hide an offender landing in a file that had none.
func TestGuardPerFileCountCatchesFlatTotal(t *testing.T) {
	dir := t.TempDir()
	homeDir := initGitRepo(t, dir)
	writeFile(t, dir, "pawl.yaml", guardPerFileConfig)
	writeFile(t, dir, "pawl.snapshot.json", guardFlatTotalBaseSnapshot)
	base := gitCommitAll(t, dir, homeDir, "baseline: three offenders in a.go")

	writeFile(t, dir, "pawl.snapshot.json", guardFlatTotalPRSnapshot)
	res := runPawl(t, dir, gitEnv(homeDir), "guard", base)
	if res.exit != 1 {
		t.Fatalf("exit = %d, want 1\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	if !strings.Contains(res.stdout, "b.go 0 → 1") {
		t.Errorf("stdout does not name the file that regressed: %s", res.stdout)
	}

	// A trailer covering the metric's total accepts the per-file violation too:
	// the author accepted the metric's debt, wherever inside it the count moved.
	gitCommitAll(t, dir, homeDir, "accept the offender landing in b.go\n\nPawl-Accept: markers 3\n")
	res2 := runPawl(t, dir, gitEnv(homeDir), "guard", base)
	if res2.exit != 0 {
		t.Fatalf("exit = %d, want 0 with an accept trailer\nstdout=%s\nstderr=%s", res2.exit, res2.stdout, res2.stderr)
	}
	if !strings.Contains(res2.stdout, "accepted regression") || !strings.Contains(res2.stdout, "b.go 0 → 1") {
		t.Errorf("stdout missing the accepted-regression detail: %s", res2.stdout)
	}
}

// A trailer that declares less than the metric now sits at does not cover the
// per-file violation — the snapshot moved past what was accepted.
func TestGuardPerFileCountTrailerMustCoverTheTotal(t *testing.T) {
	dir := t.TempDir()
	homeDir := initGitRepo(t, dir)
	writeFile(t, dir, "pawl.yaml", guardPerFileConfig)
	writeFile(t, dir, "pawl.snapshot.json", guardFlatTotalBaseSnapshot)
	base := gitCommitAll(t, dir, homeDir, "baseline: three offenders in a.go")

	writeFile(t, dir, "pawl.snapshot.json", guardFlatTotalPRSnapshot)
	gitCommitAll(t, dir, homeDir, "accept less than the metric now is\n\nPawl-Accept: markers 2\n")

	res := runPawl(t, dir, gitEnv(homeDir), "guard", base)
	if res.exit != 1 {
		t.Fatalf("exit = %d, want 1 (trailer declares 2, the metric is at 3)\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	if !strings.Contains(res.stdout, "b.go 0 → 1") {
		t.Errorf("stdout missing the violation line: %s", res.stdout)
	}
}

// Shrinking the metric passes: a file that shrank, a file that disappeared, and
// a total that improved all clear the per-file predicate.
func TestGuardPerFileCountPassesOnShrinkingFiles(t *testing.T) {
	dir := t.TempDir()
	homeDir := initGitRepo(t, dir)
	writeFile(t, dir, "pawl.yaml", guardPerFileConfig)
	writeFile(t, dir, "pawl.snapshot.json",
		`{"metrics":{"markers":{"direction":"lower-is-better","value":3,"unit":"offenders","breakdown":{"a.go:1":1,"a.go:2":1,"b.go:4":1}}}}`+"\n")
	base := gitCommitAll(t, dir, homeDir, "baseline: offenders in two files")

	for _, tc := range []struct {
		name    string
		metric  string
		missing string
	}{
		{
			name:    "one file deleted, the other unchanged",
			metric:  `{"direction":"lower-is-better","value":2,"unit":"offenders","breakdown":{"a.go:1":1,"a.go:2":1}}`,
			missing: "b.go",
		},
		{
			name:    "every file smaller and the total better",
			metric:  `{"direction":"lower-is-better","value":1,"unit":"offenders","breakdown":{"a.go:1":1}}`,
			missing: "regressed",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			writeFile(t, dir, "pawl.snapshot.json", `{"metrics":{"markers":`+tc.metric+`}}`+"\n")
			res := runPawl(t, dir, gitEnv(homeDir), "guard", base)
			if res.exit != 0 {
				t.Fatalf("exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
			}
			if strings.Contains(res.stdout, tc.missing) {
				t.Errorf("stdout mentions %q for a shrinking snapshot: %s", tc.missing, res.stdout)
			}
		})
	}
}

const guardRedefineDefinitions = `dimensions:
  - id: "a"
    title: "A"
    direction: "lower-is-better"
    command: "echo '{\"value\": 5}'"
  - id: "b"
    title: "B"
    direction: "lower-is-better"
    command: "echo '{\"value\": 5}'"
`

// Changing a metric's recorded definition is not comparable, so guard requires
// the author to state the change was deliberate in a commit trailer.
func TestGuardRedefinedMetricNeedsRedefineTrailer(t *testing.T) {
	defA := "v1:sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	defB := "v1:sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	dir := t.TempDir()
	homeDir := initGitRepo(t, dir)
	writeFile(t, dir, "pawl.yaml", guardRedefineDefinitions)
	writeFile(t, dir, "pawl.snapshot.json", `{"metrics":{"a":{"direction":"lower-is-better","definition":"`+defA+`","value":5,"unit":"count","breakdown":null}}}`+"\n")
	base := gitCommitAll(t, dir, homeDir, "baseline under the old definition")

	writeFile(t, dir, "pawl.snapshot.json", `{"metrics":{"a":{"direction":"lower-is-better","definition":"`+defB+`","value":9,"unit":"count","breakdown":null}}}`+"\n")

	res := runPawl(t, dir, gitEnv(homeDir), "guard", base)
	if res.exit != 1 {
		t.Fatalf("exit = %d, want 1 (redefinition without a trailer)\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	if !strings.Contains(res.stdout, "Pawl-Redefine: a") {
		t.Errorf("stdout does not name the missing trailer: %s", res.stdout)
	}

	// Accepting a value is not confirming a definition, and a trailer naming no
	// id confirms nothing: neither may quietly satisfy the redefinition.
	writeFile(t, dir, "note.txt", "an accept trailer is not a redefine trailer\n")
	gitCommitAll(t, dir, homeDir, "wrong trailer\n\nPawl-Accept: a 9\n")
	if res := runPawl(t, dir, gitEnv(homeDir), "guard", base); res.exit != 1 {
		t.Fatalf("Pawl-Accept exit = %d, want 1 (it does not confirm a definition)\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}

	writeFile(t, dir, "note2.txt", "an empty trailer confirms nothing\n")
	gitCommitAll(t, dir, homeDir, "empty trailer\n\nPawl-Redefine:\n")
	if res := runPawl(t, dir, gitEnv(homeDir), "guard", base); res.exit != 1 {
		t.Fatalf("empty Pawl-Redefine exit = %d, want 1 (no id confirmed)\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}

	writeFile(t, dir, "note3.txt", "why the definition changed\n")
	gitCommitAll(t, dir, homeDir, "the config change is deliberate\n\nPawl-Redefine: a\n")
	res2 := runPawl(t, dir, gitEnv(homeDir), "guard", base)
	if res2.exit != 0 {
		t.Fatalf("exit = %d, want 0 with the trailer\nstdout=%s\nstderr=%s", res2.exit, res2.stdout, res2.stderr)
	}
	if !strings.Contains(res2.stdout, "measurement definition changed for: a") {
		t.Errorf("stdout missing the redefinition notice: %s", res2.stdout)
	}
}

// One trailer line may confirm several redefined dimensions at once.
func TestGuardRedefineTrailerNamesSeveralIDs(t *testing.T) {
	defA := "v1:sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	defB := "v1:sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	metric := func(definition string) string {
		return `{"direction":"lower-is-better","definition":"` + definition + `","value":5,"unit":"count","breakdown":null}`
	}
	dir := t.TempDir()
	homeDir := initGitRepo(t, dir)
	writeFile(t, dir, "pawl.yaml", guardRedefineDefinitions)
	writeFile(t, dir, "pawl.snapshot.json",
		`{"metrics":{"a":`+metric(defA)+`,"b":`+metric(defA)+`}}`+"\n")
	base := gitCommitAll(t, dir, homeDir, "baseline under the old definition")

	writeFile(t, dir, "pawl.snapshot.json",
		`{"metrics":{"a":`+metric(defB)+`,"b":`+metric(defB)+`}}`+"\n")
	res := runPawl(t, dir, gitEnv(homeDir), "guard", base)
	if res.exit != 1 {
		t.Fatalf("exit = %d, want 1 (neither id confirmed)\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}

	gitCommitAll(t, dir, homeDir, "one line confirming both dimensions\n\nPawl-Redefine: a, b\n")
	res2 := runPawl(t, dir, gitEnv(homeDir), "guard", base)
	if res2.exit != 0 {
		t.Fatalf("exit = %d, want 0 (one trailer line confirmed both ids)\nstdout=%s\nstderr=%s", res2.exit, res2.stdout, res2.stderr)
	}
}

// A snapshot predating fingerprints is the legacy compatibility state: the
// upgrade is not a redefinition and needs no trailer.
func TestGuardLegacyDefinitionUpgradeNeedsNoTrailer(t *testing.T) {
	definition := "v1:sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	dir := t.TempDir()
	homeDir := initGitRepo(t, dir)
	writeFile(t, dir, "pawl.yaml", guardConfig)
	writeFile(t, dir, "pawl.snapshot.json", `{"metrics":{"a":{"direction":"lower-is-better","value":5,"unit":"count","breakdown":null}}}`+"\n")
	base := gitCommitAll(t, dir, homeDir, "legacy baseline without a definition")

	writeFile(t, dir, "pawl.snapshot.json", `{"metrics":{"a":{"direction":"lower-is-better","definition":"`+definition+`","value":5,"unit":"count","breakdown":null}}}`+"\n")
	res := runPawl(t, dir, gitEnv(homeDir), "guard", base)
	if res.exit != 0 {
		t.Fatalf("exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	if strings.Contains(res.stdout, "Pawl-Redefine") || strings.Contains(res.stdout, "measurement definition changed") {
		t.Errorf("a legacy upgrade must not read as a redefinition: %s", res.stdout)
	}
}

// A PR that relaxes a dimension's gate is not a silent bypass: the gate is part
// of the definition fingerprint, so guard sees the redefinition and demands a
// Pawl-Redefine trailer instead of quietly comparing against the weaker
// predicate.
func TestGuardGateChangeLandsInRedefinition(t *testing.T) {
	dir := t.TempDir()
	homeDir := initGitRepo(t, dir)
	writeFile(t, dir, "pawl.yaml", guardPerFileConfig)
	writeFile(t, dir, "a.go", "package a\n// MARKER one\n// MARKER two\n")
	writeFile(t, dir, "b.go", "package b\n")
	gitCommitAll(t, dir, homeDir, "two offenders in a.go")

	if rec := runPawl(t, dir, gitEnv(homeDir), "record"); rec.exit != 0 {
		t.Fatalf("record exit = %d, want 0\nstdout=%s\nstderr=%s", rec.exit, rec.stdout, rec.stderr)
	}
	base := gitCommitAll(t, dir, homeDir, "record the per-file-count baseline")

	// Relax the gate and move one offender into b.go: the total is unchanged,
	// so a total-gated comparison alone would see a clean snapshot.
	writeFile(t, dir, "pawl.yaml", strings.Replace(guardPerFileConfig, `gate: "per-file-count"`, `gate: "total"`, 1))
	writeFile(t, dir, "a.go", "package a\n// MARKER one\n")
	writeFile(t, dir, "b.go", "package b\n// MARKER two\n")
	if rec := runPawl(t, dir, gitEnv(homeDir), "record"); rec.exit != 0 {
		t.Fatalf("record after the gate change exit = %d, want 0\nstdout=%s\nstderr=%s", rec.exit, rec.stdout, rec.stderr)
	}
	gitCommitAll(t, dir, homeDir, "relax the gate to total and re-record")

	res := runPawl(t, dir, gitEnv(homeDir), "guard", base)
	if res.exit != 1 {
		t.Fatalf("exit = %d, want 1 (a relaxed gate must be redefinition, not a silent pass)\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	if !strings.Contains(res.stdout, "Pawl-Redefine: markers") {
		t.Errorf("stdout does not name the missing trailer: %s", res.stdout)
	}

	writeFile(t, dir, "note.txt", "why the gate was relaxed\n")
	gitCommitAll(t, dir, homeDir, "the gate change is deliberate\n\nPawl-Redefine: markers\n")
	if res2 := runPawl(t, dir, gitEnv(homeDir), "guard", base); res2.exit != 0 {
		t.Fatalf("exit = %d, want 0 with the trailer\nstdout=%s\nstderr=%s", res2.exit, res2.stdout, res2.stderr)
	}
}
