# Echo Satellite — High-Level Design and Implementation Plan

## 1. Purpose

Echo Satellite repurposes a rooted Amazon Echo Dot Gen 2 as a self-hosted network voice terminal.

The Echo Dot provides the hardware-facing capabilities — microphone capture, **local voice activity detection for wake gating**, **local wake-word detection**, speaker playback, LEDs, buttons, volume and mute — while a separate gateway performs speech recognition, assistant orchestration, conversation management, speech generation, fleet management and agent updates.

Hermes is the first assistant backend, not part of the device protocol. The same satellite should be able to work later with OpenClaw, a Raspberry Pi-hosted assistant, another local agent, or a cloud service without rewriting the device agent.

The project also treats deployed Echo devices as a small managed fleet. After the
initial rooted-device bootstrap, the gateway chooses the desired agent release
and the device safely verifies, stages and atomically replaces its single
installed agent. Ordinary Echo Satellite updates must not require reflashing
FireOS, boot, recovery or system partitions. A bad committed replacement has no
device-local rollback path: if the new client cannot reconnect, recovery
requires ADB.

This document defines the initial architecture and implementation direction. Details that depend on real Echo Dot hardware should be validated experimentally rather than treated as assumptions.

The single-agent deployment decision supersedes the multi-copy recovery
contracts recorded in earlier finished plans. Those historical plans are kept
unchanged as execution records; their slot, pre-commit health, local recovery
component and device-local fallback requirements were superseded before the
deployment implementation began and are not current design requirements.

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
- Safely verify, stage and atomically replace the single installed agent under
  `/data`.
- Make recovery limits explicit: a connected client can receive an older
  release, while an agent that cannot reconnect requires ADB recovery.
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
- Building an automatic device-local rollback mechanism or preserved agent
  fallback.

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

Upstream openWakeWord offers a Silero VAD option, whose same-step
`vad_threshold` rule is a reference behaviour rather than this device's
runtime. Echo Satellite's qualified openWakeWord path uses an adapted local
level VAD and accepts only when its effective (bounded-lookback) VAD score is
above the configured threshold; see §16 for the measured configuration.

The gateway may configure the active wake model, wake threshold and local VAD settings and may receive wake diagnostics, but it does not receive a continuous microphone stream and does not score wake words or wake VAD itself.

A manual Action-button / push-to-talk trigger may start a turn without a wake word for development, accessibility and recovery. This is an alternate **trigger**, not an alternate wake-word engine.

### 3.3 Wake VAD and command endpointing are separate concerns

There are two different uses of voice activity detection in the system:

1. **Wake VAD — device-local, always-on.** It helps decide whether a wake-word score is credible speech and should be allowed to trigger.
2. **Command endpointing — after a wake/button trigger.** It decides when the user's spoken command has ended so STT can proceed.

For v0.1, command endpointing runs on the device after a local wake/button
trigger. Active-turn audio streams upstream while the window is open, and the
device closes that window when endpointing decides the command is complete.

The two functions have separate configuration and thresholds.

### 3.4 The gateway owns assistant and fleet orchestration

The gateway is responsible for:

