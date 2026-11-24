package main

import (
	"strings"
	"testing"
)

func TestValidateReportsMissingDiscoveredConfig(t *testing.T) {
	dir := t.TempDir()
	for _, format := range []string{"text", "json"} {
		t.Run(format, func(t *testing.T) {
			res := runPawl(t, dir, baseEnv(), "validate", "--format", format)
			if res.exit != 2 || res.stderr != "" {
				t.Fatalf("exit=%d stderr=%q stdout=%q; want exit 2 and empty stderr", res.exit, res.stderr, res.stdout)
			}
			if format == "text" {
				if !strings.Contains(res.stdout, "no pawl.yaml found") {
					t.Errorf("missing discovery error: %q", res.stdout)
				}
				return
			}
			rep := parseValidate(t, res.stdout)
			if rep.SchemaVersion != 1 || rep.Command != "validate" || rep.OK || rep.Config != "pawl.yaml" || rep.Dimensions != 0 || rep.Analyzers != 0 || len(rep.Errors) != 1 || rep.UnknownKeys == nil || len(rep.UnknownKeys) != 0 {
				t.Fatalf("unexpected report: %+v", rep)
			}
			if !strings.Contains(rep.Errors[0].Message, "no pawl.yaml found") {
				t.Errorf("missing discovery error: %q", rep.Errors[0].Message)
			}
		})
	}
}
