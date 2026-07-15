package main

import (
	"os"
	"path/filepath"
	"testing"
)

// "Never overwrites" must also mean "never writes through a symlink". A symlink
// at the config path (dangling or not) is an existing entry: atomic
// O_CREATE|O_EXCL refuses it, closing the hole where init would follow the link
// and truncate a file outside the intended path.
func TestInitRefusesToWriteThroughSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "outside-target")
	if err := os.Symlink(target, filepath.Join(dir, "pawl.yaml")); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	res := runPawl(t, dir, baseEnv(), "init")
	if res.exit != 2 {
		t.Fatalf("init exit = %d, want 2 (must refuse a symlink at the config path)\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	if _, err := os.Stat(target); err == nil {
		t.Errorf("init followed the symlink and created %s — it must not write through a symlink", target)
	}
}

// The scaffolded config must not count its own file: `pawl init && pawl record`
// in an empty project records todos = 0, not a phantom baseline from the marker
// words in the generated pawl.yaml.
func TestInitStarterDoesNotMeasureItsOwnConfig(t *testing.T) {
	dir := t.TempDir()
	if res := runPawl(t, dir, baseEnv(), "init"); res.exit != 0 {
		t.Fatalf("init exit = %d\nstderr=%s", res.exit, res.stderr)
	}
	if res := runPawl(t, dir, baseEnv(), "record"); res.exit != 0 {
		t.Fatalf("record exit = %d\nstderr=%s", res.exit, res.stderr)
	}
	snap := readSnapshot(t, dirJoin(dir, "pawl.snapshot.json"))
	if v := snap.Metrics["todos"].Value; v != 0 {
		t.Errorf("todos = %v in an empty project, want 0 (the scaffold's own pawl.yaml must be excluded, not counted)", v)
	}
}
