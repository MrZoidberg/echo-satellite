# Agentic harness and Echo Dot device-lab implementation plan

**Status:** in-progress
**Owner or active agent:** Codex (/root)
**Created:** 2026-09-08
**Updated:** 2026-09-10
**Started:** 2026-09-08
**Completed:** not completed

## Objective

Make repository agent sessions shorter, safer, and more reproducible by replacing
ad hoc Echo Dot commands with a deterministic device-lab runner, loading the
hardware workflow only when relevant, standardizing fresh-context review, and
avoiding repeated hook and verification work.

## Non-goals

- Changing device, gateway, update, wake, or audio product behavior.
- Replacing ADB, Magisk, the existing Go build, or the plan lifecycle.
- Running physical-device work concurrently across agents.
- Adding an MCP server, externally distributed plugin, production dependency,
  credential manager, or general-purpose orchestration framework.
- Rewriting completed task history or replacing existing hardware evidence.

## Source references and constraints

- `AGENTS.md`: preserve the voice/update boundaries, live-device safety setup,
  `ErrDeviceBusy` coordination rule, fresh-context review, and final verification.
- `docs/plans/README.md`: plans remain the lifecycle source of truth; hardware
  tasks require exact commands, observations, cleanup, and real-device evidence.
- `docs/DESIGN.md` §§7.4, 10, and 26: deployment diagnostics must preserve the
  installed `echod`, use the qualified Magisk layout, and never write FireOS,
  boot, recovery, or bootloader partitions.
- Repository automation remains standard-library Python executed with
  `uv run --no-project --script`; the on-device timing helper remains pure Go.
- Device commands require an explicit ADB path and serial. Secrets, token bytes,
  private-key contents, and complete signed URLs must never enter logs/evidence.
- Only the orchestrating agent may control the physical Dot. Review agents are
  read-only and receive sanitized summaries rather than raw device logs.

## Dependencies and prerequisites

- Land or otherwise preserve the current Milestone 3 Task 2 changes before this
  plan starts; this plan must begin from a clean worktree or explicitly recorded
  unrelated dirty paths.
- Trusted repository hooks, `uv`, Go, ADB, PowerShell/Windows interop, and the
  existing qualified Dot remain available.
- Live Task 6 requires rooted Dot `G090LF0964060EHP`, Magisk root, SELinux
  permissive, GPIO 444 low, the current installed-agent backup, and an
  authenticated gateway configuration.

## Architecture and high-level plan

```text
plan task / hardware prompt
  -> repository hardware skill loads on demand
  -> device_lab.py validates explicit inputs and acquires locks
  -> version-controlled remote payloads run through root-owned scripts
  -> resumable phase journal survives ADB disconnects/reboots
  -> sanitized evidence.json + concise Markdown summary
  -> plan-lint validates task/evidence lifecycle
  -> read-only fresh-review agent receives diff + summary
```

The runner owns orchestration and cleanup, not product behavior. It writes only
under an ignored host session directory and a session-specific remote diagnostic
root. A local lock plus a token-bearing remote lock serialize device access; an
unknown lock or microphone holder is reported and never killed or removed.

## Public interfaces and evidence contract

Primary command:

```sh
uv run --no-project --script tools/device-lab/device_lab.py <command> \
  --adb <path> --serial <serial> [command options]
```

Commands are `preflight`, `prepare`, `cleanup`, `verify-clean`, and
`render-evidence`. They support `--resume <session-directory>` after an
interruption. No command infers a serial, credential, gateway, or agent path.

Each ignored `.bin/device-lab/<session-id>/evidence.json` contains:

- `schema_version`, session ID, serial, timestamps, harness Git revision, and
  requested command;
- sanitized inputs and initial device/service/GPIO/installed-agent state;
- ordered phases with status, duration, sanitized command identifier, bounded
  observations, and artifact digests;
- startup timing samples and declared percentile method;
- cleanup attempts, restored state, residual paths/processes, and final
  installed-agent digest;
- explicit `hardware`, `host`, or `simulated` provenance for every check.

