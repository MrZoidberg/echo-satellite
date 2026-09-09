# Echo Satellite Protocol

Protocol version: **1**

This document is the wire contract between a device agent (`echod`, or `dotsim`
standing in for it) and the gateway. It tracks [`internal/protocol`](../internal/protocol)
and **must be updated in the same change as any wire change there**.

`docs/DESIGN.md` §8 remains authoritative for why the protocol looks like this.
This document is authoritative for what is actually on the wire.

### Single-agent architecture transition

The approved deployment architecture is the single-agent boundary in
`docs/DESIGN.md` §§3.7–3.10 and §10: the gateway owns desired agent version,
while the device owns pre-install verification and atomic replacement of
`/data/local/bin/echod`. A failure before replacement leaves the installed agent
untouched. After replacement there is no preserved fallback; a client that
cannot reconnect requires ADB recovery, while gateway rollback for a connected
client means deploying a previous signed compatible release.

Update payloads are strict: required fields cannot be omitted and unknown
fields are rejected. A deployment is identified by its non-empty
`deployment_id` in every update message.

## 1. Transport and framing

- One long-lived **outbound** secure WebSocket per device (`wss`). The device
  always dials the gateway; the gateway never dials a device.
- Milestone 2 authenticates that connection with a shared development bearer
  token. TLS verification is on by default; `tls_skip_verify` is a visible,
  development-only escape hatch. Per-device authentication and mTLS are future
  production work.
- **Control and events** travel as JSON text frames, each carrying one envelope.
- **Audio** travels as raw PCM in binary WebSocket frames. Binary frames are
  only valid inside an audio window:
  - device → gateway between `audio.start` and `audio.stop`;
  - gateway → device between `play.start` and `play.stop`.
- The connection may stay idle indefinitely. While idle, **no microphone audio
  is sent**: the device is listening locally and has not opened a turn.

### Audio window rules

| Direction | Opened by | Closed by | Format for protocol 1 |
|---|---|---|---|
| device → gateway | `audio.start` | `audio.stop` | `pcm_s16le`, sample rate and channel count from `audio.start` |
| gateway → device | `play.start` | `play.stop` | `pcm_s16le`, sample rate and channel count from `play.start` |

A binary frame received outside a window is ignored, never buffered "just in
case". Invalid control-frame sequencing remains a protocol error and closes
the connection.

For a device input window, all binary PCM is associated with the single active
`turn.start`/`audio.start` correlation id. A device cannot open a second input
window until it closes the first one.

## 2. Envelope

Every control frame is:

```json
{
  "type": "turn.start",
  "id": "01J...",
  "ts": "2026-08-19T12:00:00Z",
  "payload": { }
}
```

| Field | Type | Notes |
|---|---|---|
| `type` | string | one of the message types below |
| `id` | string | required and non-empty for `turn.start`, `audio.start`, `audio.stop`, and `config`; one input turn reuses one id for its markers |
| `ts` | RFC 3339 timestamp | when the sender produced the frame |
| `payload` | object | omitted for messages that carry no data, such as `ping` |

**Unknown `type` values are not errors.** A peer speaking a newer protocol will
send types this build does not know; the receiver ignores that frame and keeps
the connection. An envelope with an empty `type` *is* an error.

## 3. Message families

Direction is `D→G` (device to gateway), `G→D`, or `both`. Status is `defined`
(a payload type exists in `internal/protocol`) or `reserved` (the type name is
fixed, and its payload lands with the milestone that needs it).

| Type | Direction | Status | Purpose |
|---|---|---|---|
| `hello` | D→G | defined | identity, versions, capabilities, wake summary, active config version, update state |
| `welcome` | G→D | defined | server identity and gateway-managed device config |
| `config` | G→D | defined | versioned gateway-managed device configuration |
| `config.result` | D→G | defined | device acknowledgement of a config revision |
| `state` | both | defined | semantic device state (LED ring, listening, thinking) |
| `health` | D→G | reserved | periodic device health report |
| `log` | D→G | defined | forwarded device log records |
| `turn.start` | D→G | defined | opens a voice turn; **always produced by the device** |
| `turn.cancel` | both | reserved | abandons the current turn |
| `wake.models` | both | reserved | wake model inventory and synchronization |
| `wake.status` | D→G | reserved | device-local wake stack status |
| `audio.start` | D→G | defined | opens the command audio window |
| `audio.stop` | D→G | defined | closes the command audio window |
| `play.start` | G→D | defined | opens the playback window |
| `play.stop` | G→D | defined | closes the playback window |
| `update.offer` | G→D | defined | offers a signed release and its HTTPS resources |
| `update.decision` | D→G | defined | device accepts or rejects an offer |
| `update.progress` | D→G | defined | current update phase and progress |
| `update.confirmed` | D→G | defined | installed version/build confirmed after reconnect |
| `update.cancelled` | D→G | defined | deployment cancelled before replacement |
| `update.failed` | D→G | defined | terminal update failure, with a code |
| `button` | D→G | reserved | action button press |
| `mute` | both | reserved | microphone mute state |
| `volume` | both | reserved | volume state |
| `ping` | both | defined (no payload) | liveness probe |
| `pong` | both | defined (no payload) | liveness reply |
| `error` | both | defined | error with a code and a message |

