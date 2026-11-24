package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

// init reads marker files next to the config and generates include and shared
// exclude globs for the stacks it finds. Each case below is one marker set; the
// generated config must load, record, and gate without a warning.
func TestInitDetectsProjectTypes(t *testing.T) {
	cases := []struct {
		name        string
		files       map[string]string
		detected    []string
		wantInclude string
		wantExclude string
		// wantScanned is how many files the generated file-length dimension must
		// open: the fixture's source files, with the stack's build/dependency
		// tree excluded. It proves the generated include globs match real files.
		wantScanned int
	}{
		{
			name: "package.json",
			files: map[string]string{
				"package.json":         "{\n  \"name\": \"app\"\n}\n",
				"src/index.ts":         "export const answer = 42\n",
				"src/other.tsx":        "export const other = 1\n",
				"node_modules/dep.js":  "module.exports = {}\n",
				"coverage/report.html": "<html></html>\n",
			},
			detected:    []string{"Node/TypeScript (package.json)"},
			wantInclude: `["**/*.{ts,tsx,js,jsx}"]`,
			wantExclude: `["**/node_modules/**", "**/dist/**", "**/build/**", "**/coverage/**"]`,
			wantScanned: 2,
		},
		{
			name: "go.mod",
			files: map[string]string{
				"go.mod":         "module example.com/app\n",
				"main.go":        "package main\n",
				"vendor/dep.go":  "package dep\n",
				"main_test.go":   "package main\n",
				"README.md":      "# app\n",
				"tools/tool.sh":  "#!/bin/sh\n",
				"proto/api.yaml": "openapi: 3\n",
			},
			detected:    []string{"Go (go.mod)"},
			wantInclude: `["**/*.go"]`,
			wantExclude: `["**/vendor/**"]`,
			wantScanned: 2,
		},
		{
			name: "Cargo.toml",
			files: map[string]string{
				"Cargo.toml":    "[package]\nname = \"app\"\n",
				"src/main.rs":   "fn main() {}\n",
				"target/old.rs": "fn old() {}\n",
			},
			detected:    []string{"Rust (Cargo.toml)"},
			wantInclude: `["**/*.rs"]`,
			wantExclude: `["**/target/**"]`,
			wantScanned: 1,
		},
		{
			name: "pyproject.toml",
			files: map[string]string{
				"pyproject.toml": "[project]\nname = \"app\"\n",
				"app.py":         "print('hi')\n",
				".venv/lib.py":   "print('vendored')\n",
			},
			detected:    []string{"Python (pyproject.toml)"},
			wantInclude: `["**/*.py"]`,
			wantExclude: `["**/.venv/**", "**/venv/**", "**/__pycache__/**"]`,
			wantScanned: 1,
		},
		{
			name: "requirements.txt",
			files: map[string]string{
				"requirements.txt": "requests\n",
				"app.py":           "print('hi')\n",
				"__pycache__/m.py": "print('cached')\n",
			},
			detected:    []string{"Python (requirements.txt)"},
			wantInclude: `["**/*.py"]`,
			wantExclude: `["**/.venv/**", "**/venv/**", "**/__pycache__/**"]`,
			wantScanned: 1,
		},
		{
			name: "Package.swift",
			files: map[string]string{
				"Package.swift":          "// swift-tools-version:5.9\n",
				"Sources/app/main.swift": "print(\"hi\")\n",
				".build/old.swift":       "print(\"old\")\n",
			},
			detected:    []string{"Swift (Package.swift)"},
			wantInclude: `["**/*.swift"]`,
			wantExclude: `["**/.build/**"]`,
			wantScanned: 2,
		},
		{
			name: "two stacks merge",
			files: map[string]string{
				"package.json":        "{\n  \"name\": \"app\"\n}\n",
				"go.mod":              "module example.com/app\n",
				"src/index.ts":        "export const answer = 42\n",
				"main.go":             "package main\n",
				"node_modules/dep.js": "module.exports = {}\n",
				"vendor/dep.go":       "package dep\n",
			},
			detected:    []string{"Node/TypeScript (package.json)", "Go (go.mod)"},
			wantInclude: `["**/*.{ts,tsx,js,jsx}", "**/*.go"]`,
			wantExclude: `["**/node_modules/**", "**/dist/**", "**/build/**", "**/coverage/**", "**/vendor/**"]`,
			wantScanned: 2,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			for rel, content := range tc.files {
				writeFile(t, dir, rel, content)
			}

			res := runPawl(t, dir, baseEnv(), "init")
			if res.exit != 0 {
				t.Fatalf("init exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
			}
			for _, want := range tc.detected {
				if !strings.Contains(res.stdout, want) {
					t.Errorf("init output does not name the detected stack %q:\n%s", want, res.stdout)
				}
			}

			config := readFile(t, filepath.Join(dir, "pawl.yaml"))
			if got := strings.Count(config, "include: "+tc.wantInclude); got != 3 {
				t.Errorf("generated config has %d dimensions scanning %s, want 3:\n%s", got, tc.wantInclude, config)
			}
			if !strings.Contains(config, "\nexclude: "+tc.wantExclude+"\n") {
				t.Errorf("generated config has no top-level exclude %s:\n%s", tc.wantExclude, config)
			}
			if strings.Contains(config, "__PAWL_") {
				t.Errorf("generated config still carries a template placeholder:\n%s", config)
			}

			record := runPawl(t, dir, baseEnv(), "record")
			if record.exit != 0 {
				t.Fatalf("record on the generated config exit = %d, want 0\nstdout=%s\nstderr=%s", record.exit, record.stdout, record.stderr)
			}
			if strings.Contains(record.stderr, "unknown key") {
				t.Errorf("the generated config warns about unknown keys:\n%s", record.stderr)
			}
			if want := fmt.Sprintf("file-length scanned %d file(s)", tc.wantScanned); !strings.Contains(record.stderr, want) {
				t.Errorf("generated include/exclude globs did not scan the fixture's source files; stderr does not contain %q:\n%s", want, record.stderr)
			}

			check := runPawl(t, dir, baseEnv(), "check", "-c", "pawl.yaml")
			if check.exit != 0 {
				t.Fatalf("check -c on the generated config exit = %d, want 0\nstdout=%s\nstderr=%s", check.exit, check.stdout, check.stderr)
			}
			if strings.Contains(check.stderr, "unknown key") {
				t.Errorf("check on the generated config warns about unknown keys:\n%s", check.stderr)
			}
		})
	}
}

