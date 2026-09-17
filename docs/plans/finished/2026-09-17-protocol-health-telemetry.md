# Protocol health telemetry implementation plan

**Status:** in-progress (hardware qualification deferred)
**Owner or active agent:** Codex
**Created:** 2026-09-17
**Updated:** 2026-09-17
**Started:** 2026-09-17
**Completed:** not completed

## Objective

Define and implement a bounded, device-to-gateway health telemetry stream that
makes capture, wake, conditioning, resource, and per-turn endpointing facts
observable without uploading idle microphone audio or giving the gateway any
role in wake or endpointing decisions. Extend the existing device-owned
`audio.stop` payload with its correlated terminal telemetry rather than adding
a second per-turn event.

## Non-goals

- Gateway-side wake scoring, wake VAD, endpointing, or any gateway request to
  start/stop a voice turn.
- Continuous raw microphone upload, transcript collection, or raw audio
  retention by default.
- A general metrics backend, management UI, fleet-health policy, or protocol
  version bump.
- Changing the selected conditioning profile or claiming Task 10/11 acoustic
  acceptance before their required real-Dot evidence exists.
- Replacing the existing explicit, opt-in gateway diagnostic WAV facility.

## Source references and constraints

- `docs/DESIGN.md` §3.2–3.3, §8, §16, and §27: wake and wake-VAD remain local;
  endpointing is device-local after a local trigger; the gateway observes but
  does not decide either; raw audio is not stored by default.
- `docs/protocol.md` §§2–4 and §8: `health` is reserved D→G, `audio.stop`
  already has the active turn's non-empty correlation ID, and unknown control
  types are forward-compatible.
- `docs/plans/in-progress/2026-09-08-milestone-3-single-agent-deployment-audio.md`
  Task 11: gateway-correlated wake counts, stop reasons/durations, WAV
  confirmation, and attributable capture/conditioning measurements remain
  required hardware evidence.
- `internal/gateway/turns`: optional WAV output is opt-in and publishes only
  completed valid turns. Its received PCM byte count and gateway timestamps are
  authoritative for the received audio window.
- Telemetry values must be bounded, validated, finite, and secret-free. No
  arbitrary maps, paths, raw samples, tokens, credentials, model assets, or
  waveform-derived payloads may cross the protocol boundary.

## Dependencies and prerequisites

- The Milestone 3 plan remains in progress. This plan must not be moved to
  `in-progress/` until its owner and the Task 11 owner agree on file ownership
  and execution order.
- Task 11 owns the real-Dot qualification. Its existing staged diagnostic
  payload and device-lab session safety model remain the only way this work
  accesses the qualified Dot.
- The gateway diagnostic evidence and WAV directories must be explicit,
  existing, owner-writable opt-ins. Their cleanup and raw-audio deletion must
  be proven in the Task 11 run, not assumed from code review.
- The telemetry wire contract must land before agent, gateway, and payload
  integration. Until it does, all new behavior is feature-gated by capability
  and local diagnostic configuration.

## Architecture and high-level plan

The device sends two bounded observability reports over its existing
authenticated WSS session:

```text
periodic device state
  echod -> health(capture/resource/wake/conditioning snapshot) -> gateway

device-local active turn
  local wake or Action button -> turn.start(id)
  -> audio.start(id) -> PCM -> audio.stop(id, reason, telemetry)
                                    -> gateway evidence record
```

`health` is a typed D→G snapshot, emitted at a bounded local interval. It
contains process and capture counters plus cumulative, local wake and
conditioning observations. It is informational; a gateway receiving it cannot
issue a command in response.

The optional `audio.stop.telemetry` object is a terminal delta/snapshot for the
same turn identified by the envelope ID. It includes the device-observed stop
time and duration, capture XRuns and fanout drops observed during that turn,
conditioning measurements, and a resource sample. `reason` remains the
device's endpointing outcome. The gateway adds only its own received-window
facts (receive timestamps, PCM bytes, and optional WAV name/size/derived
duration) to a diagnostic evidence record.

A device advertises `health.telemetry.v1` in `hello` when it can produce the
defined shapes. Older gateways safely ignore the defined-yet-new `health`
message and optional `audio.stop.telemetry`; newer gateways accept and validate
them. The capability is an observation of device support, never permission for
the gateway to alter the health interval or turn behavior.

