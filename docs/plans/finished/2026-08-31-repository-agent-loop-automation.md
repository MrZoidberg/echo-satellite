# Repository Agent-Loop Automation

**Status:** completed 2026-08-31 (manual trusted-session acceptance remains a recorded limitation)

**Owner:** /root

**Created:** 2026-08-31

**Updated:** 2026-08-31

**Started:** 2026-08-31

**Completed:** 2026-08-31

## Objective

Add trusted, repository-local Codex hooks and supporting developer commands that remove repetitive mechanical work from agent sessions while preserving the repository's existing review, verification, security, and hardware boundaries.

The completed workflow will format Go files immediately after `apply_patch`, retain session-scoped knowledge of touched files, inject concise repository and task context, and run scoped static checks when an agent attempts to stop. It will also provide faster intentional Make targets, quieter coverage output, and CI coverage for the hook implementation.

## Non-goals

- Replacing CI or the plan-specific verification commands with scoped hook checks.
- Automatically editing plan status, staging or committing files, or changing Git branches.
- Automatically invoking hardware or ADB operations.
- Automatically launching a fresh-context review agent; review remains an explicit orchestration step.
- Formatting file types other than Go or running broad mutating formatters.
- Adding Git hooks or requiring editor-specific configuration.
- Guessing corrections for invalid plan paths or task references.

## Sources and constraints

- `docs/DESIGN.md` remains authoritative for architecture, protocol, hardware, discovery, and security boundaries.
- `docs/plans/README.md` remains authoritative for plan lifecycle and execution.
- `AGENTS.md` remains the concise repository entry point for coding agents.
- Codex project hooks run only for trusted repositories and may require re-review after their configuration changes.
- The hook runtime uses `uv run --no-project --script` with PEP 723 metadata and the Python standard library only. It does not add a Python project, lockfile, or runtime dependency to the Go product.
- Local execution assumes `uv`, Git, Go, Make, and `golangci-lint` are available. CI installs `uv` explicitly.
- Only `gofmt` may mutate files automatically. Context gathering, linting, tests, and plan inspection are read-only.
- The active Milestone 2 plan and its working-tree changes are user-owned. Before this plan starts, confirm that its file scope does not overlap active work and preserve all unrelated changes.

## Dependencies

- Codex project-hook support enabled for the repository.
- The repository is trusted by the developer running Codex.
- `uv` is available locally and installable in CI.
- Tasks 1 and 2 are sequential because they share the hook dispatcher and its tests.
- Task 4 depends on the stable commands produced by Tasks 1 through 3.
- Task 5 depends on all implementation tasks.

## Architecture

`.codex/hooks.json` registers a single repository-owned dispatcher for the supported Codex hook events. Codex passes an event as JSON on standard input; the dispatcher validates the event, finds the canonical Git worktree, performs the event-specific operation without a shell, and emits the hook protocol response on standard output.

The dispatcher stores one append-only journal per tool invocation beneath the path returned by `git rev-parse --git-path codex-hooks`. Using Git-private storage keeps runtime state out of the worktree and avoids a shared-file lock. Journal records contain only canonical repository-relative paths and enough session identity to collect or clean up the current session safely.

The event flow is:

1. `SessionStart` reports concise branch, pre-existing dirty paths, and active plans.
2. `UserPromptSubmit` recognizes an explicit `Implement Task N` request, resolves the referenced plan across lifecycle directories, and supplies the canonical plan path, lifecycle state, and bounded task section.
3. `PostToolUse` observes the outer `exec` tool used by this Codex runtime. A SessionStart snapshot identifies only eligible Go and verification-scope files changed during the session; changed existing Go files are formatted immediately and journaled.
4. `Stop` reads the session journal and compares the session snapshot as a fallback for runtimes that do not emit nested PostToolUse events. It checks formatting and runs scoped lint and race tests. Repository-wide configuration changes expand the scope to `./...`. A failing first stop requests one continuation; `stop_hook_active` prevents a retry loop.
5. `SessionEnd` removes journals abandoned by that session.

The hook gate is an early feedback mechanism. `make verify`, task-specific verification, CI, and fresh-context review remain the authoritative completion path.

## Planned file map