## 4. Payloads

### `hello` (D→G)

```json
{
  "device_id": "dot-kitchen",
  "agent_version": "0.3.0",
	"protocol": 1,
  "capabilities": ["audio.capture", "audio.playback", "update.single.v1", "wake.local"],
  "wake_config": {
    "engine": "openwakeword",
    "models": ["okay_nabu"],
    "wake_threshold": 0.6,
    "vad_threshold": 0.5,
    "pre_roll_ms": 500
  },
  "config_version": 3,
	"update_state": "idle"
}
```

`wake_config` is reported for observability. The gateway cannot change the local
wake stack by replying with a different summary, and it never scores wake audio.

`hello` does not carry a launcher/supervisor version. The launcher has no
deployment compatibility contract.

### `welcome` (G→D)

```json
{ "server_id": "home-gateway", "protocol": 1, "config": { "version": 3, "wake": { "engine": "openwakeword", "model": "okay_nabu", "threshold": 0.5, "vad_enabled": true, "vad_threshold": 0.5, "vad_lookback_ms": 1200, "pre_roll_ms": 600, "min_interval_ms": 2000, "always_score_wake": true }, "endpointing": { "speech_threshold": 0.5, "speech_onset_ms": 160, "trailing_silence_ms": 1500, "no_speech_timeout_ms": 3000, "max_turn_ms": 60000 }, "audio": { "conditioning_profile": "bypass-v1" }, "logs": { "forward_level": "info" } } }
```

`config` is a typed, complete device configuration. Protocol version 1 owns its
schema; its positive `version` orders gateway desired state and is not the wire
protocol version. A `config` message uses the same payload and requires a
non-empty envelope id.

### `config.result` (D→G)

```json
{ "version": 3, "status": "applied", "code": "", "detail": "" }
```

`status` is `pending`, `applied`, or `rejected`. A rejected result includes a
non-empty `code`; a pending wake-model change applies when the device is idle.

### `turn.start` (D→G)

```json
{ "trigger": "wake", "model": "okay_nabu", "wake_score": 0.87, "vad_score": 0.93, "pre_roll_ms": 500 }
```

`trigger` is `wake` or `button` — both are device-local decisions. Scores are
what the device already computed; the gateway consumes them as telemetry, not as
inputs to a decision it re-makes.

### `audio.start` / `play.start`

```json
{ "sample_rate": 16000, "channels": 1, "format": "pcm_s16le" }
```

### `audio.stop` / `play.stop`

```json
{ "reason": "endpointed" }
```

For a device input window, `reason` is one of `endpointed`, `no_speech`,
`timeout`, `eof`, or `capture_overrun`. `audio.stop` reuses the turn's
non-empty correlation id. Playback stop reasons are gateway-defined.

### `log` (D→G)

```json
{ "level": "info", "message": "connected", "fields": { "server_id": "home-gateway" } }
```

`level` is `debug`, `info`, `warn`, or `error`. Fields are structured string
values; implementations sanitize credentials before forwarding them.

### `state` (both)

```json
{ "state": "thinking", "detail": "asking assistant" }
```

States, in display order: `idle`, `listening`, `thinking`, `speaking`, `muted`,
`offline`, `error`, `updating`. `muted` distinguishes the local privacy state
from a fault and `offline` indicates loss of gateway connectivity.

### `update.offer` (G→D)

```json
{
	"deployment_id": "01J...",
  "version": "0.3.0",
  "build_id": "git-abc123",
  "artifact_url": "https://gateway.local/artifacts/echod-0.3.0",
  "size": 12849320,
  "sha256": "…",
	"manifest_url": "https://gateway.local/artifacts/echod-0.3.0/manifest.json",
	"signature_url": "https://gateway.local/artifacts/echod-0.3.0/manifest.sig"
}
```