The renderer produces a small durable Markdown record from this JSON. Raw logs
remain ignored; evidence rejects fields matching token, authorization,
credential, private-key, query-string, or signed-URL patterns.

## Planned file map

- `tools/device-lab/`: Python orchestrator, PowerShell gateway launcher, pure-Go
  timer, root shell payloads, schema/renderer, and fake-ADB tests.
- `.agents/skills/echo-dot-hardware/`: narrowly triggered workflow instructions
  and references to the version-controlled runner.
- `.codex/agents/fresh-review.toml` and `.codex/config.toml`: project-scoped,
  read-only review role and agent registration.
- `.codex/hooks/agent_loop.py` plus tests: rolling change checkpoint and plan
  evidence checks without hardware access.
- `.codex/hooks/plan_lint.py` plus tests: deterministic plan/status/evidence/link
  validation.
- `AGENTS.md` and `docs/plans/README.md`: shorter hardware routing and an
  explicit non-redundant verification cadence.

## Numbered tasks

### Task 1: Define the device-lab session and evidence core

**Status:** superseded by Task 7

**Purpose:** Establish deterministic state, redaction, subprocess, and evidence
primitives before any device mutation is implemented.

**Dependencies:** None.

**Hardware required:** no.

**Files or components:** Create the Python runner core, schema/renderer, fake ADB,
and unit tests under `tools/device-lab/`.

**Concrete changes:**

- Parse explicit `--adb` and `--serial`; reject missing/blank serials and broad
  or unresolved remote roots.
- Store session state atomically under `.bin/device-lab/<session-id>/` and expose
  idempotent resume semantics.
- Run subprocesses without a host shell, bound output size, emit short progress
  records, and retain full ignored logs.
- Implement structured redaction and fail closed if evidence contains a secret
  field or URL query.
- Implement local and remote ownership tokens; never steal a lock.
- Render deterministic Markdown from validated evidence JSON.

**Expected outcome:** Fake commands can execute, resume, redact, lock, and render
without ADB or repository-tracked side effects.

**Verification:**

```sh
uv run --no-project --script tools/device-lab/device_lab_test.py -k core
git diff --check
```

Expected: core, redaction, lock, interrupted-write, schema, and renderer tests
pass; no secret fixture appears in captured output.

### Task 2: Implement safe device preparation and cleanup

**Status:** superseded by Task 7

**Purpose:** Make every hardware session start and end from recorded, recoverable
state without fragile inline root-shell quoting.

**Dependencies:** Task 1.

**Hardware required:** no; fake ADB covers implementation.

**Files or components:** Add version-controlled remote payloads and the
`preflight`, `prepare`, `cleanup`, and `verify-clean` commands.

**Concrete changes:**

- Preflight ADB state, root, product/ABI, SELinux, BusyBox applets, Magisk
  version/service directories, `/data` space/inodes/mount, agent version/digest,
  required CLI flags, process holders, and gateway reachability.
- Push fixed scripts to a session-specific root and invoke them as the sole
  argument to remote `su -c`; do not compose nested redirections on the host.
- Record initial `ledcontroller`, `mdnsd`, boot-animation, GPIO export/direction/
  value, installed digest, and process state before preparation.
- Back up the installed binary by digest under `.bin/device-backups/` without
  changing the installed pathname.
- Make cleanup idempotent, remove only token-owned paths/hooks/locks, fsync
  affected directories where supported, restore exactly the recorded service
  and GPIO state, and verify no session process remains.
- Treat device busy, unknown locks, missing ownership tokens, and unexpected
  installed-digest changes as hard failures requiring operator coordination.

**Expected outcome:** All safety and cleanup behavior is testable with fake ADB,
and generated device commands never target the installed binary for writes.

**Verification:**

```sh
uv run --no-project --script tools/device-lab/device_lab_test.py -k safety
rg -n "(/data/local/bin/echod.*(mv|cp|rm|write)|(?:mv|cp|rm).*\/data\/local\/bin\/echod)" tools/device-lab
```

