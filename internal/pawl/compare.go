package pawl

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Worse reports whether cur regressed past base by more than tolerance —
// absolute slack in the worse direction. Exactly AT the tolerance boundary
// passes.
func Worse(d Direction, base, cur, tolerance float64) bool {
	if d == HigherIsBetter {
		return cur < base-tolerance
	}
	return cur > base+tolerance
}

// Better reports strict improvement (tolerance never applies).
func Better(d Direction, base, cur float64) bool {
	if d == HigherIsBetter {
		return cur > base
	}
	return cur < base
}

// FormatNumber prints v in minimal decimal notation, never exponent form —
// snapshot values and regression lines must stay grep-able and diff-stable
// at any magnitude.
func FormatNumber(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}

// OffenderCountsByFile counts breakdown KEYS grouped by the key's file part
// (substring before the first ':'; a key with no ':' is itself the file).
// Counting keys, not summing values, keeps the per-file-count gate robust to
// code moving around inside a file.
func OffenderCountsByFile(breakdown map[string]float64) map[string]int {
	out := map[string]int{}
	for k := range breakdown {
		file, _, _ := strings.Cut(k, ":")
		out[file]++
	}
	return out
}

// RegressionsOf returns human-readable regression lines for one dimension,
// honoring its gate mode. The scalar total is always checked (with
// tolerance); the per-file / per-key check on top stops a localized
// regression from hiding behind a net-zero total.
func RegressionsOf(spec GateSpec, base, cur MetricSample) []string {
	var out []string
	if Worse(spec.Direction, base.Value, cur.Value, spec.Tolerance) {
		out = append(out, fmt.Sprintf("total %s → %s", FormatNumber(base.Value), FormatNumber(cur.Value)))
	}
	switch spec.Gate {
	case GatePerFileCount:
		b := OffenderCountsByFile(base.Breakdown)
		c := OffenderCountsByFile(cur.Breakdown)
		for _, f := range sortedKeyUnion(b, c) {
			if c[f] > b[f] {
				out = append(out, fmt.Sprintf("%s  %d → %d", f, b[f], c[f]))
			}
		}
	case GatePerKeyValue:
		for _, k := range sortedKeys(base.Breakdown) {
			cv, ok := cur.Breakdown[k]
			if ok && Worse(spec.Direction, base.Breakdown[k], cv, spec.Tolerance) {
				out = append(out, fmt.Sprintf("%s  %s → %s", k, FormatNumber(base.Breakdown[k]), FormatNumber(cv)))
			}
		}
	}
	return out
}

