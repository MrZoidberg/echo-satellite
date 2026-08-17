# Echo Satellite — High-Level Design and Implementation Plan

## 1. Purpose

Echo Satellite repurposes a rooted Amazon Echo Dot Gen 2 as a self-hosted network voice terminal.

The Echo Dot provides the hardware-facing capabilities — microphone capture, **local wake-word detection**, speaker playback, LEDs, buttons, volume and mute — while a separate gateway performs speech recognition, assistant orchestration, conversation management and speech generation.

Hermes is the first assistant backend, not part of the device protocol. The same satellite should be able to work later with OpenClaw, a Raspberry Pi-hosted assistant, another local agent, or a cloud service without rewriting the device agent.

This document defines the initial architecture and implementation direction. Details that depend on real Echo Dot hardware should be validated experimentally rather than treated as assumptions.

---

## 2. Goals

### Core goals

- Run a small Go daemon on a rooted Echo Dot Gen 2.
- Perform wake-word detection locally on the Echo Dot.
- Reuse/adapt the proven local wake-word implementation from existing Echo Dot projects where practical.
- Stream microphone audio to the gateway only for an active voice turn, rather than continuously for wake detection.
- Play gateway-generated audio through the Echo speaker.
- Expose LEDs, buttons, mute and volume as device capabilities.
- Support automatic local gateway discovery using mDNS, with explicit/static configuration as a fallback and override.
- Support English and Ukrainian speech recognition.
- Integrate first with Hermes while keeping the assistant backend replaceable.
- Preserve conversation context across multiple voice turns.
- Allow creating a new conversation and switching back to previous conversations.
- Provide a simple web management UI.
- Provide a host-side CLI for installation, configuration, diagnostics and upgrades.
- Make Windows + WSL2 development fast and practical.
- Make most gateway work testable without physical Echo hardware.

### Non-goals for the first release

- Recreating every Alexa feature.
- Gateway-side wake-word detection.
- Continuously streaming microphone audio to the gateway solely for wake detection.
- Running STT or the LLM directly on the Echo Dot.
- Supporting arbitrary Echo generations before Gen 2 is stable.
- Multi-room arbitration in the first vertical slice.
- Perfect far-field tuning before the basic end-to-end voice loop works.
- Building a general home-automation protocol.

---

## 3. Design Principles

### 3.1 The Echo is a voice satellite, not an assistant

The Echo device should know nothing about Hermes, OpenClaw, LLM APIs, conversation storage or speech providers.

Its responsibilities are limited to:

- hardware initialization;
- continuous local microphone capture needed by the wake engine;
- **local wake-word inference**;
- optional beamforming / AEC / preprocessing;
- short audio buffering around a wake trigger;
- streaming command audio for an active turn;
- audio playback;
- LED and button control;
- mute and volume;
- local gateway discovery;
- configuration;
- diagnostics;
- maintaining a secure connection to the gateway.

### 3.2 Wake detection is always local

Wake-word detection is a device capability and is not implemented by the gateway.

The gateway may configure the active model and sensitivity and may receive wake diagnostics, but it does not receive a continuous microphone stream and does not score wake words itself.

A manual Action-button / push-to-talk trigger may start a turn without a wake word for development, accessibility and recovery. This is an alternate **trigger**, not an alternate wake-word engine.

### 3.3 The gateway owns assistant orchestration

The gateway is responsible for:

- advertising its local endpoint using mDNS;
- device registration and configuration;
- turn lifecycle after a local wake/button trigger;
- VAD / endpointing for the command audio initially;
- speech-to-text provider selection;
- assistant backend selection;
- conversation management;
- text-to-speech provider selection;
- turn state and observability;
- management API and UI.

### 3.4 Stable internal contracts, replaceable providers

The system should define its own interfaces for:

- satellite protocol;
- local discovery;
- wake-model configuration;
- STT;
- TTS;
- assistant backend;
- conversation storage.

Hermes-specific concepts stay inside a Hermes adapter.

### 3.5 Capability negotiation instead of firmware-version logic

Each device announces what it supports. Example:

