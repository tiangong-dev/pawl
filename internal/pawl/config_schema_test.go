package pawl

// schema/pawl.schema.json is the second description of the config structure —
// the one editors read — so these tests pin it to the parser instead of trusting
// the two to be maintained in step. A key added to a parse struct without a
// matching schema property (or the reverse), or a builtin whose enum entry the
// code no longer accepts, fails here rather than silently misleading an editor.
// Reading the file through encoding/json also proves it is valid JSON.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

const schemaRelPath = "../../schema/pawl.schema.json"

// schemaNode is the part of a JSON Schema these tests read: enough of an object
// schema to compare its key set with the parser's and to follow an allOf branch
// to the options object it selects.
type schemaNode struct {
	ID                   string                `json:"$id"`
	Ref                  string                `json:"$ref"`
	Enum                 []string              `json:"enum"`
	Const                *string               `json:"const"`
	Required             []string              `json:"required"`
	AdditionalProperties json.RawMessage       `json:"additionalProperties"`
	Properties           map[string]schemaNode `json:"properties"`
	PatternProperties    map[string]schemaNode `json:"patternProperties"`
	Items                *schemaNode           `json:"items"`
	AllOf                []schemaNode          `json:"allOf"`
	If                   *schemaNode           `json:"if"`
	Then                 *schemaNode           `json:"then"`
}

func loadConfigSchema(t *testing.T) schemaNode {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "schema", "pawl.schema.json"))
	if err != nil {
		t.Fatalf("read the schema editors use: %v", err)
	}
	var schema schemaNode
	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatalf("%s is not valid JSON: %v", schemaRelPath, err)
	}
	return schema
}

// property returns the named property schema, failing the test when the schema
// does not declare it — a missing property is a failure here, not an empty
// comparison.
func (n schemaNode) property(t *testing.T, name string) schemaNode {
	t.Helper()
	p, ok := n.Properties[name]
	if !ok {
		t.Fatalf("schema has no property %q", name)
	}
	return p
}

// keySet turns the declared properties into the map shape yamlKeySet produces,
// so the parser's accepted keys and the schema's can be compared as sets.
func (n schemaNode) keySet() map[string]bool {
	keys := make(map[string]bool, len(n.Properties))
	for name := range n.Properties {
		keys[name] = true
	}
	return keys
}

func (n schemaNode) rejectsUnknownKeys() bool {
	var allowed bool
	if err := json.Unmarshal(n.AdditionalProperties, &allowed); err != nil {
		return false
	}
	return !allowed
}

// The schema's key sets are the parser's key sets, in both directions: a struct
// field whose tag the schema lacks (or a schema property the parser would drop)
// fails this test.
func TestSchemaKeySetsMatchParser(t *testing.T) {
	schema := loadConfigSchema(t)
	analyzers := schema.property(t, "analyzers")
	dimensions := schema.property(t, "dimensions")
	if analyzers.Items == nil || dimensions.Items == nil {
		t.Fatal("schema declares analyzers/dimensions without an item schema")
	}

	cases := []struct {
		name   string
		node   *schemaNode
		sample any
	}{
		{"top level", &schema, configFile{}},
		{"analyzers[]", analyzers.Items, analyzerConfig{}},
		{"dimensions[]", dimensions.Items, dimensionConfig{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := sortedKeyNames(tc.node.keySet())
			want := sortedKeyNames(yamlKeySet(tc.sample))
			if !reflect.DeepEqual(got, want) {
				t.Errorf("schema properties = %v, parser accepts %v", got, want)
			}
		})
	}

	// An unknown key is what pawl warns about at exactly these three levels, so
	// the schema closes them to give the editor something to flag; `x-` keys
	// stay open because pawl never reports them.
	t.Run("unknown keys are closed, x- stays open", func(t *testing.T) {
		for _, node := range []*schemaNode{&schema, analyzers.Items, dimensions.Items} {
			if !node.rejectsUnknownKeys() {
				t.Errorf("additionalProperties must be false so an unknown key is flagged")
			}
			if _, ok := node.PatternProperties["^x-"]; !ok {
				t.Errorf("patternProperties must keep the x- extension prefix valid")
			}
		}
	})
}

