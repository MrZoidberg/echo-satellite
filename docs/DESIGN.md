# Echo Satellite — High-Level Design and Implementation Plan

## 1. Purpose

Echo Satellite repurposes a rooted Amazon Echo Dot Gen 2 as a self-hosted network voice terminal.

The Echo Dot provides the hardware-facing capabilities — microphone capture, **local voice activity detection for wake gating**, **local wake-word detection**, speaker playback, LEDs, buttons, volume and mute — while a separate gateway performs speech recognition, assistant orchestration, conversation management, speech generation, fleet management and agent updates.

Hermes is the first assistant backend, not part of the device protocol. The same satellite should be able to work later with OpenClaw, a Raspberry Pi-hosted assistant, another local agent, or a cloud service without rewriting the device agent.

The project also treats deployed Echo devices as a small managed fleet. After the initial rooted-device bootstrap, normal agent releases must be remotely deployable from the gateway with safe application-level A/B slots and automatic rollback. Ordinary Echo Satellite updates must not require reflashing FireOS, boot partitions or Magisk.

This document defines the initial architecture and implementation direction. Details that depend on real Echo Dot hardware should be validated experimentally rather than treated as assumptions.

---

## 2. Goals

### Core goals

- Run a small Go daemon on a rooted Echo Dot Gen 2.
- Perform wake-word detection locally on the Echo Dot.
- Use local VAD as part of the wake pipeline to suppress non-speech false activations.
- Reuse/adapt proven local wake-word implementations from existing projects where practical.
- Stream microphone audio to the gateway only for an active voice turn, rather than continuously for wake detection.
- Play gateway-generated audio through the Echo speaker.
- Expose LEDs, buttons, mute and volume as device capabilities.
- Support automatic local gateway discovery using mDNS, with explicit/static configuration as a fallback and override.
- Support English and Ukrainian speech recognition.
- Integrate first with Hermes while keeping the assistant backend replaceable.
- Preserve conversation context across multiple voice turns.
- Allow creating a new conversation and switching back to previous conversations.
- Provide a simple web management UI.
- Provide a host-side CLI for installation, configuration and diagnostics.
- Support **gateway-managed agent updates** after initial provisioning.
- Use **application-level A/B slots** so a failed agent release can be rolled back without ADB.
- Automatically roll back an agent that crashes or fails to become healthy after activation.
- Support manual and automated/staged fleet rollout policies.
- Make Windows + WSL2 development fast and practical.
- Make most gateway work testable without physical Echo hardware.

### Non-goals for the first release

- Recreating every Alexa feature.
- Gateway-side wake-word detection.
- Gateway-side VAD for wake-word gating.
- Continuously streaming microphone audio to the gateway solely for wake detection.
- Running STT or the LLM directly on the Echo Dot.
- Supporting arbitrary Echo generations before Gen 2 is stable.
- Multi-room arbitration in the first vertical slice.
- Perfect far-field tuning before the basic end-to-end voice loop works.
- Building a general home-automation protocol.
- Updating FireOS/system/boot partitions during ordinary Echo Satellite agent updates.
- Automatically updating the low-level recovery/supervisor mechanism before the agent OTA path is proven safe.

---

## 3. Design Principles

### 3.1 The Echo is a voice satellite, not an assistant

The Echo device should know nothing about Hermes, OpenClaw, LLM APIs, conversation storage or speech providers.

Its responsibilities are limited to:

- hardware initialization;
- continuous local microphone capture needed by the wake stack;
- local preprocessing / beamforming where enabled;
- **local VAD used to gate wake inference**;
- **local wake-word inference**;
- short audio buffering around a wake trigger;
- streaming command audio for an active turn;
- audio playback;
- LED and button control;
- mute and volume;
- local gateway discovery;
- configuration;
- diagnostics;
- agent staging/update participation;
- maintaining a secure connection to the gateway.

### 3.2 Wake detection, including wake VAD, is always local

Wake-word detection is a device capability and is not implemented by the gateway.

The always-on wake stack is conceptually:

```text
microphone
  -> optional DSP / beamforming / noise suppression
  -> local VAD
  -> local wake-word model
  -> wake accepted only when wake criteria are satisfied
```

For openWakeWord specifically, upstream openWakeWord includes a Silero VAD option. When `vad_threshold` is enabled, each input frame gets a VAD score and wake predictions are accepted only when the VAD score is above the configured threshold.

The gateway may configure the active wake model, wake threshold and local VAD settings and may receive wake diagnostics, but it does not receive a continuous microphone stream and does not score wake words or wake VAD itself.

A manual Action-button / push-to-talk trigger may start a turn without a wake word for development, accessibility and recovery. This is an alternate **trigger**, not an alternate wake-word engine.

### 3.3 Wake VAD and command endpointing are separate concerns

There are two different uses of voice activity detection in the system:

1. **Wake VAD — device-local, always-on.** It helps decide whether a wake-word score is credible speech and should be allowed to trigger.
2. **Command endpointing — after a wake/button trigger.** It decides when the user's spoken command has ended so STT can proceed.

For v0.1, command endpointing may run on the gateway. This does not make wake detection gateway-side: the gateway sees audio only after the device has already created a voice turn.

The two functions have separate configuration and thresholds.

### 3.4 The gateway owns assistant and fleet orchestration

The gateway is responsible for:

- advertising its local endpoint using mDNS;
- device registration and configuration;
- turn lifecycle after a local wake/button trigger;
- post-wake command endpointing;
- speech-to-text provider selection;
- assistant backend selection;
- conversation management;
- text-to-speech provider selection;
- release discovery and artifact caching;
- desired agent version per device/fleet;
- update rollout policy and progress tracking;
- management API and UI;
- observability.

### 3.5 Stable internal contracts, replaceable providers

The system should define its own interfaces for:

- satellite protocol;
- local discovery;
- wake-model configuration;
- local wake VAD configuration;
- command endpointing;
- STT;
- TTS;
- assistant backend;
- conversation storage;
- agent release manifests;
- artifact storage/distribution;
- rollout policy.

Hermes-specific concepts stay inside a Hermes adapter. GitHub-specific release discovery stays inside a release-source adapter.

