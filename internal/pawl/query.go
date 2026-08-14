package pawl

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
)

const rankNearRatio = 0.9

// runStatus prints the committed snapshot without measuring — a cheap read
// for agents that need the current baseline before editing.
func runStatus(cfg *Config, format string, stdout, stderr io.Writer) int {
	baseline, parsed, err := ReadSnapshotFile(cfg.SnapshotPath)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	if baseline == nil {
		fmt.Fprintf(stderr, "no %s yet — run `pawl record` first.\n", cfg.SnapshotPath)
		return 2
	}
	if shapeErrors := SnapshotShapeErrors(parsed); len(shapeErrors) > 0 {
		fmt.Fprintf(stderr, "%s has an invalid shape:\n", cfg.SnapshotPath)
		for _, e := range shapeErrors {
			fmt.Fprintf(stderr, "  • %s\n", e)
		}
		return 2
	}

	titleByID := map[string]string{}
	for _, d := range cfg.Dimensions {
		titleByID[d.ID] = d.Title
	}
	ids := sortedMetricKeys(baseline.Metrics)
	type statusMetric struct {
		ID        string    `json:"id"`
		Title     string    `json:"title,omitempty"`
		Direction Direction `json:"direction"`
		Value     float64   `json:"value"`
		Unit      string    `json:"unit"`
	}
	metrics := make([]statusMetric, 0, len(ids))
	for _, id := range ids {
		m := baseline.Metrics[id]
		metrics = append(metrics, statusMetric{
			ID:        id,
			Title:     titleByID[id],
			Direction: m.Direction,
			Value:     m.Value,
			Unit:      m.Unit,
		})
	}
	if format == "json" {
		payload := map[string]any{
			"schema_version": 2,
			"command":        "status",
			"snapshot":       displayPath(cfg.SnapshotPath),
			"metrics":        metrics,
		}
		return writeQueryJSON(stdout, stderr, payload)
	}
	idWidth := 6
	for _, m := range metrics {
		if len(m.ID) > idWidth {
			idWidth = len(m.ID)
		}
	}
	fmt.Fprintln(stdout, "")
	fmt.Fprintf(stdout, "%s  %9s  unit\n", pad("metric", idWidth), "value")
	fmt.Fprintln(stdout, strings.Repeat("-", idWidth+9+8))
	for _, m := range metrics {
		fmt.Fprintf(stdout, "%s  %9s  %s\n", pad(m.ID, idWidth), FormatNumber(m.Value), m.Unit)
	}
	fmt.Fprintln(stdout, "")
	return 0
}

// runConstraints dumps the configured dimensions (thresholds, globs, patterns)
// without measuring — the budget an agent should inject into a subagent prompt.
func runConstraints(cfg *Config, format string, stdout, stderr io.Writer) int {
	type constraint struct {
		ID        string         `json:"id"`
		Title     string         `json:"title"`
		Direction Direction      `json:"direction"`
		Gate      GateMode       `json:"gate"`
		Builtin   string         `json:"builtin,omitempty"`
		Command   string         `json:"command,omitempty"`
		Source    string         `json:"source,omitempty"`
		Options   map[string]any `json:"options,omitempty"`
	}
	out := make([]constraint, 0, len(cfg.Dimensions))
	for _, d := range cfg.Dimensions {
		gate := d.Gate
		if gate == "" {
			gate = GateTotal
		}
		out = append(out, constraint{
			ID:        d.ID,
			Title:     d.Title,
			Direction: d.Direction,
			Gate:      gate,
			Builtin:   d.Builtin,
			Command:   d.Command,
			Source:    d.Source,
			Options:   d.Options,
		})
	}
	if format == "json" {
		payload := map[string]any{
			"schema_version": 2,
			"command":        "constraints",
			"dimensions":     out,
		}
		return writeQueryJSON(stdout, stderr, payload)
	}
	idWidth := 6
	for _, c := range out {
		if len(c.ID) > idWidth {
			idWidth = len(c.ID)
		}
	}
	fmt.Fprintln(stdout, "")
	for _, c := range out {
		src := c.Builtin
		if src == "" {
			src = c.Command
		}
		if c.Source != "" {
			src = "source:" + c.Source
		}
		fmt.Fprintf(stdout, "%s  %s  %s  %s\n", pad(c.ID, idWidth), c.Direction, c.Gate, src)
		if c.Options != nil {
			if th, ok := numberOption(c.Options, "threshold"); ok {
				fmt.Fprintf(stdout, "%s  threshold %s\n", pad("", idWidth), FormatNumber(th))
			}
			if p, _ := c.Options["pattern"].(string); p != "" {
				fmt.Fprintf(stdout, "%s  pattern %s\n", pad("", idWidth), p)
			}
			if inc := stringList(c.Options["include"]); len(inc) > 0 {
				fmt.Fprintf(stdout, "%s  include %s\n", pad("", idWidth), fmt.Sprintf("%v", inc))
			}
		}
	}
	fmt.Fprintln(stdout, "")
	return 0
}

