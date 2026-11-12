package main

// Integration tests for config discovery: without an explicit -c, a command
// that reads a config adopts the nearest pawl.yaml from the working directory
// upward, bounded by the enclosing git repository, and announces an adopted
// parent config on stderr. See spec/commands/config.md § Locating the config
// file.

import (
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

// discoveryConfig measures the line count of a.txt, so whatever the run adopts
// shows up in the metric value rather than only in the progress note.
func discoveryConfig() string {
	return buildConfig("", dimDef{
		id: "lines", direction: "lower-is-better",
		command: `printf '{"value": %s}' "$(wc -l < a.txt | tr -d ' ')"`,
	})
}

// resolvedPath is the physical form of a fixture path: pawl resolves its
// working directory through the OS, so on a host whose temporary root is a
// symlink (macOS /var → /private/var) the path it reports is textually
// different from the one the fixture was created with.
func resolvedPath(t *testing.T, path string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatalf("EvalSymlinks(%s): %v", path, err)
	}
	return resolved
}

// usingConfigPath returns the path from the `pawl: using config <path>`
// progress line, failing when the line is absent.
func usingConfigPath(t *testing.T, stderr string) string {
	t.Helper()
	for _, line := range strings.Split(stderr, "\n") {
		if path, ok := strings.CutPrefix(line, "pawl: using config "); ok {
			return strings.TrimSpace(path)
		}
	}
	t.Fatalf("stderr has no `pawl: using config` line:\n%s", stderr)
	return ""
}

// metricSummary renders each metric's current value and status, so two runs can
// be compared without depending on what differs per invocation.
func metricSummary(t *testing.T, report jsonReport) map[string]string {
	t.Helper()
	summary := map[string]string{}
	for _, m := range report.Metrics {
		current := "none"
		if m.Current != nil {
			current = strconv.FormatFloat(*m.Current, 'f', -1, 64)
		}
		summary[m.ID] = current + "/" + m.Status
	}
	return summary
}

// makedirs creates a nested fixture directory.
func makedirs(t *testing.T, path string) string {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
	return path
}

