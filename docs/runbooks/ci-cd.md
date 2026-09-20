# Runbook — CI/CD and the release path

What runs, why it exists, what to do when it goes red, and which settings in
the GitHub UI this all assumes. If you are changing a workflow, read
[Branch protection](#branch-protection) first — several gates only mean
anything if the repository is configured to require them.

## The pipeline at a glance

| Workflow | Trigger | What it is for |
| --- | --- | --- |
| `verify.yml` | called by the two below | The test contract. Defined once so CI and Release cannot drift apart. |
| `ci.yml` | pull request, merge group, manual | Everything that must be true before a merge. |
| `release.yml` | push to `main`, manual | Publish an immutable image, deploy it, prove it is serving, roll back if not. |
| `nightly.yml` | 04:00 UTC daily, manual | Slow, Docker-heavy, and time-dependent checks — including the ones that go red with no commit. |

`verify.yml` is a reusable workflow, not a copy-paste. The previous `ci.yml`
and `release.yml` each carried their own `go vet && go build && go test -race`.
Two copies of a gate drift, and they drift the wrong way: the release copy is
the one nobody edits, so `main` ends up held to a weaker standard than the
branches feeding it.

## What each gate is actually for

### The TDD gates

These are the ones specific to this project. The rest of the pipeline is
ordinary good practice; these exist because the repository's own docs make
claims that nothing was checking.

**`test-first discipline`** (`scripts/ci/test-touched.sh`) fails a pull request
that changes production Go and touches no `_test.go` file. It cannot tell a
good test from a bad one — that is the next gate's job. What it catches is
behaviour changing while the suite stays word-for-word identical, which on a
project whose central promise is "the sim is provably the same as v1" must
never pass quietly.

*Escape hatch:* the `no-test-needed` label. Deliberately a label, not a
commit-message token: a label is applied on the pull request, shows in the
timeline, and someone other than the author can take it off again.

**`coverage of changed lines`** (`scripts/ci/diff-coverage.py`) asks the
sharper question. Total coverage is a lagging indicator — a repo this size at
72% absorbs an entirely untested 200-line feature without visibly moving. This
measures only the statements the branch added, and wants 85% of them covered.

That number is calibrated, not picked: the most recent real feature branch
(the seasonal-themes work, 390 changed statements) scores **87.9%** against it.
The bar sits where good work already clears it and an untested feature does
not.

**`coverage floors`** (`scripts/ci/coverage-gate.py`) is the ratchet. Per-package
minimums live in `scripts/ci/coverage-floors.txt`, one point under the measured
value when written. Raising a floor is free and CI tells you when to. Lowering
one takes a pull request that says why, with a reviewer on it — that is the
entire mechanism, and there is no override flag, because a lowered floor should
cost a conversation. A new package with no entry **fails**: untracked coverage
is how coverage quietly evaporates.

**`sentinel mutations`** (`scripts/ci/sentinel-mutations.py`) is the one worth
understanding properly.

`docs/tests/01` and `docs/tests/02` both end with acceptance criteria of the
form *"a deliberate rank-math bug and a deliberate filter bypass each fail
named tests — verified once, noted in PR"*. **Verified once** is the problem. A
suite that catches a bug in September can stop catching it in December: a test
gets rewritten to assert something weaker, a golden gets regenerated to match a
bug, a table loses the row that mattered. Nothing goes red, because nothing is
checking that the tests still bite. Green CI cannot distinguish a correct
implementation from an implementation whose tests have stopped looking at it.

Each sentinel injects a specific realistic bug, asserts the named test
**fails**, and reverts. Four run today:

| Sentinel | Injects | Must be caught by |
| --- | --- | --- |
| `leaderboard-tie-off-by-one` | `rank = i + 1` → `rank = i` | `TestRankMathTable`, the property test |
| `moderation-leet-fold-dropped` | the leet fold never fires | `TestMustBlockCorpusIsDeniedByEveryEvasionClass` |
| `sim-parity-payout-drift` | one extra coin per harvest | `TestScriptedSequenceMatchesV1Golden` |
| `sim-grow-time-drift` | one extra second per crop | three sim unit tests |

This is deliberately a short hand-written list, not general mutation testing: a
full mutation run is slow, noisy with equivalent mutants, and needs triage
nobody has time for. Four sentinels that each encode a real acceptance
criterion beat four hundred generated mutants that get ignored.

> **Known gap, found by the harness on its first run.** `docs/tests/01` claims
> "any sim edit fails at least one **parity** test". For a grow-time edit that
> is true of the sim *unit* suite but **not** of the v1 parity goldens: the
> scripted sequence advances 300s against crops that mature well inside that
> window, so a second of drift is absorbed before the first assertion. The sim
> is not unguarded — nine unit tests catch it — but the goldens specifically do
> not. Tightening the scripted fixture to harvest at the exact ready tick would
> close it. The sentinel is aimed at the tests that genuinely hold, with this
> noted inline, rather than at the tests the docs assume hold.

### The everyday gates

| Gate | Why |
| --- | --- |
| `gofmt` | Three files in the tree were not gofmt-clean when this was introduced. |
| `go vet`, and again with `-tags farm_test_v1_db` | The manual decode-everything hook must still *compile*, or it rots unnoticed until the day before a cutover when somebody needs it. |
| `go mod tidy -diff` | A `go.sum` drifting from `go.mod` is how a dependency gets added without review. `modernc.org/sqlite` was marked indirect while being imported directly. |
| `test -race -count=2 -shuffle=on` | `-count=2` is `docs/tests/01`'s own ask. `-shuffle` finds tests that only pass in declaration order. |
| Windows in the matrix | `docs/tests/01` asks for green on Windows and Linux. This is where that becomes enforced rather than asserted. |
| suite duration budget | `docs/tests/01` budgets 90s. Warns at 90, fails at 180. A slow suite is a *TDD* problem: once the red-green loop stops being fast, people stop running it and start guessing. |
| working tree unchanged | Catches a test that writes into the repo — a golden regenerated by a stray `-update`, a fixture mutated in place. Those tests pass forever and assert nothing. |
| container image build on every PR | The Dockerfile is production code. Building it only on `main` means finding it broken at the moment you wanted to ship. |
| `govulncheck` | Reachability-based, so it reports what this module actually calls. That precision is what makes it safe to block a merge on. |
| `gitleaks` | `.gitleaksignore` already existed, which means this was being run by hand. A control that depends on somebody remembering is not a control. |
| dependency review | The published image carries an Apache-2.0 obligation already (`NOTICE`); a copyleft dependency arriving unnoticed is a licensing problem, not a packaging one. |

## Running the gates locally

```sh
scripts/ci/verify.sh              # everything that does not need a base ref
scripts/ci/verify.sh origin/main  # ...plus test-touched and diff-coverage
```

Same commands, same order as CI. Only two things cannot run on a laptop: the
Windows leg of the matrix, and the container build.

## The release path

```
push to main
  └─ verify        (the same contract a PR faced, against the merge result)
  └─ publish       build → push :latest, :sha-<short>, :<version>
                   + SBOM + signed provenance + Trivy scan
  └─ deploy        SSM → scripts/deploy/remote-deploy.sh on the host
                     ├─ validate the probe against the RUNNING container
                     ├─ pull by DIGEST, retag local :latest
                     ├─ docker compose up -d --no-deps --pull never farm
                     ├─ smoke test: read the SSH banner
                     └─ on failure: retag previous digest, restart, exit 1
```

### Three things changed here, and why

**Deploys are by digest, not by tag.** `docker compose pull farm` fetches
whatever `:latest` points at *by the time the host gets round to pulling* —
which, on two merges in quick succession, is not necessarily the build that run
tested. The digest is. The host is handed the exact digest and re-tags it
locally as `:latest`, so the fleet compose file needs no change and stays the
fleet's to own.

**The smoke test reads the SSH banner.** `internal/server` feeds
`internal/version.Version` into wish's `Version` option, so the identification
string on the wire is literally `SSH-2.0-2.4.0` — and the arcade router already
health-probes it. Checking that one line proves the process started, bound its
port, finished enough init to accept TCP, *and* is the build just shipped. The
previous check — `sleep 10` then `docker compose ps --status running` — proved
only that the container had not exited yet, which a container serving the wrong
build or wedged mid-init also satisfies.

**The deploy script lives in the repo.** `scripts/deploy/remote-deploy.sh`, not
a JSON string array inside the workflow: it can be reviewed as a diff, run by
hand during an incident without reconstructing JSON, and a shell script with
conditionals has no quoting story that survives being embedded in an SSM
`commands=[...]` list.

It also **validates its own instrument**. Before touching anything it probes
the container that is *already running and known good*. If it gets no banner
from that, the probe is broken — not the build — and it stops, changes nothing,
and says so. A health check that cannot work would otherwise report every
deploy as broken and roll back perfectly good builds, which is worse than
having no smoke test at all because it looks like a real failure.

### When a deploy fails

The job is red and the log says which of two states the host is in:

1. **Rolled back** — `rolled back to <digest> and it is serving`. Players are
   on the previous build. Fix forward; there is no urgency beyond the usual.
2. **Down** — `FATAL: rollback ... did not come back up either`. Players
   cannot connect. Go to the host:

```sh
cd /srv/ssharcade
docker compose logs --tail 200 farm
docker tag ghcr.io/mynameis-nigel/ssh-farm:<known-good-sha-tag> \
           ghcr.io/mynameis-nigel/ssh-farm:latest
docker compose up -d --no-deps --pull never farm
```

Every build is tagged `sha-<short>` and `<version>`, so a known-good target is
always nameable. To re-run a deploy without a new commit, use **Run workflow**
on Release.

`docs/framework/03` is still the source of truth for the *denylist
provisioning* prerequisite: `farm` runs with `FARM_REQUIRE_MODERATION=true` and
crash-loops by design if `/srv/ssharcade/private/farm/denylist.toml` is absent.
A deploy failing on a fresh host is usually that, and the container logs say so.

## Nightly

Slow and Docker-heavy checks, plus the category most projects miss: **the ones
that can go red with no commit at all.** A CVE disclosed against a dependency,
or against the alpine base of an image not rebuilt in six weeks, makes a
*shipped* build unsafe while every workflow stays green, because nothing ran.

`docs/tests/02` already asks for the durability drills to run "on a schedule,
not just unit tests". The scripts have been sitting in `scripts/restore-drill/`
waiting for something to call them — `nightly.yml` is that something, running
all four (dev-mode, restore, kill drill with measured RPO, restore-to-scratch).

Failures e-mail the repository owner through GitHub's own scheduled-workflow
notification. There is deliberately no bot filing issues: a nightly that opens
a duplicate issue every night gets muted, and a muted alarm is worse than none.

## Branch protection

Several gates above mean nothing unless the repository requires them.
**Settings → Branches → `main`:**

- Require a pull request before merging
- Require status checks to pass, and select:
  - `verify / static analysis`
  - `verify / test (ubuntu-latest)`
  - `verify / test (windows-latest)`
  - `verify / coverage`
  - `verify / sentinel mutations`
  - `verify / lint`
  - `test-first discipline`
  - `coverage of changed lines`
  - `supply chain`
- Require branches to be up to date before merging
- Require conversation resolution before merging

**Settings → Actions → General:**

- Fork pull request workflows: **Require approval for all external
  contributors.** This is the correct control for the risk the old `ci.yml`
  comment was worried about — see the header of `ci.yml` for why the
  `pull_request` trigger itself is the safe one.
- Workflow permissions: **Read repository contents**. Jobs that need more
  (`packages: write`, `id-token: write`) request it per job, and only
  `release.yml` does.

**Settings → Advanced Security → Dependency graph: enable it.** Two things in
this pipeline are inert without it. `ci.yml`'s dependency-review step fails
with "Dependency review is not supported on this repository" — a repository
setting talking, not a finding about the diff — and is marked
`continue-on-error` until the toggle is on; delete that line once it is. The
same toggle turns on Dependabot **security alerts**, so until then
`dependabot.yml` only does half its job: it will raise version bumps on a
schedule, but nothing will tell you that a dependency you already have has
become vulnerable. `govulncheck` in CI covers the reachable subset of that and
is the stronger signal, but it only runs when something triggers a workflow.

**Settings → Environments → `production`:** add required reviewers if you want
a human gate between a green `main` and a live deploy. The `deploy` job already
targets this environment, so the approval prompt appears with no workflow
change.

## Things deliberately not done

- **Actions are not all SHA-pinned.** `release.yml`'s original three pins are
  kept verbatim. The actions added here use major-version tags, because pinning
  to a SHA that has not been verified is how you ship a pipeline that cannot
  run. Pin them in one pass with [`pinact`](https://github.com/suzuki-shunsuke/pinact)
  (`pinact run`), then let the Dependabot `github-actions` entry keep them
  fresh. This matters more here than on most repositories: a compromised action
  runs inside the job holding `packages: write` and the OIDC deploy role.
- **No multi-arch image.** The fleet is one x86 EC2 host. Add `platforms:` to
  the build step if that changes.
- **Lint is a ratchet, not a cleanup.** Pull requests are gated on new issues
  only; the existing backlog (20 findings at introduction) is reported nightly
  so it stays a number someone can watch go down. Introducing a linter any
  other way means a 40-file cleanup commit riding along with unrelated work.