- advertising its local endpoint using mDNS;
- device registration and configuration;
- turn lifecycle after a local wake/button trigger;
- consume device-endpointed active-turn audio for STT;
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
    "update.single.v1"
  ]
}
```

Normal product behaviour is negotiated by capability, not by checks such as `version >= X`.

Release manifests may still declare minimum protocol compatibility because it
is an **installation safety constraint**, not runtime feature negotiation.

### 3.7 Agent updates are application-level, not FireOS OTA

Echo Satellite replaces one **Echo Satellite agent binary** under `/data`. This
is not Android partition updating and not Amazon's FireOS OTA mechanism.

A normal agent update must never write the Echo's bootloader, boot image,
recovery or system partition.

### 3.8 The gateway owns desired version; the device owns safe installation

The gateway selects and records the desired agent version. The device does not
poll external release sources or choose its own release.

Before replacing the installed binary, the device must:

- fetch the signed manifest, signature and artifact from authenticated URLs;
- validate manifest compatibility and require offer metadata to match it;
- verify sufficient free space, exact size, SHA-256 and production signature;
- write and fsync a same-directory `.part` staging file;
- atomically rename that verified file over the installed `echod`;
- fsync the containing directory where supported;
- request a controlled restart.

Any failure before the atomic rename leaves the currently installed executable
untouched. After that rename succeeds, the replacement is committed: there is
no preserved fallback binary or automatic device-local recovery.

### 3.9 Recovery is deliberately operator-assisted

When the agent remains connected, gateway rollback means deploying a previous
signed compatible release through the same installation flow. It does not mean
switching to a preserved local copy, and downgrade deployment must not be
blocked merely because its semantic version is lower.

If a committed agent cannot start or reconnect, the gateway cannot repair it.
An operator must use ADB and `echoctl update install` to install a known-good
signed release. The initial bootstrap and operational documentation must keep
that recovery route available and tested.

### 3.10 The launcher is not recovery infrastructure

A minimal Magisk `service.d` launcher starts the single installed `echod`,
restarts a controlled update exit immediately, and applies bounded exponential
backoff to unexpected repeated exits. It does not inspect update health, choose
versions, alter installed files, or perform rollback.

### 3.11 Hardware-independent development where possible

The protocol and gateway should be testable against a simulated device (`dotsim`) that feeds WAV files instead of microphone hardware and writes playback audio to disk instead of a speaker.

The simulator must also emulate verified staging, replacement, restart,
reconnect, failure and offline-after-replacement outcomes so the gateway's
rollout logic can be tested without intentionally breaking a real Echo.

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
- size + SHA-256 verification.

EchoLocal already provides a useful Go-native local wake stack and supports both openWakeWord and microWakeWord model kinds. Echo Satellite should reuse/adapt those low-level pieces rather than inventing a separate wake inference stack.

EchoLocal's current Go openWakeWord path does not include the upstream Silero
VAD gate. Echo Satellite therefore ships an additional adapted local level-VAD
component; §26 records why a pure-Go Silero runtime is blocked and why this is
not Silero-equivalent.

EchoLocal's updater remains a useful reference for downloading completely and
verifying size/hash before installation. Its retained-binary and boot-hook
recovery model is not part of Echo Satellite's approved single-agent design.

### openWakeWord

Repository: `dscripka/openWakeWord`

Reference for:

- 16 kHz PCM input expectations;
- wake-model scoring;
- wake activation thresholds;
- optional Speex noise suppression;
- upstream bundled Silero VAD gating through `vad_threshold`;
- upstream's simultaneous-VAD acceptance rule, which Echo Satellite adapts to
  its independently qualified effective-VAD lookback rule.

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
- controller-managed fleet deployment;
- transfer and verification before replacement;
- release discovery and controller-side artifact caching;
- keeping provisioning-time payloads in sync after deployment.

EchoMuse's controller-side wake architecture and its multi-copy OTA recovery
model are not the target architectures for Echo Satellite. The useful update
lessons are gateway ownership of releases, pre-install verification and
operational reconciliation.

Another important operational lesson is that anything placed on the Dot only during provisioning will eventually drift unless it has a reconciliation/update path. Echo Satellite should therefore treat the agent binary, wake assets, configuration and launcher assets as separately versioned desired state.

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
| Magisk launcher -> echod     |       | mDNS advertisement             |
| single binary under /data    |       | device manager                 |
| verified atomic replacement |       | turn/conversation managers     |
|                              |       | endpointed-turn receiver       |
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
  -> device-local command endpointing closes audio window
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
  -> device verifies manifest, compatibility and free space
  -> download artifact to echod.part
  -> verify exact size + SHA-256 + signature
  -> fsync and atomically replace echod
  -> persist installed-release metadata
  -> controlled exit; Magisk launcher restarts echod
  -> new agent reconnects and reports its version/build

failure before atomic replacement
  -> installed echod remains unchanged

bad committed replacement that cannot reconnect
  -> operator restores a known-good signed release with ADB
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
│   │   ├── endpointing/       # post-wake command endpointing
│   │   ├── wake/              # local wake engines, wake VAD, models
│   │   ├── update/            # staging, verification, atomic installation
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
│   └── launcher/              # minimal Magisk service.d launcher
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
│   └── updates/               # manifests + deployment failure fixtures
└── README.md
```

Keeping the first versions in one repository simplifies coordinated protocol,
launcher and agent changes.

---

