package pawl

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// Units and breakdown keys routinely contain HTML-sensitive characters
// ("files > 500 lines", a path with `&`). Writing them verbatim keeps the
// snapshot readable and its diffs reviewable; the document stays valid JSON.
func TestMarshalSnapshotKeepsHTMLSensitiveCharactersLiteral(t *testing.T) {
	metrics := map[string]Metric{
		"files-over": {
			Direction: LowerIsBetter,
			Value:     2,
			Unit:      "files > 500 lines",
			Breakdown: map[string]float64{"pkg/a&b.go": 612, "pkg/c<d.go": 700},
		},
	}

	out := string(MarshalSnapshot(metrics))
	for _, escape := range []string{`\u003c`, `\u003e`, `\u0026`} {
		if strings.Contains(out, escape) {
			t.Errorf("snapshot contains the %s escape:\n%s", escape, out)
		}
	}
	for _, want := range []string{`"files > 500 lines"`, `"pkg/a&b.go"`, `"pkg/c<d.go"`} {
		if !strings.Contains(out, want) {
			t.Errorf("snapshot is missing literal %s:\n%s", want, out)
		}
	}

	var roundTrip Snapshot
	if err := json.Unmarshal([]byte(out), &roundTrip); err != nil {
		t.Fatalf("written snapshot is not valid JSON: %v\n%s", err, out)
	}
	if got := roundTrip.Metrics["files-over"]; !reflect.DeepEqual(got, metrics["files-over"]) {
		t.Errorf("round trip changed the metric:\ngot  %+v\nwant %+v", got, metrics["files-over"])
	}
}

// Dropping HTML escaping must leave the rest of the JSON string rules alone:
// control characters, quotes, backslashes and the JavaScript line separators
// are still escaped, and the value survives a round trip unchanged.
func TestMarshalSnapshotStillEscapesNonHTMLCharacters(t *testing.T) {
	unit := "a\nb\tc\"d\\e\u0001\u2028"
	metrics := map[string]Metric{"m": {Direction: LowerIsBetter, Unit: unit}}

	out := string(MarshalSnapshot(metrics))
	for _, escape := range []string{`\n`, `\t`, `\"`, `\\`, `\u0001`, `\u2028`} {
		if !strings.Contains(out, escape) {
			t.Errorf("snapshot no longer escapes %s:\n%s", escape, out)
		}
	}

	var roundTrip Snapshot
	if err := json.Unmarshal([]byte(out), &roundTrip); err != nil {
		t.Fatalf("written snapshot is not valid JSON: %v\n%s", err, out)
	}
	if got := roundTrip.Metrics["m"].Unit; got != unit {
		t.Errorf("unit round trip = %q, want %q", got, unit)
	}
}

// Snapshots written before HTML-sensitive characters stopped being escaped
// stay readable: both spellings parse to the same measurement, so an existing
// baseline does not need re-recording to remain comparable.
func TestParseSnapshotTreatsEscapedHTMLCharactersAsLiteral(t *testing.T) {
	escaped := []byte(`{"metrics":{"m":{"direction":"lower-is-better","value":1,` +
		`"unit":"files \u003e 500 lines \u0026 more","breakdown":{"pkg/a\u0026b.go":1,"pkg/c\u003cd.go":2}}}}`)
	literal := []byte(`{"metrics":{"m":{"direction":"lower-is-better","value":1,` +
		`"unit":"files > 500 lines & more","breakdown":{"pkg/a&b.go":1,"pkg/c<d.go":2}}}}`)

	escapedSnap, _, err := ParseSnapshot(escaped)
	if err != nil {
		t.Fatalf("escaped snapshot did not parse: %v", err)
	}
	literalSnap, _, err := ParseSnapshot(literal)
	if err != nil {
		t.Fatalf("literal snapshot did not parse: %v", err)
	}
	if !reflect.DeepEqual(escapedSnap, literalSnap) {
		t.Errorf("escaping changed the parsed snapshot:\nescaped %+v\nliteral %+v", escapedSnap, literalSnap)
	}
}

// The definition fingerprint is what tells `guard` a recorded number still
// means the same thing. It is hashed from json.Marshal bytes, so it must not
// follow the snapshot's string rendering: if it moved, every recorded metric
// would look redefined and the gate would pass them all. The expected values
// were produced from the tree before HTML-sensitive escaping was dropped from
// the snapshot writer.
func TestDefinitionFingerprintIgnoresSnapshotStringRendering(t *testing.T) {
	// Split so this test's own source does not add markers to pawl's gate.
	markerPattern := "TO" + "DO > FIX" + "ME & HACK < WIP"
	cfg := &Config{Analyzers: map[string]Analyzer{
		"lints": {ID: "lints", Builtin: "eslint"},
	}}
	cases := []struct {
		name string
		dim  Dimension
		want string
	}{
		{
			name: "builtin options",
			dim: Dimension{
				ID:        "markers",
				Direction: LowerIsBetter,
				Builtin:   "pattern-count",
				Options:   map[string]any{"pattern": markerPattern, "glob": "**/*.go"},
			},
			want: "v1:sha256:6221158defcb042ecf1649c7c04221d8bd4620eb27a18c5766287a6fc1536dc3",
		},
		{
			name: "analyzer selector",
			dim: Dimension{
				ID:        "lint-count",
				Direction: LowerIsBetter,
				Source:    "lints",
				Options:   map[string]any{"level": "error", "rules": []any{"a & b", "c > d"}},
			},
			want: "v1:sha256:cb1ac752354e64232842ad547eb5654f9323035bda40a3fbd19fea7cb95627b9",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := definitionFingerprint(cfg, tc.dim)
			if err != nil {
				t.Fatalf("definitionFingerprint: %v", err)
			}
			if got != tc.want {
				t.Errorf("fingerprint moved for HTML-sensitive %s: got %s, want %s", tc.name, got, tc.want)
			}
		})
	}
}
