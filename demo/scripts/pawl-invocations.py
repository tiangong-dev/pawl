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

# An agent that writes files through the shell leaves no Edit/Write tool call.
# Missing those made the last-edit position -1, which made "a check ran after
# the last edit" trivially true — the script manufactured passes for exactly
# the runs it was built to judge. Redirection into a path, in-place sed, tee,
# and heredocs all count as edits.
BASH_EDIT = re.compile(
    r">\s*[\w./-]+\.\w+"       # > src/foo.js  (not > /dev/null, no extension)
    r"|>>\s*[\w./-]+\.\w+"
    r"|\bsed\s+-i\b"
    r"|\btee\s+[\w./-]+"
    r"|\bcat\s*>\s*"
    r"|\bpatch\b"
)

# A bare `pawl`, not the `-code-pawl/` inside a scratchpad path: the invocation
# must sit at a command position — the start of the string, or right after a
# shell separator. An earlier `|\s` alternative also matched `pawl` as an
# *argument*: `which pawl` was read as an invocation, and describe() classified
# it as a default check, so a probe that never ran the gate forged a
# verification pass. Optional env assignments and wrappers still count.
PAWL_CALL = re.compile(
    r"(?:^|[;&|(])\s*"
    r"(?:(?:\w+=\S+|env|time|sudo|nohup|exec)\s+)*"
    r"pawl(\s+[^;&|)\n]*)?"
)

# Includes commands pawl has since retired (diff, status, constraints), so
# transcripts recorded before their removal still classify instead of falling
# through to "check (default)". `measure` matters most: it prints numbers with
# no baseline and no verdict, so classifying it as a check would credit an
# agent that measured but never asked whether anything got worse.
KNOWN_COMMANDS = {
    "init", "agent-md", "record", "check", "measure", "baseline-guard",
    "trend", "rank", "version", "help",
    "diff", "status", "constraints",
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
                    if BASH_EDIT.search(cmd) and not PAWL_CALL.search(cmd):
                        yield "edit", f"shell write: {cmd.splitlines()[0][:70]}"


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
    if last_edit < 0:
        # No edit found at all. Either the agent changed nothing, or it edited
        # in a way this script cannot see — and "the last check came after the
        # last edit" is vacuously true either way. Say so instead of passing.
        print("VERDICT verifies-against-the-gate-before-finishing: INDETERMINATE — "
              "no edit detected in the transcript; check by hand whether the agent "
              "changed anything")
        json_calls = [i + 1 for i, c in enumerate(calls) if "--format" in c and "json" in c]
        print(f"VERDICT uses-json-format: {len(json_calls)}/{len(calls)} invocations, "
              f"at position(s) {json_calls or 'none'}")
        sys.exit(1)
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