### 3.6 Capability negotiation instead of feature gates by firmware version

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
    "wake.local.microwakeword",
    "wake.local.vad",
    "update.ab.v1"
  ]
}
```

Normal product behaviour is negotiated by capability, not by checks such as `version >= X`.

Release manifests may still declare minimum protocol or supervisor compatibility because those are **installation safety constraints**, not runtime feature negotiation.

### 3.7 Agent updates are application-level, not FireOS OTA

Echo Satellite A/B slots refer to two copies of the **Echo Satellite agent binary** under `/data`, not Android A/B partitions and not Amazon's FireOS OTA mechanism.

A normal agent update must never require writing the Echo's bootloader, boot image, recovery or system firmware.

### 3.8 The updater must survive the binary it is updating

The component that decides whether a new agent boot succeeded cannot live only inside that same replaceable agent binary.

A small stable supervisor/bootstrap hook must live outside the A/B agent slots and be capable of:

- starting the selected slot;
- observing trial state;
- detecting startup/health failure;
- restoring the previous slot;
- keeping a small persistent recovery log.

The supervisor should be intentionally small and change rarely.

### 3.9 Never destroy the rollback path before the replacement is proven

When updating the inactive slot:

- keep the existing inactive binary untouched while downloading;
- write the replacement to a separate `.part`/staging path;
- verify the full artifact;
- atomically promote the staged file into the inactive slot;
- only then activate the slot.

A failed transfer must leave both the current running agent and the previous rollback candidate usable.

### 3.10 New binaries run on trial before being committed

A successful process start is not sufficient proof of a good deployment.

After activating a new slot, it remains **on trial** until it demonstrates critical health such as:

- process remains alive;
- required local hardware initialization succeeds;
- configuration loads;
- gateway discovery/static configuration succeeds;
- secure gateway connection succeeds;
- protocol `hello`/`welcome` handshake succeeds.

Only then should the trial be committed. If the trial times out or repeatedly crashes, the supervisor restores the previous slot.

### 3.11 Hardware-independent development where possible

The protocol and gateway should be testable against a simulated device (`dotsim`) that feeds WAV files instead of microphone hardware and writes playback audio to disk instead of a speaker.

The simulator must also emulate update states, reconnects and rollbacks so the gateway's rollout logic can be tested without intentionally breaking a real Echo.

### 3.12 Zero-configuration local discovery, explicit configuration when needed

A newly installed satellite should normally be able to find a gateway on the same local network without requiring an IP address.

mDNS is the default local discovery mechanism. It only locates candidate gateway endpoints; it is not authentication.

An explicitly configured gateway URL takes precedence over mDNS for VLANs, routed networks, VPNs, multiple gateways and networks where multicast DNS is unavailable.

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
- local openWakeWord and microWakeWord support;
- TFLite model loading;
- wake-model metadata and sensitivity configuration;
- host-side installer UX;
- ADB-based development workflow;
- safe download/staging of agent binaries;
- size + SHA-256 verification;
- trial/commit semantics;
- boot-hook rollback concepts.

EchoLocal already provides a useful Go-native local wake stack and supports both openWakeWord and microWakeWord model kinds. Echo Satellite should reuse/adapt those low-level pieces rather than inventing a separate wake inference stack.

At the time of this design, EchoLocal's current Go openWakeWord path does not appear to include the upstream Silero VAD gate. Echo Satellite should therefore treat local wake VAD as an additional local component that must be implemented/adapted and validated.

EchoLocal's updater is also a useful reference for a key safety rule: download completely, verify size/hash, retain the old binary, and treat the new binary as on trial until it has run long enough to be trusted. Echo Satellite should extend that idea with gateway-handshake health rather than using uptime alone.

### openWakeWord

Repository: `dscripka/openWakeWord`

Reference for:

- 16 kHz PCM input expectations;
- wake-model scoring;
- wake activation thresholds;
- optional Speex noise suppression;
- bundled Silero VAD gating through `vad_threshold`;
- the rule that wake predictions are accepted only when the simultaneous VAD score is above the configured VAD threshold.

Echo Satellite does not need to run the upstream Python package on the Dot. Its behaviour is the reference for the local Go implementation/adaptation.

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
- operational lessons from real Echo hardware;
- **application-level A/B agent slots**;
- controller-managed fleet OTA;
- inactive-slot transfer and verification;
- atomic active-slot symlink switching;
- automatic rollback after repeated fast starts;
- manual instant rollback to the preserved slot;
- release discovery and controller-side artifact caching;
- keeping provisioning-time payloads in sync after deployment.

EchoMuse's controller-side wake architecture is not the target architecture for Echo Satellite, but its A/B OTA architecture is directly relevant.

A key lesson from EchoMuse is that A/B here means two application binaries such as `server_a` and `server_b`, selected through a stable symlink. It does **not** mean Android/FireOS partition A/B.

Another important operational lesson is that anything placed on the Dot only during provisioning will eventually drift unless it has a reconciliation/update path. Echo Satellite should therefore treat the agent binary, wake assets, configuration and supervisor/bootstrap assets as separately versioned desired state.

---

## 5. High-Level Architecture

```text
                              +--------------------------+
                              |      Management UI       |
                              | devices / voice / convos |
                              | releases / deployments   |
                              +------------+-------------+
                                           |
                                           | HTTP/WS
                                           v
+------------------------------+  WSS  +--------------------------------+
| Echo Dot Gen 2               |<----->|         Voice Gateway          |
|                              |       |                                |
| stable supervisor            |       | mDNS advertisement             |
|   +-> echod -> echod_a       |       | device manager                 |
|   |           echod_b        |       | turn/conversation managers     |
|   |                          |       | command endpointing             |
|   +-> trial/rollback state   |       | STT/TTS + assistant routing    |
|------------------------------|       | Update Manager                 |
| mDNS discovery               |       | Release Source(s)              |
| mic capture                  |       | Artifact Cache                 |
| local DSP / beamforming      |       | Fleet Rollout Controller       |
| local wake VAD               |       | SQLite                         |
| local wake engine            |       +----------+----------+----------+
| short pre-roll buffer        |                  |          |
| speaker / LEDs / buttons     |                  |          |
| mute / volume                |        +---------+          +----------------+
+------------------------------+        v                                      v
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
local microphone
  -> local preprocessing
  -> local VAD
  -> local wake model
  -> wake accepted
  -> immediate local feedback
  -> turn.start + command audio over WSS
  -> gateway command endpointing
  -> STT
  -> assistant
  -> TTS
  -> Echo speaker
```

Normal agent update flow:

```text
release source / local uploaded build
  -> Gateway Update Manager
  -> cache + verify release artifact
  -> select eligible device(s)
  -> device stages artifact into inactive A/B slot
  -> verify size + SHA-256 + signature
  -> atomic slot activation
  -> clean restart
  -> new agent enters trial
  -> local init + secure gateway handshake
  -> explicit healthy/commit

failure before commit
  -> stable supervisor restores previous slot
  -> device reconnects on known-good version
  -> gateway records rolled_back
