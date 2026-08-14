package pawl

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Version is stamped at build time via
// `-ldflags "-X github.com/tiangong-dev/pawl/internal/pawl.Version=<x.y.z>"`; source builds
// (including `go install`) report "dev". The npm distribution stamps it; a
// VCS-derived fallback is deliberately avoided because Go stamps even plain
// `go build` binaries, which would make the reported version non-deterministic.
var Version = "dev"

// RunCLI executes one pawl invocation and returns the process exit code:
// 0 = pass, 1 = regression/violation, 2 = anything that prevents an honest
// verdict (and must never read as a pass).
func RunCLI(args []string, stdout, stderr io.Writer) int {
	command := ""
	configPath := "pawl.yaml"
	format := "text"
	since := ""
	limit := 20
	limitSet := false
	only := ""
	onlyProvided := false
	dryRun := false
	acceptWorse := false
	versionRequested := false
	helpRequested := false
	var positional []string
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "-c" || args[i] == "--config":
			if i+1 >= len(args) {
				fmt.Fprintf(stderr, "%s requires a path argument\n", args[i])
				return 2
			}
			i++
			configPath = args[i]
		case args[i] == "--limit":
			if i+1 >= len(args) {
				fmt.Fprintf(stderr, "--limit requires a non-negative integer\n")
				return 2
			}
			i++
			n, err := strconv.Atoi(args[i])
			if err != nil || n < 0 {
				fmt.Fprintf(stderr, "--limit must be a non-negative integer, got %q\n", args[i])
				return 2
			}
			limit = n
			limitSet = true
		case args[i] == "--only":
			if i+1 >= len(args) {
				fmt.Fprintf(stderr, "--only requires a comma-separated list of dimension ids\n")
				return 2
			}
			i++
			only = args[i]
			onlyProvided = true
		case args[i] == "--dry-run":
			dryRun = true
		case args[i] == "--accept-worse":
			acceptWorse = true
		case args[i] == "--format":
			if i+1 >= len(args) {
				fmt.Fprintf(stderr, "--format requires a value (text|json|codeclimate)\n")
				return 2
			}
			i++
			format = args[i]
		case args[i] == "--since":
			if i+1 >= len(args) {
				fmt.Fprintf(stderr, "--since requires a git ref\n")
				return 2
			}
			i++
			since = args[i]
		case args[i] == "--version":
			versionRequested = true
		case args[i] == "-h" || args[i] == "--help":
			helpRequested = true
		case strings.HasPrefix(args[i], "-"):
			fmt.Fprintf(stderr, "unknown flag %q\n", args[i])
			return 2
		default:
			positional = append(positional, args[i])
		}
	}
	if format != "text" && format != "json" && format != "codeclimate" {
		fmt.Fprintf(stderr, "--format must be text, json or codeclimate, got %q\n", format)
		return 2
	}
	if len(positional) > 0 {
		// An explicit positional is taken verbatim — an empty string is an
		// unknown command, not "no command", so a wrapper running
		// `pawl "$PAWL_COMMAND"` with the variable unset fails loud instead
		// of silently running the default gate.
		command = positional[0]
	} else if helpRequested {
		command = "help"
	} else if versionRequested {
		// `pawl --version` is the version command, not an implicit check —
		// otherwise the default would make check-scoped flags (--since) look
		// valid on a version print.
		command = "version"
	} else {
		command = "check"
	}
	// An unknown command is reported first — even alongside --version, so
	// `pawl frobnicate --version` is the usage error the contract promises,
	// never laundered into a clean version print.
	switch command {
	case "init", "record", "check", "diff", "baseline-guard", "trend", "status", "constraints", "rank", "version", "help":
	default:
		fmt.Fprintf(stderr, "unknown command %q. use: init | record | check | diff | baseline-guard <ref> | trend [<id>] | status | constraints | rank | version | help\n", command)
		return 2
	}
	// Commands have a fixed operand arity; an extra operand is a usage error,
	// so a mistyped invocation (`pawl record only x` — the dashes of --only
	// forgotten) fails loud instead of silently running a different,
	// state-writing command.
	maxOperands := 0
	if command == "trend" || command == "baseline-guard" || command == "help" {
		maxOperands = 1
	}
	if len(positional) > 1+maxOperands {
		fmt.Fprintf(stderr, "unexpected argument %q — `%s` takes at most %d positional argument(s)\n",
			positional[1+maxOperands], command, maxOperands)
		return 2
	}
	// Command-scoped flags are rejected on any other command — including
	// version, so these guards run before the version short-circuit and e.g.
	// `pawl version --limit 1` is the usage error the contract promises
	// rather than a silent version print.
	if onlyProvided && command != "record" && command != "check" && command != "diff" {
		fmt.Fprintf(stderr, "--only is only valid on `record`, `check`, or `diff`, not %q\n", command)
		return 2
	}
	if dryRun && command != "record" {
		fmt.Fprintf(stderr, "--dry-run is only valid on `record`, not %q\n", command)
		return 2
	}
	if acceptWorse && command != "record" {
		fmt.Fprintf(stderr, "--accept-worse is only valid on `record`, not %q\n", command)
		return 2
	}
	if since != "" && command != "check" {
		fmt.Fprintf(stderr, "--since is only valid on `check`, not %q\n", command)
		return 2
	}
	if limitSet && command != "trend" {
		fmt.Fprintf(stderr, "--limit is only valid on `trend`, not %q\n", command)
		return 2
	}
	if (command == "trend" || command == "status" || command == "constraints" || command == "rank") && format == "codeclimate" {
		fmt.Fprintf(stderr, "--format codeclimate is not valid on `%s` (use text or json)\n", command)
		return 2
	}
	if (command == "help" || helpRequested) && format != "text" {
		fmt.Fprintln(stderr, "--format is not valid on `help`")
		return 2
	}
	if helpRequested || command == "help" {
		topic := command
		if command == "help" {
			topic = ""
			if len(positional) > 1 {
				topic = positional[1]
			}
		}
		if !validHelpTopic(topic) {
			fmt.Fprintf(stderr, "unknown help topic %q\n", topic)
			return 2
		}
		printHelp(stdout, topic)
		return 0
	}
	// version never reads config — it must work in any directory. A --version
	// riding on a valid, validly-flagged command (`pawl check --version`) also
	// wins here; every usage error above outranks the version print.
	if versionRequested || command == "version" {
		fmt.Fprintf(stdout, "pawl %s\n", Version)
		return 0
	}

	// trend never measures — it reads config only for the snapshot path, so a
	// temporarily-invalid measurement config (a bad adapter, zero dimensions)
	// must not block viewing local history.
	if command == "trend" {
		cfg, err := LoadConfigLite(configPath)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 2
		}
		metricID := ""
		if len(positional) > 1 {
			metricID = positional[1]
		}
		return runTrend(cfg, metricID, limit, format, stdout, stderr)
	}

	// init writes a new config; it must not require (or read) an existing one.
	if command == "init" {
		return runInit(configPath, stdout, stderr)
	}

	cfg, err := LoadConfig(configPath)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}

	if command == "baseline-guard" {
		ref := ""
		if len(positional) > 1 {
			ref = positional[1]
		}
		return runBaselineGuard(cfg, ref, stdout, stderr)
	}
	if command == "record" && onlyProvided {
		ids := parseOnly(only)
		if len(ids) == 0 {
			fmt.Fprintf(stderr, "--only requires at least one dimension id\n")
			return 2
		}
		return runRecordOnly(cfg, ids, format, dryRun, acceptWorse, stdout, stderr)
	}
	if command == "status" {
		return runStatus(cfg, format, stdout, stderr)
	}
	if command == "constraints" {
		return runConstraints(cfg, format, stdout, stderr)
	}
	if command == "rank" {
		return runRank(cfg, format, stdout, stderr)
	}
	measureCfg := cfg
	if (command == "check" || command == "diff") && onlyProvided {
		onlySet, code := resolveOnlyIDs(cfg, only, command, stderr)
		if code != 0 {
			return code
		}
		if format == "codeclimate" {
			fmt.Fprintf(stderr, "%s --only cannot emit codeclimate: a partial measurement is not a complete current findings report\n", command)
			return 2
		}
		measureCfg = configWithOnly(cfg, onlySet)
	}
	return runMeasureCommand(cfg, measureCfg, command, format, since, dryRun, acceptWorse, stdout, stderr)
}

