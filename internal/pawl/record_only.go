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

// resolveOnlyIDs validates `--only` against the config. An empty list or an
// unknown id is a usage error (exit 2) before anything is measured. The
// returned id slice is deduplicated and sorted — it is what the verdict
// reports as its `only` scope, so it must not vary with argument order.
func resolveOnlyIDs(cfg *Config, only, command string, stderr io.Writer) (map[string]bool, []string, int) {
	ids := parseOnly(only)
	if len(ids) == 0 {
		fmt.Fprintf(stderr, "--only requires at least one dimension id\n")
		return nil, nil, 2
	}
	byID := map[string]bool{}
	for _, d := range cfg.Dimensions {
		byID[d.ID] = true
	}
	onlySet := map[string]bool{}
	for _, id := range ids {
		if !byID[id] {
			fmt.Fprintf(stderr, "%s --only: no dimension %q in the config.\n", command, id)
			return nil, nil, 2
		}
		onlySet[id] = true
	}
	sorted := make([]string, 0, len(onlySet))
	for id := range onlySet {
		sorted = append(sorted, id)
	}
	sort.Strings(sorted)
	return onlySet, sorted, 0
}

func configWithOnly(cfg *Config, onlySet map[string]bool) *Config {
	sub := *cfg
	sub.Dimensions = nil
	for _, d := range cfg.Dimensions {
		if onlySet[d.ID] {
			sub.Dimensions = append(sub.Dimensions, d)
		}
	}
	return &sub
}

