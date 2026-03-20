# Ralph Loop Prompt — mnemonic-enricher Phase 01

You are executing one Ralph loop cycle for the `mnemonic-enricher` repository.
Follow the repository-specific rules below.

## Objective

Complete exactly one unchecked cycle from the active PRD, verify it, update the
PRD state, append a progress entry, and stop.

## Inputs

You will be given:

- the active PRD
- the current progress log, if one exists
- the repository working tree

The active PRD for this project is:

- `docs/plans/phase-01/PRD.md`

The primary supporting document is:

- `docs/plans/phase-01/phase-01.md`

The canonical progress log path for this repository is:

- `docs/plans/phase-01/progress.txt`

If `docs/plans/phase-01/progress.txt` does not exist, create it when completing
the first cycle.

## Non-Negotiable Rules

1. Execute exactly one PRD cycle.
2. Work only on the first unchecked `- [ ]` cycle in the PRD.
3. Do not skip ahead.
4. Do not combine multiple cycles into one run.
5. Search the repository before editing. Do not assume code or files are missing.
6. Respect the cycle's `Agent`, `Files`, `Steps`, and `Verify` fields.
7. Keep changes scoped to the selected cycle.
8. Run verification before marking the cycle complete.
9. Update the PRD and progress log only after the cycle passes verification.
10. Stop after finishing that one cycle.

## Repo-Specific Build and Test Rules

- Go module root is `src/`; all `go` commands must run from `src/` (e.g. `cd src && go build ./...`)
- Full CI build: `make build` from the project root (not `src/`) — runs unit tests, builds Docker image, runs e2e tests
- All commits must be GPG-signed: use `git commit -S` — never use `--no-gpg-sign`
- If a pre-commit hook fails, fix the underlying issue and recommit; do not bypass hooks with `--no-verify`
- Run `go vet ./...` and `golangci-lint run` after any source change (from `src/`)
- Do not modify files outside the cycle's `Files` list unless they are directly required to make the build pass

## Ralph Loop Procedure

### Step 1: Read the PRD and select the cycle

- Open `docs/plans/phase-01/PRD.md`.
- Find the first unchecked `- [ ]` cycle under `## Implementation Plan`.
- Extract: cycle title, description, `Agent`, `Files`, `Steps`, `Verify`, `Done`.

If no unchecked cycle exists, stop and report that the PRD is complete.

### Step 2: Read supporting context

- Read `docs/plans/phase-01/phase-01.md` for design decisions relevant to the selected cycle.
- Read the progress log if it exists.
- Search the codebase before editing anything — do not assume files are missing or present.

### Step 3: Plan narrowly

- Form a minimal plan that completes only the selected cycle.
- Do not plan future cycles.
- Do not expand scope beyond the listed files and directly necessary support files.

### Step 4: Delegate or execute

- Delegate to the cycle's named `Agent` subagent.
- Keep the implementation bounded to the selected cycle.

### Step 5: Verify

Run the cycle's `Verify` command exactly as written.

Go baseline (run after every source change, from `src/`):

1. `go vet ./...` — no vet errors
2. `go build ./...` — compiles cleanly
3. `go test ./...` — all unit tests pass

If any check fails:

- fix the problem if it is within the cycle scope
- rerun verification
- do not mark the cycle complete until all checks pass

### Step 6: Commit and tag the changes

After verification passes, stage and commit all files produced or modified by the cycle:

- stage only the files listed in the cycle's `Files` field and any directly necessary support files
- use a concise commit message naming the cycle number and title (e.g. `Cycle 1 - Fix module path and remove routes.go`)
- commit must be GPG-signed: `git commit -S`
- do not use `--no-verify` or `--no-gpg-sign`
- if the commit fails due to a hook, fix the issue and recommit

After the commit succeeds, create a git tag for this cycle:

```
VERSION=$(git describe --tags --abbrev=0 2>/dev/null || echo "v0.0.0")
CYCLE=$(printf "%02d" <cycle-number>)
git tag "${VERSION}-cycle${CYCLE}"
```

For example, if the current tag is `v1.0.0` and this is Cycle 3, the tag is `v1.0.0-cycle03`.

### Step 7: Update project records

After the commit and tag succeed:

- change the selected PRD cycle marker from `- [ ]` to `- [x]`
- append a concise entry to `docs/plans/phase-01/progress.txt`
- stage and commit the updated PRD and progress log as a follow-up commit (also GPG-signed)

Each progress entry must include:

- cycle number and title
- date (YYYY-MM-DD)
- summary of work completed
- verification command run and its outcome
- any important follow-up notes

### Step 8: Report and stop

At the end of the loop:

- report what changed
- report what verification passed
- report the next unchecked cycle
- stop

Do not continue into the next cycle.

## Failure Modes to Avoid

- completing more than one cycle in one run
- editing files unrelated to the selected cycle
- skipping repository search and duplicating existing code
- marking a cycle complete before verification passes
- bypassing GPG signing or pre-commit hooks
- skipping the cycle tag (`${VERSION}-cycle${CYCLE}`) after a successful commit
- removing packages listed in "Files to keep" in `phase-01.md`
- changing enrichment pipeline logic (`processChunkJob`, `runGraphPipeline`, etc.) — these are out of scope

## Output Contract

Your final response for a completed loop must contain:

- the completed cycle number and title
- the files changed
- the verification command run and its outcome
- the next unchecked cycle

If you could not complete the cycle, state exactly why and do not mark it done.
