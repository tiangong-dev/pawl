package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckBaseIgnoresWorkingSnapshotAndAcceptedDebt(t *testing.T) {
	dir := t.TempDir()
	home := initGitRepo(t, dir)
	config := buildConfig("", dimDef{id: "a", direction: "lower-is-better", command: `echo '{"value": 5}'`})
	mustRecord(t, dir, config)
	base := gitCommitAll(t, dir, home, "trusted baseline")

	writeFile(t, dir, "pawl.snapshot.json", `{"metrics":{"a":{"direction":"lower-is-better","value":100,"unit":"count","breakdown":null}}}`+"\n")
	writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{id: "a", direction: "lower-is-better", command: `echo '{"value": 8}'`}))
	gitCommitAll(t, dir, home, "accept value in snapshot history only\n\nPawl-Accept: a 8")

	ordinary := runPawl(t, dir, gitEnv(home), "check")
	if ordinary.exit != 0 {
		t.Fatalf("ordinary check changed behavior: exit=%d\nstdout=%s\nstderr=%s", ordinary.exit, ordinary.stdout, ordinary.stderr)
	}
	trusted := runPawl(t, dir, gitEnv(home), "check", "--base", base, "--format", "json")
	if trusted.exit != 1 {
		t.Fatalf("check --base exit = %d, want 1\nstdout=%s\nstderr=%s", trusted.exit, trusted.stdout, trusted.stderr)
	}
	r := parseReport(t, trusted.stdout)
	if r.BaseRef == nil || *r.BaseRef != base {
		t.Fatalf("base_ref = %v, want %q", r.BaseRef, base)
	}
	m, ok := metricByID(r, "a")
	if !ok || m.Base == nil || *m.Base != 5 || m.Current == nil || *m.Current != 8 {
		t.Fatalf("trusted comparison = %+v, want base 5/current 8", m)
	}
	if !strings.Contains(trusted.stdout, "total 5 → 8") {
		t.Errorf("trusted report does not show the ref's baseline: %s", trusted.stdout)
	}
	if err := os.Remove(filepath.Join(dir, "pawl.snapshot.json")); err != nil {
		t.Fatal(err)
	}
	deleted := runPawl(t, dir, gitEnv(home), "check", "--base", base)
	if deleted.exit != 1 {
		t.Fatalf("check --base after deleting the working snapshot = %d, want 1\nstdout=%s\nstderr=%s", deleted.exit, deleted.stdout, deleted.stderr)
	}
}

