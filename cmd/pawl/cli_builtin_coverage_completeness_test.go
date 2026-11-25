package main

import (
	"fmt"
	"strings"
	"testing"
)

func TestBuiltinCoverageCompletenessFloors(t *testing.T) {
	t.Run("min_found rejects fractional counters in each lcov record", func(t *testing.T) {
		dir := t.TempDir()
		report := "SF:src/a.go\nLF:0.5\nLH:0\nend_of_record\nSF:src/b.go\nLF:0.5\nLH:0\nend_of_record\n"
		writeFile(t, dir, "coverage.info", report)
		writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
			id: "coverage", direction: "higher-is-better", builtin: "coverage",
			optionLines: append(coverageOptionLines("", "coverage.info", "lcov", "lines"), "min_found = 1"),
		}))
		res := runPawl(t, dir, baseEnv(), "record")
		if res.exit != 2 || !strings.Contains(res.stderr+res.stdout, "integer") {
			t.Fatalf("record exit=%d, want 2 for fractional per-record counters\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
		}
	})
	t.Run("fractional counters remain compatible without min_found", func(t *testing.T) {
		dir := t.TempDir()
		report := "SF:src/a.go\nLF:0.5\nLH:0\nend_of_record\nSF:src/b.go\nLF:0.5\nLH:0\nend_of_record\n"
		writeFile(t, dir, "coverage.info", report)
		writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
			id: "coverage", direction: "higher-is-better", builtin: "coverage",
			optionLines: coverageOptionLines("", "coverage.info", "lcov", "lines"),
		}))
		res := runPawl(t, dir, baseEnv(), "record")
		if res.exit != 0 {
			t.Fatalf("record exit=%d, want legacy compatibility\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
		}
	})

	t.Run("lcov rejects duplicate source evidence for min_found", func(t *testing.T) {
		dir := t.TempDir()
		absA := dirJoin(dir, "src/a.go")
		writeFile(t, dir, "coverage.info", "SF:src/a.go\nLF:4\nLH:3\nend_of_record\nSF:"+absA+"\nLF:4\nLH:3\nend_of_record\nSF:src/b.go\nLF:4\nLH:4\nend_of_record\n")
		writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
			id: "coverage", direction: "higher-is-better", builtin: "coverage",
			optionLines: append(coverageOptionLines("", "coverage.info", "lcov", "lines"), "min_files = 2", "min_found = 12"),
		}))
		res := runPawl(t, dir, baseEnv(), "record")
		if res.exit != 2 || !strings.Contains(res.stderr+res.stdout, "duplicate source") {
			t.Fatalf("record exit=%d, want 2 because duplicate source records cannot prove distinct found units\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
		}
	})
	t.Run("lcov duplicate records retain legacy percentage without min_found", func(t *testing.T) {
		dir := t.TempDir()
		absA := dirJoin(dir, "src/a.go")
		writeFile(t, dir, "coverage.info", "SF:src/a.go\nLF:4\nLH:3\nend_of_record\nSF:"+absA+"\nLF:4\nLH:3\nend_of_record\nSF:src/b.go\nLF:4\nLH:4\nend_of_record\n")
		writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
			id: "coverage", direction: "higher-is-better", builtin: "coverage",
			optionLines: coverageOptionLines("", "coverage.info", "lcov", "lines"),
		}))
		res := runPawl(t, dir, baseEnv(), "record")
		if res.exit != 0 {
			t.Fatalf("record exit = %d\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
		}
		if got := readSnapshot(t, dirJoin(dir, "pawl.snapshot.json")).Metrics["coverage"].Value; got != 83.33333333333334 {
			t.Errorf("value = %v, want legacy percentage based on duplicate records", got)
		}
	})
	for _, tc := range []struct {
		metric, minFound string
		want             float64
	}{
		{"functions", "5", 80},
		{"branches", "10", 70},
	} {
		t.Run("lcov selected denominator "+tc.metric, func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, dir, "coverage.info", lcovTwoRecordFixture)
			writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
				id: "coverage", direction: "higher-is-better", builtin: "coverage",
				optionLines: append(coverageOptionLines("", "coverage.info", "lcov", tc.metric), "min_found = "+tc.minFound),
			}))
			res := runPawl(t, dir, baseEnv(), "record")
			if res.exit != 0 {
				t.Fatalf("record exit = %d\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
			}
			if got := readSnapshot(t, dirJoin(dir, "pawl.snapshot.json")).Metrics["coverage"].Value; got != tc.want {
				t.Errorf("value = %v, want %v", got, tc.want)
			}
		})
	}
	t.Run("cobertura counts distinct filenames and root denominator", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, "coverage.xml", `<coverage line-rate="0.8" lines-valid="10"><packages><package><classes><class filename="a.c"/><class filename="a.c"/><class filename="b.c"/></classes></package></packages></coverage>`)
		writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
			id: "coverage", direction: "higher-is-better", builtin: "coverage",
			optionLines: append(coverageOptionLines("", "coverage.xml", "cobertura", "lines"), "min_files = 2", "min_found = 10"),
		}))
		res := runPawl(t, dir, baseEnv(), "record")
		if res.exit != 0 {
			t.Fatalf("record exit = %d\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
		}
		if got := readSnapshot(t, dirJoin(dir, "pawl.snapshot.json")).Metrics["coverage"].Value; got != 80 {
			t.Errorf("value = %v, want 80", got)
		}
	})
	t.Run("cobertura uses selected branches denominator", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, "coverage.xml", `<coverage line-rate="0.8" branch-rate="0.7" lines-valid="12" branches-valid="10"><packages/></coverage>`)
		writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
			id: "coverage", direction: "higher-is-better", builtin: "coverage",
			optionLines: append(coverageOptionLines("", "coverage.xml", "cobertura", "branches"), "min_found = 10"),
		}))
		res := runPawl(t, dir, baseEnv(), "record")
		if res.exit != 0 {
			t.Fatalf("record exit = %d\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
		}
		if got := readSnapshot(t, dirJoin(dir, "pawl.snapshot.json")).Metrics["coverage"].Value; got != 70 {
			t.Errorf("value = %v, want 70", got)
		}
	})
	t.Run("cobertura source roots resolve and deduplicate class filenames", func(t *testing.T) {
		dir := t.TempDir()
		absoluteSource := strings.ReplaceAll(dirJoin(dir, "src"), "\\", "/")
		writeFile(t, dir, "coverage.xml", fmt.Sprintf(`<coverage line-rate="0.8" lines-valid="10"><packages><package><sources><source>src</source></sources><classes><class filename="a.c"/></classes></package><package><sources><source>%s</source></sources><classes><class filename="a.c"/></classes></package></packages></coverage>`, absoluteSource))
		writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
			id: "coverage", direction: "higher-is-better", builtin: "coverage",
			optionLines: append(coverageOptionLines("", "coverage.xml", "cobertura", "lines"), "min_files = 1"),
		}))
		res := runPawl(t, dir, baseEnv(), "record")
		if res.exit != 0 {
			t.Fatalf("record exit = %d\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
		}
	})
	t.Run("cobertura root sources deduplicate relative and absolute class paths", func(t *testing.T) {
		dir := t.TempDir()
		absoluteClass := strings.ReplaceAll(dirJoin(dir, "src/a.c"), "\\", "/")
		writeFile(t, dir, "coverage.xml", fmt.Sprintf(`<coverage line-rate="0.8" lines-valid="10"><sources><source>src</source></sources><packages><package><classes><class filename="a.c"/><class filename="%s"/></classes></package></packages></coverage>`, absoluteClass))
		writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
			id: "coverage", direction: "higher-is-better", builtin: "coverage",
			optionLines: append(coverageOptionLines("", "coverage.xml", "cobertura", "lines"), "min_files = 2"),
		}))
		res := runPawl(t, dir, baseEnv(), "record")
		if res.exit != 2 || !strings.Contains(res.stderr, "distinct valid source file") {
			t.Fatalf("record exit = %d, want 2 due to one normalized file\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
		}
	})
	t.Run("cobertura resolves source roots using existing files", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, "src/same.c", "")
		report := `<coverage line-rate="0.8" lines-valid="10"><sources><source>src</source><source>lib</source></sources><packages><package><classes><class filename="same.c"/></classes></package></packages></coverage>`
		writeFile(t, dir, "coverage.xml", report)
		writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
			id: "coverage", direction: "higher-is-better", builtin: "coverage",
			optionLines: append(coverageOptionLines("", "coverage.xml", "cobertura", "lines"), "min_files = 1"),
		}))
		res := runPawl(t, dir, baseEnv(), "record")
		if res.exit != 0 {
			t.Fatalf("one existing source candidate should resolve successfully: exit=%d\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
		}
	})
	t.Run("cobertura rejects multiple existing source candidates", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, "src/same.c", "")
		writeFile(t, dir, "lib/same.c", "")
		writeFile(t, dir, "coverage.xml", `<coverage line-rate="0.8" lines-valid="10"><sources><source>src</source><source>lib</source></sources><packages><package><classes><class filename="same.c"/></classes></package></packages></coverage>`)
		writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
			id: "coverage", direction: "higher-is-better", builtin: "coverage",
			optionLines: append(coverageOptionLines("", "coverage.xml", "cobertura", "lines"), "min_files = 1"),
		}))
		res := runPawl(t, dir, baseEnv(), "record")
		if res.exit != 2 || !strings.Contains(res.stderr, "multiple existing source files") {
			t.Fatalf("record exit=%d, want ambiguous existing source candidates to fail\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
		}
	})
	t.Run("cobertura rejects unresolved multiple source candidates", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, "coverage.xml", `<coverage line-rate="0.8" lines-valid="10"><sources><source>src</source><source>lib</source></sources><packages><package><classes><class filename="missing.c"/></classes></package></packages></coverage>`)
		writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
			id: "coverage", direction: "higher-is-better", builtin: "coverage",
			optionLines: append(coverageOptionLines("", "coverage.xml", "cobertura", "lines"), "min_files = 1"),
		}))
		res := runPawl(t, dir, baseEnv(), "record")
		if res.exit != 2 || !strings.Contains(res.stderr, "no existing in-root source file") {
			t.Fatalf("record exit=%d, want unresolved candidates to fail closed\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
		}
	})
	t.Run("legacy cobertura rate remains valid with unrelated denominator text", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, "coverage.xml", `<coverage line-rate="0.8" lines-valid="unknown"><packages/></coverage>`)
		writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
			id: "coverage", direction: "higher-is-better", builtin: "coverage",
			optionLines: coverageOptionLines("", "coverage.xml", "cobertura", "lines"),
		}))
		res := runPawl(t, dir, baseEnv(), "record")
		if res.exit != 0 {
			t.Fatalf("record exit = %d\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
		}
	})

	for _, tc := range []struct {
		name, format, report, options string
		wantExit                      int
	}{
		{"lcov file floor", "lcov", "SF:a.c\nLF:3\nLH:2\nend_of_record\n", "min_files = 2", 2},
		{"lcov found floor", "lcov", "SF:a.c\nLF:3\nLH:2\nend_of_record\n", "min_found = 4", 2},
		{"lcov missing source filename", "lcov", "SF:\nLF:3\nLH:2\nend_of_record\n", "min_files = 0", 2},
		{"lcov outside config root", "lcov", "SF:../outside.c\nLF:3\nLH:2\nend_of_record\n", "min_files = 1", 2},
		{"cobertura file floor", "cobertura", `<coverage line-rate="0.8" lines-valid="10"><packages><package><classes><class filename="a.c"/><class filename="a.c"/><class filename="b.c"/></classes></package></packages></coverage>`, "min_files = 3", 2},
		{"cobertura found floor", "cobertura", `<coverage line-rate="0.8" lines-valid="10"><packages><package><classes><class filename="a.c"/></classes></package></packages></coverage>`, "min_found = 11", 2},
		{"cobertura missing filenames", "cobertura", `<coverage line-rate="0.8" lines-valid="10"><packages><package><classes><class/></classes></package></packages></coverage>`, "min_files = 0", 2},
		{"cobertura source outside config root", "cobertura", `<coverage line-rate="0.8" lines-valid="10"><packages><package><sources><source>src</source></sources><classes><class filename="inside.c"/></classes></package><package><sources><source>../outside</source></sources><classes><class filename="outside.c"/></classes></package></packages></coverage>`, "min_files = 2", 2},
		{"cobertura root source outside config root", "cobertura", `<coverage line-rate="0.8" lines-valid="10"><sources><source>../outside</source></sources><packages><package><classes><class filename="outside.c"/></classes></package></packages></coverage>`, "min_files = 1", 2},
		{"cobertura trimmed external source root", "cobertura", "<coverage line-rate=\"0.8\" lines-valid=\"10\"><sources><source>\n ../outside \n</source></sources><packages><package><classes><class filename=\"a.c\"/></classes></package></packages></coverage>", "min_files = 1", 2},
		{"cobertura windows absolute source root", "cobertura", `<coverage line-rate="0.8" lines-valid="10"><sources><source>C:\agent\_work\1\s</source></sources><packages><package><classes><class filename="src/a.c"/></classes></package></packages></coverage>`, "min_files = 1", 2},
		{"cobertura UNC absolute source root", "cobertura", `<coverage line-rate="0.8" lines-valid="10"><sources><source>\\server\share\repo</source></sources><packages><package><classes><class filename="src/a.c"/></classes></package></packages></coverage>`, "min_files = 1", 2},
		{"cobertura missing denominator", "cobertura", `<coverage line-rate="0.8"><packages><package><classes><class filename="a.c"/></classes></package></packages></coverage>`, "min_found = 1", 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			ext := "info"
			if tc.format == "cobertura" {
				ext = "xml"
			}
			writeFile(t, dir, "coverage."+ext, tc.report)
			options := append(coverageOptionLines("", "coverage."+ext, tc.format, "lines"), tc.options)
			writeFile(t, dir, "pawl.yaml", buildConfig("", dimDef{
				id: "coverage", direction: "higher-is-better", builtin: "coverage", optionLines: options,
			}))
			res := runPawl(t, dir, baseEnv(), "record")
			if res.exit != tc.wantExit {
				t.Fatalf("record exit = %d, want %d\nstdout=%s\nstderr=%s", res.exit, tc.wantExit, res.stdout, res.stderr)
			}
		})
	}
}
