package pawl

import (
	"fmt"
	"io"
	"reflect"
	"strings"
)

// finishRecord decides whether record may write. A full record that would
// silently accept a worse value in a dimension the author wasn't specifically
// looking at defeats the point of a ratchet, so by default record refuses
// (exit 1, nothing written) whenever any dimension would be recorded worse
// than the committed baseline; --accept-worse says the author reviewed that
// and means it. The refusal check runs BEFORE --dry-run is even considered:
// `record --dry-run` alone, on a regression, still refuses (exit 1, nothing
// written) exactly as a real record would — --dry-run previews a write, and a
// refused invocation never gets to the write. Only `--dry-run --accept-worse`
// reaches the preview path. When it does write, the snapshot write happens
// BEFORE any stdout so a write failure exits 2 without having already
// emitted an `exit_code: 0` verdict it can't take back.
func finishRecord(cfg *Config, format string, baseline *Snapshot, current map[string]Metric, artifacts map[string]*ArtifactInfo, dryRun, acceptWorse bool, stdout, stderr io.Writer) int {
	baselineMetrics := map[string]Metric{}
	if baseline != nil {
		baselineMetrics = baseline.Metrics
	}
	worse := WorseDimensions(cfg.Dimensions, baselineMetrics, current)

	if len(worse) > 0 && !acceptWorse {
		return refuseRecord(cfg, format, baseline, baselineMetrics, current, artifacts, worse, dryRun, stdout, stderr)
	}
	if dryRun {
		return previewRecord(cfg, format, baseline, baselineMetrics, current, artifacts, worse, stdout, stderr)
	}

	if err := WriteSnapshotFile(cfg.SnapshotPath, current); err != nil {
		// A snapshot that could not be written is exit 2 like any other
		// could-not-measure, and owes --format json the same verdict object:
		// exit 2 with an empty stdout is indistinguishable from a broken parse.
		return abortCouldNotMeasure(reportScope{command: "record"}, format, err.Error(), nil, stdout, stderr)
	}
	if format == "json" {
		rep := buildReport("record", cfg, baseline, current, artifacts)
		rep.AcceptedWorse = acceptedWorseEntries(worse)
		rep.ExitCode = 0
		if err := renderReportJSON(stdout, rep); err != nil {
			fmt.Fprintln(stderr, err)
			return 2
		}
		return 0
	}
	printTable(stdout, cfg, baseline, current, nil)
	fmt.Fprintf(stdout, "📸 snapshot written to %s\n", displayPath(cfg.SnapshotPath))
	printAcceptedWorseTrailers(stdout, worse)
	return 0
}

// refuseRecord is finishRecord's default path when worse is non-empty and
// --accept-worse was not given: nothing is written, exit 1, regardless of
// dryRun (a refusal IS what --dry-run would have predicted, so there is
// nothing further to preview). dryRun only controls the JSON dry_run field,
// so a `--dry-run` caller can tell this refusal apart from a real one that
// hit the same exit code.
func refuseRecord(cfg *Config, format string, baseline *Snapshot, baselineMetrics, current map[string]Metric, artifacts map[string]*ArtifactInfo, worse []WorseDimension, dryRun bool, stdout, stderr io.Writer) int {
	if format == "json" {
		rep := buildReport("record", cfg, baseline, current, artifacts)
		rep.DryRun = dryRun
		rep.ExitCode = 1
		if err := renderReportJSON(stdout, rep); err != nil {
			fmt.Fprintln(stderr, err)
			return 2
		}
		return 1
	}
	printTable(stdout, cfg, baseline, current, nil)
	fmt.Fprintf(stdout, "❌ record refused — %d dimension(s) would be recorded worse than the committed baseline:\n", len(worse))
	printWorseDetail(stdout, cfg.Dimensions, baselineMetrics, current)
	fmt.Fprintln(stdout, "record keeps every other dimension's gain; accepting a worse value must be explicit.")
	fmt.Fprintln(stdout, "re-run with --accept-worse to record this as accepted debt, or fix the regression first.")
	return 1
}

