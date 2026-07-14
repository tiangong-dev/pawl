package main

// Regression guards for bugs found in adversarial review of the extract /
// --format json / --since implementation. Each test pins one fixed hole so it
// cannot silently reopen.

import (
	"path/filepath"
	"strings"
	"testing"
)

// --since must catch a same-key value growth on an EDITED line: per-file-count
// counts keys, so a line gaining a second marker doesn't change the key count —
// only the scalar moves, and --since drops the scalar. The fix re-derives the
// verdict from the breakdown, flagging a key whose value grew on an added line.
func TestSinceValueGrowthOnEditedLineFails(t *testing.T) {
	dir := t.TempDir()
	homeDir := initGitRepo(t, dir)
	writeFile(t, dir, "pawl.yaml", patternCountSinceConfig())
	writeFile(t, dir, "a.go", "package a\n// NOLINT keep\n")
	runPawl(t, dir, gitEnv(homeDir), "record") // a.go:2 = 1 marker
	base := gitCommitAll(t, dir, homeDir, "base")

	// Edit line 2 to carry a second marker: same key a.go:2, value 1 → 2.
	writeFile(t, dir, "a.go", "package a\n// NOLINT NOLINT keep\n")
	gitCommitAll(t, dir, homeDir, "second marker on the edited line")

	res := runPawl(t, dir, gitEnv(homeDir), "check", "--since", base)
	if res.exit != 1 {
		t.Fatalf("exit = %d, want 1 (value growth on an edited line)\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
}

// A new offender with no attributable line (breakdown key "x.go:0") cannot be
// proven pre-existing, so --since must count it live (conservative), not
// suppress it because line 0 isn't in the added-line set.
func TestSinceUnscopeableLineZeroCountedLive(t *testing.T) {
	dir := t.TempDir()
	homeDir := initGitRepo(t, dir)
	clean := buildConfig("", dimDef{
		id: "m", direction: "lower-is-better", gate: "per-file-count",
		command: `echo '{"value":0,"breakdown":{}}'`,
	})
	writeFile(t, dir, "pawl.yaml", clean)
	runPawl(t, dir, gitEnv(homeDir), "record")
	base := gitCommitAll(t, dir, homeDir, "base")

	writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
		id: "m", direction: "lower-is-better", gate: "per-file-count",
		command: `echo '{"value":1,"breakdown":{"x.go:0":1}}'`,
	}))
	gitCommitAll(t, dir, homeDir, "offender with no line number")

	res := runPawl(t, dir, gitEnv(homeDir), "check", "--since", base)
	if res.exit != 1 {
		t.Fatalf("exit = %d, want 1 (line-0 offender is unscopeable → live)\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
}

// A hunk-body added line whose content begins with "++ " renders in `git diff`
// as "+++ ..." — it must be read as an added content line, not mistaken for a
// file header, or its offender is silently dropped from the changed-line set.
func TestSinceAddedLineRenderingAsPlusPlusPlus(t *testing.T) {
	dir := t.TempDir()
	homeDir := initGitRepo(t, dir)
	writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
		id: "marks", direction: "lower-is-better", gate: "per-file-count",
		builtin: "pattern-count", optionLines: []string{`pattern = "NOLINT"`, `include = ["**/*.txt"]`},
	}))
	writeFile(t, dir, "a.txt", "start\n")
	runPawl(t, dir, gitEnv(homeDir), "record")
	base := gitCommitAll(t, dir, homeDir, "base")

	writeFile(t, dir, "a.txt", "start\n++ NOLINT\n") // diff line becomes "+++ NOLINT"
	gitCommitAll(t, dir, homeDir, "offender whose diff line starts with +++")

	res := runPawl(t, dir, gitEnv(homeDir), "check", "--since", base)
	if res.exit != 1 {
		t.Fatalf("exit = %d, want 1 (added +++ content line must be attributed)\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
}

// extract regex must match the logical line even with CRLF stdout: a trailing
// \r must be stripped, or a `$`-anchored pattern fails to match and the run
// aborts (exit 2) on output it should have accepted.
func TestExtractRegexHandlesCRLF(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "pawl.yaml", extractConfig("r", "lower-is-better", "",
		`printf 'ISSUE\r\nISSUE\r\n'`, "extract:", `  regex: '^ISSUE$'`))

	res := runPawl(t, dir, baseEnv(), "record")
	if res.exit != 0 {
		t.Fatalf("record exit = %d, want 0 (CRLF lines must match)\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	if got := readSnapshot(t, filepath.Join(dir, "pawl.snapshot.json")).Metrics["r"].Value; got != 2 {
		t.Errorf("value = %v, want 2", got)
	}
}

// A non-string extract.regex is a config error (exit 2), not a compile of the
// empty string that matches everything and reports a fabricated value.
func TestExtractRegexNonStringIsConfigError(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "pawl.yaml", extractConfig("r", "lower-is-better", "",
		`echo '{"value":1}'`, "extract:", `  regex: 123`))

	res := runPawl(t, dir, baseEnv(), "record")
	if res.exit != 2 {
		t.Fatalf("exit = %d, want 2 (non-string regex is a config error)\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
}

// per-key-value is enforced in full under --since (its scalar is not a sum of
// its breakdown and it ignores new keys, so it cannot be line-scoped without
// diverging). A new key that raises the scalar must therefore still fail — via
// the full-strength scalar check, not by inventing a per-key regression.
func TestSincePerKeyValueScalarRegressionEnforcedInFull(t *testing.T) {
	dir := t.TempDir()
	homeDir := initGitRepo(t, dir)
	clean := buildConfig("", dimDef{
		id: "m", direction: "lower-is-better", gate: "per-key-value",
		command: `echo '{"value":0,"breakdown":{}}'`,
	})
	writeFile(t, dir, "pawl.yaml", clean)
	writeFile(t, dir, "a.go", "package a\n")
	runPawl(t, dir, gitEnv(homeDir), "record")
	base := gitCommitAll(t, dir, homeDir, "base")

	writeFile(t, dir, "a.go", "package a\nvar x = 1\n")
	writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
		id: "m", direction: "lower-is-better", gate: "per-key-value",
		command: `echo '{"value":1,"breakdown":{"a.go:2":1}}'`,
	}))
	gitCommitAll(t, dir, homeDir, "scalar rises under per-key-value")

	res := runPawl(t, dir, gitEnv(homeDir), "check", "--since", base)
	if res.exit != 1 {
		t.Fatalf("exit = %d, want 1 (per-key-value scalar enforced in full)\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
}

// --since must not INVENT a regression full mode wouldn't raise: per-key-value
// ignores new keys, so a run where the scalar improved and no baseline key
// worsened passes full mode and must pass --since too — even with a new key on
// an added line.
func TestSincePerKeyValueScalarImprovedNewKeyPasses(t *testing.T) {
	dir := t.TempDir()
	homeDir := initGitRepo(t, dir)
	writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
		id: "m", direction: "lower-is-better", gate: "per-key-value",
		command: `echo '{"value":10,"breakdown":{"old.go:1":10}}'`,
	}))
	writeFile(t, dir, "old.go", "package old\n")
	runPawl(t, dir, gitEnv(homeDir), "record")
	base := gitCommitAll(t, dir, homeDir, "base")

	writeFile(t, dir, "new.go", "package new\n") // new.go:1 is an added line
	writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
		id: "m", direction: "lower-is-better", gate: "per-key-value",
		command: `echo '{"value":5,"breakdown":{"old.go:1":0,"new.go:1":5}}'`,
	}))
	gitCommitAll(t, dir, homeDir, "scalar improves; new key on an added line")

	res := runPawl(t, dir, gitEnv(homeDir), "check", "--since", base)
	if res.exit != 0 {
		t.Fatalf("exit = %d, want 0 (full mode passes, so --since must not invent a regression)\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
}

// per-file-count is direction-agnostic (an offender count rise fails regardless
// of direction), so a higher-is-better per-file-count dimension's new offender
// on an added line must be flagged by --since, matching full mode — the scoping
// must not gate new keys behind a lower-is-better check.
func TestSincePerFileCountHigherIsBetterNewOffenderFlagged(t *testing.T) {
	dir := t.TempDir()
	homeDir := initGitRepo(t, dir)
	writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
		id: "m", direction: "higher-is-better", gate: "per-file-count",
		command: `echo '{"value":0,"breakdown":{}}'`,
	}))
	writeFile(t, dir, "a.go", "package a\n")
	runPawl(t, dir, gitEnv(homeDir), "record")
	base := gitCommitAll(t, dir, homeDir, "base")

	writeFile(t, dir, "a.go", "package a\nvar x = 1\n") // a.go:2 added
	writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
		id: "m", direction: "higher-is-better", gate: "per-file-count",
		command: `echo '{"value":1,"breakdown":{"a.go:2":1}}'`,
	}))
	gitCommitAll(t, dir, homeDir, "new offender on added line")

	res := runPawl(t, dir, gitEnv(homeDir), "check", "--since", base)
	if res.exit != 1 {
		t.Fatalf("exit = %d, want 1 (per-file-count offender rise is direction-agnostic)\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
}

// A clean line-addressable metric under --since --format json serializes its
// regressions as [] (like full mode), never null.
func TestSinceJSONCleanMetricRegressionsIsEmptyArray(t *testing.T) {
	dir := t.TempDir()
	homeDir := initGitRepo(t, dir)
	writeFile(t, dir, "pawl.yaml", patternCountSinceConfig())
	writeFile(t, dir, "a.go", "package a\n// NOLINT keep\n")
	runPawl(t, dir, gitEnv(homeDir), "record")
	base := gitCommitAll(t, dir, homeDir, "base")

	// A plain added line — no new offender, so the metric stays clean.
	writeFile(t, dir, "a.go", "package a\n// NOLINT keep\nplain\n")
	gitCommitAll(t, dir, homeDir, "no new offender")

	res := runPawl(t, dir, gitEnv(homeDir), "check", "--since", base, "--format", "json")
	if res.exit != 0 {
		t.Fatalf("exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	if strings.Contains(res.stdout, `"regressions": null`) {
		t.Errorf("clean metric serialized regressions as null, want []:\n%s", res.stdout)
	}
}

// Line-based scoping, mitigated by git's own diff matching: a pre-existing
// offender relocated with its LINE CONTENT unchanged is tracked by git as
// context, not an added line, so --since does NOT flag it. Ordinary code motion
// therefore doesn't trip the gate — only offenders on genuinely changed/added
// lines count. This pins the reassuring half of the line-based trade-off; the
// remaining edge (an offender on a line whose content truly changed is flagged
// even if "morally" moved) is the documented clean-as-you-code behavior.
func TestSincePureRelocationNotFlagged(t *testing.T) {
	dir := t.TempDir()
	homeDir := initGitRepo(t, dir)
	writeFile(t, dir, "pawl.yaml", patternCountSinceConfig())
	writeFile(t, dir, "a.go", "package a\n// NOLINT one\n")
	runPawl(t, dir, gitEnv(homeDir), "record") // offender at a.go:2
	base := gitCommitAll(t, dir, homeDir, "base")

	// Insert two plain lines above the offender: its own line content is
	// unchanged, only its number shifts (2 → 4). git matches it as context.
	writeFile(t, dir, "a.go", "package a\nplain\nplain\n// NOLINT one\n")
	gitCommitAll(t, dir, homeDir, "shift the unchanged offender line down")

	res := runPawl(t, dir, gitEnv(homeDir), "check", "--since", base)
	if res.exit != 0 {
		t.Fatalf("exit = %d, want 0 (a shifted-but-unchanged offender line is not a changed line)\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
}
