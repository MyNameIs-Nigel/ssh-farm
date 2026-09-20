#!/usr/bin/env python3
"""Measure coverage of the lines this branch ADDED, not the repo average.

Total coverage is a lagging indicator and a poor gate: a 40k-statement repo at
72% absorbs an entirely untested 200-line feature without visibly moving. On a
TDD project the question that matters is narrower and much sharper — "were the
lines written in this change exercised by a test?" — and it has a much higher
acceptable bar than the repo average, because new code has no excuse.

Usage:
    diff-coverage.py --profile coverage.out --base origin/main [--min 85]

Lines that are not statements (declarations, comments, braces, struct literals)
are ignored; only lines the Go coverage profile actually tracks are counted.
"""

from __future__ import annotations

import argparse
import collections
import os
import re
import subprocess
import sys

BLOCK = re.compile(r"^(?P<file>.+):(?P<sl>\d+)\.\d+,(?P<el>\d+)\.\d+ (?P<stmts>\d+) (?P<count>\d+)$")
HUNK = re.compile(r"^@@ -\d+(?:,\d+)? \+(?P<start>\d+)(?:,(?P<len>\d+))? @@")


def run(*args: str) -> str:
    return subprocess.run(args, check=True, capture_output=True, text=True).stdout


def changed_lines(base: str) -> dict[str, set[int]]:
    """Repo-relative .go file -> set of line numbers added or modified."""
    diff = run("git", "diff", "--unified=0", "--no-color", f"{base}...HEAD", "--", "*.go")
    out: dict[str, set[int]] = collections.defaultdict(set)
    path: str | None = None
    for line in diff.splitlines():
        if line.startswith("+++ b/"):
            path = line[6:]
            # Test files are the measuring instrument, not the thing measured.
            if path.endswith("_test.go"):
                path = None
            continue
        if line.startswith("+++ "):  # /dev/null — a deletion
            path = None
            continue
        if path is None:
            continue
        m = HUNK.match(line)
        if m:
            start = int(m.group("start"))
            length = int(m.group("len") or 1)
            out[path].update(range(start, start + length))
    return {p: lines for p, lines in out.items() if lines}


def exempt_packages(floors: str) -> set[str]:
    """Packages that declare a 0.0 floor are exempt here too.

    coverage-floors.txt is the one place a package is allowed to say "this is
    an operator tool / a thin shim, it is not unit-tested and that is a
    decision, not an oversight". Honouring the same declaration in both gates
    means there is exactly one list to argue with, and no second place for an
    exemption to hide.
    """
    exempt: set[str] = set()
    try:
        with open(floors, encoding="utf-8") as fh:
            for raw in fh:
                line = raw.split("#", 1)[0].strip()
                if not line:
                    continue
                parts = line.split()
                if len(parts) == 2 and parts[0] != "TOTAL" and float(parts[1]) == 0.0:
                    exempt.add(parts[0])
    except FileNotFoundError:
        pass
    return exempt


def profile_blocks(profile: str, module: str) -> dict[str, list[tuple[int, int, int]]]:
    """Repo-relative file -> [(startLine, endLine, executionCount)]."""
    blocks: dict[str, list[tuple[int, int, int]]] = collections.defaultdict(list)
    with open(profile, encoding="utf-8") as fh:
        for line in fh:
            line = line.strip()
            if not line or line.startswith("mode:"):
                continue
            m = BLOCK.match(line)
            if m is None:
                continue
            f = m.group("file")
            if not f.startswith(module + "/"):
                continue
            rel = f[len(module) + 1 :]
            blocks[rel].append((int(m.group("sl")), int(m.group("el")), int(m.group("count"))))
    return blocks


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--profile", required=True)
    ap.add_argument("--base", default="origin/main")
    ap.add_argument("--min", type=float, default=85.0)
    ap.add_argument("--module", default="github.com/mynameis-nigel/ssh-farm")
    ap.add_argument("--floors", default="scripts/ci/coverage-floors.txt")
    ap.add_argument("--summary", help="append Markdown here (GITHUB_STEP_SUMMARY)")
    args = ap.parse_args()

    exempt = exempt_packages(args.floors)
    changed = changed_lines(args.base)
    skipped = sorted(p for p in changed if os.path.dirname(p) in exempt)
    changed = {p: v for p, v in changed.items() if os.path.dirname(p) not in exempt}
    if skipped:
        print("diff-coverage: exempt (0.0 floor declared): " + ", ".join(skipped))
    if not changed:
        print("diff-coverage: no non-test Go lines changed — nothing to measure.")
        return 0

    blocks = profile_blocks(args.profile, args.module)

    covered = 0
    total = 0
    misses: dict[str, list[int]] = collections.defaultdict(list)

    for path in sorted(changed):
        for line in sorted(changed[path]):
            containing = [c for sl, el, c in blocks.get(path, []) if sl <= line <= el]
            if not containing:
                continue  # not a tracked statement: a decl, a comment, a brace
            total += 1
            if any(c > 0 for c in containing):
                covered += 1
            else:
                misses[path].append(line)

    if total == 0:
        print("diff-coverage: changed lines contain no tracked statements — nothing to measure.")
        return 0

    pct = 100.0 * covered / total
    verdict = "PASS" if pct + 1e-9 >= args.min else "FAIL"
    headline = f"{pct:.1f}% of {total} changed statements covered (minimum {args.min:.0f}%) — {verdict}"
    print(f"diff-coverage: {headline}")

    lines_md = [f"### Coverage of changed lines\n", f"**{headline}**\n"]
    if misses:
        print("\nUncovered changed lines:")
        lines_md.append("<details><summary>Uncovered changed lines</summary>\n")
        for path in sorted(misses):
            rendered = ", ".join(str(n) for n in misses[path])
            print(f"  {path}: {rendered}")
            lines_md.append(f"- `{path}`: {rendered}")
        lines_md.append("\n</details>\n")
        if verdict == "FAIL":
            print(
                "\nEvery line above ran in production but never ran in a test. On a TDD\n"
                "project that ordering is backwards: write the failing test first, then\n"
                "the line. If a line is genuinely untestable (an os.Exit path, a\n"
                "defensive branch that cannot be reached), say so in the PR and lower\n"
                "--min for this run with a reviewer's agreement."
            )

    if args.summary:
        with open(args.summary, "a", encoding="utf-8") as fh:
            fh.write("\n".join(lines_md) + "\n")

    return 0 if verdict == "PASS" else 1


if __name__ == "__main__":
    sys.exit(main())