```

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
│   │   ├── wake/              # local wake engines, wake VAD, models
│   │   ├── update/            # staging, verification, slots, trial client
│   │   ├── buttons/
│   │   ├── led/
│   │   ├── mixer/
│   │   └── system/
│   ├── discovery/             # mDNS advertisement/discovery
│   ├── protocol/              # shared Dot <-> Gateway messages
│   ├── release/               # manifest/signature/version primitives
│   ├── gateway/
│   │   ├── devices/
│   │   ├── turns/
│   │   ├── endpointing/       # post-wake command endpointing only
│   │   ├── conversations/
│   │   ├── updates/           # desired versions + rollout state machine
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
├── device_payloads/
│   └── supervisor/            # stable external supervisor/bootstrap hook
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
│   ├── wake/
│   └── updates/               # manifests + failure/rollback fixtures
└── README.md
```

Keeping the first versions in one repository simplifies coordinated protocol, supervisor and agent changes.

---

## 7. Device Agent (`echod`) and Stable Supervisor

### 7.1 `echod` responsibilities

`echod` runs as a supervised service on the rooted Echo Dot.

It should:

- derive stable device identity from the device serial;
- initialize supported hardware;
- discover a local gateway using mDNS when no explicit gateway is configured;
- establish/re-establish the gateway WebSocket;
- advertise capabilities and software/update state;
- continuously capture microphone frames while idle;
- run the local preprocessing / wake-VAD / wake-model pipeline;
- maintain a small audio ring buffer so speech after the wake phrase is not clipped;
- start a turn after an accepted local wake or Action-button trigger;
- stream command PCM only during the active listening phase;
- receive and play response PCM;
- report button and mute events;
- apply volume/config changes;
- render semantic LED states;
- report health and logs;
- stage and verify agent updates when instructed by the gateway;
- request a controlled restart after activating a new slot;
- explicitly mark an on-trial release healthy only after critical initialization and gateway handshake.

### 7.2 Audio strategy

Start with the simplest known-good microphone path and prove local wake detection plus the complete voice loop before optimizing far-field behaviour.

The same local capture path should feed wake processing and active-turn streaming so changing from idle wake monitoring to command capture does not require reopening/reconfiguring ALSA.

```text
ALSA mic
  -> optional beamforming / noise suppression
  -> local wake VAD
  -> local wake model
```

For openWakeWord-compatible models, retain the reference 16 kHz PCM expectations where practical.

### 7.3 Agent slots

Proposed application layout:

```text
/data/local/bin/
  echod -> echod_a          # stable symlink / launcher target
  echod_a                   # one full agent binary
  echod_b                   # other full agent binary

/data/local/etc/echo-satellite/
  update-state.json         # pending/trial metadata
  supervisor.log            # small bounded recovery log
  config.*
  credentials/
  wake-models/
```

The exact path can change after hardware validation, but the semantics should remain the same.

The symlink target identifies the active slot. The other slot is the rollback/update slot.

### 7.4 Stable supervisor

A minimal root-capable supervisor/startup hook must live outside `echod_a` and `echod_b`.

It should:

- resolve the active slot;
- verify that the selected target exists and is executable before launching it;
- start `echod`;
- distinguish ordinary operational exits from an update trial failure;
- count repeated fast exits during trial;
- enforce a trial deadline;
- revert the active symlink when trial failure criteria are reached;
- restart the known-good slot;
- keep a bounded persistent recovery log under `/data`;
- never depend on the gateway being reachable in order to roll back.

This supervisor is recovery infrastructure. It should be kept small enough to reason about and test independently.

### 7.5 Trial health

A newly activated slot is not committed merely because it stayed alive for N seconds.

An update should be considered healthy only after the new agent can demonstrate at least:

```text
process running
local configuration loaded
critical hardware initialized sufficiently for normal operation
gateway discovered/resolved or explicit endpoint loaded
TLS/authentication succeeded
protocol hello/welcome completed
reported expected running version/build identity
```

The agent can then write/emit a local `trial_healthy` marker/event. The supervisor removes pending trial state only after this condition.

A timeout protects against a binary that stays alive but can no longer join the control plane.

### 7.6 Device state

```text
idle
listening
thinking
speaking
muted
offline
error
updating
update_trial
```

Wake tone/LED feedback should happen immediately on the device after local detection instead of waiting for gateway latency.

---

## 8. Satellite Protocol and Local Discovery

### 8.1 Transport

Initial transport:

- secure WebSocket (`wss`);
- one long-lived outbound connection per device;
- JSON text frames for control/events;
- binary WebSocket frames for PCM audio.

The WSS connection may remain idle while the local wake stack listens. Microphone frames are not forwarded until a turn starts.

Agent artifacts should not be streamed as thousands of ordinary control messages. The gateway should normally provide an authenticated HTTPS artifact URL while WSS carries deployment commands/progress.

### 8.2 mDNS gateway discovery

The gateway advertises:

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
4. Select compatible/preferred gateway
5. Connect and authenticate over WSS
6. Retry with backoff if unavailable
```

A previously paired `server_id` is preferred even when its IP address changes.

### 8.3 Optional device advertisement

For provisioning/diagnostics, `echod` may optionally advertise:

```text
_echo-satellite-device._tcp.local.
```

Only non-sensitive metadata such as device ID, version and pairing state should be exposed.

### 8.4 Network limitations and fallback

mDNS normally stays within a multicast domain. VLANs, multicast filtering, routed segments, VPNs or container isolation may require an mDNS reflector/repeater or explicit gateway URL.

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

### 8.5 Connection and turn flow

```text
Device boots
  -> supervisor selects active agent slot
  -> echod initializes mic + local wake VAD + wake engine
  -> load explicit/previous gateway configuration
  -> if necessary discover gateway over mDNS
  -> connect/authenticate over WSS
  -> hello(device id, version, capabilities, wake config, update state)
  <- welcome/config
  -> if on trial and critical checks passed: commit/mark healthy
  -> idle; local wake stack continues
```

Wake flow:

```text
Wake accepted locally
  -> immediate local LED/tone
  -> turn.start(trigger=wake, model, wake_score, vad_score)
  -> audio.start
  -> binary PCM command audio
  <- state(thinking)
  <- play.start + binary response audio
  -> playback complete
  -> return to idle/local wake stack
```

### 8.6 Candidate message families

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

update.offer
update.accept / update.reject
update.progress
update.staged
update.restarting
update.trial
update.confirmed
update.rolled_back
update.failed

button
mute
volume

ping / pong
error
```