Expected: wrong-device, busy-device, stale-lock, partial-prepare, interrupted
cleanup, state-restoration, and digest-change tests pass; the write-target scan
has no live qualification-path match.

### Task 3: Implement deployment qualification and gateway lifecycle

**Status:** superseded by Task 7

**Purpose:** Replace the temporary scripts and manual Windows gateway commands
used in Milestone 3 Task 2 with one reproducible workflow.

**Dependencies:** Tasks 1–2.

**Hardware required:** no; fake ADB and fake PowerShell processes cover
implementation.

**Files or components:** Add the pure-Go timer, filesystem/restart/service-hook
payloads, Windows PowerShell launcher, and qualification phases.

**Concrete changes:**

- Build/stage the current ARM64 agent only beneath the diagnostic root and
  confirm it supports authenticated gateway flags before starting trials.
- Measure executable permission enforcement, staged-file fsync, same-directory
  rename, containing-directory fsync, and old-process lifetime on diagnostic
  copies only.
- Measure at least the requested number of authenticated welcome events with a
  static ARM64 helper, preserving raw samples and the percentile method.
- Start the Windows gateway through `powershell.exe -File`, record its exact PID,
  wait for TLS endpoint readiness, and stop only that PID during cleanup.
- Compare token-file digests without logging token bytes and reject incompatible
  installed-agent flags before a long trial begins.

**Expected outcome:** One command produces the complete Task 2 class of evidence
and always attempts verified cleanup.

**Verification:**

```sh
uv run --no-project --script tools/device-lab/device_lab_test.py -k qualification
go test ./tools/device-lab/...
git diff --check
```

Expected: timing timeout, wrong ownership, gateway startup failure, exact-PID
stop, and cleanup fault tests
pass with deterministic evidence.

### Task 4: Add the on-demand hardware skill and slim universal context

**Status:** superseded by Task 7

**Purpose:** Load detailed device procedure only for hardware work instead of
spending context tokens in every repository task.

**Dependencies:** Tasks 1–3.

**Hardware required:** no.

**Files or components:** Create `.agents/skills/echo-dot-hardware/`; modify
`AGENTS.md` without weakening safety boundaries.

**Concrete changes:**

- Trigger on Echo Dot, FireOS, ADB, live audio, reboot, Magisk, and hardware
  qualification prompts; do not trigger for simulator-only or ordinary Go work.
- Require the active plan/design read, runner preflight, single-agent device
  ownership, evidence capture, cleanup, and verified restoration.
- Route detailed procedures to focused references and scripts; keep `SKILL.md`
  concise and imperative.
- Retain the voice/update/security boundaries and short critical device safety
  rules in `AGENTS.md`, replacing duplicated command recipes with the skill
  invocation and runner entrypoint.

**Expected outcome:** Hardware prompts reliably load the workflow while
non-hardware prompts receive less universal instruction text.

**Verification:**

```sh
uv run --no-project --script tools/device-lab/device_lab_test.py -k skill
rg -n "voice boundary|update boundary|ErrDeviceBusy|echo-dot-hardware" AGENTS.md .agents/skills/echo-dot-hardware/SKILL.md
```

Expected: trigger/negative-trigger fixtures pass and every hard boundary remains
present in universal or skill-scoped instructions as intended.

### Task 5: Standardize review and optimize repository hooks

**Status:** superseded by Task 7

**Purpose:** Preserve independent review and automatic checks while eliminating
repeated formatting, oversized context, and missing plan-evidence validation.

**Dependencies:** Task 1.

**Hardware required:** no.

**Files or components:** Add the fresh-review agent and plan linter; modify the
existing hook dispatcher/config/tests and verification guidance.

**Concrete changes:**

- Define `fresh-review` as a read-only `gpt-5.6-terra` agent with high reasoning;
  require severity, file/line, violated requirement, evidence, and disposition
  recommendation, and prohibit edits or external actions.
- Give review only the task section, relevant design sections, diff, command
  summary, and sanitized evidence. Reuse its thread for targeted re-review.
