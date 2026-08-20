Part of the pawl engine contract. See [spec/README.md](../README.md).

## init

`pawl init` scaffolds a working starter `pawl.yaml` so a new project can go from
nothing to a passing gate in two commands (`pawl init && pawl record`). It reads
no existing config.

- Writes to the config path (`-c <path>`, default `./pawl.yaml`).
- **Never overwrites**: if a file already exists at that path, it prints a
  message naming the path and exits 2 (a scaffolder that clobbered a hand-tuned
  config would be worse than useless). A different pre-existing filesystem error
  on the stat is likewise exit 2.
- The written config is **valid and non-empty**: it declares at least one
  dimension using only zero-dependency primitive builtins (`file-length`,
  `pattern-count`), so `pawl record` succeeds immediately with no external tool
  installed. Comments in the file point at the recipe cookbook for more.
- On success it writes the file and prints next-steps lines (naming the file,
  pointing at `pawl record`, and naming
  [`agent`](agent.md) for repositories worked on by a coding agent),
  exit 0.