The gateway writes durable sanitized JSONL evidence only when an explicit
diagnostic evidence directory is configured. Normal operation retains no new
telemetry files. Task 11 enables that sink and the existing WAV sink only for
the controlled run, then validates its records, removes WAVs, and restores
diagnostic storage to disabled.

## Planned file map

- Modify `internal/protocol/messages.go` and `internal/protocol/*_test.go`:
  typed health and terminal telemetry payloads, capability, validation,
  finite/range limits, and compatibility fixtures.
- Modify `docs/protocol.md`: mark `health` defined; document exact schemas,
  direction, cadence semantics, optional `audio.stop.telemetry`, privacy, and
  authority boundaries.
- Modify `internal/device/audio/capture.go` and tests only if a narrow,
  race-safe capture/conditioning snapshot interface is needed; do not expose
  PCM buffers or change the fanout ownership model.
- Modify `cmd/echod/runtime.go`, `cmd/echod/main.go`, configuration and tests:
  collect bounded snapshots, compute per-turn deltas, schedule health reports,
  and send through the established client session.
- Modify `internal/device/client/client.go` and tests: queue/serialize typed
  health without allowing telemetry backpressure to block local audio or turn
  control.
- Modify `internal/gateway/devices/server.go` and tests: validate, log, and
  correlate health and terminal telemetry while preserving existing turn
  lifecycle checks.
- Modify `internal/gateway/turns/*` only if a small completed-turn metadata
  accessor is necessary; WAV writing/promotion semantics remain unchanged.
- Modify `cmd/gateway/config.go`, `cmd/gateway/main.go`, deployment diagnostics
  override, and tests: explicit evidence directory and safe lifecycle wiring.
- Modify `tools/device-lab/payloads/task11_command_audio.sh`,
  `tools/device-lab/device_lab.py`, and their tests: live phase cues, opt-in
  telemetry/evidence collection contract, and deletion/restore checks.
- Modify `docs/DESIGN.md`, `docs/gateway-deployment.md`,
  `docs/device-diagnostics.md`, and the Milestone 3 plan: protocol decision,
  operator procedure, actual trial evidence, and finding dispositions.

## Numbered tasks

### Task 1: Freeze the protocol contract and compatibility rules

**Status:** completed 2026-09-17

**Purpose:** Make the health and terminal telemetry schema precise before any
runtime code can emit it.

**Dependencies:** None.

**Hardware required:** no.

**Files or components:**

- Modify `internal/protocol/messages.go`, protocol tests, and `docs/protocol.md`.

**Concrete changes:**

- Define a `Health` payload with a fixed version/kind and named bounded fields
  for capture health, wake/VAD counters and timing, conditioning metrics, and
  resource usage.
- Define an optional typed `AudioStop.Telemetry` payload keyed implicitly by
  the existing non-empty envelope ID. Specify counter-delta versus end-snapshot
  semantics, duration precision, mandatory profile identity, and legal ranges.
- Add `health.telemetry.v1` capability and exact rules for absent capability,
  unknown optional fields, older gateways, and duplicate/out-of-order health
  samples.
- Reject NaN/Inf, negative counts/durations, invalid profile names, oversized
  payloads, unrecognized report versions/kinds, and raw/secret-bearing fields.
- State that `audio.stop.reason` is still the device-local endpointing outcome;
  telemetry cannot override it. State which timestamps are device observations
  and which are gateway receipt observations.

**Expected outcome:** Both peers share a version-1-compatible, bounded,
privacy-preserving contract with no ambiguity about authority or correlation.

**Verification:**

```sh
go test -race ./internal/protocol/...
git diff --check
```

Expected: valid fixtures round-trip; every invalid, oversized, non-finite, or
secret-like shape is rejected; existing protocol fixtures remain compatible.

### Task 2: Expose race-safe local health snapshots

**Status:** in progress — initial snapshot path is wired; full conditioning snapshot/delta coverage remains

**Purpose:** Supply the agent with metrics that describe the same conditioned
capture path used by wake, pre-roll, endpointing, and command PCM.

