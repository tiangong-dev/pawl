package main

// `pawl validate` is the pre-flight a config editor, a pre-commit hook, or CI
// can run on every save: it loads the config the way the measuring commands do
// and reports what is wrong, without running a dimension, reading or writing a
// snapshot, or touching git. Unlike them, a key the schema does not define is a
// problem here instead of an advisory warning — the point of the command is to
// stop a config mistake before it silently changes what the gate measures.
// See spec/commands/validate.md.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// validateJSON is the report `pawl validate --format json` writes to stdout.
type validateJSON struct {
	SchemaVersion int    `json:"schema_version"`
	Command       string `json:"command"`
	OK            bool   `json:"ok"`
	Config        string `json:"config"`
	Dimensions    int    `json:"dimensions"`
	Analyzers     int    `json:"analyzers"`
	Errors        []struct {
		Message string `json:"message"`
	} `json:"errors"`
	UnknownKeys []struct {
		Path       string `json:"path"`
		Suggestion string `json:"suggestion"`
	} `json:"unknown_keys"`
}

// parseValidate asserts stdout is exactly one JSON object (nothing else) and
// decodes it — the same "stdout stays pure machine output" contract check's
// verdict has.
func parseValidate(t *testing.T, out string) validateJSON {
	t.Helper()
	var r validateJSON
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatalf("stdout is not one JSON object: %v\nstdout=%s", err, out)
	}
	return r
}

// validateAnalyzerConfig has two dimensions and one analyzer: a named `lines`
// analyzer a dimension reaches through `source`, plus a dependency-free
// builtin. Nothing here needs an external tool or a file on disk, so a valid
// result proves the counts come from the config alone.
const validateAnalyzerConfig = `analyzers:
  - id: "shared-lint"
    builtin: "lines"
    options:
      command: "echo findings"
      regex: '^(?P<path>[^:]+):(?P<line>\d+):'

dimensions:
  - id: "lint-errors"
    title: "Lint errors"
    direction: "lower-is-better"
    source: "shared-lint"

  - id: "marker-count"
    title: "Marker count"
    direction: "lower-is-better"
    builtin: "pattern-count"
    options:
      pattern: "MARKER"
      include: ["**/*.txt"]
`

// A valid config: the text line names the config and what it declares, the JSON
// document carries the same facts in fields, and nothing goes to stderr.
func TestValidateReportsAValidConfig(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "pawl.yaml", validateAnalyzerConfig)

	res := runPawl(t, dir, baseEnv(), "validate")
	if res.exit != 0 {
		t.Fatalf("validate exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	if res.stderr != "" {
		t.Errorf("validate wrote to stderr: %q", res.stderr)
	}
	if want := "ok: pawl.yaml (2 dimension(s), 1 analyzer(s))\n"; res.stdout != want {
		t.Errorf("stdout = %q, want %q", res.stdout, want)
	}

	jsonRes := runPawl(t, dir, baseEnv(), "validate", "--format", "json")
	if jsonRes.exit != 0 {
		t.Fatalf("validate --format json exit = %d, want 0\nstdout=%s\nstderr=%s", jsonRes.exit, jsonRes.stdout, jsonRes.stderr)
	}
	rep := parseValidate(t, jsonRes.stdout)
	if !rep.OK || rep.SchemaVersion != 1 || rep.Command != "validate" {
		t.Errorf("verdict = %+v, want ok/schema_version 1/command validate", rep)
	}
	if rep.Config != "pawl.yaml" || rep.Dimensions != 2 || rep.Analyzers != 1 {
		t.Errorf("config/dimensions/analyzers = %q/%d/%d, want pawl.yaml/2/1", rep.Config, rep.Dimensions, rep.Analyzers)
	}
	if len(rep.Errors) != 0 || len(rep.UnknownKeys) != 0 {
		t.Errorf("a valid config reported problems: errors=%+v unknown_keys=%+v", rep.Errors, rep.UnknownKeys)
	}
}

