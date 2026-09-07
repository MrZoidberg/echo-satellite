# Milestone 2 — Discovery, protocol, simulator, and device turns implementation plan

**Status:** in-progress (blocked)
**Owner or active agent:** /root
**Created:** 2026-08-27
**Updated:** 2026-09-07
**Started:** 2026-08-30
**Completed:** pending lifecycle move

## Objective

Deliver a simulator-first network vertical slice in which a host-run gateway
advertises itself through mDNS and accepts authenticated WSS device sessions;
`dotsim` discovers or explicitly connects to it, registers capabilities,
applies versioned configuration, forwards structured logs, reconnects, and
streams endpointed PCM; and `echod` performs the same flow after a local wake or
Action-button trigger through its existing single microphone capture path.

Device-local command endpointing stops active-turn audio and reports
`audio.stop(reason="endpointed")`. The gateway validates turn framing and may
write explicitly enabled diagnostic WAV files, but raw audio is not stored by
default. The gateway is also packaged and smoke-tested through Docker Compose
using an explicit WSS URL; Docker multicast is not treated as mDNS acceptance.

## Non-goals

- Gateway-side wake scoring, wake VAD, or continuous idle microphone streaming.
- Gateway-side command-silence detection. Wake VAD and command endpointing are
  separate device-local components with independent detector state and config.
- STT, TTS, assistant backends, conversations, or response playback.
- Gateway-to-device `play.start`/binary/`play.stop` beyond protocol state tests.
- SQLite, the management UI, or durable gateway device/session state.
- The stable supervisor, A/B slots, artifact staging, trial, rollback, or OTA.
- Wake-model asset synchronization. Pushed wake config may select only an
  already installed and verified model.
- Production device authentication, per-device credentials, certificate
  pinning, or mTLS. Milestone 2's shared bearer token is development-only.
- Docker-based mDNS acceptance; Compose tests use an explicit WSS URL.

## Source references and constraints

- `docs/DESIGN.md` §§3, 6–9, 19–21, 24, and 27 govern the voice boundary,
  package layout, discovery, transport, security, observability, simulator, and
  Milestone 2 scope.
- `docs/protocol.md` is authoritative for the wire format and changes in the
  same task as `internal/protocol`.
- `docs/plans/finished/2026-08-19-milestone-1-hardware-wake.md` defines the
  existing `audio.Fanout`, `wake.Event.PreRoll`, local wake pipeline, model
  store, LED/button behavior, and the requirement that turn streaming add a
  subscriber instead of reopening ALSA.
- `AGENTS.md` must change because this plan deliberately moves v0.1 command
  endpointing from the gateway to the device. The two hard boundaries remain:
  wake acceptance is always device-local, and gateway/device update ownership
  is unchanged.
- Explicit gateway URL remains the highest-precedence resolution source.
- Capability checks, never agent-version comparisons, control behavior.
- Raw audio is never logged and is persisted only when the operator explicitly
  configures a diagnostic WAV directory.
- Selected dependencies are `github.com/coder/websocket`,
  `github.com/betamos/zeroconf`, and `github.com/pelletier/go-toml/v2`.
- All four binaries retain `jessevdk/go-flags`, with flag, environment, ini,
  then default precedence for local CLI configuration.
- Every task ends with focused checks plus `make fmt-check`, `make lint`, and
  `make test`. Findings are fixed rather than broadly suppressed.

## Dependencies and prerequisites

- Milestone 1 is merged into `master` before execution begins.
- Refresh local refs and confirm the execution base contains the completed
  Milestone 1 packages, hardware findings, and plan before creating the
  Milestone 2 branch.
- Inspect `docs/plans/in-progress/` for overlapping work and record any shared
  scope before moving this plan to `in-progress/`.
- Execute in a dedicated clean branch or worktree. Preserve unrelated changes
  in the current checkout.
- Hardware acceptance requires the rooted Echo Dot Gen 2 used for Milestone 1,
  its qualified installed wake model, working mic/LED/buttons, Magisk root, and
  Windows ADB reachable from WSL.
- Local-network acceptance requires the host and Dot to share an mDNS-capable
  multicast domain.
- Gateway startup requires PEM certificate/key files, a shared development
  token file containing at least 32 random bytes, and a TOML profile file.

## Architecture and high-level plan

Implementation is simulator-first:

1. Amend the governing boundary and wire contracts.
2. Implement device configuration persistence and command endpointing.
3. Implement mDNS and authenticated paired-gateway persistence.
4. Implement versioned gateway TOML profiles.
5. Implement the shared device-side WSS/reconnect/log/turn client.
6. Implement the gateway WSS endpoint, registry, config delivery, and turn
   receiver.
7. Complete `dotsim` and prove the host-run end-to-end loop.
8. Add Docker packaging and an explicit-URL smoke test.
9. Integrate the proven path into `echod`.
10. Validate real network and hardware behavior.
11. Run cross-cutting checks and fresh-context review.

### Versioned configuration

The gateway loads one TOML document with an operator-managed monotonic
`version`, a complete default profile, and optional per-device overrides:

```toml
version = 1

[defaults.wake]
engine = "openwakeword"
model = "okay_nabu"
threshold = 0.50
vad_enabled = true
vad_threshold = 0.50
vad_lookback_ms = 1200
pre_roll_ms = 600
min_interval_ms = 2000
always_score_wake = true

[defaults.endpointing]
speech_threshold = 0.50
speech_onset_ms = 160
trailing_silence_ms = 1500
no_speech_timeout_ms = 3000
max_turn_ms = 60000

[defaults.logs]
forward_level = "info"

[devices."dot-kitchen".endpointing]
trailing_silence_ms = 1000
```

Rules:

- Local CLI/environment/ini values provide version-0 bootstrap defaults.
- In normal connected operation, persisted or pushed gateway config version 1+
  owns wake, endpointing, and log-forwarding settings.
- `--wake-only` remains fully local and ignores gateway-persisted config.
- SIGHUP reload succeeds only when the complete TOML validates and `version` is
  greater than the running gateway version.
- Devices reject lower versions; acknowledge equal, byte-equivalent effective
  config idempotently; reject equal-but-different config; and validate higher
  versions atomically.
- A valid wake-model change received during a turn reports `pending`, applies
  only after idle, then reports `applied`.
- Missing models, unsupported engines, invalid settings, or persistence errors
  reject the entire revision and leave the prior runtime untouched.
- `echod` atomically persists the last-known-good typed config and version.
  Corrupt or unusable state is reported and falls back to local version 0.
- `hello` reports the active config version. `welcome` always carries the
  gateway's effective config, making equal-version conflicts observable.

### Transport and session behavior

- Only `wss` device endpoints are accepted.
- Authentication uses `Authorization: Bearer TOKEN_BYTES_FROM_FILE` before
  WebSocket upgrade.
- TLS verification is enabled by default. Device/dotsim
  `--tls-skip-verify` is an explicit development escape hatch that logs a
  persistent warning but does not disable WSS encryption.
- The first frame must be a valid `hello` within five seconds.
- Control and binary messages are limited to 64 KiB each.
- Heartbeats run every 15 seconds with a 10-second response deadline.
- Reconnect starts at 500 ms, doubles to a 30-second cap, uses full jitter, and
  resets only after a valid `welcome`.
- Resolution order is explicit URL, persisted authenticated gateway, then mDNS.
- The newest authenticated session replaces an older session with the same
  `device_id`.
- A high-priority control/audio queue and bounded low-priority log queue feed
  one WSS writer. Log pressure drops and counts records; it cannot block audio.
- In this Milestone 2 slice, disconnect cancels any active turn. A later design
  will define reconnect turn resumption with explicit turn detection and a
  timeout; it is not implemented here.

### Device-local command endpointing

A separate continuously warmed `vadlevel.Detector` observes canonical audio but
does not participate in wake acceptance. For each active turn it:

- transmits wake pre-roll first but excludes it from command-speech decisions;
- requires 160 ms of consecutive speech score at or above 0.50;
- stops after 1.5 seconds of consecutive sub-threshold audio after speech onset;
- stops with `no_speech` if onset does not occur within three seconds;
- stops with `timeout` when transmitted audio reaches 60 seconds;
- uses `eof` when a simulator fixture ends first;
- uses `capture_overrun` if active-turn fanout delivery drops frames;
- includes trailing silence in transmitted audio; and
- snapshots endpointing config at turn start.

### Wire and internal interfaces

`internal/protocol` and `docs/protocol.md` gain:

- `Hello.ConfigVersion uint64`.
- Typed `DeviceConfig{Version, Wake, Endpointing, Logs}`.
- Typed `Welcome.Config`; `config` uses the same `DeviceConfig` payload.
- New `config.result` with version, `pending|applied|rejected`, code, and detail.
- A typed `LogRecord` with level, message, and sanitized structured fields.
- Typed `AudioStopReason` constants: `endpointed`, `no_speech`, `timeout`,
  `eof`, and `capture_overrun`.
- Capability `command.endpointing.local`.
- Validation for all new enums, durations, thresholds, and required fields.

Protocol version 1 owns the config schema; config `version` is only monotonic
desired-state ordering. No externally public Go API is introduced. Interfaces
are declared by their consumers and all implementation remains under
`internal/`.

## Planned file map

- `internal/protocol`: versioned config, result, log, capability, stop-reason,
  and framing contracts.
- `internal/device/config`: persisted last-known-good runtime config and atomic
  application rules.
- `internal/device/endpointing`: command-speech state machine over `vadlevel`.
- `internal/device/client`: discovery, WSS, auth, reconnect, queues, heartbeat,
  config handling, logs, and turn transmission shared by `dotsim` and `echod`.
- `internal/discovery/mdns`: `betamos/zeroconf` implementations of the existing
  browser and advertiser interfaces.
- `internal/gateway/config`: strict TOML loading, merging, version enforcement,
  and immutable reload snapshots.
- `internal/gateway/devices`: authenticated sessions, registration,
  capabilities, duplicate replacement, and config results.
- `internal/gateway/turns`: per-device turn framing, PCM validation, and opt-in
  WAV sinks.
- `cmd/gateway`, `cmd/dotsim`, `cmd/echod`: composition roots only.
- `deploy/`: non-root gateway image, Compose service, and safe examples.
- `testdata/audio`: generated command-speech/trailing-silence fixtures.
- `docs/DESIGN.md`, `docs/protocol.md`, `AGENTS.md`, `README.md`, and diagnostic
  docs: changed behavior, wire contract, operation, and hardware evidence.

## Numbered tasks

The orchestrating session owns this plan document, task claims, shared state,
final checks, and review triage. When several tasks execute in one session,
dispatch one subagent per task. Dependent tasks run in order, and subagents do
not edit this plan.

### Task 1: Repository-tracked plan document exists

**Status:** completed 2026-08-27

**Purpose:** `AGENTS.md` and `docs/plans/README.md` require non-trivial work to
be tracked in the repository before implementation begins.

**Dependencies:** None.

**Hardware required:** no.

**Files or components:**

- Create: `docs/plans/future/2026-08-27-milestone-2-discovery-protocol-simulator.md`

**Concrete changes:**

- Create this document from the repository template with the approved design
  decisions, numbered tasks, dependencies, verification, risks, recovery, and
  acceptance criteria.
- On execution start, set owner/start metadata and `git mv` the file to
  `docs/plans/in-progress/`.

**Expected outcome:** A tracked plan whose lifecycle directory matches its
status and whose implementation requires no unresolved design decisions.

**Verification:**

```sh
git status --short docs/plans
rg -n "T[B]D|T[O]DO|<Featur[e]>|YYYY-MM-D[D]|exact/pat[h]|State wor[k]" \
  docs/plans/future/2026-08-27-milestone-2-discovery-protocol-simulator.md
```