- Add a per-session rolling checkpoint beside the existing baseline. PostToolUse
  formats/journals only paths changed since the previous event; Stop retains the
  full-session baseline fallback.
- Extend the journal to active-plan and evidence changes and clean both snapshot
  types at SessionEnd.
- Add deterministic plan linting for lifecycle/status/owner consistency,
  completed-task evidence, hardware provenance/cleanup, exact runner references,
  and relative links.
- Add a loopback capability classification so agents request one scoped
  `make verify` permission rather than first emitting predictable sandbox-only
  failures.
- Amend `AGENTS.md` and `docs/plans/README.md` to use scoped iteration checks,
  one pre-review full verify, targeted remediation checks, and a second full
  verify only when remediation changes software/build behavior.

**Expected outcome:** Read-only commands no longer reformat/rejournal old changes;
docs-only plan work receives relevant checks; review is consistent and compact.

**Verification:**

```sh
uv run --no-project --script .codex/hooks/agent_loop_test.py
uv run --no-project --script .codex/hooks/plan_lint_test.py
uv run --no-project --script .codex/hooks/plan_lint.py \
  docs/plans/in-progress/2026-09-08-agentic-harness-device-lab.md
```

Expected: rolling-checkpoint, unchanged-exec, fallback, cleanup, docs-only,
broken-link, missing-evidence, and lifecycle fixtures pass; this plan lints.

### Task 6: Qualify the harness on the rooted Dot

**Status:** not started

**Purpose:** Prove the automation itself on real hardware before making it the
required path for later device tasks.

**Dependencies:** Task 8.

**Hardware required:** yes — rooted qualified Dot, installed binary backed up,
no other device session, authenticated Windows gateway inputs available.

**Files or components:** Run the device-lab workflow; add a sanitized evidence
record and concise diagnostic summary. Do not modify product code.

**Concrete changes:**

- Run preflight and inspect the fully resolved, redacted target summary.
- Run `prepare` then `cleanup`/`verify-clean` without reboot as the first smoke
  test; compare restored state to the captured initial state.
- Run the complete deployment qualification with 20 starts and reboot/resume.
- Compare filesystem, Magisk path, timing, restart, backoff, digest, and cleanup
  results with Milestone 3 Task 2; explain any material divergence rather than
  silently updating expectations.
- Commit only sanitized generated evidence/summary; retain no raw credentials,
  raw voice audio, or diagnostic device files.

**Expected outcome:** The runner reproduces the prior qualification, restores the
Dot exactly, and leaves the installed agent untouched.

**Verification:**

```sh
uv run --no-project --script tools/device-lab/device_lab.py preflight \
  --adb "$ADB" --serial "$DEVICE_SERIAL"
uv run --no-project --script tools/device-lab/device_lab.py qualify-deployment \
  --adb "$ADB" --serial "$DEVICE_SERIAL" --agent .bin/linux_arm64/echod \
  --gateway-url "$GATEWAY_URL" --token-file "$GATEWAY_TOKEN_FILE" \
  --gateway-exe "$GATEWAY_EXE" --tls-cert "$GATEWAY_TLS_CERT" \
  --tls-key "$GATEWAY_TLS_KEY" --device-profile "$GATEWAY_DEVICE_CONFIG" \
  --trials 20
uv run --no-project --script tools/device-lab/device_lab.py verify-clean \
  --resume .bin/device-lab/<session-id>
```

Expected: every phase passes, 20 authenticated welcomes are recorded, the final
installed digest equals the initial digest/backup, every owned hook/root/lock is
absent, and initial services/GPIO are restored. Evidence must identify these as
real-hardware provenance checks; no simulator result can satisfy this task.

### Task 7: Single-run live qualification gate

**Status:** superseded by Task 8

**Purpose:** Historical single-run gate, superseded because the fresh review
showed its harness implementation was incomplete.

**Dependencies:** None.

**Hardware required:** yes — rooted qualified Dot `G090LF0964060EHP`, Magisk
root, SELinux permissive, GPIO 444 low, no competing device session, and
explicit authenticated gateway inputs.

