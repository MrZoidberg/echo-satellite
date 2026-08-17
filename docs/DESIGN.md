# Echo Satellite — High-Level Design and Implementation Plan

## 1. Purpose

Echo Satellite repurposes a rooted Amazon Echo Dot Gen 2 as a self-hosted network voice terminal.

The Echo Dot should provide the hardware-facing capabilities — microphone capture, speaker playback, LEDs, buttons, volume and mute — while a separate gateway performs the changeable parts of the voice-assistant pipeline and integrates with an assistant backend such as Hermes.

The primary architectural goal is **backend independence**: Hermes is the first backend, not part of the device protocol. The same Echo Dot agent should be able to work later with OpenClaw, a Raspberry Pi-hosted assistant, another local agent, or a cloud service without being rewritten.

This document defines the initial architecture and a practical implementation sequence. It is intentionally high-level where real Echo Dot hardware testing is still required.

---

## 2. Goals

### Core goals

- Run a small Go daemon on a rooted Echo Dot Gen 2.
- Capture microphone audio and stream it to a self-hosted gateway.
- Play audio returned by the gateway through the Echo speaker.
- Expose LEDs, buttons, mute and volume as device capabilities.
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
- Running STT or the LLM directly on the Echo Dot.
- Supporting arbitrary Echo generations before Gen 2 is stable.
- Multi-room arbitration in the first vertical slice.
- Perfect far-field beamforming before basic end-to-end voice works.
- Building a general home-automation protocol.

---

## 3. Design Principles

### 3.1 The Echo is a voice satellite, not an assistant

The Echo device should know nothing about Hermes, OpenClaw, LLM APIs, conversation storage or speech providers.

Its responsibilities are limited to:

- hardware initialization;
- microphone capture;
- optional beamforming / AEC / local wake detection;
- audio playback;
- LED and button control;
- mute and volume;
- configuration;
- diagnostics;
- maintaining a secure connection to the gateway.

### 3.2 The gateway owns assistant orchestration

The gateway is responsible for:

- device registration and configuration;
- wake-word processing when done off-device;
- voice activity detection / endpointing;
- speech-to-text provider selection;
- assistant backend selection;
- conversation management;
- text-to-speech provider selection;
- turn state and observability;
- management API and UI.

### 3.3 Stable internal contracts, replaceable providers

The system should define its own interfaces for:

- satellite protocol;
- STT;
- TTS;
- assistant backend;
- conversation storage.

Hermes-specific concepts stay inside a Hermes adapter.

### 3.4 Capability negotiation instead of firmware-version logic

