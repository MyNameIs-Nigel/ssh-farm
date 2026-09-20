#!/usr/bin/env python3
"""Enforce per-package and total coverage floors against a Go coverage profile.

Why floors in a committed file rather than a coverage service: the floors are
reviewable in the same PR as the code that moves them. Lowering a floor is a
deliberate, visible act with a reviewer on it — which is the whole point of a
ratchet. Raising one is mechanical, and this script tells you when to.

Usage:
    coverage-gate.py coverage.out scripts/ci/coverage-floors.txt [--summary FILE]

Exit status is non-zero if any package (or the total) sits below its floor.
"""

from __future__ import annotations

import argparse
import collections
import os
import re
import sys

# A coverage profile line:
#   <import/path/file.go>:<l>.<c>,<l>.<c> <numStatements> <executionCount>
BLOCK = re.compile(r"^(?P<file>.+):\d+\.\d+,\d+\.\d+ (?P<stmts>\d+) (?P<count>\d+)$")

# How far above its floor a package must climb before we nag about raising it.
RATCHET_SLACK = 3.0


def parse_profile(path: str) -> dict[str, tuple[int, int]]:
    """Return {package: (covered_statements, total_statements)}."""
    tally: dict[str, list[int]] = collections.defaultdict(lambda: [0, 0])
    with open(path, encoding="utf-8") as fh:
        for line in fh:
            line = line.strip()
            if not line or line.startswith("mode:"):
                continue
            m = BLOCK.match(line)
            if m is None:
                raise SystemExit(f"coverage-gate: unparsable profile line: {line!r}")
            pkg = os.path.dirname(m.group("file"))
            stmts = int(m.group("stmts"))
            tally[pkg][1] += stmts
            if int(m.group("count")) > 0:
                tally[pkg][0] += stmts
    return {pkg: (c, t) for pkg, (c, t) in tally.items()}


def parse_floors(path: str) -> dict[str, float]:
    floors: dict[str, float] = {}
    with open(path, encoding="utf-8") as fh:
        for raw in fh:
            line = raw.split("#", 1)[0].strip()
            if not line:
                continue
            parts = line.split()
            if len(parts) != 2:
                raise SystemExit(f"coverage-gate: malformed floor line: {raw!r}")
            floors[parts[0]] = float(parts[1])
    if "TOTAL" not in floors:
        raise SystemExit("coverage-gate: floors file has no TOTAL entry")
    return floors


def pct(covered: int, total: int) -> float:
    return 100.0 * covered / total if total else 100.0


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("profile")
    ap.add_argument("floors")
    ap.add_argument("--summary", help="append a Markdown table here (GITHUB_STEP_SUMMARY)")
    ap.add_argument("--module", default="github.com/mynameis-nigel/ssh-farm")
    args = ap.parse_args()

    by_pkg = parse_profile(args.profile)
    floors = parse_floors(args.floors)

    covered_total = sum(c for c, _ in by_pkg.values())
    stmt_total = sum(t for _, t in by_pkg.values())

    rows: list[tuple[str, float, float, str]] = []
    failures: list[str] = []
    ratchets: list[str] = []

    for pkg in sorted(by_pkg):
        short = pkg[len(args.module) + 1 :] if pkg.startswith(args.module + "/") else pkg
        actual = pct(*by_pkg[pkg])  # (covered, total)
        floor = floors.get(short)
        if floor is None:
            # An unlisted package is not a silent pass: new packages must
            # declare a floor, or coverage can be added untracked and then
            # quietly removed again.
            failures.append(f"{short}: {actual:.1f}% — no floor declared in {args.floors}")
            rows.append((short, actual, float("nan"), "NO FLOOR"))
            continue
        if actual + 1e-9 < floor:
            failures.append(f"{short}: {actual:.1f}% is below its floor of {floor:.1f}%")
            rows.append((short, actual, floor, "FAIL"))
        else:
            if actual >= floor + RATCHET_SLACK:
                ratchets.append(f"{short}: {floor:.1f} -> {actual:.1f}")
            rows.append((short, actual, floor, "ok"))

    total_actual = pct(covered_total, stmt_total)
    total_floor = floors["TOTAL"]
    if total_actual + 1e-9 < total_floor:
        failures.append(f"TOTAL: {total_actual:.1f}% is below its floor of {total_floor:.1f}%")
    rows.append(("TOTAL", total_actual, total_floor, "FAIL" if total_actual < total_floor else "ok"))
    if total_actual >= total_floor + RATCHET_SLACK:
        ratchets.append(f"TOTAL: {total_floor:.1f} -> {total_actual:.1f}")

    lines = ["| package | coverage | floor | |", "| --- | ---: | ---: | :-- |"]
    for short, actual, floor, status in rows:
        floor_cell = "—" if floor != floor else f"{floor:.1f}%"  # NaN check
        mark = {"ok": "✅", "FAIL": "❌", "NO FLOOR": "⚠️"}[status]
        lines.append(f"| `{short}` | {actual:.1f}% | {floor_cell} | {mark} |")
    table = "\n".join(lines)

    print(table)
    if ratchets:
        print("\nAbove floor by more than "
              f"{RATCHET_SLACK:.0f} points — raise these in scripts/ci/coverage-floors.txt:")
        for r in ratchets:
            print(f"  {r}")
    if failures:
        print("\ncoverage-gate: FAILED")
        for f in failures:
            print(f"  {f}")

    if args.summary:
        with open(args.summary, "a", encoding="utf-8") as fh:
            fh.write("### Coverage\n\n" + table + "\n\n")
            if ratchets:
                fh.write("<details><summary>Floors worth raising</summary>\n\n")
                fh.write("\n".join(f"- `{r}`" for r in ratchets) + "\n\n</details>\n\n")
            if failures:
                fh.write("**Coverage gate failed:**\n\n")
                fh.write("\n".join(f"- {f}" for f in failures) + "\n\n")

    return 1 if failures else 0


if __name__ == "__main__":
    sys.exit(main())
