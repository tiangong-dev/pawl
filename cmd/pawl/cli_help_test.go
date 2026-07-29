package main

import (
	"strings"
	"testing"
)

func TestHelpWorksWithoutConfig(t *testing.T) {
	dir := t.TempDir()
	for _, args := range [][]string{{"--help"}, {"-h"}, {"help"}} {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			res := runPawl(t, dir, baseEnv(), args...)
			if res.exit != 0 {
				t.Fatalf("pawl %v exit = %d, want 0\nstdout=%s\nstderr=%s", args, res.exit, res.stdout, res.stderr)
			}
			if !strings.Contains(res.stdout, "Usage:") || !strings.Contains(res.stdout, "record") {
				t.Fatalf("pawl %v did not print global help: %s", args, res.stdout)
			}
		})
	}
}

func TestCommandHelpWorksBothWays(t *testing.T) {
	dir := t.TempDir()
	for _, args := range [][]string{{"record", "--help"}, {"help", "record"}} {
		res := runPawl(t, dir, baseEnv(), args...)
		if res.exit != 0 {
			t.Fatalf("pawl %v exit = %d, want 0\nstdout=%s\nstderr=%s", args, res.exit, res.stdout, res.stderr)
		}
		if !strings.Contains(res.stdout, "pawl record") || !strings.Contains(res.stdout, "--only") {
			t.Fatalf("pawl %v output = %q, want record usage", args, res.stdout)
		}
	}
}

func TestUnknownCommandStillOutranksHelp(t *testing.T) {
	res := runPawl(t, t.TempDir(), baseEnv(), "frobnicate", "--help")
	if res.exit != 2 || !strings.Contains(res.stderr, `unknown command "frobnicate"`) {
		t.Fatalf("exit/stdout/stderr = %d/%q/%q, want unknown-command exit 2", res.exit, res.stdout, res.stderr)
	}
}

func TestUnknownHelpTopicFails(t *testing.T) {
	res := runPawl(t, t.TempDir(), baseEnv(), "help", "frobnicate")
	if res.exit != 2 || !strings.Contains(res.stderr, `unknown help topic "frobnicate"`) {
		t.Fatalf("exit/stdout/stderr = %d/%q/%q, want unknown-topic exit 2", res.exit, res.stdout, res.stderr)
	}
}

func TestHelpRejectsMachineFormatsBothWays(t *testing.T) {
	for _, args := range [][]string{
		{"help", "--format", "json"},
		{"check", "--help", "--format", "json"},
	} {
		res := runPawl(t, t.TempDir(), baseEnv(), args...)
		if res.exit != 2 || !strings.Contains(res.stderr, "--format is not valid on `help`") {
			t.Fatalf("pawl %v = exit %d, stdout %q, stderr %q", args, res.exit, res.stdout, res.stderr)
		}
	}
}