func runMeasureCommand(full, measure *Config, command, format, since string, dryRun, acceptWorse bool, stdout, stderr io.Writer) int {
	baseline, parsedBaseline, err := ReadSnapshotFile(full.SnapshotPath)
	if err != nil {
		return abortCouldNotMeasure(command, format, err.Error(), nil, stdout, stderr)
	}
	if command != "record" {
		if baseline == nil {
			return abortCouldNotMeasure(command, format,
				fmt.Sprintf("no %s yet — run `pawl record` first.", full.SnapshotPath),
				nil, stdout, stderr)
		}
		if shapeErrors := SnapshotShapeErrors(parsedBaseline); len(shapeErrors) > 0 {
			msg := full.SnapshotPath + " has an invalid shape:\n"
			for _, e := range shapeErrors {
				msg += "  • " + e + "\n"
			}
			return abortCouldNotMeasure(command, format, strings.TrimSuffix(msg, "\n"), nil, stdout, stderr)
		}
		ids := make([]string, 0, len(full.Dimensions))
		for _, d := range full.Dimensions {
			ids = append(ids, d.ID)
		}
		if orphans := OrphanedMetrics(ids, baseline.Metrics); len(orphans) > 0 {
			return abortCouldNotMeasure(command, format,
				fmt.Sprintf("orphaned metric(s) in %s — deleting a dimension must also drop it from the snapshot (re-run `pawl record`): %s",
					full.SnapshotPath, strings.Join(orphans, ", ")),
				nil, stdout, stderr)
		}
	}

	current, err := MeasureAll(measure, stderr)
	if err != nil {
		return abortCouldNotMeasure(command, format, err.Error(), failedMetricIDs(err), stdout, stderr)
	}

	if command == "record" {
		return finishRecord(full, format, baseline, current, dryRun, acceptWorse, stdout, stderr)
	}

	// check / diff. The report is the machine-readable and diff-scoped source of
	// truth; the legacy text path stays the byte-for-byte human default.
	rep := buildReport(command, measure, baseline, current)
	var scope *sinceScope
	if since != "" {
		s, err := applySinceScope(full, rep, baseline, current, since)
		if err != nil {
			return abortCouldNotMeasure(command, format, err.Error(), nil, stdout, stderr)
		}
		scope = s
	}
	exit := 0
	if command == "check" && hasLiveRegression(rep) {
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
	if format == "codeclimate" {
		// Findings mode: emit the current offenders regardless of the gate
		// verdict, but keep the verdict's exit code so the gate still fails CI.
		if err := renderCodeClimate(stdout, measure, current); err != nil {
			fmt.Fprintln(stderr, err)
			return 2
		}
		return exit
	}
	if since != "" {
		renderSinceText(stdout, rep, scope)
		return exit
	}
	return renderCheckTextLegacy(measure, command, baseline, current, stdout)
}

// renderCheckTextLegacy is the byte-for-byte human default output for
// non-diff-scoped check/diff: the table, the regression block, the improvement
// hint, and (for check under CI) the GitHub annotations and notice.
func renderCheckTextLegacy(cfg *Config, command string, baseline *Snapshot, current map[string]Metric, stdout io.Writer) int {
	type regression struct {
		dim    Dimension
		detail []string
	}
	var regressions []regression
	regressedIDs := map[string]bool{}
	var improved []string
	for _, dim := range cfg.Dimensions {
		base, ok := baseline.Metrics[dim.ID]
		if !ok {
			continue // a brand-new dimension has no baseline to regress against
		}
		cur, ok := current[dim.ID]
		if !ok {
			continue
		}
		detail := RegressionsOf(dim.GateSpecOf(),
			MetricSample{Value: base.Value, Breakdown: base.Breakdown},
			MetricSample{Value: cur.Value, Breakdown: cur.Breakdown})
		if len(detail) > 0 {
			regressions = append(regressions, regression{dim: dim, detail: detail})
			regressedIDs[dim.ID] = true
		}
		if Better(dim.Direction, base.Value, cur.Value) {
			improved = append(improved, dim.ID)
		}
	}

	printTable(stdout, cfg, baseline, current, regressedIDs)

	if len(regressions) > 0 {
		fmt.Fprintln(stdout, "❌ regressions:")
		for _, r := range regressions {
			fmt.Fprintf(stdout, "  • %s (%s)\n", r.dim.ID, r.dim.Title)
			for _, line := range r.detail {
				fmt.Fprintf(stdout, "      %s\n", line)
			}
		}
	}
	if len(improved) > 0 {
		fmt.Fprintf(stdout, "🎉 improved: %s\n", strings.Join(improved, ", "))
		fmt.Fprintf(stdout, "   run `%s` to lock in the gains.\n", recordOnlyCommand(improved))
	}
	if command == "check" {
		if onCI() {
			for _, r := range regressions {
				base := baseline.Metrics[r.dim.ID]
				cur := current[r.dim.ID]
				for _, line := range GitHubAnnotations(r.dim.ID, r.dim.Title, r.dim.GateSpecOf(),
					MetricSample{Value: base.Value, Breakdown: base.Breakdown},
					MetricSample{Value: cur.Value, Breakdown: cur.Breakdown}) {
					fmt.Fprintln(stdout, line)
				}
			}
		}
		if notice := ImprovementNotice(improved, onCI()); notice != "" {
			fmt.Fprintln(stdout, notice)
		}
		if len(regressions) > 0 {
			return 1
		}
	}
	return 0
}

func printTable(w io.Writer, cfg *Config, baseline *Snapshot, current map[string]Metric, regressedIDs map[string]bool) {
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
		var base *float64
		if baseline != nil {
			if m, ok := baseline.Metrics[dim.ID]; ok {
				v := m.Value
				base = &v
			}
		}
		cur := current[dim.ID].Value
		tolerance := 0.0
		if dim.Tolerance != nil {
			tolerance = *dim.Tolerance
		}
		status := statusOf(dim.Direction, base, cur, tolerance)
		// A per-file/per-key regression can leave the scalar unchanged — the
		// gate's verdict overrides the scalar-only status.
		if regressedIDs[dim.ID] {
			status = "❌ worse"
		}
		fmt.Fprintf(w, "%s  %9s  %9s  %6s  %s\n",
			pad(dim.ID, idWidth), baseCell(base), FormatNumber(cur), fmtDelta(base, cur), status)
	}
	fmt.Fprintln(w, "")
}

func statusOf(direction Direction, base *float64, cur, tolerance float64) string {
	if base == nil {
		return "🆕 new"
	}
	if Worse(direction, *base, cur, tolerance) {
		return "❌ worse"
	}
	// Strictly worse but inside the declared slack — the gate passes, and the
	// table must not print a "worse" the exit code contradicts.
	if Worse(direction, *base, cur, 0) {
		return "✅ within tolerance"
	}
	if Better(direction, *base, cur) {
		return "🎉 better"
	}
	return "✅ same"
}

func fmtDelta(base *float64, cur float64) string {
	if base == nil {
		return "new"
	}
	d := round2(cur - *base)
	if d == 0 {
		return "±0"
	}
	if d > 0 {
		return "+" + FormatNumber(d)
	}
	return FormatNumber(d)
}

func baseCell(base *float64) string {
	if base == nil {
		return "—"
	}
	return FormatNumber(*base)
}

func pad(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-len(s))
}

func displayPath(path string) string {
	if cwd, err := os.Getwd(); err == nil {
		if rel, err := filepath.Rel(cwd, path); err == nil && !strings.HasPrefix(rel, "..") {
			return rel
		}
	}
	return path
}