Each device announces what it supports, for example:

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
    "volume"
  ]
}
```

The gateway must enable behaviour based on capabilities rather than hard-coded firmware-version comparisons.

### 3.5 Hardware-independent development where possible

The protocol and gateway should be testable against a simulated device (`dotsim`) that feeds WAV files instead of microphone hardware and writes playback audio to disk instead of a speaker.

---

## 4. Reference Projects

Two existing projects are particularly useful references.

### EchoLocal

Repository: `ygelfand/echolocal`

Best source for:

- Echo Dot Gen 2 hardware access;
- pure-Go ALSA implementation;
- microphone handling;
- speaker handling;
- LEDs and buttons;
- AEC / beamforming experiments;
- local wake-word infrastructure;
- host-side installer UX;
- ADB-based development workflow.

The intent is to selectively reuse or adapt suitable MIT-licensed code while keeping Echo Satellite's architecture independent from Home Assistant / ESPHome.

### EchoMuse

Repository: `wilbowes/EchoMuse`

Best source for:

- device/controller separation;
- continuous audio streaming architecture;
- WebSocket protocol ideas;
- device registration;
- per-device configuration;
- capability negotiation;
- management dashboard patterns;
- operational lessons from real Echo hardware.

EchoMuse should primarily be treated as an architecture and implementation reference rather than the base repository.

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
+-------------------+       +---------------------------+
| Echo Dot Gen 2    | WSS   |       Voice Gateway       |
|                   |<----->|                           |
| echod (Go)        |       | device manager            |
|-------------------|       | turn state machine        |
| mic capture       |       | conversation manager      |
| speaker playback  |       | speech routing            |
| LEDs/buttons      |       | assistant routing         |
| mute/volume       |       | SQLite                    |
| optional DSP      |       +------+------+-------------+
+-------------------+              |      |
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

The Echo initiates the connection to the gateway. The gateway does not need to open inbound connections to devices.

---

## 6. Proposed Repository Structure

Initial monorepo:

```text
echo-satellite/
├── cmd/
│   ├── echod/                 # daemon running on Echo Dot
│   ├── echoctl/               # host provisioning/diagnostic CLI
│   ├── gateway/               # central API + orchestration service
│   └── dotsim/                # simulated Echo Dot
│
├── internal/
│   ├── device/
│   │   ├── audio/
│   │   ├── buttons/
│   │   ├── led/
│   │   ├── mixer/
│   │   └── system/
│   │
│   ├── protocol/              # shared Dot <-> Gateway messages
│   ├── gateway/
│   │   ├── devices/
│   │   ├── turns/
│   │   ├── conversations/
│   │   └── config/
│   │
│   ├── assistant/
│   │   ├── backend.go
│   │   ├── hermes/
│   │   ├── mock/
│   │   └── openclaw/          # future
│   │
│   ├── speech/
│   │   ├── stt.go
│   │   ├── tts.go
│   │   ├── hermes/
│   │   └── local/
│   │
│   └── store/
│
├── services/
│   └── speech-worker/         # optional Python ML worker
│
├── web/                       # management SPA
├── deploy/
│   └── docker-compose.yml
├── docs/
│   ├── DESIGN.md
│   └── protocol.md
├── testdata/
│   └── audio/
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
- establish/re-establish the gateway WebSocket;
- advertise capabilities;
- stream microphone PCM;
- receive and play PCM;
- report button and mute events;
- apply volume/config changes;
- render semantic LED states;
- report health and logs.

### Initial audio strategy

Start with the simplest known-good microphone output, ideally mono/omnidirectional, and prove the full pipeline before optimizing far-field capture.

Later options can include:

- beamforming;
- acoustic echo cancellation;
- local wake-word recognition;
- preprocessing / gain tuning.

These should remain capability-driven and configurable.

### Device state

Suggested user-visible states:

```text
idle
listening
thinking
speaking
muted
offline
error
updating
```

The gateway sends semantic states; the device decides how those states map to LED effects.

---

## 8. Satellite Protocol

### Transport

Initial transport:

- secure WebSocket (`wss`);
- one long-lived outbound connection per device;
- JSON text frames for control/events;
- binary WebSocket frames for PCM audio.

This keeps the initial protocol simple, ordered and easy to inspect.

### Connection flow

```text
Device boots
  -> discover/load gateway
  -> connect WSS
  -> hello(device id, version, capabilities)
  <- welcome(config revision, server information)
  <- config
  -> state/health
  -> continuous or requested audio
```

### Example messages

Device registration:

```json
{
  "type": "hello",
  "protocol": 1,
  "device_id": "G0K0XXXXXXXX",
  "version": "0.1.0",
  "capabilities": ["mic", "speaker", "led", "buttons"]
}
```

Audio stream metadata:

```json
{
  "type": "audio.start",
  "stream_id": "01J...",
  "format": {
    "encoding": "pcm_s16le",
    "sample_rate": 16000,
    "channels": 1
  }
}
```

Playback request:

```json
{
  "type": "play.start",
  "stream_id": "01J...",
  "format": {
    "encoding": "pcm_s16le",
    "sample_rate": 48000,
    "channels": 2
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

audio.start / audio.stop
play.start / play.stop

button
mute
volume

turn.start / turn.cancel

ping / pong
error
```

The exact framing and schemas should be documented in `docs/protocol.md` once the first hardware experiments confirm formats and latency requirements.

---

## 9. Gateway

The gateway should initially be written in Go.

Reasons:

- shared protocol models with the device;
- strong concurrency primitives;
- simple WebSocket/audio streaming;
- single static binary deployment;
- easy ARM64 deployment later;
- ability to embed the compiled management UI.

### Major modules

#### Device Manager

Maintains:

- connected devices;
- capabilities;
- current state;
- last-seen time;
- configuration revisions;
- commands and audio streams.