**Dependencies:** Task 1.

**Hardware required:** no.

**Files or components:**

- Modify `internal/device/audio/*` only as required and add one test file per
  changed source file.
- Modify `cmd/echod/runtime.go` and tests.

**Concrete changes:**

- Add an immutable snapshot boundary around capturer XRuns, subscriber drops,
  and the selected conditioner without exposing mutable PCM slices.
- For the selected profile, report cumulative conditioning gain, peak/RMS,
  clipping count/fraction, total processing time, and maximum block duration.
- Take a baseline at device-local turn start and compute a terminal delta at
  `audio.stop`; do not infer a turn's metrics from a later periodic sample.
- Sample CPU/RSS from `/proc/self` on the agent with a documented failure
  representation that does not fail a healthy turn merely because a metric is
  unavailable.

**Expected outcome:** Snapshot reads are race-free and cannot change capture
timing, fanout delivery, or local voice behavior.

**Verification:**

```sh
go test -race ./internal/device/audio/... ./cmd/echod/...
make check-portability
```

Expected: deterministic delta tests cover no-speech, endpointed, timeout, and
capture-overrun terminal paths; no health snapshot retains PCM.

### Task 3: Send bounded device health without affecting turn control

**Status:** in progress — bounded reporting is wired; diagnostic interval override and saturation coverage remain

**Purpose:** Emit periodic health and terminal telemetry through the existing
authenticated client while retaining local audio priority.

**Dependencies:** Tasks 1–2.

**Hardware required:** no.

**Files or components:**

- Modify `cmd/echod/config.go`, `cmd/echod/main.go`, `cmd/echod/runtime.go`,
  `internal/device/client/client.go`, and their tests.

**Concrete changes:**

- Advertise `health.telemetry.v1` only in builds that implement the completed
  schema.
- Add a bounded local health interval with a conservative default and explicit
  diagnostic override. The gateway does not configure the interval.
- Send health on a best-effort, bounded queue. A full telemetry queue may drop
  a health sample and increment a local telemetry-drop counter, but must never
  block capture, wake acceptance, `turn.start`, `audio.start`, PCM, or
  `audio.stop`.
- Attach terminal telemetry before sending `audio.stop`, preserving one final
  control-frame ordering and the existing turn ID.
- On reconnect, discard stale periodic samples; never replay a terminal sample
  for a previous session or attach it to a different turn ID.

**Expected outcome:** Health is observable during a healthy connection yet has
no control-plane authority and no capacity to delay local voice processing.

**Verification:**

```sh
go test -race ./cmd/echod/... ./internal/device/client/...
go test -race -count=20 ./internal/device/audio/...
```

Expected: queue saturation, reconnect, shutdown, and concurrent turn tests
prove audio/turn priority and exact terminal correlation.

### Task 4: Accept, correlate, and optionally persist gateway evidence

**Status:** completed 2026-09-17

**Purpose:** Turn device reports into reviewable Task 11 evidence without
creating default raw-audio or telemetry retention.

**Dependencies:** Tasks 1 and 3.

**Hardware required:** no.

**Files or components:**

- Modify `internal/gateway/devices/server.go`, `internal/gateway/turns/*` only
  if necessary, `cmd/gateway/*`, deployment diagnostics config, and tests.

**Concrete changes:**

- Validate health frames and optional terminal telemetry in the device session;
  reject malformed terminal data as a protocol error while treating a missing
  optional telemetry object as compatible with older devices.
- Keep the gateway turn receiver authoritative for received PCM byte count and
  gateway timestamps. Join terminal telemetry only when its envelope ID is the
  active completed turn's ID and its session/device matches.
- Add an explicit diagnostic evidence-directory option. If unset, log bounded
  observations only and do not create telemetry files.
- When enabled, atomically append/rotate sanitized JSONL records containing
  correlation ID, device ID, gateway and device timestamps, start/stop values,
  metrics, PCM bytes, and optional WAV filename/size/derived duration. Never
  serialize raw PCM, absolute gateway paths, credentials, or arbitrary device
  log fields.
- Make evidence-write failure observable and isolated: it must not accept an
  invalid turn, but it must not retroactively alter a valid device-local
  endpointing decision.

