package main

import (
	"os"
	"strings"
	"testing"
)

// A snapshot that cannot be written is exit 2 — the run could not honestly
// finish. Under --format json that still has to arrive as the verdict object:
// an agent that gets exit 2 with an empty stdout cannot tell "the gate failed
// to measure" from "my JSON parse broke", which is the whole reason exit 2 is
// structured in the first place.
func TestRecordJSONEmitsVerdictWhenSnapshotWriteFails(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root — file permissions cannot make a write fail")
	}
	cfg := buildConfig("", dimDef{
		id: "a", title: "A", direction: "lower-is-better",
		command: `echo '{"value": 1}'`,
	})

	for _, tc := range []struct {
		name string
		args []string
	}{
		{"full", []string{"record", "--format", "json"}},
		{"only", []string{"record", "--only", "a", "--format", "json"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, dir, "pawl.yaml", cfg)
			if res := runPawl(t, dir, baseEnv(), "record"); res.exit != 0 {
				t.Fatalf("setup record exit = %d, want 0\nstderr=%s", res.exit, res.stderr)
			}
			// Readable (so the baseline still loads) but not writable.
			if err := os.Chmod(dir+"/pawl.snapshot.json", 0o444); err != nil {
				t.Fatalf("chmod: %v", err)
			}

			res := runPawl(t, dir, baseEnv(), tc.args...)
			if res.exit != 2 {
				t.Fatalf("exit = %d, want 2\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
			}
			if strings.TrimSpace(res.stdout) == "" {
				t.Fatalf("stdout is empty; exit 2 under --format json must still print the verdict\nstderr=%s", res.stderr)
			}
			rep := parseReport(t, res.stdout)
			if rep.ExitCode != 2 || rep.FailureClass != "could-not-measure" {
				t.Errorf("exit_code/failure_class = %d/%q, want 2/could-not-measure", rep.ExitCode, rep.FailureClass)
			}
			if rep.Error == "" {
				t.Errorf("error is empty; it must carry the write diagnostic")
			}
			if rep.Command != "record" {
				t.Errorf("command = %q, want record", rep.Command)
			}
		})
	}
}