Expected: the new plan is the only intended plan change and the placeholder
scan returns no output.

### Task 2: Update the governing boundary and wire contracts

**Status:** completed 2026-08-30

**Purpose:** Make device-owned endpointing and versioned config authoritative
before implementation.

**Dependencies:** Task 1.

**Hardware required:** no.

**Files or components:**

- Modify: `docs/DESIGN.md`, `docs/protocol.md`, `AGENTS.md`
- Modify/test: `internal/protocol`

**Concrete changes:**

- Replace every v0.1 gateway-command-endpointing statement, diagram, milestone
  item, repository-layout entry, and stack row with separate device-local
  command endpointing.
- Update Milestone 5 to consume already endpointed audio for STT.
- Document shared-token authentication and TLS verification bypass as
  development-only limitations, preserving per-device auth/mTLS as follow-up.
- Add every type and validation listed in “Wire and internal interfaces.”
- Require non-empty correlation IDs for `turn.start`, `audio.start`,
  `audio.stop`, and `config`; one turn reuses one ID and binary PCM is associated
  with the single active input window.
- Keep unknown message-type forward compatibility.

**Expected outcome:** Documentation and code describe one consistent local-wake,
local-endpointing protocol with no idle-audio path.

**Verification:**

```sh
go test -race ./internal/protocol/...
rg -n "gateway command endpointing|gateway-side initially|command endpointing.*gateway" \
  docs/DESIGN.md docs/protocol.md AGENTS.md
make fmt-check
make lint
make test
```

Expected: all checks pass and the `rg` command finds no stale v0.1
gateway-endpointing claim.

### Task 3: Device config persistence and endpointing core

**Status:** completed 2026-08-30

**Purpose:** Provide deterministic components used first by dotsim and later by
echod.

**Dependencies:** Task 2.

**Hardware required:** no.

**Files or components:**

- Create/test: `internal/device/config`
- Create/test: `internal/device/endpointing`
- Add: generated fixtures under `testdata/audio`

**Concrete changes:**

- Implement strict conversion between protocol config and internal
  wake/endpoint/log settings.
- Implement version ordering, equal-version content comparison, version-0
  bootstrap behavior, and atomic JSON persistence through staged write, file
  sync, rename, and directory sync.
- Preserve corrupt persisted state for diagnosis while returning a typed error.
- Implement idle, waiting-for-speech, in-speech, and completed endpoint states
  over a separate continuously warmed `vadlevel.Detector`.
- Snapshot config per turn and expose the idle boundary used to apply staged
  revisions.
- Generate a fixture containing room noise, speech, a sub-1.5-second internal
  pause, more speech, and at least 1.5 seconds of trailing silence.
- Test onset, internal pauses, trailing silence, no speech, hard timeout, EOF,
  cancellation, config snapshotting, invalid/non-finite values, stale and
  conflicting versions, interrupted persistence, and restart recovery.

**Expected outcome:** Endpoint and config decisions are reproducible without
sockets or hardware.

**Verification:**

```sh
go test -race ./internal/device/config/... ./internal/device/endpointing/... \
  ./internal/device/wake/vadlevel/...
go test -race -count=20 ./internal/device/endpointing/...
make fmt-check
make lint
make test
```

Expected: all tests pass repeatedly and no test uses sleeps as synchronization.

#### Task 3 review remediation: fixture location and transmitted turn limit

**Status:** completed 2026-08-30

**Dependencies:** Task 3.

**Files or components:**

- Modify/test: `internal/device/endpointing`
- Add: `testdata/audio/command_endpointing_16k_mono.wav`

**Concrete changes:**

- Correct the fixture path so regeneration writes to the repository-tracked
  fixture directory and verify the WAV's duration and noise/speech sections.
- Count transmitted wake pre-roll toward the hard turn deadline while continuing
  to exclude it from onset and trailing-silence decisions.

**Verification:**

Run:

```sh
go test -race ./internal/device/endpointing/...
go test -race -count=20 ./internal/device/endpointing/...
```

Expected: the fixture is available in a clean checkout and pre-roll cannot make
a transmitted turn exceed its maximum duration.

### Task 4: Real mDNS and authenticated paired-gateway persistence

**Status:** completed 2026-08-30

**Purpose:** Fill the existing discovery interfaces without coupling composition
roots to the selected library.

**Dependencies:** Task 2.

**Hardware required:** no for implementation; real multicast is Tasks 8 and 11.

**Files or components:**

- Create/test: `internal/discovery/mdns`
- Create/test: paired-gateway state in `internal/discovery`
- Modify: `go.mod`, `go.sum`

**Concrete changes:**

- Back `discovery.Advertiser` and `discovery.Browser` with
  `betamos/zeroconf`.
- Advertise `_echo-satellite._tcp.local.` with protocol, server ID, `tls=1`,
  and device path only.
- Browse for three seconds, validate TXT, discard malformed/incompatible
  records, deduplicate, and preserve deterministic preferred-server behavior.
- Shut down promptly on context cancellation.
- Persist only the authenticated `server_id` and endpoint metadata, never
  credentials, through an atomic state store.
- Provide the strict atomic paired-gateway store that Task 6 writes after a
  valid `welcome`; Task 6 also owns retrying without a paired candidate after
  a connection failure because that behavior requires the WSS session.
- Test IPv4/IPv6 conversion, malformed and secret-like TXT, cancellation,
  deduplication, preferred selection, corrupt state, and atomic replacement.

**Expected outcome:** Discovery remembers the last authenticated gateway while
retaining explicit URL precedence and mDNS fallback.

**Verification:**

```sh
go test -race ./internal/discovery/...
go test -race -count=10 ./internal/discovery/...
make check-portability
make fmt-check
make lint
make test
```

Expected: deterministic tests pass without multicast access in CI.

#### Task 4 review remediation: secure record and strict state validation

**Status:** completed 2026-08-30

**Dependencies:** Task 4.

**Files or components:**

- Modify/test: `internal/discovery`, `internal/discovery/mdns`

**Concrete changes:**

- Reject plaintext or non-device-path advertisements, reject unknown fields in
  paired state (including credentials), and preserve corrupt files.
- Deduplicate mDNS responses by stable `server_id`, selecting conflicting
  endpoint metadata deterministically and merging address sets only for the
  same endpoint.
- Test cancellation while a browse is active without sleep-based timing.

**Verification:**

Run:

```sh
go test -race ./internal/discovery/...
go test -race -count=10 ./internal/discovery/...
make check-portability
make fmt-check
make lint
make test
```

Expected: all checks pass, and no malformed discovery state becomes trusted.

### Task 5: Versioned gateway TOML profiles

**Status:** completed 2026-08-30

**Purpose:** Give config push an operator-usable source before SQLite and the UI.

**Dependencies:** Tasks 2 and 4. This dependency serializes `go.mod` ownership.

**Hardware required:** no.

**Files or components:**

- Create/test: `internal/gateway/config`
- Add: valid and invalid TOML fixtures
- Modify: `go.mod`, `go.sum`

**Concrete changes:**

- Decode TOML with `pelletier/go-toml/v2` into a complete default profile and
  partial per-device overrides.
- Reject unknown fields, incomplete defaults, empty device IDs, invalid values,
  version 0, and ambiguous overrides.
- Produce one complete typed effective config per device.
- Implement immutable snapshots and validate-before-swap reload.
- Reject reload when the new version is not greater than the active version.
- Return the affected effective configs so the gateway can push every connected
  device and retain its latest `config.result` in memory.
- Keep tokens, certificates, and other secrets outside the TOML profile.

**Expected outcome:** Valid TOML deterministically produces versioned desired
state and an invalid SIGHUP cannot alter running config.

**Verification:**

```sh
go test -race ./internal/gateway/config/...
go test -race -count=20 ./internal/gateway/config/...
make fmt-check
make lint
make test
```

Expected: merge, strict validation, monotonic reload, and immutability tests pass.

### Task 6: Shared device-side WSS session client

**Status:** completed 2026-08-30

**Purpose:** Share one tested connection implementation between dotsim and echod.

**Dependencies:** Tasks 2–5.

**Hardware required:** no.

**Files or components:**

- Create/test: `internal/device/client`
- Modify: `go.mod`, `go.sum`

**Concrete changes:**

- Use `coder/websocket` for authenticated WSS dialing, text/binary framing,
  ping/pong, close handling, and limits.
- Load a token file, trim surrounding whitespace, require 32 bytes, compare
  without exposing it, and never include it in logs.
- Require WSS and implement explicit TLS verification bypass with stable warning
  fields.
- Implement explicit/persisted/mDNS resolution, pairing, and full-jitter
  reconnect with injected clock and randomness.
- Send `hello`, validate `welcome`, invoke a config consumer, and send
  `config.result`.
- Serialize one high-priority turn/control queue and one bounded 256-record log
  queue. Sanitize credential-shaped fields and bound field values.
- Support one outbound turn with strict ordering and canonical little-endian PCM.
- End an overrun turn with `capture_overrun`, cancel on disconnect, and never
  queue idle or disconnected microphone audio.
- Inject dialer, discovery, pairing store, config consumer, turn source, clock,
  and jitter for deterministic tests.

**Expected outcome:** A fake device can discover, authenticate, configure, log,
stream a turn, and reconnect without composition-root behavior.

**Verification:**

```sh
go test -race ./internal/device/client/...
go test -race -count=20 ./internal/device/client/...
make check-portability
make fmt-check
make lint
make test
```

Expected: handshake, timeout, reconnect, queue priority, redaction, config, and
turn-order tests pass under the race detector.

### Task 7: Gateway WSS endpoint, registry, config delivery, and turn receiver

**Status:** completed 2026-08-30

**Purpose:** Provide the server half of the simulator-first vertical slice.

**Dependencies:** Tasks 2, 5, and 6.

**Hardware required:** no.

**Files or components:**

- Create/test: `internal/gateway/devices`, `internal/gateway/turns`
- Modify/test: `cmd/gateway`

**Concrete changes:**

- Add certificate, key, token-file, TOML-profile, mDNS enablement, and optional
  diagnostic-WAV flags.
- Remove optional plaintext advertisement; the device endpoint advertises WSS.
- Authenticate before upgrade with constant-time token comparison and return
  HTTP 401 without revealing details.
- Require `hello` in five seconds, validate protocol/capabilities, always send a
  typed `welcome` config, then register.
- Atomically replace an older session with the same device ID.
- Keep in-memory metadata, config result, last seen, capabilities, wake summary,
  and active turn.
- Enforce turn framing and 64-KiB limits. Invalid sequencing returns a protocol
  error; binary outside an audio window is ignored rather than buffered.
- Accept Milestone 2 audio only as mono 16-kHz `pcm_s16le`.
- Discard PCM by default. Under explicit diagnostic configuration, write a
  collision-safe staged WAV and atomically promote it on successful completion.
- Receive bounded structured logs with device/session context.
- On SIGHUP, validate and swap TOML before pushing the new version; retain the
  prior snapshot after any failure.
- Run WSS and mDNS under one context and close sessions gracefully.
- Expose `/healthz` with no device or secret data and use JSON local logs.

**Expected outcome:** The gateway safely accepts multiple authenticated sessions
and complete endpointed turns.

**Verification:**

```sh
go test -race ./internal/gateway/devices/... ./internal/gateway/turns/... \
  ./cmd/gateway/...
go test -race -count=10 ./internal/gateway/devices/... ./internal/gateway/turns/...
make build
make fmt-check
make lint
make test
```

Expected: unauthorized, malformed, duplicate-session, reload, framing,
default-discard, and opt-in-WAV tests pass.

### Task 8: Complete dotsim and prove the host-run vertical slice

**Status:** completed 2026-08-31 (local verification; manual host mDNS check deferred by user)

