package pawl

import (
	"fmt"
	"io"
	"sort"
	"strings"
)

// parseOnly splits a `--only` value into trimmed, non-empty dimension ids.
func parseOnly(s string) []string {
	var ids []string
	for _, p := range strings.Split(s, ",") {
		if t := strings.TrimSpace(p); t != "" {
			ids = append(ids, t)
		}
	}
	return ids
}

// runRecordOnly re-measures only the named dimensions and writes a snapshot
// that preserves every other configured metric's committed value — the surgical
// counterpart to a full record, so a win on one dimension can be locked in
// without re-blessing (or accidentally blessing a regression in) the rest.
// See SPEC.md § Partial record.
func runRecordOnly(cfg *Config, only []string, format string, stdout, stderr io.Writer) int {
	byID := map[string]bool{}
	for _, d := range cfg.Dimensions {
		byID[d.ID] = true
	}
	onlySet := map[string]bool{}
	for _, id := range only {
		if !byID[id] {
			fmt.Fprintf(stderr, "record --only: no dimension %q in the config.\n", id)
			return 2
		}
		onlySet[id] = true
	}
	if format == "codeclimate" {
		fmt.Fprintln(stderr, "record --only cannot emit codeclimate: a partial measurement is not a complete current findings report")
		return 2
	}

	// An existing, well-formed snapshot is what --only preserves; "preserve the
	// rest" is meaningless without a baseline.
	baseline, parsed, err := ReadSnapshotFile(cfg.SnapshotPath)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	if baseline == nil {
		fmt.Fprintf(stderr, "record --only needs an existing %s to preserve — run a full `pawl record` first.\n", cfg.SnapshotPath)
		return 2
	}
	if shapeErrors := SnapshotShapeErrors(parsed); len(shapeErrors) > 0 {
		fmt.Fprintf(stderr, "%s has an invalid shape:\n", cfg.SnapshotPath)
		for _, e := range shapeErrors {
			fmt.Fprintf(stderr, "  • %s\n", e)
		}
		return 2
	}

	// Measure only the listed dimensions — an unrelated broken adapter must not
	// block locking in the win.
	sub := *cfg
	sub.Dimensions = nil
	for _, d := range cfg.Dimensions {
		if onlySet[d.ID] {
			sub.Dimensions = append(sub.Dimensions, d)
		}
	}
	measured, err := MeasureAll(&sub, stderr)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}

	// Merge: freshly measured listed dims + preserved values for every other
	// CONFIGURED dim. Iterating config dimensions (not the old snapshot) drops
	// any orphan exactly as a full record does.
	merged := map[string]Metric{}
	preserved := 0
	for _, d := range cfg.Dimensions {
		if onlySet[d.ID] {
			merged[d.ID] = measured[d.ID]
			continue
		}
		if m, ok := baseline.Metrics[d.ID]; ok {
			merged[d.ID] = m
			preserved++
		}
	}

	if err := WriteSnapshotFile(cfg.SnapshotPath, merged); err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}

	// Render only the metrics actually in the written snapshot. A configured
	// dimension that is neither listed nor preserved is intentionally absent, and
	// bare `current[id]` indexing in the renderers would otherwise invent a
	// measured-looking 0 for it — "could not measure" must never read as measured.
	shownCfg := *cfg
	shownCfg.Dimensions = nil
	for _, d := range cfg.Dimensions {
		if _, ok := merged[d.ID]; ok {
			shownCfg.Dimensions = append(shownCfg.Dimensions, d)
		}
	}

	if format == "json" {
		rep := buildPartialRecordReport(&shownCfg, baseline, measured, merged)
		rep.ExitCode = 0
		if err := renderReportJSON(stdout, rep); err != nil {
			fmt.Fprintln(stderr, err)
			return 2
		}
		return 0
	}
	printPartialRecordTable(stdout, &shownCfg, baseline, measured, merged)
	recorded := append([]string(nil), only...)
	sort.Strings(recorded)
	fmt.Fprintf(stdout, "📸 re-recorded %s; preserved %d other metric(s) → %s\n",
		strings.Join(recorded, ", "), preserved, displayPath(cfg.SnapshotPath))
	return 0
}

func printPartialRecordTable(w io.Writer, cfg *Config, baseline *Snapshot, measured, merged map[string]Metric) {
	idWidth := 6
	for _, d := range cfg.Dimensions {
		if len(d.ID) > idWidth {
			idWidth = len(d.ID)
		}
	}
	fmt.Fprintln(w, "")
	fmt.Fprintf(w, "%s  %9s  %9s  %6s  status\n", pad("metric", idWidth), "baseline", "current", "Δ")
	fmt.Fprintln(w, strings.Repeat("-", idWidth+9+9+6+12))
	for _, dim := range cfg.Dimensions {
		if _, ok := merged[dim.ID]; !ok {
			continue
		}
		var base *float64
		if old, exists := baseline.Metrics[dim.ID]; exists {
			base = floatPtr(old.Value)
		}
		if cur, wasMeasured := measured[dim.ID]; wasMeasured {
			tolerance := dim.GateSpecOf().Tolerance
			fmt.Fprintf(w, "%s  %9s  %9s  %6s  %s\n",
				pad(dim.ID, idWidth), baseCell(base), FormatNumber(cur.Value),
				fmtDelta(base, cur.Value), statusOf(dim.Direction, base, cur.Value, tolerance))
			continue
		}
		fmt.Fprintf(w, "%s  %9s  %9s  %6s  preserved\n",
			pad(dim.ID, idWidth), baseCell(base), "—", "—")
	}
	fmt.Fprintln(w, "")
}