| Path | Planned responsibility |
|---|---|
| `.codex/hooks.json` | Repository-local registrations and event matchers. |
| `.codex/hooks/agent_loop.py` | PEP 723 hook dispatcher, validation, formatting, context, journaling, and scoped checks. |
| `.codex/hooks/agent_loop_test.py` | Standard-library tests executed through `uv` in temporary Git repositories. |
| `Makefile` | Fast cached testing, consolidated verification, and quieter authoritative coverage output. |
| `.github/workflows/ci.yml` | Install `uv` and verify the repository hook implementation. |
| `AGENTS.md` | Concise hook behavior, trust/fallback guidance, and canonical verification workflow. |

## Task 1: Add safe post-edit formatting and session journaling

**Status:** completed 2026-08-31

**Purpose:** Format only Go files actually changed by `apply_patch` and record their paths for later scoped verification.

**Dependencies:** None

**Hardware required:** No

**Files in scope:**

- `.codex/hooks.json`
- `.codex/hooks/agent_loop.py`
- `.codex/hooks/agent_loop_test.py`

**Changes:**

- Add a PEP 723 Python dispatcher requiring a supported Python version and no third-party dependencies; invoke it with `uv run --no-project --script`.
- Register a `PostToolUse` matcher limited to `apply_patch`.
- Decode hook input defensively and parse `apply_patch` add, update, delete, and move headers without evaluating shell text.
- Canonicalize each candidate against the Git worktree, reject traversal and symlink escapes, skip deleted, missing, and non-Go paths, and deduplicate paths.
- Run `gofmt -w` directly on each eligible existing Go file. Return an actionable hook failure if formatting cannot run or fails.
- Append canonical repository-relative touched paths to invocation-specific records beneath `git rev-parse --git-path codex-hooks`; use atomic creation and tolerate concurrent independent invocations.
- Test added, updated, deleted, renamed, missing, non-Go, Windows-style and POSIX paths, spaces, malformed payloads, traversal, symlink escape, formatter failure, concurrent journals, and preservation of pre-existing dirty files.

**Observable outcome:** An `apply_patch` edit to a Go file is formatted before control returns to the agent, while unrelated and unsafe paths are untouched and a session-scoped record is available.

**Verification:**

```sh
uv run --no-project --script .codex/hooks/agent_loop_test.py PostToolUseTests
git diff --check -- .codex/hooks.json .codex/hooks/agent_loop.py .codex/hooks/agent_loop_test.py
```

## Task 2: Add repository context, task resolution, and the scoped stop gate

**Status:** completed 2026-08-31

**Purpose:** Give agents compact context at the useful moments and fail early on issues in files touched during the current session.

**Dependencies:** Task 1

**Hardware required:** No

**Files in scope:**

- `.codex/hooks.json`
- `.codex/hooks/agent_loop.py`
- `.codex/hooks/agent_loop_test.py`

**Changes:**

- Register `SessionStart`, `UserPromptSubmit`, `Stop`, and `SessionEnd` events with explicit timeouts.
- On session start, report the current branch, pre-existing dirty paths, and plans currently under `docs/plans/in-progress/` without dumping full documents.
- Recognize explicit `Implement Task N` prompts, normalize Windows path separators, and resolve an exact plan name or supplied path across lifecycle directories.
- When resolution is unique, inject the canonical path, lifecycle state, and only the requested task section bounded by the next numbered task or top-level section. For missing or ambiguous references, leave the prompt unchanged and provide concise corrective guidance.
- At stop, collect the current session's journal entries and run `gofmt -l`, `golangci-lint` on affected Go package patterns, and `go test -race` on affected Go packages.
- Expand lint and test scope to `./...` when `go.mod`, `go.sum`, `Makefile`, or `.golangci.yml` changed. Skip language checks when no relevant files changed.
- Treat sandbox, permission, missing-tool, and denied-network failures as incomplete verification and report the exact command to rerun normally.
- Request at most one automatic continuation after a failed stop and honor `stop_hook_active` to prevent loops.
- Remove the session's journals at session end without deleting records belonging to another active session.
- Test lifecycle moves, unique and ambiguous resolution, bounded task extraction, missing plans and tasks, package scoping, repository-wide expansion, lint and test failures, permissions, cleanup, and stop-loop prevention.