## 7. Device Agent (`echod`) and Launcher

### 7.1 `echod` responsibilities

`echod` runs as a service on the rooted Echo Dot, started by a minimal Magisk
launcher.

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
- stage, verify and atomically install agent updates when instructed by the
  gateway;
- persist installed-release metadata;
- request a controlled restart after committing a replacement.

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

### 7.3 Installed agent layout

Proposed application layout:

```text
/data/local/bin/
  echod                     # single installed agent
  echod.part                # temporary same-directory staging path

/data/local/etc/echo-satellite/
  installed-release.json    # installed version/build/digest metadata
  config.*
  credentials/
  wake-models/
```

The exact path can change only if hardware validation requires it; the
single-installed-binary and same-directory atomic-replacement semantics remain.
The `.part` file is never a runnable fallback and is removed or replaced on the
next safe installation attempt.

### 7.4 Minimal Magisk launcher

A small `service.d` shell script lives outside the agent binary. It should:

- verify that `/data/local/bin/echod` exists and is executable before launch;
- start `echod`;
- restart the documented controlled-update exit immediately;
- apply bounded exponential backoff to unexpected repeated exits;
- log only enough bounded information to diagnose launch/restart failures.

It must not inspect deployment health, select a release, rewrite the installed
binary or perform rollback. Bootstrap must install it in the service directory
actually consumed by the installed Magisk version. The qualified Dot runs
Magisk v17.3, where that persistent directory is
`/sbin/.core/img/.core/service.d`; its daemon does not consume the modern
`/data/adb/service.d` path. Bootstrap must recognize this qualified legacy
layout (and may recognize the modern layout on explicitly supported newer
Magisk versions), preserve any conflicting hook, and fail closed when it cannot
identify a supported service directory. Exit code 75 is the controlled-update
restart signal. Hardware evidence for boot timing, exit propagation, restart,
backoff and cleanup is recorded in
[`docs/device-diagnostics.md`](device-diagnostics.md).

### 7.5 Device state

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

The gateway publishes independently on every up, multicast-capable,
non-loopback interface that has a usable assigned address. Each interface's
A/AAAA records contain only addresses assigned to that interface, so a WLAN
response never claims that a WSL, VM, VPN, or container-only address is
reachable on the WLAN. Explicitly configured advertisement addresses restrict
publication to the interfaces that own them. Registration fails if no validated
interface/address pair exists; it must not publish a local-only or addressless
record.

An `echod` browse is explicitly confined to the Dot's infrastructure `wlan0`
interface; host tools retain normal all-interface browsing. When a compatible
response contains addresses from several interfaces, the browser retains them
but prefers an address sharing the browse interface's subnet. This selection is
only endpoint routing; WSS/TLS and device authentication remain mandatory.

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
  -> Magisk launcher starts the installed echod
  -> echod initializes mic + local wake VAD + wake engine
  -> load explicit/previous gateway configuration
  -> if necessary discover gateway over mDNS
  -> connect/authenticate over WSS
  -> hello(device id, version, capabilities, wake config, update state)
  <- welcome/config
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
update.confirmed
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
- installed-release metadata;
- single-agent update capability;
- current update status;
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

### 9.4 Command-audio receiver

Receives active-turn audio while the device-controlled window is open and
validates its protocol framing for downstream STT. The gateway does not decide
when the spoken command is complete or close the input window. Command
endpointing runs on the device and remains distinct from wake VAD.

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
- ensure target device capability and release compatibility;
- create deployment records;
- issue update offers;
- monitor progress, restart, reconnect, confirmation and failure;
- limit update concurrency;
- support canary/staged deployments;
- stop or pause rollout on failures or clients that do not reconnect;
- deploy a previous signed compatible release as gateway rollback when the
  client remains reachable;
- keep release notes and deployment outcomes available in the UI.

The Dot should not independently poll GitHub for releases. The gateway is the fleet control plane and performs external release discovery once for the whole deployment.

---

## 10. Single-Agent Update Architecture

### 10.1 Scope

