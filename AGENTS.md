# AGENTS.md

Universal instructions for AI coding agents working in this repository. They apply to the whole repository and are not specific to any one tool.

## Repository state

Milestone 1 has landed its device hardware and local-wake vertical slice: pure-Go ALSA capture/playback, LEDs, buttons, local VAD and openWakeWord, model installation, and Dot diagnostics. Gateway transport, mDNS, the supervisor/A/B updater, persistence, and assistant integration remain later milestones. Follow the layout, naming, and stack choices in `docs/DESIGN.md` §6 and §27 rather than inventing alternatives.

## Build and test commands

```sh
make all           # test, then build
make test-fast     # cached go test -race loop for iteration
make test          # fresh race tests plus coverage; report at .bin/coverage-functions.txt
make race          # go test -race -timeout=100s ./...
make lint          # golangci-lint run
make fmt           # gofmt -l -w .
make fmt-check     # fail if any Go file needs formatting
make verify        # final software check: formatting, lint, fresh tests, and host builds
make build         # host binaries into .bin/
make build-device  # static linux/arm64 echod into .bin/linux_arm64/
make build-device-ctl   # static linux/arm64 echoctl for the Dot
make build-device-noasm # static linux/arm64 echod with portable vector fallback
make check-portability  # compile all packages for darwin/arm64 and linux/arm64
make bench              # Go benchmark results with allocation counts
make version       # print the git-derived revision the build stamps in

go test ./internal/<pkg>/...   # one package
go generate ./...              # regenerate moq mocks
go test ./internal/release -run TestFixtures_Regenerate -update-fixtures   # rewrite release fixtures
```

Run `make lint` while working, not only at the end: `.golangci.yml` is strict and `nolintlint` requires every suppression to name a specific linter and give a reason.

Hardware access is confined to `internal/device/audio/alsa` and
`internal/device/mixer`, behind Linux build tags with `!linux` stubs. LED,
buttons and system code instead use injected filesystem roots, so their tests
remain portable without build tags.

## Codex automation

This trusted repository includes project hooks under `.codex/`; re-review the
hook configuration whenever it changes before trusting it. The hooks require
`uv` locally and use `uv run --no-project --script` with only Python's standard
library.

- After `apply_patch`, the hook immediately runs `gofmt` only on existing Go
  files safely identified as changed by that patch.
- At session stop, it checks session-journaled Go changes with formatting,
  scoped lint, and scoped race tests. A change to `go.mod`, `go.sum`,
  `Makefile`, or `.golangci.yml` expands those checks to `./...`; it requests at
  most one continuation after a failure.
- Hooks are early feedback only: they never stage, commit, change plan status,
  or access hardware. Run task-specific checks and `make verify` before final
  software handoff.

If hooks are disabled, unavailable, or `uv` is not installed, work normally
with `make fmt-check`, `make lint`, the relevant `go test -race` command, and
`make verify`. CI installs `uv` and runs the hook tests independently.

## Code conventions

- **CLI:** `jessevdk/go-flags` in all four binaries. Each command has `main.go` (`main`, `run`, `var revision`) and `config.go` (`opts` struct plus a unit-tested `parseArgs`). Precedence is flag, then environment, then ini config file, then default.
- **Tests:** `stretchr/testify` — `require` for preconditions and error assertions, `assert` for the rest. One `_test.go` per source file.
- **Mocks:** `matryer/moq` via `//go:generate`, into a `mocks/` subpackage. Never hand-edit generated files. Prefer a hand-written stub when the interface has one method.
- **Errors:** wrap errors crossing a package boundary with `%w`; compare with `errors.Is`/`errors.As`; sentinels are exported `Err*` variables.
- **No `init()`.** Wiring belongs in the composition root (`main`).
- **Interfaces are declared by the consumer,** with the concrete implementation in the provider package and injection at the composition root. Keep the exported surface minimal.
- Version is injected at link time (`-X main.revision`); there is no build-info package.

## Sources of truth

Before planning or implementing, read:

1. **[docs/DESIGN.md](docs/DESIGN.md)** — authoritative for architecture, boundaries, behavior, constraints, and acceptance criteria.
2. **[docs/plans/README.md](docs/plans/README.md)** — authoritative for plan creation, execution, verification, and lifecycle state.

Do not replace explicit decisions in these documents with assumptions. When implementation reveals that an operational behavior or design assumption must change, update the relevant document as part of the same change.

`docs/DESIGN.md` §26 lists open questions that must be resolved by experiment on real hardware, not by guessing. If work depends on one of them, resolve it with a focused diagnostic and record the answer in the design document.

## Planning, verification, and review

For any non-trivial change, follow [docs/plans/README.md](docs/plans/README.md)
for the required plan lifecycle, task ownership, progress record, and
completion evidence. A plan is required when work spans packages, changes a
documented behavior or boundary, or needs more than one verification command.
An isolated bug fix, test addition, or documentation edit can proceed directly.

An approved numbered task in an in-progress plan is already designed; do not
re-brainstorm it unless implementation reveals material ambiguity or a required
design change. Do not expand a task's scope without first updating its plan.
When multiple independent plan tasks run in one session, use one agent per task
with non-overlapping files; the orchestrator owns plan state, cross-cutting
checks, and final reporting.

Before review, run `make fmt-check`, `make lint`, `make test`, and the task's
exact verification; `make verify` is the final software check. Fix lint
findings rather than suppressing them. Review coverage for touched functions:
test new exported functions, error paths, and design-boundary rules, and explain
deliberately untested paths in new packages below roughly 70% coverage.
Hardware-dependent behavior is not verified by a passing `dotsim` run; report
which checks used real hardware.