```json
{
  "type": "hello",
  "protocol": 1,
  "device_id": "G0K0XXXXXXXX",
  "version": "0.1.0",
  "capabilities": [
    "mic",
    "speaker",
    "led",
    "buttons",
    "mute",
    "volume",
    "wake.local.openwakeword",
    "wake.local.microwakeword"
  ]
}
```

The gateway enables behaviour based on capabilities rather than hard-coded firmware-version comparisons.

### 3.6 Hardware-independent development where possible

The protocol and gateway should be testable against a simulated device (`dotsim`) that feeds WAV files instead of microphone hardware and writes playback audio to disk instead of a speaker.

The simulator must also be able to generate local-wake events so gateway development does not depend on running a wake model.

### 3.7 Zero-configuration local discovery, explicit configuration when needed

A newly installed satellite should normally be able to find a gateway on the same local network without requiring an IP address.

mDNS is the default local discovery mechanism. It only locates candidate gateway endpoints; it is not authentication.

An explicitly configured gateway URL takes precedence over mDNS for VLANs, routed networks, VPNs, multiple gateways, and networks where multicast DNS is unavailable.

---

## 4. Reference Projects

### EchoLocal

Repository: `ygelfand/echolocal`

Primary implementation reference for:

- Echo Dot Gen 2 hardware access;
- pure-Go ALSA handling;
- microphone and speaker paths;
- LEDs and buttons;
- AEC / beamforming work;
- **local openWakeWord and microWakeWord support**;
- TFLite model loading;
- wake-model metadata and sensitivity configuration;
- host-side installer UX;
- ADB-based development workflow.

EchoLocal already separates wake-model configuration from the detection engine and supports both openWakeWord and microWakeWord model kinds. Echo Satellite should reuse/adapt these low-level ideas rather than inventing a second wake stack.

### EchoMuse

Repository: `wilbowes/EchoMuse`

Useful reference for:

- device/controller separation;
- WebSocket protocol ideas;
- mDNS controller discovery patterns;
- device registration;
- per-device configuration;
- capability negotiation;
- management dashboard patterns;
- operational lessons from real Echo hardware.

EchoMuse's controller-side wake architecture is **not** the target architecture for Echo Satellite. It remains useful for the surrounding device/controller design.

---

## 5. High-Level Architecture

```text
                         +-----------------------+
                         |     Management UI     |
                         | devices / voice /     |
                         | assistants / convos   |
                         +-----------+-----------+
                                     |
                                     | HTTP/WS
                                     v
+------------------------+   WSS   +---------------------------+
| Echo Dot Gen 2         |<------->|       Voice Gateway       |
|                        |         |                           |
| echod (Go)             |         | mDNS advertisement        |
|------------------------|         | device manager            |
| mDNS discovery         |         | turn state machine        |
| mic capture            |         | VAD / endpointing         |
| local wake engine      |         | conversation manager      |
| short pre-roll buffer  |         | STT / TTS routing         |
| speaker playback       |         | assistant routing         |
| LEDs/buttons           |         | SQLite                    |
| mute/volume            |         +------+------+-------------+
+------------------------+                |      |
                                         |      |
                          +--------------+      +----------------+
                          v                                      v
                +-------------------+                  +-------------------+
                | Speech Providers  |                  | Assistant Backend |
                |-------------------|                  |-------------------|
                | local Whisper     |                  | Hermes            |
                | Hermes STT/TTS    |                  | OpenClaw (future) |
                | future providers  |                  | other (future)    |
                +-------------------+                  +-------------------+
```

Normal turn flow:

```text
local mic -> local wake engine
          -> wake detected
          -> local feedback immediately
          -> turn.start + command audio over WSS
          -> gateway VAD/endpointing
          -> STT
          -> assistant
          -> TTS
          -> Echo speaker
```

No gateway-side wake detector exists in this architecture.

---

## 6. Proposed Repository Structure