// A stray key is fatal here — this is the one command that refuses to guess
// what the author meant. Both formats name the key path and the near miss.
func TestValidateRejectsUnknownKey(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "pawl.yaml", unknownKeyConfig(1, "    gat: \"total\"\n"))

	res := runPawl(t, dir, baseEnv(), "validate")
	if res.exit != 2 {
		t.Fatalf("validate exit = %d, want 2\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	if !strings.Contains(res.stdout, `unknown key "dimensions[0].gat"`) {
		t.Errorf("stdout does not name the stray key: %s", res.stdout)
	}
	if !strings.Contains(res.stdout, `did you mean "gate"?`) {
		t.Errorf("stdout does not suggest the intended key: %s", res.stdout)
	}
	if res.stderr != "" {
		t.Errorf("validate wrote to stderr: %q", res.stderr)
	}

	jsonRes := runPawl(t, dir, baseEnv(), "validate", "--format", "json")
	if jsonRes.exit != 2 {
		t.Fatalf("validate --format json exit = %d, want 2\nstdout=%s", jsonRes.exit, jsonRes.stdout)
	}
	rep := parseValidate(t, jsonRes.stdout)
	if rep.OK {
		t.Error("ok = true with an unknown key")
	}
	if len(rep.Errors) != 0 {
		t.Errorf("an unknown key is reported as a config error too: %+v", rep.Errors)
	}
	if len(rep.UnknownKeys) != 1 || rep.UnknownKeys[0].Path != "dimensions[0].gat" || rep.UnknownKeys[0].Suggestion != "gate" {
		t.Errorf("unknown_keys = %+v, want one dimensions[0].gat/gate entry", rep.UnknownKeys)
	}
	// The dimension itself parses fine, so the counts still describe the file.
	if rep.Dimensions != 1 {
		t.Errorf("dimensions = %d, want 1", rep.Dimensions)
	}
}

// The same file that validate rejects is still only a warning for a measuring
// command: making an unknown key fatal everywhere would break every config
// carrying a key the schema does not use.
func TestUnknownKeyStaysAdvisoryForMeasuringCommands(t *testing.T) {
	dir := t.TempDir()
	mustRecord(t, dir, unknownKeyConfig(1, ""))
	writeFile(t, dir, "pawl.yaml", unknownKeyConfig(1, "    gat: \"total\"\n"))

	checked := runPawl(t, dir, baseEnv(), "check")
	if checked.exit != 0 {
		t.Fatalf("check exit = %d, want 0 (an ignored key does not move the verdict)\nstdout=%s\nstderr=%s",
			checked.exit, checked.stdout, checked.stderr)
	}
	if !strings.Contains(checked.stderr, "unknown key") {
		t.Errorf("check stopped warning about the stray key: %s", checked.stderr)
	}

	if validated := runPawl(t, dir, baseEnv(), "validate"); validated.exit != 2 {
		t.Errorf("validate exit = %d, want 2 for the same config", validated.exit)
	}
}

