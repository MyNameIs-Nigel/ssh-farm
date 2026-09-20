#!/bin/sh
# Fail a change that edits production Go code without touching a single test.
#
# This is the cheapest TDD gate there is, and the bluntest. It cannot tell a
# good test from a bad one — diff-coverage.py does that. What it catches is the
# specific failure this project cares about: behaviour changing while the test
# suite stays word-for-word identical, which on a repo whose whole premise is
# "the sim is provably the same as v1" is the one thing that must never pass
# silently.
#
# Escape hatch: the `no-test-needed` PR label. Deliberately a label and not a
# commit-message token — a label is applied on the PR, is visible in the
# timeline, and someone other than the author can take it off again.
#
# Usage: test-touched.sh <base-ref> [labels-json]
set -eu

BASE="${1:?usage: test-touched.sh <base-ref> [labels-json]}"
LABELS="${2:-}"

case "$LABELS" in
*'"no-test-needed"'*)
	echo "test-touched: skipped — PR carries the 'no-test-needed' label."
	exit 0
	;;
esac

CHANGED=$(git diff --name-only --diff-filter=d "$BASE...HEAD" -- '*.go')

# Production code = any .go file that is not itself a test.
PROD=$(printf '%s\n' "$CHANGED" | grep -v '_test\.go$' | grep '\.go$' || true)
TESTS=$(printf '%s\n' "$CHANGED" | grep '_test\.go$' || true)

if [ -z "$PROD" ]; then
	echo "test-touched: no production Go files changed — nothing to enforce."
	exit 0
fi

if [ -n "$TESTS" ]; then
	echo "test-touched: OK — production code and tests both changed."
	echo "  production:"
	printf '    %s\n' $PROD
	echo "  tests:"
	printf '    %s\n' $TESTS
	exit 0
fi

cat >&2 <<MSG
test-touched: FAILED

These production files changed and no _test.go file changed with them:

$(printf '  %s\n' $PROD)

On a test-driven project the test moves first. If the behaviour genuinely did
not change — a rename, a comment, a dependency bump — say so and apply the
'no-test-needed' label to the pull request. If it did change, the test that
proves it is the deliverable, not an extra.
MSG
exit 1