**Purpose:** Establish complete protocol behavior before integrating echod.

**Dependencies:** Tasks 3–7.

**Hardware required:** no; real host multicast is a manual network check.

**Files or components:**

- Modify/test: `cmd/dotsim`
- Add: gateway/dotsim runner integration tests

**Concrete changes:**

- Wire the shared client, endpointing, fixture audio, trigger metadata, config
  persistence, JSON logs, and mDNS browser.
- Add token-file, TLS-skip, preferred-server-ID, state-directory,
  discovery-timeout, and `--once` options.
- Stay connected and reconnect by default. Trigger the configured fixture once
  per process after the first handshake and never replay it after reconnect.
- Apply pushed wake settings to simulated active wake metadata while retaining
  CLI-provided diagnostic scores.
- Stop on endpointing; use EOF only if the fixture ends first.
- Return a clear unsupported error if gateway playback arrives.
- Test explicit WSS, config versions, SIGHUP, equal-version conflict,
  unauthorized token, forced reconnect, log pressure during audio, and WAV
  diagnostic output.
- Run a manual host gateway+dotsim test over real mDNS.

**Expected outcome:** Host dotsim discovers the gateway, registers, applies
config, sends one endpointed turn, survives restart, and stays connected.

**Verification:**

```sh
go test -race ./cmd/dotsim/... ./cmd/gateway/... ./internal/device/client/... \
  ./internal/gateway/...
go test -race -count=10 ./cmd/dotsim/... ./internal/device/client/...
make build

.bin/gateway --listen :8770 --server-id m2-local \
  --hostname echo-gateway.local. \
  --tls-cert /tmp/echo-satellite-m2/dev-cert.pem \
  --tls-key /tmp/echo-satellite-m2/dev-key.pem \
  --device-token-file /tmp/echo-satellite-m2/device-token \
  --device-config /tmp/echo-satellite-m2/devices.toml

.bin/dotsim --device-id dotsim-m2 --discover mdns \
  --gateway-token-file /tmp/echo-satellite-m2/device-token \
  --tls-skip-verify \
  --mic testdata/audio/turn_speech_trailing_silence.wav --once
```

Expected: dotsim resolves `m2-local`, warns about skipped verification, receives
the config version, sends `turn.start`, PCM, and
`audio.stop(reason="endpointed")`, then exits successfully under `--once`.

### Task 9: Docker gateway packaging and explicit-URL smoke test

**Status:** completed 2026-08-31

**Purpose:** Verify the target deployment shape without making a false Docker
multicast claim.

**Dependencies:** Tasks 7 and 8.

**Hardware required:** no; Docker Engine or Docker Desktop is required.

**Files or components:**

- Create: gateway `Dockerfile`, `.dockerignore`, `deploy/docker-compose.yml`
- Create: non-secret example TOML and deployment documentation

**Concrete changes:**

- Build a static gateway in a multi-stage image and run it as non-root.
- Mount certificate, key, token, TOML, and optional WAV directory with minimum
  required access.
- Publish port 8770 and disable mDNS in the Compose profile.
- Do not commit generated certificates, tokens, or other secrets.
- Document host dotsim connecting to `wss://localhost:8770/device`.
- Ensure Compose stop triggers graceful gateway cancellation.

**Expected outcome:** The gateway runs in Compose and accepts host dotsim over an
explicit authenticated WSS URL.

**Verification:**

```sh
docker compose -f deploy/docker-compose.yml config
docker compose -f deploy/docker-compose.yml build gateway
docker compose -f deploy/docker-compose.yml up -d gateway

.bin/dotsim --device-id dotsim-docker --discover disabled \
  --gateway-url wss://localhost:8770/device \
  --gateway-token-file /tmp/echo-satellite-m2/device-token \
  --tls-skip-verify \
  --mic testdata/audio/command_endpointing_16k_mono.wav --once

docker compose -f deploy/docker-compose.yml down
make fmt-check
make lint
make test
```

Expected: Compose validates/builds and dotsim completes one endpointed turn.

### Task 10: Integrate discovery, config, endpointing, and turns into echod

**Status:** completed 2026-08-31

**Purpose:** Reuse the simulator-proven path on the real Milestone 1 runtime.

**Dependencies:** Task 8 must complete first. Task 9 may execute independently
after Task 8 because its files do not overlap this task.

**Hardware required:** no for fixture and stub tests; real proof is Task 11.

**Files or components:**

- Modify/test: `cmd/echod`
- Modify/test as required: wake runtime orchestration
- Reuse: `internal/device/client`, `config`, and `endpointing`

**Concrete changes:**

- Preserve `--wake-only` as a local diagnostic that opens no socket.
- Normal mode loads persisted gateway/config state, initializes the current
  model, and starts the shared client.
- Add token-file, TLS-skip, pairing-state, config-state, and discovery-timeout
  options while retaining the required local CLI precedence for version 0.
- Open ALSA/FileSource once and add the turn consumer through `audio.Fanout`.
- Warm the separate endpoint detector continuously.
- On accepted wake, render immediate local feedback, activate one turn with
  `wake.Event.PreRoll`, and reject nested triggers.
- On Action tap, start a button-triggered turn without wake diagnostics.
- Guarantee no sample gap between the event position/pre-roll and live frames;
  cover asynchronous fanout handoff using `audio.Frame.Offset` tests.
- Stream only during an active turn and return the LED to idle after stop or
  disconnect.
- While busy, stage valid higher config. At idle, prepare the installed model,
  persist the revision, atomically swap settings, close replaced resources, and
  report `applied`.
- Reject missing/incompatible models without changing the running or persisted
  last-known-good model.
- Keep local capture/wake running during gateway outages but never queue offline
  audio for later upload.
- Test wake and button turns, reconnect, config swap, load failure, endpoint
  stop, capture overrun, shutdown, and absence of idle binary writes.

**Expected outcome:** echod speaks the same protocol as dotsim while preserving
single-capture and local-wake boundaries.

**Verification:**

```sh
go test -race ./cmd/echod/... ./internal/device/...
go test -race -count=10 ./cmd/echod/... ./internal/device/client/... \
  ./internal/device/endpointing/...
make check-portability
make build-device
make fmt-check
make lint
make test
```

Expected: fixture/stub paths pass, ARM64 builds remain static, and tests prove
idle audio never reaches transport.

### Task 11: HARDWARE — mDNS, WSS turns, and endpointing on the Echo Dot

**Status:** completed 2026-09-02

**Scope amendment:** The 60-second hard-timeout and audio-quality experiments
are no longer Milestone 2 acceptance. The retained 41.840-second false endpoint
established that channel-0/bypass audio needs a separately qualified
preprocessor; Milestone 3 now owns seven-channel characterization, gain and
leveling, beamforming, and renewed quiet-speech/timeout qualification. Task 11
therefore records the completed Milestone 2 transport proof only.

Forwarded-log pressure and rejected corrupt/missing-model configuration are not
hardware acceptance criteria: Task 8 owns the former's host integration test,
and Tasks 3 and 10 own the latter's deterministic configuration and runtime
tests. This amendment does not defer either behavior or weaken its test
coverage; it only keeps the live-Dot scope to behavior that requires a Dot.

**Purpose:** Prove simulator behavior on target hardware and record the real
endpointing experiment required by the architecture change.

**Dependencies:** Tasks 2–10.

**Hardware required:** **yes** — rooted Echo Dot Gen 2 with the qualified
Milestone 1 model and hardware path, on the same multicast domain as the host.

**Files or components:**

- Update: `docs/DESIGN.md` §26 results
- Update: device/network diagnostic documentation
- Production-code expansion after a failed assumption requires a plan amendment.

**Concrete changes:**

- Run the gateway directly on the host with mDNS, WSS, shared token, versioned
  TOML, and diagnostic WAV output.
- Stage the token and binary on the Dot; set token permissions to 0600.
- Verify discovery without an explicit URL, authenticated handshake, persisted
  server preference, and reconnect after gateway restart/address change.
- Trigger repeated wake and Action-button turns.
- Verify diagnostics, pre-roll continuity, active-only audio,
  `audio.stop(reason="endpointed")`, and intelligible WAV output.
- Exercise pauses below and above 1.5 seconds and no speech.
- Record endpoint behavior, capture drops, and measured tuning in
  `docs/DESIGN.md`. Continuous quiet-speech, background-noise, and 60-second
  qualification move to Milestone 3's audio-conditioning scope.
- Record a transport or framing failure honestly; preprocessing quality is not
  implied by this task's completion.

**Expected outcome:** The real Dot discovers, authenticates, streams continuous
turn PCM only after a local trigger, and stops locally on command silence.

**Verification:**

```sh
ADB=/mnt/c/tools/android-platform-tools/adb.exe \
DEVICE_SERIAL=G090LF0964060EHP make device-check

ADB=/mnt/c/tools/android-platform-tools/adb.exe \
DEVICE_SERIAL=G090LF0964060EHP make push-device
```

Then run the documented host gateway command and rooted foreground `echod`
command from `docs/development-windows-wsl.md`.

Expected: mDNS resolves without `--gateway-url`; WSS/auth/handshake succeed;
wake and Action each create one turn; no binary audio exists outside the audio
window; sub-1.5-second pauses remain inside a turn; longer silence endpoints it;
WAV audio is continuous; gateway restart reconnects with jitter; and measured
transport results are recorded. Audio-conditioning and the 60-second hard cap
are Milestone 3 acceptance.

#### Task 11 remediation: Windows diagnostic WAV finalization and observability

**Status:** completed 2026-09-02

**Purpose:** Repair the Windows-hosted gateway failure discovered during the
real Dot Action-turn test, and ensure future turn-finalization errors are
visible in gateway logs rather than only as a generic protocol close reason.

**Dependencies:** Task 11 live WSS evidence.

**Files or components:**

- Modify/test: `internal/gateway/turns`
- Modify/test: `internal/gateway/devices`

**Concrete changes:**

- Retain file `Sync`, hard-link promotion, and staged-file removal for
  diagnostic WAVs; skip only directory `Sync` on Windows, where directory
  handles do not support the Unix durability operation.
- When a turn receiver rejects/fails an `audio.stop`, log the device ID, turn
  ID, and wrapped underlying error before closing the session.
- Add focused regression tests for accepted diagnostic finalization and visible
  receiver-stop failure logging; cross-compile for Windows and rerun the real
  Windows-gateway/Dot Action-turn test.

**Verification:**

```sh
go test -race ./internal/gateway/turns/... ./internal/gateway/devices/...
GOOS=windows GOARCH=amd64 go test -c -o /tmp/turns-windows.test.exe ./internal/gateway/turns
GOOS=windows GOARCH=amd64 go test -c -o /tmp/devices-windows.test.exe ./internal/gateway/devices
```

Expected: a Windows gateway completes an endpointed Action turn with diagnostic
WAV output, and any remaining finalization failure is present in gateway logs.

#### Task 11 remediation: endpoint-VAD threshold calibration

**Status:** completed 2026-09-01

**Purpose:** Correct the real-Dot false endpoint observed during continuous
speech without weakening the independently configured 1.5-second trailing
silence rule.

**Dependencies:** The completed explicit-WSS Action/wake acceptance above and
the opt-in diagnostic WAV evidence.

**Hardware required:** yes — the qualified Dot, its microphone cut disabled
(GPIO 444 low), and the Windows-hosted diagnostic gateway.

**Files or components:**

- Modify: ignored `.m2/devices.toml` development gateway profile only.
- Observe: Dot foreground `echod`, Windows gateway logs, and opt-in `.m2/wav`.

**Concrete changes:**

- Raise the monotonic development profile revision from 1 to 2 and lower only
  `defaults.endpointing.speech_threshold` from 0.50 to 0.20. Do not change
  `speech_onset_ms`, `trailing_silence_ms`, `no_speech_timeout_ms`, or
  `max_turn_ms`.