**Deployment replaces only the Echo Satellite agent under `/data`.** It is not
an Android partition update and is not a replacement for the original
rooting/unbrick process.

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
    "protocol_min": 1,
    "protocol_max": 1,
    "architecture": "linux-arm64"
  },
  "artifact_url": "https://gateway/.../artifacts/01J...?token=...",
  "manifest_url": "https://gateway/.../manifests/01J...?token=...",
  "signature_url": "https://gateway/.../manifests/01J...sig?token=..."
}
```

All three URLs should be short-lived and scoped to the deployment/device. The
offer's version, build ID, size and SHA-256 must exactly match the verified
manifest; unsigned offer metadata is not authoritative.

### 10.3 Device-side staging flow

```text
receive update.offer
  -> verify update capability and local eligibility
  -> fetch manifest + detached signature
  -> verify signature and manifest compatibility
  -> require offer metadata to match the manifest
  -> verify sufficient free space
  -> download to /data/local/bin/echod.part
  -> stream-compute SHA-256
  -> verify expected size
  -> chmod and fsync staged executable
  -> atomically rename echod.part over echod
  -> fsync containing directory where supported
  -> persist installed-release metadata
  -> report restarting
  -> exit with the controlled update code
  -> Magisk launcher starts the installed echod
```

Before the atomic rename, every rejection or interruption leaves the installed
`echod` pathname untouched. The running process may continue from its unlinked
inode after rename until controlled exit, but the old executable is not retained
as a recovery copy.

### 10.4 Commit and reconnect

The atomic rename commits the new agent. After restart, the new process reports
its installed version/build in `hello`; the gateway uses that reconnect as the
deployment confirmation signal. A successful pre-install verification does not
guarantee that the committed process will start or reconnect.

If it does not reconnect, the gateway marks the attempt failed/offline, stops
the rollout according to policy and surfaces that ADB recovery is required. It
does not claim that the device restored itself.

### 10.5 Recovery and rollback

There is no device-local fallback executable after commit and no automated
recovery from a bad replacement.

```text
connected agent
  -> gateway offers a previous signed compatible release
  -> device performs the normal verified replacement flow

agent cannot start or reconnect
  -> operator connects with ADB
  -> echoctl update install <known-good-release>
  -> launcher starts the restored echod
  -> operator verifies reconnect and reported version/build
```

Gateway rollback is therefore a new audited deployment whose target is an older
release, not a local switch. Version comparison must allow an authenticated,
verified downgrade.

### 10.6 Update state machine

Suggested gateway-visible state:

```text
idle
available
queued
downloading
verifying
staged
restarting
confirmed
failed
cancelled
```

A device should report the current phase and progress when meaningful.

### 10.7 Updating ancillary device payloads

Not every device-side file belongs to the agent installation.

Treat deployed state explicitly:

```text
agent binary           -> verified single-agent replacement
wake/VAD models        -> authenticated asset synchronization
normal device config   -> config synchronization
credentials            -> explicit secure rotation/provisioning
Magisk launcher        -> bootstrap/explicit maintenance only
```

Every payload installed during bootstrap must either be immutable by design or have a reconciliation path for already-deployed devices.

### 10.8 Launcher maintenance

The launcher is installed during bootstrap and is not delivered through the
agent update path. Changes require an explicit operator maintenance action and
real-device qualification. The launcher has no version compatibility field in
the agent release manifest.

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

Keep at least the currently rolling release and a recent known-good release in
the cache. That older release enables a connected-device downgrade or an ADB
recovery install; cache eviction must not be presented as device-local safety.

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
  -> wait for confirmed reconnect on the expected version/build
  -> next device(s)
  -> remaining fleet
```

Initial default:

```text
max_concurrent_updates = 1
stop_on_failure = true
```

If the canary fails or does not reconnect within policy, the gateway should stop
the remaining automatic rollout and surface the reason and recovery route.

### 12.4 Eligibility

Before deploying, check:

- device online and approved;
- not currently in a voice turn/update;
- `update.single.v1` capability present;
- architecture matches;
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

If the selected STT provider needs whole utterances, the gateway buffers the already endpointed active turn. Streaming STT can be added later without changing the local wake architecture.

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
installed_release_digest
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
command endpointing:  device, after turn.start
STT:                  gateway-side provider
```

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
  threshold: 0.50       # measured on the qualified Dot; see docs/device-diagnostics.md
  vad:
    enabled: true
    threshold: 0.50     # measured on the qualified Dot; see docs/device-diagnostics.md
    lookback_ms: 1200   # measured effective-VAD lookback
  preroll_ms: 600       # measured shortest value preserving the first command word
```