```text
echo-satellite/
├── cmd/
│   ├── echod/                 # daemon running on Echo Dot
│   ├── echoctl/               # provisioning/diagnostic CLI
│   ├── gateway/               # central API + orchestration service
│   └── dotsim/                # simulated Echo Dot
│
├── internal/
│   ├── device/
│   │   ├── audio/
│   │   ├── wake/              # local wake engine + model management
│   │   ├── buttons/
│   │   ├── led/
│   │   ├── mixer/
│   │   └── system/
│   ├── discovery/             # mDNS advertisement/discovery
│   ├── protocol/              # shared Dot <-> Gateway messages
│   ├── gateway/
│   │   ├── devices/
│   │   ├── turns/
│   │   ├── conversations/
│   │   └── config/
│   ├── assistant/
│   │   ├── backend.go
│   │   ├── hermes/
│   │   ├── mock/
│   │   └── openclaw/          # future
│   ├── speech/
│   │   ├── stt.go
│   │   ├── tts.go
│   │   ├── hermes/
│   │   └── local/
│   └── store/
│
├── services/
│   └── speech-worker/         # optional Python ML worker
├── web/                       # management SPA
├── deploy/
│   └── docker-compose.yml
├── docs/
│   ├── DESIGN.md
│   └── protocol.md
├── testdata/
│   ├── audio/
│   └── wake/
└── README.md
```

Keeping the first versions in one repository simplifies coordinated protocol changes and releases.

---

## 7. Device Agent (`echod`)

### Responsibilities

`echod` runs as a supervised service on the rooted Echo Dot.

Initial responsibilities:

- derive stable device identity from the device serial;
- initialize supported hardware;
- discover a local gateway using mDNS when no explicit gateway is configured;
- establish/re-establish the gateway WebSocket;
- advertise capabilities;
- continuously feed local microphone frames into the local wake engine while idle;
- maintain a small local audio ring buffer so speech immediately after the wake phrase is not clipped;
- start a turn after a local wake or Action-button trigger;
- stream command PCM only during the active listening phase;
- receive and play response PCM;
- report button and mute events;
- apply volume/config changes;
- render semantic LED states;
- report health and logs.

### Audio strategy

Start with the simplest known-good microphone path and prove wake detection plus the complete voice loop before optimizing far-field behaviour.

The same local capture path should feed both the wake engine and turn streaming so switching from idle detection to active listening does not require reopening/reconfiguring ALSA.

Beamforming, AEC and preprocessing can be adopted from the reference projects as needed, but the wake engine remains on-device regardless of which DSP stages are enabled.

### Device state

Suggested states:

```text
idle         # local wake engine active
listening    # wake/button triggered; command audio streaming
thinking
speaking
muted        # wake engine must not trigger while hardware/software muted
offline
error
updating
```

The device should provide wake tone/LED feedback immediately after local detection instead of waiting for gateway round-trip latency.

---

## 8. Satellite Protocol and Local Discovery

### Transport

Initial transport:

- secure WebSocket (`wss`);
- one long-lived outbound connection per device;
- JSON text frames for control/events;
- binary WebSocket frames for PCM audio.

The WSS connection may remain idle while the local wake engine listens. Microphone frames are not forwarded until a turn starts.

### mDNS gateway discovery

The gateway advertises a DNS-SD service:

```text
_echo-satellite._tcp.local.
```

Conceptual record:

```text
Instance: echo-satellite-<server-id>._echo-satellite._tcp.local.
Host:     echo-gateway.local.
Port:     8770
TXT:
  protocol=1
  server_id=<stable-server-id>
  tls=1
  path=/device
```

TXT records contain discovery metadata only. No credentials or secrets are advertised.

Satellite resolution order:

```text
1. Explicitly configured gateway URL
2. Previously paired/discovered gateway, if reachable
3. Browse _echo-satellite._tcp.local. using mDNS
4. Select a compatible/preferred gateway
5. Connect and authenticate over WSS
6. Retry with backoff if unavailable
```

A previously paired `server_id` is preferred even when its IP address changes.

### Optional device advertisement

For provisioning/diagnostics, `echod` may optionally advertise:

```text
_echo-satellite-device._tcp.local.
```

Only non-sensitive metadata such as device ID, version and pairing state should be exposed.

### Network limitations and fallback

mDNS normally stays within a multicast domain. VLANs, multicast filtering, routed segments, VPNs or container isolation may require an mDNS reflector/repeater or an explicit gateway URL.