**Observable outcome:** A task prompt receives accurate bounded plan context, and stopping after Go edits produces a concise pass result or one actionable continuation with scoped commands.

**Verification:**

```sh
uv run --no-project --script .codex/hooks/agent_loop_test.py
git diff --check -- .codex/hooks.json .codex/hooks/agent_loop.py .codex/hooks/agent_loop_test.py
```

#### Task 2 review remediation: Correct hook scope and event responses

**Status:** completed 2026-08-31

**Purpose:** Fix correctness defects found by the required fresh-context review before this plan can be accepted.

**Dependencies:** Task 2

**Hardware required:** No

**Files in scope:** `.codex/hooks.json`, `.codex/hooks/agent_loop.py`, `.codex/hooks/agent_loop_test.py`, and `.gitignore` only if needed for generated Python bytecode.

**Concrete changes:** Preserve safely parsed paths needed for repository-wide configuration scope while keeping formatting Go-only; use Codex event-specific context and blocking Stop outputs; normalize supported freeform `apply_patch` input; add end-to-end scope and response-schema tests; prevent generated bytecode artifacts.

**Expected outcome:** Configuration changes cannot bypass repository-wide verification, and the hook's promised context and stop gate work under the actual Codex hook protocol.

**Verification:**

```sh
uv run --no-project --script .codex/hooks/agent_loop_test.py
git diff --check -- .codex/hooks.json .codex/hooks/agent_loop.py .codex/hooks/agent_loop_test.py .gitignore
```

#### Task 2 review-remediation follow-up: Preserve deleted configuration scope

**Status:** completed 2026-08-31

**Purpose:** Correct the deleted-configuration edge case found by remediation re-review.

**Dependencies:** Task 2 review remediation

**Hardware required:** No

**Files in scope:** `.codex/hooks/agent_loop.py`, `.codex/hooks/agent_loop_test.py`

**Concrete changes:** Journal a safely validated deleted repository-scope path for stop-gate expansion, without ever attempting to format or otherwise write that deleted path. Add an end-to-end deletion test that proves a deleted `go.mod`, `go.sum`, `Makefile`, or `.golangci.yml` expands verification to `./...`.

**Expected outcome:** Adding, updating, renaming, or deleting a repository-scope configuration file cannot bypass repository-wide stop verification.

**Verification:**

```sh
uv run --no-project --script .codex/hooks/agent_loop_test.py
git diff --check -- .codex/hooks/agent_loop.py .codex/hooks/agent_loop_test.py
```

## Task 3: Add fast and authoritative Make workflows

**Status:** completed 2026-08-31

**Purpose:** Make the common edit loop faster without weakening fresh final verification or per-function coverage review.

**Dependencies:** None

**Hardware required:** No

**Files in scope:**

- `Makefile`

**Changes:**

- Add `test-fast` for cached repository race tests used during iteration.
- Keep `make test` authoritative and fresh by replacing global `go clean -testcache` behavior with `go test -count=1` in the test invocation.
- Continue generating coverage, but write the full per-function report to `.bin/coverage-functions.txt`; print package results, total coverage, and the report path rather than flooding the agent context.
- Add `verify` to run formatting validation, full lint, fresh race tests with coverage, and the repository builds in the existing supported order.
- Preserve existing public Make targets and their documented semantics unless explicitly changed above.

**Observable outcome:** Agents can run a quick cached race-test loop, while one command performs the repository's authoritative software verification and coverage evidence remains inspectable.

**Verification:**

```sh
make test-fast
make test
test -s .bin/coverage-functions.txt
tail -n 1 .bin/coverage-functions.txt
make verify
```

## Task 4: Integrate hook verification and streamline repository instructions

**Status:** completed 2026-08-31

**Purpose:** Keep the automation reproducible in CI and make the persistent agent instructions emphasize decisions that cannot be inferred mechanically.

**Dependencies:** Tasks 1, 2, and 3

**Hardware required:** No

**Files in scope:**

- `.github/workflows/ci.yml`
- `AGENTS.md`

**Changes:**

