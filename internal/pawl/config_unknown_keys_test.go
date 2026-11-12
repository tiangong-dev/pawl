package pawl

// Unknown-key detection is advisory: the keys are already dropped by the
// parser, so the value of the feature is entirely in what the warning says —
// the full key path, and a suggestion when the typo is close to a real key.
// See spec/commands/config.md.

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// Every stray key in the document is reported, at the path that locates it —
// not just the first one encountered.
func TestUnknownConfigKeysReportsEveryStrayKey(t *testing.T) {
	data := []byte(`snapshots: "s.json"
dimensions:
  - id: "a"
    title: "A"
    direction: "lower-is-better"
    gat: "total"
    optoins: {}
    command: "true"
  - id: "b"
    title: "B"
    direction: "lower-is-better"
    command: "true"
`)
	got := unknownConfigKeys(data)
	want := []string{
		`unknown key "snapshots" — did you mean "snapshot"?`,
		`unknown key "dimensions[0].gat" — did you mean "gate"?`,
		`unknown key "dimensions[0].optoins" — did you mean "options"?`,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("unknownConfigKeys =\n%q\nwant\n%q", got, want)
	}
}

// A typo inside an analyzers[] entry is checked too, against the analyzer
// schema rather than the dimension schema.
func TestUnknownConfigKeysReportsStrayAnalyzerKey(t *testing.T) {
	data := []byte(`analyzers:
  - id: "a"
    builtin: "lines"
    opitoons:
      command: "x"
      regex: "y"
dimensions:
  - id: "d"
    title: "D"
    direction: "lower-is-better"
    source: "a"
`)
	got := unknownConfigKeys(data)
	want := []string{`unknown key "analyzers[0].opitoons" — did you mean "options"?`}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("unknownConfigKeys = %q, want %q", got, want)
	}
}

// The `x-` prefix is the reserved extension namespace at every level: it is how
// people park YAML anchors and tool-specific metadata, so it never warns.
func TestUnknownConfigKeysAllowsExtensionKeys(t *testing.T) {
	data := []byte(`x-anchors:
  shared: &shared "true"
dimensions:
  - id: "a"
    title: "A"
    direction: "lower-is-better"
    x-note: "kept for tooling"
    command: *shared
`)
	if got := unknownConfigKeys(data); len(got) != 0 {
		t.Errorf("extension keys produced warnings: %q", got)
	}
}

// A document whose top level is a sequence (a bare dimension fragment, as
// RECIPES.md contains) is not a config and cannot have a top-level key typo.
func TestUnknownConfigKeysIgnoresNonMappingDocument(t *testing.T) {
	data := []byte(`- id: "a"
  title: "A"
  direction: "lower-is-better"
  command: "true"
`)
	if got := unknownConfigKeys(data); len(got) != 0 {
		t.Errorf("a sequence document produced warnings: %q", got)
	}
}

// `<<` is the YAML merge key, a language feature rather than a user typo, so it
// must never be reported — in the single-alias form, the sequence form, or at
// the top level, and however the anchor is defined (including under an `x-`
// key).
func TestUnknownConfigKeysIgnoresMergeKeys(t *testing.T) {
	cases := map[string]string{
		"single alias in a dimension": `x-base: &base
  direction: "lower-is-better"
  gate: "per-file-count"
dimensions:
  - <<: *base
    id: "a"
    title: "a"
    command: "echo 1"
    extract: number
`,
		"sequence of aliases": `x-a: &a
  gate: "per-file-count"
x-b: &b
  direction: "lower-is-better"
dimensions:
  - <<: [*a, *b]
    id: "a"
    title: "a"
    command: "true"
`,
		"merge at the top level": `x-base: &base
  snapshot: "s.json"
<<: *base
dimensions:
  - id: "a"
    title: "a"
    direction: "lower-is-better"
    command: "true"
`,
		"merge inside an analyzer": `x-base: &base
  command: "true"
analyzers:
  - <<: *base
    id: "a"
    builtin: "lines"
    options:
      regex: "x"
dimensions:
  - id: "d"
    title: "D"
    direction: "lower-is-better"
    source: "a"
`,
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			if got := unknownConfigKeys([]byte(data)); len(got) != 0 {
				t.Errorf("merge keys produced warnings: %q", got)
			}
		})
	}
}