// Every config error LoadConfig raises is reported, in its own words, with the
// same message in text and JSON — validate must not have a second opinion about
// what the measuring commands refuse to run.
func TestValidateRejectsInvalidConfigs(t *testing.T) {
	cases := []struct {
		name    string
		config  string
		wantMsg string
	}{
		{
			name: "unknown builtin",
			config: `dimensions:
  - id: "d"
    title: "D"
    direction: "lower-is-better"
    builtin: "nope"
`,
			wantMsg: `unknown builtin "nope"`,
		},
		{
			name: "uncompilable pattern",
			config: `dimensions:
  - id: "d"
    title: "D"
    direction: "lower-is-better"
    builtin: "pattern-count"
    options:
      pattern: "("
      include: ["**/*.txt"]
`,
			wantMsg: "pattern: error parsing regexp",
		},
		{
			name: "source names no configured analyzer",
			config: `dimensions:
  - id: "d"
    title: "D"
    direction: "lower-is-better"
    source: "missing"
`,
			wantMsg: `source "missing" names no configured analyzer`,
		},
		{
			name: "duplicate dimension id",
			config: `dimensions:
  - id: "d"
    title: "D"
    direction: "lower-is-better"
    command: "echo 1"
  - id: "d"
    title: "D again"
    direction: "lower-is-better"
    command: "echo 2"
`,
			wantMsg: `duplicate dimension id "d"`,
		},
		{
			name:    "no dimensions",
			config:  "dimensions: []\n",
			wantMsg: "declares no dimensions",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, dir, "pawl.yaml", tc.config)

			res := runPawl(t, dir, baseEnv(), "validate")
			if res.exit != 2 {
				t.Fatalf("validate exit = %d, want 2\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
			}
			if !strings.Contains(res.stdout, tc.wantMsg) {
				t.Errorf("stdout does not point at the problem (%q): %s", tc.wantMsg, res.stdout)
			}
			if res.stderr != "" {
				t.Errorf("validate wrote to stderr: %q", res.stderr)
			}

			jsonRes := runPawl(t, dir, baseEnv(), "validate", "--format", "json")
			if jsonRes.exit != 2 {
				t.Fatalf("validate --format json exit = %d, want 2\nstdout=%s", jsonRes.exit, jsonRes.stdout)
			}
			rep := parseValidate(t, jsonRes.stdout)
			if rep.OK || len(rep.Errors) != 1 {
				t.Fatalf("errors = %+v, want exactly one and ok=false", rep.Errors)
			}
			if got := strings.TrimSpace(res.stdout); rep.Errors[0].Message != got {
				t.Errorf("json message %q != text message %q", rep.Errors[0].Message, got)
			}
		})
	}
}

// A config can be wrong in two ways at once, and both are reported: the error
// that stops the load, and the stray key that may well be why.
func TestValidateReportsAConfigErrorAndAStrayKeyTogether(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "pawl.yaml", `dimensions:
  - id: "d"
    title: "D"
    direction: "lower-is-better"
    builtin: "nope"
    gat: "total"
`)

	res := runPawl(t, dir, baseEnv(), "validate")
	if res.exit != 2 {
		t.Fatalf("validate exit = %d, want 2\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	for _, want := range []string{`unknown builtin "nope"`, `unknown key "dimensions[0].gat"`} {
		if !strings.Contains(res.stdout, want) {
			t.Errorf("stdout is missing %q: %s", want, res.stdout)
		}
	}

	jsonRes := runPawl(t, dir, baseEnv(), "validate", "--format", "json")
	rep := parseValidate(t, jsonRes.stdout)
	if len(rep.Errors) != 1 || len(rep.UnknownKeys) != 1 {
		t.Errorf("errors/unknown_keys = %d/%d, want 1/1", len(rep.Errors), len(rep.UnknownKeys))
	}
}

// validate answers a question about the config, so it must not run anything the
// config describes: no command, no snapshot file. The same config is recorded
// afterwards to prove the command really does have that side effect.
func TestValidateRunsNoAdapterCommandAndWritesNothing(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "pawl.yaml", `dimensions:
  - id: "side-effect"
    title: "Side effect"
    direction: "lower-is-better"
    command: "touch marker.txt && echo 1"
    extract: number
`)

	res := runPawl(t, dir, baseEnv(), "validate")
	if res.exit != 0 {
		t.Fatalf("validate exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	for _, side := range []string{"marker.txt", "pawl.snapshot.json"} {
		if _, err := os.Stat(filepath.Join(dir, side)); !os.IsNotExist(err) {
			t.Errorf("validate created %s (stat err = %v)", side, err)
		}
	}

	recorded := runPawl(t, dir, baseEnv(), "record")
	if recorded.exit != 0 {
		t.Fatalf("record exit = %d, want 0\nstdout=%s\nstderr=%s", recorded.exit, recorded.stdout, recorded.stderr)
	}
	if _, err := os.Stat(filepath.Join(dir, "marker.txt")); err != nil {
		t.Fatalf("record did not run the dimension's command: %v", err)
	}
}