Example configuration:

```yaml
gateway:
  discovery: mdns
  url: ""               # when set, overrides discovery
  preferred_server_id: ""
```

Explicit configuration:

```yaml
gateway:
  discovery: disabled
  url: "wss://192.168.10.20:8770/device"
  preferred_server_id: "home-gateway"
```

### Connection and turn flow

```text
Device boots
  -> initialize mic + local wake engine
  -> load explicit/previous gateway configuration
  -> if necessary discover gateway over mDNS
  -> connect/authenticate over WSS
  -> hello(device id, version, capabilities, installed wake models)
  <- welcome/config
  -> idle; local wake inference continues

Wake detected locally
  -> immediate local LED/tone
  -> turn.start(trigger=wake, model, score)
  -> audio.start
  -> binary PCM command audio
  <- state(thinking)
  <- play.start + binary response audio
  -> playback complete
  -> return to idle/local wake inference
```

### Turn-start example

```json
{
  "type": "turn.start",
  "turn_id": "01J...",
  "trigger": {
    "type": "wake",
    "model_id": "okay_nabu",
    "score": 0.87
  }
}
```

Manual Action-button trigger:

```json
{
  "type": "turn.start",
  "turn_id": "01J...",
  "trigger": {
    "type": "button"
  }
}
```

Candidate message families:

```text
hello / welcome
config
state
health
log

turn.start / turn.cancel
wake.models / wake.status

audio.start / audio.stop
play.start / play.stop

button
mute
volume

ping / pong
error
```

Exact framing belongs in `docs/protocol.md` once hardware formats and latency behaviour are validated.

---

## 9. Gateway

The gateway should initially be written in Go.

### Major modules

#### Discovery Service

Owns:

- stable `server_id`;
- `_echo-satellite._tcp.local.` advertisement;
- advertised protocol/WSS endpoint metadata;
- service lifecycle across network-interface changes;
- optional diagnostic browsing of satellite advertisements.

#### Device Manager

Maintains:

- connected devices;
- capabilities;
- installed/active wake-model metadata reported by devices;
- current state;
- last-seen time;
- configuration revisions;
- commands and active audio streams.

#### Turn Manager

Owns the server-side turn state after a device trigger:

```text
IDLE
  -> LISTENING
  -> THINKING
  -> SPEAKING
  -> IDLE
```

The gateway never transitions `IDLE -> LISTENING` because of wake inference of its own; it does so only after `turn.start` from a device or simulator.

#### Speech Router

Selects STT and TTS providers through internal interfaces.

#### Assistant Router

Selects an assistant adapter. Hermes is the first implementation.

#### Conversation Manager

Owns local conversation identity and maps it to backend-specific sessions/threads.

---

## 10. Speech Recognition and Generation

### STT

Conceptual interface:

```go
type STT interface {
    Transcribe(ctx context.Context, audio Audio, opts STTOptions) (Transcript, error)
}

type Transcript struct {
    Text       string
    Language   string
    Confidence float64
}
```

Initial candidates:

- local Whisper/faster-whisper worker;
- Hermes-provided STT if available.

English and Ukrainian must both be supported. Auto language detection is the default, with optional language hints.

If the selected STT provider needs whole utterances, the gateway buffers the active turn until endpointing. Streaming STT can be added later without changing the satellite trigger model.

### Python speech worker

If local Whisper is used:

```text
Gateway (Go)
   -> speech-worker HTTP/gRPC
       -> faster-whisper
```

### TTS

Conceptual interface:

```go
type TTS interface {
    Synthesize(ctx context.Context, text string, opts TTSOptions) (AudioStream, error)
}
```

TTS may initially come from Hermes or a local provider.

---

## 11. Assistant Backend Abstraction

Hermes is implemented behind an internal backend interface.

```go
type AssistantBackend interface {
    Capabilities(ctx context.Context) (BackendCapabilities, error)
    CreateConversation(ctx context.Context, opts ConversationOptions) (BackendConversation, error)
    Send(ctx context.Context, conversation BackendConversation, input UserInput) (<-chan BackendEvent, error)
}
```