All three resource URLs must be absolute HTTPS URLs. The artifact is fetched
over authenticated HTTPS, never streamed through this connection as control messages.

### `update.decision` (D→G)

```json
{ "deployment_id": "01J...", "decision": "rejected", "code": "busy", "detail": "voice turn active" }
```

`decision` is `accepted` or `rejected`. Accepted decisions have an empty code;
rejected decisions use a stable failure code.

### `update.progress` (D→G)

```json
{ "deployment_id": "01J...", "phase": "downloading", "percent": 42, "detail": "" }
```

### `update.failed` (D→G)

```json
{ "deployment_id": "01J...", "code": "digest_mismatch", "detail": "sha256 did not match manifest" }
```

`update.confirmed` contains `deployment_id`, `version`, and `build_id` after
reconnect. `update.cancelled` contains `deployment_id` and sanitized `detail`.
Failures and rejected decisions use only: `busy`, `invalid_offer`,
`ineligible`, `insufficient_space`, `download_failed`, `signature_invalid`,
`size_mismatch`, `digest_mismatch`, `stage_failed`, `install_failed`, or
`restart_failed`. A pre-replacement failure leaves the installed agent
untouched; a post-replacement failure may require ADB recovery.

### `error` (both)

```json
{ "code": "unauthorized", "message": "unknown device" }
```

## 5. Update phases

`update.progress.phase` and `hello.update_state` use:

```
idle · available · queued · downloading · verifying · staged
restarting · confirmed · failed · cancelled
```

`confirmed`, `failed`, and `cancelled` are terminal: nothing follows without a
new offer. `update.progress` only reports non-terminal active phases.

The device owns progression through its reported installation phases. The
gateway observes those reports, owns desired version and rollout policy, and
never treats a requested phase as proof that the device reached it.

## 6. Capabilities

Capabilities announced in `hello` are **the only** supported way to decide
whether a feature may be used with a device:

| Capability | Meaning |
|---|---|
| `wake.local` | runs the full local wake stack; every real satellite announces it |
| `wake.model_sync` | can receive wake models from the gateway |
| `audio.capture` | can stream command audio during a turn |
| `audio.playback` | can play gateway-supplied audio |
| `command.endpointing.local` | ends active-turn audio on the device |
| `update.single.v1` | supports single-agent atomic replacement |
| `led` | can display semantic LED states |
| `button` | can report action-button presses |
| `mute` | exposes a microphone mute control |

Deciding behavior by comparing agent versions is forbidden. Version minimums
exist only in release manifests as installation-safety constraints, never as
feature gates.

## 7. Boundary guarantees

These are properties of the protocol itself, not of any one implementation:

- There is **no message that carries idle microphone audio**. Audio exists only
  inside an explicit window opened after a turn started.
- There is **no message that asks a gateway to score a wake word**, and no
  gateway wake mode to enable. `turn.start` is always produced by the device.
- Wake VAD (device-local, gates whether a wake score is credible speech) and
  command endpointing (also device-local, ends an active command) are separate
  concerns with separate configuration. Only wake VAD appears in
  `hello.wake_config`; full desired settings are in `welcome.config`/`config`.
- The gateway owns desired agent version. The device must verify manifest
  compatibility, offer equality, exact size, SHA-256 and production signature
  before atomically replacing the installed agent.
- The protocol does not imply local recovery after replacement. A connected
  client can be offered an older release; a client that cannot reconnect
  requires ADB recovery.

## 8. Connection flow

```
device boots
  -> Magisk launcher starts /data/local/bin/echod
  -> echod initializes mic + local wake VAD + wake engine
  -> resolve gateway: explicit url, then paired server_id, then mDNS browse
  -> connect and authenticate over wss
  -> hello
  <- welcome
  -> idle; the local wake stack keeps listening

wake accepted locally
  -> local LED/tone immediately
  -> turn.start(id=turn-id, trigger=wake, model, wake_score, vad_score)
  -> audio.start(id=turn-id), binary PCM, audio.stop(id=turn-id, reason=endpointed)
  <- state(thinking)
  <- play.start, binary PCM, play.stop
  -> back to idle
```

Architectural update flow (Task 3 will define its exact message schemas):

```text
gateway offers desired signed release
  -> device fetches and verifies manifest, signature and artifact
  -> device stages /data/local/bin/echod.part
  -> device atomically replaces /data/local/bin/echod
  -> controlled exit; Magisk launcher restarts echod
  -> new echod reconnects and reports version/build

if replacement cannot reconnect
  -> gateway records failure and stops rollout according to policy
  -> operator restores a known-good signed release with ADB
```