**Expected outcome:** One inspectable, attributed record exists for each
completed diagnostic turn, while normal deployments add no persistence.

**Verification:**

```sh
go test -race ./internal/gateway/devices/... ./internal/gateway/turns/... ./cmd/gateway/...
```

Expected: tests cover enabled/disabled retention, ID/session mismatch,
redaction, invalid payload rejection, WAV metadata calculation, write failure,
and older-device compatibility.

### Task 5: Adapt Task 11's controlled operator procedure

**Status:** blocked — deferred until the implemented software is available on the qualified Dot

**Purpose:** Convert the current schedule-only Dot payload into a reproducible
qualification run with human-visible phase boundaries and complete sanitized
evidence.

**Dependencies:** Task 4 and agreement with the active Milestone 3 Task 11
owner.

**Hardware required:** yes — rooted qualified Dot `G090LF0964060EHP`, explicit
Linux ADB path, paired gateway with explicit temporary diagnostic evidence/WAV
directories, and a present operator for every spoken trial.

**Files or components:**

- Modify `tools/device-lab/payloads/task11_command_audio.sh`,
  `tools/device-lab/device_lab.py`, and their tests.
- Modify `docs/device-diagnostics.md` and the Milestone 3 plan evidence only
  after a completed, reviewed run.

**Concrete changes:**

- Add host-visible phase cues with UTC boundaries and trial labels, without
  recording spoken content.
- Start the staged agent with health telemetry enabled and confirm the gateway
  sees its capability before beginning the first wake window.
- Define a host-side validator that requires all scheduled wake/idle/continuous
  speech/endpointed/no-speech records, reports missing or extra correlations,
  verifies thresholds, and validates the diagnostic WAV metadata.
- Before access, run device-lab `preflight` with explicit ADB/serial; then
  prepare the token-owned root. Before microphone diagnostics, stop
  `ledcontroller`, set `boot_animation=0`, and verify GPIO 444 is low through
  the quoted remote root script.
- After the run, delete every gateway diagnostic WAV, restore disabled
  diagnostics, run runner cleanup and `verify-clean`, and prove the installed
  `echod` digest is unchanged.

**Expected outcome:** Task 11 can produce the required gateway-correlated
evidence without installing a diagnostic binary or retaining raw audio.

**Verification:**

```sh
sh -n tools/device-lab/payloads/task11_command_audio.sh
uv run --no-project --script tools/device-lab/device_lab_test.py
```

Expected: static contracts prove isolated paths, explicit opt-in behavior,
phase schedule, bounded cleanup, required health evidence, and no device-side
raw-audio artifact.

### Task 6: Documentation, independent review, and qualification evidence

**Status:** in progress — hardware qualification and fresh-context review remain

**Purpose:** Reconcile the contract, user-facing operator procedure, and
hardware evidence, then obtain a fresh-context security/correctness review.

**Dependencies:** Tasks 1–5.

**Hardware required:** yes — Task 5's qualification run; no additional device
action for review.

**Files or components:**

- Modify `docs/DESIGN.md`, `docs/protocol.md`, `docs/gateway-deployment.md`,
  `docs/device-diagnostics.md`, and the Milestone 3 plan as warranted by
  accepted results.

**Concrete changes:**

- Document cadence, fields, compatibility, and retention defaults in the
  design and protocol sources of truth.
- Record the exact gateway/device commands, session ID, evidence artifact
  location, accepted/rejected trial counts, stop reasons/durations, metrics,
  diagnostic WAV validation/deletion, cleanup, and unchanged installed digest.
- Dispatch a fresh-context reviewer with this plan, the diff, and the design
  documents. Record every finding as fixed, declined with rationale, or
  postponed in the active plan.

**Expected outcome:** The evidence is independently reviewable and clearly
separated into host/unit, gateway, and physical-Dot results.

**Verification:**

```sh
make fmt-check
make lint
make test
make build-device
make build-device-noasm
make check-portability
make verify
```

Expected: all checks pass. Hardware acceptance additionally requires the
Task 11 numeric thresholds: at least 18/20 accepted wake trials, zero false
wakes during 15-minute idle/music, exact continuous-speech timeout and
endpoint/no-speech timing windows, zero XRuns/dropped frames, clipping at or
below 0.1%, gateway WAV validation, and restored disabled raw-audio storage.