**Files or components:** No product or harness implementation changes; update
this plan and append sanitized generated evidence/summary only after success.

**Concrete changes:**

- Treat Tasks 1–5 as superseded verification paths. Their existing harness
  code remains unvalidated by unit or fake-ADB tests.
- Run the runner's preflight, one `prepare`/`cleanup`/`verify-clean` smoke
  cycle, then one complete 20-trial deployment qualification with reboot/resume.
- Preserve the update boundary: do not write `/data/local/bin/echod`; stop on
  unknown locks, a busy microphone holder, a changed installed digest, or any
  cleanup failure.
- Obtain fresh-context read-only review of the plan/diff and sanitize/triage
  every finding before final repository verification.

**Expected outcome:** One real-hardware qualification provides the sole harness
validation evidence. It does not establish unit-level regression coverage.

**Verification:**

```sh
uv run --no-project --script tools/device-lab/device_lab.py preflight \
  --adb /usr/bin/adb --serial G090LF0964060EHP
uv run --no-project --script tools/device-lab/device_lab.py prepare \
  --adb /usr/bin/adb --serial G090LF0964060EHP
uv run --no-project --script tools/device-lab/device_lab.py cleanup \
  --adb /usr/bin/adb --serial G090LF0964060EHP --resume .bin/device-lab/<session-id>
uv run --no-project --script tools/device-lab/device_lab.py verify-clean \
  --adb /usr/bin/adb --serial G090LF0964060EHP --resume .bin/device-lab/<session-id>
uv run --no-project --script tools/device-lab/device_lab.py qualify-deployment \
  --adb /usr/bin/adb --serial G090LF0964060EHP --agent .bin/linux_arm64/echod \
  --gateway-url <gateway-url> --token-file <gateway-token-file> \
  --gateway-exe <gateway-exe> --tls-cert <tls-cert> --tls-key <tls-key> \
  --device-profile <device-profile> --trials 20
uv run --no-project --script tools/device-lab/device_lab.py verify-clean \
  --adb /usr/bin/adb --serial G090LF0964060EHP --resume .bin/device-lab/<session-id>
make fmt-check
make lint
make test
git diff --check
make verify
```

Expected: all phases pass; evidence marks every device check `hardware` provenance; the
initial installed digest, services, and GPIO state are restored; no token or
signed URL occurs in committed evidence; fresh review findings have explicit
dispositions; and all final commands pass.

### Task 8 review remediation: make safe session claims observable

**Status:** in progress

**Purpose:** Address the safe session-management findings from the 2026-09-09
fresh review. It supersedes Task 7's no-unit-test restriction and removes live
deployment qualification from this harness.

**Dependencies:** Tasks 1--5 implementation artifacts and the 2026-09-09 fresh review.

**Hardware required:** no for implementation and deterministic tests; yes for
later Task 6 evidence.

**Files or components:** `tools/device-lab/`, `.codex/hooks/plan_lint.py`, its
tests, and this plan.

**Concrete changes:** Enforce root-owned remote locking and permissions,
microphone-busy detection, initial-state capture, contained resumable state,
durable state publication, real restoration/cleanup verification and digest
comparison. The harness does not start a gateway, stage an agent, run welcome
trials, or assess deployment/launcher behavior; those remain Milestone 3 work.
Parse task-scoped plan requirements and relative links in plan lint.

**Verification:**

```sh
uv run --no-project --script tools/device-lab/device_lab_test.py
uv run --no-project --script .codex/hooks/plan_lint_test.py
uv run --no-project --script .codex/hooks/plan_lint.py docs/plans/in-progress/2026-09-08-agentic-harness-device-lab.md
```

Expected: all review findings have deterministic regression coverage. No
hardware success is claimed by these tests.

### Task 9: Make qualified operator handoffs observable

**Status:** completed 2026-09-10

**Purpose:** Remove the remaining ad hoc transfer and evidence gaps exposed by
the 2026-09-10 Milestone 3 hardware session without giving device-lab authority
to install, select, validate, or replace the agent.