#### Turn Manager

Owns the voice-turn state machine:

```text
IDLE
  -> LISTENING
  -> THINKING
  -> SPEAKING
  -> IDLE
```

One logical owner should mutate turn state to avoid races between audio, backend events, user cancellation and playback completion.

#### Speech Router

Selects STT and TTS providers through internal interfaces.

#### Assistant Router

Selects an assistant adapter. Hermes is the first implementation.

#### Conversation Manager

Owns persistent local conversation identity and maps local conversations to backend-specific sessions/threads.

---

## 10. Speech Recognition and Generation

### STT interface

Conceptually:

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

Initial provider candidates:

- local Whisper/faster-whisper worker;
- Hermes-provided STT if available.

English and Ukrainian must both be supported. Auto language detection should be the initial default, with optional per-device or global language hints.

### Python speech worker

If local Whisper is used, keep ML concerns in a separate Python service instead of embedding Python into the gateway.

Example:

```text
Gateway (Go)
   -> speech-worker HTTP/gRPC
       -> faster-whisper
```

This keeps the gateway small and makes speech implementation independently replaceable.

### TTS interface

Conceptually:

```go
type TTS interface {
    Synthesize(ctx context.Context, text string, opts TTSOptions) (AudioStream, error)
}
```

Initial TTS can come from Hermes. Local/cloud alternatives can be added later without changing the device protocol.

---

## 11. Assistant Backend Abstraction

Hermes must be implemented behind an internal backend interface, not called directly throughout the gateway.

Conceptual interface:

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

The mock backend is important for proving the complete voice pipeline before Hermes integration is introduced.

---

## 12. Conversation Model

Conversation identity belongs to Echo Satellite, not to Hermes.

Suggested persistence model:

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

A local conversation can therefore map to different backend representations without changing its identity.

For backends without persistent sessions, the adapter may reconstruct context from locally stored turns.

### Voice conversation controls

Infrastructure-level utterances can be intercepted before the assistant, for example:

```text
"New conversation"
"Нова розмова"

"Switch to travel"
"Переключись на подорож"
```

The device's active conversation is only a pointer. Conversation history should not belong to a physical Dot, allowing future cross-device continuation.

---

## 13. Wake Word and Endpointing

### Initial recommendation

Keep wake-word detection and VAD/endpointing gateway-side for the first version.

Initial pipeline:

```text
Echo microphone
  -> gateway audio stream
  -> wake-word detector
  -> VAD / endpointing
  -> STT
  -> assistant
  -> TTS
  -> Echo speaker
```

Advantages:

- easier debugging;
- easier model replacement;
- easier tuning;
- fewer device builds;
- less CPU pressure on old hardware.

### Later modes

Make wake mode configurable:

```yaml
wake:
  mode: gateway   # gateway | device | push-to-talk
```

`push-to-talk` should exist early as a debugging mode, using the Echo action button to begin a turn.

---

## 14. Management UI

The first UI should be intentionally small.

### Devices

- online/offline state;
- version and capabilities;
- current voice state;
- volume/mute;
- microphone test;
- speaker test;
- recent logs;
- restart/update actions later.

### Voice

- wake mode/model;
- STT provider;
- language hints;
- TTS provider;
- basic audio tuning.

### Assistants

- configured backend;
- endpoint/configuration;
- health check;
- backend capability display.

### Conversations

- active conversation;
- recent conversations;
- create new;
- rename;
- switch/resume;
- delete/archive later.

A React/Vite SPA is acceptable, with production assets embedded in the Go gateway binary.

---

## 15. `echoctl` Provisioning and Diagnostics CLI

The host CLI should work on Windows and Linux.

Suggested commands:

```text
echoctl devices
echoctl inspect

echoctl install
echoctl uninstall

echoctl pair
echoctl config

echoctl status
echoctl logs
echoctl restart

echoctl mic record
echoctl speaker test
echoctl led test
echoctl buttons test

echoctl update        # later
```

### Install flow

Because the expected starting point is an already rooted/Magisk-enabled Dot, normal installation should avoid reflashing the boot image.

Suggested flow:

```text
find ADB device
  -> verify expected hardware
  -> verify root
  -> inspect existing installation
  -> back up changed files/state
  -> push echod
  -> push device config/credentials
  -> install or configure supervised startup
  -> disable/avoid conflicting services as required
  -> start echod
  -> verify gateway connectivity
  -> microphone health test
  -> speaker health test
```

Destructive rooting/flashing procedures should remain separate from normal application installation.

---

## 16. Security

Initial model:

- WSS for device/gateway traffic;
- stable serial for identity, but not authentication;
- per-device random credential/token;
- gateway stores Hermes/assistant/speech secrets;
- Echo stores only its own device credential;
- management API requires authentication before non-local deployment.

Future improvement:

- per-device client certificates / mTLS.

A browser-exposed root shell should not be part of v1. ADB is sufficient for development and recovery.

---

## 17. Persistence

SQLite is sufficient for the first releases.

Expected data:

- devices;
- per-device config;
- conversations;
- backend bindings;
- turns;
- selected diagnostic metadata;
- system configuration.

Schema migrations must be ordered and append-only once released.

Do not store raw microphone audio by default. Development/debug mode may optionally persist selected turns.

---

## 18. Observability and Record/Replay

Every turn should have a generated `turn_id` used consistently through the pipeline.

Useful correlation fields:

```text
device_id
connection_id
turn_id
conversation_id
audio_stream_id
backend
state
```

In development mode, optionally persist:

```text
raw/input audio
processed audio
transcript
assistant request metadata
assistant events
TTS output
timing information
```

This enables repeated debugging of STT and assistant behaviour without repeatedly speaking the same phrase into the physical device.

---

## 19. Device Simulator (`dotsim`)

Build `dotsim` early.

It should implement the same gateway protocol as `echod` but replace hardware with files/terminal events.

Example:

```text
dotsim
  --gateway ws://localhost:8080/device
  --mic testdata/audio/uk/question.wav
  --speaker-out ./response.wav
```

Capabilities to simulate:

- registration;
- microphone streams;
- speaker playback;
- button events;
- reconnects;
- network failures;
- partial/older capability sets;
- device logs and state.

The simulator should be used for gateway integration tests and local development.

---

## 20. Windows Development Workflow

Recommended environment:

```text
Windows 11
  |
  +-- VS Code
  |     +-- Remote WSL
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
  |     +-- Hermes / test dependencies
  |
  +-- Windows Android Platform Tools
        +-- adb.exe -> Echo Dot
```

Prefer using Windows `adb.exe` from WSL rather than requiring USB passthrough for normal iteration.

Example configuration:

```bash
export ADB=/mnt/c/Android/platform-tools/adb.exe
```

The Go build should keep the Echo binary pure Go if feasible:

```text
GOOS=linux
GOARCH=arm64
CGO_ENABLED=0
```

A fast device loop should look like:

```text
build echod
  -> adb push /data/local/tmp/echod
  -> run/restart
  -> tail logs
```

Use structured logging and diagnostic commands instead of depending on an interactive debugger on FireOS.

---

## 21. Deployment

Initial deployment target for gateway-side services: Docker Compose.

Example components:

```text
gateway
speech-worker
Hermes integration / dependencies
persistent data volume
```

The gateway should also remain runnable as a native Go binary.

Future target: Raspberry Pi / ARM64 host without architectural changes.

---

## 22. Implementation Milestones

### Milestone 0 — Repository and development foundation

- establish Go module and package layout;
- add formatting/lint/test CI;
- create `echod`, `gateway`, `echoctl` and `dotsim` entrypoints;
- define initial protocol types;
- add Windows/WSL development documentation.

### Milestone 1 — Hardware vertical slice

Prove real Echo hardware independently from speech/assistant integration:

```text
Echo mic -> gateway -> WAV file
WAV/PCM -> gateway -> Echo speaker
Gateway -> Echo LED state
Echo buttons -> gateway event
```

Success criterion: microphone and speaker round-trip are reliable and observable.

### Milestone 2 — Protocol and simulator

- device registration;
- capabilities;
- reconnect/backoff;
- config push;
- structured logs;
- health reporting;
- binary audio framing;
- `dotsim` integration tests.

### Milestone 3 — Local speech

