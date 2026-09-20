#!/bin/sh
# Run the pull-request gates locally, in the order CI runs them.
#
# The point is that CI should never be the first place you learn something is
# wrong. Every gate below is the same command the workflow runs, so a green
# run here means a green run there — with two exceptions, noted as they come
# up: the Windows leg of the matrix, and the container build.
#
# Usage:
#   scripts/ci/verify.sh            # everything except diff coverage
#   scripts/ci/verify.sh origin/main  # ...plus the gates that need a base ref
set -eu
cd "$(dirname "$0")/../.."

BASE="${1:-}"
FAILED=""

step() {
	printf '\n\033[1m== %s\033[0m\n' "$1"
}

check() {
	name="$1"
	shift
	if "$@"; then
		printf '\033[32mPASS\033[0m %s\n' "$name"
	else
		printf '\033[31mFAIL\033[0m %s\n' "$name"
		FAILED="$FAILED\n  $name"
	fi
}

step "gofmt"
unformatted=$(gofmt -l .)
if [ -n "$unformatted" ]; then
	echo "$unformatted" | sed 's/^/  /'
	echo "fix with: gofmt -w ."
	FAILED="$FAILED\n  gofmt"
else
	printf '\033[32mPASS\033[0m gofmt\n'
fi

step "go vet"
check "go vet" go vet ./...
check "go vet (farm_test_v1_db)" go vet -tags farm_test_v1_db ./...

step "go.mod"
check "go mod tidy -diff" go mod tidy -diff
check "go mod verify" go mod verify

step "build"
check "go build" go build ./...

step "test -race -count=2 -shuffle=on"
check "go test" go test -race -count=2 -shuffle=on ./...

step "coverage floors"
if go test -covermode=atomic -coverprofile=coverage.out ./... >/dev/null; then
	check "coverage-gate" python3 scripts/ci/coverage-gate.py coverage.out scripts/ci/coverage-floors.txt
else
	FAILED="$FAILED\n  coverage measurement"
fi

step "sentinel mutations"
check "sentinels" python3 scripts/ci/sentinel-mutations.py

if [ -n "$BASE" ]; then
	step "gates that need a base ref ($BASE)"
	check "test-touched" sh scripts/ci/test-touched.sh "$BASE"
	check "diff-coverage" python3 scripts/ci/diff-coverage.py --profile coverage.out --base "$BASE" --min 85
else
	printf '\nSkipping test-touched and diff-coverage: pass a base ref to run them,\n'
	printf 'e.g. scripts/ci/verify.sh origin/main\n'
fi

rm -f coverage.out

if [ -n "$FAILED" ]; then
	printf '\n\033[31mFAILED:\033[0m%b\n' "$FAILED"
	echo
	echo "Not run here (CI only): the Windows leg of the test matrix, and the"
	echo "container image build. Both need what a laptop usually has not got."
	exit 1
fi

printf '\n\033[32mAll local gates passed.\033[0m\n'
echo "Still CI-only: the Windows test leg and the container image build."