Possible backend events:

```text
response.started
text.delta
text.final
audio.chunk
tool.started
tool.finished
response.finished
error
```

Initial adapters:

```text
assistant/hermes
assistant/mock
```

Future adapters:

```text
assistant/openclaw
assistant/http
```

The mock backend should prove the complete voice loop before Hermes integration.

---

## 12. Conversation Model

Conversation identity belongs to Echo Satellite, not Hermes.

Suggested entities:

### Conversation

```text
id
name/title
created_at
updated_at
last_used_at
```

### Backend Binding

```text
conversation_id
backend
backend_conversation_id
backend_metadata
```

### Turn

```text
id
conversation_id
device_id
trigger_type
wake_model_id
started_at
finished_at
language
transcript
assistant_text
backend
status
latency metadata
```

### Device

```text
id
name
active_conversation_id
version
last_seen
config
```

Infrastructure-level utterances may be intercepted before the assistant, for example:

```text
"New conversation"
"Нова розмова"

"Switch to travel"
"Переключись на подорож"
```

Conversation history is not owned by a physical Dot, enabling future cross-device continuation.

---

## 13. Wake Word

### Architecture decision

**Wake-word detection is local-only. Gateway-side wake detection is intentionally unsupported.**

The initial implementation should reuse/adapt EchoLocal's current local wake stack instead of developing a separate detector.

The reference implementation already supports two useful model families:

- **openWakeWord** classifiers using TFLite models and a shared on-device feature/embedding pipeline;
- **microWakeWord** TFLite models using the existing microWakeWord runtime.

The model-loading layer should preserve the useful reference behaviour:

- discover/install `.tflite` wake models;
- support optional sidecar metadata describing phrase and trained languages;
- identify which local engine should run a model;
- expose a configurable sensitivity/threshold;
- allow models to be selected by stable ID;
- keep wake configuration independent of Hermes or any other assistant backend.

### v0.1 behaviour

Start with one active wake model per device to keep behaviour and CPU usage predictable. The internal API should not prevent multiple simultaneously active models later.

The local detector runs continuously while the device is `idle` and not muted.

```text
mic capture
  -> optional DSP/beamforming
  -> local wake engine
  -> threshold reached
  -> immediate local wake feedback
  -> create turn_id
  -> send turn.start
  -> stream command audio
```

The openWakeWord-compatible path should preserve the reference project's 16 kHz input expectations rather than introducing an unnecessary resampling/model format of our own.

### Pre-roll and speech clipping

The device should maintain a short PCM ring buffer while idle. When a wake word fires, a small configurable tail of pre-trigger audio can be included at the beginning of the active turn so the first word after the wake phrase is not clipped.

The initial value should be conservative and tuned from recordings. If the pre-roll frequently contains the wake phrase itself, reduce it or strip the configured wake phrase from STT output rather than moving wake inference to the gateway.

### Endpointing

Wake detection and endpointing are separate concerns.

For v0.1:

- wake detection: **device only**;
- command VAD/endpointing: gateway;
- STT: gateway-side provider.

Local endpointing can be explored later if it gives useful latency/bandwidth benefits, but it is not required to keep wake detection local.

### Action button

The Action button should start the same voice-turn pipeline without running a synthetic wake detector:

```text
button press
  -> turn.start(trigger=button)
  -> command audio stream
```

This is useful during development and when wake models are misconfigured.

### Configuration

Example device configuration:

```yaml
wake:
  enabled: true
  model: okay_nabu
  threshold: 0.80
  preroll_ms: 250
```

There is deliberately no `gateway` wake mode.

### Wake model distribution

For the first implementation, `echoctl` may install/update wake-model files on the device while the gateway manages selection and sensitivity.

Later, authenticated model delivery through the gateway can be added if useful. Model binaries must not be fetched from arbitrary unauthenticated network locations by the Dot.

### Diagnostics

Useful wake diagnostics:

```text
active model id
model kind (openWakeWord / microWakeWord)
trained language metadata
threshold
last wake score
wake count
false-trigger test recordings (opt-in)
inference timing / CPU usage
```

