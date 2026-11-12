package main

// A key the config schema does not define is a silent behavior change: the
// dimension quietly falls back to its defaults. Pawl warns about it on stderr
// (naming the full key path and suggesting a near-miss) without changing the
// exit code, because treating it as an error would break every config that
// carries an unused key. See spec/commands/config.md.

import (
	"fmt"
	"strings"
	"testing"
)

// unknownKeyConfig renders a one-dimension config whose command prints the
// given value; dimExtra is spliced into the dimension mapping verbatim, which
// is where a test plants a stray key.
func unknownKeyConfig(value int, dimExtra string) string {
	return fmt.Sprintf(`dimensions:
  - id: "m"
    title: "M"
    direction: "lower-is-better"
%s    command: %q
`, dimExtra, fmt.Sprintf(`echo '{"value": %d}'`, value))
}

// A misspelled `gate` is ignored exactly as if it were absent: the verdict is
// unchanged, and the warning points at the key and the intended one.
func TestConfigUnknownKeyKeepsVerdictAndSuggests(t *testing.T) {
	dir := t.TempDir()
	mustRecord(t, dir, unknownKeyConfig(1, ""))

	writeFile(t, dir, "pawl.yaml", unknownKeyConfig(2, ""))
	clean := runPawl(t, dir, baseEnv(), "check")

	writeFile(t, dir, "pawl.yaml", unknownKeyConfig(2, "    gat: \"total\"\n"))
	typed := runPawl(t, dir, baseEnv(), "check")

	if clean.exit != 1 {
		t.Fatalf("clean check exit = %d, want 1 (a total regression)\nstdout=%s\nstderr=%s", clean.exit, clean.stdout, clean.stderr)
	}
	if typed.exit != clean.exit {
		t.Errorf("check exit with a stray key = %d, without = %d; the typo must not change the verdict", typed.exit, clean.exit)
	}
	if !strings.Contains(typed.stderr, "dimensions[0].gat") {
		t.Errorf("stderr does not name the stray key path: %s", typed.stderr)
	}
	if !strings.Contains(typed.stderr, "gate") {
		t.Errorf("stderr does not suggest `gate`: %s", typed.stderr)
	}
	if strings.Contains(clean.stderr, "unknown key") {
		t.Errorf("a clean config must not warn: %s", clean.stderr)
	}
}