// A config that cannot be read is still a result: exit 2 with the reason, and
// under --format json a parseable object rather than a bare error line.
func TestValidateMissingConfigIsAVerdictNotACrash(t *testing.T) {
	dir := t.TempDir()

	res := runPawl(t, dir, baseEnv(), "validate", "-c", "absent.yaml")
	if res.exit != 2 {
		t.Fatalf("validate exit = %d, want 2\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	if !strings.Contains(res.stdout, "absent.yaml") {
		t.Errorf("stdout does not name the missing config: %s", res.stdout)
	}
	if res.stderr != "" {
		t.Errorf("validate wrote to stderr: %q", res.stderr)
	}

	jsonRes := runPawl(t, dir, baseEnv(), "validate", "-c", "absent.yaml", "--format", "json")
	if jsonRes.exit != 2 {
		t.Fatalf("validate --format json exit = %d, want 2\nstdout=%s", jsonRes.exit, jsonRes.stdout)
	}
	rep := parseValidate(t, jsonRes.stdout)
	if rep.OK || rep.Config != "absent.yaml" || len(rep.Errors) != 1 {
		t.Fatalf("verdict = %+v, want ok=false, config absent.yaml, one error", rep)
	}
	if !strings.Contains(rep.Errors[0].Message, "absent.yaml") {
		t.Errorf("error does not name the missing config: %q", rep.Errors[0].Message)
	}
}

// -q is accepted and ignored: validate emits no progress, and its report is its
// result, so there is nothing for quiet to hold back or suppress.
func TestValidateIgnoresQuiet(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "pawl.yaml", validateAnalyzerConfig)

	plain := runPawl(t, dir, baseEnv(), "validate")
	quiet := runPawl(t, dir, baseEnv(), "validate", "-q")
	if quiet.exit != plain.exit || quiet.stdout != plain.stdout || quiet.stderr != plain.stderr {
		t.Errorf("-q changed the valid run: plain=%+v quiet=%+v", plain, quiet)
	}

	writeFile(t, dir, "pawl.yaml", unknownKeyConfig(1, "    gat: \"total\"\n"))
	failing := runPawl(t, dir, baseEnv(), "validate", "-q")
	if failing.exit != 2 || !strings.Contains(failing.stdout, "unknown key") {
		t.Errorf("-q hid a failing report: exit=%d stdout=%s", failing.exit, failing.stdout)
	}
}

// Flags that describe measuring are usage errors here, so a wrapper cannot make
// validate look like it scoped or measured something.
func TestValidateRejectsFlagsItDoesNotUse(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "pawl.yaml", validateAnalyzerConfig)

	for _, args := range [][]string{
		{"--only", "lint-errors"},
		{"--since", "HEAD"},
		{"--current", "-"},
		{"--limit", "1"},
		{"--dry-run"},
		{"--accept-worse"},
		{"--write", "agent"},
		{"extra"},
	} {
		res := runPawl(t, dir, baseEnv(), append([]string{"validate"}, args...)...)
		if res.exit != 2 {
			t.Errorf("validate %v exit = %d, want 2\nstdout=%s", args, res.exit, res.stdout)
		}
		if res.stderr == "" {
			t.Errorf("validate %v: no usage error on stderr", args)
		}
	}
}

// The configs this repo ships must stay loadable, which is what validate is
// for. It reads them only: no measurement, so the report a dimension reads need
// not exist.
func TestShippedConfigsValidate(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolving repo root: %v", err)
	}

	for _, rel := range []string{"pawl.yaml", "pawl.smoke.yaml", "pawl.codeql.yaml", "demo/fixture/pawl.yaml"} {
		res := runPawl(t, root, baseEnv(), "validate", "-c", rel)
		if res.exit != 0 {
			t.Errorf("validate -c %s exit = %d\nstdout=%s\nstderr=%s", rel, res.exit, res.stdout, res.stderr)
			continue
		}
		if !strings.HasPrefix(res.stdout, "ok: "+rel+" (") {
			t.Errorf("validate -c %s stdout = %q, want an ok line naming %s", rel, res.stdout, rel)
		}
	}
}
