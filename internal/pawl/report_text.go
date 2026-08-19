package pawl

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// renderCheckTextLegacy is the byte-for-byte human default output for
// non-diff-scoped check/diff: the table, the regression block, the improvement
// hint, and (for check under CI) the GitHub annotations and notice.
func renderCheckTextLegacy(cfg *Config, command string, baseline *Snapshot, current map[string]Metric, excluded []string, stdout io.Writer) int {
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

	if len(excluded) > 0 {
		fmt.Fprintf(stdout, "ℹ️  %d dimension(s) not measured this run (--only scope): %s\n\n",
			len(excluded), strings.Join(excluded, ", "))
	}

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