- Restart the Windows development gateway so it loads the complete revision;
  retain the existing explicit development WSS URL and TLS bypass.
- Verify the Dot applies configuration version 2, then run a continuous
  8–10-second count followed by a deliberate pause longer than 1.5 seconds.
- Accept this calibration only if the count is retained to its natural end and
  the deliberate final pause still produces `audio.stop(reason="endpointed")`.
  A false cut, excessive noise-held turn, or lost config leaves this remediation
  in progress and requires a separately designed hysteresis/telemetry change.

**Expected outcome:** Quiet continuous speech is no longer treated as a full
trailing-silence interval, while the configured 1.5-second endpoint delay and
device-local endpointing boundary remain intact.

**Observed outcome:** Version 2 / 0.20 was a useful lower-bound experiment but
still falsely cut continuous speech; the completed iteration-2 version 3 / 0.05
profile achieved this outcome without changing a timing value.

**Verification:**

Use the Windows gateway command from Task 11 after changing the ignored test
profile, then run the bounded Dot foreground diagnostic. Confirm the gateway's
`device connected` record reports `config_version:2`, the count WAV reaches the
user's final spoken count, and the gateway reports `reason:"endpointed"` only
after the intentional final pause. Inspect the WAV and record duration,
continuity, endpoint observation, and any ALSA drops in the progress log.

##### Calibration iteration 2: lower threshold bracket

**Status:** completed 2026-09-01

**Purpose:** Test the lower bound indicated by the failed 0.20 calibration
without changing endpoint timing or protocol semantics.

**Concrete changes:** Raise the ignored development profile to version 3 and
lower only `speech_threshold` to 0.05. Restart the Windows gateway, confirm the
Dot persists version 3, then repeat the continuous count and deliberate final
pause. A mid-speech cut or noise-held final pause stops configuration tuning and
requires separately designed hysteresis plus endpoint-score telemetry.

**Verification:** The count's WAV retains all spoken counting until the user
stops; a following quiet period longer than 1.5 seconds produces
`audio.stop(reason="endpointed")` with no ALSA-drop reason.

#### Task 11 remediation: Windows mDNS publication and browsing

**Status:** completed 2026-09-02

**Purpose:** Restore the required native Windows mDNS discovery path after a
live gateway advertisement was visible to mDNS-Browser but neither native
Windows `dotsim` nor the Dot's client observed any compatible instance.

**Dependencies:** Task 4 discovery contracts and the Task 11 live Windows
gateway diagnostic.

**Hardware required:** no for implementation and native-Windows simulation;
yes — the qualified Dot on the same WLAN for final Task 11 acceptance.

**Files or components:**

- Modify/test: `internal/discovery/mdns`
- Modify/test: `cmd/gateway` only if configuration normalization belongs at
  the composition root.
- Update: this plan's Task 11 evidence.

**Concrete changes:**

- Preserve the `discovery.Advertiser`/`Browser` boundary and the documented
  `_echo-satellite._tcp.local.` record/TXT schema.
- Correct the proxy hostname passed to the DNS-SD implementation so an input
  ending in `.local.` does not become `*.local.local.`.
- Replace or repair the mDNS implementation's Windows UDP-5353
  publication/browse behavior so a native Windows `dotsim` discovers the
  native Windows gateway without an explicit URL.
- Add deterministic regression coverage for hostname normalization and the
  adapter's publication/browse contracts. Do not expose tokens or add a
  gateway URL fallback to this acceptance path.

**Expected outcome:** mDNS-Browser shows a single-local-domain host record,
and a clean-state native Windows `dotsim --discover mdns --once` completes an
authenticated fixture turn. The Dot may then repeat the same clean-state test.

**Observed outcome:** Native Windows acceptance passed on 2026-09-01. The
gateway advertised `echo-gateway.local.` (not `*.local.local.`), and a
clean-state `dotsim` discovered it without `--gateway-url`, connected with
config version 3, and completed `dotsim-turn-1` with
`audio.stop(reason="endpointed")` and 118,400 PCM bytes. The Dot rerun remains
required for Task 11 completion.

**Dot outcome:** blocked on 2026-09-01. The rebuilt foreground `echod` ran
without `--gateway-url` after its custom pairing was removed, but FireOS denied
every UDP multicast send before discovery with `sendto: operation not permitted`
for `224.0.0.251:5353`, including the root `su -c` process. It therefore made
no browse, handshake, or new pairing. The prior pairing was restored mode 0600
and the bounded foreground agent stopped. This is a device multicast-policy or
capability investigation, not a Windows advertisement regression.

**Verification:**

```sh
go test -race ./internal/discovery/... ./cmd/gateway/...
GOOS=windows GOARCH=amd64 go test ./internal/discovery/... ./cmd/gateway/...
make fmt-check
make lint
```

On Windows, start the native gateway with no `--no-mdns`, use mDNS-Browser to
confirm `_echo-satellite._tcp.local.` resolves to `echo-gateway.local.:8770`,
then run a clean-state `dotsim --discover mdns --once` with no
`--gateway-url`. The hardware follow-up removes the Dot pairing state and
repeats that command path with `echod`.

#### Task 11 remediation: FireOS mDNS interface selection

**Status:** completed 2026-09-02

**Purpose:** Correct the FireOS-specific `p2p0` multicast-send failure without
changing the protocol or reducing host/platform discovery support.

**Dependencies:** Task 11 native Windows mDNS acceptance.

**Hardware required:** yes — the qualified Dot and Windows-hosted gateway on
the same WLAN.

**Files or components:**

- Modify/test: `internal/discovery/mdns`
- Modify: `go.mod`, `go.sum`
- Update: this plan's Task 11 evidence.

**Concrete changes:**

- Use the pinned `betamos/zeroconf` adapter whose Windows transmitter sets the
  multicast interface socket option; the earlier libraries either rejected
  valid packets or could not select a Windows transmitter interface.
- Confine only `echod` browsing to an up, multicast-capable `wlan0`; Windows,
  simulator, gateway, and other host callers retain zeroconf's normal
  all-interface selection.
- Retain the documented operational `p2p0`-down preparation, but do not add a
  protocol fallback or expose new metadata.

##### Advertisement-address remediation

**Status:** completed 2026-09-02

**Purpose:** Supply the DNS-SD A/AAAA data required for a satellite to turn a
visible service PTR/SRV/TXT record into a connectable endpoint.

**Concrete changes:** Enumerate every up, multicast-capable, non-loopback
gateway interface with usable assigned addresses and run one zeroconf publisher
per interface. Each publisher emits only that interface's A/AAAA records;
configured addresses restrict publication to interfaces that own them. Fail
registration if validation produces no interface/address pair and close prior
publishers if a later interface fails. On browse, consume blocking resolver
events concurrently, honor removal events, and prefer an address sharing the
browse interface's subnet when an older or third-party response contains
addresses from several interfaces. Do not place addresses in TXT or add
secrets.

**Verification:** A native Windows gateway record in mDNS-Browser is associated
with its LAN adapter (not Loopback Pseudo-Interface 1) and includes its LAN
IPv4 address. A separate-LAN-host `avahi-browse -rt _echo-satellite._tcp`
shows that endpoint and discovery TXT. A fresh-state Dot mDNS-only run then
receives a compatible record, performs `hello`/`welcome`, and persists its
pairing.

**Expected outcome:** A Dot browse does not transmit on FireOS `p2p0`; a
fresh-state mDNS-only run reaches the existing Windows gateway and persists an
authenticated pairing.

**Verification:**

```sh
go test -race ./internal/discovery/...
make lint
make build-device
```

With a live gateway, bring `p2p0` up to exercise code-level selection, remove
only the custom diagnostic pairing state, and run the foreground `echod`
without `--gateway-url`. Confirm `welcome` and newly persisted pairing, then
restore the original diagnostic state and binary. If ALSA is busy, do not stop
the holder; record the holder/condition and leave this remediation in progress.

### Task 12: Cross-cutting verification, documentation, and fresh review

**Status:** completed 2026-09-07

**Purpose:** Establish that Milestone 2 is complete, portable, reviewed, and
accurately documented.

**Dependencies:** Tasks 2–11.

**Hardware required:** yes — Task 11 evidence is part of completion.

**Files or components:**

- Update: `README.md`, development/device diagnostics, and this plan
- Review: full diff against this plan, `docs/DESIGN.md`, and `docs/protocol.md`

**Concrete changes:**

- Document host mDNS, explicit fallback, cert/token setup, TLS-skip warning,
  TOML version increment/SIGHUP, dotsim lifecycle, Docker smoke test, state
  paths, and diagnostic-WAV privacy.
- Read per-function coverage for touched packages. Test every exported function,
  error path, config-version rule, endpoint state, and protocol boundary.
- Dispatch a fresh review agent with the diff, plan, design, and protocol docs.
- Triage every finding as fix, decline, or postpone. Correctness/security
  findings are not silently postponed.
- Re-run all checks after fixes and record exact evidence below.
- Move the plan to `finished/` only when every acceptance item passes.

**Expected outcome:** The diff is clean, reviewed, reproducible, and supported by
host, Docker, simulator, and hardware evidence.

**Verification:**

```sh
git diff --check
make fmt-check
make lint
make test
make build
make check-portability
make build-device
make build-device-ctl
docker compose -f deploy/docker-compose.yml config
docker compose -f deploy/docker-compose.yml build gateway
```

Expected: zero issues; review has no untriaged findings; all required host,
Docker, dotsim, and real-device evidence is recorded.

## Cross-task risks

- **Local endpointing cuts commands or waits too long.** Impact: lost speech or
  excess latency/bandwidth. Mitigation: separate config, pause fixtures, a
  60-second cap, and mandatory real-room measurements.
- **Remote wake reconfiguration strands the runtime.** Impact: a device loses
  wake capability. Mitigation: prepare candidate model first, apply only while
  idle, persist atomically, retain the old runtime until commit, and reject the
  whole revision on failure.
- **TLS verification bypass permits MITM despite encryption.** Impact: a LAN
  attacker could observe command audio or impersonate the gateway. Mitigation:
  off by default, WSS mandatory, persistent warning, redacted token, explicit
  development scope, and production auth retained as follow-up.
- **A shared token cannot identify devices.** Impact: `device_id` is registration
  metadata, not authentication. Mitigation: document this limitation and never
  extend this credential to update/deployment authorization.
- **mDNS fails across WSL, VLANs, VPNs, firewalls, or Docker bridges.** Impact:
  discovery fails. Mitigation: canonical host-run acceptance, explicit URL
  fallback, bounded browse, and no Docker multicast claim.
- **Logs interfere with audio.** Impact: gaps corrupt commands. Mitigation:
  separate bounded queue, audio/control priority, drop accounting, and stress
  tests.
- **Fanout scheduling creates a wake-to-live-audio gap.** Impact: clipped command
  start. Mitigation: offset-aware handoff tests and continuity checks against
  fixture and real WAV output.
- **Execution begins from stale or dirty state.** Impact: missing Milestone 1
  contracts or overwritten user work. Mitigation: refresh/inspect the base and
  use a dedicated clean branch/worktree.

## Rollback or recovery

- All work is additive on a dedicated Milestone 2 branch. Returning to merged
  Milestone 1 restores the previous system.
- `echod --wake-only` remains the network-independent hardware recovery mode.
- An explicit WSS URL bypasses mDNS without disabling authentication.
- Failed TOML reload retains the prior gateway snapshot.
- Failed/interrupted device config retains the prior persisted file and running
  wake engine.
- Corrupt device state falls back to version-0 local settings and is reported.
- Disabling the diagnostic WAV directory restores default audio disposal.
- A failed hardware or multicast item leaves the plan in `in-progress/` with an
  honest blocker; partial work is not filed as finished.

