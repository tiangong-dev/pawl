package pawl

import (
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"
)

// runTrend reconstructs each metric's value over time from the committed
// snapshot file's own git history — a fully local trend, no cloud. It never
// measures: it walks git log for the snapshot path, parses the snapshot at each
// commit that touched it, and prints the series. See SPEC.md § Trend.
func runTrend(cfg *Config, metricID string, limit int, format string, stdout, stderr io.Writer) int {
	// Meta notes (skip warnings, the truncation line) go to stdout in text mode
	// but stderr in json mode, so `--format json` stdout stays pure JSON.
	metaW := stdout
	if format == "json" {
		metaW = stderr
	}

	relPath, code := trendSnapshotRelPath(cfg, stderr)
	if code != 0 {
		return code
	}

	logOut, code, gitErr := gitOutput(cfg.Dir, "log", "--format=%H%x09%cI", "--", relPath)
	if code != 0 {
		fmt.Fprintf(stderr, "trend: git log for %s failed: %s\n", relPath, gitErr)
		return 2
	}
	logOut = strings.TrimSpace(logOut)
	if logOut == "" {
		fmt.Fprintf(stderr, "trend: no committed history for %s — commit the snapshot first.\n", relPath)
		return 2
	}

	// Newest-first, as git log emits them.
	type commitRec struct{ sha, date string }
	var commits []commitRec
	for _, line := range strings.Split(logOut, "\n") {
		sha, iso, found := strings.Cut(line, "\t")
		if !found {
			continue
		}
		commits = append(commits, commitRec{sha: sha, date: iso})
	}

	total := len(commits)
	if limit > 0 && total > limit {
		commits = commits[:limit] // keep the `limit` most recent
		fmt.Fprintf(metaW, "showing %d of %d snapshots (--limit 0 for all)\n", limit, total)
	}

	type point struct {
		commit, date string
		value        float64
	}
	series := map[string][]point{}    // id -> points, newest-first until reversed
	latestMeta := map[string]Metric{} // id -> most recent Metric (for direction/unit)

	for _, c := range commits {
		data, code, _ := gitOutput(cfg.Dir, "show", c.sha+":"+relPath)
		if code != 0 {
			continue // git log said it touched the path; a show failure is not fatal
		}
		snap, _, err := ParseSnapshot([]byte(data))
		if err != nil {
			note := fmt.Sprintf("trend: skipping %s — its %s is not valid JSON", shortSHA(c.sha), relPath)
			if onCI() {
				fmt.Fprintf(metaW, "::warning::%s\n", note)
			} else {
				fmt.Fprintf(metaW, "⚠️  %s\n", note)
			}
			continue
		}
		for id, m := range snap.Metrics {
			if metricID != "" && id != metricID {
				continue
			}
			if _, seen := latestMeta[id]; !seen {
				latestMeta[id] = m // first sighting walking newest-first = most recent
			}
			series[id] = append(series[id], point{commit: c.sha, date: c.date, value: m.Value})
		}
	}

	if metricID != "" && len(series[metricID]) == 0 {
		fmt.Fprintf(stderr, "trend: no metric %q in the snapshot history.\n", metricID)
		return 2
	}
	if len(series) == 0 {
		fmt.Fprintf(stderr, "trend: no parseable metrics in the snapshot history of %s.\n", relPath)
		return 2
	}

	ids := make([]string, 0, len(series))
	for id := range series {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	rep := trendReport{SchemaVersion: 1, Command: "trend", Snapshot: relPath}
	for _, id := range ids {
		pts := series[id]
		// series is newest-first; the output is oldest → newest.
		points := make([]trendPoint, 0, len(pts))
		for i := len(pts) - 1; i >= 0; i-- {
			points = append(points, trendPoint{Commit: pts[i].commit, Date: pts[i].date, Value: pts[i].value})
		}
		rep.Metrics = append(rep.Metrics, trendMetric{
			ID:        id,
			Direction: latestMeta[id].Direction,
			Unit:      latestMeta[id].Unit,
			Points:    points,
		})
	}

	if format == "json" {
		data, err := json.MarshalIndent(rep, "", "  ")
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 2
		}
		fmt.Fprintf(stdout, "%s\n", data)
		return 0
	}
	renderTrendText(stdout, rep.Metrics)
	return 0
}

// trendSnapshotRelPath resolves the snapshot's repo-relative, slash-separated
// path — the same two checks baseline-guard makes before touching git history.
func trendSnapshotRelPath(cfg *Config, stderr io.Writer) (string, int) {
	toplevel, code, gitErr := gitOutput(cfg.Dir, "rev-parse", "--show-toplevel")
	if code != 0 {
		fmt.Fprintf(stderr, "trend: %s is not inside a git repository: %s\n", cfg.Dir, gitErr)
		return "", 2
	}
	relPath, err := filepath.Rel(toplevel, cfg.SnapshotPath)
	if err != nil || strings.HasPrefix(relPath, "..") {
		fmt.Fprintf(stderr, "trend: snapshot %s is outside the git repository %s\n", cfg.SnapshotPath, toplevel)
		return "", 2
	}
	return filepath.ToSlash(relPath), 0
}

type trendPoint struct {
	Commit string  `json:"commit"`
	Date   string  `json:"date"`
	Value  float64 `json:"value"`
}

type trendMetric struct {
	ID        string       `json:"id"`
	Direction Direction    `json:"direction"`
	Unit      string       `json:"unit"`
	Points    []trendPoint `json:"points"`
}

type trendReport struct {
	SchemaVersion int           `json:"schema_version"`
	Command       string        `json:"command"`
	Snapshot      string        `json:"snapshot"`
	Metrics       []trendMetric `json:"metrics"`
}

// renderTrendText prints, per metric, a header and one row per point oldest →
// newest: short sha, date (YYYY-MM-DD), value, and the Δ from the previous point.
func renderTrendText(w io.Writer, metrics []trendMetric) {
	for i, tm := range metrics {
		if i > 0 {
			fmt.Fprintln(w)
		}
		fmt.Fprintf(w, "%s  (%s, %s)\n", tm.ID, tm.Direction, tm.Unit)
		var prev *float64
		for _, p := range tm.Points {
			fmt.Fprintf(w, "  %s  %s  %s  %s\n", shortSHA(p.Commit), dateOnly(p.Date), FormatNumber(p.Value), trendDelta(prev, p.Value))
			v := p.Value
			prev = &v
		}
	}
}

// trendDelta is the per-point change: "—" for the first (oldest) point, else the
// signed change from the previous point (`±0` when unchanged).
func trendDelta(prev *float64, cur float64) string {
	if prev == nil {
		return "—"
	}
	d := round2(cur - *prev)
	if d == 0 {
		return "±0"
	}
	if d > 0 {
		return "+" + FormatNumber(d)
	}
	return FormatNumber(d)
}

func shortSHA(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

// dateOnly trims an ISO-8601 timestamp to its YYYY-MM-DD date for the text table
// (the JSON output keeps the full timestamp).
func dateOnly(iso string) string {
	if len(iso) >= 10 {
		return iso[:10]
	}
	return iso
}
