## What changed

<!-- One paragraph. What does the game or the fleet do now that it did not before? -->

## The test that proves it

<!--
This project is test-driven and the CI gates assume it: `test-first discipline`
fails a change to production Go with no test alongside it, and `coverage of
changed lines` wants 85% of the statements you added to be exercised.

Name the test. If the test came second, or if a line is genuinely untestable
(an os.Exit path, an unreachable defensive branch), say so here — a reviewer
agreeing in the open is the intended escape hatch, not a silent one.
-->

## Parity

<!--
Delete if this touches neither internal/sim nor internal/content.

docs/README.md: "Parity beats improvement." A v1 player whose save is imported
must see the same farm and the same numbers. If this changes sim behaviour on
purpose, say which goldens moved and why that is intended rather than a
regression.
-->

## Checklist

- [ ] Tests fail without the production change (red first)
- [ ] `sh scripts/ci/verify.sh` passes locally
- [ ] No denylist content in the diff, the commit message, or a test name (docs/README.md convention 2)
- [ ] Coverage floors raised if CI suggested it (`scripts/ci/coverage-floors.txt`)
- [ ] Runbooks updated if operational behaviour changed