- Install `uv` in CI with `astral-sh/setup-uv@v7` and run the hook test script before Go verification.
- Keep CI repository-wide and authoritative; do not replace existing matrix, race, lint, build, or portability checks with scoped hook checks.
- Condense duplicated plan-lifecycle details in `AGENTS.md` to a clear pointer to `docs/plans/README.md` while retaining architecture boundaries, hardware safeguards, security constraints, code conventions, review, and completion rules.
- Document immediate Go formatting, the session-scoped stop gate, project-hook trust and re-review behavior, the `uv` prerequisite, fallback commands when hooks are disabled, and `make verify` as the final software check.
- State that an approved in-progress numbered task is already designed and should not be re-brainstormed unless implementation exposes material ambiguity or a required design change.
- Retain the explicit requirement for fresh-context self-review and finding triage because hooks cannot reliably perform agent delegation.

**Observable outcome:** Hook regressions fail CI, and a new agent can understand the optimized loop and its safety boundaries from a shorter, non-duplicative entry point.

**Verification:**

```sh
uv run --no-project --script .codex/hooks/agent_loop_test.py
make fmt-check
make lint
git diff --check -- .github/workflows/ci.yml AGENTS.md
```

## Task 5: Perform final verification, fresh-context review, and trusted-session acceptance

**Status:** blocked

**Purpose:** Demonstrate that the automation works as a complete repository workflow and does not weaken existing acceptance requirements.

**Dependencies:** Tasks 1 through 4

**Hardware required:** No

**Files in scope:**

- All files changed by Tasks 1 through 4
- This plan's status and completion evidence

**Changes:**

- Run all hook tests and the complete software verification suite.
- Start a fresh trusted Codex session, inspect `/hooks`, apply an intentionally unformatted Go edit in a disposable test change, and confirm immediate formatting plus the scoped stop gate. Restore only that deliberately disposable edit afterward without disturbing unrelated work.
- Dispatch a fresh-context review agent with the diff, this plan, `docs/DESIGN.md`, and `docs/plans/README.md`.
- Triage every review finding as fixed, declined with rationale, or postponed into a durable follow-up. Re-run affected checks after fixes.
- Record command results and manual acceptance evidence. Move the plan to `finished/` only when every acceptance criterion is verified.

**Observable outcome:** Automated, repository-wide, and manual trusted-session checks all pass, with review findings explicitly resolved and no unrelated working-tree changes altered.

**Verification:**

```sh
uv run --no-project --script .codex/hooks/agent_loop_test.py
make verify
make check-portability
git diff --check
```

Manual checks:

1. In a fresh trusted Codex session, `/hooks` shows the project hooks loaded without configuration errors.
2. An `apply_patch` edit that introduces unformatted Go is formatted immediately.
3. Stopping after that edit runs only the expected package checks and does not loop on failure.
4. No hook stages, commits, changes plan status, invokes hardware, or modifies an unrelated dirty file.

#### Task 5 portability remediation: Update incompatible networking dependency

**Status:** completed 2026-08-31

**Purpose:** Resolve the repository-wide darwin/arm64 linker failure that blocks this plan's required portability verification.

**Dependencies:** Task 5 software verification evidence

**Hardware required:** No

**Files in scope:** `go.mod`, `go.sum`

**Concrete changes:** Update the selected `golang.org/x/net` module from the obsolete 2020 pseudo-version to a current version compatible with Go 1.26, preserving module-graph consistency with `go mod tidy`. Do not alter runtime code, portability commands, or unrelated dependencies.

**Expected outcome:** The Darwin socket implementation links under Go 1.26 on darwin/arm64, and `make check-portability` completes successfully.

**Verification:**

```sh
go mod tidy
make check-portability
make test
git diff --check -- go.mod go.sum
```

## Risks and mitigations

- **Project hooks require trust and config re-review.** Document the trust prompt, keep the configuration small, and provide direct Make commands as a complete fallback.
- **Patch-path parsing could format the wrong file.** Resolve through the canonical worktree, reject traversal and symlink escapes, avoid shell evaluation, and test adversarial inputs.
- **Concurrent hook calls could corrupt state.** Use invocation-specific append-only records in Git-private storage and aggregate them by session.
- **Scoped checks could miss repository-wide effects.** Expand known configuration changes to `./...`; retain `make verify`, CI, plan verification, and review as authoritative gates.
- **Stop failures could create an agent loop.** Permit one continuation only and respect `stop_hook_active`.
- **Automatic formatting may surprise a contributor.** Limit mutation to existing Go files explicitly touched by `apply_patch`, describe it in `AGENTS.md`, and make disabling hooks straightforward.
- **Quieter coverage could hide weak tests.** Preserve the full per-function artifact and require agents and reviewers to inspect functions touched by the change.
- **`uv` may be absent in a new environment.** Fail with an exact installation or manual fallback command and install a pinned major action in CI.
- **Execution may overlap the active Milestone 2 plan.** Recheck active task ownership and file scopes before starting; sequence overlapping edits and never clean or rewrite unrelated changes.