**Dependencies:** Task 8's contained session, locking, redaction, and cleanup
behavior.

**Hardware required:** no for deterministic implementation tests; yes for a
future qualified-Dot evidence run.

**Files or components:** `tools/device-lab/device_lab.py`,
`tools/device-lab/device_lab_test.py`, and this plan.

**Concrete changes:**

- When a host lock belongs to a locally proven session for the selected serial,
  report its canonical `--resume .bin/device-lab/<session-id>` guidance; never
  infer a recovery path for an unknown owner.
- Record BusyBox path/version plus `cmp -s`, `sed`, `awk`, and `sha256sum`
  availability/behavior as non-gating preflight evidence.
- Add `stage-external --artifact <local-file> [--retain]`: push through a
  unique per-invocation `/data/local/tmp` path and root-move into a restrictive session-owned
  path after preparation has captured the cleanup baseline. Reject symlinks and
  verify final root ownership/mode, size, and SHA-256 before recording basename,
  size, digest, final path, and retention. Default cleanup removes the staged
  file; `--retain` makes the handoff an explicit operator-owned exception.
- Add `record-external-action --action bootstrap|install --checkpoint
  before|after --resume <prepared-session>`, which reads and journals installed
  digest, qualified hook status, and installed-release metadata status. Reject
  duplicate checkpoints, preflight-only sessions, and a serial that disagrees
  with resumed evidence. Its default is the measured legacy Magisk hook path;
  it accepts no raw command and invokes no `echoctl` command or agent installation.
- On cleanup digest drift, retain expected and observed hashes in evidence and
  print the ADB/known-good signed `echoctl update install` recovery hint without
  attempting recovery.

**Expected outcome:** Operators can stage their artifact and record their own
bootstrap/install work reproducibly, while the harness remains unable to alter
the installed agent.

**Verification:**

```sh
uv run --no-project --script tools/device-lab/device_lab_test.py
uv run --no-project --script .codex/hooks/plan_lint.py docs/plans/in-progress/2026-09-08-agentic-harness-device-lab.md
```

Expected: deterministic tests cover lock guidance, diagnostic capability
capture, staged-artifact ownership/cleanup/retention, read-only external-action
journaling, and digest drift. No physical-device claim is made by these tests.

## Cross-task risks

- **Automation increases blast radius:** strict explicit targets, safe path
  validation, ownership tokens, and fake-ADB fault tests gate live use.
- **Reboot or ADB loss interrupts cleanup:** the host journal is fsynced before
  each mutation and `--resume`/`cleanup` are idempotent.
- **Magisk layouts drift:** support only measured layouts and fail closed on
  ambiguity; never guess a service directory.
- **Evidence leaks credentials:** allowlisted structured fields plus a final
  redaction validator reject the artifact before rendering.
- **Staging blurs the update boundary:** artifact identity capture and
  root-owned transfer are permitted, but signature verification, compatibility,
  metadata mutation, and installation remain `echoctl update install` work.
- **Hook optimization misses changes:** retain the session baseline as a Stop
  fallback and test deleted, renamed, untracked, concurrent, and missing-event
  cases.
- **Custom agents increase tokens:** use only one read-only reviewer and reuse its
  thread; keep device operations on the main agent.

## Rollback or recovery

- The harness is additive until Task 6 passes; existing Make targets and manual
  procedures remain available during rollout.
- Disable the repository skill or custom-agent registration without deleting
  scripts/evidence.
- Revert hook changes independently; Git-private session journals remain
  compatible or are ignored and cleaned by the prior dispatcher.
- On live failure, run `cleanup --resume <session-directory>`; if ownership
  cannot be proven, stop and inspect rather than deleting or killing anything.
- A changed installed-agent digest, missing reconnect, or unknown service hook is
  a manual ADB recovery condition governed by the update boundary.

## Final acceptance criteria

- [ ] Safe-session commands use no inline nested ADB shell construction.
- [ ] One real-Dot prepare/cleanup/verify-clean smoke run records cleanup,
  installed-digest preservation, and evidence sanitization.