Raw continuous microphone audio must not be uploaded merely for wake scoring.

---

## 14. Management UI

The first UI should remain small.

### Devices

- online/offline state;
- version and capabilities;
- discovered/paired gateway state;
- current voice state;
- volume/mute;
- microphone test;
- speaker test;
- recent logs.

### Voice / Wake

- installed local wake models reported by each device;
- active model;
- model kind and language metadata;
- wake sensitivity/threshold;
- wake enable/disable;
- pre-roll setting;
- STT provider;
- STT language hints;
- TTS provider;
- basic audio tuning.

The UI must not offer a gateway-side wake mode.

### Assistants

- configured backend;
- endpoint/configuration;
- health check;
- backend capabilities.

### Conversations

- active conversation;
- recent conversations;
- create new;
- rename;
- switch/resume.

### Network / Discovery

- gateway `server_id`;
- advertised mDNS service name;
- current host/port;
- connected/discovered satellites;
- discovery diagnostics;
- explicit gateway override guidance.

A React/Vite SPA can be embedded in the production Go gateway binary.

---

## 15. `echoctl` Provisioning and Diagnostics CLI

Suggested commands:

```text
echoctl devices
echoctl discover
echoctl inspect

echoctl install
echoctl uninstall

echoctl pair
echoctl config

echoctl status
echoctl logs
echoctl restart

echoctl mic record
echoctl wake list
echoctl wake install <model>
echoctl wake test <wav-or-live>
echoctl speaker test
echoctl led test
echoctl buttons test

echoctl update        # later
```

`echoctl wake test` should make it possible to evaluate local models and thresholds independently from STT/Hermes.

### Install flow

The expected starting point is an already rooted/Magisk-enabled Dot. Normal installation should avoid reflashing the boot image.

```text
find ADB device
  -> verify expected hardware/root
  -> inspect existing installation
  -> back up changed state
  -> push echod
  -> install default wake model/assets
  -> push config/credentials
  -> configure supervised startup
  -> handle conflicting Alexa services as required
  -> start echod
  -> discover/configure gateway
  -> verify gateway connectivity
  -> microphone test
  -> local wake test
  -> speaker test
```

Rooting/flashing remains separate from normal application installation.

---

## 16. Security and Privacy

Initial model:

- WSS for device/gateway traffic;
- mDNS for endpoint discovery only;
- serial for identity, not authentication;
- stable gateway `server_id` for preference/pairing, not as a secret;
- per-device random credential/token;
- gateway stores Hermes/assistant/speech secrets;
- Echo stores only device credentials and local wake models/configuration;
- management API requires authentication before non-local deployment.

Local wake detection also gives a useful privacy property: while idle, microphone audio required for wake recognition remains on the device rather than being continuously sent to the gateway.

Future improvement: per-device client certificates / mTLS.

A browser-exposed root shell is not part of v1; ADB is sufficient for development and recovery.

---

## 17. Persistence

SQLite is sufficient initially.

Expected gateway data:

- devices;
- per-device configuration;
- reported wake-model inventory and selected model;
- gateway/server identity;
- preferred/paired gateway metadata;
- conversations;
- backend bindings;
- turns;
- selected diagnostics;
- system configuration.

Raw microphone audio is not stored by default. Development/debug mode may optionally persist selected turns or explicit wake-test recordings.

---

## 18. Observability and Record/Replay

Every turn gets a `turn_id` propagated through the pipeline.

Useful fields:

```text
device_id
connection_id
turn_id
conversation_id
trigger_type
wake_model_id
wake_score
audio_stream_id
backend
state
```

Development artifacts may optionally include:

```text
turn input WAV
processed WAV
transcript
assistant request/event log
TTS output
timing information
```

Wake testing should support explicitly captured fixtures so thresholds/models can be evaluated offline without adding gateway-side real-time wake detection.

---

## 19. Device Simulator (`dotsim`)

`dotsim` implements the same protocol as `echod` but replaces hardware with files/terminal events.

Example:

```text
dotsim
  --discover mdns
  --trigger wake
  --wake-model okay_nabu
  --mic testdata/audio/uk/question.wav
  --speaker-out ./response.wav
```