Exact schemas belong in `docs/protocol.md`.

---

## 9. Gateway

The gateway should initially be written in Go.

### 9.1 Discovery Service

Owns:

- stable `server_id`;
- `_echo-satellite._tcp.local.` advertisement;
- advertised protocol/WSS endpoint metadata;
- service lifecycle across network-interface changes;
- optional diagnostic browsing of satellite advertisements.

### 9.2 Device Manager

Maintains:

- connected devices;
- capabilities;
- running agent version/build;
- active/inactive slot metadata;
- update capability/supervisor version;
- pending trial/update status;
- installed/active wake-model metadata;
- local wake-VAD configuration/status;
- current voice state;
- last-seen time;
- configuration revisions;
- commands and active audio streams.

### 9.3 Turn Manager

Owns the server-side turn state after a device trigger:

```text
IDLE
  -> LISTENING
  -> THINKING
  -> SPEAKING
  -> IDLE
```

The gateway never transitions `IDLE -> LISTENING` because of wake inference of its own; it does so only after `turn.start` from a device or simulator.

### 9.4 Command Endpointing

Consumes only active-turn audio and decides when the spoken command is complete.

The implementation may use a VAD model, silence timing, streaming STT information or a combination. It is distinct from local wake VAD.

### 9.5 Speech Router

Selects STT and TTS providers through internal interfaces.

### 9.6 Assistant Router

Selects an assistant adapter. Hermes is the first implementation.

### 9.7 Conversation Manager

Owns local conversation identity and maps it to backend-specific sessions/threads.

### 9.8 Update Manager

The Update Manager owns desired agent state and fleet rollout.

Responsibilities:

- query configured release source(s), initially GitHub Releases and local uploads;
- cache release metadata/artifacts on the gateway;
- verify artifact metadata/signatures before offering a release;
- compare running versus desired releases;
- enforce update channel/policy;
- ensure target device capabilities and supervisor compatibility;
- create deployment records;
- issue update offers;
- monitor progress, reconnect, trial, confirmation and rollback;
- limit update concurrency;
- support canary/staged deployments;
- stop or pause rollout on failures/rollbacks;
- expose manual rollback;
- keep release notes and deployment outcomes available in the UI.

The Dot should not independently poll GitHub for releases. The gateway is the fleet control plane and performs external release discovery once for the whole deployment.

---

## 10. Agent A/B Update Architecture

### 10.1 Scope

**A/B deployment applies only to the Echo Satellite agent and closely related application assets.**

It is not an Android A/B partition design and is not a replacement for the original rooting/unbrick process.

Once a device is bootstrapped, ordinary releases should operate entirely from writable application state under `/data` whenever possible.

### 10.2 Release offer

Conceptual server message:

```json
{
  "type": "update.offer",
  "deployment_id": "01J...",
  "release": {
    "version": "0.3.0",
    "build_id": "git-abc123",
    "size": 12849320,
    "sha256": "...",
    "signature": "...",
    "protocol_min": 1,
    "protocol_max": 1,
    "supervisor_min": 1,
    "architecture": "linux-arm64"
  },
  "artifact_url": "https://gateway/.../artifacts/01J...?token=..."
}
```

The artifact URL should be short-lived and scoped to the deployment/device.

### 10.3 Device-side staging flow

```text
receive update.offer
  -> verify update capability and local eligibility
  -> determine active slot A/B
  -> choose inactive slot
  -> verify sufficient free space
  -> download to inactive.part
  -> stream-compute SHA-256
  -> verify expected size
  -> verify release signature
  -> fsync staged file
  -> atomically replace inactive slot
  -> fsync relevant directory/state where practical
  -> persist pending deployment metadata
  -> atomically flip active symlink
  -> request graceful restart
```

At no point should the currently running slot be overwritten.

The previous inactive slot should remain untouched until the complete new artifact has been verified. A broken/interrupted transfer therefore cannot erase the only fallback copy.

### 10.4 Trial and commit

After restart:

```text
supervisor launches newly active slot
  -> trial marker is present
  -> echod initializes
  -> echod connects/authenticates to gateway
  -> hello identifies expected build/version
  -> gateway replies successfully
  -> echod records local trial health
  -> update.confirmed emitted
  -> supervisor commits slot by clearing trial state
```

Trial health should be a local decision that incorporates gateway handshake. The gateway's acknowledgement is useful evidence, but rollback must still work if the gateway disappears.

### 10.5 Automatic rollback

Rollback conditions should include:

- repeated fast process exits during trial;
- process cannot be started/executed;
- trial deadline expires without healthy marker;
- optionally, explicit local fatal initialization result before the deadline.

On rollback:

```text
supervisor
  -> verify fallback slot exists/executable
  -> atomically flip active symlink to previous slot
  -> persist rollback reason
  -> launch/restart previous agent

previous agent reconnects
  -> reports rolled-back deployment/version/reason
  -> gateway marks deployment rolled_back
```

The supervisor must never flip to a missing/non-executable fallback slot.

### 10.6 Manual rollback

If the previous slot is still present and valid, an administrator should be able to request an immediate rollback without retransferring a binary.

```text
gateway -> update.rollback request
Dot/supervisor -> switch to previous slot -> restart -> reconnect
```

The manual operation should still record a deployment/audit event.

### 10.7 Update state machine

Suggested gateway-visible state:

```text
idle
available
queued
downloading
verifying
staged
restarting
trial
confirmed
failed
rolled_back
cancelled
```

A device should report the current phase and progress when meaningful.

### 10.8 Updating ancillary device payloads

Not every device-side file belongs in the agent A/B slot.

Treat deployed state explicitly:

```text
agent binary           -> A/B OTA
wake/VAD models        -> authenticated asset synchronization
normal device config   -> config synchronization
credentials            -> explicit secure rotation/provisioning
supervisor/bootstrap   -> rare separately versioned recovery update
```

Every payload installed during bootstrap must either be immutable by design or have a reconciliation path for already-deployed devices.

### 10.9 Supervisor updates

The stable supervisor is more dangerous to update than `echod` because it is the recovery mechanism.

For v0.1:

- install it during bootstrap;
- expose/report a supervisor version;
- keep it backward-compatible with agent slots where practical;
- do not automatically update it as part of normal agent rollout;
- require an explicit admin operation for supervisor changes until a separately safe recovery strategy is proven.

The agent may report that a release requires a newer supervisor; the gateway should then mark that release ineligible instead of attempting it.

---

## 11. Release Artifacts and Trust

### 11.1 Release bundle

CI should produce at minimum:

```text
echod
manifest.json
manifest.sig
```

