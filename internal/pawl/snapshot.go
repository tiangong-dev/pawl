package pawl

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ParseSnapshot decodes snapshot JSON into both the typed form (for
// comparison) and the untyped form (for shape validation). Invalid JSON is
// an error; shape validation is the caller's second step — the two failure
// modes carry different messages.
func ParseSnapshot(data []byte) (*Snapshot, any, error) {
	var parsed any
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil, nil, fmt.Errorf("not valid JSON: %w", err)
	}
	var snap Snapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		// Valid JSON of the wrong shape (e.g. a bare array): keep the untyped
		// form so SnapshotShapeErrors can name the problem precisely.
		return &Snapshot{}, parsed, nil
	}
	return &snap, parsed, nil
}

// ReadSnapshotFile loads a snapshot from disk; a missing file returns
// (nil, nil, nil) — "no baseline yet" is a legitimate state whose handling
// (refuse vs record) belongs to each command.
func ReadSnapshotFile(path string) (*Snapshot, any, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	snap, parsed, err := ParseSnapshot(data)
	if err != nil {
		return nil, nil, fmt.Errorf("%s: %w", path, err)
	}
	return snap, parsed, nil
}

// WriteSnapshotFile serializes with a pinned byte shape — 2-space indent,
// sorted metric ids, per-metric field order direction/value/unit/breakdown/
// tolerance, minimal decimal numbers, trailing newline — so the snapshot
// diffs cleanly in review and byte-identical measurements produce
// byte-identical files.
func WriteSnapshotFile(path string, metrics map[string]Metric) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, MarshalSnapshot(metrics), 0o644)
}

// MarshalSnapshot renders the pinned snapshot byte shape. Hand-rolled rather
// than json.Marshal because Go maps serialize in random order and
// encoding/json cannot express "omit tolerance only when undeclared" plus
// minimal-decimal numbers at the same time.
func MarshalSnapshot(metrics map[string]Metric) []byte {
	var b strings.Builder
	b.WriteString("{\n  \"metrics\": {")
	ids := sortedMetricKeys(metrics)
	for i, id := range ids {
		m := metrics[id]
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, "\n    %s: {\n", jsonString(id))
		fmt.Fprintf(&b, "      \"direction\": %s,\n", jsonString(string(m.Direction)))
		fmt.Fprintf(&b, "      \"value\": %s,\n", FormatNumber(m.Value))
		fmt.Fprintf(&b, "      \"unit\": %s,\n", jsonString(m.Unit))
		b.WriteString("      \"breakdown\": ")
		writeBreakdown(&b, m.Breakdown)
		if m.Tolerance != nil {
			fmt.Fprintf(&b, ",\n      \"tolerance\": %s", FormatNumber(*m.Tolerance))
		}
		b.WriteString("\n    }")
	}
	if len(ids) > 0 {
		b.WriteString("\n  ")
	}
	b.WriteString("}\n}\n")
	return []byte(b.String())
}

func writeBreakdown(b *strings.Builder, breakdown map[string]float64) {
	if breakdown == nil {
		b.WriteString("null")
		return
	}
	if len(breakdown) == 0 {
		b.WriteString("{}")
		return
	}
	keys := make([]string, 0, len(breakdown))
	for k := range breakdown {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	b.WriteString("{")
	for i, k := range keys {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(b, "\n        %s: %s", jsonString(k), FormatNumber(breakdown[k]))
	}
	b.WriteString("\n      }")
}

func jsonString(s string) string {
	encoded, err := json.Marshal(s)
	if err != nil {
		// json.Marshal of a string cannot fail; keep the gate honest anyway.
		panic(err)
	}
	return string(encoded)
}