- [ ] The runner never writes the installed agent.
- [ ] Device access is serialized by recorded ownership.
- [ ] Evidence is deterministic, sanitized, provenance-labeled, and sufficient
  for plan completion/review without embedding temporary harness source.
- [ ] Hardware instructions load through the repo skill only when relevant.
- [ ] Root `AGENTS.md` retains all architecture/security invariants with less
  non-hardware context.
- [ ] The fresh-review agent is read-only and reports structured findings.
- [ ] Rolling hook checkpoints avoid repeated work while Stop retains a safe
  full-session fallback.
- [ ] Plan lint catches missing hardware evidence, cleanup, lifecycle, and links.
- [ ] Real-device smoke testing restores initial state with the installed digest
  unchanged.
- [ ] `make fmt-check`, `make lint`, `make test`, `git diff --check`, and
  `make verify` pass after fresh-context finding triage.

## Progress log

- 2026-09-08: Plan created from the Milestone 3 Task 2 session retrospective.
  The session showed repeated nested-shell quoting failures, reactive FireOS/
  Magisk capability discovery, temporary-harness reproducibility gaps, repeated
  full verification, and oversized documentation evidence. The selected design
  uses one repo skill, deterministic scripts, one read-only reviewer, and
  lightweight hook improvements; additional device agents and MCP/plugin scope
  were rejected as unnecessary.
- 2026-09-08: Execution started by Codex (/root). The pre-existing Milestone 3
  documentation changes remain unrelated and untouched. No live-device inputs
  were provided, so Tasks 1–5 will be implemented and fake-tested; Task 6 will
  remain blocked pending an operator-controlled qualified Dot session.
- 2026-09-08: Added the initial runner/evidence format, static diagnostic
  payloads, timer, hardware skill, fresh-review definition, plan linter, and
  rolling hook checkpoint. Fresh review found state-token persistence, remote
  ownership, serial-path validation, resume locking, provenance, and renderer
  defects. State now remains private, serials are allowlisted, resume reacquires
  the owned host lock, remote sidecars are compared before privileged payloads,
  and renderer input is explicit. The remaining fake-ADB safety/qualification
  matrix is still required, so Tasks 1–5 are not complete.
- 2026-09-08: Corrected the project `fresh-review` role after restart exposed
  an unsupported `reasoning_effort` field and missing required description.
  It now uses the supported `model_reasoning_effort` and
  `developer_instructions` fields; `codex --strict-config --help` accepts it.
- 2026-09-08: Removed the redundant `.codex/config.toml` registration after a
  restart showed that Codex automatically discovers role files below
  `.codex/agents/`; keeping both definitions produced a duplicate role error.
- 2026-09-08: At the operator's request, Tasks 1–5's unit/fake-ADB verification
  paths were superseded by Task 7. Completion now requires one successful,
  operator-authorized real-Dot run with 20 trials, verified cleanup, fresh
  review/triage, and final repository checks. This deliberately leaves no
  automated harness regression coverage.
- 2026-09-08: Ran Task 7's requested one-time qualification on rooted Dot
  `G090LF0964060EHP` with the explicit `wss://echo-gateway.local.:8770/device`
  endpoint and `.m2` credential paths. Preflight, token-owned payload staging,
  prepare, qualification, cleanup, and verify-clean returned successfully in
  session `20260908T190250Z-96510cbea3`. The sanitized evidence has zero
  welcome samples and no gateway, agent-process, reboot/resume, digest,
  backoff, or state-restoration checks because `qualify.sh` currently only
  calls `sync` and the Python runner never starts the gateway. Task 7 and Task
  6 remain blocked on implementing those checks; this run is not deployment
  qualification evidence.
- 2026-09-08: Resumed Task 7 to replace the no-op qualification payload with
  explicit gateway startup, token-owned diagnostic staging, and observed
  authenticated welcome trials. The earlier no-op run remains recorded as a
  failed acceptance attempt rather than being rewritten.
