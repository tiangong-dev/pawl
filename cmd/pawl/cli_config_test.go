package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Every config validation failure must abort with exit 2 ("could not
// measure/compare honestly"), never a silent pass or a crash.
func TestConfigValidationErrorsExitTwo(t *testing.T) {
	validCmd := `echo '{"value": 1}'`

	cases := []struct {
		name      string
		config    string
		skipWrite bool
	}{
		{
			name:      "missing config file",
			skipWrite: true,
		},
		{
			name:   "zero dimensions",
			config: `snapshot: "s.json"` + "\n",
		},
		{
			name: "duplicate dimension id",
			config: buildConfig("",
				dimDef{id: "a", direction: "lower-is-better", command: validCmd},
				dimDef{id: "a", direction: "lower-is-better", command: validCmd},
			),
		},
		{
			name: "missing id",
			config: "dimensions:\n" +
				"  - title: \"A\"\n" +
				"    direction: \"lower-is-better\"\n" +
				"    command: \"echo '{\\\"value\\\": 1}'\"\n",
		},
		{
			name: "missing title",
			config: "dimensions:\n" +
				"  - id: \"a\"\n" +
				"    direction: \"lower-is-better\"\n" +
				"    command: \"echo '{\\\"value\\\": 1}'\"\n",
		},
		{
			name:   "missing direction",
			config: buildConfig("", dimDef{id: "a", command: validCmd}),
		},
		{
			name:   "invalid direction",
			config: buildConfig("", dimDef{id: "a", direction: "sideways", command: validCmd}),
		},
		{
			name:   "invalid gate",
			config: buildConfig("", dimDef{id: "a", direction: "lower-is-better", gate: "not-a-real-gate", command: validCmd}),
		},
		{
			name: "both command and builtin",
			config: buildConfig("", dimDef{
				id: "a", direction: "lower-is-better", command: validCmd, builtin: "file-length",
				optionLines: []string{`include = ["**/*.go"]`},
			}),
		},
		{
			name:   "neither command nor builtin",
			config: buildConfig("", dimDef{id: "a", direction: "lower-is-better"}),
		},
		{
			name:   "unknown builtin name",
			config: buildConfig("", dimDef{id: "a", direction: "lower-is-better", builtin: "does-not-exist"}),
		},
		{
			name: "file-length builtin missing required include",
			config: buildConfig("", dimDef{
				id: "a", direction: "lower-is-better", builtin: "file-length",
				optionLines: []string{"threshold = 500"},
			}),
		},
		{
			name: "pattern-count builtin with invalid regexp",
			config: buildConfig("", dimDef{
				id: "a", direction: "lower-is-better", builtin: "pattern-count",
				optionLines: []string{`pattern = "(unclosed"`, `include = ["**/*.go"]`},
			}),
		},
		{
			name: "unparseable yaml",
			// A tab in indentation is never valid YAML.
			config: "dimensions:\n\t- id: a",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if !tc.skipWrite {
				writeFile(t, dir, "pawl.yaml", tc.config)
			}
			res := runPawl(t, dir, baseEnv(), "check")
			if res.exit != 2 {
				t.Fatalf("exit = %d, want 2\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
			}
		})
	}
}

// An unknown command must not be silently ignored or misread as a pass — it
// names the valid commands on stderr and exits 2.
func TestUnknownCommandListsValidCommands(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
		id: "a", direction: "lower-is-better", command: `echo '{"value": 1}'`,
	}))
	res := runPawl(t, dir, baseEnv(), "frobnicate")
	if res.exit != 2 {
		t.Fatalf("exit = %d, want 2\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	for _, want := range []string{"record", "check", "diff", "baseline-guard"} {
		if !strings.Contains(res.stderr, want) {
			t.Errorf("stderr does not mention valid command %q: %s", want, res.stderr)
		}
	}
}

// -c selects a config file at a non-default path.
func TestCustomConfigFlag(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "custom.yaml", buildConfig("", dimDef{
		id: "a", direction: "lower-is-better", command: `echo '{"value": 1}'`,
	}))
	res := runPawl(t, dir, baseEnv(), "record", "-c", "custom.yaml")
	if res.exit != 0 {
		t.Fatalf("record -c custom.yaml exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	// The default snapshot path is resolved relative to the config file
	// pointed at by -c, not the process cwd's pawl.yaml (which doesn't exist).
	if _, err := os.Stat(filepath.Join(dir, "pawl.snapshot.json")); err != nil {
		t.Fatalf("expected snapshot next to custom.yaml, got: %v", err)
	}
}
