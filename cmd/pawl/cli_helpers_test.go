package main

// Shared test infrastructure for the CLI integration tests in this package.
// The pawl binary is built once (from the real source, not by calling run()
// directly) and every test execs it as a subprocess against a per-test
// fixture directory — this exercises the real process boundary (argv, env,
// exit code, stdout/stderr separation) that the SPEC's contract is about.

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

var pawlBin string

func TestMain(m *testing.M) {
	tmpDir, err := os.MkdirTemp("", "pawl-bin-*")
	if err != nil {
		fmt.Fprintln(os.Stderr, "pawl test setup: mkdtemp:", err)
		os.Exit(1)
	}
	pawlBin = filepath.Join(tmpDir, "pawl")
	build := exec.Command("go", "build", "-o", pawlBin, ".")
	if out, err := build.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "pawl test setup: go build failed: %v\n%s\n", err, out)
		os.RemoveAll(tmpDir)
		os.Exit(1)
	}
	code := m.Run()
	os.RemoveAll(tmpDir)
	os.Exit(code)
}

type cliResult struct {
	stdout string
	stderr string
	exit   int
}

// runPawl execs the built binary with an explicit environment and working
// directory, mirroring how a CI job would invoke it.
func runPawl(t *testing.T, dir string, env []string, args ...string) cliResult {
	t.Helper()
	cmd := exec.Command(pawlBin, args...)
	cmd.Dir = dir
	cmd.Env = env
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("running pawl %v: %v\nstdout=%s\nstderr=%s", args, err, stdout.String(), stderr.String())
		}
	}
	return cliResult{stdout: stdout.String(), stderr: stderr.String(), exit: cmd.ProcessState.ExitCode()}
}

// baseEnv is the minimal, explicit environment for a pawl invocation: PATH
// (so `sh`, `echo`, `sleep`, `git` resolve) plus whatever the test opts into.
func baseEnv(extra ...string) []string {
	env := []string{"PATH=" + os.Getenv("PATH")}
	return append(env, extra...)
}

func writeFile(t *testing.T, dir, rel, content string) string {
	t.Helper()
	p := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatalf("mkdir for %s: %v", p, err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", p, err)
	}
	return p
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

// dimDef describes one dimension entry for buildConfig.
type dimDef struct {
	id          string
	title       string
	direction   string
	gate        string
	tolerance   *float64
	timeout     string
	command     string
	builtin     string
	optionLines []string // raw "key = value" lines under [dimension.options]
}

// buildConfig renders a pawl.yaml from dimDefs. optionLines are authored as
// `key = value` for brevity; they are re-emitted as YAML `key: value` under
// `options:` (splitting on the first `=`, so a value containing `=` — e.g. an
// eslint NODE_OPTIONS command — survives). Inline arrays like `["a","b"]` are
// valid YAML flow sequences, so they carry over unchanged.
func buildConfig(snapshot string, dims ...dimDef) string {
	var b strings.Builder
	if snapshot != "" {
		fmt.Fprintf(&b, "snapshot: %q\n", snapshot)
	}
	b.WriteString("dimensions:\n")
	for _, d := range dims {
		title := d.title
		if title == "" {
			title = d.id
		}
		fmt.Fprintf(&b, "  - id: %q\n", d.id)
		fmt.Fprintf(&b, "    title: %q\n", title)
		if d.direction != "" {
			fmt.Fprintf(&b, "    direction: %q\n", d.direction)
		}
		if d.gate != "" {
			fmt.Fprintf(&b, "    gate: %q\n", d.gate)
		}
		if d.tolerance != nil {
			fmt.Fprintf(&b, "    tolerance: %v\n", *d.tolerance)
		}
		if d.timeout != "" {
			fmt.Fprintf(&b, "    timeout: %q\n", d.timeout)
		}
		if d.command != "" {
			fmt.Fprintf(&b, "    command: %q\n", d.command)
		}
		if d.builtin != "" {
			fmt.Fprintf(&b, "    builtin: %q\n", d.builtin)
		}
		if len(d.optionLines) > 0 {
			b.WriteString("    options:\n")
			for _, line := range d.optionLines {
				if key, val, found := strings.Cut(line, "="); found {
					fmt.Fprintf(&b, "      %s: %s\n", strings.TrimSpace(key), strings.TrimSpace(val))
				} else {
					fmt.Fprintf(&b, "      %s\n", strings.TrimSpace(line))
				}
			}
		}
		b.WriteString("\n")
	}
	return b.String()
}

func nLines(n int) string {
	return strings.Repeat("line\n", n)
}

func dirJoin(dir, rel string) string {
	return filepath.Join(dir, rel)
}