- 2026-09-09: A live qualification against `wss://192.168.110.127:8770/device`
  recorded 20 authenticated welcomes in session `20260908T194355Z-88add3eb7e`.
  A fresh read-only review found that cleanup/restoration remains a no-op and
  that remote locking, preflight state capture, gateway readiness, timing,
  reboot/resume, Magisk/backoff, digest, and plan-lint lifecycle checks are
  missing. These are fix findings, not completion evidence; Task 7 remains in
  progress.
- 2026-09-09: Fresh-review remediation approved. Task 7 is superseded by Task
  8, and Task 6 now depends on Task 8. Task 8 supersedes Task 7's
  incomplete implementation/verification restriction; it adds deterministic
  regression coverage before any further live qualification is attempted.
- 2026-09-09: The operator removed reboot/Magisk launcher qualification from
  this harness revision. The temporary hook payloads and reboot commands were
  deleted; future launcher qualification remains Milestone 3 work rather than
  an agentic-harness acceptance condition.
- 2026-09-09: The operator removed live gateway/agent qualification completely
  from this harness. Gateway launch, staged-agent trial, timing, and welcome
  payloads were deleted; the runner is now limited to safe diagnostic-session
  ownership, state capture, cleanup, verification, and evidence rendering.
- 2026-09-09: After narrowing the scope, the device-lab unit suite, plan-lint
  suite, active-plan lint, payload shell syntax checks, formatting, lint, and
  fresh race-enabled repository tests passed. Live preflight reached the Dot,
  but the prepared-session `initial-state` artifact remains absent despite a
  successful runner phase, so live qualification remains blocked rather than
  being claimed from that false-positive result.
- 2026-09-10: Task 9 completed with deterministic coverage. The runner now
  reports canonical recover-lock guidance only for a proven serial-matching
  session; records system and BusyBox command capabilities; stages a
  root-owned, remotely verified external artifact through a unique temporary
  path; and journals only prepared, serial-matching external-action checkpoints.
  Three fresh-context reviews produced seven findings, all fixed: baseline-less
  cleanup, transfer identity/ownership verification, qualified hook default,
  complete capability capture, duplicate checkpoints, preflight-only journaling,
  and predictable temporary names. No finding was declined or postponed.

## Completion evidence

- 2026-09-08: `uv run --no-project --script tools/device-lab/device_lab_test.py -k core` — core evidence/redaction/input/state tests passed.
- 2026-09-08: `uv run --no-project --script .codex/hooks/agent_loop_test.py` and `uv run --no-project --script .codex/hooks/plan_lint_test.py` — passed.
- 2026-09-08: `go test ./tools/device-lab/...`, `make fmt-check`, `make lint`, `make test`, and `git diff --check` — passed.
- 2026-09-08: `device_lab.py qualify-deployment` plus `verify-clean` on
  `G090LF0964060EHP` — runner phases passed, but evidence is limited to
  preflight/payload staging/no-op payload execution. It does not satisfy the
  20 authenticated-welcome or cleanup-restoration acceptance conditions.
- 2026-09-09: `make fmt-check`, `make lint`, `make test`, `git diff --check`,
  and `make verify` — passed after the welcome-count changes. Fresh review
  nevertheless found unresolved correctness/safety gaps in the harness.
- 2026-09-09: `uv run --no-project --script tools/device-lab/device_lab_test.py`,
  `uv run --no-project --script .codex/hooks/plan_lint_test.py`, active-plan
  lint, payload shell syntax checks, `make fmt-check`, `make lint`, and
  `make test` — passed. Live prepare checkpoint remains blocked as recorded in
  the progress log.
- 2026-09-10: `uv run --no-project --script tools/device-lab/device_lab_test.py`
  (15 tests), active-plan lint, `git diff --check`, and `make verify` — passed.
  No live Dot action was performed; Task 9’s future hardware evidence remains
  owned by the plan’s blocked real-device work.
- Remaining: Task 7 and Task 6 real-device evidence, fresh review/triage, and
  final repository verification.
