package pawl

import (
	"fmt"
	"io"
)

func validHelpTopic(topic string) bool {
	switch topic {
	case "", "init", "agent-md", "record", "check", "baseline-guard", "trend", "rank", "version":
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
  record                  measure and write the snapshot
  check                   fail when a metric regresses (default)
  baseline-guard [ref]    ensure the snapshot did not regress
  trend [id]              show snapshot history
  rank                    rank files by line or byte size
  version                 print the version

Global flags:
  -c, --config <path>     config file (default pawl.yaml)
      --format <format>   text or json
  -h, --help              show help
      --version           print the version

Command flags:
  record --only <ids>     re-record only comma-separated dimensions
  check --only <ids>      measure and gate only those dimensions
  agent-md --write        append that loop to AGENTS.md instead of printing it
  record --dry-run        preview what record would write, without writing
  record --accept-worse   record a dimension even if it comes back worse
  check --since <ref>     scope located findings to the working tree since <ref>
  trend --limit <n>       limit history entries`)
		return
	}
	fmt.Fprintf(w, "Usage: %s\n", map[string]string{
		"init":           "pawl init [-c pawl.yaml]",
		"agent-md":       "pawl agent-md [--write]",
		"record":         "pawl record [--only <id>[,<id>…]] [--dry-run] [--accept-worse] [--format text|json]",
		"check":          "pawl check [--since <ref>] [--only <id>[,<id>…]] [--format text|json]",
		"baseline-guard": "pawl baseline-guard [<ref>]",
		"trend":          "pawl trend [<id>] [--limit <n>] [--format text|json]",
		"rank":           "pawl rank [--format text|json]",
		"version":        "pawl version",
	}[topic])
}