## Final acceptance criteria

- [x] Governing docs consistently assign wake and command endpointing to
  separate device-local components.
- [x] No gateway wake/VAD mode or idle microphone-audio path exists.
- [x] Gateway advertises `_echo-satellite._tcp.local.` and host dotsim/echod
  resolve it.
- [x] Explicit URL, persisted gateway, preferred server ID, and mDNS precedence
  are tested.
- [x] Unauthorized upgrades fail; authenticated WSS completes hello/welcome.
- [x] TLS verification defaults on; bypass is visibly development-only.
- [x] Config versions are monotonic, persisted, atomic, acknowledged, and
  conflict-tested.
- [x] Pushed wake settings select only an installed valid model and cannot break
  the current model on rejection.
- [x] Logs are JSON locally, bounded/redacted over WSS, and lower priority than
  audio.
- [x] dotsim discovers/connects, configures, streams one endpointed turn, stays
  connected, and reconnects.
- [x] Gateway enforces framing and stores audio only under explicit diagnostics.
- [x] Compose gateway accepts host dotsim through explicit WSS.
- [x] Real echod wake and Action turns stream continuous canonical PCM and stop
  locally on silence.
- [x] Real hardware measurements and tuning are recorded in `docs/DESIGN.md`.
- [x] Format, lint, race tests, builds, portability, Docker build, and fresh
  review pass.
- [x] Every review finding has an explicit disposition.

## Progress log

- 2026-08-31: Started Task 11 preparation at user direction. The native Linux
  ADB client and existing ignored `.m2` development credentials are the test
  inputs. Preparation may build and stage the ephemeral `/data/local/tmp`
  binary and protected token, but does not start the gateway or `echod`, stop
  the Amazon LED service, change the microphone GPIO, or claim hardware
  acceptance before the user authorizes the live diagnostic.

- 2026-08-31: Preparation completed without a live diagnostic: native Linux
  `adb` saw the qualified rooted `biscuit` / arm64-v8a / permissive device;
  the static `echod` was staged at `/data/local/tmp/echod` and passed
  `--version`; the development token was staged mode 0600 at
  `/data/local/tmp/echo-satellite-state/device-token`; and the installed
  `okay_nabu` model plus OpenWakeWord shared assets were confirmed. The
  pre-existing `/data/local/etc/echo-satellite` rejects writes even through
  Magisk `su`, so the eventual foreground test must explicitly use
  `/data/local/tmp/echo-satellite-state/{paired-gateway,config}.json` rather
  than the production-default state paths. No gateway or device agent process
  was started, and the LED/microphone controls were left untouched.

- 2026-09-01: Began the authorized live Task 11 diagnostic. Native Linux ADB
  connected and the host gateway started with mDNS/WSS plus diagnostic WAVs.
  `echod` ran without an explicit URL for a bounded discovery window but never
  created pairing/config state, so no authenticated session occurred. Explicit
  fallback diagnosis then found the Dot has no network route to either the
  host LAN address or WSL address (`Network is unreachable`). The test gateway
  and foreground agent were stopped. The LED boot animation was already `0`,
  but this FireOS session denied the documented sysfs LED/GPIO writes and did
  not expose GPIO 444 after export; consequently wake/microphone results were
  not attempted or claimed. Task 11 remains in progress and blocked on joining
  the Dot to the test LAN, then resolving the microphone-cut control path.

- 2026-09-01: Corrected the microphone-cut preparation diagnosis. The initial
  GPIO-444 writes were accidentally parsed outside the remote `su -c` shell;
  the documented fully quoted root command exported GPIO 444, set it to output,
  drove it low, and read back `0`. The physical microphone cut is therefore
  cleared. The only remaining Task 11 blocker is the Dot's missing network
  route.

- 2026-09-01: Revalidated the local hardware path without network transport.
  With GPIO 444 read back low, `echod --wake-only` accepted two deliberate
  `okay nabu` utterances at the qualified 0.50 threshold (scores 0.7987 and
  0.6921), with zero frame drops/XRuns; sampled RSS was 20,201,472 then
  21,721,088 bytes and CPU 48.40% then 49.20%. The process was stopped cleanly.
  This confirms local microphone/wake behavior only; it does not satisfy the
  blocked mDNS, WSS, Action-turn, endpointing, or WAV acceptance items.

- 2026-09-01: The Dot joined WLAN (`192.168.110.216/24`, default route
  `192.168.110.1`), but neither mDNS nor the documented explicit-WSS fallback
  reached an authenticated session. A healthy gateway bound in WSL answered
  local `/healthz`, while `echod` pointed explicitly at the Windows LAN host
  (`wss://192.168.110.127:8770/device`, development TLS bypass) produced no
  welcome, pairing state, or config state. This isolates the remaining blocker
  to WSL/Windows inbound-LAN forwarding or firewall configuration, not Dot
  Wi-Fi association, credentials, or the local wake path. The temporary
  gateway and device processes were stopped.

- 2026-09-01: Retested against a gateway run directly on the Windows LAN host.
  Explicit WSS completed successfully: the Dot persisted authenticated pairing
  and gateway config state. An Action tap was observed locally, but after the
  active audio window the gateway closed the connection with
  `StatusProtocolError: invalid audio stop`. The protocol framing itself is
  valid in gateway tests; code inspection shows that `turns.Receiver.Stop`
  finalizes/promotes the configured diagnostic WAV then syncs its directory,
  and any filesystem error is collapsed by the session handler to that close
  reason. Windows directory sync is the likely unsupported operation. This is
  a hardware-discovered cross-platform diagnostic-output defect, not a WSS,
  Wi-Fi, credential, or local Action-button failure. The Dot test process was
  stopped; the user-owned Windows gateway was left running.

- 2026-09-01: Implemented the Task 11 Windows diagnostic-WAV remediation.
  WAV file flush and hard-link promotion remain unchanged; only the unsupported
  directory sync is skipped on Windows. Gateway sessions now warn with device
  ID, turn ID, and the receiver's underlying error before issuing a generic
  `invalid audio stop` close. Fresh review found the initial Windows branch
  lacked runtime coverage; fixed by making the OS choice directly testable and
  adding the Windows-skip regression. `go test -race
  ./internal/gateway/turns/... ./internal/gateway/devices/...`, Windows test
  binary compilation for both packages, `make fmt-check`, `make lint` (0
  issues), and `git diff --check` passed. The rebuilt `.bin/gateway.exe` awaits
  the Windows-hosted real-Dot Action-turn retry.

- 2026-09-01: Retried against the rebuilt Windows gateway at
  `wss://192.168.110.127:8770/device` with the staged token and explicit
  development TLS bypass. The Dot retained its authenticated pairing and
  version-1 configuration; the fully quoted hardware preparation command again
  read GPIO 444 as `0`. Action taps completed without a protocol close or a
  gateway-session error on the Dot. The Windows diagnostic directory published
  three new finalized 16 kHz mono PCM WAVs (140,844, 105,004, and 110,124
  bytes); the first contains 4.400 seconds of samples and followed the spoken
  Action test. This proves the Windows WAV-finalization remediation in the real
  WSS/Dot path. One ALSA XRun occurred at agent startup before the Action test,
  so this run does not claim a zero-drop result. Gateway terminal output must
  still be retained for the exact reported stop reasons; the remaining Task 11
  tests are unchanged.

- 2026-09-01: In the same sustained explicit-WSS session, a local
  `okay nabu` wake was accepted (wake score 0.7892) and the following command
  finalized as a new 134,444-byte diagnostic WAV. `ffprobe` confirmed 4.200
  seconds of 16 kHz mono PCM. Comparing its completion time with the accepted
  wake places the first audio about 0.74 seconds before wake acceptance, which
  is consistent with the configured 600 ms pre-roll plus framing cadence; no
  session close or transport error occurred. Windows gateway evidence confirms
  the first Action turn and two further Action turns all ended `endpointed`
  with 140,800, 104,960, and 110,080 PCM bytes respectively, and the
  wake-triggered turn ended `endpointed` with 134,400 PCM bytes. Each value
  exactly matches its corresponding WAV data chunk. This completes the real
  Windows-gateway Action and wake endpointing/WAV-finalization acceptance;
  formal continuity inspection and the other Task 11 scenarios remain.

- 2026-09-01: The continuous-count test demonstrated a real false endpoint:
  the version-1, 0.50-threshold WAV stopped at 4.160 seconds while it still
  contained voiced counting. Version 2 with only `speech_threshold` reduced to
  0.20 was persisted by the Dot and extended the same test to 5.680 seconds,
  but still cut during counting. The approved version-3 lower-bracket profile
  (`speech_threshold = 0.05`; all endpoint timings unchanged) was persisted and
  passed: a continuous count followed by deliberate silence produced a
  9.120-second 16 kHz mono WAV (291,840 PCM bytes) whose final quiet region was
  1.563 seconds. The Windows gateway logged config version 3 and
  `audio.stop(reason="endpointed")` for the matching Action turn. This is a
  configuration calibration for the qualified room/Dot, not a change to the
  endpointing timer or protocol; remaining Task 11 scenarios still need
  validation.

- 2026-09-01: Pause-boundary acceptance passed in the existing authenticated
  explicit-WSS session using the version-3 endpoint profile (speech threshold
  0.05; 1,500-ms trailing silence). After an Action trigger, the operator spoke,
  paused for approximately one second, continued, then remained silent. The
  gateway published one new diagnostic WAV,
  `turn-turn-1788284834245921907-e9f4a5250f352dc4.wav`: 297,004 bytes total,
  9.280 seconds, 16-kHz mono PCM. Its 296,960-byte data chunk exactly equals
  9.280 seconds at 32,000 bytes/s, so no discontinuity or second turn was
  introduced at the pause. `ffmpeg` silence analysis at -45 dB found the
  intended internal quiet interval at 3.059250--4.145440 seconds (1.086190 s),
  below the configured endpoint duration; the final quiet tail was 1.700 s.
  The retained single WAV establishes that the short pause stayed in one turn;
  the other Task 11 hardware scenarios remain outstanding.

- 2026-09-01: No-speech timing acceptance passed in the same authenticated
  explicit-WSS session. An Action trigger followed by more than five seconds
  without intentional speech finalized one new diagnostic WAV,
  `turn-turn-1788284948505824760-7dd1f45a1221188b.wav`: 97,324 bytes total,
  with a 97,280-byte data chunk (3.040 seconds of 16-kHz mono PCM). `ffmpeg`
  measured -70.1 dB mean level, consistent with silence apart from incidental
  environmental/transient samples. This is within one 40-ms capture frame of
  the configured 3,000-ms no-speech limit. The retained Windows gateway record
  for the matching turn confirms
  `audio.stop(reason="no_speech", pcm_bytes=97280)`, completing the required
  protocol-level acceptance rather than inferring the stop reason solely from
  duration and audio content.

- 2026-09-01: Hard-timeout acceptance failed. After an Action trigger, the
  operator counted continuously for more than 65 seconds with the version-3
  (0.05-threshold) profile, but the gateway ended
  `turn-1788285069810714536` with `reason="endpointed"` and 1,338,880 PCM
  bytes, i.e. 41.840 seconds, rather than the required 60-second
  `reason="timeout"`. The matching 1,338,924-byte WAV is 41.840 seconds of
  16-kHz mono PCM. At -45 dB, its final six detected quiet intervals are only
  0.238--0.466 seconds each; none independently explains the configured
  1,500-ms endpoint, so this remains a quiet/continuous-speech false cut under
  the deployed detector/profile rather than accepted evidence of a real long
  pause. No endpoint configuration or code was changed: per the completed
  calibration's stop condition, any remediation requires separately designed
  hysteresis and endpoint-score telemetry before rerunning the hard-cap test.