## Cross-task risks

- **Telemetry backpressure delays voice control:** use a separate bounded,
  lossy telemetry queue and prove `audio.stop` priority under saturation.
- **Protocol growth leaks sensitive data:** fixed typed fields, maximum sizes,
  finite validation, redaction tests, and no arbitrary map/string payloads.
- **Metrics are attributed to the wrong turn:** capture a local baseline at
  turn start; require matching session/device/envelope ID on the gateway; drop
  stale reports after reconnect.
- **Evidence storage becomes an implicit audio-retention feature:** require
  explicit existing directories, retain metadata only by default, document
  WAV opt-in, and verify deletion after the qualification run.
- **Diagnostic process changes the product installation:** retain the
  token-owned device-lab root and digest-before/after check; never write
  `/data/local/bin/echod`.
- **Concurrent Task 11 edits conflict:** name ownership before activation and
  assign non-overlapping files/tasks; the orchestrator owns the Milestone 3
  plan status and final evidence.

## Rollback or recovery

- The wire additions are optional and backward-compatible: removing the
  gateway evidence-directory option stops persistence; older agents continue
  to send the current `audio.stop` shape.
- If the agent's health implementation fails, disable the diagnostic telemetry
  override and run the installed known-good agent; do not alter the launcher,
  installed binary, or FireOS partitions.
- If a task-owned device-lab session is interrupted, resume only its canonical
  session root, run cleanup and `verify-clean`, and investigate any installed
  digest mismatch manually. Do not kill unknown microphone holders.

## Final acceptance criteria

- [x] `health` and optional `audio.stop.telemetry` are typed, bounded,
  validated, and documented as D→G observability only.
- [x] The Dot advertises `health.telemetry.v1`; health reporting cannot block
  local capture, wake, endpointing, PCM, or turn controls.
- [x] Per-turn telemetry shares the terminal `audio.stop` correlation ID and
  is never associated with another session or turn.
- [x] Gateway evidence is opt-in, sanitized, and records received PCM/WAV
  metadata separately from device-observed values.
- [x] Normal deployments retain neither new telemetry files nor raw audio by
  default.
- [ ] Task 11's required gateway-correlated counts, terminal reasons/durations,
  capture/conditioning metrics, WAV validation, raw-audio deletion, cleanup,
  and installed-digest check are recorded from a real Dot.
- [ ] Formatting, lint, fresh race tests, portability builds, both device
  builds, and `make verify` pass.
- [ ] Fresh-context review findings have explicit dispositions.

## Progress log

- 2026-09-17: Created as a future plan at the user's direction after the
  schedule-only Task 11 session demonstrated that its existing payload cannot
  supply gateway-correlated turn records or attributable runtime metrics. No
  code, gateway, or device state was changed by creating this plan.
- 2026-09-17: User deferred Task 11 hardware testing until this plan's software
  implementation is complete. Tasks 1–4 are being implemented first; the
  lifecycle move to `in-progress/` is unavailable because that directory is
  read-only in the mounted workspace.
- 2026-09-17: Tasks 1–4 implemented. The device now advertises
  `health.telemetry.v1`, emits bounded best-effort health, attaches terminal
  per-turn observations to `audio.stop`, and the gateway optionally appends
  sanitized JSONL evidence. Tasks 5–6 remain open for the qualified-Dot run and
  fresh-context review.

## Completion evidence

- Host/software verification completed 2026-09-17:
  `go test ./...`, `go test -race` scoped protocol/client/device/echod/gateway
  packages, `make fmt-check`, `make lint`, `make test`, `make build`,
  `make check-portability`, and `make verify` all passed. Coverage was 69.4%.
- Hardware qualification is deferred: no real-Dot Task 11 session, WAV
  deletion/restore proof, installed digest comparison, or acoustic thresholds
  were claimed. A fresh-context review is also still required.
- This section will list exact host/gateway/device commands,
  results, session IDs, evidence locations, reviewer findings and dispositions,
  plus any remaining limitations when the plan is completed.
