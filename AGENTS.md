# AGENTS.md

Universal instructions for AI coding agents working in this repository. They apply to the whole repository and are not specific to any one tool.

## Repository state

Milestone 0 has landed: the Go module, build, CI, and the shared contracts (`internal/protocol`, `internal/discovery`, `internal/release`) exist, and `echod`, `gateway`, `echoctl`, and `dotsim` build. No microphone, wake model, WebSocket endpoint, mDNS advertisement, supervisor, or persistence exists yet — those arrive with Milestones 1 and later. Follow the layout, naming, and stack choices in `docs/DESIGN.md` §6 and §27 rather than inventing alternatives.

## Build and test commands

```sh
make all           # test, then build
make test          # go test -race with coverage, mocks excluded from the profile
make race          # go test -race -timeout=100s ./...
make lint          # golangci-lint run
make fmt           # gofmt -l -w .
make build         # host binaries into .bin/
make build-device  # static linux/arm64 echod into .bin/linux_arm64/
make version       # print the git-derived revision the build stamps in

go test ./internal/<pkg>/...   # one package
go generate ./...              # regenerate moq mocks
go test ./internal/release -run TestFixtures_Regenerate -update-fixtures   # rewrite release fixtures
```

Run `make lint` while working, not only at the end: `.golangci.yml` is strict and `nolintlint` requires every suppression to name a specific linter and give a reason.

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

## Architecture: the two boundaries that govern everything

The design has exactly two hard boundaries. Most correctness questions in this repo reduce to one of them.

### Voice boundary — wake detection is device-local, always

The Echo Dot runs the full always-on wake stack: mic capture → optional DSP/beamforming → **local VAD** → **local wake model** → accept. The gateway never scores wake words and never receives a continuous microphone stream. It sees audio only *after* the device has created a voice turn (`turn.start`), triggered locally by wake or the Action button.

Two distinct uses of VAD exist and must not be conflated:

- **Wake VAD** — device-only, always-on, gates whether a wake score is credible speech.
- **Command endpointing** — decides when the user's command has ended; runs on the *gateway* for v0.1, consuming only active-turn audio.

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

## Implementation planning

All non-trivial work is tracked as a plan document under `docs/plans/`. **Read [docs/plans/README.md](docs/plans/README.md) before creating or executing one** — it is authoritative and contains the required template.

Working rules that apply whenever a plan is active:

1. An approved plan's scope and numbered tasks are the work boundaries. Review and update the plan before implementing a material scope expansion.
2. Inspect the repository and working tree before editing. Preserve unrelated user changes; avoid unrelated refactors.
3. Decompose into small, dependency-aware tasks with one observable outcome each. State dependencies explicitly instead of hiding them in prose.
4. Give every task exact verification commands or concrete manual checks. Mark a task complete only after its verification succeeds.
5. Plan location is the lifecycle state: `future/`, `in-progress/`, `finished/`, `superseded/`. Keep only actively executing work in `in-progress/`, and set the active owner when execution begins.
6. Do not let multiple agents claim the same task. Overlapping plans must name their shared scope and coordination requirements before work proceeds.
7. Keep failed, paused, or blocked work in `in-progress/` with an honest blocker and remaining work. Partial or unverified execution is not completion.
8. Never rewrite a completed task; append a `Task N review remediation` subtask instead.
9. Hardware-dependent verification is not satisfied by a passing `dotsim` run. Record which checks ran against real hardware.

The milestone sequence in `docs/DESIGN.md` §24 is deliberately ordered — notably, the safe supervisor and A/B OTA (Milestone 3) land *before* Hermes integration so later device work uses the production update path. Plans should follow that sequence unless there is a recorded reason not to.

## Security-sensitive areas

Permission to deploy an agent is permission to run privileged code on a rooted Dot. Update and deployment endpoints require strong admin authorization and an audit trail. Production release manifests are signed (Ed25519 preferred); the release private key never lives on the gateway or a device. `allow_unsigned_dev_builds` is a development escape hatch, off by default, and must be surfaced in status/UI when enabled.

Raw continuous microphone audio is never uploaded for wake scoring, and raw audio is not stored by default.
