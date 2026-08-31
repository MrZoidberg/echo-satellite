# Milestone 2 — Discovery, protocol, simulator, and device turns implementation plan

**Status:** in-progress
**Owner or active agent:** /root
**Created:** 2026-08-27
**Updated:** 2026-08-30
**Started:** 2026-08-30
**Completed:** not completed

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
  `github.com/grandcat/zeroconf`, and `github.com/pelletier/go-toml/v2`.
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
- `internal/discovery/mdns`: `grandcat/zeroconf` implementations of the existing
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
  `grandcat/zeroconf`.
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

**Status:** in progress

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

**Status:** not started

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
  --mic testdata/audio/turn_speech_trailing_silence.wav --once

docker compose -f deploy/docker-compose.yml down
make fmt-check
make lint
make test
```

Expected: Compose validates/builds and dotsim completes one endpointed turn.

### Task 10: Integrate discovery, config, endpointing, and turns into echod

**Status:** not started

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

**Status:** not started

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
- Exercise pauses below and above 1.5 seconds, no speech, quiet speech, background
  noise, and the 60-second cap.
- Record false cuts, endpoint latency, CPU/RSS, drops, and any measured tuning in
  `docs/DESIGN.md`.
- Stress forwarded logs during a turn and confirm they cause no audio drops.
- Confirm corrupt config fallback and missing-model rejection.
- Keep the plan in progress if multicast or endpointing acceptance fails.

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
results are recorded.

### Task 12: Cross-cutting verification, documentation, and fresh review

**Status:** not started

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

- [ ] Governing docs consistently assign wake and command endpointing to
  separate device-local components.
- [ ] No gateway wake/VAD mode or idle microphone-audio path exists.
- [ ] Gateway advertises `_echo-satellite._tcp.local.` and host dotsim/echod
  resolve it.
- [ ] Explicit URL, persisted gateway, preferred server ID, and mDNS precedence
  are tested.
- [ ] Unauthorized upgrades fail; authenticated WSS completes hello/welcome.
- [ ] TLS verification defaults on; bypass is visibly development-only.
- [ ] Config versions are monotonic, persisted, atomic, acknowledged, and
  conflict-tested.
- [ ] Pushed wake settings select only an installed valid model and cannot break
  the current model on rejection.
- [ ] Logs are JSON locally, bounded/redacted over WSS, and lower priority than
  audio.
- [ ] dotsim discovers/connects, configures, streams one endpointed turn, stays
  connected, and reconnects.
- [ ] Gateway enforces framing and stores audio only under explicit diagnostics.
- [ ] Compose gateway accepts host dotsim through explicit WSS.
- [ ] Real echod wake and Action turns stream continuous canonical PCM and stop
  locally on silence.
- [ ] Real hardware measurements and tuning are recorded in `docs/DESIGN.md`.
- [ ] Format, lint, race tests, builds, portability, Docker build, and fresh
  review pass.
- [ ] Every review finding has an explicit disposition.

## Progress log

- 2026-08-31: Started Task 8. `dotsim` now composes the shared WSS client with
  bounded mDNS resolution, authenticated pairing/config persistence, local
  JSON logs, endpointed canonical WAV fixture turns, and one-shot completion
  after the terminal `audio.stop` is written. It accepts the planned token,
  TLS-bypass, preferred-server, state-directory, discovery-timeout, and
  `--once` options. A real TLS gateway/dotsim runner test covers explicit WSS,
  welcome/config persistence, turn framing, and clean `--once` completion;
  it also exposed and fixed a nil response-body panic in `WSSDialer`.
  Focused race checks, `make fmt-check`, `make lint` (0 issues), `make test`,
  and `make build` passed. The required manual host mDNS exercise remains
  outstanding, so this task is not yet marked complete.

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
- 2026-08-27: Dependency choices: `coder/websocket`, `grandcat/zeroconf`, and
  `pelletier/go-toml/v2`.
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

## Completion evidence

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
