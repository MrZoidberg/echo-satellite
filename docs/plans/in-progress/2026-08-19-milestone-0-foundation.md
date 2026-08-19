# Milestone 0 — Repository and development foundation implementation plan

**Status:** in-progress
**Owner or active agent:** Claude Code (branch `milestone-0-foundation`)
**Created:** 2026-08-19
**Updated:** 2026-08-19
**Started:** 2026-08-19
**Completed:** not completed

**Remaining work:** every task is implemented and verified locally. The plan stays in `in-progress/` for one open item only: the branch has not been pushed, so the GitHub Actions build job has never run. Push `milestone-0-foundation`, confirm the job is green, record the run in the completion evidence, then set the status to `finished` and `git mv` this file to `docs/plans/finished/`.

## Objective

Deliver the repository, build, CI, and shared-contract foundation for `echo-satellite` — Go module and package layout, format/test/vet/lint CI, the four command entrypoints, protocol and discovery contracts, release manifest/signature primitives, and Windows/WSL developer documentation — so that Milestones 1–3 can be implemented without inventing module layout, tooling conventions, protocol types, discovery semantics, or release-trust primitives.

## Non-goals

- Microphone capture, DSP, wake models, wake VAD, and command endpointing (Milestones 1 and 5).
- A real mDNS advertiser/browser or a live gateway WSS server (Milestone 2). Only interfaces, records, and resolution *logic* land here.
- Supervisor, A/B slots, staging, trial/rollback (Milestone 3) and the gateway Update Manager (Milestone 4). Only the release primitives they consume land here.
- SQLite persistence, management UI, speech worker, Hermes/mock assistants.
- Goreleaser/release workflow, dependency vendoring, coverage reporting services, `docs/ARCHITECTURE.md`.
- Any hardware interaction. Every task in this plan is hardware-independent; nothing here requires an Echo Dot.

## Source references and constraints

- `docs/DESIGN.md` §6 — repository structure. Followed exactly; no alternative package names.
- `docs/DESIGN.md` §8.1, §8.2, §8.5, §8.6 — transport, mDNS record, resolution order, message families.
- `docs/DESIGN.md` §10.7 — update state machine states. §10.9 — `supervisor_min` ineligibility.
- `docs/DESIGN.md` §11 — release bundle, manifest fields, Ed25519 signature, `allow_unsigned_dev_builds`.
- `docs/DESIGN.md` §22 — Windows/WSL workflow; `GOOS=linux GOARCH=arm64 CGO_ENABLED=0`; `make` in the toolchain.
- `docs/DESIGN.md` §24 Milestone 0 and §25 task 1 — the scope boundary for this plan.
- `docs/DESIGN.md` §27 — target stack. **Amended by Task 9**: the "Provisioning CLI" row changes from `Go + Cobra` to `Go + go-flags`.
- `AGENTS.md` — requires build/test commands to be added to `AGENTS.md` when the first code lands (Task 10).
- Boundary constraints that no task may violate: no gateway-side wake-scoring type or configuration surface; mDNS TXT records carry no secrets; unsigned releases are rejected unless the explicit development escape hatch is enabled.
- Toolchain: Go 1.26, golangci-lint 2.12.x. Module path `github.com/MrZoidberg/echo-satellite`.

## Dependencies and prerequisites

- No preceding plan. `docs/plans/in-progress/` held no other plan when this one started, so there is no shared scope or task-claim conflict.
- `github.com/matryer/moq` is installed on demand (`go install github.com/matryer/moq@latest`) only if an interface in this milestone actually needs a generated mock.

## Architecture and high-level plan

Three shared packages carry all cross-component contracts and are the substance of this milestone:

- `internal/protocol` — the device↔gateway wire contract: envelope, message-type constants for every family in §8.6, typed payloads for what Milestones 1–3 need, a capability set (capability *announcement*, never version comparison), and the update phases from §10.7.
- `internal/discovery` — DNS-SD service name, default port, TXT encode/decode with a secret-rejecting validator, consumer-side `Advertiser`/`Browser` interfaces, and a pure `Resolver` implementing the §8.5 order (explicit URL → previously paired `server_id` → browse) over an injected `Browser`, fully testable without multicast.
- `internal/release` — manifest and canonical JSON, artifact integrity (size + SHA-256), Ed25519 manifest verification against a link-time public key, a `TrustPolicy` carrying `AllowUnsignedDevBuilds` (default false, with a status string for UI surfacing), and installation-safety eligibility against `protocol_min`/`protocol_max` and `supervisor_min`.

Around them: the module and §6 directory skeleton, a Makefile and GitHub Actions workflow, a strict golangci-lint v2 configuration, four go-flags `main` packages that parse options, print their version, and shut down cleanly on SIGINT/SIGTERM, and the developer documentation.

Tasks are ordered so each is independently verifiable and the tree stays green after every task.

### Reference conventions

