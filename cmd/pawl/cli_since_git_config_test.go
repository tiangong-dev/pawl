package main

import "testing"

// The developer's own git configuration must not be able to change the verdict.
// These are the two settings that rewrite what `git diff` prints for a path:
// `diff.mnemonicPrefix` replaces the `a/`/`b/` header prefixes with `c/`/`w/`,
// and `core.quotePath` (on by default) escapes every non-ASCII byte. Either one
// used to leave the added-line set empty for the affected files, so a brand-new
// offender was silently exempted as pre-existing debt — the exact "reads as
// nothing changed" failure --since must never have.

func TestSinceIgnoresMnemonicPrefixConfig(t *testing.T) {
	dir := t.TempDir()
	homeDir := initGitRepo(t, dir)
	runGit(t, dir, homeDir, "config", "diff.mnemonicPrefix", "true")
	writeFile(t, dir, "pawl.yaml", patternCountSinceConfig())
	writeFile(t, dir, "a.go", "package a\n// NOLINT keep\n")
	runPawl(t, dir, gitEnv(homeDir), "record")
	base := gitCommitAll(t, dir, homeDir, "base")

	writeFile(t, dir, "a.go", "package a\n// NOLINT keep\n// NOLINT added\n")

	res := runPawl(t, dir, gitEnv(homeDir), "check", "--since", base)
	if res.exit != 1 {
		t.Fatalf("exit = %d, want 1 (diff.mnemonicPrefix must not hide an added line)\nstdout=%s\nstderr=%s",
			res.exit, res.stdout, res.stderr)
	}
}

// Non-ASCII paths are git-quoted by default (core.quotePath), so this fails
// with no git config of its own — any repository with a non-ASCII filename hit
// it.
func TestSinceScopesNonASCIIPath(t *testing.T) {
	dir := t.TempDir()
	homeDir := initGitRepo(t, dir)
	writeFile(t, dir, "pawl.yaml", patternCountSinceConfig())
	writeFile(t, dir, "文件.go", "package a\n// NOLINT keep\n")
	runPawl(t, dir, gitEnv(homeDir), "record")
	base := gitCommitAll(t, dir, homeDir, "base")

	writeFile(t, dir, "文件.go", "package a\n// NOLINT keep\n// NOLINT added\n")

	res := runPawl(t, dir, gitEnv(homeDir), "check", "--since", base)
	if res.exit != 1 {
		t.Fatalf("exit = %d, want 1 (a quoted non-ASCII path must still scope)\nstdout=%s\nstderr=%s",
			res.exit, res.stdout, res.stderr)
	}
}

// The same quoting breaks the touched-file set watch is built from: `git diff
// --name-only` quotes the path, so the file this invocation actually edited
// never matched the config-relative path and silently dropped out of watch.
func TestWatchListsNonASCIITouchedFile(t *testing.T) {
	dir := t.TempDir()
	homeDir := initGitRepo(t, dir)
	writeFile(t, dir, "pawl.yaml", fileLengthWatchConfig(11))
	writeFile(t, dir, "文件.txt", nLines(2))
	runPawl(t, dir, gitEnv(homeDir), "record")
	gitCommitAll(t, dir, homeDir, "base")

	writeFile(t, dir, "文件.txt", nLines(10))

	res := runPawl(t, dir, gitEnv(homeDir), "check", "--format", "json")
	r := parseReport(t, res.stdout)
	if len(r.Watch) != 1 || r.Watch[0].Path != "文件.txt" {
		t.Fatalf("watch = %+v, want one entry for 文件.txt\nstderr=%s", r.Watch, res.stderr)
	}
}