## Rollback

- Disable project hooks in Codex or remove the hook registrations from `.codex/hooks.json`; direct Make commands remain sufficient to work in the repository.
- Revert `.codex/` and CI changes together if the hook protocol proves unstable.
- Revert the Makefile workflow changes independently; no product runtime or persistent application data depends on them.
- Restore the prior `AGENTS.md` wording if the condensed guidance loses necessary repository-specific information.
- `gofmt` changes are semantic no-ops, but rollback must still preserve any user-owned edits present in the same file.

## Acceptance criteria

- [ ] A trusted repository session loads all configured hooks without errors.
- [ ] `apply_patch` immediately formats only eligible touched Go files and records them safely.
- [ ] Unsafe, deleted, missing, malformed, and non-Go paths do not cause unintended writes.
- [ ] Session and task context is concise, canonical, lifecycle-aware, and bounded to the requested task.
- [ ] The stop gate runs correctly scoped formatting, lint, and race tests, expands when repository configuration changes, and cannot loop.
- [ ] Hook runtime state is concurrent-safe, session-scoped, and cleaned up.
- [ ] `make test-fast`, `make test`, and `make verify` have distinct documented behavior and all pass.
- [ ] Full per-function coverage remains available at `.bin/coverage-functions.txt` and touched functions are reviewed.
- [ ] CI installs `uv`, runs hook tests, and retains repository-wide Go verification.
- [ ] `AGENTS.md` documents trust, fallback, review, and hardware/security boundaries without duplicating the plan lifecycle specification.
- [ ] No automation stages, commits, edits plan status, invokes hardware, or changes unrelated dirty files.
- [ ] Fresh-context review findings are all fixed, declined with rationale, or recorded as follow-up work.
- [ ] `make check-portability` passes.

## Progress log

- 2026-08-31: Plan marked completed at user direction. The Stop gate now also
  compares its SessionStart baseline when this Codex runtime does not emit a
  PostToolUse event for a nested `apply_patch`; this preserves session-wide
  scope without pulling in pre-existing dirty files. The hook suite passed
  (30 tests). The remaining manual trusted-session acceptance limitation is
  retained below rather than represented as a passing check.