Conceptual manifest:

```json
{
  "schema": 1,
  "version": "0.3.0",
  "build_id": "git-abc123",
  "architecture": "linux-arm64",
  "size": 12849320,
  "sha256": "...",
  "protocol_min": 1,
  "protocol_max": 1,
  "supervisor_min": 1,
  "released_at": "2026-08-18T00:00:00Z"
}
```

### 11.2 Signature

Prefer a small public-key signature scheme such as Ed25519 for release manifests.

The release private key belongs in the controlled build/release process, not on the gateway or Echo devices. The verification public key can be embedded in the agent/bootstrap/update code.

Both gateway and device should verify signed production releases.

### 11.3 Development builds

Local iteration needs a deliberate escape hatch, for example:

```yaml
updates:
  allow_unsigned_dev_builds: true
```

This must be disabled by default in production-style deployments and clearly surfaced in UI/status when enabled.

### 11.4 Gateway artifact cache

The gateway should download an immutable release artifact once and reuse it across the fleet.

Cache entries should be trusted only when their recorded digest still matches the bytes on disk. An incomplete/corrupt cache must be treated as a cache miss, not as a valid release.

Keep at least the currently rolling release and a recent rollback release in the cache. Cache eviction is an optimization and must not change device rollback safety because each Dot preserves its own previous slot.

---

## 12. Automated and Staged Fleet Rollout

### 12.1 Update channels

Suggested channels:

```text
stable
beta
dev
```

A device or fleet can follow a channel, but the gateway chooses and records the concrete desired release.

### 12.2 Policies

Suggested policies:

```text
manual     # notify/show update; administrator initiates deployment
auto       # gateway deploys eligible releases according to rollout rules
```

Manual should be the initial default until the updater has substantial real-device validation.

### 12.3 Staged rollout

Even a small household fleet benefits from sequential deployment:

```text
new release
  -> canary device
  -> wait for confirmed healthy state
  -> next device(s)
  -> remaining fleet
```

Initial default:

```text
max_concurrent_updates = 1
stop_on_rollback = true
stop_on_failure = true
```

If the canary rolls back or fails, the gateway should stop the remaining automatic rollout and surface the reason.

### 12.4 Eligibility

Before deploying, check:

- device online and approved;
- not currently in a voice turn/update;
- `update.ab.v1` capability present;
- architecture matches;
- supervisor meets release minimum;
- protocol compatibility is plausible for the new agent/gateway pair;
- enough device free space if known;
- desired version differs from running version;
- no conflicting deployment already active.

### 12.5 Update timing

Automatic updates should avoid interrupting active voice playback/turns.

For v0.1 it is sufficient to begin only when the device is idle. A later policy may support quiet hours or maintenance windows.

---

## 13. Speech Recognition and Generation

### STT

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

If the selected STT provider needs whole utterances, the gateway buffers the active turn until command endpointing completes. Streaming STT can be added later without changing the local wake architecture.

### Python speech worker

```text
Gateway (Go)
   -> speech-worker HTTP/gRPC
       -> faster-whisper
```

### TTS

```go
type TTS interface {
    Synthesize(ctx context.Context, text string, opts TTSOptions) (AudioStream, error)
}
```

TTS may initially come from Hermes or a local provider.

---

## 14. Assistant Backend Abstraction

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

## 15. Conversation Model

Conversation identity belongs to Echo Satellite, not Hermes.

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
wake_score
wake_vad_score
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
build_id
active_slot
supervisor_version
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

## 16. Wake Word and Local Wake VAD

### Architecture decision

**Wake-word detection and the VAD used to gate wake detection are local-only. Gateway-side wake detection is intentionally unsupported.**

The initial implementation should reuse/adapt EchoLocal's current local wake stack and add/reuse the local VAD behaviour needed to match upstream openWakeWord semantics.

### Wake engines

The reference implementation already supports two useful model families:

- **openWakeWord** classifiers using TFLite models and a shared on-device feature/embedding pipeline;
- **microWakeWord** TFLite models using the existing microWakeWord runtime.

The model-loading layer should preserve:

- `.tflite` wake models;
- optional sidecar metadata describing phrase and trained languages;
- engine detection/model kind;
- configurable wake sensitivity/threshold;
- stable model IDs;
- backend-independent wake configuration.

### Local VAD

For the openWakeWord path, local VAD should be supported as part of the wake pipeline and enabled by default for normal deployments unless real-device testing shows a reason not to.

```text
for each audio frame:
  compute VAD score
  compute wake model score
  accept positive wake prediction only if:
      wake score >= wake threshold
      AND
      VAD score >= vad_threshold
```

The project default VAD threshold should be chosen from real-device testing rather than hard-coded prematurely.

For microWakeWord, use the behaviour of the selected runtime/model and do not force openWakeWord-specific VAD semantics onto it without testing. The device-level wake interface should nevertheless expose whether local VAD gating is active.

### v0.1 behaviour

Start with one active wake model per device to keep behaviour and CPU use predictable. The internal API should not prevent multiple simultaneously active models later.

```text
mic capture
  -> optional DSP / beamforming / noise suppression
  -> local wake VAD
  -> local wake model
  -> wake + VAD thresholds satisfied
  -> immediate local wake feedback
  -> create turn_id
  -> send turn.start
  -> stream command audio
```

### Pre-roll

Maintain a small PCM ring buffer while idle. When wake fires, include a configurable tail of pre-trigger audio so the first word after the wake phrase is not clipped.

Tune this from real recordings. If pre-roll repeatedly carries the wake phrase into STT, reduce it or strip the configured phrase from STT output rather than moving wake processing to the gateway.

### Command endpointing

Wake VAD is not the mechanism that ends the user's command.

For v0.1:

```text
wake VAD:             device only
wake inference:       device only
command endpointing:  gateway, after turn.start
STT:                  gateway-side provider
```

Later, command endpointing may also move to the device if latency/bandwidth testing justifies it.

### Action button

```text
button press
  -> turn.start(trigger=button)
  -> command audio stream
```

This bypasses wake inference but enters the same post-trigger command pipeline.

### Configuration

```yaml
wake:
  enabled: true
  engine: openwakeword
  model: okay_nabu
  threshold: 0.80
  vad:
    enabled: true
    threshold: 0.50   # example only; tune on real device
    lookback_ms: 0    # same-step diagnostic baseline; qualify a nonzero production value
  preroll_ms: 250
```

There is deliberately no gateway wake mode.