// OrphanedMetrics returns the sorted ids of baseline metrics that no
// configured dimension claims. Deleting a dimension must also drop its
// metric from the snapshot, or a regression could hide behind a vanished
// measurement.
func OrphanedMetrics(dimensionIDs []string, baseline map[string]Metric) []string {
	claimed := map[string]bool{}
	for _, id := range dimensionIDs {
		claimed[id] = true
	}
	var out []string
	for id := range baseline {
		if !claimed[id] {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}

// BaselineGuardViolations compares two recorded snapshots' metrics directly:
// violations are metrics that worsened per their recorded direction (empty
// direction reads as lower-is-better, the conservative default for
// hand-crafted snapshots) and recorded tolerance; removed are metrics present
// in base but missing from pr. A metric only in pr has no baseline to
// violate and is ignored. Both lists are sorted by id.
func BaselineGuardViolations(base, pr map[string]Metric) (violations, removed []string) {
	for _, id := range sortedMetricKeys(base) {
		b := base[id]
		p, ok := pr[id]
		if !ok {
			removed = append(removed, id)
			continue
		}
		direction := b.Direction
		if direction == "" {
			direction = LowerIsBetter
		}
		tolerance := 0.0
		if b.Tolerance != nil {
			tolerance = *b.Tolerance
		}
		if Worse(direction, b.Value, p.Value, tolerance) {
			violations = append(violations, fmt.Sprintf("%s: %s → %s", id, FormatNumber(b.Value), FormatNumber(p.Value)))
		}
	}
	return violations, removed
}

// SnapshotShapeErrors validates a parsed (json.Unmarshal into any) snapshot's
// shape — valid JSON alone is not a trustworthy baseline: a truncated or
// hand-corrupted snapshot must not read as "consistent" for free.
func SnapshotShapeErrors(parsed any) []string {
	obj, ok := parsed.(map[string]any)
	if !ok {
		return []string{"snapshot is not an object"}
	}
	metrics, ok := obj["metrics"].(map[string]any)
	if !ok {
		return []string{"snapshot.metrics is missing or not an object"}
	}
	if len(metrics) == 0 {
		return []string{"snapshot.metrics is empty"}
	}
	var errs []string
	for _, id := range sortedKeysAny(metrics) {
		metric, ok := metrics[id].(map[string]any)
		if !ok {
			errs = append(errs, fmt.Sprintf("metric %q is not an object", id))
			continue
		}
		if _, ok := metric["value"].(float64); !ok {
			errs = append(errs, fmt.Sprintf("metric %q has no numeric value", id))
		}
	}
	return errs
}

// ImprovementNotice is the CI annotation naming every dimension that improved
// since the snapshot — a developer who only looked at check's exit code still
// finds out an improvement is sitting unrecorded. Empty off-CI or when
// nothing improved.
func ImprovementNotice(improvedIDs []string, onCI bool) string {
	if !onCI || len(improvedIDs) == 0 {
		return ""
	}
	return fmt.Sprintf("::notice::pawl improved: %s — run `pawl record` to lock in the gains.", strings.Join(improvedIDs, ", "))
}

// GitHubAnnotations renders GitHub Actions `::error::` workflow commands for one
// regressed dimension so violations surface inline on the PR diff, reusing the
// `path:line` breakdown key shape. Per-file-count emits one line-anchored
// annotation per NEW offender key in a file whose offender count rose;
// per-key-value emits one per key that worsened; a total-gate (or detail-less)
// regression emits a single file-less annotation. Empty when nothing regressed.
func GitHubAnnotations(id, title string, spec GateSpec, base, cur MetricSample) []string {
	var out []string
	switch spec.Gate {
	case GatePerFileCount:
		bFiles := OffenderCountsByFile(base.Breakdown)
		cFiles := OffenderCountsByFile(cur.Breakdown)
		for _, key := range sortedKeys(cur.Breakdown) {
			file, _, _ := strings.Cut(key, ":")
			_, isOld := base.Breakdown[key]
			if cFiles[file] > bFiles[file] && !isOld {
				out = append(out, annotationLine(id, title, key, "new "+id+" offender"))
			}
		}
	case GatePerKeyValue:
		for _, k := range sortedKeys(base.Breakdown) {
			if cv, ok := cur.Breakdown[k]; ok && Worse(spec.Direction, base.Breakdown[k], cv, spec.Tolerance) {
				out = append(out, annotationLine(id, title, k, FormatNumber(base.Breakdown[k])+" → "+FormatNumber(cv)))
			}
		}
	}
	if len(out) == 0 && Worse(spec.Direction, base.Value, cur.Value, spec.Tolerance) {
		out = append(out, fmt.Sprintf("::error title=pawl: %s::%s regressed: %s → %s",
			id, title, FormatNumber(base.Value), FormatNumber(cur.Value)))
	}
	return out
}

// annotationLine builds one `::error file=…,line=…::` command from a
// "path:line" breakdown key (the line clause is dropped when the key carries
// no numeric line suffix).
func annotationLine(id, title, key, detail string) string {
	file, line, hasLine := strings.Cut(key, ":")
	loc := "file=" + file
	if hasLine && line != "" {
		loc += ",line=" + line
	}
	return fmt.Sprintf("::error %s,title=pawl: %s::%s: %s", loc, id, title, detail)
}

func sortedKeys(m map[string]float64) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortedKeysAny(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortedMetricKeys(m map[string]Metric) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortedKeyUnion(a, b map[string]int) []string {
	seen := map[string]bool{}
	var out []string
	for k := range a {
		if !seen[k] {
			seen[k] = true
			out = append(out, k)
		}
	}
	for k := range b {
		if !seen[k] {
			seen[k] = true
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}
