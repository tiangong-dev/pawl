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
- **Detects the project type from marker files** in the config file's directory.
  Detection reads those files' presence only — it runs no command, parses no
  manifest, and fetches nothing:

  | marker file | stack | generated `include` | generated excludes |
  |---|---|---|---|
  | `package.json` | Node/TypeScript | `**/*.{ts,tsx,js,jsx}` | `**/node_modules/**`, `**/dist/**`, `**/build/**`, `**/coverage/**` |
  | `go.mod` | Go | `**/*.go` | `**/vendor/**` |
  | `Cargo.toml` | Rust | `**/*.rs` | `**/target/**` |
  | `pyproject.toml` or `requirements.txt` | Python | `**/*.py` | `**/.venv/**`, `**/venv/**`, `**/__pycache__/**` |
  | `Package.swift` | Swift | `**/*.swift` | `**/.build/**` |

  Several markers merge: the generated dimensions scan the union of the detected
  stacks' `include` globs, and the union of their excludes goes in one top-level
  [`exclude`](config.md#shared-exclude) list rather than being repeated in each
  dimension. A directory with no marker keeps the generic starter: `include:
  ["**/*"]` and the common build/dependency trees excluded. init names the
  detected stacks on stdout.
- The written config is **valid and non-empty**: it declares at least one
  dimension using only zero-dependency primitive builtins (`file-length`,
  `pattern-count`), so `pawl record` succeeds immediately with no external tool
  installed. Comments in the file point at the recipe cookbook for more.
- Its **first line** is the editor directive
  `# yaml-language-server: $schema=<schema $id>`, pointing at
  [`schema/pawl.schema.json`](../../schema/pawl.schema.json) so an editor
  completes and checks keys from the moment the file is written — see
  [§ Editor schema](config.md#editor-schema).
- On success it writes the file and prints next-steps lines (naming the file,
  the detected stacks when there are any, pointing at `pawl record`, the
  accessible `https://github.com/tiangong-dev/pawl/blob/main/RECIPES.md`
  cookbook, and naming [`agent`](agent.md) for repositories worked on by a
  coding agent), exit 0.