```text
Echo/dotsim
  -> wake or push-to-talk
  -> VAD/endpointing
  -> STT
  -> transcript
```

Validate at least:

- English test utterances;
- Ukrainian test utterances;
- recorded/replayable fixtures.

### Milestone 4 — Complete mock voice loop

Before Hermes:

```text
STT
 -> MockBackend("You said: ...")
 -> TTS
 -> Echo speaker
```

Success criterion: complete two-way spoken interaction without an external assistant dependency.

### Milestone 5 — Hermes adapter

- implement `AssistantBackend` adapter for Hermes;
- map local conversation IDs to Hermes sessions;
- expose backend health/capabilities;
- keep Hermes credentials server-side.

### Milestone 6 — Persistent conversations

- SQLite schema;
- conversation creation;
- turn storage;
- active conversation per device;
- new/switch/resume operations;
- infrastructure-level voice commands for conversation switching.

### Milestone 7 — Management UI

- device list/status;
- audio diagnostics;
- speech/provider configuration;
- assistant backend configuration;
- conversation management.

### Milestone 8 — Productization

- stable bootstrap/install flow;
- upgrades/rollback;
- stronger device authentication;
- deployment documentation;
- Raspberry Pi/ARM64 validation;
- multi-Dot arbitration;
- optional device-side wake word.

---

## 23. First Engineering Tasks

A sensible first implementation order is:

1. Create the Go module and package skeleton.
2. Add CI for `go test`, `go vet` and linting.
3. Port/adapt only the minimum Echo hardware code required to capture microphone audio.
4. Implement a minimal gateway WebSocket server.
5. Stream mic PCM from Echo to gateway and write it to WAV.
6. Implement gateway-to-Echo speaker playback.
7. Add LED state and action-button events.
8. Extract protocol types into a shared package.
9. Implement `dotsim` against the same protocol.
10. Add record/replay fixtures.
11. Add a mock assistant backend.
12. Add STT/TTS provider interfaces.
13. Add local Whisper integration.
14. Add Hermes only after the voice transport is proven.

This sequence intentionally reduces the number of systems being debugged at once.

---

## 24. Architectural Questions to Validate on Hardware

The following should remain decisions under test rather than assumptions:

- Which Echo microphone path/channel arrangement provides the best initial mono stream?
- Whether beamforming should be reused from EchoLocal or implemented differently.
- Whether AEC is necessary in the initial conversational model or only for barge-in/full-duplex behaviour.
- Exact native speaker format and whether resampling belongs on the Dot or gateway.
- Best supervised startup mechanism on the existing Magisk-rooted FireOS installation.
- Whether continuous audio streaming is acceptable on the target LAN and CPU profile.
- Which gateway wake-word implementation works best for English/Ukrainian household use.
- Whether Hermes should provide STT/TTS in the initial integration or only the assistant response.

These should be resolved with small diagnostics and recorded evidence rather than buried inside the main daemon.

---

## 25. Target v0.1 Stack

| Component | Initial choice |
|---|---|
| Echo daemon | Go |
| Echo hardware reference | EchoLocal |
| Device transport | WSS |
| Control frames | JSON |
| Audio frames | binary PCM |
| Gateway | Go |
| Persistence | SQLite |
| Management UI | React + Vite, embedded in Go |
| Provisioning CLI | Go + Cobra |
| STT | local faster-whisper worker initially |
| Languages | English + Ukrainian |
| Wake word | gateway-side / push-to-talk first |
| TTS | provider abstraction; Hermes or local initially |
| Assistant | Hermes adapter + mock adapter |
| Local dev | Windows + WSL2 |
| Device iteration | Windows ADB called from WSL |
| Integration testing | `dotsim` + recorded WAV fixtures |
| Deployment | Docker Compose |

---

## 26. Core Boundary

The most important architectural boundary is:

```text
Echo Dot
    |
    | Echo Satellite Protocol
    v
Voice Gateway
    |
    +-- STT Provider       -> Whisper / Hermes / other
    +-- Assistant Backend -> Hermes / OpenClaw / other
    +-- TTS Provider       -> Hermes / local / other
```

If this boundary remains clean, the Echo Dot becomes a reusable local voice terminal rather than a Hermes-specific appliance.
