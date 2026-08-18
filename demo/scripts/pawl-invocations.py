#!/usr/bin/env python3
"""Extract an eval agent's pawl-invocation timeline from its session transcript.

Two capability items in ../capabilities.yaml are about *when* and *how* an
agent called pawl, not about what it said afterwards, and an agent's own
summary is not evidence for either:

  verifies-against-the-gate-before-finishing — did any `pawl check` happen
    after the last file edit? An agent that edits, then stops, then asserts
    the gate is fine, never measured it. Reading this off the transcript by
    eye is exactly the kind of judgement that drifts between runs.
  uses-json-format — how many invocations carried --format json, and at
    which position. A single-shot task cannot distinguish "never adopts it"
    from "adopts it on the second look"; the invocation index can.

Usage:
    pawl-invocations.py <agent-jsonl-path>

Prints the ordered timeline (edits and pawl calls interleaved), then a
verdict block. Exit 0 if a pawl check ran after the last edit, 1 if not,
2 if the transcript held no pawl invocation at all.
"""
import json
import re
import sys

EDIT_TOOLS = {"Edit", "Write", "NotebookEdit", "MultiEdit"}

# A bare `pawl`, not the `-code-pawl/` inside a scratchpad path: the invocation
# must start a command or follow a shell separator.
PAWL_CALL = re.compile(r"(?:^|[;&|(]\s*|\s)pawl(\s+[^;&|)\n]*)?")

KNOWN_COMMANDS = {
    "init", "agent-md", "record", "check", "diff", "baseline-guard",
    "trend", "status", "constraints", "rank", "version", "help",
}


def events(jsonl_path):
    """Yield ("edit"|"pawl", detail) in transcript order."""
    with open(jsonl_path) as f:
        for line in f:
            line = line.strip()
            if not line:
                continue
            try:
                obj = json.loads(line)
            except json.JSONDecodeError:
                continue
            msg = obj.get("message")
            if not msg or msg.get("role") != "assistant":
                continue
            content = msg.get("content")
            if not isinstance(content, list):
                continue
            for c in content:
                if c.get("type") != "tool_use":
                    continue
                name = c.get("name")
                if name in EDIT_TOOLS:
                    path = c.get("input", {}).get("file_path", "?")
                    yield "edit", f"{name} {path}"
                elif name == "Bash":
                    cmd = c.get("input", {}).get("command", "")
                    for m in PAWL_CALL.finditer(cmd):
                        yield "pawl", ("pawl" + (m.group(1) or "")).strip()


def describe(call):
    """(subcommand, flags) for one pawl invocation.

    `pawl --version` and `pawl -h` are not the default check — counting them
    as one would let a trailing version probe forge a
    verifies-against-the-gate-before-finishing pass.
    """
    parts = call.split()[1:]
    flags = [p for p in parts if p.startswith("-")]
    if parts and parts[0] in KNOWN_COMMANDS:
        return parts[0], flags
    if "--version" in flags:
        return "version", flags
    if "--help" in flags or "-h" in flags:
        return "help", flags
    return "check (default)", flags


def main():
    if len(sys.argv) != 2:
        print(__doc__)
        sys.exit(2)

    timeline = list(events(sys.argv[1]))
    calls = [d for kind, d in timeline if kind == "pawl"]

    n = 0
    for kind, detail in timeline:
        if kind == "pawl":
            n += 1
            sub, flags = describe(detail)
            mark = "json" if "--format" in detail and "json" in detail else "text"
            scope = " ".join(f for f in flags if f in ("--only", "--since")) or "full"
            print(f"  [{n:>2}] pawl {sub:<16} {mark:<5} {scope:<8} | {detail}")
        else:
            print(f"       --edit-- {detail}")

    print()
    if not calls:
        print("VERDICT verifies-against-the-gate-before-finishing: FAIL — pawl was never invoked")
        print("VERDICT uses-json-format: not-applicable")
        sys.exit(2)

    last_edit = max((i for i, (k, _) in enumerate(timeline) if k == "edit"), default=-1)
    last_check = max(
        (i for i, (k, d) in enumerate(timeline)
         if k == "pawl" and describe(d)[0] in ("check (default)", "check")),
        default=-1,
    )
    verified = last_check > last_edit
    print(f"VERDICT verifies-against-the-gate-before-finishing: "
          f"{'PASS' if verified else 'FAIL'} — "
          f"{'a check ran after the last edit' if verified else 'no check after the last edit'}")

    json_calls = [i + 1 for i, c in enumerate(calls) if "--format" in c and "json" in c]
    print(f"VERDICT uses-json-format: {len(json_calls)}/{len(calls)} invocations, "
          f"at position(s) {json_calls or 'none'}")
    sys.exit(0 if verified else 1)


if __name__ == "__main__":
    main()