- 2026-09-02: Investigation of the retained hard-timeout WAV found no defect
  in hard-limit accounting or stop-reason precedence. `Controller.Observe`
  accumulates canonical 16-kHz sample duration and checks the 60,000-ms cap
  before the endpointing branch on every frame; it can therefore report
  `endpointed` at 41.840 seconds only after the detector has classified a
  trailing-silence interval. Replaying the WAV's 1,280-sample frames through
  the same `vadlevel.Detector` at the deployed 0.05 threshold found a
  below-threshold run from 40.320 to 41.840 seconds (1.520 seconds). Its final
  physical quiet interval is only 0.466 seconds at -45 dB, so quiet speech was
  classified as non-speech. The replay begins with a fresh detector and is not
  a substitute for live-state telemetry, but it corroborates the observed
  classification failure. No code/configuration change was made; add the
  planned endpoint-score telemetry and hysteresis design amendment before a
  new 60-second acceptance attempt.

- 2026-09-02: Scope decision: the user moved command-audio conditioning into
  Milestone 3 and explicitly removed the hard-timeout hardware experiment from
  Task 11 acceptance. Milestone 3 now owns all-channel capture
  characterization, bounded gain/leveling, delay-and-sum beamforming, and
  renewed quiet-speech, deliberate-silence, and 60-second endpointing
  qualification. The completed Task 11 records the Milestone 2 mDNS/WSS,
  pairing, reconnect, wake/Action-turn, framing, and diagnostic-WAV evidence;
  it does not make an audio-quality claim beyond the tested baseline.

- 2026-09-01: mDNS-only acceptance remains blocked. The known foreground
  explicit-URL `echod` test was stopped, and its custom paired-state file at
  `/data/local/tmp/echo-satellite-state/paired-gateway.json` was backed up and
  removed. A fresh foreground run omitted `--gateway-url`, used
  `--discovery mdns`, and retained the staged development token and TLS bypass.
  Between 17:56:34Z and 17:56:52Z it made five bounded three-second browse
  attempts; every attempt reported `browsed 0 instance(s), none compatible
  with protocol 1`. Consequently no WSS/auth handshake or new pairing occurred.
  The prior pairing (`echo-gateway` at `192.168.110.127:8770`) was restored
  with mode 0600 after the bounded run, and no `echod` process remains. This is
  evidence of multicast-advertisement visibility failure, not successful mDNS
  discovery; retain Task 11 in progress until the Windows-host gateway is
  demonstrably advertising on the Dot's WLAN multicast domain.

- 2026-09-01: Follow-up diagnosis established that the first fresh-state
  browse preceded the then-current Windows `gateway.exe` start by nearly four
  minutes, so it could not test a live advertisement. Repeating it after that
  gateway was confirmed running still produced five `browsed 0 instance(s)`
  results and no handshake or new pairing. A separately built native Windows
  `dotsim.exe`, run on the gateway host with the same mDNS mode, token, and TLS
  bypass, reproduced the same five zero-instance browses. This rules out the
  Dot WLAN/VLAN as the immediate cause: the Windows gateway is not discoverable
  even to a native local client. `Get-NetUDPEndpoint` showed the gateway TCP
  listener on 8770 but no UDP-5353 endpoint owned by `gateway.exe`; port 5353
  was instead held by Windows/ChatGPT processes. An attempted `pktmon` capture
  was denied without elevation. The gateway invocation is correct; investigate
  the current `grandcat/zeroconf` Windows registration/bind behavior and its
  interaction with existing UDP-5353 listeners before rerunning Task 11. The
  Dot's original custom pairing was restored mode 0600 and all test clients
  were stopped.

- 2026-09-01: Gateway-restart reconnect acceptance passed. The running `echod`
  process was deliberately left in place while the Windows gateway was stopped
  and restarted with the unchanged version-3 profile. The gateway began at
  20:54:26.463+03:00 and logged the returning authenticated device session at
  20:54:32.094+03:00 (5.631 seconds later), with the expected device ID,
  `echo-gateway` server ID, version 3, and local-endpointing capability. A
  post-reconnect Action turn then started at 20:54:56.766+03:00 and ended
  successfully at 20:54:56.794+03:00 with `reason="endpointed"` and 184,320
  PCM bytes. The corresponding finalized WAV,
  `turn-turn-1788285291213754934-c11364bedde319d6.wav`, is 5.760 seconds of
  16-kHz mono PCM (184,364 bytes including its 44-byte header). This proves
  automatic reconnect and a subsequent successful turn without restarting the
  device agent.

- 2026-08-31: Task 10 implementation is in progress. `echod` now composes the
  existing single ALSA/FileSource capture through `audio.Fanout`, keeps its
  wake pipeline and a separately warmed endpoint controller local, and gives
  the shared WSS client only completed, active-turn PCM. Wake pre-roll joins
  the next fanout frame by `audio.Frame.Offset`; Action starts a diagnostic-free
  button turn; disconnected sessions cancel/discard turns rather than retain
  microphone audio for a later reconnect. Normal mode now loads pairing/config
  state, uses bounded mDNS resolution, and has token/TLS/state/timeout options;
  `--wake-only` remains socket-free. Gateway configuration is model-prepared
  before persistence and published at the idle boundary, with deferred results
  reported explicitly. Fresh-context review initially found offline turn
  retention, non-atomic ordering, unsupported model-engine acceptance, and
  stale reconnect `hello` state; all were fixed. Focused coordinator tests
  cover pre-roll overlap/continuity, Action diagnostics/nested rejection, and
  offline discard. Broader composition-stub coverage remains to be added before
  Task 10 can be marked completed.

- 2026-08-31: A second fresh-context Task 10 review confirmed the offline,
  reconnect-hello, and model-kind fixes, then found further configuration edge
  cases. Pending revisions now participate in monotonic/conflict comparison
  before model preparation, avoiding both revision rollback and prepared-model
  leaks; disconnect cancellation now runs the idle callback so pending desired
  state is not stranded. The review also identified two remaining blockers to
  completion: dynamically increasing `wake.pre_roll_ms` needs safe ring-buffer
  capacity handling, and wake-resource/config persistence publication needs a
  fully transactional failure strategy. They remain in scope for Task 10.

- 2026-08-31: Completed Task 10. The remaining review blockers were fixed and
  regression-tested: pre-roll ring capacity grows without discarding history;
  endpoint-controller state is synchronized with config delivery; disconnect
  drains queued triggers but still invokes the idle callback; disconnected
  triggers and stale queued requests cannot carry microphone audio across a
  reconnect; and bounded, offset-aware handoff history fills frames consumed
  by the turn subscriber before a delayed wake event arrives. Endpoint config
  publication is now an infallible post-validation operation, so config state
  cannot be persisted then rejected before runtime publication. The required
  fresh-context review found four correctness/privacy issues; all were fixed,
  with none declined or postponed. Verification passed: `go test -race
  ./cmd/echod/... ./internal/device/...`; `go test -race -count=10
  ./cmd/echod/... ./internal/device/client/... ./internal/device/endpointing/...`;
  `make check-portability`; `make build-device`; `make fmt-check`; `make lint`
  (0 issues); and `make test` (70.4% total non-mock coverage). No real-hardware
  proof was attempted; that remains Task 11.

- 2026-08-31: Completed Task 9. Added a static multi-stage `Dockerfile` with
  a distroless non-root runtime, secret-aware `.dockerignore`, localhost-only
  Compose publication, read-only credential/profile mounts, a read-only root
  filesystem, dropped capabilities, explicit `--no-mdns`, and a ten-second
  graceful-stop period. The optional diagnostics override is the only Compose
  path that mounts a writable WAV directory, preserving default raw-audio
  discard. Added an ignored `.gateway-secrets/` workflow, non-secret example
  profile, and deployment guide for explicit host dotsim WSS. Docker Desktop
  did not expose individual `/tmp` bind files reliably in this WSL session, so
  the guide uses the ignored workspace directory instead. The Task 9 plan's
  stale fixture reference was corrected to the tracked
  `testdata/audio/command_endpointing_16k_mono.wav`. Compose validation and
  image build passed; the Compose gateway accepted `dotsim-docker` over
  `wss://localhost:8770/device`, recorded an endpointed 118400-byte turn, and
  logged `gateway stopped` after Compose SIGTERM. Fresh-context review found
  that pre-existing `.m2/` may contain generated development credentials but
  was not ignored; fixed by ignoring it without modifying its contents. No
  findings were declined or postponed.

- 2026-08-31: Started Task 9. Docker packaging is limited to the gateway's
  explicit authenticated WSS endpoint: Compose disables mDNS and does not make
  a multicast-visibility claim. Runtime credentials remain host-provided,
  read-only bind mounts; diagnostic WAV output will require a separate explicit
  Compose override.

- 2026-08-31: Completed Task 8 at user direction after local verification.
  `dotsim` now composes the shared WSS client with
  bounded mDNS resolution, authenticated pairing/config persistence, local
  JSON logs, endpointed canonical WAV fixture turns, and one-shot completion
  after the terminal `audio.stop` is written. It accepts the planned token,
  TLS-bypass, preferred-server, state-directory, discovery-timeout, and
  `--once` options. A real TLS gateway/dotsim runner test covers explicit WSS,
  welcome/config persistence, turn framing, and clean `--once` completion;
  it also exposed and fixed a nil response-body panic in `WSSDialer`.
  Focused race checks, `make fmt-check`, `make lint` (0 issues), `make test`,
  and `make build` passed. Manual explicit-URL gateway/dotsim testing also
  completed successfully on Windows. The manual host mDNS exercise remains
  deferred by user direction; it is not represented as completed evidence.

- 2026-08-30: Completed Task 7. Added the authenticated WSS gateway server, in-memory duplicate-replacing device registry, typed welcome/config delivery, bounded structured device logs, strict device-turn framing, default PCM discard, and opt-in atomically published diagnostic WAVs. The gateway command now requires TLS, token-file, and TOML-profile inputs; always advertises WSS (unless mDNS is explicitly disabled); exposes a data-free `/healthz`; reloads validated higher-version profiles on SIGHUP; and shuts down sessions with the HTTP service. Fresh-context review found stalled config writes, incorrect pre-window PCM handling, an implicit 32-KiB WebSocket limit, dropped log fields, request-context use after upgrade, and missing acceptance coverage. The first four correctness issues were fixed with bounded config-write contexts plus close-before-lock, ignored pre-window PCM, an explicit 64-KiB read limit, and bounded redacted log fields; the remaining review test gaps were addressed in focused tests and existing config-store coverage. Verification passed: `go test -race ./internal/gateway/devices/... ./internal/gateway/turns/... ./cmd/gateway/...`; `go test -race -count=10 ./internal/gateway/devices/... ./internal/gateway/turns/...`; `make build`; `make fmt-check`; `make lint` (0 issues); and `make test`. No hardware verification applies.

