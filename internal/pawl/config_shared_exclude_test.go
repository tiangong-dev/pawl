package pawl

import (
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// loadConfigFromString writes content to a throwaway pawl.yaml and loads it.
func loadConfigFromString(t *testing.T, content string) *Config {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "pawl.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err := LoadConfig(path, io.Discard)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	return cfg
}

func definitionOf(t *testing.T, cfg *Config, id string) string {
	t.Helper()
	for _, dim := range cfg.Dimensions {
		if dim.ID == id {
			return dim.Definition
		}
	}
	t.Fatalf("config has no dimension %q", id)
	return ""
}

func optionsOf(t *testing.T, cfg *Config, id string) map[string]any {
	t.Helper()
	for _, dim := range cfg.Dimensions {
		if dim.ID == id {
			return dim.Options
		}
	}
	t.Fatalf("config has no dimension %q", id)
	return nil
}

const repeatedExcludeConfig = `dimensions:
  - id: "markers"
    title: "Markers"
    direction: "lower-is-better"
    gate: "per-file-count"
    builtin: "pattern-count"
    options:
      pattern: "needle"
      include: ["**/*.txt"]
      exclude: ["**/node_modules/**", "**/dist/**"]
  - id: "length"
    title: "Length"
    direction: "lower-is-better"
    builtin: "file-length"
    options:
      threshold: 500
      include: ["**/*.txt"]
      exclude: ["**/node_modules/**", "**/dist/**"]
`

// Lifting the globs every dimension repeats into the top-level `exclude` list
// must not redefine the measurement: the effective exclusion set is unchanged,
// so a recorded baseline stays comparable and no re-baseline is forced.
func TestSharedExcludeKeepsDefinitionWhenRepeatedGlobsMoveToTopLevel(t *testing.T) {
	// Same two globs, written in the opposite order: the exclude list is a set,
	// so order must not matter either.
	const sharedExcludeConfig = `exclude: ["**/dist/**", "**/node_modules/**"]
dimensions:
  - id: "markers"
    title: "Markers"
    direction: "lower-is-better"
    gate: "per-file-count"
    builtin: "pattern-count"
    options:
      pattern: "needle"
      include: ["**/*.txt"]
  - id: "length"
    title: "Length"
    direction: "lower-is-better"
    builtin: "file-length"
    options:
      threshold: 500
      include: ["**/*.txt"]
`

	repeated := loadConfigFromString(t, repeatedExcludeConfig)
	shared := loadConfigFromString(t, sharedExcludeConfig)
	for _, id := range []string{"markers", "length"} {
		if got, want := definitionOf(t, shared, id), definitionOf(t, repeated, id); got != want {
			t.Errorf("dimension %q definition changed by moving excludes to the top level:\n got %s\nwant %s", id, got, want)
		}
	}
}

// A different effective exclusion set measures different files, so it is a
// different definition and the recorded baseline is no longer comparable.
func TestSharedExcludeChangesDefinitionWhenEffectiveSetChanges(t *testing.T) {
	const sharedExcludeConfig = `exclude: ["**/node_modules/**"]
dimensions:
  - id: "markers"
    title: "Markers"
    direction: "lower-is-better"
    gate: "per-file-count"
    builtin: "pattern-count"
    options:
      pattern: "needle"
      include: ["**/*.txt"]
      exclude: ["**/dist/**"]
`
	const widenedExcludeConfig = `exclude: ["**/node_modules/**", "**/build/**"]
dimensions:
  - id: "markers"
    title: "Markers"
    direction: "lower-is-better"
    gate: "per-file-count"
    builtin: "pattern-count"
    options:
      pattern: "needle"
      include: ["**/*.txt"]
      exclude: ["**/dist/**"]
`
	before := loadConfigFromString(t, sharedExcludeConfig)
	after := loadConfigFromString(t, widenedExcludeConfig)
	if definitionOf(t, before, "markers") == definitionOf(t, after, "markers") {
		t.Error("adding a top-level exclude glob left the definition unchanged, but it changes which files are measured")
	}
}

// The top-level list appends to the dimension's own exclude; the dimension's
// globs stay in effect alongside it.
func TestSharedExcludeAppendsToDimensionOwnExclude(t *testing.T) {
	const config = `exclude: ["**/vendor/**"]
dimensions:
  - id: "markers"
    title: "Markers"
    direction: "lower-is-better"
    builtin: "pattern-count"
    options:
      pattern: "needle"
      include: ["**/*.txt"]
      exclude: ["**/skip/**"]
`
	options := optionsOf(t, loadConfigFromString(t, config), "markers")
	got := options["exclude"]
	want := []any{"**/skip/**", "**/vendor/**"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("effective exclude = %#v, want %#v (dimension's own globs first, then the shared list)", got, want)
	}
}

// A glob the dimension already lists must not appear twice: appending verbatim
// would change the fingerprint even though the excluded set is identical.
func TestSharedExcludeDeduplicatesRepeatedGlobs(t *testing.T) {
	const config = `exclude: ["**/skip/**", "**/vendor/**"]
dimensions:
  - id: "markers"
    title: "Markers"
    direction: "lower-is-better"
    builtin: "pattern-count"
    options:
      pattern: "needle"
      include: ["**/*.txt"]
      exclude: ["**/skip/**"]
`
	options := optionsOf(t, loadConfigFromString(t, config), "markers")
	got := options["exclude"]
	want := []any{"**/skip/**", "**/vendor/**"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("effective exclude = %#v, want %#v", got, want)
	}
}

// Only filesystem-scanning builtins take the shared list. A dimension whose
// input is a command or a report has nothing to exclude, so its options and its
// definition are untouched.
func TestSharedExcludeLeavesNonScanningDimensionsAlone(t *testing.T) {
	const withoutShared = `dimensions:
  - id: "runs"
    title: "Runs"
    direction: "lower-is-better"
    command: "echo '{\"value\": 1}'"
`
	const withShared = `exclude: ["**/node_modules/**"]
dimensions:
  - id: "runs"
    title: "Runs"
    direction: "lower-is-better"
    command: "echo '{\"value\": 1}'"
`
	plain := loadConfigFromString(t, withoutShared)
	shared := loadConfigFromString(t, withShared)
	if got := optionsOf(t, shared, "runs"); got != nil {
		t.Errorf("command dimension options = %#v, want nil (no include/exclude concept)", got)
	}
	if definitionOf(t, plain, "runs") != definitionOf(t, shared, "runs") {
		t.Error("a top-level exclude must not change a command dimension's definition")
	}
}