func TestCheckBaseWorksFromSymlinkedPWD(t *testing.T) {
	dir := t.TempDir()
	home := initGitRepo(t, dir)
	writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{id: "a", direction: "lower-is-better", command: `echo '{"value": 1}'`}))
	mustRecord(t, dir, readFile(t, filepath.Join(dir, "pawl.yaml")))
	base := gitCommitAll(t, dir, home, "base")
	link := filepath.Join(t.TempDir(), "repo-link")
	if err := os.Symlink(dir, link); err != nil {
		t.Fatal(err)
	}
	env := append(gitEnv(home), "PWD="+link)
	res := runPawl(t, link, env, "check", "--base", base, "--format", "json")
	if res.exit != 0 {
		t.Fatalf("check --base through symlink cwd exit=%d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
}

func TestCheckBaseRejectsSnapshotOutsideThroughSymlinkedDirectory(t *testing.T) {
	dir := t.TempDir()
	home := initGitRepo(t, dir)
	external := t.TempDir()
	writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{id: "a", direction: "lower-is-better", command: `echo '{"value": 1}'`}))
	if err := os.Remove(filepath.Join(dir, "pawl.yaml")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, dir, "pawl.yaml", "snapshot: snap/pawl.json\ndimensions:\n  - id: a\n    title: A\n    direction: lower-is-better\n    command: \"echo '{\\\"value\\\": 1}'\"\n")
	if err := os.Symlink(external, filepath.Join(dir, "snap")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, external, "pawl.json", `{"metrics":{"a":{"direction":"lower-is-better","value":1,"unit":"count","breakdown":null}}}`+"\n")
	base := gitCommitAll(t, dir, home, "snapshot directory symlink points outside")
	res := runPawl(t, dir, gitEnv(home), "check", "--base", base, "--format", "json")
	if res.exit != 2 || !strings.Contains(res.stdout, "outside the git repository") {
		t.Fatalf("outside snapshot path exit=%d, want 2\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
}

func TestCheckBaseDoesNotFollowSnapshotFileSymlink(t *testing.T) {
	dir := t.TempDir()
	home := initGitRepo(t, dir)
	writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{id: "a", direction: "lower-is-better", command: `echo '{"value": 1}'`}))
	external := filepath.Join(t.TempDir(), "trusted.json")
	if err := os.WriteFile(external, []byte(`{"metrics":{"a":{"direction":"lower-is-better","value":1,"unit":"count","breakdown":null}}}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, filepath.Join(dir, "pawl.snapshot.json")); err != nil {
		t.Fatal(err)
	}
	base := gitCommitAll(t, dir, home, "snapshot file symlinks outside")
	res := runPawl(t, dir, gitEnv(home), "check", "--base", base, "--format", "json")
	if res.exit != 2 || !strings.Contains(res.stdout, "snapshot") {
		t.Fatalf("outside snapshot file symlink exit=%d, want 2\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
}

func TestCheckBaseSupportsOldGitWithoutEndOfOptions(t *testing.T) {
	dir := t.TempDir()
	home := initGitRepo(t, dir)
	writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{id: "a", direction: "lower-is-better", command: `echo '{"value": 1}'`}))
	mustRecord(t, dir, readFile(t, filepath.Join(dir, "pawl.yaml")))
	base := gitCommitAll(t, dir, home, "base")
	actualGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	shimDir := t.TempDir()
	shim := "#!/bin/sh\nfor arg do\n  if [ \"$arg\" = \"--end-of-options\" ]; then echo 'unknown option' >&2; exit 129; fi\ndone\nexec '" + strings.ReplaceAll(actualGit, "'", "'\\''") + "' \"$@\"\n"
	writeFile(t, shimDir, "git", shim)
	if err := os.Chmod(filepath.Join(shimDir, "git"), 0o755); err != nil {
		t.Fatal(err)
	}
	env := []string{"PATH=" + shimDir + string(os.PathListSeparator) + os.Getenv("PATH"), "HOME=" + home, "GIT_CONFIG_NOSYSTEM=1"}
	res := runPawl(t, dir, env, "check", "--base", base, "--format", "json")
	if res.exit != 0 {
		t.Fatalf("old Git shim check --base exit=%d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	badRef := runPawl(t, dir, env, "check", "--base", "--help", "--format", "json")
	if badRef.exit != 2 || !strings.Contains(badRef.stdout, "must not begin with '-'") {
		t.Fatalf("option-like ref should be rejected safely: exit=%d\nstdout=%s\nstderr=%s", badRef.exit, badRef.stdout, badRef.stderr)
	}
}

func TestCheckBaseRequiresReadableRefSnapshot(t *testing.T) {
	for _, tc := range []struct {
		name  string
		ref   string
		want  string
		setup func(t *testing.T, dir, home string)
	}{
		{name: "invalid ref", ref: "missing-ref", want: "could not resolve"},
		{name: "missing snapshot", ref: "base", want: "no snapshot", setup: func(t *testing.T, dir, home string) {
			writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{id: "a", direction: "lower-is-better", command: `echo '{"value": 1}'`}))
			gitCommitAll(t, dir, home, "base without snapshot")
			runGit(t, dir, home, "tag", "base")
		}},
		{name: "malformed snapshot", ref: "base", want: "invalid shape", setup: func(t *testing.T, dir, home string) {
			writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{id: "a", direction: "lower-is-better", command: `echo '{"value": 1}'`}))
			writeFile(t, dir, "pawl.snapshot.json", `{"metrics":{"a":{"value":"bad"}}}`)
			gitCommitAll(t, dir, home, "base with malformed snapshot")
			runGit(t, dir, home, "tag", "base")
		}},
		{name: "invalid JSON", ref: "base", want: "snapshot", setup: func(t *testing.T, dir, home string) {
			writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{id: "a", direction: "lower-is-better", command: `echo '{"value": 1}'`}))
			writeFile(t, dir, "pawl.snapshot.json", "{")
			gitCommitAll(t, dir, home, "base with invalid json")
			runGit(t, dir, home, "tag", "base")
		}},
		{name: "invalid tolerance type", ref: "base", want: "tolerance", setup: func(t *testing.T, dir, home string) {
			writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{id: "a", direction: "lower-is-better", command: `echo '{"value": 1}'`}))
			writeFile(t, dir, "pawl.snapshot.json", `{"metrics":{"a":{"direction":"lower-is-better","value":1,"tolerance":"bad"}}}`)
			gitCommitAll(t, dir, home, "base with invalid tolerance")
			runGit(t, dir, home, "tag", "base")
		}},
		{name: "invalid breakdown value type", ref: "base", want: "breakdown", setup: func(t *testing.T, dir, home string) {
			writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{id: "a", direction: "lower-is-better", command: `echo '{"value": 1}'`}))
			writeFile(t, dir, "pawl.snapshot.json", `{"metrics":{"a":{"direction":"lower-is-better","value":1,"breakdown":{"key":"bad"}}}}`)
			gitCommitAll(t, dir, home, "base with invalid breakdown")
			runGit(t, dir, home, "tag", "base")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			home := initGitRepo(t, dir)
			if tc.setup != nil {
				tc.setup(t, dir, home)
			} else {
				writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{id: "a", direction: "lower-is-better", command: `echo '{"value": 1}'`}))
				gitCommitAll(t, dir, home, "initial")
			}
			res := runPawl(t, dir, gitEnv(home), "check", "--base", tc.ref, "--format", "json")
			if res.exit != 2 {
				t.Fatalf("exit = %d, want 2\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
			}
			report := parseReport(t, res.stdout)
			if report.BaseRef == nil || *report.BaseRef != tc.ref {
				t.Errorf("base_ref = %v, want %q", report.BaseRef, tc.ref)
			}
			if report.FailureClass != "could-not-measure" || !strings.Contains(strings.ToLower(report.Error), tc.want) {
				t.Errorf("error %q missing from exit-2 verdict: %+v", tc.want, report)
			}
		})
	}
}

func TestCheckBaseGitSpawnFailureIsStructured(t *testing.T) {
	dir := t.TempDir()
	home := initGitRepo(t, dir)
	writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{id: "a", direction: "lower-is-better", command: `echo '{"value": 1}'`}))
	writeFile(t, dir, "pawl.snapshot.json", `{"metrics":{"a":{"direction":"lower-is-better","value":1,"unit":"count","breakdown":null}}}`+"\n")
	base := gitCommitAll(t, dir, home, "base")
	missingGitPath := t.TempDir()
	writeFile(t, missingGitPath, "git", "not executable")
	t.Setenv("PATH", missingGitPath)
	env := []string{"PATH=" + missingGitPath, "HOME=" + home, "GIT_CONFIG_NOSYSTEM=1"}
	res := runPawl(t, dir, env, "check", "--base", base, "--format", "json")
	if res.exit != 2 {
		t.Fatalf("exit = %d, want 2 when git cannot start\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	report := parseReport(t, res.stdout)
	if report.BaseRef == nil || *report.BaseRef != base || report.FailureClass != "could-not-measure" {
		t.Errorf("git spawn failure verdict = %+v", report)
	}
	if !strings.Contains(strings.ToLower(report.Error), "executable file not found") {
		t.Errorf("error does not explain that git could not start: %s", report.Error)
	}
}

func TestCheckBaseCurrentInputFailureHasStructuredRef(t *testing.T) {
	dir := t.TempDir()
	home := initGitRepo(t, dir)
	writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{id: "a", direction: "lower-is-better", command: `echo '{"value": 1}'`}))
	writeFile(t, dir, "pawl.snapshot.json", `{"metrics":{"a":{"direction":"lower-is-better","value":1,"unit":"count","breakdown":null}}}`+"\n")
	base := gitCommitAll(t, dir, home, "base")
	writeFile(t, dir, "invalid.json", "{")
	for _, current := range []string{"missing.json", "invalid.json"} {
		t.Run(current, func(t *testing.T) {
			res := runPawl(t, dir, gitEnv(home), "check", "--base", base, "--current", current, "--format", "json")
			if res.exit != 2 {
				t.Fatalf("exit = %d, want 2\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
			}
			report := parseReport(t, res.stdout)
			if report.BaseRef == nil || *report.BaseRef != base || report.FailureClass != "could-not-measure" {
				t.Errorf("current-input failure verdict = %+v", report)
			}
		})
	}
}

func TestCheckRefusesInvalidTypedSnapshotFields(t *testing.T) {
	dir := t.TempDir()
	config := buildConfig("", dimDef{id: "a", direction: "lower-is-better", command: `echo '{"value": 1}'`})
	mustRecord(t, dir, config)
	writeFile(t, dir, "pawl.snapshot.json", `{"metrics":{"a":{"direction":"lower-is-better","value":1,"tolerance":"bad"}}}`)
	res := runPawl(t, dir, baseEnv(), "check")
	if res.exit != 2 {
		t.Fatalf("check exit = %d, want 2 for invalid typed snapshot field\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
}

func TestCheckBaseAllowsAddedAndRemovedDimensions(t *testing.T) {
	dir := t.TempDir()
	home := initGitRepo(t, dir)
	baseConfig := buildConfig("",
		dimDef{id: "a", direction: "lower-is-better", command: `echo '{"value": 1}'`},
		dimDef{id: "b", direction: "lower-is-better", command: `echo '{"value": 1}'`},
	)
	mustRecord(t, dir, baseConfig)
	base := gitCommitAll(t, dir, home, "base has a")

	addedConfig := buildConfig("",
		dimDef{id: "a", direction: "lower-is-better", command: `echo '{"value": 1}'`},
		dimDef{id: "b", direction: "lower-is-better", command: `echo '{"value": 1}'`},
		dimDef{id: "c", direction: "lower-is-better", command: `echo '{"value": 2}'`},
	)
	writeFile(t, dir, "pawl.yaml", addedConfig)
	added := runPawl(t, dir, gitEnv(home), "check", "--base", base, "--format", "json")
	if added.exit != 0 {
		t.Fatalf("new dimension exit = %d, want 0\nstdout=%s\nstderr=%s", added.exit, added.stdout, added.stderr)
	}
	r := parseReport(t, added.stdout)
	b, ok := metricByID(r, "c")
	if !ok || b.Base != nil || b.Status != "new" {
		t.Errorf("new dimension report = %+v, want no base and status new", b)
	}
	selected := runPawl(t, dir, gitEnv(home), "check", "--base", base, "--only", "c", "--format", "json")
	if selected.exit != 0 {
		t.Fatalf("new selected dimension exit = %d, want 0\nstdout=%s\nstderr=%s", selected.exit, selected.stdout, selected.stderr)
	}
	selectedReport := parseReport(t, selected.stdout)
	if len(selectedReport.Metrics) != 1 || selectedReport.Metrics[0].ID != "c" || selectedReport.Metrics[0].Base != nil {
		t.Errorf("selected new dimension report = %+v", selectedReport.Metrics)
	}

	writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{id: "a", direction: "lower-is-better", command: `echo '{"value": 1}'`}))
	writeFile(t, dir, "pawl.snapshot.json", `{"metrics":{"a":{"direction":"lower-is-better","value":1,"unit":"count","breakdown":null}}}`+"\n")
	removed := runPawl(t, dir, gitEnv(home), "check", "--base", base)
	if removed.exit != 0 {
		t.Fatalf("removed configured dimension exit = %d, want 0\nstdout=%s\nstderr=%s", removed.exit, removed.stdout, removed.stderr)
	}
}

func TestCheckBaseFlagRequiresRefAndCheckCommand(t *testing.T) {
	dir := t.TempDir()
	for _, args := range [][]string{{"check", "--base"}, {"record", "--base", "HEAD"}} {
		res := runPawl(t, dir, baseEnv(), args...)
		if res.exit != 2 {
			t.Errorf("pawl %s exit = %d, want 2\nstdout=%s\nstderr=%s", strings.Join(args, " "), res.exit, res.stdout, res.stderr)
		}
	}
}

func TestCheckBaseDefinitionMismatchIsCouldNotMeasure(t *testing.T) {
	dir := t.TempDir()
	home := initGitRepo(t, dir)
	baseConfig := buildConfig("", dimDef{id: "a", direction: "lower-is-better", builtin: "file-length", optionLines: []string{"threshold=500", `include=["**/*.go"]`}})
	writeFile(t, dir, "pawl.yaml", baseConfig)
	writeFile(t, dir, "a.go", "package a\n")
	if res := runPawl(t, dir, baseEnv(), "record"); res.exit != 0 {
		t.Fatalf("record exit = %d\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	base := gitCommitAll(t, dir, home, "base definition")
	changed := buildConfig("", dimDef{id: "a", direction: "lower-is-better", builtin: "file-length", optionLines: []string{"threshold=400", `include=["**/*.go"]`}})
	writeFile(t, dir, "pawl.yaml", changed)
	gitCommitAll(t, dir, home, "redefine measurement\n\nPawl-Redefine: a")
	res := runPawl(t, dir, gitEnv(home), "check", "--base", base, "--format", "json")
	if res.exit != 2 {
		t.Fatalf("exit = %d, want 2 for changed measurement definition\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	report := parseReport(t, res.stdout)
	if report.BaseRef == nil || *report.BaseRef != base {
		t.Errorf("base_ref = %v, want %q on failure", report.BaseRef, base)
	}
	if strings.Contains(report.Error, "Pawl-Redefine") {
		t.Errorf("Pawl-Redefine must not waive check --base definition mismatch: %s", report.Error)
	}
	if !strings.Contains(strings.ToLower(report.Error), "trusted") || strings.Contains(report.Error, "run a full `pawl record`") {
		t.Errorf("definition mismatch guidance must direct review and recording on a trusted base branch: %s", report.Error)
	}
}

func TestCheckBaseAcceptsLegacyDefinitionAndHonorsOnlyCurrentOnce(t *testing.T) {
	dir := t.TempDir()
	home := initGitRepo(t, dir)
	config := twoDimConfig(`echo x >> runs; echo '{"value": 5}'`, `echo '{"value": 1}'`)
	writeFile(t, dir, "pawl.yaml", config)
	writeFile(t, dir, "runs", "")
	if res := runPawl(t, dir, baseEnv(), "record"); res.exit != 0 {
		t.Fatalf("record exit = %d\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	writeFile(t, dir, "pawl.yaml", twoDimConfig(`echo x >> runs; echo '{"value": 5}'`, `sh -c 'echo b-ran >> runs; exit 1'`))
	// Remove only the fingerprint so the ref models a pre-fingerprint snapshot.
	// A legacy snapshot is constructed directly while preserving the measured value.
	writeFile(t, dir, "pawl.snapshot.json", `{"metrics":{"a":{"direction":"lower-is-better","value":5,"unit":"count","breakdown":null},"b":{"direction":"lower-is-better","value":1,"unit":"count","breakdown":null}}}`+"\n")
	base := gitCommitAll(t, dir, home, "legacy snapshot")

	measured := runPawl(t, dir, baseEnv(), "measure", "--only", "a")
	if measured.exit != 0 {
		t.Fatalf("measure exit = %d\nstdout=%s\nstderr=%s", measured.exit, measured.stdout, measured.stderr)
	}
	writeFile(t, dir, "current.json", measured.stdout)
	before := readFile(t, dir+"/runs")
	res := runPawl(t, dir, gitEnv(home), "check", "--base", base, "--only", "a", "--current", "current.json", "--format", "json")
	if res.exit != 0 {
		t.Fatalf("check --base --only --current exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	if after := readFile(t, dir+"/runs"); after != before {
		t.Errorf("check re-ran an adapter with --current: before=%q after=%q", before, after)
	}
	r := parseReport(t, res.stdout)
	if len(r.Metrics) != 1 || r.Metrics[0].ID != "a" {
		t.Errorf("partial report metrics = %+v, want only a", r.Metrics)
	}
}

func TestCheckBaseRejectsCombinationWithDifferentSinceRef(t *testing.T) {
	dir := t.TempDir()
	home := initGitRepo(t, dir)
	writeFile(t, dir, "pawl.yaml", patternCountSinceConfig())
	writeFile(t, dir, "a.go", "package a\n// NOLINT first\n")
	if res := runPawl(t, dir, baseEnv(), "record"); res.exit != 0 {
		t.Fatalf("record exit = %d\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	trustedBase := gitCommitAll(t, dir, home, "trusted numeric base")
	writeFile(t, dir, "a.go", "package a\n// NOLINT first\n// NOLINT existing\n")
	sinceRef := gitCommitAll(t, dir, home, "since ref includes pre-existing regression")
	res := runPawl(t, dir, gitEnv(home), "check", "--base", trustedBase, "--since", sinceRef)
	if res.exit != 2 || !strings.Contains(res.stderr, "cannot be combined") {
		t.Fatalf("different --base/--since refs exit = %d, want 2 with a clear usage diagnostic\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
}