### Echo Dot hardware sessions

Before the first live diagnostic in an ADB session, run `su -c 'stop
ledcontroller'` and write `0` to
`/sys/bus/i2c/devices/0-003f/boot_animation` so Amazon's indicator service
cannot overwrite project test feedback. Do this once per session; it is
reversible with `start ledcontroller` or a reboot. Also confirm the physical
microphone cut is off before interpreting a wake result. On the qualified Dot,
the MTK pin-87 line is GPIO 444; if the microphone Mute button is red or the
line is absent, export it, set its direction to `out`, write `0`, and read back
`0` from `/sys/class/gpio/gpio444/value`. A high value physically cuts the
microphones and can make an otherwise healthy wake run report only near-zero
scores. If microphone capture returns `ErrDeviceBusy`, treat it as evidence
that another test or agent may be running: inspect the holder and coordinate
rather than killing a process or stopping unrelated services.

### Fresh-context self-review and finding triage

Dispatch a review agent with fresh context — the implementing session is the worst reviewer of its own diff. Give it the diff, the plan, and `docs/DESIGN.md`, and ask it to look for:

- correctness bugs and unhandled error paths;
- violations of the two boundaries above, of the discovery rules, or of the security constraints;
- drift between code and `docs/DESIGN.md` / `docs/protocol.md` / the plan;
- tests that assert the implementation rather than the requirement;
- unnecessary complexity and dead exported surface.

Each finding gets one of three dispositions, and none of them is silence:

- **fix** — do it now, then re-run step 4;
- **decline** — state why it is not a defect (wrong premise, intended behavior, out of scope for this change);
- **postpone** — record it in the plan as a follow-up task or in the plan's limitations section, so it survives the session.

Correctness and security findings are not postponed without saying so in the final report.

Report what was actually done and verified, including commands and outcomes,
deviations, declined or postponed findings, and anything left open. An
unverified acceptance item leaves its plan in `in-progress` with the blocker
recorded. Hooks cannot reliably delegate review, so fresh-context review and
explicit triage remain required.

## Architecture: the two boundaries that govern everything

The design has exactly two hard boundaries. Most correctness questions in this repo reduce to one of them.

### Voice boundary — wake detection is device-local, always

The Echo Dot runs the full always-on wake stack: mic capture → optional DSP/beamforming → **local VAD** → **local wake model** → accept. The gateway never scores wake words and never receives a continuous microphone stream. It sees audio only *after* the device has created a voice turn (`turn.start`), triggered locally by wake or the Action button.

Two distinct uses of VAD exist and must not be conflated:

- **Wake VAD** — device-only, always-on, gates whether a wake score is credible speech.
- **Command endpointing** — decides when the user's command has ended; runs on the *device* after a wake/button trigger, then closes the active-turn audio window.

They have separate configuration and thresholds. There is deliberately no gateway wake mode, and the management UI must not offer one.

The Echo knows nothing about Hermes, LLM APIs, conversation storage, or speech providers. Anything assistant-specific belongs behind the `AssistantBackend` interface; anything Hermes-specific stays inside the Hermes adapter.

### Update boundary — the gateway owns desired state, the device owns recovery

Agent updates are **application-level A/B slots** (`echod_a` / `echod_b` under `/data`, selected by a stable symlink) — *not* Android partition A/B and never FireOS OTA. A normal agent update must never write the bootloader, boot image, recovery, or system partition.

Invariants that must not be violated by any update-path change:

- A small **stable supervisor lives outside both A/B slots** and is the thing that decides whether a new boot succeeded. It must be able to roll back with the gateway unreachable.
- **Never destroy the rollback path before the replacement is proven**: download to a `.part` staging file, verify size + SHA-256 + signature, fsync, atomically promote into the *inactive* slot, and only then flip the symlink. The running slot is never overwritten.
- **A new slot runs on trial, not committed on process start.** Trial health requires config loaded, critical hardware initialized, gateway resolved, TLS/auth succeeded, and the `hello`/`welcome` handshake completed. A trial deadline or repeated fast exits trigger automatic rollback to the previous slot.
- The supervisor is recovery infrastructure: keep it small, change it rarely, and do not ship it through the normal agent rollout path (see §10.9).

Feature behavior is negotiated by **capability announcement** in `hello`, never by `version >= X` checks. Version minimums appear only in release manifests as installation-safety constraints.

### Discovery

The gateway advertises `_echo-satellite._tcp.local.`; TXT records carry discovery metadata only, never secrets. mDNS locates endpoints — it is not authentication. Resolution order: explicit configured URL → previously paired gateway → mDNS browse. An explicit URL always wins.

### Hardware-independent development

`dotsim` speaks the same protocol as `echod` with files instead of hardware, and must also simulate update states, trial timeouts, crashes, rollbacks, and reconnects so fleet rollout logic is testable without breaking a real Dot. Gateway work should be testable without an Echo; wake/VAD correctness is tested separately against audio fixtures.

## Security-sensitive areas

Permission to deploy an agent is permission to run privileged code on a rooted Dot. Update and deployment endpoints require strong admin authorization and an audit trail. Production release manifests are signed (Ed25519 preferred); the release private key never lives on the gateway or a device. `allow_unsigned_dev_builds` is a development escape hatch, off by default, and must be surfaced in status/UI when enabled.

Raw continuous microphone audio is never uploaded for wake scoring, and raw audio is not stored by default.