// runRecordOnly re-measures only the named dimensions and writes a snapshot
// that preserves every other configured metric's committed value — the surgical
// counterpart to a full record, so a win on one dimension can be locked in
// without re-blessing (or accidentally blessing a regression in) the rest. A
// listed dimension that comes back worse than the committed baseline is
// refused (exit 1, nothing written) unless dryRun or acceptWorse says
// otherwise — the same accepted-debt gate a full record applies, scoped to
// just the dimensions this invocation actually measured. See
// spec/commands/record.md § Partial record and § Accepted debt.
func runRecordOnly(cfg *Config, onlySet map[string]bool, only []string, format string, dryRun, acceptWorse bool, stdout, stderr io.Writer) int {
	runScope := reportScope{command: "record", only: only}
	if format == "codeclimate" {
		fmt.Fprintln(stderr, "record --only cannot emit codeclimate: a partial measurement is not a complete current findings report")
		return 2
	}

	// An existing, well-formed snapshot is what --only preserves; "preserve the
	// rest" is meaningless without a baseline.
	baseline, parsed, err := ReadSnapshotFile(cfg.SnapshotPath)
	if err != nil {
		return abortCouldNotMeasure(runScope, format, err.Error(), nil, stdout, stderr)
	}
	if baseline == nil {
		return abortCouldNotMeasure(runScope, format,
			fmt.Sprintf("record --only needs an existing %s to preserve — run a full `pawl record` first.", cfg.SnapshotPath),
			nil, stdout, stderr)
	}
	if shapeErrors := SnapshotShapeErrors(parsed); len(shapeErrors) > 0 {
		msg := cfg.SnapshotPath + " has an invalid shape:\n"
		for _, e := range shapeErrors {
			msg += "  • " + e + "\n"
		}
		return abortCouldNotMeasure(runScope, format, strings.TrimSuffix(msg, "\n"), nil, stdout, stderr)
	}

	// Measure only the listed dimensions — an unrelated broken adapter must not
	// block locking in the win.
	sub := configWithOnly(cfg, onlySet)
	measured, artifacts, err := MeasureAll(sub, stderr)
	if err != nil {
		return abortCouldNotMeasure(runScope, format, err.Error(), failedMetricIDs(err), stdout, stderr)
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

	// Only the listed (freshly measured) dimensions can possibly be worse — a
	// preserved value is copied verbatim from the baseline and can never regress.
	worse := WorseDimensions(sub.Dimensions, baseline.Metrics, measured)
	if len(worse) > 0 && !acceptWorse {
		return refuseRecordOnly(&shownCfg, format, baseline, measured, merged, artifacts, only, worse, dryRun, stdout, stderr)
	}
	if dryRun {
		return previewRecordOnly(&shownCfg, format, baseline, measured, merged, artifacts, only, preserved, worse, stdout, stderr)
	}

	if err := WriteSnapshotFile(cfg.SnapshotPath, merged); err != nil {
		// Same as a full record: exit 2 still owes --format json its verdict.
		return abortCouldNotMeasure(runScope, format, err.Error(), nil, stdout, stderr)
	}

	if format == "json" {
		rep := buildPartialRecordReport(&shownCfg, baseline, measured, merged, artifacts, only, true)
		rep.AcceptedWorse = acceptedWorseEntries(worse)
		rep.ExitCode = 0
		if err := renderReportJSON(stdout, rep); err != nil {
			fmt.Fprintln(stderr, err)
			return 2
		}
		return 0
	}
	printPartialRecordTable(stdout, &shownCfg, baseline, measured, merged)
	fmt.Fprintf(stdout, "📸 re-recorded %s; preserved %d other metric(s) → %s\n",
		strings.Join(only, ", "), preserved, displayPath(cfg.SnapshotPath))
	printAcceptedWorseTrailers(stdout, worse)
	return 0
}

// refuseRecordOnly is runRecordOnly's default path when worse is non-empty
// and --accept-worse was not given: nothing is written, exit 1, regardless of
// dryRun — see refuseRecord's comment for why dryRun still matters for the
// JSON dry_run field even on a refusal.
func refuseRecordOnly(shownCfg *Config, format string, baseline *Snapshot, measured, merged map[string]Metric, artifacts map[string]*ArtifactInfo, only []string, worse []WorseDimension, dryRun bool, stdout, stderr io.Writer) int {
	if format == "json" {
		rep := buildPartialRecordReport(shownCfg, baseline, measured, merged, artifacts, only, false)
		rep.DryRun = dryRun
		rep.ExitCode = 1
		if err := renderReportJSON(stdout, rep); err != nil {
			fmt.Fprintln(stderr, err)
			return 2
		}
		return 1
	}
	printPartialRecordTable(stdout, shownCfg, baseline, measured, merged)
	fmt.Fprintf(stdout, "❌ record --only refused — %d dimension(s) would be recorded worse than the committed baseline:\n", len(worse))
	printWorseDetail(stdout, shownCfg.Dimensions, baseline.Metrics, measured)
	fmt.Fprintln(stdout, "re-run with --accept-worse to record this as accepted debt, or fix the regression first.")
	return 1
}

// previewRecordOnly is runRecordOnly's --dry-run path, reached only once the
// write would not be refused: same rendering as a real record --only,
// nothing written, exit 0.
func previewRecordOnly(shownCfg *Config, format string, baseline *Snapshot, measured, merged map[string]Metric, artifacts map[string]*ArtifactInfo, only []string, preserved int, worse []WorseDimension, stdout, stderr io.Writer) int {
	if format == "json" {
		rep := buildPartialRecordReport(shownCfg, baseline, measured, merged, artifacts, only, false)
		rep.DryRun = true
		rep.AcceptedWorse = acceptedWorseEntries(worse)
		rep.ExitCode = 0
		if err := renderReportJSON(stdout, rep); err != nil {
			fmt.Fprintln(stderr, err)
			return 2
		}
		return 0
	}
	printPartialRecordTable(stdout, shownCfg, baseline, measured, merged)
	fmt.Fprintf(stdout, "🔍 dry run — nothing written. record --only would re-record %s; preserve %d other metric(s).\n",
		strings.Join(only, ", "), preserved)
	if len(worse) > 0 {
		fmt.Fprintln(stdout, "⚠️  this would record the following as accepted debt (--accept-worse):")
		for _, w := range worse {
			fmt.Fprintf(stdout, "    Pawl-Accept: %s %s\n", w.ID, FormatNumber(w.Current))
		}
	}
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