There is deliberately no gateway wake mode.

Wake and VAD scores must not be assumed to be temporally aligned on the same PCM
step. A wake engine may score a temporal receptive field and emit its peak only
after speech has ended. For the qualified `okay_nabu` pipeline, the acceptance
gate uses the maximum recent VAD score from a bounded 1,200 ms lookback rather
than only the instantaneous VAD score. The selection was measured against
false-accept and false-reject traces; each newly qualified model must repeat
that comparison. Every candidate remains entirely device-local.

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

The qualified `okay_nabu` pipeline evaluates wake inference on every step even
when instantaneous VAD is below threshold (`always_score_wake=true`); VAD gates
acceptance, not diagnostic scoring. Its measured defaults are specific to the
openWakeWord engine, adapted level VAD, 16 kHz mono channel 0, 1,280-sample
steps and bypass preprocessing. The evidence and limitations are in
[`docs/device-diagnostics.md`](device-diagnostics.md); model replacement and
qualification are documented in [`docs/wake-model-training.md`](wake-model-training.md).

### Wake model distribution

Wake/VAD models are device assets, not agent releases.

Initially, `echoctl` may install/update them. The gateway should later synchronize selected/required signed or otherwise trusted assets independently of the agent binary.

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
- installed-release digest;
- current update state;
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
- queued/current deployment progress;
- restarting/confirmed/failed state;
- last failure and whether ADB recovery is required;
- deploy selected release to one device;
- staged fleet deploy;
- deploy a previous release to a connected device;
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
echoctl update install <release>     # local/ADB recovery
echoctl update channel <stable|beta|dev> [device]
```

For normal post-bootstrap operation, update commands should go through the gateway control plane rather than requiring ADB.

ADB remains the development/recovery mechanism.

Hardware diagnostic commands are cross-built for `linux/arm64` and run on the
Dot through `adb shell`; they do not proxy hardware access from the host.
`echoctl wake install` accepts only a local model path and its required
SHA-256 digest. `echoctl status --json` is the machine-readable bug-report
artifact, combining identity, hardware probes, wake configuration/model
inventory, wake statistics and resource usage.

### Initial bootstrap flow

The expected starting point is an already rooted/Magisk-enabled Dot. Normal installation should avoid reflashing the boot image.

```text
find ADB device
  -> verify expected hardware/root
  -> inspect existing installation
  -> back up changed state
  -> install minimal Magisk service.d launcher
  -> install first echod at /data/local/bin/echod
  -> install default wake/VAD assets
  -> push config/credentials
  -> handle conflicting Alexa services as required
  -> start launcher/echod
  -> discover/configure gateway
  -> verify gateway connectivity
  -> microphone test
  -> local wake + VAD test
  -> speaker test
  -> verify installed version/build and update status
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

Milestone 2 uses a shared bearer token solely for development. TLS verification remains enabled by default; `tls_skip_verify` is a visible development-only escape hatch, not a production configuration. Per-device credentials and mTLS remain required follow-up work before production use.

The update subsystem is security-sensitive: permission to deploy an agent is effectively permission to run privileged code on a rooted Dot. Update/deployment endpoints must therefore require strong administrator authorization and an audit trail.

Local wake detection and local wake VAD provide a privacy benefit: while idle, microphone audio needed for activation decisions stays on the device rather than being continuously sent to the gateway.

Future improvement: per-device credentials and client certificates / mTLS.

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
state
progress
started_at
confirmed_at
recovery_required
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
phase
bytes_downloaded
artifact_size
verification_result
atomic_replace_completed
restart_requested
confirmed
reconnected_version
recovery_required
failure_stage
```

The agent and launcher should keep bounded deployment/restart logs under `/data`.
They must not duplicate verbose logs or be allowed to fill device storage.

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
- single-agent update capability;
- update download progress;
- successful restart into new version;
- verification failure before replacement;
- repeated crash/fast-exit behaviour;
- failure to reconnect after committed replacement;
- connected-device downgrade deployment and ADB recovery outcomes.

Gateway integration tests should be able to drive a fake fleet such as:

```text
3 devices
  -> deploy release
  -> device 1 confirms
  -> device 2 fails to reconnect
  -> verify device 3 is not updated when stop_on_failure=true
  -> record that device 2 requires ADB recovery
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
  |     +-- adb -> Echo Dot (main adb path)
  |
  +-- Docker Desktop / WSL backend
  |     +-- gateway
  |     +-- speech-worker
  |     +-- Hermes/test dependencies
  |
  +-- Windows Android Platform Tools
        +-- adb.exe -> Echo Dot (alternate adb path if USB passthrough is not activated)
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