Wake and VAD scores must not be assumed to be temporally aligned on the same PCM
step. A wake engine may score a temporal receptive field and emit its peak only
after speech has ended. The leading remediation candidate is for the acceptance
gate to use the maximum recent VAD score from a bounded, configurable lookback
window rather than only the instantaneous VAD score. It must be compared against
other alignment strategies using false-accept and false-reject measurements
before becoming the production rule. Every candidate remains entirely
device-local.

Any alignment setting is a property of the qualified wake/VAD pipeline
configuration, not of a particular phrase. Qualification identifies at least
the wake engine and model, VAD implementation, PCM step geometry, and
preprocessing configuration, and covers relevant speakers and acoustic
conditions. It must be measured from aligned per-step traces for every supported
bundled or newly trained model. Configuration may override the qualified value
for diagnostics, but production defaults must come from recorded measurements
rather than a phrase-specific constant. Diagnostics report both instantaneous
VAD and the effective aligned VAD evidence used by the gate, plus the alignment
configuration, so a rejection can be explained without storing raw audio.

### Wake model distribution

Wake/VAD models are device assets, not agent releases.

Initially, `echoctl` may install/update them. The gateway should later synchronize selected/required signed or otherwise trusted assets independently of the agent A/B binary.

Model binaries must not be fetched from arbitrary unauthenticated locations by the Dot.

### Diagnostics

Useful local wake diagnostics:

```text
active model id
model kind
trained language metadata
wake threshold
wake VAD enabled/disabled
wake VAD threshold
configured VAD lookback
last wake score
last instantaneous VAD score
last effective VAD score used by the gate
wake count
rejected high-wake/low-VAD candidate count
false-trigger test recordings (opt-in)
inference timing / CPU usage
```

Raw continuous microphone audio must not be uploaded merely for wake scoring.

---

## 17. Management UI

### Devices

- online/offline state;
- agent version/build and capabilities;
- active A/B slot;
- supervisor version;
- update/trial state;
- discovered/paired gateway state;
- current voice state;
- volume/mute;
- microphone test;
- speaker test;
- recent logs.

### Voice / Wake

- installed local wake models;
- active model;
- model kind and language metadata;
- wake sensitivity/threshold;
- local wake-VAD enable/disable and threshold;
- last wake score / VAD score diagnostics;
- pre-roll setting;
- command endpointing settings;
- STT provider and language hints;
- TTS provider;
- basic audio tuning.

The UI must not offer gateway-side wake or wake-VAD modes.

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

### Updates

- latest releases and release notes;
- release channel;
- manual/auto policy;
- per-device current and desired version;
- active/inactive slot information;
- supervisor compatibility;
- queued/current deployment progress;
- trial/confirmed/failed/rolled-back state;
- last failure/rollback reason;
- deploy selected release to one device;
- staged fleet deploy;
- explicit rollback;
- `allow_unsigned_dev_builds` warning when enabled.

A React/Vite SPA can be embedded in the production Go gateway binary.

---

## 18. `echoctl` Provisioning and Diagnostics CLI

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
echoctl wake vad-test <wav-or-live>
echoctl speaker test
echoctl led test
echoctl buttons test

echoctl update status [device]
echoctl update deploy <version> [device]
echoctl update rollback [device]
echoctl update channel <stable|beta|dev> [device]
```

For normal post-bootstrap operation, update commands should go through the gateway control plane rather than requiring ADB.

ADB remains the development/recovery mechanism.

### Initial bootstrap flow

The expected starting point is an already rooted/Magisk-enabled Dot. Normal installation should avoid reflashing the boot image.

```text
find ADB device
  -> verify expected hardware/root
  -> inspect existing installation
  -> back up changed state
  -> install stable supervisor/bootstrap hook
  -> install first echod into slot A
  -> initialize slot B with a valid fallback or known-good copy
  -> create active symlink -> A
  -> install default wake/VAD assets
  -> push config/credentials
  -> handle conflicting Alexa services as required
  -> start supervisor/echod
  -> discover/configure gateway
  -> verify gateway connectivity
  -> microphone test
  -> local wake + VAD test
  -> speaker test
  -> verify A/B/update status
```

Rooting/flashing remains separate from normal application installation.

---

## 19. Security and Privacy

Initial model:

- WSS for device/gateway control traffic;
- HTTPS for gateway-hosted update artifacts;
- mDNS for endpoint discovery only;
- serial for identity, not authentication;
- stable gateway `server_id` for preference/pairing, not as a secret;
- per-device random credential/token;
- production agent release manifests signed by a release key;
- device verifies release signature, expected size and SHA-256 before activation;
- gateway also verifies production releases before fleet deployment;
- short-lived device/deployment-scoped artifact URLs;
- gateway stores Hermes/assistant/speech secrets;
- Echo stores only device credentials, release verification public key and local assets/configuration;
- management API requires authentication before non-local deployment.

The update subsystem is security-sensitive: permission to deploy an agent is effectively permission to run privileged code on a rooted Dot. Update/deployment endpoints must therefore require strong administrator authorization and should have an audit trail.

Local wake detection and local wake VAD provide a privacy benefit: while idle, microphone audio needed for activation decisions stays on the device rather than being continuously sent to the gateway.

Future improvement: per-device client certificates / mTLS.

A browser-exposed root shell is not part of v1; ADB is sufficient for development and recovery.

---

## 20. Persistence and Observability

SQLite is sufficient initially.

### Persistent gateway entities

Expected data:

- devices;
- per-device configuration;
- gateway/server identity;
- preferred/paired gateway metadata;
- reported wake-model inventory/selection;
- reported local wake-VAD configuration/status;
- conversations;
- backend bindings;
- turns;
- releases;
- release metadata/cache index;
- deployments;
- per-device deployment attempts;
- desired version/channel/update policy;
- system configuration.

Conceptual deployment record:

```text
id
release_version
release_build_id
scope/device_id
policy/manual-or-auto
created_at
started_at
finished_at
status
failure_reason
```

Conceptual device deployment attempt:

```text
deployment_id
device_id
from_version
to_version
from_slot
to_slot
state
progress
started_at
trial_started_at
confirmed_at
rollback_reason
last_error
```

### Turn observability

Every turn gets a `turn_id` propagated through the pipeline.

```text
device_id
connection_id
turn_id
conversation_id
trigger_type
wake_model_id
wake_score
wake_vad_score
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

### Update observability

Useful update fields/events:

```text
device_id
deployment_id
running_version
running_build_id
active_slot
inactive_slot
supervisor_version
phase
bytes_downloaded
artifact_size
verification_result
restart_requested
trial_started
trial_health_conditions
confirmed
rollback_reason
failure_stage
```