- 2026-08-30: Completed Task 6. Added the shared `internal/device/client` WSS client with injected resolver, pairing store, configuration consumer, turn source, dialer, clock, and jitter. It requires WSS and a trimmed 32-byte token-file value, gives TLS bypass an explicit development warning, persists only authenticated welcomes, serializes local turns, and keeps logs bounded, redacted, and subordinate to turn/control traffic.
- 2026-08-30: Fresh-context Task 6 review found reconnect worker leakage, stale turn frames surviving reconnect, non-strict log priority, absent outbound limits, narrow credential redaction, and partial framing after high-queue exhaustion. All six were fixed with per-session cancellation/waiting and queue draining, priority recheck, 64-KiB outbound checks, broader bounded redaction, and preflight turn queue capacity validation. The reviewer also noted missing dedicated timeout/backoff and forced-reconnect tests; postponed to Task 8's runner integration suite, which owns real reconnect behavior across client sessions.
- 2026-08-30: Task 6 verification passed: `go test -race ./internal/device/client/...`; `go test -race -count=20 ./internal/device/client/...`; `make check-portability`; `make fmt-check`; `make lint` (0 issues); and `make test`. The new client package reached 72.1% statement coverage in the full test run. No hardware verification applies.
- 2026-08-30: Started Task 6. The shared client will own resolution, authenticated WSS session lifecycle, config acknowledgement, bounded log forwarding, and active-turn framing; composition roots remain out of scope.
- 2026-08-30: Started Task 5. The gateway profile package is isolated from WSS/session wiring, which remains Task 7.
- 2026-08-30: Completed Task 5. Added strict TOML profile loading, complete-default and partial-override merging, immutable snapshots, and atomic monotonic reloads that return effective configuration for connected devices. `pelletier/go-toml/v2` is the Task 5-owned direct dependency.
- 2026-08-30: Fresh-context review found that a zero-value `Snapshot` could seed an invalid store and that strictness cases were incomplete. Both findings were fixed: `NewStore` now validates its initial snapshot, an uninitialized zero-value store rejects reloads, and tests cover override-level unknown fields, whitespace device IDs, and duplicate override tables.
- 2026-08-30: Task 5 verification passed: `go test -race ./internal/gateway/config/...`; `go test -race -count=20 ./internal/gateway/config/...`; `make fmt-check`; `make lint` (0 issues); and `make test`. The new package reached 82.6% statement coverage in the full test run.
- 2026-08-27: Plan created from `docs/DESIGN.md` and the completed Milestone 1
  contracts. Chosen execution order is simulator-first. Gateway and dotsim run
  on the host for canonical mDNS/protocol proof; Docker is an explicit-URL
  deployment smoke test.
- 2026-08-27: Deliberate design change: v0.1 command endpointing moves from the
  gateway to a separate device-local `vadlevel` instance. It reports the
  existing `audio.stop(reason="endpointed")` event. Balanced defaults are 0.50
  speech threshold, 160-ms onset, 1.5-second trailing silence, three-second
  no-speech timeout, and a user-selected 60-second hard cap.
- 2026-08-27: Dependency choices: `coder/websocket`, `betamos/zeroconf`, and
  `pelletier/go-toml/v2`. The adapter replaced `grandcat/zeroconf` during Task
  11 remediation because it supports selecting the multicast interface on
  Windows, while retaining the internal advertiser/browser boundary.
- 2026-08-27: Security choices: WSS with configured cert/key files, a shared
  development bearer token, and explicit opt-in TLS verification bypass. This
  is not production device authentication.
- 2026-08-27: Config choices: one operator-incremented monotonic version, TOML
  defaults plus device overrides, gateway ownership in normal mode, atomic
  last-known-good device persistence, SIGHUP push, staged-until-idle model
  changes, and `config.result` acknowledgements.
- 2026-08-27: Logging/audio choices: JSON local logs plus bounded low-priority
  WSS forwarding on the single connection, and opt-in diagnostic WAV storage.
  Response playback remains deferred.
- 2026-08-30: Task 2 completed. The authoritative documents now assign command
  endpointing to the device, describe its already-endpointed audio as the STT
  input, and limit the shared bearer token/TLS bypass to development. Protocol
  v1 now carries typed complete configuration, configuration acknowledgements,
  structured log records, local-endpointing capability, typed input stop
  reasons, and enforced turn/config correlation IDs.
- 2026-08-30: Fresh-context review found incomplete configuration acceptance,
  payload-less required frames, and a disabled-VAD validation gap. All three
  findings were fixed; none were declined or postponed.
- 2026-08-30: Task 3 completed. Added typed device configuration conversion,
  version comparison, atomic staged JSON persistence with file and directory
  sync, corrupt-state fallback, and a separately warmed local endpointing state
  machine. The deterministic WAV fixture covers noise, speech, an internal
  pause, resumed speech, and trailing silence. No hardware verification applies.
- 2026-08-30: Fresh-context Task 3 review found a fixture path outside the
  repository and a hard-timeout calculation that excluded transmitted wake
  pre-roll. Both were fixed in Task 3 review remediation. The reviewer also
  noted that fixture tests do not assert `vadlevel` classifications directly;
  declined because the endpoint state machine's score semantics are isolated
  through a scripted detector, while the fixture test verifies the required
  deterministic acoustic structure. `vadlevel` remains independently tested.
- 2026-08-30: Task 4 completed. Added the `grandcat/zeroconf` adapter behind
  the discovery interfaces, three-second bounded browse, IPv4/IPv6 conversion,
  validation, stable server-identity deduplication, and atomic strict-schema
  paired-gateway persistence. Real multicast remains for Tasks 8 and 11.
- 2026-08-30: Fresh-context Task 4 review found insecure advertisement input,
  permissive paired-state decoding, endpoint-based rather than server-identity
  deduplication, and missing in-flight cancellation coverage; all fixed in
  Task 4 review remediation. The review also noted that no composition root
  yet persists after `welcome` or retries after connection failure; postponed
  to Task 6 because its explicitly scoped WSS session owns authentication,
  handshake validation, and reconnect behavior.

- 2026-09-01: FireOS multicast diagnosis showed that the Dot's `p2p0` was up
  alongside `wlan0`; bringing only `p2p0` down removed the `EPERM` multicast
  send failure. The temporary `sonnt85/mdns` replacement then reached the
  gateway but produced gateway `dns: bad rdata` parsing errors, so the adapter
  was restored to `grandcat/zeroconf` and now selects a usable `wlan0` when
  present, falling back to normal selection elsewhere. Focused race tests,
  lint, and device build passed. The first rebuilt-binary hardware rerun could
  not open ALSA card 0/device 24 because it was busy; no holder was terminated,
  and the prior binary, pairing state (mode 0600), and `p2p0`-down condition
  were restored. The fresh mDNS/WSS acceptance therefore remains in progress.
  Fresh-context review found that the initial regression test did not prove
  resolver construction received the preferred interface; fixed with an
  injectable resolver-construction seam and tests for both the exact `wlan0`
  selection and empty fallback. The review found no protocol, secret, or voice
  boundary violation.

- 2026-09-02: The Windows gateway service was visible in mDNS-Browser while
  the Dot consistently browsed zero entries without multicast send errors.
  `Get-NetUDPEndpoint` showed Windows `svchost`, not the gateway process, owns
  UDP 5353; shared-port advertisement remains visible. Inspection established
  the functional defect: `opts.advertisement` leaves `Instance.Addrs` empty,
  and the zeroconf proxy therefore emits no A/AAAA record. mDNS-Browser can
  show the service, but the device resolver has no address from which to build
  a WSS endpoint. The foreground Dot process was stopped; the original binary,
  pairing state mode 0600, and `p2p0`-down condition were restored.
  The address remediation now derives only non-loopback, non-link-local
  addresses from up multicast-capable interfaces, preserves configured
  addresses without probing the host, and fails registration rather than
  emitting another addressless record. The focused race test, lint, and
  Windows compilation passed; the rebuilt `gateway-next.exe` awaits the
  user-owned gateway restart and live mDNS/WSS retry. Fresh-context review
  found the first implementation could select an unreachable tunnel address
  and mask address-discovery failure; both P1 findings were fixed. The
  follow-up review found no remaining correctness, security, or design issue.
  The live retry still browsed zero records, which exposed a second
  `RegisterProxy` interoperability defect: passing `echo-gateway.local.` makes
  grandcat publish `echo-gateway.local.local.`. Registration now passes the
  bare host label the library requires, with regression coverage for all three
  accepted host forms. The replacement `gateway-next.exe` was rebuilt after
  this correction and awaits another Windows restart before the hardware retry.

- 2026-09-02: Windows advertisement binding remediation now uses the IPv4
  source selected for `224.0.0.251:5353` to locate an up,
  multicast-capable, non-loopback interface, passes that concrete interface to
  `RegisterProxy`, and emits only usable A/AAAA addresses assigned to it.
  Registration fails when route/source/interface/address validation cannot
  establish a LAN target. Deterministic tests cover LAN binding, loopback and
  non-multicast exclusion, absent usable source ownership, selected-interface
  A/AAAA membership, configured-address rejection, and addressless failure.
  Fresh-context review found a case-insensitive DNS hostname normalization gap;
  fixed it with mixed-case `.local` coverage. No other finding was confirmed.
  `go test -race ./internal/discovery/...`, `make build-windows`, `make
  fmt-check`, `make lint`, `make test`, and `make verify` passed. The required
  native-Windows LAN-adapter observation, separate-Linux-host browse, and
  clean-pair Dot retry remain human/hardware acceptance and keep this
  remediation in progress.

- 2026-09-02: The first Windows route probe exposed a platform behavior not
  visible on Linux: a connected UDP multicast socket retained `0.0.0.0` as its
  local address, so registration correctly refused to guess an interface but
  could not start. The Windows path now asks `GetBestInterfaceEx` for the
  `224.0.0.251:5353` route when that wildcard result occurs, selects a usable
  IPv4 address only from that returned interface, and retains the existing
  interface/address validation before proxy registration. This avoids sending
  a synthetic multicast packet merely to force source assignment. Deterministic
  coverage exercises the wildcard-to-route-interface fallback; focused race
  tests and Windows cross-compilation passed. Re-run the native gateway with
  the rebuilt `.bin/gateway.exe`; the live adapter and cross-host checks remain
  required.

- 2026-09-02: A second fresh-context review found that an underlying
  `GetBestInterfaceEx` error was being flattened into the no-route case. Fixed
  it by wrapping that API error across the registration boundary and added an
  `errors.Is` regression test. The reviewer found no P1 issue with the Windows
  API use, build tags, route-interface validation, or advertisement boundary.

- 2026-09-02: Live Windows evidence showed that `GetBestInterfaceEx` also
  selected Loopback Pseudo-Interface 1 for the multicast group, which strict
  validation correctly rejected. The wildcard-only Windows path now retains
  multicast-route preference but falls back to the OS-selected default unicast
  route when that route has no usable interface/address. It still binds only a
  validated up, multicast-capable, non-loopback interface and never sends a
  probe packet. A deterministic regression test covers loopback multicast
  route plus Wi-Fi default-route selection; focused race tests and Windows
  cross-compilation passed. The rebuilt `gateway.exe` awaits the next native
  startup observation.

- 2026-09-02: Fresh review found that the first fallback's fixed unicast probe
  was not a true default-route lookup and was too broad: it could override any
  unusable multicast route, not only the observed loopback route. Fixed it to
  read the Windows IPv4 route and interface tables, choose the actual lowest
  effective-metric default route, and invoke that fallback only when the mDNS
  route interface is explicitly loopback. Non-loopback missing/down/addressless
  mDNS routes continue to fail registration. Regression coverage proves the
  no-fallback case; focused race tests and Windows test compilation passed.

##### Windows gateway mDNS binding diagnosis summary

**Status:** in progress — native Windows/LAN verification remains required.

- A gateway registered through `grandcat/zeroconf` with a `nil` interface list.
  Windows mDNS Browser displayed the service only through Loopback
  Pseudo-Interface 1; a separate Linux `avahi-browse` and the Dot received no
  record. This established advertisement-interface binding, rather than Dot
  browse, pairing, TLS, or WSS, as the failure boundary.
- The first address remediation derived usable host addresses but still passed
  `nil` to `RegisterProxy`. It repaired missing A/AAAA data but did not repair
  the loopback transmitter selection.
- Binding to a source selected by a connected UDP socket failed on Windows:
  `LocalAddr` remained `0.0.0.0`, and the gateway correctly refused to publish
  a local-only record with `multicast route selected no usable source address`.