// previewRecord is finishRecord's --dry-run path, reached only once the
// write would not be refused (no worse dimensions, or --accept-worse was
// given): same rendering as a real record, nothing written, exit 0.
func previewRecord(cfg *Config, format string, baseline *Snapshot, baselineMetrics, current map[string]Metric, artifacts map[string]*ArtifactInfo, worse []WorseDimension, stdout, stderr io.Writer) int {
	if format == "json" {
		rep := buildReport("record", cfg, baseline, current, artifacts)
		rep.DryRun = true
		rep.AcceptedWorse = acceptedWorseEntries(worse)
		rep.ExitCode = 0
		if err := renderReportJSON(stdout, rep); err != nil {
			fmt.Fprintln(stderr, err)
			return 2
		}
		return 0
	}
	printTable(stdout, cfg, baseline, current, nil)
	changed := recordPreviewSummary(cfg.Dimensions, baselineMetrics, current)
	if len(changed) == 0 {
		fmt.Fprintln(stdout, "🔍 dry run — nothing written. record would change nothing (no metric differs from the baseline).")
	} else {
		fmt.Fprintf(stdout, "🔍 dry run — nothing written. record would update %d metric(s): %s\n", len(changed), strings.Join(changed, ", "))
	}
	if len(worse) > 0 {
		fmt.Fprintln(stdout, "⚠️  this would record the following as accepted debt (--accept-worse):")
		for _, w := range worse {
			fmt.Fprintf(stdout, "    Pawl-Accept: %s %s\n", w.ID, FormatNumber(w.Current))
		}
	}
	return 0
}

// printAcceptedWorseTrailers prints the "recorded worse" hint block naming
// the Pawl-Accept trailer(s) to add to the commit — a no-op when worse is
// empty, so callers can invoke it unconditionally after a real write.
func printAcceptedWorseTrailers(w io.Writer, worse []WorseDimension) {
	if len(worse) == 0 {
		return
	}
	fmt.Fprintln(w, "⚠️  recorded worse — add these trailers to the commit that includes the snapshot:")
	for _, wd := range worse {
		fmt.Fprintf(w, "    Pawl-Accept: %s %s\n", wd.ID, FormatNumber(wd.Current))
	}
	fmt.Fprintln(w, "baseline-guard treats a worsened metric without a matching trailer as unauthorized.")
}

// printWorseDetail prints one "• id (title)" header per dimension with a live
// regression, followed by RegressionsOf's own detail lines — the same
// rendering check's "❌ regressions:" block uses. A scalar-only "base →
// current" line would be misleading (or silently "same value") for a
// per-file-count/per-key-value dimension whose scalar total didn't move but
// still gate-regressed; this reuses the gate-aware detail so the refusal
// explains itself the same way `check` would have.
func printWorseDetail(w io.Writer, dims []Dimension, baseline, current map[string]Metric) {
	for _, dim := range dims {
		base, ok := baseline[dim.ID]
		if !ok {
			continue
		}
		cur, ok := current[dim.ID]
		if !ok {
			continue
		}
		detail := RegressionsOf(dim.GateSpecOf(),
			MetricSample{Value: base.Value, Breakdown: base.Breakdown},
			MetricSample{Value: cur.Value, Breakdown: cur.Breakdown})
		if len(detail) == 0 {
			continue
		}
		fmt.Fprintf(w, "  • %s (%s)\n", dim.ID, dim.Title)
		for _, line := range detail {
			fmt.Fprintf(w, "      %s\n", line)
		}
	}
}

// recordPreviewSummary formats "id base→current" (or "id new→current" with no
// baseline, or "id current (breakdown changed)" when the scalar held but the
// per-file/per-key breakdown didn't — a net-zero-scalar regression still
// writes different bytes) for every dimension whose fresh measurement differs
// from the committed baseline, in config order — the --dry-run "would update"
// list.
func recordPreviewSummary(dims []Dimension, baseline, current map[string]Metric) []string {
	var out []string
	for _, dim := range dims {
		cur, ok := current[dim.ID]
		if !ok {
			continue
		}
		base, hadBase := baseline[dim.ID]
		if !hadBase {
			out = append(out, fmt.Sprintf("%s new→%s", dim.ID, FormatNumber(cur.Value)))
			continue
		}
		if reflect.DeepEqual(base, cur) {
			continue
		}
		if base.Value != cur.Value {
			out = append(out, fmt.Sprintf("%s %s→%s", dim.ID, FormatNumber(base.Value), FormatNumber(cur.Value)))
		} else {
			out = append(out, fmt.Sprintf("%s %s (breakdown changed)", dim.ID, FormatNumber(cur.Value)))
		}
	}
	return out
}