Capabilities to simulate:

- mDNS discovery;
- multiple gateways;
- registration;
- local wake event and score;
- button-triggered turns;
- microphone turn streams;
- speaker playback;
- reconnects;
- network failures;
- partial/older capability sets.

The simulator does not need to run a real wake model for ordinary gateway tests. Local wake-engine correctness is tested separately with device/unit/audio-fixture tests.

---

## 20. Windows Development Workflow

Recommended environment:

```text
Windows 11
  |
  +-- VS Code + Remote WSL
  |
  +-- WSL2 Ubuntu
  |     +-- Go
  |     +-- Python + uv
  |     +-- make
  |     +-- Docker CLI
  |     +-- golangci-lint
  |     +-- source checkout
  |
  +-- Docker Desktop / WSL backend
  |     +-- gateway
  |     +-- speech-worker
  |     +-- Hermes/test dependencies
  |
  +-- Windows Android Platform Tools
        +-- adb.exe -> Echo Dot
```

Prefer Windows `adb.exe` called from WSL for normal iteration:

```bash
export ADB=/mnt/c/Android/platform-tools/adb.exe
```

Keep the device binary pure Go where feasible:

```text
GOOS=linux
GOARCH=arm64
CGO_ENABLED=0
```

A fast loop:

```text
build echod
  -> adb push
  -> restart foreground/service
  -> tail logs
  -> run mic/wake fixture tests
```

mDNS must be tested from the actual Docker/WSL deployment because multicast visibility can differ by network mode. Explicit host/port remains the fallback.

---

## 21. Deployment

Initial gateway deployment: Docker Compose.

```text
gateway
speech-worker
Hermes integration/dependencies
persistent data volume
```

The gateway should also run as a native Go binary.

Deployment documentation must explain how mDNS advertisement reaches the physical LAN. Static gateway configuration remains the universal fallback.

Future target: Raspberry Pi / ARM64 host without architectural changes.

---

## 22. Implementation Milestones

### Milestone 0 — Repository and development foundation

- establish Go module/package layout;
- CI for format/test/vet/lint;
- create `echod`, `gateway`, `echoctl`, `dotsim` entrypoints;
- define protocol/discovery interfaces;
- add Windows/WSL docs.

### Milestone 1 — Hardware + local wake vertical slice

Prove locally on the Dot:

```text
mic -> WAV
mic -> reused/adapted local wake engine -> wake event
WAV/PCM -> speaker
LED/button access
```

Success criterion: the Dot can detect a selected local wake model repeatedly without any gateway wake processing.

### Milestone 2 — Discovery, protocol and simulator

- gateway mDNS advertisement;
- satellite mDNS browse/resolution;
- explicit gateway override;
- preferred `server_id`;
- registration/capabilities;
- reconnect/backoff;
- config push;
- structured logs;
- `turn.start` triggers;
- binary turn-audio framing;
- `dotsim` integration tests.

### Milestone 3 — Local-wake speech loop

```text
Echo local wake / simulator trigger
  -> turn audio
  -> gateway VAD/endpointing
  -> STT
  -> transcript
```

Validate English and Ukrainian utterances using replayable fixtures.

### Milestone 4 — Complete mock voice loop

```text
local wake
 -> STT
 -> MockBackend("You said: ...")
 -> TTS
 -> Echo speaker
```

### Milestone 5 — Hermes adapter

- implement `AssistantBackend` adapter;
- map local conversations to Hermes sessions;
- backend health/capabilities;
- secrets remain server-side.

### Milestone 6 — Persistent conversations

- SQLite schema;
- turn storage;
- create/switch/resume conversations;
- infrastructure voice commands.

### Milestone 7 — Management UI

- device list/status;
- discovery/network diagnostics;
- local wake model/sensitivity configuration;
- audio diagnostics;
- STT/TTS configuration;
- assistant configuration;
- conversation management.

### Milestone 8 — Productization

- stable bootstrap/install;
- wake-model update workflow;
- upgrades/rollback;
- stronger device authentication;
- Raspberry Pi/ARM64 validation;
- multi-Dot arbitration;
- optional local endpointing / barge-in improvements.