// check and record in a subdirectory find the repository's pawl.yaml and
// behave exactly as they do at the repository root.
func TestConfigReadFromSubdirectoryMatchesRoot(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)
	writeFile(t, dir, "pawl.yaml", discoveryConfig())
	writeFile(t, dir, "a.txt", "one\ntwo\nthree\n")
	sub := makedirs(t, filepath.Join(dir, "src", "deep"))

	if res := runPawl(t, dir, baseEnv(), "record"); res.exit != 0 {
		t.Fatalf("record at the root exit = %d\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}

	atRoot := runPawl(t, dir, baseEnv(), "check", "--format", "json")
	if atRoot.exit != 0 {
		t.Fatalf("check at the root exit = %d\nstdout=%s\nstderr=%s", atRoot.exit, atRoot.stdout, atRoot.stderr)
	}
	if strings.Contains(atRoot.stderr, "using config") {
		t.Errorf("a config in the working directory must be used silently, stderr:\n%s", atRoot.stderr)
	}

	inSub := runPawl(t, sub, baseEnv(), "check", "--format", "json")
	if inSub.exit != atRoot.exit {
		t.Fatalf("check in a subdirectory exit = %d, want the root's %d\nstdout=%s\nstderr=%s",
			inSub.exit, atRoot.exit, inSub.stdout, inSub.stderr)
	}
	if got := usingConfigPath(t, inSub.stderr); readFile(t, got) != discoveryConfig() {
		t.Errorf("adopted config %s, want the repository's pawl.yaml", got)
	}
	root, subReport := metricSummary(t, parseReport(t, atRoot.stdout)), metricSummary(t, parseReport(t, inSub.stdout))
	if !reflect.DeepEqual(root, subReport) {
		t.Errorf("metrics differ between the root and a subdirectory: root=%v sub=%v", root, subReport)
	}

	// record in the subdirectory writes the snapshot next to the config it
	// adopted, not into the subdirectory.
	if res := runPawl(t, sub, baseEnv(), "record"); res.exit != 0 {
		t.Fatalf("record in a subdirectory exit = %d\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	if _, err := os.Stat(filepath.Join(sub, "pawl.snapshot.json")); !os.IsNotExist(err) {
		t.Errorf("record in a subdirectory created a snapshot there (stat err = %v)", err)
	}
}

// An explicit -c is used verbatim: a path that does not exist is a loud not-found
// error, never a silent fallback to the config a search would have found.
func TestExplicitMissingConfigDoesNotSearchUpward(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)
	writeFile(t, dir, "pawl.yaml", discoveryConfig())
	writeFile(t, dir, "a.txt", "one\n")
	sub := makedirs(t, filepath.Join(dir, "src"))
	if res := runPawl(t, dir, baseEnv(), "record"); res.exit != 0 {
		t.Fatalf("record exit = %d\nstderr=%s", res.exit, res.stderr)
	}

	for _, flag := range []string{"-c", "--config"} {
		res := runPawl(t, sub, baseEnv(), "check", flag, "nonexistent.yaml")
		if res.exit != 2 {
			t.Fatalf("check %s nonexistent.yaml exit = %d, want 2\nstdout=%s\nstderr=%s", flag, res.exit, res.stdout, res.stderr)
		}
		if !strings.Contains(res.stderr, "nonexistent.yaml") {
			t.Errorf("stderr does not name the explicit config path:\n%s", res.stderr)
		}
		if strings.Contains(res.stderr, "using config") {
			t.Errorf("an explicit %s must never search and adopt another config:\n%s", flag, res.stderr)
		}
	}
}

// Outside a git repository the search stops at the working directory: a parent's
// config is not adopted, and the error names the directory that was searched.
func TestParentConfigOutsideGitRepositoryIsNotAdopted(t *testing.T) {
	top := t.TempDir()
	writeFile(t, top, "pawl.yaml", discoveryConfig())
	writeFile(t, top, "a.txt", "one\ntwo\n")
	sub := makedirs(t, filepath.Join(top, "inner"))

	if res := runPawl(t, top, baseEnv(), "record"); res.exit != 0 {
		t.Fatalf("record beside pawl.yaml exit = %d\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	if res := runPawl(t, top, baseEnv(), "check"); res.exit != 0 {
		t.Fatalf("check in the directory holding pawl.yaml exit = %d\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}

	res := runPawl(t, sub, baseEnv(), "check")
	if res.exit != 2 {
		t.Fatalf("check in an unversioned subdirectory exit = %d, want 2\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	if !strings.Contains(res.stderr, "no pawl.yaml found in") || !strings.Contains(res.stderr, resolvedPath(t, sub)) {
		t.Errorf("stderr does not name the searched directory %s:\n%s", sub, res.stderr)
	}
	if !strings.Contains(res.stderr, "no .git in any parent") {
		t.Errorf("stderr does not say the search ranged over the working directory only:\n%s", res.stderr)
	}
	if parentConfig := filepath.Join(resolvedPath(t, top), "pawl.yaml"); strings.Contains(res.stderr, parentConfig) {
		t.Errorf("stderr names %s, which must not be searched outside a git repository:\n%s", parentConfig, res.stderr)
	}
}

// A .git file (a git worktree checkout) bounds the search just like a .git
// directory, and a config above the boundary is not adopted.
func TestGitFileBoundsTheSearch(t *testing.T) {
	outer := t.TempDir()
	// Invalid above the boundary: adopting it would be exit 2, so a passing run
	// proves the boundary held.
	writeFile(t, outer, "pawl.yaml", "snapshot: \"s.json\"\n")

	wt := makedirs(t, filepath.Join(outer, "worktree"))
	writeFile(t, wt, ".git", "gitdir: /nonexistent/worktree\n")
	writeFile(t, wt, "pawl.yaml", discoveryConfig())
	writeFile(t, wt, "a.txt", "one\ntwo\n")
	sub := makedirs(t, filepath.Join(wt, "src", "deep"))

	if res := runPawl(t, wt, baseEnv(), "record"); res.exit != 0 {
		t.Fatalf("record at the worktree root exit = %d\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	res := runPawl(t, sub, baseEnv(), "check")
	if res.exit != 0 {
		t.Fatalf("check under a .git-file worktree exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	if got := usingConfigPath(t, res.stderr); readFile(t, got) != discoveryConfig() {
		t.Errorf("adopted config %s, want the worktree root's pawl.yaml", got)
	}
}

// A config above the repository root is out of range: the repository has none,
// so the gate reports the range it searched instead of borrowing the parent's.
func TestConfigAboveRepositoryRootIsNotAdopted(t *testing.T) {
	outer := t.TempDir()
	writeFile(t, outer, "pawl.yaml", discoveryConfig())

	repo := makedirs(t, filepath.Join(outer, "repo"))
	initGitRepo(t, repo)
	sub := makedirs(t, filepath.Join(repo, "src"))

	res := runPawl(t, sub, baseEnv(), "check")
	if res.exit != 2 {
		t.Fatalf("check with no config inside the repository exit = %d, want 2\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	wantRange := "no pawl.yaml found in " + resolvedPath(t, sub) + " or any parent up to " + resolvedPath(t, repo)
	if !strings.Contains(res.stderr, wantRange) {
		t.Errorf("stderr does not state the search range %q:\n%s", wantRange, res.stderr)
	}
}

// A repository whose root holds no config searched only its root: the message
// must say so, not claim there was no .git to bound a wider search.
func TestNoConfigAtRepositoryRoot(t *testing.T) {
	repo := t.TempDir()
	initGitRepo(t, repo)

	res := runPawl(t, repo, baseEnv(), "check")
	if res.exit != 2 {
		t.Fatalf("check at a repository root with no config exit = %d, want 2\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	want := "no pawl.yaml found in " + resolvedPath(t, repo) + " (repository root)"
	if !strings.Contains(res.stderr, want) {
		t.Errorf("stderr does not state %q:\n%s", want, res.stderr)
	}
	if strings.Contains(res.stderr, "no .git in any parent") {
		t.Errorf("stderr claims no .git bounds the search, but the working directory is the repository root:\n%s", res.stderr)
	}
}

// A config in the working directory keeps the plain relative name: messages that
// quote the config path must read as they did before discovery existed.
func TestWorkingDirectoryConfigKeepsRelativeName(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)
	// Valid YAML, zero dimensions: the config is read and rejected by name.
	writeFile(t, dir, "pawl.yaml", "snapshot: \"pawl.snapshot.json\"\n")

	res := runPawl(t, dir, baseEnv(), "check")
	if res.exit != 2 {
		t.Fatalf("check with a config that declares no dimensions exit = %d, want 2\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	if !strings.Contains(res.stderr, "pawl.yaml declares no dimensions") {
		t.Errorf("stderr does not quote the config by its relative name:\n%s", res.stderr)
	}
	if strings.Contains(res.stderr, resolvedPath(t, dir)) {
		t.Errorf("stderr quotes the absolute path %s for a config in the working directory:\n%s", dir, res.stderr)
	}
}

// init never searches: in a subdirectory it scaffolds a config there and leaves
// the upper config byte-for-byte untouched.
func TestInitInSubdirectoryLeavesUpperConfigAlone(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)
	upper := discoveryConfig()
	upperPath := writeFile(t, dir, "pawl.yaml", upper)
	sub := makedirs(t, filepath.Join(dir, "nested"))

	res := runPawl(t, sub, baseEnv(), "init")
	if res.exit != 0 {
		t.Fatalf("init in a subdirectory exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	if !strings.Contains(readFile(t, filepath.Join(sub, "pawl.yaml")), "dimensions:") {
		t.Errorf("init did not scaffold %s", filepath.Join(sub, "pawl.yaml"))
	}
	if got := readFile(t, upperPath); got != upper {
		t.Errorf("init modified the upper config:\n--- got ---\n%s\n--- want ---\n%s", got, upper)
	}
}

// -q silences the adoption note, and --format json keeps stdout a single JSON
// verdict with the note on stderr.
func TestAdoptionNoteIsProgressNotVerdict(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)
	writeFile(t, dir, "pawl.yaml", discoveryConfig())
	writeFile(t, dir, "a.txt", "one\ntwo\n")
	sub := makedirs(t, filepath.Join(dir, "src"))
	if res := runPawl(t, dir, baseEnv(), "record"); res.exit != 0 {
		t.Fatalf("record exit = %d\nstderr=%s", res.exit, res.stderr)
	}

	quiet := runPawl(t, sub, baseEnv(), "check", "-q")
	if quiet.exit != 0 {
		t.Fatalf("check -q exit = %d, want 0\nstdout=%s\nstderr=%s", quiet.exit, quiet.stdout, quiet.stderr)
	}
	if strings.Contains(quiet.stderr, "using config") {
		t.Errorf("-q did not silence the adoption note:\n%s", quiet.stderr)
	}

	jsonRes := runPawl(t, sub, baseEnv(), "check", "--format", "json")
	if jsonRes.exit != 0 {
		t.Fatalf("check --format json exit = %d\nstdout=%s\nstderr=%s", jsonRes.exit, jsonRes.stdout, jsonRes.stderr)
	}
	// parseReport also asserts stdout holds exactly one JSON object.
	if report := parseReport(t, jsonRes.stdout); report.ExitCode != 0 {
		t.Errorf("exit_code = %d, want 0", report.ExitCode)
	}
	if !strings.Contains(jsonRes.stderr, "pawl: using config ") {
		t.Errorf("a JSON run must still report the adopted config on stderr:\n%s", jsonRes.stderr)
	}
}

// check --since runs against the same adopted config from a subdirectory as from
// the repository root: same exit code, same scoped metrics.
func TestSinceInSubdirectoryMatchesRootRun(t *testing.T) {
	dir := t.TempDir()
	homeDir := initGitRepo(t, dir)
	writeFile(t, dir, "pawl.yaml", patternCountSinceConfig())
	writeFile(t, dir, "a.go", "package a\n// NOLINT keep\n")
	runPawl(t, dir, gitEnv(homeDir), "record")
	base := gitCommitAll(t, dir, homeDir, "base")

	// A new offender on an added line: the diff-scoped gate fails either way.
	writeFile(t, dir, "a.go", "package a\n// NOLINT keep\n// NOLINT added\n")
	gitCommitAll(t, dir, homeDir, "add offender on a new line")

	sub := makedirs(t, filepath.Join(dir, "src", "deep"))
	atRoot := runPawl(t, dir, gitEnv(homeDir), "check", "--since", base, "--format", "json")
	inSub := runPawl(t, sub, gitEnv(homeDir), "check", "--since", base, "--format", "json")
	if atRoot.exit != 1 {
		t.Fatalf("check --since at the root exit = %d, want 1\nstdout=%s\nstderr=%s", atRoot.exit, atRoot.stdout, atRoot.stderr)
	}
	if inSub.exit != atRoot.exit {
		t.Fatalf("check --since in a subdirectory exit = %d, want the root's %d\nstdout=%s\nstderr=%s",
			inSub.exit, atRoot.exit, inSub.stdout, inSub.stderr)
	}
	root, subReport := metricSummary(t, parseReport(t, atRoot.stdout)), metricSummary(t, parseReport(t, inSub.stdout))
	if !reflect.DeepEqual(root, subReport) {
		t.Errorf("since-scoped metrics differ between the root and a subdirectory: root=%v sub=%v", root, subReport)
	}
	if !strings.Contains(inSub.stderr, "pawl: using config ") {
		t.Errorf("the subdirectory run did not report the adopted config:\n%s", inSub.stderr)
	}
}
