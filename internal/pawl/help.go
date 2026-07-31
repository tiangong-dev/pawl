package pawl

import (
	"fmt"
	"io"
)

func validHelpTopic(topic string) bool {
	switch topic {
	case "", "init", "record", "check", "diff", "baseline-guard", "trend", "version":
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
  record                  measure and write the snapshot
  check                   fail when a metric regresses (default)
  diff                    report changes without failing on regressions
  baseline-guard [ref]    ensure the snapshot did not regress
  trend [id]              show snapshot history
  version                 print the version

Global flags:
  -c, --config <path>     config file (default pawl.yaml)
      --format <format>   text, json or codeclimate
  -h, --help              show help
      --version           print the version

Command flags:
  record --only <ids>     re-record only comma-separated dimensions
  record --dry-run        preview what record would write, without writing
  record --accept-worse   record a dimension even if it comes back worse
  check --since <ref>     scope located findings to added lines
  trend --limit <n>       limit history entries`)
		return
	}
	fmt.Fprintf(w, "Usage: %s\n", map[string]string{
		"init":           "pawl init [-c pawl.yaml]",
		"record":         "pawl record [--only <id>[,<id>…]] [--dry-run] [--accept-worse] [--format text|json|codeclimate]",
		"check":          "pawl check [--since <ref>] [--format text|json|codeclimate]",
		"diff":           "pawl diff [--format text|json|codeclimate]",
		"baseline-guard": "pawl baseline-guard [<ref>]",
		"trend":          "pawl trend [<id>] [--limit <n>] [--format text|json]",
		"version":        "pawl version",
	}[topic])
}
