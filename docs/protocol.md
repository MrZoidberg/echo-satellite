# Echo Satellite Protocol

Protocol version: **1**

This document is the wire contract between a device agent (`echod`, or `dotsim`
standing in for it) and the gateway. It tracks [`internal/protocol`](../internal/protocol)
and **must be updated in the same change as any wire change there**.

`docs/DESIGN.md` §8 remains authoritative for why the protocol looks like this.
This document is authoritative for what is actually on the wire.

## 1. Transport and framing

- One long-lived **outbound** secure WebSocket per device (`wss`). The device
  always dials the gateway; the gateway never dials a device.
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

A binary frame received outside a window is a protocol error and must be
answered with `error` and ignored, never buffered "just in case".

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
| `id` | string | optional correlation id; a reply echoes the id it answers |
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
| `hello` | D→G | defined | identity, versions, capabilities, wake summary, update state |
| `welcome` | G→D | defined | server identity and gateway-managed device config |
| `config` | G→D | reserved | configuration update outside the handshake |
| `state` | both | defined | semantic device state (LED ring, listening, thinking) |
| `health` | D→G | reserved | periodic device health report |
| `log` | D→G | reserved | forwarded device log records |
| `turn.start` | D→G | defined | opens a voice turn; **always produced by the device** |
| `turn.cancel` | both | reserved | abandons the current turn |
| `wake.models` | both | reserved | wake model inventory and synchronization |
| `wake.status` | D→G | reserved | device-local wake stack status |
| `audio.start` | D→G | defined | opens the command audio window |
| `audio.stop` | D→G | defined | closes the command audio window |
| `play.start` | G→D | defined | opens the playback window |
| `play.stop` | G→D | defined | closes the playback window |
| `update.offer` | G→D | defined | offers a release, with an authenticated artifact URL |
| `update.accept` | D→G | reserved | device accepts an offer |
| `update.reject` | D→G | reserved | device declines an offer, with a reason |
| `update.progress` | D→G | defined | current update phase and progress |
| `update.staged` | D→G | reserved | verified and promoted into the inactive slot |
| `update.restarting` | D→G | reserved | about to restart into the new slot |
| `update.trial` | D→G | reserved | running on trial, not yet committed |
| `update.confirmed` | D→G | reserved | trial passed, slot committed |
| `update.rolled_back` | D→G | reserved | trial failed, previous slot restored |
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
  "supervisor_version": "1",
  "protocol": 1,
  "capabilities": ["audio.capture", "audio.playback", "update.ab", "wake.local"],
  "wake_config": {
    "engine": "openwakeword",
    "models": ["okay_nabu"],
    "wake_threshold": 0.6,
    "vad_threshold": 0.5,
    "pre_roll_ms": 500
  },
  "update_state": "trial"
}
```

`wake_config` is reported for observability. The gateway cannot change the local
wake stack by replying with a different summary, and it never scores wake audio.

`update_state` lets the gateway learn immediately that this device is running a
trial slot, before anything else happens on the connection.

### `welcome` (G→D)

```json
{ "server_id": "home-gateway", "protocol": 1, "config": {} }
```

`config` is gateway-managed device configuration. Its schema is owned by the
gateway configuration layer and is opaque to the protocol.

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

### `state` (both)

```json
{ "state": "thinking", "detail": "asking assistant" }
```

States, in display order: `idle`, `listening`, `thinking`, `speaking`, `muted`,
`offline`, `error`, `updating`, `update_trial`. `muted` distinguishes the local
privacy state from a fault, `offline` indicates loss of gateway connectivity,
and `update_trial` identifies an uncommitted A/B slot trial.

### `update.offer` (G→D)

```json
{
  "version": "0.3.0",
  "build_id": "git-abc123",
  "artifact_url": "https://gateway.local/artifacts/echod-0.3.0",
  "size": 12849320,
  "sha256": "…",
  "manifest_url": "https://gateway.local/artifacts/echod-0.3.0/manifest.json"
}
```

The artifact is fetched over authenticated HTTPS, never streamed through this
connection as control messages.

### `update.progress` (D→G)

```json
{ "phase": "downloading", "percent": 42, "detail": "" }
```

### `update.failed` (D→G)

```json
{ "code": "digest_mismatch", "message": "sha256 did not match manifest" }
```

A failed update does not mean an unhealthy device: the device keeps running its
previous slot.

### `error` (both)

```json
{ "code": "unauthorized", "message": "unknown device" }
```

## 5. Update phases

`update.progress.phase` and `hello.update_state` use exactly these values
(`docs/DESIGN.md` §10.7):

```
idle · available · queued · downloading · verifying · staged
restarting · trial · confirmed · failed · rolled_back · cancelled
```

`confirmed`, `failed`, `rolled_back` and `cancelled` are terminal: nothing
follows without a new offer.

The device owns this state machine. The gateway observes it and never drives a
device past a phase the device has reported.

## 6. Capabilities

Capabilities announced in `hello` are **the only** supported way to decide
whether a feature may be used with a device:

| Capability | Meaning |
|---|---|
| `wake.local` | runs the full local wake stack; every real satellite announces it |
| `wake.model_sync` | can receive wake models from the gateway |
| `audio.capture` | can stream command audio during a turn |
| `audio.playback` | can play gateway-supplied audio |
| `update.ab` | supports application-level A/B agent updates |
| `led` | can display semantic LED states |
| `button` | can report action-button presses |
| `mute` | exposes a microphone mute control |

Deciding behavior by comparing agent versions is forbidden. Version minimums
exist only in release manifests (`protocol_min`, `protocol_max`,
`supervisor_min`) as installation-safety constraints, never as feature gates.

## 7. Boundary guarantees

These are properties of the protocol itself, not of any one implementation:

- There is **no message that carries idle microphone audio**. Audio exists only
  inside an explicit window opened after a turn started.
- There is **no message that asks a gateway to score a wake word**, and no
  gateway wake mode to enable. `turn.start` is always produced by the device.
- Wake VAD (device-local, gates whether a wake score is credible speech) and
  command endpointing (gateway-side for v0.1, decides when a command ended) are
  separate concerns with separate configuration. Only wake VAD appears in
  `hello.wake_config`.

## 8. Connection flow

```
device boots
  -> supervisor selects the active agent slot
  -> echod initializes mic + local wake VAD + wake engine
  -> resolve gateway: explicit url, then paired server_id, then mDNS browse
  -> connect and authenticate over wss
  -> hello
  <- welcome
  -> if on trial and local health checks passed: report confirmed
  -> idle; the local wake stack keeps listening

wake accepted locally
  -> local LED/tone immediately
  -> turn.start(trigger=wake, model, wake_score, vad_score)
  -> audio.start, binary PCM, audio.stop
  <- state(thinking)
  <- play.start, binary PCM, play.stop
  -> back to idle
```