Once single-agent deployment is working, normal iteration on agent builds should
also support:

```text
build signed/dev release bundle
  -> upload to local gateway
  -> deploy to the test Dot
  -> observe atomic replacement, restart and reconnect
  -> use ADB recovery if a bad committed build cannot reconnect
```

This becomes the preferred device iteration loop because it exercises the same mechanism used in real deployments.

mDNS acceptance is performed with a native gateway on the physical LAN. Docker
Compose deliberately disables mDNS and is smoke-tested only through an explicit
WSS URL; it cannot establish multicast reachability. Explicit host/port remains
the fallback.

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

Deployment documentation must distinguish the Compose explicit-WSS smoke test
from native-gateway mDNS acceptance on the physical LAN. Static gateway
configuration remains the universal fallback.

Future target: Raspberry Pi / ARM64 host without architectural changes.

### Satellite lifecycle

Satellite deployment is a different lifecycle problem from gateway deployment:

```text
one-time rooted bootstrap via ADB
  -> install launcher + single agent + credentials

normal operation
  -> gateway-managed configuration/assets
  -> gateway-managed single-agent updates
  -> ADB required only when a committed client cannot reconnect
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

### Milestone 3 — Single-agent deployment and command-audio conditioning

Implement this **before Hermes integration** so subsequent device development can use the production update path.

- qualify and implement the minimal Magisk launcher;
- define the single installed-agent layout under `/data`;
- implement same-directory `.part` staging;
- size + SHA-256 verification;
- release signature verification;
- manifest/offer compatibility verification;
- atomic executable replacement and directory persistence where supported;
- controlled exit and launcher restart;
- installed version/build reporting after reconnect;
- connected-device downgrade deployment;
- ADB recovery installation and runbook;
- simulator replacement/reconnect/failure tests.
- characterize all seven physical microphone channels on the qualified Dot;
- retain the single capture path while comparing channel 0, an unsteered mix,
  and a steerable delay-and-sum beamformer;
- add bounded capture gain and output leveling with clipping, gain, and
  speech/noise metrics rather than a blind fixed boost;
- requalify local wake and command endpointing with the selected preprocessing,
  including continuous quiet speech, deliberate silence, and the 60-second
  hard timeout.

Success criteria: a valid build replaces the installed agent and reconnects; a
pre-commit failure leaves the installed agent untouched; a deliberately broken
committed build is recoverable through the documented ADB path; and the
qualified command-audio path preserves continuous quiet speech until the
60-second cap while still endpointing deliberate silence.

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
- stop-on-failure or missing reconnect;
- update status API.

Success criterion: a gateway can update several devices sequentially, stop a
rollout after a simulated/real failure or missing reconnect, and deploy an
older release to a connected client.

### Milestone 5 — Local-wake speech loop

```text
Echo local wake / simulator trigger
  -> device-endpointed turn audio
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
- rollout/recovery status.

### Milestone 10 — Productization

- robust bootstrap/install;
- wake/VAD asset reconciliation;
- automated stable-channel policy after sufficient validation;
- launcher maintenance strategy;
- stronger device authentication/mTLS if useful;
- Raspberry Pi/ARM64 validation;
- multi-Dot arbitration;
- optional barge-in improvements.

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
10. Qualify the Magisk launcher, `/data` atomic replacement, controlled restart,
    free-space margin and ADB recovery path on the Dot.
11. Implement verified same-directory staging, atomic replacement, installed
    metadata and controlled restart.
12. Define signed release manifest format and local dev-build override.
13. Add update protocol states and `dotsim` update simulations.
14. Implement gateway release cache + one-device deploy.
15. Deliberately deploy a broken binary and prove the documented ADB recovery.
16. Add staged fleet rollout, stop-on-failure behaviour and connected-device
    downgrade deployment.
