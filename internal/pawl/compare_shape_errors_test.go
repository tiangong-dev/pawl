package pawl_test

import (
	"encoding/json"
	"reflect"
	"sort"
	"testing"

	pawl "github.com/tiangong-dev/pawl/internal/pawl"
)

// SnapshotShapeErrors validates a snapshot parsed via json.Unmarshal into
// `any`, so it must accept the same shapes json.Unmarshal produces (maps,
// slices, strings, numbers, bools, nil) and refuse anything that json.Valid
// alone would let through as "consistent". Errors are checked in the exact
// precedence order the SPEC pins: not-an-object, then metrics
// missing/not-object, then metrics-empty, then per-metric checks.
func TestSnapshotShapeErrors(t *testing.T) {
	cases := []struct {
		name string
		json string
		want []string
	}{
		{
			name: "a bare JSON string is not an object",
			json: `"just a string"`,
			want: []string{"snapshot is not an object"},
		},
		{
			name: "a JSON array is not an object",
			json: `[1, 2, 3]`,
			want: []string{"snapshot is not an object"},
		},
		{
			name: "a JSON number is not an object",
			json: `42`,
			want: []string{"snapshot is not an object"},
		},
		{
			name: "null is not an object",
			json: `null`,
			want: []string{"snapshot is not an object"},
		},
		{
			name: "missing metrics key",
			json: `{}`,
			want: []string{"snapshot.metrics is missing or not an object"},
		},
		{
			name: "metrics is not an object",
			json: `{"metrics": "nope"}`,
			want: []string{"snapshot.metrics is missing or not an object"},
		},
		{
			name: "metrics is an array, not an object",
			json: `{"metrics": [1, 2]}`,
			want: []string{"snapshot.metrics is missing or not an object"},
		},
		{
			name: "metrics present but empty",
			json: `{"metrics": {}}`,
			want: []string{"snapshot.metrics is empty"},
		},
		{
			name: "a metric that is not an object",
			json: `{"metrics": {"a": "not an object"}}`,
			want: []string{`metric "a" is not an object`},
		},
		{
			name: "a metric with no value field at all",
			json: `{"metrics": {"a": {"direction": "lower-is-better"}}}`,
			want: []string{`metric "a" has no numeric value`},
		},
		{
			name: "a metric whose value is a string, not a number",
			json: `{"metrics": {"a": {"value": "5"}}}`,
			want: []string{`metric "a" has no numeric value`},
		},
		{
			name: "a metric whose value is null",
			json: `{"metrics": {"a": {"value": null}}}`,
			want: []string{`metric "a" has no numeric value`},
		},
		{
			name: "a metric whose value is a bool",
			json: `{"metrics": {"a": {"value": true}}}`,
			want: []string{`metric "a" has no numeric value`},
		},
		{
			name: "a valid metric produces no errors",
			json: `{"metrics": {"a": {"value": 5}}}`,
			want: nil,
		},
		{
			name: "a valid metric with all fields produces no errors",
			json: `{"metrics": {"a": {"direction": "lower-is-better", "value": 5, "unit": "count", "breakdown": {"x.go": 1}, "tolerance": 1}}}`,
			want: nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var parsed any
			if err := json.Unmarshal([]byte(tc.json), &parsed); err != nil {
				t.Fatalf("test fixture is not valid JSON: %v", err)
			}
			got := pawl.SnapshotShapeErrors(parsed)
			sort.Strings(got)
			want := append([]string(nil), tc.want...)
			sort.Strings(want)
			if len(got) == 0 && len(want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("SnapshotShapeErrors(%s) = %#v, want %#v", tc.json, got, want)
			}
		})
	}
}

// When multiple metrics are individually malformed, each is reported — one
// bad metric must not hide another.
func TestSnapshotShapeErrorsReportsEachBadMetric(t *testing.T) {
	var parsed any
	src := `{"metrics": {"a": {"value": 5}, "b": {}, "c": "not an object"}}`
	if err := json.Unmarshal([]byte(src), &parsed); err != nil {
		t.Fatalf("test fixture is not valid JSON: %v", err)
	}
	got := pawl.SnapshotShapeErrors(parsed)
	want := []string{`metric "b" has no numeric value`, `metric "c" is not an object`}
	sort.Strings(got)
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("SnapshotShapeErrors = %#v, want %#v", got, want)
	}
}