// The merge key is recognized by its `!!merge` tag, not by the text `<<`: a
// quoted `"<<"` is an ordinary string key and is still reported.
func TestUnknownConfigKeysReportsQuotedMergeSpelling(t *testing.T) {
	data := []byte(`dimensions:
  - id: "a"
    title: "a"
    direction: "lower-is-better"
    command: "true"
    "<<": 1
`)
	got := unknownConfigKeys(data)
	if len(got) != 1 || !strings.Contains(got[0], `dimensions[0].<<`) {
		t.Errorf("quoted %q key = %q, want one warning about dimensions[0].<<", "<<", got)
	}
}

// A merge key in the same mapping as a genuine typo reports only the typo.
func TestUnknownConfigKeysReportsTypoNextToMergeKey(t *testing.T) {
	data := []byte(`x-base: &base
  direction: "lower-is-better"
  gate: "per-file-count"
dimensions:
  - <<: *base
    id: "a"
    title: "a"
    gat: "total"
    command: "true"
`)
	got := unknownConfigKeys(data)
	want := []string{`unknown key "dimensions[0].gat" — did you mean "gate"?`}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("unknownConfigKeys = %q, want %q", got, want)
	}
}

// An alias where a mapping or sequence is expected is skipped, not dereferenced
// or treated as a stray key.
func TestUnknownConfigKeysSkipsAliases(t *testing.T) {
	cases := map[string]string{
		"sequence-valued entry is an alias": `x-dims: &dims
  - id: "a"
    title: "a"
    direction: "lower-is-better"
    command: "true"
dimensions: *dims
`,
		"sequence item is an alias": `x-dim: &dim
  id: "a"
  title: "a"
  direction: "lower-is-better"
  command: "true"
dimensions:
  - *dim
`,
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			if got := unknownConfigKeys([]byte(data)); len(got) != 0 {
				t.Errorf("an alias produced warnings: %q", got)
			}
		})
	}
}

// The warning's legal-key vocabulary is derived from the parse structs, so this
// pins the derived set to the schema the spec documents: rename a tag and the
// accepted keys (and this test) move together.
func TestYAMLKeySetsMatchSchema(t *testing.T) {
	cases := []struct {
		name   string
		sample any
		want   []string
	}{
		{"config", configFile{}, []string{"analyzers", "dimensions", "exclude", "snapshot"}},
		{"analyzer", analyzerConfig{}, []string{"builtin", "id", "options", "timeout", "verify"}},
		{"dimension", dimensionConfig{}, []string{
			"artifact_max_age", "builtin", "command", "direction", "extract", "gate",
			"id", "options", "source", "timeout", "title", "tolerance", "valid_exit_codes",
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := make([]string, 0)
			for key := range yamlKeySet(tc.sample) {
				got = append(got, key)
			}
			sort.Strings(got)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("yamlKeySet = %v, want %v", got, tc.want)
			}
		})
	}
}

// The configs the repo actually ships must not warn: the root pawl.yaml and
// its smoke/CodeQL variants, every pawl.yaml under demo/, and each YAML block
// in RECIPES.md. A new key added to an example without adding it to the schema
// fails here, which is the point.
func TestDocumentedConfigsHaveNoUnknownKeys(t *testing.T) {
	root := filepath.Join("..", "..")

	for _, rel := range []string{"pawl.yaml", "pawl.smoke.yaml", "pawl.codeql.yaml", "demo/fixture/pawl.yaml"} {
		path := filepath.Join(root, rel)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if got := unknownConfigKeys(data); len(got) != 0 {
			t.Errorf("%s warns about unknown keys: %q", rel, got)
		}
	}

	recipes, err := os.ReadFile(filepath.Join(root, "RECIPES.md"))
	if err != nil {
		t.Fatalf("read RECIPES.md: %v", err)
	}
	blocks := yamlFencedBlocks(string(recipes))
	if len(blocks) == 0 {
		t.Fatal("RECIPES.md yielded no YAML blocks to check")
	}
	for i, block := range blocks {
		if got := unknownConfigKeys([]byte(block)); len(got) != 0 {
			t.Errorf("RECIPES.md YAML block #%d warns about unknown keys: %q\nblock:\n%s", i+1, got, block)
		}
	}
}

// yamlFencedBlocks returns the body of every ```yaml fence in a Markdown
// document, in order.
func yamlFencedBlocks(md string) []string {
	var blocks []string
	var current []string
	inFence := false
	for _, line := range strings.Split(md, "\n") {
		trimmed := strings.TrimSpace(line)
		if !inFence {
			if trimmed == "```yaml" {
				inFence = true
				current = nil
			}
			continue
		}
		if trimmed == "```" {
			blocks = append(blocks, strings.Join(current, "\n"))
			inFence = false
			continue
		}
		current = append(current, line)
	}
	return blocks
}
