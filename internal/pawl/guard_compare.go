package pawl

import (
	"fmt"
	"strconv"
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
	// RenamedFrom is the base path this count was recorded against when git
	// reports the file as renamed; empty when the count came from the same
	// path. Only the rendered line reads it.
	RenamedFrom string
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
// entries plus a trailing "and N more". An entry whose base count came from a
// renamed path says so, so the number that was compared is not mistaken for a
// count recorded against the new name.
func formatFileGrowth(files []FileGrowth) string {
	parts := make([]string, 0, len(files)+1)
	for i, f := range files {
		if i == fileGrowthShown {
			parts = append(parts, fmt.Sprintf("and %d more", len(files)-fileGrowthShown))
			break
		}
		entry := fmt.Sprintf("%s %s → %s", f.File, FormatNumber(f.Base), FormatNumber(f.Current))
		if f.RenamedFrom != "" {
			entry += fmt.Sprintf(" (renamed from %s)", f.RenamedFrom)
		}
		parts = append(parts, entry)
	}
	return strings.Join(parts, "; ")
}

// remapBaseRenames re-keys a base breakdown so counts recorded against a path
// git reports as renamed are counted under the destination path. A pair is
// dropped — falling back to the plain per-path comparison — when the base
// already has an entry for the destination (its counts would have to be added
// to a path that was already there), when two pairs claim the same destination,
// or when the source is absent from the base (nothing to move). The second
// result names, per destination, the source its count came from, for the
// violation's "(renamed from …)" note.
func remapBaseRenames(base map[string]float64, renames map[string]string) (map[string]float64, map[string]string) {
	if len(base) == 0 || len(renames) == 0 {
		return base, nil
	}
	bFiles := OffenderCountsByFile(base)
	claims := map[string]int{}
	for _, to := range renames {
		claims[to]++
	}
	applied := map[string]string{}
	for from, to := range renames {
		if claims[to] > 1 || from == to {
			continue
		}
		if _, known := bFiles[to]; known {
			continue
		}
		if _, known := bFiles[from]; !known {
			continue
		}
		applied[from] = to
	}
	if len(applied) == 0 {
		return base, nil
	}
	out := make(map[string]float64, len(base))
	origin := map[string]string{}
	for key, v := range base {
		file, line := splitLocationKey(key)
		to, renamed := applied[file]
		if !renamed {
			out[key] = v
			continue
		}
		out[rekeyedLocation(to, line)] += v
		origin[to] = file
	}
	return out, origin
}

// rekeyedLocation moves a breakdown key's path part onto to, keeping the
// location suffix so a file-only base entry stays file-only.
func rekeyedLocation(to string, line *int) string {
	if line == nil {
		return to
	}
	return to + ":" + strconv.Itoa(*line)
}

// guardFileGrowth is the per-file predicate guard applies on top of the scalar
// one. Only a per-file-count metric has one, and it reuses perFileRegressions —
// the same single source of truth check, record and --since read — so guard
// cannot disagree with the gate it protects. renames re-keys the base by git's
// detected renames before that comparison (empty for every other gate, which
// renames cannot affect). A metric gated total, gated per-key-value, or absent
// from the config is compared on the scalar alone.
func guardFileGrowth(gate GateMode, base, cur map[string]float64, renames map[string]string) []FileGrowth {
	if gate != GatePerFileCount {
		return nil
	}
	remapped, origin := remapBaseRenames(base, renames)
	var out []FileGrowth
	for _, r := range perFileRegressions(remapped, cur) {
		out = append(out, FileGrowth{File: r.File, Base: r.Base, Current: r.Current, RenamedFrom: origin[r.File]})
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
	return GuardViolationsWithGatesAndRenames(base, pr, gates, nil)
}

// GuardViolationsWithGatesAndRenames is GuardViolationsWithGates with git's
// renames applied to the per-file comparison: renames maps a path in base to
// the path git says the same file has in pr, both relative to the config dir
// (the form breakdown keys use). Applying it stops a purely renamed file from
// reading as a brand-new offender while still catching a rename that also added
// one. It changes nothing for a total or per-key-value gate, and a nil map is
// the plain comparison.
func GuardViolationsWithGatesAndRenames(base, pr map[string]Metric, gates map[string]GateMode, renames map[string]string) (violations []GuardViolation, removed []string) {
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
		files := guardFileGrowth(gates[id], b.Breakdown, p.Breakdown, renames)
		if Worse(direction, b.Value, p.Value, tolerance) || len(files) > 0 {
			violations = append(violations, GuardViolation{
				ID: id, Direction: direction, Base: b.Value, Current: p.Value, Tolerance: tolerance, Files: files,
			})
		}
	}
	return violations, removed
}
