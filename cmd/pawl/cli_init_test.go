package main

// Integration tests for `pawl init`, per SPEC.md § init: it scaffolds a
// starter pawl.yaml at the config path (honoring -c), reads no existing
// config, never overwrites an existing file (exit 2, naming the path), and
// the written config is valid and immediately usable by `pawl record` with
// no external tool (only the zero-dependency primitive builtins).

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// init on a directory with no pawl.yaml writes one and exits 0, and its
// stdout points the user at the next command in the two-command on-ramp.
func TestInitScaffoldsConfigAndPointsToRecord(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "pawl.yaml")
	if _, err := os.Stat(cfgPath); !os.IsNotExist(err) {
		t.Fatalf("precondition: pawl.yaml must not exist yet, stat err = %v", err)
	}

	res := runPawl(t, dir, baseEnv(), "init")
	if res.exit != 0 {
		t.Fatalf("init exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	if _, err := os.Stat(cfgPath); err != nil {
		t.Fatalf("expected pawl.yaml to exist after init: %v", err)
	}
	if !strings.Contains(res.stdout, "pawl record") {
		t.Errorf("stdout does not mention next step `pawl record`: %q", res.stdout)
	}
}

// init is the zero-friction on-ramp: it requires no pre-existing pawl.yaml
// (there is nothing else in the directory for it to read).
func TestInitRequiresNoExistingConfig(t *testing.T) {
	dir := t.TempDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("os.ReadDir: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("precondition: dir must start empty, got %v", entries)
	}

	res := runPawl(t, dir, baseEnv(), "init")
	if res.exit != 0 {
		t.Fatalf("init exit = %d, want 0 in an empty directory\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
}

// The scaffolded config is valid and declares at least one zero-dependency
// primitive builtin dimension, so `pawl init && pawl record` succeeds with
// no external tool installed and produces a snapshot carrying file-length.
func TestInitScaffoldedConfigRecordsSuccessfully(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "main.go", "package main\n\nfunc main() {}\n")
	writeFile(t, dir, "util.go", "package main\n\nfunc helper() int {\n\treturn 1\n}\n")

	initRes := runPawl(t, dir, baseEnv(), "init")
	if initRes.exit != 0 {
		t.Fatalf("init exit = %d, want 0\nstdout=%s\nstderr=%s", initRes.exit, initRes.stdout, initRes.stderr)
	}

	recordRes := runPawl(t, dir, baseEnv(), "record")
	if recordRes.exit != 0 {
		t.Fatalf("record after init exit = %d, want 0 (scaffolded config must be valid and self-sufficient)\nstdout=%s\nstderr=%s",
			recordRes.exit, recordRes.stdout, recordRes.stderr)
	}

	snapPath := filepath.Join(dir, "pawl.snapshot.json")
	if _, err := os.Stat(snapPath); err != nil {
		t.Fatalf("expected snapshot at default path after record: %v", err)
	}
	snap := readSnapshot(t, snapPath)
	if len(snap.Metrics) == 0 {
		t.Fatalf("snapshot has no metrics: %+v", snap)
	}
	fl, ok := snap.Metrics["file-length"]
	if !ok {
		t.Fatalf("snapshot missing file-length metric (the scaffold's zero-dependency builtin): %+v", snap)
	}
	if fl.Value < 0 {
		t.Errorf("file-length value = %v, want a non-negative measurement", fl.Value)
	}
}

// init never overwrites a pre-existing config: refusal is exit 2, names the
// path, and leaves the file byte-for-byte untouched — a scaffolder that
// clobbers a hand-tuned config is worse than useless.
func TestInitRefusesToOverwriteExistingValidConfig(t *testing.T) {
	dir := t.TempDir()
	sentinel := `dimensions:
  - id: "sentinel"
    title: "Sentinel dimension"
    direction: "lower-is-better"
    builtin: "file-length"
    options:
      include: ["**/*.go"]
`
	cfgPath := writeFile(t, dir, "pawl.yaml", sentinel)

	res := runPawl(t, dir, baseEnv(), "init")
	if res.exit != 2 {
		t.Fatalf("init exit = %d, want 2 when pawl.yaml already exists\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	if !strings.Contains(res.stderr, "pawl.yaml") {
		t.Errorf("stderr does not name the existing path pawl.yaml: %q", res.stderr)
	}
	if got := readFile(t, cfgPath); got != sentinel {
		t.Errorf("existing config was modified:\n--- got ---\n%s\n--- want (unchanged) ---\n%s", got, sentinel)
	}
}

// The overwrite refusal is content-agnostic: even a file that is not a valid
// pawl config at all must survive untouched, because init reads no existing
// config before deciding to refuse.
func TestInitRefusesToOverwriteArbitraryExistingFile(t *testing.T) {
	dir := t.TempDir()
	sentinel := "this is not a pawl config, just arbitrary bytes {{{\n"
	cfgPath := writeFile(t, dir, "pawl.yaml", sentinel)

	res := runPawl(t, dir, baseEnv(), "init")
	if res.exit != 2 {
		t.Fatalf("init exit = %d, want 2 when pawl.yaml already exists (even if not valid config)\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	if !strings.Contains(res.stderr, "pawl.yaml") {
		t.Errorf("stderr does not name the existing path pawl.yaml: %q", res.stderr)
	}
	if got := readFile(t, cfgPath); got != sentinel {
		t.Errorf("existing file was modified:\n--- got ---\n%s\n--- want (unchanged) ---\n%s", got, sentinel)
	}
}

// -c <path> redirects both the write target and the overwrite check to a
// custom config path, leaving the default pawl.yaml untouched.
func TestInitHonorsCustomConfigPath(t *testing.T) {
	dir := t.TempDir()
	customPath := filepath.Join(dir, "custom.yaml")
	defaultPath := filepath.Join(dir, "pawl.yaml")

	res := runPawl(t, dir, baseEnv(), "init", "-c", "custom.yaml")
	if res.exit != 0 {
		t.Fatalf("init -c custom.yaml exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	if _, err := os.Stat(customPath); err != nil {
		t.Fatalf("expected custom.yaml to exist after init -c custom.yaml: %v", err)
	}
	if _, err := os.Stat(defaultPath); err == nil {
		t.Errorf("pawl.yaml should not have been created when -c custom.yaml was given")
	}
	if !strings.Contains(res.stdout, "pawl record") {
		t.Errorf("stdout does not mention next step `pawl record`: %q", res.stdout)
	}
}

// The overwrite refusal applies to the -c path too, not just the default.
func TestInitHonorsCustomConfigPathRefusesOverwrite(t *testing.T) {
	dir := t.TempDir()
	sentinel := `dimensions:
  - id: "sentinel"
    title: "Sentinel dimension"
    direction: "lower-is-better"
    builtin: "file-length"
    options:
      include: ["**/*.go"]
`
	cfgPath := writeFile(t, dir, "custom.yaml", sentinel)

	res := runPawl(t, dir, baseEnv(), "init", "-c", "custom.yaml")
	if res.exit != 2 {
		t.Fatalf("init -c custom.yaml exit = %d, want 2 when custom.yaml already exists\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	if !strings.Contains(res.stderr, "custom.yaml") {
		t.Errorf("stderr does not name the existing path custom.yaml: %q", res.stderr)
	}
	if got := readFile(t, cfgPath); got != sentinel {
		t.Errorf("existing custom.yaml was modified:\n--- got ---\n%s\n--- want (unchanged) ---\n%s", got, sentinel)
	}
}
