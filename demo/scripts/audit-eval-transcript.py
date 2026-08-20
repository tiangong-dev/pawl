#!/usr/bin/env python3
"""Flag Claude/Codex eval commands that leaked into the real pawl repo instead
of staying inside the assigned eval directory.

A spawned subagent's default Bash cwd is the OUTER session's cwd (the real
pawl repo), not whatever directory its task prompt tells it to `cd` into —
the working directory is only requested in prose, never forced by the
harness. A command with no `cd` prefix silently runs against the real repo.

This does NOT flag every cd-less command — an agent legitimately using
scratchpad space alongside its eval directory (e.g. building a comparison
copy elsewhere under the same scratchpad root) is fine and is common in
clean runs. It flags only commands that actually touch the real repo: either
by referencing repo-root literally, or by reading one of the specific
spoiler files (demo/README.md, demo/capabilities.yaml, fixture/TASKS.md) via
a bare relative path while not scoped into eval-dir.

Usage:
    audit-eval-transcript.py <agent-jsonl-path> <eval-dir-absolute-path> <repo-root-absolute-path>

Exit 0 and prints "clean" if no command touched the real repo. Exit 1 and
lists violations otherwise. See demo/README.md's "Known gaps" section for
how this was discovered (2026-08-17 evaluation batch, 3 of 8 runs leaked
into the real repo this way).
"""
import json
import os
import re
import sys

SPOILER_PATTERNS = [
    r"\bdemo/README\.md\b",
    r"\bdemo/capabilities\.yaml\b",
    r"\bcapabilities\.yaml\b",
    r"\bfixture/TASKS\.md\b",
    r"\bTASKS\.md\b",
    r"(^|\s)demo(\s|$|/)",
]


def bash_commands(jsonl_path):
    with open(jsonl_path) as f:
        for line in f:
            line = line.strip()
            if not line:
                continue
            try:
                obj = json.loads(line)
            except json.JSONDecodeError:
                continue
            item = obj.get("item")
            if (obj.get("type") == "item.completed" and isinstance(item, dict)
                    and item.get("type") == "command_execution"):
                cmd = item.get("command", "")
                if cmd:
                    yield cmd
                continue

            msg = obj.get("message")
            if not msg or msg.get("role") != "assistant":
                continue
            content = msg.get("content")
            if not isinstance(content, list):
                continue
            for c in content:
                if c.get("type") == "tool_use" and c.get("name") == "Bash":
                    cmd = c.get("input", {}).get("command", "")
                    if cmd:
                        yield cmd


def spellings(path):
    """Every way a shell command might name one directory.

    /tmp is a symlink to /private/tmp on macOS. The scratchpad path a harness
    is handed is the /private form; the path a task prompt tells an agent to
    cd into is usually the /tmp form. Comparing the two spellings as strings
    read a command scoped *into* the eval directory as one that had escaped
    it, and the protocol discards a contaminated run — so a clean run got
    thrown away and re-run on the strength of a naming difference.
    """
    forms = {path, os.path.realpath(path)}
    for form in list(forms):
        if form.startswith("/private/"):
            forms.add(form[len("/private"):])
        else:
            forms.add("/private" + form)
    return {form.rstrip("/") for form in forms if form}


# A root reference either ends there or continues below that root. Treating a
# root as an arbitrary substring confuses a sibling such as `pawl-ab` with the
# `pawl` repository and discards a clean run.
PATH_BOUNDARY = r"(?=$|[/\s'\"`;,:|&(){}\[\]<>])"


def references_path(command, path):
    return any(re.search(re.escape(form) + PATH_BOUNDARY, command)
               for form in spellings(path))


def is_violation(cmd, eval_dir, repo_root):
    if references_path(cmd, repo_root):
        return True
    # cd into eval_dir anywhere in the command means later relative reads in
    # that same command are scoped there, not to repo_root — only check
    # commands that never scope themselves to eval_dir at all.
    if references_path(cmd, eval_dir):
        return False
    for pat in SPOILER_PATTERNS:
        if re.search(pat, cmd):
            return True
    return False


def main():
    if len(sys.argv) != 4:
        print(__doc__)
        sys.exit(2)
    jsonl_path, eval_dir, repo_root = sys.argv[1], sys.argv[2].rstrip("/"), sys.argv[3].rstrip("/")

    violations = []
    total = 0
    for cmd in bash_commands(jsonl_path):
        total += 1
        if is_violation(cmd, eval_dir, repo_root):
            violations.append(cmd)

    if total == 0:
        print("no Bash commands found in transcript — nothing to audit")
        sys.exit(2)

    if violations:
        print(f"CONTAMINATED: {len(violations)}/{total} commands touched the real repo:")
        for v in violations:
            print(f"  - {v}")
        sys.exit(1)

    print(f"clean: none of {total} commands touched {repo_root}")
    sys.exit(0)


if __name__ == "__main__":
    main()