// A directory with no marker file keeps the generic starter: it measures
// everything except the usual build and dependency trees, and init reports no
// detected type.
func TestInitWithoutMarkersKeepsTheGenericStarter(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "notes.txt", "no project marker here\n")

	res := runPawl(t, dir, baseEnv(), "init")
	if res.exit != 0 {
		t.Fatalf("init exit = %d, want 0\nstdout=%s\nstderr=%s", res.exit, res.stdout, res.stderr)
	}
	if strings.Contains(res.stdout, "detected") {
		t.Errorf("init reported a detected stack in a directory with no marker:\n%s", res.stdout)
	}
	config := readFile(t, filepath.Join(dir, "pawl.yaml"))
	if got := strings.Count(config, `include: ["**/*"]`); got != 3 {
		t.Errorf("generic starter has %d dimensions scanning **/*, want 3:\n%s", got, config)
	}
	if !strings.Contains(config, `exclude: ["pawl.yaml", "pawl.snapshot.json", "**/node_modules/**", "**/dist/**", "**/vendor/**", "**/.git/**"]`) {
		t.Errorf("generic starter does not exclude the common trees at the top level:\n%s", config)
	}
}

// Detection does not weaken the never-overwrite guarantee: a project with
// markers and an existing config is still refused, byte for byte.
func TestInitRefusesOverwriteInADetectedProject(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "go.mod", "module example.com/app\n")
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
	if got := readFile(t, cfgPath); got != sentinel {
		t.Errorf("existing config was modified:\n--- got ---\n%s\n--- want ---\n%s", got, sentinel)
	}
}