17. On accepted local wake, stream command PCM to gateway and write it to WAV.
18. Implement device-local command endpointing.
19. Implement gateway-to-Echo speaker playback and semantic LED state.
20. Add STT/TTS interfaces and local Whisper integration.
21. Add mock assistant end-to-end loop.
22. Add Hermes after hardware, local wake/VAD, transport and safe agent OTA are proven.

This sequence avoids debugging wake inference, deployment, audio transport, STT
and Hermes simultaneously, while delivering a verified remote iteration path
with an explicit ADB recovery boundary early.

---

## 26. Architectural Questions to Validate on Hardware

- **Microphone path/channel arrangement:** card 0, device 24, 16 kHz, nine
  interleaved S24_3LE channels; select physical microphone channel 0 and
  exclude channels 7--8, which are playback loopback. Evidence:
  [`docs/device-diagnostics.md`](device-diagnostics.md).
- **EchoLocal reuse:** copied and adapted rather than imported because its
  packages are Go `internal` packages. The adapted MIT code includes NEON
  `Dot`/`AXPY` assembly, with a portable `noasm` fallback. Evidence:
  [`docs/device-diagnostics.md`](device-diagnostics.md) and
  [`docs/third-party-notices.md`](third-party-notices.md).
- **Local Silero VAD:** blocked for the pure-Go ARM64 path. `onnx-go` lacks
  LSTM/GRU/RNN support; `gonnx` cannot extend its unexported opset-13 registry,
  and the evaluated Silero exports require unsupported operators (`If`, `Pad`,
  `Pow`, `ReduceMean`, `Sqrt`, and, by export, others). The shipped VAD is the
  adapted room-adaptive level detector; it is deliberately not
  Silero-equivalent. Evidence: [`docs/device-diagnostics.md`](device-diagnostics.md).
- **CPU/memory impact:** corrected streaming benchmark: NEON p50/p95 total
  wake+VAD 42.616/80.361 ms at 100.6% CPU and 14,553,088 bytes RSS; `noasm`
  65.342/125.880 ms at 100.6% CPU and 16,019,456 bytes RSS. This fails the
  <=20 ms combined target. The local-wake vertical slice is accepted with this
  known performance limitation because it repeatedly meets the 80 ms cadence
  in the qualified use case. Before making a production performance claim,
  evaluate a TFLite binding/runtime for the streaming embedding path; it must
  preserve the device-local wake boundary and be qualified on the Dot. Evidence:
  [`docs/device-diagnostics.md`](device-diagnostics.md).
- **Wake-VAD engine scope:** deferred. One openWakeWord engine exists; the
  per-engine VAD setting remains in `wake.Config`, so a future engine can make
  its own qualified decision without changing the device-local boundary.
- **Default model:** `okay_nabu`, qualified with the primary and additional
  speaker sessions. Evidence: [`docs/device-diagnostics.md`](device-diagnostics.md).
- **Thresholds:** wake 0.50, level-VAD 0.50, and 1,200 ms VAD lookback are the
  measured `okay_nabu` defaults. Evidence:
  [`docs/device-diagnostics.md`](device-diagnostics.md).
- **Pre-roll:** 600 ms is the measured shortest value that retains the first
  command word without retaining the wake phrase. Evidence:
  [`docs/device-diagnostics.md`](device-diagnostics.md).
- **Command endpointing:** on the qualified Dot/room, the independently
  configured level-VAD speech threshold is 0.05. The original 0.50 and an
  intermediate 0.20 setting falsely endpointed continuous quiet counting;
  0.05 retained the 10-count test and then endpointed after a measured 1.563 s
  final quiet region with the configured 1,500 ms trailing-silence limit.
  This is a gateway profile calibration, not a wake-VAD setting or a change to
  device-local endpointing ownership. Evidence: Task 11 in
  [`docs/plans/in-progress/2026-08-27-milestone-2-discovery-protocol-simulator.md`](plans/in-progress/2026-08-27-milestone-2-discovery-protocol-simulator.md).
- What is the cost of one versus multiple active local wake models?
- **Beamforming:** beamforming initially bypassed; the `Preprocessor` seam reserves it for
  later hardware qualification. Evidence:
  [`docs/device-diagnostics.md`](device-diagnostics.md).
