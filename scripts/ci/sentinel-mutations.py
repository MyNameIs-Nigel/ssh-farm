#!/usr/bin/env python3
"""Prove the test suite can still fail.

docs/tests/01 and docs/tests/02 each end with an acceptance criterion of the
same shape:

  * "any sim edit fails at least one parity test (verified once, noted in PR)"
  * "a deliberate rank-math bug (off-by-one in the tie rule) and a deliberate
     filter bypass (drop the leet fold) each fail named tests — verified once,
     noted in PR"

"Verified once" is the problem. A suite that could catch a bug in September
can stop catching it in December — a test gets rewritten to assert something
weaker, a golden gets regenerated to match a bug, a table loses the one row
that mattered — and nothing goes red, because nothing is checking that the
tests still bite. Green CI cannot distinguish a correct implementation from an
implementation whose tests no longer look at it.

This harness turns "verified once" into "verified on every run". Each sentinel
injects a specific, realistic bug into production code, asserts the named test
FAILS, and reverts. If a sentinel's test passes under mutation, the suite has
lost its grip on that behaviour, and that is reported as a CI failure even
though every ordinary test is green.

It is deliberately a short hand-written list of the bugs this project has
decided it cannot afford, not general mutation testing: a full mutation run is
slow, noisy with equivalent mutants, and needs triage nobody has time for. Ten
sentinels that each encode a real acceptance criterion are worth more than
four hundred generated mutants that get ignored.

Usage:
    sentinel-mutations.py [--list] [--only NAME] [--summary FILE]
"""

from __future__ import annotations

import argparse
import pathlib
import subprocess
import sys

REPO = pathlib.Path(__file__).resolve().parents[2]

# name, file, exact source to replace, replacement, package, test -run regex, why
SENTINELS = [
    (
        "leaderboard-tie-off-by-one",
        "internal/leaderboard/engine.go",
        "\t\t\trank = i + 1",
        "\t\t\trank = i",
        "./internal/leaderboard",
        "TestRankMathTable|TestLeaderboardPropertyAgainstNaiveRecount",
        "Competition ranking is the most-read number in the game. An off-by-one "
        "in the tie rule is invisible in a 3-row fixture and wrong for every "
        "player on a real board (docs/tests/02).",
    ),
    (
        "moderation-leet-fold-dropped",
        "internal/moderation/normalize.go",
        "\t\tif f, ok := leetFold[r]; ok {",
        "\t\tif f, ok := leetFold[r]; ok && false {",
        "./internal/moderation",
        "TestMustBlockCorpusIsDeniedByEveryEvasionClass",
        "Dropping the leet fold leaves a filter that blocks only the literal "
        "spelling — the decorative kind the spec explicitly calls out. The "
        "must_block corpus exists to catch exactly this (docs/tests/02).",
    ),
    (
        "sim-parity-payout-drift",
        "internal/sim/advance.go",
        "\tpayout = s.sellMultiplied(c, base, crop.ID, now)",
        "\tpayout = s.sellMultiplied(c, base, crop.ID, now) + 1",
        "./internal/sim",
        "TestScriptedSequenceMatchesV1Golden",
        "One coin per harvest. The parity promise is that a v1 player whose save "
        "crosses over sees the same numbers, and the scripted golden is what "
        "makes that a claim rather than a hope (docs/tests/01).",
    ),
    (
        "sim-grow-time-drift",
        "internal/sim/derive.go",
        "\tg := crop.GrowSeconds * (100 - reduction) / 100",
        "\tg := crop.GrowSeconds*(100-reduction)/100 + 1",
        "./internal/sim",
        "TestUpgradesAffectTheNextRun|TestGrowthAndHarvestOverFixedSpans|TestVeryLongAutoFarmCatchUpIsBoundedAndExact",
        "One second per crop is the smallest sim change anyone could make by "
        "accident. NOTE: this sentinel is aimed at the sim unit tests on purpose. "
        "The v1 parity goldens do NOT catch it — the scripted sequence advances "
        "300s against crops that mature well inside that window, so a second of "
        "drift is absorbed before the first assertion. docs/tests/01 claims 'any "
        "sim edit fails at least one parity test'; for grow-time edits that is "
        "true of the unit suite, not of the goldens. Tightening the scripted "
        "fixture to harvest at the exact ready tick would close the gap.",
    ),
]


def run_tests(pkg: str, pattern: str) -> subprocess.CompletedProcess[str]:
    return subprocess.run(
        ["go", "test", "-count=1", "-run", pattern, pkg],
        cwd=REPO,
        capture_output=True,
        text=True,
    )


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--list", action="store_true", help="print the sentinels and exit")
    ap.add_argument("--only", help="run a single sentinel by name")
    ap.add_argument("--summary", help="append Markdown here (GITHUB_STEP_SUMMARY)")
    args = ap.parse_args()

    if args.list:
        for name, path, _, _, pkg, pattern, why in SENTINELS:
            print(f"{name}\n  {path} -> {pkg} -run '{pattern}'\n  {why}\n")
        return 0

    selected = [s for s in SENTINELS if args.only in (None, s[0])]
    if not selected:
        print(f"sentinel: no sentinel named {args.only!r}", file=sys.stderr)
        return 2

    results: list[tuple[str, str, str]] = []
    failures = 0

    for name, relpath, old, new, pkg, pattern, why in selected:
        path = REPO / relpath
        original = path.read_text(encoding="utf-8")
        occurrences = original.count(old)

        if occurrences != 1:
            # Not a pass and not a normal failure: the sentinel itself is stale.
            # Whoever moved the code is the right person to re-aim it, and they
            # are looking at this CI run right now.
            msg = (
                f"anchor found {occurrences} times, expected exactly 1 — the code moved. "
                f"Re-aim this sentinel in scripts/ci/sentinel-mutations.py."
            )
            print(f"✗ {name}: {msg}")
            results.append((name, "STALE", msg))
            failures += 1
            continue

        try:
            path.write_text(original.replace(old, new), encoding="utf-8")
            proc = run_tests(pkg, pattern)
        finally:
            path.write_text(original, encoding="utf-8")

        if proc.returncode != 0:
            print(f"✓ {name}: mutation caught by {pattern}")
            results.append((name, "CAUGHT", pattern))
            continue

        failures += 1
        print(f"✗ {name}: MUTATION SURVIVED")
        print(f"    mutated: {relpath}")
        print(f"    {old.strip()}  ->  {new.strip()}")
        print(f"    and `go test -run '{pattern}' {pkg}` still passed.")
        print(f"    why this matters: {why}")
        results.append((name, "SURVIVED", pattern))

    if args.summary:
        with open(args.summary, "a", encoding="utf-8") as fh:
            fh.write("### Sentinel mutations\n\n")
            fh.write("| sentinel | result | guarded by |\n| --- | --- | --- |\n")
            for name, status, detail in results:
                mark = {"CAUGHT": "✅ caught", "SURVIVED": "❌ survived", "STALE": "⚠️ stale"}[status]
                fh.write(f"| `{name}` | {mark} | `{detail}` |\n")
            fh.write("\n")

    if failures:
        print(f"\nsentinel-mutations: {failures} of {len(selected)} sentinels did not hold.")
        return 1

    print(f"\nsentinel-mutations: all {len(selected)} sentinels held.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
