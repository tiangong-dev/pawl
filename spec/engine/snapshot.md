Part of the pawl engine contract. See [spec/README.md](../README.md).

## Snapshot — `pawl.snapshot.json`

```json
{
  "metrics": {
    "file-length": {
      "direction": "lower-is-better",
      "value": 3,
      "unit": "files > 500 lines",
      "breakdown": { "pkg/big.go": 612 },
      "tolerance": 1
    }
  }
}
```

- Field order per metric: `direction`, `value`, `unit`, `breakdown`, `tolerance`.
  `breakdown` is `null` when the measurement produced none. `tolerance` is present
  only when the dimension declares it (so `guard`, which never sees the
  config, grants the same slack the gate does).
- Metric ids are serialized in sorted order; 2-space indent; trailing newline.
- Numbers print in minimal decimal notation, never exponent form
  (`3613`, `72.41` — as by Go's `strconv.FormatFloat(v, 'f', -1, 64)`).

### Shape validation

`check` and `guard` refuse (exit 2) to compare against a
malformed snapshot. Shape errors, checked in this order per snapshot:

1. not a JSON object → `snapshot is not an object`
2. `metrics` missing or not an object → `snapshot.metrics is missing or not an object`
3. `metrics` empty → `snapshot.metrics is empty`
4. per metric: not an object → `metric "<id>" is not an object`;
   `value` missing or not a finite number → `metric "<id>" has no numeric value`

JSON.parse succeeding only proves valid JSON, not that the gate can trust the
shape — a truncated or hand-corrupted snapshot must not read as "consistent".