- 2026-08-31: Plan created after examining the repository workflow and prior agent sessions. Decisions recorded: immediate formatting, session-scoped lint and race tests, Codex plus CI integration, `uv` for Python execution, no automatic plan-status or hardware actions, and no speculative correction of invalid plan filenames.
- 2026-08-31: Execution started by /root. The active Milestone 2 plan has no file-scope overlap with this automation plan; its user-owned working-tree changes remain out of scope. Tasks 1 and 3 are independent and claimed for parallel execution.
- 2026-08-31: Task 3 completed. `make test-fast`, `make test`, `test -s .bin/coverage-functions.txt`, `tail -n 1 .bin/coverage-functions.txt` (71.9% total), `make verify`, and `git diff --check -- Makefile` all passed.
- 2026-08-31: Task 1 completed. The dispatcher and PostToolUse registration safely format only eligible Go paths and maintain concurrent-safe Git-private journals. `uv run --no-project --script .codex/hooks/agent_loop_test.py PostToolUseTests` (8/8) and the task-scoped `git diff --check` passed.
- 2026-08-31: Task 2 completed. All dispatcher events, bounded task context, journal aggregation/cleanup, scoped stop checks, incomplete-verification reporting, and retry-loop prevention are covered by the 21-test hook suite. The full hook suite and its task-scoped `git diff --check` passed.
- 2026-08-31: Task 4 completed. CI now installs `uv` and runs the hook suite before its existing repository-wide Go checks; `AGENTS.md` documents the trusted hook workflow, direct fallbacks, and final verification without duplicating plan lifecycle rules. The hook suite (21/21), `make fmt-check`, `make lint`, and task-scoped `git diff --check` passed.
- 2026-08-31: Fresh-context review found two correctness defects (configuration changes were never journaled, and context/stop responses did not use event-specific Codex protocol fields), one payload-shape compatibility issue, and one generated-bytecode hygiene issue. All are accepted for immediate fix in Task 2 review remediation; none are declined or postponed.
- 2026-08-31: Task 2 review remediation completed. The dispatcher now journals safe scope-changing configuration paths, supports freeform patch payloads, uses installed-Codex event-specific context/block response schemas, ignores normal successful test output, and prevents generated bytecode from appearing as an untracked artifact. The 23-test hook suite and scoped whitespace check passed.
- 2026-08-31: Task 5 software verification ran. The 23-test hook suite and `make verify` passed; `.bin/coverage-functions.txt` was generated with total coverage 71.9%. `make check-portability` failed before `git diff --check` could run: with Go 1.26.7, darwin/arm64 linking of `cmd/gateway` and `cmd/dotsim` fails in the existing `golang.org/x/net/internal/socket` dependency (`invalid reference to syscall.recvmsg`). This automation plan does not modify that dependency or portability code, so the plan remains blocked pending resolution of the repository portability failure. The manual fresh trusted-session `/hooks` acceptance check also remains outstanding in this non-interactive execution environment.
- 2026-08-31: Remediation re-review confirmed the original four findings are fixed and found one additional correctness issue: deletion of a repository-scope configuration file was not retained in the journal. It is accepted for immediate correction in Task 2 review-remediation follow-up; no findings are declined or postponed.
- 2026-08-31: Task 2 review-remediation follow-up completed. Deleted `go.mod`, `go.sum`, `Makefile`, and `.golangci.yml` paths now safely cause repository-wide stop verification without ever being formatted. The 24-test hook suite and task-scoped whitespace check passed.
- 2026-08-31: The portability blocker was diagnosed as `golang.org/x/net v0.0.0-20200114155413-6afb5195e5aa`, selected through the legacy `github.com/miekg/dns` graph, referencing the removed Darwin `syscall.recvmsg` symbol under Go 1.26. Task 5 portability remediation is scoped to its module metadata only.
- 2026-08-31: Task 5 portability remediation completed. Updating `golang.org/x/net` to v0.58.0 and tidying the module graph made both `GOOS=darwin GOARCH=arm64 go build ./...` and `GOOS=linux GOARCH=arm64 go build ./...` pass. `make test` and the task-scoped whitespace check also passed.
- 2026-08-31: Final dependency verification passed: `make fmt-check`, `make lint`, `make verify`, and `git diff --check` all passed after the module update. A fresh-context dependency review found no issues; it confirmed the required `golang.org/x/crypto` graph update, a clean `go mod tidy -diff`, focused mDNS race tests, and `go vet ./...`.
- 2026-08-31: Codex reported its 3-second SessionEnd timeout clamp. The trusted hook configuration now declares that supported limit explicitly; the 24-test hook suite, JSON validation, and whitespace check passed. Fresh-context review found no issue and confirmed the cleanup is lightweight and session-isolated.

## Completion evidence

- `uv run --no-project --script .codex/hooks/agent_loop_test.py` — 23 tests passed, 2026-08-31.
- `uv run --no-project --script .codex/hooks/agent_loop_test.py` — 30 tests
  passed after nested-tool Stop-gate fallback coverage, 2026-08-31.
- `make verify` — passed: formatting, lint (0 issues), fresh race tests with coverage, and host builds, 2026-08-31.
- `.bin/coverage-functions.txt` — generated; total statement coverage 71.9%, 2026-08-31.
- `make check-portability` — passed for darwin/arm64 and linux/arm64 after `golang.org/x/net` was updated to v0.58.0, 2026-08-31.
- `git diff --check` — passed, 2026-08-31.
- Fresh-context review — four findings, all fixed in Task 2 review remediation. Re-review found one deleted-scope edge case, fixed in the follow-up task. The focused dependency review found no issues; no findings were declined or postponed.
- Trusted-session `/hooks` manual acceptance — not run: this execution environment cannot open a new interactive trusted Codex session.