type rankFile struct {
	Path   string  `json:"path"`
	Value  float64 `json:"value"`
	Status string  `json:"status"`
}

type rankDimension struct {
	ID        string     `json:"id"`
	Kind      string     `json:"kind"`
	Threshold int        `json:"threshold"`
	Files     []rankFile `json:"files"`
}

// runRank lists every file matching each file-length / file-bytes dimension,
// including those under the threshold, sorted by size descending. Near-threshold
// files (above 90% of the limit, not yet over) are the ones an agent should
// not grow.
func runRank(cfg *Config, format string, stdout, stderr io.Writer) int {
	var dims []rankDimension
	for _, d := range cfg.Dimensions {
		var (
			kind     string
			fallback int
			sizeOf   func([]byte) int
		)
		switch d.Builtin {
		case builtinFileLength:
			kind, fallback, sizeOf = "lines", defaultFileLengthThreshold, lineCount
		case builtinFileBytes:
			kind, fallback, sizeOf = "bytes", defaultFileBytesThreshold, func(data []byte) int { return len(data) }
		default:
			continue
		}
		ranked, err := rankOne(cfg, d, fallback, sizeOf)
		if err != nil {
			fmt.Fprintf(stderr, "rank %s: %v\n", d.ID, err)
			return 2
		}
		dims = append(dims, rankDimension{
			ID:        d.ID,
			Kind:      kind,
			Threshold: intOption(d.Options, "threshold", fallback),
			Files:     ranked,
		})
	}
	if len(dims) == 0 {
		fmt.Fprintln(stderr, "rank needs a file-length or file-bytes dimension in the config.")
		return 2
	}
	if format == "json" {
		payload := map[string]any{
			"schema_version": 2,
			"command":        "rank",
			"dimensions":     dims,
		}
		return writeQueryJSON(stdout, stderr, payload)
	}
	for _, dim := range dims {
		fmt.Fprintf(stdout, "\n%s  (threshold %d %s)\n", dim.ID, dim.Threshold, dim.Kind)
		for _, f := range dim.Files {
			mark := "    "
			switch f.Status {
			case "over":
				mark = "OVER"
			case "near":
				mark = "near"
			}
			fmt.Fprintf(stdout, "  %s  %9s  %s\n", mark, FormatNumber(f.Value), f.Path)
		}
	}
	fmt.Fprintln(stdout, "")
	return 0
}

func rankOne(cfg *Config, dim Dimension, fallback int, sizeOf func([]byte) int) ([]rankFile, error) {
	threshold := intOption(dim.Options, "threshold", fallback)
	include := stringList(dim.Options["include"])
	exclude := stringList(dim.Options["exclude"])
	nearFloor := float64(threshold) * rankNearRatio
	var files []rankFile
	err := walkIncluded(cfg.Dir, include, exclude, func(rel, abs string) error {
		data, err := os.ReadFile(abs)
		if err != nil {
			return err
		}
		n := sizeOf(data)
		status := "ok"
		if n > threshold {
			status = "over"
		} else if float64(n) > nearFloor {
			status = "near"
		}
		files = append(files, rankFile{Path: rel, Value: float64(n), Status: status})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(files, func(i, j int) bool {
		if files[i].Value != files[j].Value {
			return files[i].Value > files[j].Value
		}
		return files[i].Path < files[j].Path
	})
	return files, nil
}

func writeQueryJSON(stdout, stderr io.Writer, payload map[string]any) int {
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	fmt.Fprintf(stdout, "%s\n", data)
	return 0
}
