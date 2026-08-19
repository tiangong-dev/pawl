package pawl

import (
	"fmt"
	"io"
	"os"
	"strings"
)

// `pawl measure` and `--current` split the engine's two jobs — running the
// dimensions, and judging the numbers — into separate invocations that compose
// through a file or a pipe.
//
// The document they exchange is the snapshot format, byte for byte. That is not
// a shortcut: a snapshot *is* a measurement someone decided to keep, so there
// was never a second format to invent, and `pawl measure > pawl.snapshot.json`
// means exactly what it looks like.
//
// The payoff is that one measurement can drive every decision that follows it.
// Measuring separately for the check and again for the record is how a gate
// ends up recording numbers nobody verified — and, when a dimension reads an
// artifact off disk, how it ends up recording numbers from a different build.

// runMeasure runs the dimensions and writes the measurement document to stdout.
// It renders no verdict and reads no baseline, so it is also the answer to
// "what are the numbers right now" — a question that otherwise requires asking
// for a judgement you did not want.
func runMeasure(cfg *Config, progress, stdout, stderr io.Writer) int {
	current, _, err := MeasureAll(cfg, progress, stderr)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	if _, err := stdout.Write(MarshalSnapshot(current)); err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	return 0
}

// readMeasurement loads a measurement document from a path, or from stdin when
// the path is "-".
func readMeasurement(path string, stdin io.Reader) (map[string]Metric, error) {
	source := path
	var data []byte
	var err error
	if path == "-" {
		source = "stdin"
		data, err = io.ReadAll(stdin)
	} else {
		data, err = os.ReadFile(path)
	}
	if err != nil {
		return nil, fmt.Errorf("reading measurement from %s: %w", source, err)
	}
	snap, parsed, err := ParseSnapshot(data)
	if err != nil {
		return nil, fmt.Errorf("measurement from %s: %w", source, err)
	}
	if shapeErrors := SnapshotShapeErrors(parsed); len(shapeErrors) > 0 {
		msg := "measurement from " + source + " has an invalid shape:\n"
		for _, e := range shapeErrors {
			msg += "  • " + e + "\n"
		}
		return nil, fmt.Errorf("%s", strings.TrimSuffix(msg, "\n"))
	}
	return snap.Metrics, nil
}

// acquireMeasurement produces the current numbers for cfg's dimensions: by
// running them, or by taking the ones a previous `pawl measure` produced.
//
// A supplied document that is missing a dimension in scope is a measurement
// failure naming it, never a quietly narrower run. Trusting the caller about
// the numbers is the deliberate part of --current; letting the file decide
// which dimensions exist would let a gate shrink without anyone saying so.
func acquireMeasurement(cfg *Config, supplied map[string]Metric, progress, stderr io.Writer) (map[string]Metric, map[string]*ArtifactInfo, error) {
	if supplied == nil {
		return MeasureAll(cfg, progress, stderr)
	}
	current := make(map[string]Metric, len(cfg.Dimensions))
	var missing []measureFailure
	for _, dim := range cfg.Dimensions {
		metric, ok := supplied[dim.ID]
		if !ok {
			missing = append(missing, measureFailure{
				id:      dim.ID,
				message: "the supplied measurement has no entry for it — re-run `pawl measure` with the same scope",
			})
			continue
		}
		current[dim.ID] = metric
	}
	if len(missing) > 0 {
		return nil, nil, &measureError{failures: missing}
	}
	// No artifacts: this run read no report off disk, and claiming provenance
	// for a file it never opened would be worse than reporting none.
	return current, nil, nil
}
