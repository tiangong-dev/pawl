package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckBaseOmitsRecordNextAction(t *testing.T) {
	dir := t.TempDir()
	home := initGitRepo(t, dir)
	config := buildConfig("", dimDef{id: "a", direction: "lower-is-better", command: `echo '{"value":5}'`})
	mustRecord(t, dir, config)
	base := gitCommitAll(t, dir, home, "trusted baseline")
	// The current working baseline differs from the trusted ref. A value that
	// improved against the ref may still be refused by record --only.
	writeFile(t, dir, "pawl.snapshot.json", `{"metrics":{"a":{"direction":"lower-is-better","value":1,"unit":"count","breakdown":null}}}`+"\n")
	writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{id: "a", direction: "lower-is-better", command: `echo '{"value":3}'`}))

	res := runPawl(t, dir, gitEnv(home), "check", "--base", base, "--format", "json")
	if res.exit != 0 {
		t.Fatalf("check --base exit=%d, want improvement against trusted ref\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	report := parseReport(t, res.stdout)
	metric, ok := metricByID(report, "a")
	if !ok || report.BaseRef == nil || *report.BaseRef != base || !metric.Improved || metric.NextAction != "" {
		t.Fatalf("check --base metric = %+v, want improvement without next_action", metric)
	}
	textRes := runPawl(t, dir, gitEnv(home), "check", "--base", base)
	if textRes.exit != 0 || strings.Contains(textRes.stdout, "pawl record --only a") {
		t.Errorf("check --base text must omit record hint (exit=%d): %s", textRes.exit, textRes.stdout)
	}
	ciEnv := append(gitEnv(home), "GITHUB_ACTIONS=true")
	ciRes := runPawl(t, dir, ciEnv, "check", "--base", base)
	if ciRes.exit != 0 || strings.Contains(ciRes.stdout, "::notice::pawl improved:") {
		t.Errorf("check --base CI must omit record notice (exit=%d): %s", ciRes.exit, ciRes.stdout)
	}
	refused := runPawl(t, dir, gitEnv(home), "record", "--only", "a")
	if refused.exit != 1 || !strings.Contains(refused.stdout, "record --only refused") {
		t.Fatalf("record --only exit=%d, want working-baseline refusal\nstdout=%s\nstderr=%s", refused.exit, refused.stdout, refused.stderr)
	}
}

func TestCheckBaseEnforcesStrictNewKeysAndDefinitionFingerprint(t *testing.T) {
	t.Run("strict gate rejects a new path", func(t *testing.T) {
		dir := t.TempDir()
		home := initGitRepo(t, dir)
		config := buildConfig("", dimDef{
			id: "length", direction: "lower-is-better", gate: "per-key-value-strict",
			builtin: "file-length", optionLines: []string{"threshold = 10", `include = ["*.txt"]`},
		})
		writeFile(t, dir, "pawl.yaml", config)
		writeFile(t, dir, "old.txt", nLines(11))
		if rec := runPawl(t, dir, gitEnv(home), "record"); rec.exit != 0 {
			t.Fatalf("record exit=%d\nstdout=%s\nstderr=%s", rec.exit, rec.stdout, rec.stderr)
		}
		base := gitCommitAll(t, dir, home, "strict-key base")
		if err := os.Remove(filepath.Join(dir, "old.txt")); err != nil {
			t.Fatal(err)
		}
		writeFile(t, dir, "new.txt", nLines(11))
		res := runPawl(t, dir, gitEnv(home), "check", "--base", base, "--format", "json")
		if res.exit != 1 {
			t.Fatalf("check --base exit=%d, want strict new-key regression\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
		}
		report := parseReport(t, res.stdout)
		metric, ok := metricByID(report, "length")
		if !ok || report.BaseRef == nil || *report.BaseRef != base || len(metric.Regressions) != 1 || !metric.Regressions[0].NewKey {
			t.Fatalf("strict base verdict = %+v, want base_ref and a new-key regression", report)
		}
	})

	t.Run("old gate definition is incomparable", func(t *testing.T) {
		dir := t.TempDir()
		home := initGitRepo(t, dir)
		oldConfig := buildConfig("", dimDef{
			id: "length", direction: "lower-is-better", gate: "per-key-value",
			builtin: "file-length", optionLines: []string{"threshold = 10", `include = ["*.txt"]`},
		})
		writeFile(t, dir, "pawl.yaml", oldConfig)
		writeFile(t, dir, "old.txt", nLines(11))
		if rec := runPawl(t, dir, gitEnv(home), "record"); rec.exit != 0 {
			t.Fatalf("record exit=%d\nstdout=%s\nstderr=%s", rec.exit, rec.stdout, rec.stderr)
		}
		base := gitCommitAll(t, dir, home, "old gate base")
		newConfig := strings.Replace(oldConfig, `gate: "per-key-value"`, `gate: "per-key-value-strict"`, 1)
		writeFile(t, dir, "pawl.yaml", newConfig)
		res := runPawl(t, dir, gitEnv(home), "check", "--base", base, "--format", "json")
		if res.exit != 2 {
			t.Fatalf("check --base exit=%d, want definition mismatch\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
		}
		report := parseReport(t, res.stdout)
		if report.BaseRef == nil || *report.BaseRef != base || report.FailureClass != "could-not-measure" || !strings.Contains(report.Error, "definition") {
			t.Fatalf("definition mismatch verdict = %+v", report)
		}
	})
}

func TestCheckBaseCannotCombineWithSince(t *testing.T) {
	dir := t.TempDir()
	home := initGitRepo(t, dir)
	writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{id: "a", direction: "lower-is-better", command: `echo '{"value": 1}'`}))
	mustRecord(t, dir, readFile(t, filepath.Join(dir, "pawl.yaml")))
	base := gitCommitAll(t, dir, home, "base")
	res := runPawl(t, dir, gitEnv(home), "check", "--base", base, "--since", base, "--format", "json")
	if res.exit != 2 || !strings.Contains(res.stderr, "--base cannot be combined with --since") {
		t.Fatalf("check --base --since exit=%d, want explicit exit 2\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
}
