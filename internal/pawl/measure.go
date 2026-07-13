package pawl

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"sync"
	"time"
)

// MeasureResult is what one adapter measurement produces before the engine
// normalizes it into a Metric (unit defaults to "count", breakdown to null).
type MeasureResult struct {
	Value     float64
	Unit      string
	Breakdown map[string]float64
}

// MeasureAll runs every dimension's measurement concurrently — one slow
// adapter must not serialize the batch behind it. Progress lines go to
// stderr as each measurement starts. Any single failure fails the whole
// batch: "could not measure" must never degrade into a fabricated value.
func MeasureAll(cfg *Config, stderr io.Writer) (map[string]Metric, error) {
	type outcome struct {
		id     string
		result MeasureResult
		err    error
	}
	outcomes := make([]outcome, len(cfg.Dimensions))
	var wg sync.WaitGroup
	for i, dim := range cfg.Dimensions {
		fmt.Fprintf(stderr, "  measuring %s…\n", dim.ID)
		wg.Add(1)
		go func(i int, dim Dimension) {
			defer wg.Done()
			result, err := measureOne(cfg, dim, stderr)
			outcomes[i] = outcome{id: dim.ID, result: result, err: err}
		}(i, dim)
	}
	wg.Wait()

	metrics := map[string]Metric{}
	var failures []string
	for i, o := range outcomes {
		if o.err != nil {
			failures = append(failures, fmt.Sprintf("measuring %s failed: %v", o.id, o.err))
			continue
		}
		dim := cfg.Dimensions[i]
		unit := o.result.Unit
		if unit == "" {
			unit = "count"
		}
		metrics[o.id] = Metric{
			Direction: dim.Direction,
			Value:     o.result.Value,
			Unit:      unit,
			Breakdown: o.result.Breakdown,
			Tolerance: dim.Tolerance,
		}
	}
	if len(failures) > 0 {
		var b bytes.Buffer
		for i, f := range failures {
			if i > 0 {
				b.WriteString("\n")
			}
			b.WriteString(f)
		}
		return nil, fmt.Errorf("%s", b.String())
	}
	return metrics, nil
}

func measureOne(cfg *Config, dim Dimension, stderr io.Writer) (MeasureResult, error) {
	if dim.Builtin != "" {
		return measureBuiltin(cfg, dim, stderr)
	}
	return measureExec(cfg, dim, stderr)
}

// measureExec runs one exec adapter under the SPEC contract: sh -c in the
// config dir with PAWL_ROOT set, stdout parsed as exactly one JSON object,
// stderr passed through for humans, and non-zero exit / timeout / bad JSON
// all surfacing as measurement failures rather than numbers.
func measureExec(cfg *Config, dim Dimension, stderr io.Writer) (MeasureResult, error) {
	stdout, exitCode, err := runAdapterCommand(cfg, dim, stderr, dim.Command)
	if err != nil {
		return MeasureResult{}, err
	}
	if exitCode != 0 {
		return MeasureResult{}, fmt.Errorf("command exited with an error: exit status %d", exitCode)
	}
	return parseAdapterOutput(stdout)
}

// runAdapterCommand is the shared execution environment for exec adapters
// and tool builtins: sh -c in the config dir, PAWL_ROOT set, stderr passed
// through, timeout enforced. The exit code is returned rather than judged —
// each caller owns its tool's exit-code semantics (raw exec demands 0,
// eslint legitimately exits 1 on findings).
func runAdapterCommand(cfg *Config, dim Dimension, stderr io.Writer, command string) ([]byte, int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), dim.Timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Dir = cfg.Dir
	cmd.Env = append(os.Environ(), "PAWL_ROOT="+cfg.Dir)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = stderr
	// A tool the shell forks as a grandchild inherits the (buffered) stdout
	// pipe; on timeout the whole process group is killed so it can't hold
	// that pipe open past the deadline. WaitDelay is a last-resort backstop
	// for platforms where the group kill is a no-op.
	startInOwnProcessGroup(cmd)
	cmd.WaitDelay = 2 * time.Second

	err := cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return nil, 0, fmt.Errorf("timed out after %s", dim.Timeout)
	}
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return stdout.Bytes(), exitErr.ExitCode(), nil
		}
		return nil, 0, fmt.Errorf("command failed to run: %v", err)
	}
	return stdout.Bytes(), 0, nil
}

// parseAdapterOutput enforces the stdout half of the exec contract: exactly
// one JSON object with a finite numeric "value". Anything else is a
// measurement failure — a truncated or chatty stdout must never parse into
// a fake number.
func parseAdapterOutput(raw []byte) (MeasureResult, error) {
	trimmed := bytes.TrimSpace(raw)
	var parsed map[string]any
	if err := json.Unmarshal(trimmed, &parsed); err != nil {
		return MeasureResult{}, fmt.Errorf("stdout is not a single JSON object: %v (stdout: %.200s)", err, trimmed)
	}
	value, ok := parsed["value"].(float64)
	if !ok || math.IsNaN(value) || math.IsInf(value, 0) {
		return MeasureResult{}, fmt.Errorf("stdout JSON has no finite numeric \"value\" (stdout: %.200s)", trimmed)
	}
	result := MeasureResult{Value: value}
	if unit, ok := parsed["unit"]; ok {
		s, ok := unit.(string)
		if !ok {
			return MeasureResult{}, fmt.Errorf("\"unit\" must be a string")
		}
		result.Unit = s
	}
	if rawBreakdown, ok := parsed["breakdown"]; ok && rawBreakdown != nil {
		entries, ok := rawBreakdown.(map[string]any)
		if !ok {
			return MeasureResult{}, fmt.Errorf("\"breakdown\" must be an object of numbers or null")
		}
		breakdown := make(map[string]float64, len(entries))
		for k, v := range entries {
			n, ok := v.(float64)
			if !ok {
				return MeasureResult{}, fmt.Errorf("breakdown entry %q is not a number", k)
			}
			breakdown[k] = n
		}
		result.Breakdown = breakdown
	}
	return result, nil
}
