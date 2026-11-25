package pawl

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
)

// quietStdout buffers a quiet run's text output and releases it only when the
// exit code cannot say what happened on its own. Exit 0 means every dimension
// held, which the code already reports; exit 1 and exit 2 carry a "which one,
// and by how much" the caller still needs. A --format json run is never
// buffered: a caller parsing the verdict must always receive one.
func quietStdout(quiet bool, format string, stdout io.Writer) (io.Writer, func(int) int) {
	if !quiet || format != "text" {
		return stdout, func(code int) int { return code }
	}
	buf := &bytes.Buffer{}
	return buf, func(code int) int {
		if code != 0 {
			stdout.Write(buf.Bytes())
		}
		return code
	}
}

func runMeasureCommand(full, measure *Config, command, format, since, baseRef string, onlyIDs []string, dryRun, acceptWorse bool, supplied map[string]Metric, progress, stdout, stderr io.Writer, quiet bool) int {
	excluded := excludedDimensionIDs(full, onlyIDs)
	runScope := reportScope{command: command, since: since, baseRef: baseRef, only: onlyIDs, excluded: excluded}
	var baseline *Snapshot
	var parsedBaseline any
	var err error
	if baseRef != "" {
		baseline, err = readSnapshotAtRef(full, baseRef)
	} else {
		baseline, parsedBaseline, err = ReadSnapshotFile(full.SnapshotPath)
	}
	if err != nil {
		return abortCouldNotMeasure(runScope, format, err.Error(), nil, stdout, stderr)
	}
	if command != "record" {
		if baseline == nil {
			return abortCouldNotMeasure(runScope, format,
				fmt.Sprintf("no %s yet — run `pawl record` first.", full.SnapshotPath),
				nil, stdout, stderr)
		}
		if baseRef == "" {
			if shapeErrors := SnapshotShapeErrors(parsedBaseline); len(shapeErrors) > 0 {
				msg := full.SnapshotPath + " has an invalid shape:\n"
				for _, e := range shapeErrors {
					msg += "  • " + e + "\n"
				}
				return abortCouldNotMeasure(runScope, format, strings.TrimSuffix(msg, "\n"), nil, stdout, stderr)
			}
			ids := make([]string, 0, len(full.Dimensions))
			for _, d := range full.Dimensions {
				ids = append(ids, d.ID)
			}
			if orphans := OrphanedMetrics(ids, baseline.Metrics); len(orphans) > 0 {
				return abortCouldNotMeasure(runScope, format,
					fmt.Sprintf("orphaned metric(s) in %s — deleting a dimension must also drop it from the snapshot (re-run `pawl record`): %s",
						full.SnapshotPath, strings.Join(orphans, ", ")),
					nil, stdout, stderr)
			}
		}
		if mismatches := definitionMismatches(measure, baseline.Metrics); len(mismatches) > 0 {
			return abortCouldNotMeasure(runScope, format, definitionMismatchMessage(mismatches), nil, stdout, stderr)
		}
	}

	current, artifacts, err := acquireMeasurement(measure, supplied, progress, stderr)
	if err != nil {
		return abortCouldNotMeasure(runScope, format, err.Error(), failedMetricIDs(err), stdout, stderr)
	}

	if command == "record" {
		return finishRecord(full, format, baseline, current, artifacts, dryRun, acceptWorse, stdout, stderr)
	}

	// check. The report is the machine-readable and diff-scoped source of
	// truth; the legacy text path stays the byte-for-byte human default.
	rep := buildReport(command, measure, baseline, current, artifacts)
	rep.Only = onlyIDs
	rep.Excluded = excluded
	if baseRef != "" {
		rep.BaseRef = strPtr(baseRef)
	}
	var scope *sinceScope
	if since != "" {
		s, err := applySinceScope(full, rep, baseline, current, since)
		if err != nil {
			return abortCouldNotMeasure(runScope, format, err.Error(), nil, stdout, stderr)
		}
		scope = s
		hintUnstagedRename(progress, scope)
	}
	exit := 0
	if hasLiveRegression(rep) {
		exit = 1
	}
	rep.ExitCode = exit

	if format == "json" {
		attachWatch(full, rep, scope)
		if err := renderReportJSON(stdout, rep); err != nil {
			fmt.Fprintln(stderr, err)
			return 2
		}
		return exit
	}
	if since != "" {
		renderSinceText(stdout, rep, scope)
		hintJSONIfPiped(command, quiet, stdout, stderr)
		return exit
	}
	textExit := renderCheckTextLegacy(measure, command, baseline, current, excluded, stdout)
	hintJSONIfPiped(command, quiet, stdout, stderr)
	return textExit
}

// hintJSONIfPiped nudges toward the machine-readable verdict when `check`'s
// text output is not going to a human terminal — a script or agent driving
// pawl almost certainly wants --format json, and `pawl --help` alone has not
// been enough to get several different models to reach for it across real
// evaluation runs (see demo/README.md). Even this hint firing has not
// reliably changed the outcome in single-shot tasks, where the text table
// alone is legible enough — its payoff is likelier in longer, iterative
// gate loops, which is still unverified. Scoped to `check`, the command an
// automated loop actually gates on, and printed to stderr so it never risks
// perturbing a script parsing stdout.
func hintJSONIfPiped(command string, quiet bool, stdout, stderr io.Writer) {
	if command != "check" || quiet || isTerminalWriter(stdout) {
		return
	}
	fmt.Fprintln(stderr, "hint: for machine-readable output, use `pawl check --format json`")
}

func isTerminalWriter(w io.Writer) bool {
	f, ok := w.(*os.File)
	return ok && isTerminalFile(f)
}

// excludedDimensionIDs lists configured dimension ids `--only` left
// unmeasured this run — nil when `--only` was not used. Populated even on a
// could-not-measure (exit 2) verdict via reportScope, so scoping down to fix
// one broken dimension does not make the others quietly disappear from view.
func excludedDimensionIDs(full *Config, onlyIDs []string) []string {
	if len(onlyIDs) == 0 {
		return nil
	}
	onlySet := make(map[string]bool, len(onlyIDs))
	for _, id := range onlyIDs {
		onlySet[id] = true
	}
	var excluded []string
	for _, d := range full.Dimensions {
		if !onlySet[d.ID] {
			excluded = append(excluded, d.ID)
		}
	}
	sort.Strings(excluded)
	return excluded
}