- The Windows `GetBestInterfaceEx` query for `224.0.0.251:5353` also selected
  Loopback Pseudo-Interface 1. Strict validation rejected it, producing the
  same startup failure rather than silently advertising on loopback.
- The initial fallback used a fixed public unicast probe and was rejected in
  review because policy/VPN routes could make it differ from the actual default
  route, and because it could override failures other than the loopback case.
- The current unverified remediation uses the Windows IPv4 route/interface
  tables to select the lowest-effective-metric default route only after an
  explicitly loopback mDNS route. It validates that interface and its assigned
  IPv4 address before passing it to `RegisterProxy`. It has passed deterministic
  tests, Windows compilation, and `make verify`; it is not yet evidence of
  Windows LAN advertisement or Dot discovery.
- On this host, `GetBestInterfaceEx(224.0.0.251:5353)` instead returned a
  successful zero interface index. This is another wildcard result, not a
  usable route, so the same validated default-route fallback now applies. The
  regression tests cover index zero and isolate the non-loopback rejection test
  from live host adapters. Focused Windows race tests and a gateway rebuild
  passed; fresh-context review found no issue. Native LAN advertisement and Dot
  discovery remain required.
- Native Windows observation still showed the proxy only via Loopback and a
  separate Linux host received no browse result. Source inspection identifies
  the library boundary: `grandcat/zeroconf` transmits on its selected interfaces
  with `golang.org/x/net/ipv4` per-packet `IfIndex` control messages, but that
  package's Windows implementation is explicitly unimplemented and the library
  discards the setup error. The operating system therefore chooses Loopback.
  Route/interface selection in this repository cannot correct that transmitter
  limitation; resolving it requires an mDNS advertiser that sets Windows'
  multicast interface socket option (or a maintained dependency that does).
- The adapter now uses the pinned `betamos/zeroconf` commit, whose Windows
  transmitter sets that socket option and exposes an interface filter. The
  existing discovery contract remains unchanged behind a compatibility adapter;
  focused Windows race tests, scoped lint, and a gateway rebuild pass. Native
  Windows-to-Linux Avahi and Dot discovery are still the acceptance check.
- 2026-09-02: A MacBook on the Dot WLAN resolved the Windows gateway at
  `192.168.110.127:8770`, while the Dot's repository browser still returned
  zero entries. An independent static Linux/arm64 diagnostic built with
  EchoLocal's `libp2p/zeroconf/v2` dependency then discovered the same service
  on Dot `wlan0`, proving multicast receipt and isolating the defect to this
  repository. The adapter was calling the blocking `betamos/zeroconf` browse
  synchronously before consuming its unbuffered result channel; valid events
  therefore blocked until the browse deadline and were lost. Browse execution
  now runs concurrently with result consumption and debug logs report selected
  interfaces, raw response metadata, acceptance/rejection, counts, preferred
  address, and the resolved endpoint without logging TXT contents or tokens.
  A rebuilt Dot accepted the gateway response immediately. Because the then-
  running multi-interface gateway advertised the union of WSL, Wi-Fi, and
  VirtualBox addresses, the browser now stably prefers an address sharing the
  selected interface's subnet; it resolved `wss://192.168.110.127:8770/device`,
  completed authenticated WSS, and persisted a mode-0600 pairing at 14:34 UTC.
  Gateway publication is also corrected to run one publisher per usable
  interface with only that interface's addresses. Focused race tests, lint,
  formatting, and Windows test compilation pass. A final reviewed-binary rerun
  at 14:45 UTC logged the raw response, preferred
  `192.168.110.127`, sanitized resolved WSS fields, completed authentication,
  and persisted another mode-0600 clean-state pairing. Fresh-context review
  findings were all fixed: multi-interface behavior was reconciled into this
  plan and `docs/DESIGN.md`; `wlan0` selection is now device-only; endpoint
  logs exclude userinfo/query data; removal events withdraw candidates;
  publisher lifecycle/cleanup has contract coverage; and obsolete route code
  was removed. The final interface-scoped
  Windows gateway build still requires a restart and live record observation;
  Task 11 remains in progress for its other outstanding hardware scenarios.
- 2026-09-02: After the reviewed `gateway-next.exe` restart, a fresh-state Dot
  browse at 14:51 UTC received exactly one A record, `192.168.110.127`, rather
  than the earlier union of WSL, Wi-Fi, and VirtualBox addresses. It selected
  that endpoint, completed authenticated WSS, and persisted a mode-0600
  pairing. The original diagnostic pairing and `avahi-daemon` were restored
  afterward. This completes Task 11's mDNS-only discovery, handshake, and
  pairing acceptance; its separate turn, endpointing, reconnect, and other
  scenarios remain in progress.

## Completion evidence

- 2026-09-02: Task 12 documentation and coverage review is in progress.
  Updated the README and Windows/WSL guide for the delivered local-endpointing,
  mDNS/WSS, state-path, token/TLS, profile-reload, dotsim, and Compose
  behaviors; corrected DESIGN.md so native-LAN mDNS is not conflated with the
  explicit-WSS Docker smoke test. Fresh-context review found two defects, both
  fixed: an mDNS A/AAAA connection now preserves the advertised DNS name for
  TLS verification and persists that discovery identity after `welcome`; and
  endpoint debug logs now omit paths as well as credentials/query values. A
  verified DNS-name/IP-dial WSS regression test and mDNS endpoint-identity test
  cover the first fix. `make fmt-check`, `make lint` (0 issues), focused race
  tests for discovery/client/echod/dotsim, and `make test` passed; total
  non-mock statement coverage was 70.6%, with discovery 90.4%, mDNS 73.3%,
  client 71.3%, gateway devices 75.0%, and gateway turns 70.8%. The coverage
  audit still needs explicit dispositions for composition-root and
  library-boundary functions that remain impractical to unit-test (notably the
  real mDNS browse adapter); Task 12 remains in progress.

- 2026-09-07: Task 12 fresh-context review completed. Finding 1 (**fix**, P2):
  the active plan still named `grandcat/zeroconf` although the module and
  adapter use `betamos/zeroconf`; corrected the dependency and file-map entries
  and recorded the Windows multicast-interface rationale. Finding 2 (**fix**,
  P2): the Task 11 scope amendment could appear to silently remove forwarded
  log-pressure and rejected corrupt/missing-model configuration checks;
  clarified that Tasks 8, 3, and 10 retain and verify those host/deterministic
  requirements, while Task 11 is limited to Dot-required behavior. No finding
  was declined or postponed. An additional local review finding (**fix**, P1)
  found that a removed mDNS service record could withdraw a still-live record
  with the same `server_id`; the browser now rebuilds candidates from all live
  instance names and has a race-enabled regression test.

- 2026-09-07: `git diff --check`, `make fmt-check`, and `make lint` passed.
  `make test` passed with race detection and 70.7% non-mock statement coverage;
  touched packages include discovery 90.4%, mDNS 74.5%, client 71.0%, gateway
  devices 75.0%, and gateway turns 70.8%. `make check-portability` passed for
  darwin/arm64 and linux/arm64 (the Go tool emitted non-fatal read-only module
  stat-cache warnings). Equivalent host binaries and static linux/arm64 echod
  and echoctl builds to `/tmp` passed. The exact `make build`, `make
  build-device`, and `make build-device-ctl` commands remain blocked because
  this environment cannot overwrite its existing read-only `.bin` artifacts.
  `docker compose -f deploy/docker-compose.yml config` passed with the existing
  local certificate, key, token, and profile inputs. The operator then ran
  `docker compose -f deploy/docker-compose.yml build gateway` in their
  daemon-accessible WSL session; it completed successfully on 2026-09-07 and
  produced `echo-satellite-gateway:local`. The agent's earlier socket denial
  was therefore an execution-environment restriction, not a Docker or image
  defect. A post-fix focused race sweep passed for discovery, echod, client,
  and turns; dotsim and gateway devices' direct `httptest` runs were denied an
  IPv6 loopback listener by the sandbox, while their coverage-bearing execution
  in the successful `make test` run passed. Task 12 is complete. The required
  `git mv` into `docs/plans/finished/` was attempted but could not create
  `.git/index.lock` because this agent session mounts `.git` read-only; the
  plan remains in-progress solely until that lifecycle move can be performed.

- 2026-08-31: Task 9: `docker compose -f deploy/docker-compose.yml config`
  passed with host-provided read-only certificate, key, token, and TOML mounts,
  `127.0.0.1:8770` publication, and mDNS disabled.
- 2026-08-31: Task 9: `docker compose -f deploy/docker-compose.yml build
  gateway` passed, producing `echo-satellite-gateway:local` from a static
  multi-stage build.
- 2026-08-31: Task 9: Compose gateway startup plus `.bin/dotsim
  --device-id dotsim-docker --discover disabled --gateway-url
  wss://localhost:8770/device --gateway-token-file
  /home/mike/.cache/echo-satellite-m2/device-token --tls-skip-verify --mic
  testdata/audio/command_endpointing_16k_mono.wav --once` passed. Gateway logs
  show an authenticated device, `audio.stop(reason="endpointed")`, and 118400
  PCM bytes. `docker compose ... stop` then logged `gateway stopped`; `down`
  removed the container and network.
- 2026-08-31: Task 9: `make fmt-check`, `make lint` (0 issues), and `make
  test` passed; full race-test coverage was 71.9%.
- 2026-08-31: Task 9 fresh-context review: fixed the P1 staging risk by adding
  `.m2/` to `.gitignore`; reviewer found no other Task 9 blockers.
- 2026-08-30: `go test -race ./internal/protocol/...` passed.
- 2026-08-30: stale gateway-endpointing scan returned no matches:
  `rg -n "gateway command endpointing|gateway-side initially|command endpointing.*gateway" docs/DESIGN.md docs/protocol.md AGENTS.md`.
- 2026-08-30: `make fmt-check`, `make lint` (0 issues), and `make test` passed.
  `internal/protocol` coverage was 92.5%; new config validation, required
  payload, correlation-ID, and invalid-wire-config paths are covered.
- 2026-08-30: No hardware verification applies to Tasks 1–2.
- 2026-08-30: Task 3: `go test -race ./internal/device/config/... ./internal/device/endpointing/... ./internal/device/wake/vadlevel/...` passed.
- 2026-08-30: Task 3: `go test -race -count=20 ./internal/device/endpointing/...` passed.
- 2026-08-30: Task 3: `make fmt-check`, `make lint` (0 issues), and `make test` passed. New package coverage: `internal/device/config` 73.8%; `internal/device/endpointing` 84.6%.
- 2026-08-30: Task 3 review remediation: `go test -race ./internal/device/endpointing/...` and `go test -race -count=20 ./internal/device/endpointing/...` passed; final `make fmt-check`, `make lint` (0 issues), and `make test` passed. Endpointing coverage was 85.5%.
- 2026-08-30: Task 4 and review remediation: `go test -race ./internal/discovery/...`, `go test -race -count=10 ./internal/discovery/...`, `make check-portability`, `make fmt-check`, `make lint` (0 issues), and `make test` passed. Discovery coverage was 90.2%; the new mDNS adapter was 83.8%.
- 2026-08-30: Task 6: `go test -race ./internal/device/client/...`, `go test -race -count=20 ./internal/device/client/...`, `make check-portability`, `make fmt-check`, `make lint` (0 issues), and `make test` passed. Client coverage was 72.1%; no hardware verification applies.
- 2026-08-31: Task 8: local focused race checks, `make fmt-check`, `make lint`
  (0 issues), `make test`, and `make build` passed. Windows explicit-WSS
  gateway/dotsim testing completed one `--once` turn successfully. The
  required real-mDNS manual check remains deferred and is not claimed here.