// Every stray key is reported, not just the first: one at the top level and two
// inside the dimension, each at its own path.
func TestConfigUnknownKeyListsAllStrays(t *testing.T) {
	dir := t.TempDir()
	config := `snapshots: "s.json"
dimensions:
  - id: "m"
    title: "M"
    direction: "lower-is-better"
    gat: "total"
    optoins: {}
    command: "echo '{\"value\": 1}'"
`
	writeFile(t, dir, "pawl.yaml", config)
	res := runPawl(t, dir, baseEnv(), "record")
	if res.exit != 0 {
		t.Fatalf("record exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	for _, want := range []string{"snapshots", "dimensions[0].gat", "dimensions[0].optoins"} {
		if !strings.Contains(res.stderr, want) {
			t.Errorf("stderr does not list stray key %q: %s", want, res.stderr)
		}
	}
}

// The `x-` prefix is the escape hatch for anchors and tool metadata, so it must
// not warn at either level it can appear.
func TestConfigExtensionKeysDoNotWarn(t *testing.T) {
	dir := t.TempDir()
	config := `x-anchors:
  shared: &c "echo '{\"value\": 1}'"
dimensions:
  - id: "m"
    title: "M"
    direction: "lower-is-better"
    x-note: "kept for tooling"
    command: *c
`
	writeFile(t, dir, "pawl.yaml", config)
	res := runPawl(t, dir, baseEnv(), "record")
	if res.exit != 0 {
		t.Fatalf("record exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	if strings.Contains(res.stderr, "unknown key") {
		t.Errorf("x- keys must not warn: %s", res.stderr)
	}
}

// -q silences pawl's own progress, but a config warning is not progress: it
// The YAML merge key `<<` is a language feature, not a user key, so a config
// that shares fields between dimensions through anchors must load without a
// single warning — in both the single-alias and sequence-of-aliases forms.
func TestConfigMergeKeysDoNotWarn(t *testing.T) {
	config := `x-base: &base
  direction: "lower-is-better"
  gate: "per-file-count"
x-extra: &extra
  tolerance: 0
dimensions:
  - <<: *base
    id: "a"
    title: "a"
    command: "echo '{\"value\": 1}'"
  - <<: [*extra, *base]
    id: "b"
    title: "b"
    command: "echo '{\"value\": 2}'"
`
	dir := t.TempDir()
	writeFile(t, dir, "pawl.yaml", config)
	res := runPawl(t, dir, baseEnv(), "record")
	if res.exit != 0 {
		t.Fatalf("record exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	if strings.Contains(res.stderr, "unknown key") {
		t.Errorf("merge keys warned: %s", res.stderr)
	}
}

// A real typo in a mapping that also uses `<<` is still reported, and only it.
func TestConfigMergeKeyStillReportsRealTypo(t *testing.T) {
	config := `x-base: &base
  direction: "lower-is-better"
  gate: "per-file-count"
dimensions:
  - <<: *base
    id: "a"
    title: "a"
    gat: "total"
    command: "echo '{\"value\": 1}'"
`
	dir := t.TempDir()
	writeFile(t, dir, "pawl.yaml", config)
	res := runPawl(t, dir, baseEnv(), "record")
	if res.exit != 0 {
		t.Fatalf("record exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	if !strings.Contains(res.stderr, "dimensions[0].gat") {
		t.Errorf("stderr does not name the real typo: %s", res.stderr)
	}
	if strings.Contains(res.stderr, `"<<"`) || strings.Contains(res.stderr, "dimensions[0].<<") {
		t.Errorf("stderr reports the merge key as a typo: %s", res.stderr)
	}
	if got := strings.Count(res.stderr, "unknown key"); got != 1 {
		t.Errorf("unknown-key warnings = %d, want 1: %s", got, res.stderr)
	}
}

// must survive, because a silently-ignored key is the whole failure mode.
func TestConfigUnknownKeyWarningSurvivesQuiet(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "pawl.yaml", unknownKeyConfig(1, "    gat: \"total\"\n"))
	res := runPawl(t, dir, baseEnv(), "record", "-q")
	if res.exit != 0 {
		t.Fatalf("record -q exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	if !strings.Contains(res.stderr, "dimensions[0].gat") {
		t.Errorf("-q suppressed the config warning: %s", res.stderr)
	}
}

// The warning goes to stderr only, so --format json still yields one parseable
// object on stdout.
func TestConfigUnknownKeyWarningKeepsJSONStdoutPure(t *testing.T) {
	dir := t.TempDir()
	mustRecord(t, dir, unknownKeyConfig(1, ""))
	writeFile(t, dir, "pawl.yaml", unknownKeyConfig(2, "    gat: \"total\"\n"))

	res := runPawl(t, dir, baseEnv(), "check", "--format", "json")
	if res.exit != 1 {
		t.Fatalf("check --format json exit = %d, want 1\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	parseReport(t, res.stdout) // must be one clean JSON object
	if strings.Contains(res.stdout, "unknown key") {
		t.Errorf("stdout carries the warning: %s", res.stdout)
	}
	if !strings.Contains(res.stderr, "dimensions[0].gat") {
		t.Errorf("stderr is missing the warning: %s", res.stderr)
	}
}

// Under GITHUB_ACTIONS the warning uses the workflow-command form, still on
// stderr.
func TestConfigUnknownKeyWarningUsesCIAnnotation(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "pawl.yaml", unknownKeyConfig(1, "    gat: \"total\"\n"))
	res := runPawl(t, dir, baseEnv("GITHUB_ACTIONS=true"), "record")
	if res.exit != 0 {
		t.Fatalf("record under CI exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	if !strings.Contains(res.stderr, "::warning::") {
		t.Errorf("CI stderr missing ::warning:: annotation: %s", res.stderr)
	}
	if !strings.Contains(res.stderr, "dimensions[0].gat") {
		t.Errorf("CI warning does not name the key path: %s", res.stderr)
	}
}

// trend loads the config leniently (LoadConfigLite) and never measures, so a
// stray key must not even be reported there.
func TestTrendIgnoresUnknownConfigKeys(t *testing.T) {
	dir, homeDir := newTrendRepo(t)
	commitSnapshotAt(t, dir, homeDir,
		map[string]trendMetricValue{"file-length": {direction: "lower-is-better", unit: "files > 500 lines", value: 5}},
		"v1", "2026-01-01 12:00:00 +0000")

	clean := runPawl(t, dir, gitEnv(homeDir), "trend")

	writeFile(t, dir, "pawl.yaml", `snapshots: "pawl.snapshot.json"
unknown_top: 1
dimensions:
  - id: "file-length"
    title: "Files over 500 lines"
    direction: "lower-is-better"
    gat: "total"
    command: "echo '{\"value\": 1}'"
`)
	typed := runPawl(t, dir, gitEnv(homeDir), "trend")

	if typed.exit != clean.exit {
		t.Errorf("trend exit with stray keys = %d, without = %d", typed.exit, clean.exit)
	}
	if typed.stdout != clean.stdout {
		t.Errorf("trend output changed with stray keys:\nwith=%s\nwithout=%s", typed.stdout, clean.stdout)
	}
	if strings.Contains(typed.stderr, "unknown key") || strings.Contains(typed.stderr, "::warning::") {
		t.Errorf("trend warned about a stray key: %s", typed.stderr)
	}
}