---

## 23. First Engineering Tasks

1. Create Go module/package skeleton and CI.
2. Port/adapt the minimum EchoLocal microphone path.
3. Port/adapt the EchoLocal local wake-model layer and one known working model.
4. Build `echoctl mic record` and `echoctl wake test` before gateway integration.
5. Verify local wake inference CPU, stability and false-trigger behaviour on the real Dot.
6. Define `_echo-satellite._tcp.local.` discovery record and implement gateway advertisement.
7. Implement minimal gateway WSS server and satellite discovery/connection.
8. Define `turn.start` + binary audio framing.
9. On local wake, stream command PCM to gateway and write it to WAV.
10. Implement gateway-to-Echo speaker playback and semantic LED state.
11. Implement `dotsim` with synthetic wake/button triggers.
12. Add VAD/endpointing and STT/TTS interfaces.
13. Add local Whisper integration.
14. Add mock assistant end-to-end loop.
15. Add Hermes only after hardware, local wake and transport are proven.

This sequence avoids debugging wake inference, audio transport, STT and Hermes simultaneously.

---

## 24. Architectural Questions to Validate on Hardware

- Which Echo microphone path/channel arrangement is best for both local wake inference and command capture?
- How directly can EchoLocal's current openWakeWord/microWakeWord code be reused while keeping Echo Satellite's dependencies and licensing clean?
- Which wake model should ship as the default?
- What thresholds give acceptable false-positive/false-negative behaviour in a real room?
- How much pre-roll prevents clipped command speech without polluting STT with the wake phrase?
- What is the CPU/memory cost of one versus multiple active local wake models?
- Whether beamforming should be reused from EchoLocal or initially bypassed.
- Whether AEC is required only for barge-in/full-duplex behaviour or earlier.
- Exact speaker format and best resampling location.
- Best supervised startup mechanism on the existing Magisk-rooted FireOS installation.
- Which mDNS implementation works reliably on FireOS without unnecessary native dependencies?
- Whether Docker/WSL mDNS advertisement is visible on the physical LAN in the preferred deployment.
- Whether Hermes should provide STT/TTS initially or only assistant reasoning.

Resolve these with focused diagnostics and recordings rather than hidden complexity inside the daemon.

---

## 25. Target v0.1 Stack

| Component | Initial choice |
|---|---|
| Echo daemon | Go |
| Echo hardware reference | EchoLocal |
| Wake detection | **Local on Echo only** |
| Wake engines | EchoLocal-derived openWakeWord + microWakeWord support |
| Wake models | local TFLite models |
| Manual trigger | Action button / simulator |
| Local discovery | mDNS / DNS-SD, static URL fallback |
| Gateway service | `_echo-satellite._tcp.local.` |
| Device transport | WSS |
| Control frames | JSON |
| Audio frames | binary PCM during active turns |
| Gateway | Go |
| Endpointing | gateway-side initially |
| Persistence | SQLite |
| Management UI | React + Vite, embedded in Go |
| Provisioning CLI | Go + Cobra |
| STT | local faster-whisper worker initially |
| Languages | English + Ukrainian |
| TTS | provider abstraction; Hermes or local |
| Assistant | Hermes adapter + mock adapter |
| Local dev | Windows + WSL2 |
| Device iteration | Windows ADB called from WSL |
| Integration testing | `dotsim` + WAV/wake fixtures |
| Deployment | Docker Compose |

---

## 26. Core Boundary

```text
Echo Dot
    |
    | local microphone -> local wake inference
    |                    (audio stays on device while idle)
    |
    | mDNS discovery (or static configuration)
    v
Voice Gateway endpoint
    |
    | Echo Satellite Protocol over WSS
    | turn audio only after local wake/button trigger
    v
Voice Gateway
    |
    +-- VAD / endpointing
    +-- STT Provider       -> Whisper / Hermes / other
    +-- Assistant Backend -> Hermes / OpenClaw / other
    +-- TTS Provider       -> Hermes / local / other
```

The key architectural rule is that **wake recognition belongs to the satellite**. The gateway begins processing only after the device has decided that a voice turn has started.
