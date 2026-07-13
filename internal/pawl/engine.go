package pawl

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
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
		case args[i] == "--version":
			versionRequested = true
		case strings.HasPrefix(args[i], "-"):
			fmt.Fprintf(stderr, "unknown flag %q\n", args[i])
			return 2
		default:
			positional = append(positional, args[i])
		}
	}
	if len(positional) > 0 {
		command = positional[0]
	}
	if command == "" {
		command = "check"
	}
	// version never reads config — it must work in any directory.
	if versionRequested || command == "version" {
		fmt.Fprintf(stdout, "pawl %s\n", Version)
		return 0
	}
	switch command {
	case "record", "check", "diff", "baseline-guard":
	default:
		fmt.Fprintf(stderr, "unknown command %q. use: record | check | diff | baseline-guard <ref> | version\n", command)
		return 2
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
	return runMeasureCommand(cfg, command, stdout, stderr)
}

func runMeasureCommand(cfg *Config, command string, stdout, stderr io.Writer) int {
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
		printTable(stdout, cfg, baseline, current, nil)
		if err := WriteSnapshotFile(cfg.SnapshotPath, current); err != nil {
			fmt.Fprintln(stderr, err)
			return 2
		}
		fmt.Fprintf(stdout, "📸 snapshot written to %s\n", displayPath(cfg.SnapshotPath))
		return 0
	}

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
