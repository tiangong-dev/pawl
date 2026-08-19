package pawl

import (
	"fmt"
	"io"
)

func validHelpTopic(topic string) bool {
	switch topic {
	case "", "init", "agent-md", "measure", "record", "check", "baseline-guard", "trend", "rank", "version":
		return true
	default:
		return false
	}
}

func printHelp(w io.Writer, topic string) {
	if topic == "" {
		fmt.Fprintln(w, `pawl — honest anti-regression quality gates

Usage:
  pawl [check] [flags]
  pawl <command> [flags]
  pawl help [command]

Commands:
  init                    write a starter pawl.yaml
  agent-md                print the agent operating loop for this gate
  measure                 measure every dimension and print the numbers, no verdict
  record                  measure and write the snapshot
  check                   fail when a metric regresses (default)
  baseline-guard [ref]    ensure the snapshot did not regress
  trend [id]              show snapshot history
  rank                    rank files by line or byte size
  version                 print the version

Global flags:
  -c, --config <path>     config file (default pawl.yaml)
      --format <format>   text or json
  -q, --quiet             silence progress and advisory output
  -h, --help              show help
      --version           print the version

Command flags:
  record --only <ids>     re-record only comma-separated dimensions
  check --only <ids>      measure and gate only those dimensions
  measure --only <ids>    measure only those dimensions
  check --current <path>  judge a measure document instead of measuring (- = stdin)
  record --current <path> record a measure document instead of measuring (- = stdin)
  record --dry-run        preview what record would write, without writing
  record --accept-worse   record a dimension even if it comes back worse
  check --since <ref>     scope located findings to the working tree since <ref>
  trend --limit <n>       limit history entries`)
		return
	}
	fmt.Fprintf(w, "Usage: %s\n", map[string]string{
		"init":           "pawl init [-c pawl.yaml]",
		"agent-md":       "pawl agent-md",
		"measure":        "pawl measure [--only <id>[,<id>…]] [-q]",
		"record":         "pawl record [--only <id>[,<id>…]] [--current <path>|-] [--dry-run] [--accept-worse] [-q] [--format text|json]",
		"check":          "pawl check [--since <ref>] [--only <id>[,<id>…]] [--current <path>|-] [-q] [--format text|json]",
		"baseline-guard": "pawl baseline-guard [<ref>]",
		"trend":          "pawl trend [<id>] [--limit <n>] [--format text|json]",
		"rank":           "pawl rank [--format text|json]",
		"version":        "pawl version",
	}[topic])
}
