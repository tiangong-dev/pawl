package pawl

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
)

const definitionFingerprintPrefix = "v1:sha256:"

// definitionFingerprint binds a recorded number to the validated configuration
// that gives it meaning. Presentation and execution-budget fields are excluded:
// changing a title or timeout can change neither the value nor how it gates.
func definitionFingerprint(cfg *Config, dim Dimension) (string, error) {
	gate := dim.Gate
	if gate == "" {
		gate = GateTotal
	}
	definition := map[string]any{
		"version":   1,
		"direction": dim.Direction,
		"gate":      gate,
		"tolerance": dim.GateSpecOf().Tolerance,
	}
	switch {
	case dim.Command != "":
		// The command is an adapter implementation detail: replacing a tool or
		// wrapper while keeping the same metric is a core pawl use case. Extract
		// declares the output semantics and therefore is part of the definition.
		adapter := map[string]any{"kind": "command"}
		if dim.Extract != nil {
			adapter["extract"] = canonicalExtract(dim.Extract)
		}
		definition["adapter"] = adapter
	case dim.Builtin != "":
		definition["adapter"] = map[string]any{
			"kind":    "builtin",
			"builtin": dim.Builtin,
			"options": canonicalValue(semanticOptions(dim.Builtin, dim.Options)),
		}
	case dim.Source != "":
		analyzer := cfg.Analyzers[dim.Source]
		definition["adapter"] = map[string]any{
			"kind":     "analyzer",
			"builtin":  analyzer.Builtin,
			"options":  canonicalValue(semanticOptions(analyzer.Builtin, analyzer.Options)),
			"selector": canonicalValue(dim.Options),
		}
	}
	data, err := json.Marshal(definition)
	if err != nil {
		return "", fmt.Errorf("marshalling measurement definition for %s: %w", dim.ID, err)
	}
	sum := sha256.Sum256(data)
	return definitionFingerprintPrefix + hex.EncodeToString(sum[:]), nil
}

func validDefinitionFingerprint(value string) bool {
	if len(value) != len(definitionFingerprintPrefix)+sha256.Size*2 || value[:len(definitionFingerprintPrefix)] != definitionFingerprintPrefix {
		return false
	}
	_, err := hex.DecodeString(value[len(definitionFingerprintPrefix):])
	return err == nil
}

func semanticOptions(builtin string, options map[string]any) map[string]any {
	out := map[string]any{}
	for key, value := range options {
		switch key {
		case "command", "file", "report", "verify", "verify_rules", "min_files", "valid_exit_codes":
			// Acquisition, completeness and failure policy affect whether pawl can
			// measure honestly, not what a successful number means.
			continue
		default:
			if list, ok := value.([]any); ok && len(list) == 0 {
				continue // absent and explicitly empty filters/globs are equivalent
			}
			out[key] = value
		}
	}
	switch builtin {
	case builtinFileLength:
		if _, ok := out["threshold"]; !ok {
			out["threshold"] = defaultFileLengthThreshold
		}
	case builtinFileBytes:
		if _, ok := out["threshold"]; !ok {
			out["threshold"] = defaultFileBytesThreshold
		}
	case builtinSwiftComplexity:
		if metric, _ := out["metric"].(string); metric == "" {
			out["metric"] = "cognitive"
		}
	case builtinJUnit:
		if count, _ := out["count"].(string); count == "" {
			out["count"] = "failures"
		}
	case builtinCoverage:
		if metric, _ := out["metric"].(string); metric == "" {
			out["metric"] = "lines"
		}
	}
	return out
}

func canonicalExtract(spec *ExtractSpec) map[string]any {
	out := map[string]any{"form": spec.Form}
	if spec.Regex != nil {
		out["regex"] = spec.Regex.String()
	}
	if spec.JSONPath != "" {
		out["json_path"] = spec.JSONPath
	}
	if spec.Unit != "" {
		out["unit"] = spec.Unit
	}
	return out
}

// Config lists are sets throughout pawl (globs, rules, levels, valid exits,
// verification probes). Sorting their canonical JSON makes harmless YAML
// reordering byte-identical while preserving scalar types and map structure.
func canonicalValue(v any) any {
	switch value := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(value))
		for key, item := range value {
			out[key] = canonicalValue(item)
		}
		return out
	case []any:
		items := make([]any, len(value))
		for i, item := range value {
			items[i] = canonicalValue(item)
		}
		sort.Slice(items, func(i, j int) bool {
			left, _ := json.Marshal(items[i])
			right, _ := json.Marshal(items[j])
			return string(left) < string(right)
		})
		return items
	case []string:
		items := append([]string(nil), value...)
		sort.Strings(items)
		return items
	default:
		return value
	}
}

type definitionMismatch struct {
	ID         string
	Recorded   string
	Configured string
}

// definitionsCompatible is the single compatibility law for every place that
// compares or carries recorded measurements. An empty fingerprint is the
// legacy, unknown-definition state: it remains comparable for migration. Only
// two present, different fingerprints prove that the numeric meanings differ.
func definitionsCompatible(left, right string) bool {
	return left == "" || right == "" || left == right
}

// bindMetricDefinition validates a supplied metric against the current
// definition and upgrades the legacy empty state at the point it becomes a
// newly recorded measurement.
func bindMetricDefinition(metric Metric, current string) (Metric, bool) {
	if !definitionsCompatible(metric.Definition, current) {
		return Metric{}, false
	}
	metric.Definition = current
	return metric, true
}

func definitionMismatches(cfg *Config, metrics map[string]Metric) []definitionMismatch {
	var out []definitionMismatch
	for _, dim := range cfg.Dimensions {
		metric, ok := metrics[dim.ID]
		// Snapshots written before fingerprints existed remain readable. The
		// next record upgrades whichever metrics it measures; once present, a
		// fingerprint is mandatory for every future comparison.
		if !ok || definitionsCompatible(metric.Definition, dim.Definition) {
			continue
		}
		out = append(out, definitionMismatch{ID: dim.ID, Recorded: metric.Definition, Configured: dim.Definition})
	}
	return out
}

// comparableSnapshot drops metrics whose definition changed. A record command
// then treats those dimensions as explicitly redefined rather than comparing
// unlike numbers; check rejects the same mismatch before it measures.
func comparableSnapshot(cfg *Config, baseline *Snapshot) (*Snapshot, []definitionMismatch) {
	if baseline == nil {
		return nil, nil
	}
	mismatches := definitionMismatches(cfg, baseline.Metrics)
	if len(mismatches) == 0 {
		return baseline, nil
	}
	metrics := make(map[string]Metric, len(baseline.Metrics))
	for id, metric := range baseline.Metrics {
		metrics[id] = metric
	}
	for _, mismatch := range mismatches {
		delete(metrics, mismatch.ID)
	}
	return &Snapshot{Metrics: metrics}, mismatches
}

func definitionMismatchMessage(mismatches []definitionMismatch) string {
	ids := make([]string, len(mismatches))
	for i, mismatch := range mismatches {
		ids[i] = mismatch.ID
	}
	sort.Strings(ids)
	return fmt.Sprintf("measurement definition changed for: %s — the recorded numbers are not comparable; review the config change and run a full `pawl record` to establish the new definitions", joinComma(ids))
}

func joinComma(items []string) string {
	if len(items) == 0 {
		return ""
	}
	out := items[0]
	for _, item := range items[1:] {
		out += ", " + item
	}
	return out
}
