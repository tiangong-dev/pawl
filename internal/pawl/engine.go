package pawl

import (
	"fmt"
	"io"
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
	configPath := defaultConfigName
	configExplicit := false
	format, formatProvided := "text", false
	since := ""
	baseRef := ""
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
			configExplicit = true
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
			format, formatProvided = args[i], true
		case args[i] == "--since":
			if i+1 >= len(args) {
				fmt.Fprintf(stderr, "--since requires a git ref\n")
				return 2
			}
			i++
			since = args[i]
		case args[i] == "--base":
			if i+1 >= len(args) || args[i+1] == "" {
				fmt.Fprintln(stderr, "--base requires a git ref")
				return 2
			}
			i++
			baseRef = args[i]
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
	case "init", "agent", "measure", "record", "check", "guard", "trend", "rank", "validate", "version", "help":
	default:
		fmt.Fprintf(stderr, "unknown command %q. use: init | agent | measure | record | check | guard <ref> | trend [<id>] | rank | validate | version | help\n", command)
		return 2
	}
	if format != "text" && format != "json" {
		if format == "codeclimate" {
			fmt.Fprintln(stderr, "--format codeclimate was removed in pawl 0.7; use --format json and consume the stable verdict schema")
			return 2
		}
		fmt.Fprintf(stderr, "--format must be text or json, got %q\n", format)
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
	// -q neither silences nor buffers validate: that command prints no progress,
	// and its report is its result, so there is nothing to hold back.
	if quiet && command != "measure" && command != "record" && command != "check" && command != "validate" {
		fmt.Fprintf(stderr, "--quiet is only valid on `measure`, `record`, `check` or `validate`, not %q\n", command)
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
	if command == "measure" && formatProvided && format != "json" {
		fmt.Fprintln(stderr, "--format is not valid on `measure` — it emits the measurement document")
		return 2
	}
	if since != "" && command != "check" {
		fmt.Fprintf(stderr, "--since is only valid on `check`, not %q\n", command)
		return 2
	}
	if baseRef != "" && command != "check" {
		fmt.Fprintf(stderr, "--base is only valid on `check`, not %q\n", command)
		return 2
	}
	if baseRef != "" && since != "" {
		fmt.Fprintln(stderr, "--base cannot be combined with --since")
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

	// init writes a new config; it must not require (or read) an existing one, and it never searches — scaffolding in a subdirectory must not clobber an upper config.
	if command == "init" {
		return runInit(configPath, stdout, stderr)
	}

	// agent emits a fixed operating loop, identical in every repo — it reads no config, so it still works in a repo whose config is mid-edit or broken, which is exactly when someone reaches for the instructions.
	if command == "agent" {
		return runAgent(writeTarget, stdin, stdout, stderr)
	}

	// --quiet silences pawl's own progress and advisory output. An adapter's
	// stderr is untouched: the whole point of a quiet run is that the noise goes
	// away, not the diagnosis of the tool that is about to fail.
	progress := stderr
	if quiet {
		progress = io.Discard
	}

	// Every command below reads a config. An explicit -c is used verbatim;
	// without one, resolveConfigPath adopts the nearest pawl.yaml upward.
	configPath, err := resolveConfigPath(configPath, configExplicit, progress)
	if err != nil {
		return reportConfigResolutionError(command, format, err, stdout, stderr)
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

	// validate reads the config and nothing else — no measurement, no snapshot,
	// no git — and is the one command where a stray key is fatal rather than an
	// advisory warning.
	if command == "validate" {
		return runValidate(configPath, format, stdout, stderr)
	}

	// Config warnings go to stderr directly, not to the quietable progress
	// writer: a misspelled key must never be silenced by -q. --format json is
	// unaffected because only stdout carries the verdict document.
	cfg, err := LoadConfig(configPath, stderr)
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
			if command == "check" && baseRef != "" {
				scope := reportScope{command: command, baseRef: baseRef}
				if onlyProvided {
					_, ids, code := resolveOnlyIDs(cfg, only, command, stderr)
					if code != 0 {
						return code
					}
					scope.only = ids
					scope.excluded = excludedDimensionIDs(cfg, ids)
				}
				return abortCouldNotMeasure(scope, format, err.Error(), nil, stdout, stderr)
			}
			fmt.Fprintln(stderr, err)
			return 2
		}
		supplied = m
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
	code := runMeasureCommand(cfg, measureCfg, command, format, since, baseRef, onlyIDs, dryRun, acceptWorse, supplied, progress, out, stderr, quiet)
	return flush(code)
}