The stable supervisor should keep a **small bounded persistent log under `/data`** containing only recovery decisions and trial/rollback events. It should not duplicate verbose agent logs or be allowed to fill device storage.

Raw microphone audio is not stored by default.

---

## 21. Device Simulator (`dotsim`)

`dotsim` implements the same protocol as `echod` but replaces hardware with files/terminal events.

Example voice use:

```text
dotsim
  --discover mdns
  --trigger wake
  --wake-model okay_nabu
  --wake-score 0.87
  --vad-score 0.93
  --mic testdata/audio/uk/question.wav
  --speaker-out ./response.wav
```

Capabilities to simulate:

- mDNS discovery;
- multiple gateways;
- registration;
- local wake event, wake score and VAD score;
- button-triggered turns;
- microphone turn streams;
- speaker playback;
- reconnects;
- network failures;
- partial/older capability sets;
- A/B update capability;
- update download progress;
- successful restart into new version;
- trial timeout;
- repeated crash/fast-exit behaviour;
- automatic rollback and reconnect on previous version.

Gateway integration tests should be able to drive a fake fleet such as:

```text
3 devices
  -> deploy release
  -> device 1 confirms
  -> device 2 rolls back
  -> verify device 3 is not updated when stop_on_rollback=true
```

The simulator does not need to run a real wake model for ordinary gateway tests. Local wake/VAD correctness is tested separately with device/unit/audio-fixture tests.

---

## 22. Windows Development Workflow

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

Prefer Windows `adb.exe` called from WSL for initial bootstrap and low-level iteration:

```bash
export ADB=/mnt/c/Android/platform-tools/adb.exe
```

Keep the device binary pure Go where feasible:

```text
GOOS=linux
GOARCH=arm64
CGO_ENABLED=0
```

Early development loop:

```text
build echod
  -> adb push
  -> restart foreground/service
  -> tail logs
  -> run mic/wake/VAD fixture tests
```

Once A/B OTA is working, normal iteration on agent builds should also support:

```text
build signed/dev release bundle
  -> upload to local gateway
  -> deploy to test Dot inactive slot
  -> observe restart/trial
  -> automatic rollback if bad
```

This becomes the preferred device iteration loop because it exercises the same mechanism used in real deployments.

mDNS must be tested from the actual Docker/WSL deployment because multicast visibility can differ by network mode. Explicit host/port remains the fallback.

---

## 23. Deployment and Lifecycle

### Gateway deployment

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

### Satellite lifecycle

Satellite deployment is a different lifecycle problem from gateway deployment:

```text
one-time rooted bootstrap via ADB
  -> install supervisor + initial A/B agent layout + credentials

normal operation
  -> gateway-managed configuration/assets
  -> gateway-managed A/B agent updates
  -> no routine ADB requirement
```

The documentation should keep these two lifecycle paths separate.

---

## 24. Implementation Milestones

### Milestone 0 — Repository and development foundation

- establish Go module/package layout;
- CI for format/test/vet/lint;
- create `echod`, `gateway`, `echoctl`, `dotsim` entrypoints;
- define protocol/discovery interfaces;
- define release manifest/signature primitives;
- add Windows/WSL docs.

### Milestone 1 — Hardware + local wake vertical slice

Prove locally on the Dot:

```text
mic -> WAV
mic -> local VAD -> wake engine -> wake event
WAV/PCM -> speaker
LED/button access
```

Success criterion: the Dot repeatedly detects a selected wake model locally, with observable wake and VAD scores, without gateway wake processing.

### Milestone 2 — Discovery, protocol and simulator

- gateway mDNS advertisement;
- satellite mDNS browse/resolution;
- explicit gateway override;
- preferred `server_id`;
- registration/capabilities;
- reconnect/backoff;
- config push;
- structured logs;
- `turn.start` including wake/VAD diagnostics;
- binary turn-audio framing;
- `dotsim` integration tests.

### Milestone 3 — Safe supervisor + A/B agent OTA

Implement this **before Hermes integration** so subsequent device development can use the production update path.

- define A/B slot layout under `/data`;
- implement stable external supervisor/bootstrap hook;
- implement staging into inactive slot;
- size + SHA-256 verification;
- release signature verification;
- atomic promotion + symlink flip;
- graceful restart;
- trial marker/state;
- explicit health after gateway handshake;
- trial timeout;
- automatic rollback;
- persistent supervisor recovery log;
- manual rollback;
- simulator update/rollback tests.

Success criterion: deliberately broken agent builds recover to the previous slot without ADB.

### Milestone 4 — Gateway Update Manager

- release source abstraction;
- GitHub release discovery;
- local build upload path;
- gateway artifact cache;
- device update protocol;
- deployment persistence;
- desired-version model;
- one-device manual deploy;
- staged fleet deploy with concurrency 1;
- stop-on-failure/rollback;
- update status API.

Success criterion: a gateway can safely update several devices sequentially and stop a rollout after a simulated/real rollback.

### Milestone 5 — Local-wake speech loop

```text
Echo local wake / simulator trigger
  -> turn audio
  -> gateway command endpointing
  -> STT
  -> transcript
```

Validate English and Ukrainian utterances using replayable fixtures.

### Milestone 6 — Complete mock voice loop

```text
local VAD + wake
 -> turn audio
 -> STT
 -> MockBackend("You said: ...")
 -> TTS
 -> Echo speaker
```

### Milestone 7 — Hermes adapter

- implement `AssistantBackend` adapter;
- map local conversations to Hermes sessions;
- backend health/capabilities;
- secrets remain server-side.

### Milestone 8 — Persistent conversations

- SQLite schema;
- turn storage;
- create/switch/resume conversations;
- infrastructure voice commands.

### Milestone 9 — Management UI

- device list/status;
- discovery/network diagnostics;
- local wake model/sensitivity configuration;
- local wake-VAD configuration/diagnostics;
- command-endpointing configuration;
- audio diagnostics;
- STT/TTS configuration;
- assistant configuration;
- conversation management;
- releases/deployments/update policy;
- rollout/rollback status.

### Milestone 10 — Productization

- robust bootstrap/install;
- wake/VAD asset reconciliation;
- automated stable-channel policy after sufficient validation;
- supervisor upgrade strategy;
- stronger device authentication/mTLS if useful;
- Raspberry Pi/ARM64 validation;
- multi-Dot arbitration;
- optional local command endpointing / barge-in improvements.

---

## 25. First Engineering Tasks