// The enumerated values are the code's own sets, referenced rather than copied,
// so a direction, gate mode, or builtin the code accepts cannot be missing from
// the schema the editor offers.
func TestSchemaEnumsMatchCode(t *testing.T) {
	schema := loadConfigSchema(t)
	analyzerBuiltinEnum := schema.property(t, "analyzers").Items.property(t, "builtin")
	dimensionItem := schema.property(t, "dimensions").Items

	cases := []struct {
		name string
		got  []string
		want []string
	}{
		{"direction", dimensionItem.property(t, "direction").Enum, []string{string(LowerIsBetter), string(HigherIsBetter)}},
		{"gate", dimensionItem.property(t, "gate").Enum, []string{string(GateTotal), string(GatePerFileCount), string(GatePerKeyValue)}},
		{"dimension builtin", dimensionItem.property(t, "builtin").Enum, dimensionBuiltins},
		{"analyzer builtin", analyzerBuiltinEnum.Enum, analyzerBuiltins},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := append([]string(nil), tc.got...)
			want := append([]string(nil), tc.want...)
			sort.Strings(got)
			sort.Strings(want)
			if !reflect.DeepEqual(got, want) {
				t.Errorf("schema enum = %v, code accepts %v", got, want)
			}
		})
	}
}

// Every builtin the schema offers must come with the options object it narrows
// to, so a new builtin cannot be added to the enum alone and leave the editor
// with no completion for what it takes.
func TestSchemaNarrowsOptionsPerBuiltin(t *testing.T) {
	schema := loadConfigSchema(t)
	cases := []struct {
		name string
		node schemaNode
		want []string
	}{
		{"analyzers[]", *schema.property(t, "analyzers").Items, analyzerBuiltins},
		{"dimensions[]", *schema.property(t, "dimensions").Items, dimensionBuiltins},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := optionsBranches(t, tc.node)
			sort.Strings(got)
			want := append([]string(nil), tc.want...)
			sort.Strings(want)
			if !reflect.DeepEqual(got, want) {
				t.Errorf("allOf branches on %v, builtins are %v", got, want)
			}
		})
	}
}

// The schema's builtin enums are pinned to these two registries, so a registry
// must name the sets the config parser actually accepts — otherwise the test
// above would pin the schema to a list nothing validates against.
func TestBuiltinRegistriesMatchValidation(t *testing.T) {
	for _, builtin := range dimensionBuiltins {
		t.Run("dimension/"+builtin, func(t *testing.T) {
			err := validateBuiltinOptions(builtin, nil)
			if err != nil && strings.Contains(err.Error(), "unknown builtin") {
				t.Errorf("%q is in dimensionBuiltins, but validateBuiltinOptions calls it unknown: %v", builtin, err)
			}
		})
	}
	for _, builtin := range analyzerBuiltins {
		t.Run("analyzer/"+builtin, func(t *testing.T) {
			_, err := validateAnalyzer(0, analyzerConfig{ID: "probe", Builtin: builtin})
			if err != nil && strings.Contains(err.Error(), "builtin must be") {
				t.Errorf("%q is in analyzerBuiltins, but validateAnalyzer rejects the name: %v", builtin, err)
			}
		})
	}

	t.Run("a name in neither registry is rejected", func(t *testing.T) {
		if err := validateBuiltinOptions("not-a-builtin", nil); err == nil || !strings.Contains(err.Error(), "unknown builtin") {
			t.Errorf("validateBuiltinOptions accepted a name outside dimensionBuiltins: %v", err)
		}
		if _, err := validateAnalyzer(0, analyzerConfig{ID: "probe", Builtin: "not-a-builtin"}); err == nil || !strings.Contains(err.Error(), "builtin must be") {
			t.Errorf("validateAnalyzer accepted a name outside analyzerBuiltins: %v", err)
		}
	})
}

// optionsBranches returns the builtin each allOf branch conditions on, after
// checking the branch actually selects an options schema.
func optionsBranches(t *testing.T, node schemaNode) []string {
	t.Helper()
	var out []string
	for _, branch := range node.AllOf {
		if branch.If == nil {
			continue
		}
		builtin, ok := branch.If.Properties["builtin"]
		if !ok || builtin.Const == nil {
			continue
		}
		if branch.Then == nil {
			t.Errorf("branch for builtin %q has no then", *builtin.Const)
			continue
		}
		if options := branch.Then.Properties["options"]; options.Ref == "" {
			t.Errorf("branch for builtin %q does not narrow options", *builtin.Const)
		}
		out = append(out, *builtin.Const)
	}
	return out
}

// The scaffold `pawl init` writes carries the editor directive on its first
// line, pointing at the schema's own published URL: the two are read from one
// place so a moved schema cannot leave the scaffold pointing nowhere.
func TestStarterConfigPointsAtSchema(t *testing.T) {
	schema := loadConfigSchema(t)
	const rawURLPrefix = "https://raw.githubusercontent.com/tiangong-dev/pawl/main/"
	if schema.ID != rawURLPrefix+"schema/pawl.schema.json" {
		t.Fatalf("schema $id = %q, want the file's published location under %s", schema.ID, rawURLPrefix)
	}
	firstLine, _, _ := strings.Cut(starterConfig, "\n")
	if want := "# yaml-language-server: $schema=" + schema.ID; firstLine != want {
		t.Errorf("starter config first line = %q, want %q", firstLine, want)
	}
}
