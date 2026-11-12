package pawl

import (
	"fmt"
	"strings"
)

// fileGrowthShown caps how many files one violation line names: a regression
// touching dozens of files must stay a single readable line.
const fileGrowthShown = 10

// FileGrowth is one file whose recorded offender count rose between two
// snapshots — the per-file half of a GuardViolation on a per-file-count
// metric.
type FileGrowth struct {
	File    string
	Base    float64
	Current float64
}

// GuardViolation is one metric that worsened between two recorded snapshots,
// as compared directly (not via a fresh measurement) — the shape
// `pawl guard` needs to decide whether a Pawl-Accept trailer covers it.
type GuardViolation struct {
	ID        string
	Direction Direction
	Base      float64
	Current   float64
	Tolerance float64
	// Files lists the per-file rises behind the violation of a
	// per-file-count metric; empty for every other gate mode and for a
	// purely scalar regression.
	Files []FileGrowth
}

func (v GuardViolation) String() string {
	line := fmt.Sprintf("%s: %s → %s", v.ID, FormatNumber(v.Base), FormatNumber(v.Current))
	if len(v.Files) == 0 {
		return line
	}
	return fmt.Sprintf("%s (%s)", line, formatFileGrowth(v.Files))
}

// formatFileGrowth renders the per-file detail of a violation as
// "file base → current" entries separated by "; ", capped at fileGrowthShown
// entries plus a trailing "and N more".
func formatFileGrowth(files []FileGrowth) string {
	parts := make([]string, 0, len(files)+1)
	for i, f := range files {
		if i == fileGrowthShown {
			parts = append(parts, fmt.Sprintf("and %d more", len(files)-fileGrowthShown))
			break
		}
		parts = append(parts, fmt.Sprintf("%s %s → %s", f.File, FormatNumber(f.Base), FormatNumber(f.Current)))
	}
	return strings.Join(parts, "; ")
}

// guardFileGrowth is the per-file predicate guard applies on top of the scalar
// one. Only a per-file-count metric has one, and it reuses perFileRegressions —
// the same single source of truth check, record and --since read — so guard
// cannot disagree with the gate it protects. A metric gated total, gated
// per-key-value, or absent from the config is compared on the scalar alone.
func guardFileGrowth(gate GateMode, base, cur map[string]float64) []FileGrowth {
	if gate != GatePerFileCount {
		return nil
	}
	var out []FileGrowth
	for _, r := range perFileRegressions(base, cur) {
		out = append(out, FileGrowth{File: r.File, Base: r.Base, Current: r.Current})
	}
	return out
}

// GuardViolations compares two recorded snapshots' metrics directly:
// violations are metrics that worsened per their recorded direction (empty
// direction reads as lower-is-better, the conservative default for
// hand-crafted snapshots) and recorded tolerance; removed are metrics present
// in base but missing from pr. A metric only in pr has no baseline to
// violate and is ignored. Both lists are sorted by id.
//
// It is the scalar-only comparison; GuardViolationsWithGates adds the per-file
// predicate of a configured gate on top.
func GuardViolations(base, pr map[string]Metric) (violations []GuardViolation, removed []string) {
	return GuardViolationsWithGates(base, pr, nil)
}

// GuardViolationsWithGates is GuardViolations plus the per-file predicate of
// each metric's gate mode: under a per-file-count gate a metric whose scalar
// total did not worsen is still a violation when an offender count rose in
// some file. gates comes from the working tree's config because a snapshot
// records direction and tolerance but no gate; a config that changes a gate
// changes the metric's definition fingerprint, so the metric is redefined for
// guard rather than silently reinterpreted. A nil gates map (or an id absent
// from it) compares the scalar alone, exactly like GuardViolations.
func GuardViolationsWithGates(base, pr map[string]Metric, gates map[string]GateMode) (violations []GuardViolation, removed []string) {
	for _, id := range sortedMetricKeys(base) {
		b := base[id]
		p, ok := pr[id]
		if !ok {
			removed = append(removed, id)
			continue
		}
		if !definitionsCompatible(b.Definition, p.Definition) {
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
		files := guardFileGrowth(gates[id], b.Breakdown, p.Breakdown)
		if Worse(direction, b.Value, p.Value, tolerance) || len(files) > 0 {
			violations = append(violations, GuardViolation{
				ID: id, Direction: direction, Base: b.Value, Current: p.Value, Tolerance: tolerance, Files: files,
			})
		}
	}
	return violations, removed
}
