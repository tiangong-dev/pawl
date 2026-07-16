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
	versionRequested := false
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
	case "init", "record", "check", "diff", "baseline-guard", "trend", "version":
	default:
		fmt.Fprintf(stderr, "unknown command %q. use: init | record | check | diff | baseline-guard <ref> | trend [<id>] | version\n", command)
		return 2
	}
	// Commands have a fixed operand arity; an extra operand is a usage error,
	// so a mistyped invocation (`pawl record only x` — the dashes of --only
	// forgotten) fails loud instead of silently running a different,
	// state-writing command.
	maxOperands := 0
	if command == "trend" || command == "baseline-guard" {
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
	if onlyProvided && command != "record" {
		fmt.Fprintf(stderr, "--only is only valid on `record`, not %q\n", command)
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
	if command == "trend" && format == "codeclimate" {
		fmt.Fprintf(stderr, "--format codeclimate is not valid on `trend` (use text or json)\n")
		return 2
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
		return runRecordOnly(cfg, ids, format, stdout, stderr)
	}
	return runMeasureCommand(cfg, command, format, since, stdout, stderr)
}

func runMeasureCommand(cfg *Config, command, format, since string, stdout, stderr io.Writer) int {
	baseline, parsedBaseline, err := ReadSnapshotFile(cfg.SnapshotPath)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	if command != "record" {
		if baseline == nil {
			fmt.Fprintf(stderr, "no %s yet — run `pawl record` first.\n", cfg.SnapshotPath)
			return 2
		}
		if shapeErrors := SnapshotShapeErrors(parsedBaseline); len(shapeErrors) > 0 {
			fmt.Fprintf(stderr, "%s has an invalid shape:\n", cfg.SnapshotPath)
			for _, e := range shapeErrors {
				fmt.Fprintf(stderr, "  • %s\n", e)
			}
			return 2
		}
		ids := make([]string, 0, len(cfg.Dimensions))
		for _, d := range cfg.Dimensions {
			ids = append(ids, d.ID)
		}
		if orphans := OrphanedMetrics(ids, baseline.Metrics); len(orphans) > 0 {
			fmt.Fprintf(stderr, "orphaned metric(s) in %s — deleting a dimension must also drop it from the snapshot (re-run `pawl record`): %s\n",
				cfg.SnapshotPath, strings.Join(orphans, ", "))
			return 2
		}
	}

	current, err := MeasureAll(cfg, stderr)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}

	if command == "record" {
		return finishRecord(cfg, format, baseline, current, stdout, stderr)
	}

	// check / diff. The report is the machine-readable and diff-scoped source of
	// truth; the legacy text path stays the byte-for-byte human default.
	rep := buildReport(command, cfg, baseline, current)
	var scope *sinceScope
	if since != "" {
		s, code := applySinceScope(cfg, rep, baseline, current, since, stderr)
		if code != 0 {
			return code
		}
		scope = s
	}
	exit := 0
	if command == "check" && hasLiveRegression(rep) {
		exit = 1
	}
	rep.ExitCode = exit

	if format == "json" {
		if err := renderReportJSON(stdout, rep); err != nil {
			fmt.Fprintln(stderr, err)
			return 2
		}
		return exit
	}
	if format == "codeclimate" {
		// Findings mode: emit the current offenders regardless of the gate
		// verdict, but keep the verdict's exit code so the gate still fails CI.
		if err := renderCodeClimate(stdout, cfg, current); err != nil {
			fmt.Fprintln(stderr, err)
			return 2
		}
		return exit
	}
	if since != "" {
		renderSinceText(stdout, rep, scope)
		return exit
	}
	return renderCheckTextLegacy(cfg, command, baseline, current, stdout)
}

// finishRecord writes the snapshot, then prints either the human table + 📸 line
// or, under --format json, the verdict object. The snapshot write happens BEFORE
// any stdout so a write failure exits 2 without having already emitted an
// `exit_code: 0` verdict it can't take back.
func finishRecord(cfg *Config, format string, baseline *Snapshot, current map[string]Metric, stdout, stderr io.Writer) int {
	if err := WriteSnapshotFile(cfg.SnapshotPath, current); err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	if format == "json" {
		rep := buildReport("record", cfg, baseline, current)
		rep.ExitCode = 0
		if err := renderReportJSON(stdout, rep); err != nil {
			fmt.Fprintln(stderr, err)
			return 2
		}
		return 0
	}
	if format == "codeclimate" {
		if err := renderCodeClimate(stdout, cfg, current); err != nil {
			fmt.Fprintln(stderr, err)
			return 2
		}
		return 0
	}
	printTable(stdout, cfg, baseline, current, nil)
	fmt.Fprintf(stdout, "📸 snapshot written to %s\n", displayPath(cfg.SnapshotPath))
	return 0
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
		fmt.Fprintln(stdout, "   run `pawl record` to lock in the gains.")
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

func round2(v float64) float64 {
	scaled := v * 100
	if scaled >= 0 {
		scaled = float64(int64(scaled + 0.5))
	} else {
		scaled = float64(int64(scaled - 0.5))
	}
	return scaled / 100
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

func onCI() bool {
	return os.Getenv("GITHUB_ACTIONS") != ""
}