Tooling, dependency, and code-style conventions are modelled on [umputun/revdiff](https://github.com/umputun/revdiff), applied to this repository's `cmd/` + `internal/` layout (`docs/DESIGN.md` §6 remains authoritative for structure).

Tooling and dependencies:

- Go 1.26; no vendoring (module proxy plus `go.sum`).
- `github.com/jessevdk/go-flags` for all four binaries: an `opts` struct with `long`/`env`/`description`/`default` tags, a unit-testable `parseArgs([]string)`, and `--version`/`--dbg` as standard flags. INI configuration-file support via `flags.IniParser` where a binary needs a configuration file (`echod`, `gateway`).
- `github.com/stretchr/testify` (`require` for preconditions, `assert` for assertions) in every test.
- `github.com/matryer/moq` for interface mocks: `//go:generate moq -out mocks/<name>.go -pkg mocks -skip-ensure -fmt goimports . <Interface>`; generated files are never hand-edited and are filtered out of coverage with `grep -v -E "_mock.go|/mocks/"`.
- Version injected at link time: `var revision = "unknown"` in each `main` package, set via `-ldflags "-X main.revision=$(REV) -s -w"`. No shared build-info package.

Code style, enforced by `.golangci.yml` and by review:

- `gochecknoinits`: no `init()`. Wiring happens in the composition root (`main`).
- `wrapcheck`/`errorlint`: errors crossing package boundaries are wrapped with `%w` and compared with `errors.Is`/`errors.As`; sentinel errors are exported `Err*` variables.
- `exhaustive` with `default-signifies-exhaustive: true`: switches over `UpdatePhase`/`MessageType` are exhaustive or carry a `default`.
- `gocyclo` minimum complexity 20, `dupl` threshold 100 (excluded in `_test.go`), `govet` shadow enabled, `revive` early-return / indent-error-flow / superfluous-else.
- Consumer-side interfaces: the consumer package declares the narrow interface it needs; the concrete implementation lives in the provider package and is injected at the composition root. Exported surface is kept minimal.
- One `_test.go` per source file, in the same package unless an external test package is genuinely required.

## Planned file map

- `go.mod`, `go.sum`: module `github.com/MrZoidberg/echo-satellite`, Go 1.26; direct dependencies `go-flags` and `testify`.
- `Makefile`: `all`, `build`, `build-device`, `test`, `race`, `lint`, `fmt`, `version`.
- `.golangci.yml`: golangci-lint v2 configuration; the enforcement point for the code-style rules above.
- `.github/workflows/ci.yml`: single build job — test, lint, gofmt check, arm64 cross-build.
- `internal/protocol/{envelope.go,messages.go,capabilities.go,update.go}` plus one `_test.go` each: the wire contract.
- `internal/discovery/{service.go,txt.go,resolve.go}` plus one `_test.go` each: DNS-SD record and gateway resolution order.
- `internal/release/{manifest.go,digest.go,signature.go,policy.go,eligibility.go}` plus one `_test.go` each: release manifest and trust primitives.
- `cmd/echod/`, `cmd/gateway/`, `cmd/echoctl/`, `cmd/dotsim/`: each with `main.go` (entrypoint plus `run()`), `config.go` (options plus `parseArgs`), and `config_test.go`.
- `testdata/updates/`: manifest fixtures — valid, tampered digest, bad signature, unsigned, supervisor-too-new, unknown-field.
- `docs/protocol.md`: the wire contract document that tracks `internal/protocol`.
- `docs/development-windows-wsl.md`, `README.md`: developer documentation.
- `.gitkeep` placeholders for §6 directories that stay empty this milestone: `internal/device/*`, `internal/gateway/*`, `internal/assistant/`, `internal/speech/`, `internal/store/`, `device_payloads/supervisor/`, `services/speech-worker/`, `web/`, `deploy/`, `testdata/audio/`, `testdata/wake/`.
- Modify `AGENTS.md` (repository state, build/test commands, code conventions), `docs/DESIGN.md` §27 (Cobra → go-flags), and `.gitignore` (`.bin/`, coverage artifacts).

## Numbered tasks

### Task 1: Repository-tracked plan document exists

**Status:** completed 2026-08-19

**Purpose:** `AGENTS.md` and `docs/plans/README.md` require non-trivial work to be tracked in-repository.

**Dependencies:** None.

**Hardware required:** no.

**Files or components:**

- Create: `docs/plans/future/2026-08-19-milestone-0-foundation.md`

**Concrete changes:**

- Create this document from the template in `docs/plans/README.md`, carrying the objective, non-goals, reference conventions, source references, file map, and Tasks 2–10 with their verification blocks.
- On execution start, set the owner, start date, and status, and `git mv` the file to `docs/plans/in-progress/`.

**Expected outcome:** A tracked plan document whose lifecycle directory matches its status field.

**Verification:**

```sh
git status --short docs/plans
```

Expected: the plan file is tracked, every template section is populated, and no placeholder text from the template remains.

### Task 2: Go module and §6 package skeleton

**Status:** completed 2026-08-19

**Purpose:** Establish the layout once so that no later task invents paths.

**Dependencies:** Task 1.

**Hardware required:** no.

**Files or components:**

- Create: `go.mod`, `.gitkeep` files across the `docs/DESIGN.md` §6 tree
- Modify: `.gitignore`

**Concrete changes:**

- `go mod init github.com/MrZoidberg/echo-satellite`, with `go 1.26`.
- Add `github.com/jessevdk/go-flags` and `github.com/stretchr/testify` as dependencies.
- Create every directory from `docs/DESIGN.md` §6, with a `.gitkeep` where no code lands this milestone.
- Extend `.gitignore` with `.bin/` and `coverage*.out`.

**Expected outcome:** `go build ./...` succeeds on an empty but complete tree matching §6.

**Verification:**

```sh
go build ./... && go vet ./...
```

Expected: exit 0, and the directory tree matches the `docs/DESIGN.md` §6 block.

### Task 3: Makefile, lint configuration, and CI

**Status:** completed 2026-08-19

**Purpose:** Milestone 0 requires format/test/vet/lint CI; every later task's verification runs through it.

**Dependencies:** Task 2.

**Hardware required:** no.

**Files or components:**

- Create: `Makefile`, `.golangci.yml`, `.github/workflows/ci.yml`

**Concrete changes:**

- `.golangci.yml`: golangci-lint v2 configuration with `default: none` and an explicit enabled-linter list including `errcheck`, `errorlint`, `errname`, `exhaustive`, `gochecknoinits`, `gocritic` (performance, style, experimental tags), `gocyclo`, `gosec`, `govet` (shadow), `ineffassign`, `misspell`, `nilerr`, `nolintlint` (requiring explanation and specificity), `prealloc`, `revive`, `staticcheck`, `testifylint`, `unparam`, `unused`, `wrapcheck`, and `whitespace`, plus exclusions for package-comment noise and for `dupl`/`noctx`/`unused-parameter` inside `_test.go`.
- `Makefile`: `TAG`/`BRANCH`/`HASH`/`TIMESTAMP`/`GIT_REV`/`REV` variables derived from git; `all: test build`; `build` producing all four binaries in `.bin/` with `-ldflags "-X main.revision=$(REV) -s -w"`; `build-device` producing `.bin/linux_arm64/echod` with `GOOS=linux GOARCH=arm64 CGO_ENABLED=0`; `test` running `go clean -testcache` then `go test -race -coverprofile=coverage.out ./...`, filtering mocks out of the profile and printing `go tool cover -func`; `race`; `lint`; `fmt`; `version`; and a `.PHONY` line.
- `.github/workflows/ci.yml`: `permissions: contents: read`; a `build` job on `ubuntu-latest` using `actions/checkout@v7` with `persist-credentials: false`, `actions/setup-go@v7` pinned to Go 1.26, a test step running `go test -race -timeout=100s -covermode=atomic -coverprofile=... ./...` with the mock filter, a `gofmt -l .` check that fails on non-empty output, `golangci/golangci-lint-action@v9` pinned to a 2.12.x version, and a device-build step running `make build-device` and uploading `.bin/linux_arm64/echod` as an artifact.

**Expected outcome:** Lint, test, host build, and device cross-build all run from one command locally and in CI.

**Verification:**

```sh
make lint test build build-device
file .bin/linux_arm64/echod
```

Expected: exit 0; `file` reports a statically linked ARM aarch64 Linux ELF. After pushing the branch, the GitHub Actions `build` job passes.

### Task 4: `internal/protocol` — envelope, message families, capabilities

**Status:** completed 2026-08-19

**Purpose:** The single shared wire contract consumed by `echod`, `gateway`, and `dotsim`.

**Dependencies:** Task 3.

**Hardware required:** no.

**Files or components:**

- Create: `internal/protocol/envelope.go`, `internal/protocol/messages.go`, `internal/protocol/capabilities.go`, `internal/protocol/update.go`
- Test: `internal/protocol/envelope_test.go`, `internal/protocol/messages_test.go`, `internal/protocol/capabilities_test.go`, `internal/protocol/update_test.go`

**Concrete changes:**

- `ProtocolVersion = 1`.
- `Envelope{Type MessageType; ID string; TS time.Time; Payload json.RawMessage}` with `Encode`/`Decode`. An unknown `Type` decodes without error and is reported as unknown, preserving forward compatibility.
- `MessageType` constants for every family in §8.6: `hello`, `welcome`, `config`, `state`, `health`, `log`, `turn.start`, `turn.cancel`, `wake.models`, `wake.status`, `audio.start`, `audio.stop`, `play.start`, `play.stop`, the twelve `update.*` types, `button`, `mute`, `volume`, `ping`, `pong`, and `error`.
- Typed payloads for what Milestones 1–3 need: `Hello{DeviceID, AgentVersion, SupervisorVersion, Protocol, Capabilities, WakeConfigSummary, UpdateState}`, `Welcome{ServerID, Protocol, Config}`, `TurnStart{Trigger, Model, WakeScore, VADScore, PreRollMS}` where `Trigger` is `wake` or `button`, `AudioStart{SampleRate, Channels, Format}`, `AudioStop`, `PlayStart`, `PlayStop`, `State`, `Error{Code, Message}`, and the `update.*` payloads `Offer`, `Progress{Phase, Percent}`, and `Failed{Code, Message}`.
- `Capabilities` as a set type with `Has(Capability) bool` and named capability constants. The package documentation states that feature behavior is negotiated by capability only; no `version >= X` helper may be added to this package (`docs/DESIGN.md` §3.6).
- `UpdatePhase` enum with exactly the §10.7 states, plus `String()` and `ParseUpdatePhase`.
- Package documentation records the binary-frame convention: JSON text frames for control, raw PCM in binary frames, valid only between `audio.start`/`audio.stop` and `play.start`/`play.stop`.
- A guardrail comment stating that `turn.start` is always produced by the device and that nothing in this package allows a gateway to score a wake word.
- Sentinel errors as exported `Err*` variables, wrapped with `%w` where returned.

**Expected outcome:** Both ends of the protocol can be written against one typed package.

**Verification:**

```sh
go test ./internal/protocol/... && golangci-lint run ./internal/protocol/...
```

Expected: exit 0, with tests covering round-trip encode/decode for each typed payload, unknown message types decoding as unknown rather than erroring, `UpdatePhase` parse and format for all thirteen states, and capability-set behavior.

### Task 5: `internal/discovery` — service record and resolution order

**Status:** completed 2026-08-19

**Purpose:** Fix the DNS-SD contract and the §8.5 resolution order now so Milestone 2 only plugs in a real mDNS library behind the interfaces.

**Dependencies:** Task 4.

**Hardware required:** no.

**Files or components:**

- Create: `internal/discovery/service.go`, `internal/discovery/txt.go`, `internal/discovery/resolve.go`
- Test: `internal/discovery/service_test.go`, `internal/discovery/txt_test.go`, `internal/discovery/resolve_test.go`

**Concrete changes:**

- `ServiceType = "_echo-satellite._tcp"`, `Domain = "local."`, `DefaultPort = 8770`, `DeviceServiceType = "_echo-satellite-device._tcp"`, and an instance-name helper producing `echo-satellite-<server-id>`.
- `TXTRecord{Protocol int; ServerID string; TLS bool; Path string}` with `Encode() []string` and `ParseTXT([]string) (TXTRecord, error)`. Unknown keys are ignored. `Validate()` rejects an empty `ServerID` and any key matching a denylist of secret-like names (`token`, `secret`, `key`, `password`, `psk`, `auth`), because TXT carries discovery metadata only (§8.2).
- `Instance{ServerID, Host string; Port int; Addrs []netip.Addr; TXT TXTRecord}`.
- Consumer-side `Advertiser` and `Browser` interfaces (`Advertise(ctx, Instance) error`; `Browse(ctx) ([]Instance, error)`), documented as having their real implementation land in Milestone 2. A `//go:generate moq` directive is added for `Browser` only if the resolver tests need more than a hand-written stub.
- `Config{Discovery string; URL string; PreferredServerID string}` matching the §8.4 YAML, where `Discovery` is `mdns` or `disabled`.
- `Resolver.Resolve(ctx, Config, lastPaired *Instance) (string, error)` implementing §8.5 exactly: an explicit `URL` always wins; then a reachable previously paired instance matched by `server_id` rather than IP address; then a browse, preferring `PreferredServerID` and then protocol-compatible instances. A typed `ErrNoGateway` is returned when nothing resolves.

**Expected outcome:** Gateway resolution order is implemented and testable without multicast.

**Verification:**

```sh
go test ./internal/discovery/... && golangci-lint run ./internal/discovery/...
```

Expected: exit 0, with table tests proving that an explicit URL beats a discovered instance with a different `server_id`; that a paired `server_id` wins after its IP address changes; that the browse fallback selects `PreferredServerID`; that protocol-incompatible instances are skipped; that TXT parsing rejects secret-like keys; and that `ErrNoGateway` is returned when the browser finds nothing.

### Task 6: `internal/release` — manifest, digest, and Ed25519 trust

**Status:** completed 2026-08-19

**Purpose:** The security-critical primitive that both the gateway and the device use, landed before any A/B update code exists.

**Dependencies:** Task 3.

**Hardware required:** no.

**Files or components:**

- Create: `internal/release/manifest.go`, `internal/release/digest.go`, `internal/release/signature.go`, `internal/release/policy.go`, `internal/release/eligibility.go`
- Test: one `_test.go` per source file above
- Create: fixtures under `testdata/updates/`

**Concrete changes:**

- `Manifest` with exactly the §11.1 fields (`schema`, `version`, `build_id`, `architecture`, `size`, `sha256`, `protocol_min`, `protocol_max`, `supervisor_min`, `released_at`), strict decoding that rejects unknown fields, and `Validate()`.
- `CanonicalBytes()` producing the deterministic byte sequence that is signed, documented as mandatory for both signing and verification.
- `VerifyArtifact(r io.Reader, m Manifest) error` streaming the artifact, checking the byte count against `size` and the SHA-256 against `sha256`, with distinct typed errors for size and digest mismatch.
- `Sign(priv ed25519.PrivateKey, m Manifest) ([]byte, error)` and `VerifyManifest(pub ed25519.PublicKey, m Manifest, sig []byte) error`, with the release public key injected at link time and empty by default.
- `TrustPolicy{AllowUnsignedDevBuilds bool}` whose zero value is secure. `Check(m, sig)` rejects missing or invalid signatures unless the flag is set. `StatusNotes() []string` returns a human-readable warning when the escape hatch is enabled, giving §11.3's "clearly surfaced in UI/status" requirement a source. A comment records that the release private key never lives on the gateway or a device.
- `Eligible(m Manifest, deviceProtocol int, supervisorVersion string) error` performing installation-safety checks only: architecture match, `protocol_min <= deviceProtocol <= protocol_max`, and `supervisor_min` satisfied, returning `ErrSupervisorTooOld` so the gateway can mark a release ineligible per §10.9. Documented as not being feature gating.
- Fixtures under `testdata/updates/`: valid manifest, artifact, and signature; tampered artifact; wrong-key signature; unsigned; supervisor-too-new; unknown-field manifest. The test keypair is generated deterministically inside the test, and no private key is committed.

**Expected outcome:** A release bundle can be verified end to end, and every failure mode is a distinct, testable error.

**Verification:**

```sh
go test ./internal/release/... && golangci-lint run ./internal/release/...
```

Expected: exit 0, with tests proving that a valid bundle verifies; that a single flipped artifact byte fails with a digest error; that a wrong size fails with a size error; that a signature from another key fails; that an unsigned release fails under the default policy and passes only with `AllowUnsignedDevBuilds: true` while `StatusNotes()` is non-empty; that a `supervisor_min` above the device's version returns `ErrSupervisorTooOld`; and that unknown manifest fields are rejected.

### Task 7: Four command entrypoints

**Status:** completed 2026-08-19

**Purpose:** Milestone 0 requires the `echod`, `gateway`, `echoctl`, and `dotsim` entrypoints to exist and build for their target platforms.

**Dependencies:** Tasks 4, 5, and 6.

**Hardware required:** no.

**Files or components:**

- Create: `cmd/echod/{main.go,config.go,config_test.go}`, `cmd/gateway/{main.go,config.go,config_test.go}`, `cmd/echoctl/{main.go,config.go,config_test.go}`, `cmd/dotsim/{main.go,config.go,config_test.go}`

**Concrete changes:**

- Each command uses the same composition-root shape: `main.go` holds `main()`, `run() error`, and `var revision = "unknown"`; `config.go` holds the `opts` struct and a unit-tested `parseArgs([]string) (opts, error)`.
- Common flags: `--version` printing `version: <revision>` and exiting 0, `--dbg`, and `--log-level`. Logging uses `log/slog`. Shutdown uses `signal.NotifyContext` for SIGINT and SIGTERM and exits 0.
- `cmd/echod`: `--config` (INI via `flags.IniParser`), `--gateway-url`, `--preferred-server-id`. Logs the resolved `discovery.Config` and the `protocol.Capabilities` it would announce, then blocks until a signal. No audio and no network.
- `cmd/gateway`: `--listen` defaulting to `:8770`, and `--allow-unsigned-dev-builds` defaulting to false, which logs the warning from `release.TrustPolicy.StatusNotes()` when enabled. Logs the `discovery.Instance` and TXT record it would advertise. No HTTP server yet.
- `cmd/echoctl`: a go-flags command tree with a `version` command and a `release verify --manifest --sig --artifact` command calling `internal/release`. The `mic`, `wake`, and `device` groups are deliberately not added; they arrive with their milestones.
- `cmd/dotsim`: flags matching §21 (`--discover`, `--trigger`, `--wake-model`, `--wake-score`, `--vad-score`, `--mic`, `--speaker-out`) parsed and validated into a configuration struct, logged, then exit. Behavior lands in Milestone 2.
- Each command logs explicitly that its subsystem is not yet implemented, so an inert binary is not mistaken for a working one.

**Expected outcome:** All four binaries build for their target platforms, report their version, and expose the release-verification primitive through `echoctl`.

**Verification:**

```sh
make build
.bin/echod --version && .bin/gateway --version && .bin/echoctl version && .bin/dotsim --version
.bin/echoctl release verify --manifest testdata/updates/valid/manifest.json \
  --sig testdata/updates/valid/manifest.sig --artifact testdata/updates/valid/echod
go test ./cmd/...
```

Expected: the version commands exit 0 and print the git-derived revision; `release verify` exits 0 on the valid fixture and non-zero with a clear message on the tampered fixture; `parseArgs` tests pass; `make build-device` still produces an arm64 `echod`.

### Task 8: `docs/protocol.md`

**Status:** completed 2026-08-19

**Purpose:** `docs/DESIGN.md` §8.6 states that exact schemas belong in `docs/protocol.md`, and §6 lists the file.

**Dependencies:** Task 4.

**Hardware required:** no.

**Files or components:**

- Create: `docs/protocol.md`

**Concrete changes:**

- Document the transport (one long-lived outbound WSS connection per device, JSON text control frames, binary PCM frames), the envelope shape, and the full message-family table with direction (device→gateway or gateway→device) and status (`defined` or `reserved`).
- Document the payload schemas for the types implemented in Task 4, the update phase list, and the capability-negotiation rule.
- State explicitly that the gateway never receives idle microphone audio and never scores wake words.
- Record that this document tracks `internal/protocol` and must change in the same commit as any wire change.

**Expected outcome:** A wire-contract document that is complete with respect to the implemented message types.

**Verification:**

```sh
grep -o '"[a-z]*\.\?[a-z_]*"' internal/protocol/messages.go | tr -d '"' | sort -u \
  | while read t; do grep -q "$t" docs/protocol.md || echo "MISSING: $t"; done
```

Expected: no `MISSING:` output.

### Task 9: `docs/DESIGN.md` §27 amendment — Cobra to go-flags

**Status:** completed 2026-08-19

**Purpose:** `AGENTS.md` requires a changed design assumption to be recorded in the same change. The stack table currently specifies Cobra, while this milestone standardizes on go-flags.

**Dependencies:** Task 7.

**Hardware required:** no.

**Files or components:**

- Modify: `docs/DESIGN.md` §27

**Concrete changes:**

- Change the "Provisioning CLI" row to `Go + go-flags`.
- Add one sentence recording that the repository standardizes on `jessevdk/go-flags` across all binaries for a single flag, configuration-file, and environment-variable precedence model.
- Record the decision in this plan's progress log. Change nothing else in §27.

**Expected outcome:** The design document and the code agree on the CLI library.

**Verification:**

```sh
grep -n -i cobra docs/DESIGN.md
```

Expected: no results, and the §27 "Provisioning CLI" row reads `Go + go-flags`.

### Task 10: Developer documentation and `AGENTS.md` update

**Status:** completed 2026-08-19

**Purpose:** `AGENTS.md` explicitly requires build/test commands when the first code lands, and Milestone 0 requires Windows/WSL documentation.

**Dependencies:** Tasks 3 and 7.

**Hardware required:** no.

**Files or components:**

- Create: `docs/development-windows-wsl.md`, `README.md`
- Modify: `AGENTS.md`

**Concrete changes:**

- `docs/development-windows-wsl.md` from §22: the WSL2 toolchain (Go, Python with uv, make, Docker CLI, golangci-lint), `export ADB=/mnt/c/Android/platform-tools/adb.exe`, the `GOOS=linux GOARCH=arm64 CGO_ENABLED=0` device build, the early ADB-push iteration loop, the note that A/B OTA becomes the preferred loop once implemented, and the caveat that mDNS advertisement from Docker or WSL must be tested on the physical LAN with a static URL as fallback.
- `README.md`: what the project is, the two hard boundaries summarized in two short paragraphs linking `AGENTS.md`, a quick start (`make test`, `make build`), a layout summary, and pointers to `docs/DESIGN.md`, `docs/protocol.md`, and `docs/plans/README.md`. It states that Milestone 0 delivers contracts rather than behavior.
- `AGENTS.md`: replace the "Repository state" paragraph, since the module now exists; add a "Build and test commands" section listing `make all`, `make test`, `make race`, `make lint`, `make fmt`, `make build`, `make build-device`, `go test ./internal/<pkg>/...`, and `go generate ./...`; and add a short "Code conventions" section recording go-flags, testify, moq, the no-`init()` rule, wrapped errors, consumer-side interfaces, one `_test.go` per source file, and that `.golangci.yml` is the enforcement point. Everything else stays intact.

**Expected outcome:** A new contributor or agent can build, test, and lint the repository from the documentation alone.

**Verification:**

```sh
make lint test
```

Expected: exit 0. `AGENTS.md` no longer claims the repository is documentation-only, and every command it lists runs successfully verbatim from the repository root.

## Cross-task risks

- **Contracts drifting from the design document.** Impact: later milestones implement a wire format the design does not describe. Mitigation: Tasks 4–6 mirror §8.6, §10.7, and §11.1 field for field, and Task 8's verification grep keeps `docs/protocol.md` synchronized with the code.
- **Scope creep into Milestones 2 and 3.** Impact: a real mDNS library or WSS handler lands without its own plan or verification. Mitigation: interfaces carry "real implementation lands in Milestone N" comments; adding one is a plan amendment, not an inline decision.
- **A strict lint configuration on a brand-new codebase.** Impact: `wrapcheck`, `gosec`, `dupl`, and `gocritic` produce findings on first-draft code, tempting blanket suppressions. Mitigation: run `make lint` per task rather than at the end, fix rather than suppress, and rely on `nolintlint` requiring both a specific linter and an explanation.
- **Silent boundary erosion.** Impact: a helper that lets a gateway score wake audio, or a `version >=` feature gate, violates §3.2 and §3.6. Mitigation: explicit guardrail comments in `internal/protocol`, checked during review.
- **Inert binaries read as working software.** Impact: a later reader assumes `echod` connects to a gateway. Mitigation: each command logs that its subsystem is unimplemented, and the README states that this milestone delivers contracts, not behavior.

## Rollback or recovery

All work is additive on the `milestone-0-foundation` branch off `master`. Nothing is deployed and no device is touched, so recovery from a partial or failed execution is `git checkout master` and deleting the branch. `AGENTS.md` and `docs/DESIGN.md` §27 are the only pre-existing files modified, both as single-section edits that can be reverted independently.

## Final acceptance criteria

- [x] `make lint test build build-device` passes locally.
- [ ] The GitHub Actions build job is green. **Open:** the branch has not been pushed yet.
- [x] `.bin/linux_arm64/echod` is a statically linked `linux/arm64` binary, and all four binaries report the git-derived revision via `--version`.
- [x] `internal/protocol`, `internal/discovery`, and `internal/release` are covered by tests for the behaviors listed in Tasks 4–6, and `golangci-lint run` is clean with no blanket suppressions.
- [x] `echoctl release verify` accepts the valid fixture and rejects the tampered fixture.
- [x] The directory layout matches `docs/DESIGN.md` §6, and the tooling matches the reference conventions recorded above.
- [x] `docs/protocol.md` covers every `MessageType`; `docs/development-windows-wsl.md` and `README.md` exist.
- [x] `docs/DESIGN.md` §27 records go-flags, and `AGENTS.md` reflects that code exists and lists working build/test commands and conventions.
- [x] No gateway-side wake-scoring surface, no `version >=` feature gate, no secrets in TXT records, and unsigned releases rejected by default.

## Progress log

- 2026-08-19: Plan created from `docs/DESIGN.md` §24 Milestone 0. Decisions recorded before execution: implementation depth is types plus tested primitives (no microphone, no live WSS server, no real mDNS); CI is GitHub Actions; the local task runner is a Makefile; tooling, lint configuration, test stack, and build/CI shape follow `umputun/revdiff`; `jessevdk/go-flags` replaces Cobra for all binaries, superseding the `docs/DESIGN.md` §27 entry (Task 9); dependencies are not vendored.
- 2026-08-19: Tasks 1 and 2 complete. Module `github.com/MrZoidberg/echo-satellite` on Go 1.26, full §6 directory tree with `.gitkeep` placeholders, `.gitignore` extended with `.bin/` and `coverage*.out`. Task 2's `go build ./... && go vet ./...` initially reported "matched no packages" because no Go file existed yet; it was re-run after Task 4 and passes.
- 2026-08-19: Task 3 complete. `.golangci.yml` copied from revdiff verbatim (v2 schema, ~40 linters). Makefile follows revdiff's shape with a git-derived `REV` and `-X main.revision`; `build` loops over the four commands into `.bin/`, and `fmt-check` was added so CI and local runs share one gofmt gate. CI is a single `build` job: gofmt check, `go test -race -timeout=100s -covermode=atomic`, `golangci-lint-action@v9` pinned to v2.12.2, `make build-device`, and an arm64 artifact upload.
- 2026-08-19: Task 4 complete. Two counts in the plan text were wrong against `docs/DESIGN.md` §8.6 and §10.7 and the design was followed instead of the plan: there are **ten** `update.*` message types (not twelve) and **twelve** update phases (not thirteen). `ErrInvalidTurnTrigger` was dropped in favour of `TurnTrigger.Valid()` to keep the exported surface minimal. Two `nolint` suppressions were needed and both are specific and explained: `gosec` G101 false-positives on the `wake.model_sync` capability constant, and `misspell` on the `cancelled` phase, whose spelling is fixed by the design document's wire value.
- 2026-08-19: Task 5 complete. `Resolver.Resolve` implements the §8.5 order over an injected `Browser`; a hand-written stub replaced a moq mock because `Browser` has a single method. Documented deviation: reachability of a previously paired gateway is **not** probed here. Resolve returns the paired candidate, and the caller retries with `lastPaired == nil` after a failed connection, so the browse path is reached without putting a dialer inside the resolution logic. `ParseTXT` rejects credential-shaped keys outright rather than ignoring them.
- 2026-08-19: Task 6 complete. Deviation from the plan's signature: `supervisor_min` is an integer in the §11.1 manifest, so eligibility takes `release.Device{Architecture, Protocol, SupervisorVersion int}` rather than a supervisor version string. Manifest signatures are domain-separated (`echo-satellite/release-manifest/v1`) so a signature can never be replayed for another purpose. Fixtures under `testdata/updates/` are generated from a committed test seed by `TestFixtures_Regenerate` and guarded against drift by `TestFixtures_MatchGenerator`; no private key is committed, and each fixture directory carries the matching public key.
- 2026-08-19: Task 7 complete. All four binaries use go-flags with a unit-tested `parseArgs`; `echod` and `gateway` load an ini file underneath the command line by parsing twice, so an explicit flag always wins over a config value. Deviation from the plan's documented verification command: `echoctl release verify` needs `--pubkey` for the fixtures, because this build embeds no release key and the secure default rejects an unverifiable signature. The command is `--manifest --sig --pubkey --artifact`; the plan's shorter form would have failed by design.
- 2026-08-19: Tasks 8, 9 and 10 complete. `docs/protocol.md` covers every `MessageType` (verified by the grep in Task 8) and states the boundary guarantees as protocol properties. `docs/DESIGN.md` §27 now reads `Go + go-flags` with a sentence recording why. `AGENTS.md` no longer claims the repository is documentation-only and gained "Build and test commands" and "Code conventions" sections.
- 2026-08-19: Full verification run: `make fmt-check`, `make lint` (0 issues), `make test` (all packages pass, 74.5% statement coverage), `make build`, `make build-device` (static ARM aarch64 ELF), all four `--version` commands, `echoctl release verify` on the valid and tampered fixtures, and SIGTERM/SIGINT shutdown of `gateway` and `echod`. CI was not observed on GitHub: the branch has not been pushed, which is the one acceptance item verified locally only.

## Completion evidence

All checks were run on macOS (darwin/arm64) with Go 1.26.5 and golangci-lint 2.12.2 on 2026-08-19.

- `go build ./... && go vet ./...` — exit 0.
- `make fmt-check` — exit 0, no unformatted files.
- `make lint` — `0 issues.`
- `make test` — `ok` for all seven packages (`internal/protocol`, `internal/discovery`, `internal/release`, `cmd/echod`, `cmd/gateway`, `cmd/echoctl`, `cmd/dotsim`); 74.5% of statements covered.
- `make build` — `.bin/{echod,gateway,echoctl,dotsim}` built with the git-derived revision.
- `make build-device` — `.bin/linux_arm64/echod`: `ELF 64-bit LSB executable, ARM aarch64, statically linked, stripped`.
- `.bin/echod --version`, `.bin/gateway --version`, `.bin/echoctl version`, `.bin/dotsim --version` — each printed `version: milestone-0-foundation-c2e2954-20260819T115152`.
- `.bin/echoctl release verify --manifest testdata/updates/valid/manifest.json --sig … --pubkey … --artifact …` — exit 0, `result: release bundle accepted`.
- The same command against `testdata/updates/tampered/` — exit 1, `release: artifact digest does not match manifest`.
- `.bin/gateway --server-id=home-gateway --tls` then SIGTERM — logged the TXT record it would advertise (`[protocol=1 server_id=home-gateway tls=1 path=/device]`), warned that no release public key is configured, and exited 0.
- `.bin/echod --gateway-url=… --device-id=dot-kitchen` then SIGINT — resolved the explicit endpoint, logged capabilities including `wake.local`, and exited 0.
- Task 8 grep over `internal/protocol/messages.go` against `docs/protocol.md` — no `MISSING:` output.
- `grep -n -i cobra docs/DESIGN.md` — no results.

### Limitations and follow-up work

- **CI has not been observed green.** `.github/workflows/ci.yml` is written and its steps were run locally, but the branch has not been pushed, so the GitHub Actions run itself is unverified. Confirm on first push.
- **No hardware verification, and none required.** Nothing in this milestone touches a device. The first hardware-dependent checks belong to Milestone 1.
- `moq` was not needed: the only interface introduced this milestone (`discovery.Browser`) has one method and is stubbed by hand. The `go generate` convention is documented but currently exercises nothing.
- `internal/release` has no `//go:generate` mocks and `EmbeddedPublicKey` is only covered on its empty-key path, since no build injects a release key yet. Signing infrastructure and a release workflow are deliberately deferred.
- Reserved protocol message types (`config`, `health`, `log`, `turn.cancel`, `wake.*`, most `update.*`, `button`, `mute`, `volume`) have names but no payload structs. They gain payloads with the milestone that needs them; `docs/protocol.md` marks each one's status.
- Placeholder directories from `docs/DESIGN.md` §6 hold only `.gitkeep`. They exist so later work lands in the documented location rather than inventing one.