1. Create Go module/package skeleton and CI.
2. Port/adapt the minimum EchoLocal microphone path.
3. Port/adapt the EchoLocal local wake-model layer and one known working model.
4. Add a local wake-VAD gate consistent with upstream openWakeWord behaviour for the openWakeWord path.
5. Build `echoctl mic record`, `echoctl wake test` and VAD diagnostics before gateway integration.
6. Verify local CPU/memory, wake score, VAD score, false-trigger and false-reject behaviour on the real Dot.
7. Define `_echo-satellite._tcp.local.` discovery record and implement gateway advertisement.
8. Implement minimal gateway WSS server and satellite discovery/connection.
9. Define `turn.start` + binary audio framing.
10. Implement the stable supervisor and application A/B slot layout.
11. Implement verified staging and local trial/rollback logic.
12. Define signed release manifest format and local dev-build override.
13. Add update protocol states and `dotsim` update simulations.
14. Implement gateway release cache + one-device deploy.
15. Deliberately deploy a broken binary and prove automatic rollback without ADB.
16. Add staged fleet rollout and stop-on-rollback behaviour.
17. On accepted local wake, stream command PCM to gateway and write it to WAV.
18. Implement gateway command endpointing.
19. Implement gateway-to-Echo speaker playback and semantic LED state.
20. Add STT/TTS interfaces and local Whisper integration.
21. Add mock assistant end-to-end loop.
22. Add Hermes after hardware, local wake/VAD, transport and safe agent OTA are proven.

This sequence avoids debugging wake inference, update recovery, audio transport, STT and Hermes simultaneously, while delivering a safe remote iteration path early.

---

## 26. Architectural Questions to Validate on Hardware

- Which Echo microphone path/channel arrangement is best for both local wake inference and command capture?
- How directly can EchoLocal's current openWakeWord/microWakeWord code be reused while keeping dependencies and licensing clean?
- What is the cleanest local Silero-VAD implementation for the Go/ARM64 device path?
- What is the CPU/memory impact of local VAD plus openWakeWord on the Echo Dot Gen 2?
- Should wake VAD run before all wake engines or only where supported/beneficial?
- Which wake model should ship as the default?
- What wake and VAD thresholds give acceptable false-positive/false-negative behaviour in a real room?
- How much pre-roll prevents clipped command speech without polluting STT with the wake phrase?
- What is the cost of one versus multiple active local wake models?
- Whether beamforming should be reused from EchoLocal or initially bypassed.
- Whether AEC is required only for barge-in/full-duplex behaviour or earlier.
- Exact speaker format and best resampling location.
- Which supervisor/startup integration is safest on the existing Magisk-rooted FireOS installation?
- Can agent binaries and both slots live entirely under `/data` without modifying `/system` during normal updates?
- What exact trial timeout and fast-exit limits are reliable on Echo Dot Gen 2 boot/startup timing?
- Which local health checks are required before committing a new slot?
- What filesystem primitives on this FireOS build give reliable atomic symlink/file replacement and persistence semantics?
- What free-space floor should be required before staging a release?
- Which mDNS implementation works reliably on FireOS without unnecessary native dependencies?
- Whether Docker/WSL mDNS advertisement is visible on the physical LAN in the preferred deployment.
- Whether Hermes should provide STT/TTS initially or only assistant reasoning.

Resolve these with focused diagnostics, intentionally broken update builds and recordings rather than hidden complexity inside the daemon.

---

## 27. Target v0.1 Stack

| Component | Initial choice |
|---|---|
| Echo daemon | Go |
| Echo hardware reference | EchoLocal |
| Wake detection | **Local on Echo only** |
| Wake engines | EchoLocal-derived openWakeWord + microWakeWord support |
| Wake VAD | **Local on Echo; Silero/openWakeWord-compatible behaviour for OWW path** |
| Wake models | local TFLite models |
| Manual trigger | Action button / simulator |
| Local discovery | mDNS / DNS-SD, static URL fallback |
| Gateway service | `_echo-satellite._tcp.local.` |
| Device transport | WSS |
| Control frames | JSON |
| Audio frames | binary PCM during active turns |
| Gateway | Go |
| Command endpointing | gateway-side initially, separate from wake VAD |
| Agent update architecture | **application-level A/B slots under `/data`** |
| Agent supervisor | small stable external startup/recovery component |
| Update orchestration | gateway Update Manager |
| Artifact delivery | authenticated HTTPS from gateway |
| Artifact integrity | size + SHA-256 |
| Production artifact trust | signed release manifest, preferably Ed25519 |
| Rollback | automatic on trial failure + explicit manual rollback |
| Rollout | staged; concurrency 1 initially; stop on failure/rollback |
| Release sources | GitHub Releases + local uploaded builds |
| Persistence | SQLite |
| Management UI | React + Vite, embedded in Go |
| Provisioning CLI | Go + go-flags |
| STT | local faster-whisper worker initially |
| Languages | English + Ukrainian |
| TTS | provider abstraction; Hermes or local |
| Assistant | Hermes adapter + mock adapter |
| Local dev | Windows + WSL2 |
| Device bootstrap/recovery | Windows ADB called from WSL |
| Normal device iteration | gateway A/B OTA once implemented |
| Integration testing | `dotsim` + WAV/wake/VAD/update fixtures |
| Gateway deployment | Docker Compose |

All four binaries use `jessevdk/go-flags` rather than a per-command flag library, so
CLI flags, environment variables and ini config files share one precedence model
(flag, then environment, then config file, then default). This supersedes the earlier
`Go + Cobra` choice for the provisioning CLI.

---

## 28. Core Boundaries

### Voice boundary

```text
Echo Dot
    |
    | local microphone
    |   -> preprocessing
    |   -> local wake VAD
    |   -> local wake inference
    |      (audio stays on device while idle)
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
    +-- command endpointing
    +-- STT Provider       -> Whisper / Hermes / other
    +-- Assistant Backend -> Hermes / OpenClaw / other
    +-- TTS Provider       -> Hermes / local / other
```

The key voice rule is that **wake recognition, including the VAD used to validate wake candidates, belongs to the satellite**. The gateway begins processing only after the device has decided that a voice turn has started.

### Update boundary

```text
Release source
    |
    v
Gateway Update Manager
    |
    | choose desired release
    | cache/verify artifact
    | stage fleet rollout
    v
paired Echo Dot
    |
    | download + verify artifact
    v
inactive echod slot
    |
    | atomic activation
    v
stable supervisor
    |
    | trial -> healthy -> commit
    |       or
    | trial failure -> previous slot rollback
    v
running echod
```

The key update rule is that **the gateway manages desired software state, while the Dot independently preserves a known-good local recovery path**. A bad release or unreachable gateway must not require ADB to restore the previously working agent.
