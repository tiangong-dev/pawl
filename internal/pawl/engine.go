package pawl

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"sort"
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
func RunCLI(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
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
	currentPath := ""
	writeTarget := ""
	writeProvided := false
	quiet := false
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
		case args[i] == "--write":
			// The value is validated here rather than in runAgent so a typo is a usage error even when it rides on the wrong command.
			if i+1 >= len(args) {
				fmt.Fprintf(stderr, "--write requires a target: %s\n", strings.Join(agentTargetNames(), " or "))
				return 2
			}
			i++
			writeTarget = args[i]
			writeProvided = true
			if _, ok := lookupAgentTarget(writeTarget); !ok {
				fmt.Fprintf(stderr, "--write must be %s, got %q\n", strings.Join(agentTargetNames(), " or "), writeTarget)
				return 2
			}
		case args[i] == "--dry-run":
			dryRun = true
		case args[i] == "--accept-worse":
			acceptWorse = true
		case args[i] == "-q" || args[i] == "--quiet":
			quiet = true
		case args[i] == "--current":
			if i+1 >= len(args) {
				fmt.Fprintf(stderr, "--current requires a path to a measurement document, or - for stdin\n")
				return 2
			}
			i++
			currentPath = args[i]
		case args[i] == "--format":
			if i+1 >= len(args) {
				fmt.Fprintf(stderr, "--format requires a value (text|json)\n")
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
	if format != "text" && format != "json" {
		fmt.Fprintf(stderr, "--format must be text or json, got %q\n", format)
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
	case "init", "agent", "measure", "record", "check", "guard", "trend", "rank", "version", "help":
	default:
		fmt.Fprintf(stderr, "unknown command %q. use: init | agent | measure | record | check | guard <ref> | trend [<id>] | rank | version | help\n", command)
		return 2
	}
	// Commands have a fixed operand arity; an extra operand is a usage error,
	// so a mistyped invocation (`pawl record only x` — the dashes of --only
	// forgotten) fails loud instead of silently running a different,
	// state-writing command.
	maxOperands := 0
	if command == "trend" || command == "guard" || command == "help" {
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
	if onlyProvided && command != "record" && command != "check" && command != "measure" {
		fmt.Fprintf(stderr, "--only is only valid on `record`, `check` or `measure`, not %q\n", command)
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
	if quiet && command != "measure" && command != "record" && command != "check" {
		fmt.Fprintf(stderr, "--quiet is only valid on `measure`, `record` or `check`, not %q\n", command)
		return 2
	}
	if currentPath != "" && command != "record" && command != "check" {
		fmt.Fprintf(stderr, "--current is only valid on `record` or `check`, not %q\n", command)
		return 2
	}
	if writeProvided && command != "agent" {
		fmt.Fprintf(stderr, "--write is only valid on `agent`, not %q\n", command)
		return 2
	}
	if command == "agent" && format != "text" {
		fmt.Fprintln(stderr, "--format is not valid on `agent` — it emits Markdown")
		return 2
	}
	if command == "measure" && format != "text" {
		fmt.Fprintln(stderr, "--format is not valid on `measure` — it emits the measurement document")
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

	// agent emits a fixed operating loop, identical in every repo — it reads no config, so it still works in a repo whose config is mid-edit or broken, which is exactly when someone reaches for the instructions.
	if command == "agent" {
		return runAgent(writeTarget, stdin, stdout, stderr)
	}

	cfg, err := LoadConfig(configPath)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}

	if command == "guard" {
		ref := ""
		if len(positional) > 1 {
			ref = positional[1]
		}
		return runGuard(cfg, ref, stdout, stderr)
	}
	// A supplied measurement is read once, before anything runs, so a malformed
	// document fails before a snapshot is read or a dimension is executed.
	var supplied map[string]Metric
	if currentPath != "" {
		m, err := readMeasurement(currentPath, stdin)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 2
		}
		supplied = m
	}
	// --quiet silences pawl's own progress and advisory output. An adapter's
	// stderr is untouched: the whole point of a quiet run is that the noise goes
	// away, not the diagnosis of the tool that is about to fail.
	progress := stderr
	if quiet {
		progress = io.Discard
	}
	if command == "measure" {
		measureCfg := cfg
		if onlyProvided {
			onlySet, _, code := resolveOnlyIDs(cfg, only, command, stderr)
			if code != 0 {
				return code
			}
			measureCfg = configWithOnly(cfg, onlySet)
		}
		return runMeasure(measureCfg, progress, stdout, stderr)
	}
	if command == "record" && onlyProvided {
		onlySet, onlyIDs, code := resolveOnlyIDs(cfg, only, "record", stderr)
		if code != 0 {
			return code
		}
		out, flush := quietStdout(quiet, format, stdout)
		return flush(runRecordOnly(cfg, onlySet, onlyIDs, format, dryRun, acceptWorse, supplied, progress, out, stderr))
	}
	if command == "rank" {
		return runRank(cfg, format, stdout, stderr)
	}
	measureCfg := cfg
	var onlyIDs []string
	if command == "check" && onlyProvided {
		onlySet, ids, code := resolveOnlyIDs(cfg, only, command, stderr)
		if code != 0 {
			return code
		}
		onlyIDs = ids
		measureCfg = configWithOnly(cfg, onlySet)
	}
	out, flush := quietStdout(quiet, format, stdout)
	code := runMeasureCommand(cfg, measureCfg, command, format, since, onlyIDs, dryRun, acceptWorse, supplied, progress, out, stderr, quiet)
	return flush(code)
}

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

func runMeasureCommand(full, measure *Config, command, format, since string, onlyIDs []string, dryRun, acceptWorse bool, supplied map[string]Metric, progress, stdout, stderr io.Writer, quiet bool) int {
	excluded := excludedDimensionIDs(full, onlyIDs)
	runScope := reportScope{command: command, since: since, only: onlyIDs, excluded: excluded}
	baseline, parsedBaseline, err := ReadSnapshotFile(full.SnapshotPath)
	if err != nil {
		return abortCouldNotMeasure(runScope, format, err.Error(), nil, stdout, stderr)
	}
	if command != "record" {
		if baseline == nil {
			return abortCouldNotMeasure(runScope, format,
				fmt.Sprintf("no %s yet — run `pawl record` first.", full.SnapshotPath),
				nil, stdout, stderr)
		}
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
	var scope *sinceScope
	if since != "" {
		s, err := applySinceScope(full, rep, baseline, current, since)
		if err != nil {
			return abortCouldNotMeasure(runScope, format, err.Error(), nil, stdout, stderr)
		}
		scope = s
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