- **AEC:** deferred to barge-in/full-duplex work; it is not required for this
  one-way local wake slice. Evidence:
  [`docs/device-diagnostics.md`](device-diagnostics.md).
- **Speaker format/resampling:** ALSA playback is 48 kHz stereo S16_LE,
  1,024-frame periods and four periods. Canonical audio remains 16 kHz mono;
  resampling and channel duplication occur on-device immediately before the
  PCM sink. Evidence: [`docs/device-diagnostics.md`](device-diagnostics.md).
- **Magisk launcher:** on the qualified Dot, Magisk v17.3 executed a root-owned
  hook from `/sbin/.core/img/.core/service.d` at 7.19 seconds uptime. It did not
  execute a hook from the modern `/data/adb/service.d` path. Exit 75 propagated
  distinctly and triggered an approximately 20 ms immediate restart; ordinary
  exit 1 honored measured 1, 2, 4, 8, 16, 32 and 60 second delays. Evidence:
  [`docs/device-diagnostics.md`](device-diagnostics.md).
- **Atomic agent replacement:** ext4 under `/data` supports staged-file fsync,
  same-directory atomic rename, containing-directory fsync and reboot
  persistence. Replacing the pathname left the old process running from its
  deleted inode until controlled shutdown. Evidence:
  [`docs/device-diagnostics.md`](device-diagnostics.md).
- **Update staging margin:** retain artifact size plus
  `max(16 MiB, 10% of artifact size)`. The 8,126,626-byte measured agent needed
  24,903,842 bytes against at least 336,482,304 bytes observed available, with
  more than 63,000 free inodes. Evidence:
  [`docs/device-diagnostics.md`](device-diagnostics.md).
- Can the documented `echoctl update install` procedure reliably restore a
  known-good signed agent over ADB after the installed agent cannot start or
  reconnect?
- Which mDNS implementation works reliably on FireOS without unnecessary native dependencies?
- Whether Docker/WSL mDNS advertisement is visible on the physical LAN in the preferred deployment.
- Whether Hermes should provide STT/TTS initially or only assistant reasoning.

Resolve these with focused diagnostics, intentionally broken update builds,
ADB recovery drills and recordings rather than hidden complexity inside the
daemon.

---

## 27. Target v0.1 Stack

| Component | Initial choice |
|---|---|
| Echo daemon | Go |
| Device implementation | Pure Go with `CGO_ENABLED=0`; Go assembly is permitted with a portable `noasm` fallback |
| Echo hardware reference | EchoLocal |
| Wake detection | **Local on Echo only** |
| Wake engines | EchoLocal-derived openWakeWord + microWakeWord support |
| Wake VAD | **Local on Echo; adapted room-level VAD with a qualified effective-VAD lookback (not Silero-equivalent)** |
| Wake models | local TFLite models |
| Manual trigger | Action button / simulator |
| Local discovery | mDNS / DNS-SD, static URL fallback |
| Gateway service | `_echo-satellite._tcp.local.` |
| Device transport | WSS |
| Control frames | JSON |
| Audio frames | binary PCM during active turns |
| Gateway | Go |
| Command endpointing | device-local, separate from wake VAD |
| Agent update architecture | **single agent under `/data`, verified same-directory atomic replacement** |
| Agent launcher | minimal Magisk `service.d` launcher; no health or recovery decisions |
| Update orchestration | gateway Update Manager |
| Artifact delivery | authenticated HTTPS from gateway |
| Artifact integrity | size + SHA-256 |
| Production artifact trust | signed release manifest, preferably Ed25519 |
| Rollback | deploy a previous release while connected; ADB recovery otherwise |
| Rollout | staged; concurrency 1 initially; stop on failure/missing reconnect |
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
| Normal device iteration | gateway single-agent deployment once implemented |
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
verified echod.part
    |
    | atomic rename over installed echod
    v
running echod
```

The key update rule is that **the gateway owns desired agent version, while the
Dot owns safe pre-install verification and atomic replacement**. Failure before
the rename leaves the installed agent untouched. After a bad committed agent,
gateway rollback means deploying a previous release only while the client is
reachable; if it cannot reconnect, recovery requires ADB. Normal deployment
must never write FireOS, bootloader, boot-image, recovery or system partitions.
