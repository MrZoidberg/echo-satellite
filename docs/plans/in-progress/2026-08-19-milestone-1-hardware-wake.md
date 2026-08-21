# Milestone 1 — Hardware + local wake vertical slice implementation plan

**Status:** in-progress
**Owner or active agent:** Codex (/root)
**Created:** 2026-08-19
**Updated:** 2026-08-21
**Started:** 2026-08-19
**Completed:** not completed

**Remaining work:** Tasks 15–25. Tasks 2–14 completed.

## Objective

Prove on real Echo Dot Gen 2 hardware that the device can capture its microphone to WAV, run a fully device-local VAD → openWakeWord pipeline that repeatedly and observably detects one selected wake model, play PCM through the speaker, and read and write the LED ring and buttons — with no gateway involvement of any kind, and with every `docs/DESIGN.md` §26 question this work touches measured on hardware and recorded back into the design document.

## Non-goals

Each exclusion names the milestone that owns it.

- Gateway WSS server, satellite WSS client, reconnect/backoff, `hello`/`welcome` on the wire — **Milestone 2**.
- mDNS advertisement and browse behind `discovery.Advertiser`/`Browser` — **Milestone 2**.
- `turn.start` on the wire, binary audio framing, `audio.start`/`audio.stop` windows, streaming command PCM — **Milestone 2**. This plan produces the *local* wake event and the pre-roll buffer Milestone 2 turns into a turn; it deliberately stops one step short of the wire.
- `dotsim` behaviour of any kind — **Milestone 2**. `cmd/dotsim` is not touched.
- Command endpointing and STT — **Milestone 5**. Only *wake* VAD lands here; the two uses of VAD must not be conflated (`AGENTS.md`, §3.3).
- Stable supervisor, A/B slots, `update-state.json`, trial/rollback — **Milestone 3**. `echod` is started by hand over ADB.
- Gateway-synchronized signed wake assets — **Milestone 4**. `echoctl` installs models from a local path with an explicit digest here.
- microWakeWord engine — deferred. `wake.Engine` and `wake.Kind` must not preclude it, but no microWakeWord runtime is ported.
- DSP: beamforming, noise suppression, AEC — deferred. This milestone ships the `Preprocessor` seam with a `Bypass` implementation and records "beamforming initially bypassed" as the §26 answer.
- Multiple simultaneously active wake models — one active model in v0.1 (§16); the pipeline holds a slice so the API does not preclude more.
- Wake-model **training tooling in this repository**. Task 24 documents the upstream openWakeWord pipeline; it does not reimplement it.
- Silero VAD. Blocked under `CGO_ENABLED=0`; the evidence is recorded in §26 by Task 25.

## Source references and constraints

- `docs/DESIGN.md` §4 (EchoLocal and openWakeWord as the implementation reference, including the explicit note that EchoLocal has no Silero VAD gate so wake VAD must be added), §6 (package layout), §7.1 (identity from serial, continuous capture, ring buffer, local wake pipeline, semantic LED, report health and logs), §7.2 (simplest known-good mic path; **one capture path feeds wake and active-turn streaming without reopening ALSA**; 16 kHz PCM), §7.3 (`/data/local/etc/echo-satellite/wake-models/`), §7.6 (device states), §16 (engines, model metadata, stable model IDs, the VAD gate pseudocode, pre-roll, configuration block, model distribution, diagnostics list), §18 (`echoctl` command names), §19 (serial is identity, not authentication), §20 (turn observability fields; raw microphone audio is not stored by default; device logs must be bounded and must not fill storage), §22 (`GOOS=linux GOARCH=arm64 CGO_ENABLED=0`; Windows `adb.exe` from WSL), §24 Milestone 1 (scope and success criterion), §25 tasks 2–6, §26 (hardware questions), §27 (target stack).
- `docs/plans/README.md` — authoritative template and lifecycle. Hardware-dependent tasks must name device state, give exact commands, and state what cannot be verified without hardware.
- `AGENTS.md` — go-flags in all binaries; the `echoctl` command-tree shape (`opts` with `command:` tags, `parseArgs → (opts, string, error)`, `dispatch(w, command, o)`); testify `require`/`assert`; one `_test.go` per source file; requirement-encoding test names; hand-written stub preferred over moq for small interfaces (`stubBrowser` precedent); `%w` across package boundaries with exported `Err*` sentinels; **no `init()`**; consumer-declared interfaces with the implementation in the provider package and wiring in `main`; `make lint` clean with no blanket suppressions; roughly 70% statement coverage or a written justification.

**Hard constraints**

- **Pure Go, `CGO_ENABLED=0`, static `linux/arm64`.** Go assembly (`.s`) is permitted and requires no cgo; cgo itself is not. `docs/development-windows-wsl.md`: "Keep it that way. A dependency that requires cgo turns a one-command build into a toolchain problem on every developer machine." Speex noise suppression is therefore rejected.
- **Tests must pass on linux/amd64 CI and darwin/arm64**, neither of which has `/dev/snd`, `/dev/input`, or the LED sysfs node. Build tags are confined to `internal/device/audio/alsa` and `internal/device/mixer`, both shipping a `!linux` stub returning `ErrUnsupportedPlatform`. LED, buttons and system use an injected root instead of build tags, so `t.TempDir()` is a complete test double.
- **Voice boundary.** Nothing here adds a gateway wake surface, a gateway VAD surface, or a path that ships idle microphone audio anywhere. Recording is operator-initiated only.
- **Model trust.** Wake models are device assets (§10.8, §16). `echod` never fetches a model. `echoctl wake install` takes a **local path plus a required expected SHA-256**, never a URL.
- Toolchain: Go 1.26, golangci-lint 2.12.2, module `github.com/MrZoidberg/echo-satellite`, repository MIT.

### Reference-code provenance and licensing

EchoLocal (`github.com/ygelfand/echolocal`) is MIT and CGO-free, but its packages live under `internal/` and **cannot be imported**. Every reused piece is copied and adapted into `internal/device/*` with a per-file header:

```go
// Adapted from github.com/ygelfand/echolocal (MIT). See docs/third-party-notices.md.
```

The MIT text is committed at `docs/licenses/echolocal-MIT.txt`. openWakeWord model weights (Apache-2.0) are **not committed**; `docs/wake-models.md` records vetted source URLs, licences and expected SHA-256 digests. The repository remains MIT-only: no MPL or other new licence class is introduced.

### Verified hardware and reference facts

Used as named constants, then confirmed on device in Tasks 12, 15 and 17.

| Subsystem | Facts |
|---|---|
| Mic | card 0, device 24, 16 kHz, **9 channels** (7 mic + 2 playback-loopback / AEC reference), **S24_3LE**, period 320 frames (20 ms), 8 periods |
| Speaker | card 0, device 23, **48 kHz, 2 channels, S16_LE** (the only format the playback codec accepts), period 1024 frames, 4 periods |
| Mixer | speaker amp gate control `Ext_Speaker_Amp_Switch` ("On"/"Off") |
| LED | `/sys/bus/i2c/devices/0-003f` (ISSI is31fl3236); full frame written as a **hex string** to `frame`; **36 PWM channels = 12 RGB segments**, R,G,B triples; also `led_current`, `boot_animation`; the last frame persists after the process exits |
| Buttons | evdev, `/dev/input/event*`, 16-byte records; **113 Mute, 114 VolumeDown, 115 VolumeUp, 138 Action**; tap on release when held < 700 ms |
| openWakeWord | `Step` = 1280 samples (80 ms), `melBins` = 32, `melLookback` = 480, `embedStride` = 8, `embedFrames` = 76, `embedDims` = 96, mel output scaled `v*0.1 + 2`; classifier input `[1, frames, 96]`, scalar score |
| NEON | `internal/lib/vec/{dot_arm64.s,axpy_arm64.s}` plus `vec_noasm.go`, gated `arm64 && !noasm`; `Dot` and `AXPY` via `//go:noescape` |

### Why the wake VAD is new work

EchoLocal's wake decision (`internal/feature/detect/engine.go`, `judge()`) compares the wake score against a threshold with a refractory window and near-miss reporting. There is **no VAD term** in the path, and no Silero or ONNX anywhere in the repository. §4 anticipated exactly this.

Running Silero under `CGO_ENABLED=0` is blocked. Recorded here because Task 25 writes it into §26:

- `oramasearch/onnx-go` (Gorgonia backend): 27.6% of ONNX integration tests, and **zero LSTM/GRU/RNN operators**. Silero is fundamentally conv + LSTM, so it cannot run.
- `gonnx`: implements `Lstm`/`Gru`/`Rnn`, but `operatorGetters` is **unexported and registers opset 13 only**, so operators cannot be added without forking and opset 15/18 models are rejected with `ErrUnsupportedOpsetVersion`.
- Operator inventory of the real model files, obtained by scanning the `op_type` protobuf fields (tag `0x22` + length + name), against gonnx's implemented set:

  | Model | Nodes | Missing from gonnx |
  |---|---|---|
  | `silero_vad.onnx` (v5) | 685 | `If`, `Identity`, `Pad`, `Pow`, `ReduceMean`, `Sqrt` |
  | `silero_vad.onnx` (v4) | 250 | `If`, `Identity`, `Pad`, `Pow`, `ReduceMean`, `Sqrt`, `Log`, `Neg` |
  | `silero_vad_op18_ifless.onnx` | 90 | `If`, `Pad`, `Pow`, `ReduceMean`, `Split`, `Sqrt` |
  | `silero_vad_openvino_16k.onnx` | 167 | `Pad`, `Pow`, `ReduceMean`, `Sqrt` |

  The `If` nodes are the 8 kHz/16 kHz sample-rate branch, which is why the 16 kHz-only OpenVINO export is the one without control flow. Silero ships no `.tflite`, so the interpreter ported for openWakeWord cannot be reused for it. This inventory is a static byte scan, not a full ONNX parse.

**Decision:** adapt EchoLocal's room-adaptive speech-presence detector from `internal/hardware/mic/level.go` — 12 dB above a noise floor that falls fast and climbs slowly — behind a `wake.VAD` interface. Its own comment gives the rationale: *"an absolute threshold cannot be right: the room is unknown … What is fixed is how far speech stands out from a room, which is a property of speech."* This keeps the §16 gate structurally correct, adds no dependency and no new licence, and makes a future Silero swap a composition-root change. It is **not** Silero-equivalent, and Task 25 records that as a deliberate §26 answer.

### Model replacement and training

openWakeWord uses a **frozen shared feature extractor** — the melspectrogram model plus Google's speech-embedding model — and trains only **a small per-wake-word classifier** on the 96-dimension embeddings. Upstream: *"Training new models is as simple as generating new clips for the target wake word/phrase and training a small model on top of the frozen shared feature extractor."* All bundled models were trained from **100% synthetic TTS speech** via an official Colab notebook, in **under an hour**, exporting `.tflite`.

Consequences designed into this plan:

- **Adding a wake word requires no device code change.** Install a new classifier `.tflite` and its sidecar; the shared assets, the interpreter, the pipeline and the gate are untouched. Switching models is a configuration change (`--wake-model` or the ini `wake.model` key). Task 24 verifies this by installing and switching to a second model.
- **Training happens off-device on a host.** The Dot never trains and never fetches.
- **The classifier architecture matters to us.** Upstream permits "a simple fully-connected network **or 2 layer RNN**". A fully-connected classifier needs only the kernels ported here; an RNN classifier needs recurrent kernels the interpreter will not implement. Therefore `echoctl wake install` validates the model's full opcode inventory against the interpreter's registry **on the host, at install time** (Tasks 16 and 19), rather than letting an unsupported model fail at runtime on the device.

### Inference runtime: measured escalation, not assumption

The default is pure Go plus the borrowed NEON kernels. That is a budget decision, so **Task 17 benchmarks on device immediately after the interpreter lands and before the pipeline is built on it.**

Budget: each 80 ms step must complete well inside 80 ms of wall clock; **target ≤ 20 ms combined wake + VAD** to leave headroom for capture, playback, LED and the future turn stream.

If the budget is missed, escalate in this order, cheapest constraint-cost first:

1. **More NEON kernels** — extend `internal/device/vec` to the conv2d, depthwise and fully-connected hot loops. Breaks nothing; `CGO_ENABLED=0` holds.
2. **Quantised int8 paths** for the classifier, as microWakeWord does. Breaks nothing.
3. **cgo + `libtensorflowlite` with XNNPACK** — genuinely faster kernels, keeps one process and the A/B slot model, but **breaks the pure-Go constraint** and adds a cross-toolchain to every developer machine, against §22.
4. **Python + `tflite_runtime` sidecar on the device** — last resort.

On option 4, stated explicitly because it was raised during planning: the speed argument is real, since `tflite_runtime` executes C++ XNNPACK kernels rather than interpreted Python and would likely beat hand-written Go on convolution. The deployment cost is what disqualifies it here. It requires a Python runtime plus **native wheels built for Android/bionic rather than glibc manylinux** on a memory-constrained Gen 2 device, and it breaks the update boundary Milestone 3 depends on: A/B slots swap **one self-contained binary**, so a second runtime with native extensions becomes an unversioned payload with no rollback story — exactly the provisioning drift §10.8 warns about. Option 3 buys most of the same kernel speed at a fraction of that cost, which is why it ranks higher. §13's Python speech worker is not a counter-example: it runs on the *gateway*, which has none of these constraints.

**Reference-project note on option 3's actual cost.** EchoMuse's device firmware ships an equivalent escalation as an experimental, off-by-default feature (`device/internal/wakeword/ort`): cgo bound to ONNX Runtime with XNNPACK, but the `.so` is `dlopen`'d at runtime rather than linked, so a device without the library present still boots and simply falls back to controller-side wake word — a missing 12 MB payload is not a boot failure. Their own measurement found ORT's defaults use 243% of one Cortex-A7 core, reduced to 36.2% with single-thread, no-spin configuration measured on real Echo Dot Gen 2 hardware. This suggests option 3's "cross-toolchain to every developer machine" cost can be contained (an isolated, build-tag-gated package, no static link, runtime-optional) rather than becoming a whole-binary constraint change, and that whichever runtime is chosen will still need the same kind of single-core/no-spin tuning EchoMuse needed — it does not become "free" speed. This does not change the ladder order; it lowers the assumed cost of reaching rung 3 if rungs 1–2 do not close the gap. EchoMuse's own *default* wake path, notably, runs on the controller rather than the device — a different architecture from this project's always-local voice boundary — so this is evidence about rung 3's engineering cost, not a precedent for on-device wake being easy in general.

Task 17 records the measured numbers, and any escalation decision goes in the progress log and §26 **before further code is written**.

## Dependencies and prerequisites

- Milestone 0 landed (`master` at `bb8667c`): `internal/protocol`, `internal/discovery`, `internal/release`, four go-flags binaries, Makefile, strict `.golangci.yml`, CI. `docs/plans/in-progress/` was empty when this plan was created, so there is **no shared scope and no task-claim conflict**.
- Hardware: one rooted / Magisk-enabled Echo Dot Gen 2, ADB reachable, `/data/local/bin` and `/data/local/tmp` writable, and no Alexa audio service holding `/dev/snd/pcmC0D24c` or `pcmC0D23p`. No supervisor and no pairing are required — `echod` is launched by hand.
- Host tooling: `adb` (Windows `adb.exe` from WSL per §22, or platform-tools on macOS/Linux), Go 1.26, golangci-lint 2.12.2, `make`.
- Vetted model assets verified on the host per `docs/wake-models.md`: `okay_nabu.tflite`, `melspectrogram.tflite`, `embedding_model.tflite`, and a second wake model for Task 24.
- New Go dependencies: **none**, beyond promoting `golang.org/x/sys` from indirect to direct for the ioctl wrappers.

## Architecture and high-level plan

Four layers, with hardware confined to the bottom.

1. **Portable core.** `internal/device/audio` (layout decoding, channel selection and downmix, pre-roll ring, WAV I/O, resamplers, `Fanout`, `Player`) and `internal/device/vec` (NEON `Dot`/`AXPY` with a pure-Go fallback). Pure functions over slices and `io.Reader`/`io.Writer`, so they behave identically on darwin/arm64, linux/amd64 CI, and the Dot.

2. **Hardware seam.** `audio` declares the interfaces it consumes, `PCMSource` and `PCMSink`. Two providers satisfy them: `audio/alsa` (build-tagged, `/dev/snd/pcmC0D24c` and `pcmC0D23p`, hand-written pure-Go ioctls) and `audio.FileSource`/`audio.WAVSink` (fixture-backed, portable). `internal/device/mixer` is the only other build-tagged package. LED, buttons and system need no build tags because sysfs, evdev and procfs are plain file I/O over an injected root.

3. **Wake stack.** `internal/device/wake` holds only contracts and pure logic: `Engine`, `VAD`, `Kind`, `Model`, `Store`, `Config`, `Gate`, `Pipeline`, `Stats`. Implementations live in leaf subpackages selected in the composition root: `wake/tflite` (ported interpreter), `wake/oww` (implements `Engine`), `wake/vadlevel` (implements `VAD`). The parent never imports a leaf, so swapping the VAD scorer or adding microWakeWord touches `main` only.

4. **Composition roots.** `echoctl` gains the §18 diagnostic command tree plus `status` and `bench`, and is cross-built for `linux/arm64` so it runs *on* the Dot over `adb shell`. `echod --wake-only` runs the real always-on pipeline with no socket, because §24's criterion is "**repeatedly** detects" — a property only a long-running process demonstrates.

**Ordering logic.** The interpreter's correctness (Task 16) and its on-device speed (Task 17) are the two facts everything else rests on, so both are proven before the pipeline is built on them. Hardware work is batched into four sessions — Task 12 mic/speaker, Task 15 LED/buttons, Task 17 benchmark, Task 23 wake tuning — so the device is needed in known windows. §26 answers are collected as tasks run and written back in a single final task so the design document changes once, coherently.

**Key design decisions**

- **The canonical internal representation is 16 kHz mono signed-16 PCM** (`audio.Frame`), because that is what openWakeWord expects (§7.2, "retain the reference 16 kHz PCM expectations") and it is the narrowest thing every consumer needs. The device format is converted once, in `Capturer`, immediately after the `PCMSource` read: `DecodeS24_3LE` sign-extends bit 23 and right-shifts by 8, then `SelectChannels` picks the configured physical mic channel from the 7 mic channels and excludes channels 7–8 (the playback loopback), then `Preprocessor.Process` runs — `Bypass` in this milestone. The interfaces are declared by `audio` because `audio` consumes the raw device; `alsa` is a pure provider and imports nothing from `audio`, keeping the dependency arrow `audio → alsa`.
- **`Engine.Score` and `VAD.Score` both take exactly `wake.StepSamples`** (1280 samples, 80 ms) of 16 kHz mono s16 and return one scalar. That is precisely openWakeWord's streaming step, it makes both scorers trivially fixture-testable, and it makes the Task 17 benchmark measure the same unit the budget is expressed in.
- **The accept decision is a pure function**, `Gate.Decide(Candidate) Decision`, separate from the pipeline, so §16's AND rule and the rejected-high-wake/low-VAD counter are testable with no model at all.
- **The §7.2 single-capture-path guarantee is `audio.Fanout`.** One goroutine owns the `PCMSource` for the process lifetime; consumers call `Subscribe(name, depth)` *before* `Run(ctx)` and each gets an independent buffered channel. A subscriber that cannot keep up has frames **dropped and counted**, never back-pressured, because blocking the capture loop causes an ALSA overrun and loses wake detection. Milestone 1 has two subscribers; Milestone 2 adds the turn streamer as a third `Subscribe` call with **zero change to ALSA open or configure code**. The package doc states this so a later change cannot "helpfully" reopen the device for turn capture.

### Diagnostics and observability

§7.1 requires `echod` to report health and logs, and §16 lists twelve wake diagnostics. Three surfaces land in this milestone:

- **`wake.Stats`** — mutex-guarded, `Observe` per step, `Snapshot()` returning every §16 field: active model ID, model kind, trained languages, wake threshold, VAD enabled, VAD threshold, last wake score, last VAD score, max wake score, wake count, **rejected high-wake/low-VAD count**, steps processed, frames dropped, and wake and VAD inference timing as **p50/p95/max** rather than a mean that hides stalls.
- **`system.ReadUsage` and `system.Sampler`** — `/proc/self/stat` CPU time and `VmRSS`, sampled on an interval so CPU percentage and an RSS trend are visible. This is the §26 CPU/memory instrument and the Task 17 benchmark's measuring device.
- **`echoctl status [--json]`** — one machine-readable snapshot combining device identity and serial source, hardware probe results, wake configuration, installed-model inventory with digests, `Stats`, and resource usage. This is the artifact attached to a bug report, and its field names deliberately anticipate Milestone 2's `wake.status`, `health` and `log` messages so those wire types become a rename rather than a redesign.

`echod --wake-only` logs structured `slog` records with stable keys (`device_id`, `model_id`, `wake_score`, `vad_score`, `step_ms`, `rss_bytes`) and emits a `Stats` snapshot every `--stats-interval`. On-device logging goes to a **size-bounded rotating file** with a documented cap, following §20's rule that device logs must not fill storage; without `--log-file` output is stderr only.

## Planned file map

- `internal/device/vec/{vec.go,vec_arm64.go,dot_arm64.s,axpy_arm64.s,vec_noasm.go}`: NEON `Dot`/`AXPY` with a portable fallback, gated `arm64 && !noasm`.
- `internal/device/audio/{format.go,frame.go,convert.go,ring.go,wav.go}`: sample layouts, the canonical `Frame`, S24_3LE decoding, channel selection and downmix, the pre-roll ring, WAV I/O. No build tags.
- `internal/device/audio/{resample.go,source.go,sink.go,playback.go}`: `Resampler` implementations, the consumer-declared `PCMSource`/`PCMSink`, `FileSource`, `WAVSink`, `NullSink`, and `Player`.
- `internal/device/audio/{capture.go,preprocess.go,fanout.go}`: `Capturer`, the `Preprocessor` DSP seam with `Bypass`, and the §7.2 `Fanout`.
- `internal/device/audio/{fixtures_test.go,fixtures_gen_test.go}`: the `testdata/audio` generator, `-update-fixtures`, drift guard.
- `internal/device/audio/alsa/{config.go,hwparams.go,errors.go}`: untagged configuration and the `snd_pcm_hw_params` encoder — all the arithmetic, testable everywhere.
- `internal/device/audio/alsa/{ioctl_linux.go,pcm_linux.go}` and `alsa/pcm_other.go`: the real ioctl implementation and the `!linux` stub.
- `internal/device/mixer/{names.go,control_linux.go,control_other.go,errors.go}`: the `Ext_Speaker_Amp_Switch` control.
- `internal/device/wake/{engine.go,vad.go,model.go,store.go,config.go,gate.go,stats.go,pipeline.go,diag.go}`: contracts and pure logic only.
- `internal/device/wake/tflite/{interp.go,flatbuffers.go,schema.go,tensor.go,kernels_math.go,kernels_nn.go,stream.go,opcodes.go,errors.go}`: the adapted pure-Go interpreter and its opcode inventory.
- `internal/device/wake/oww/{features.go,classifier.go,engine.go,errors.go}`: the mel → embedding → classifier pipeline and kind detection.
- `internal/device/wake/vadlevel/{detector.go,scorer.go}`: the adapted level/AGC speech-over-floor scorer.
- `internal/device/led/{frame.go,device.go,state.go,animator.go}`: frame encoding, the sysfs writer, `protocol.DeviceState` patterns, the animator.
- `internal/device/buttons/{codes.go,evdev.go,discover.go,semantics.go,watcher.go}`: key codes, the record decoder, device discovery, tap/hold semantics, the watcher.
- `internal/device/system/{paths.go,serial.go,identity.go,resources.go,logrotate.go}`: on-device paths, serial sources, stable identity, CPU/RSS sampling, bounded log rotation.
- `cmd/echoctl/{capture.go,mic.go,speaker.go,led.go,buttons.go,wake.go,status.go,bench.go}`: the §18 diagnostic command tree plus `status` and `bench`; `capture.go` holds the shared source/sink helpers so `dupl` does not fire.
- `testdata/audio/`, `testdata/wake/`, `testdata/buttons/`: generated fixtures, reference vectors, synthetic models, and a small number of recorded files marked generator-exempt.
- `docs/{third-party-notices.md,wake-models.md,device-diagnostics.md,wake-model-training.md}`, `docs/licenses/echolocal-MIT.txt`.
- One `_test.go` per source file above.
- Modify: `Makefile`, `.github/workflows/ci.yml`, `.gitignore`, `cmd/echod/{main.go,config.go,config_test.go}`, `cmd/echoctl/{config.go,main.go,config_test.go}`, `internal/protocol/messages.go` (additive device states), `docs/DESIGN.md`, `docs/protocol.md`, `AGENTS.md`, `README.md`.

## Numbered tasks

`make fmt-check`, `make lint` and `make test` must pass after every task.

### Task 1: Repository-tracked plan document exists

**Status:** completed 2026-08-19

**Purpose:** `AGENTS.md` and `docs/plans/README.md` require this work to be tracked in-repository before execution begins. A plan that lives only in a conversation does not count.

**Dependencies:** None.

**Hardware required:** no.

**Files or components:**

- Create: `docs/plans/future/2026-08-19-milestone-1-hardware-wake.md`

**Concrete changes:**

- This document, with every template section populated and no placeholder text.
- On execution start, set the owner, the start date and status `in-progress`, and `git mv` the file to `docs/plans/in-progress/`.

**Expected outcome:** The plan is tracked and its lifecycle directory matches its status field.

**Verification:**

```sh
git status --short docs/plans
```

Expected: the file is tracked in `docs/plans/in-progress/`, and its `**Status:**` field reads `in-progress`.

#### Task 1 review remediation: Verify the Windows/WSL device iteration loop

**Status:** completed 2026-08-20

**Purpose:** Establish and prove the manual ADB workflow before Milestone 1
hardware implementation depends on it.

**Dependencies:** Task 1 and one rooted/Magisk-enabled Echo Dot connected over
USB.

**Hardware required:** **yes** — an authorized Echo Dot with Magisk `su`, writable
`/data/local/tmp`, and Windows platform-tools reachable from WSL.

**Files or components:**

- Modify: `Makefile`, `docs/development-windows-wsl.md`
- Create: `.gitattributes`
- Create: `.vscode/tasks.json`

**Concrete changes:**

- Add shared Make targets for device preflight, cross-build, staging, execution,
  and argument forwarding, following EchoLocal's temporary foreground iteration
  pattern without its `/system` service installation.
- Add Remote WSL VS Code tasks that invoke those Make targets through the Windows
  ADB client.
- Document Windows versus WSL ADB ownership, Magisk root, serial selection,
  troubleshooting, Codex access, and the deliberately experimental Delve path.
- Enforce LF checkouts so WSL formatting and byte-sensitive release fixtures do
  not fail under Windows `core.autocrlf=true`.

**Expected outcome:** The same commands used by VS Code and a WSL terminal build,
push, execute, and foreground-log `echod` on the Dot without modifying `/system`.

**Verification:**

```sh
ADB=/mnt/c/tools/android-platform-tools/adb.exe \
DEVICE_SERIAL=G090LF0964060EHP make device-check
ADB=/mnt/c/tools/android-platform-tools/adb.exe \
DEVICE_SERIAL=G090LF0964060EHP make push-device
ADB=/mnt/c/tools/android-platform-tools/adb.exe \
DEVICE_SERIAL=G090LF0964060EHP make run-device
```

Expected: preflight reports `biscuit`, `arm64-v8a`, root UID 0, and permissive
SELinux; the pushed binary reports its stamped revision; foreground execution
prints the Milestone 0 startup, capabilities, and gateway-resolution records;
Ctrl+C stops the remote process without leaving it running. LED and speaker
checks remain Tasks 12 and 15 and are not claimed by this remediation.

### Task 2: Notices, licences, and the build and benchmark gates

**Status:** completed 2026-08-20

**Purpose:** Adapted EchoLocal code — including assembly — arrives in this milestone, so the notices must exist before the first ported file. The `!linux` stub path is invisible to linux/amd64 CI unless a cross-compile gate exists, `echoctl` must be cross-built to be usable on the Dot, and both inference paths must ship so Task 17 can measure them.

**Dependencies:** Task 1.

**Hardware required:** no.

**Files or components:**

- Create: `docs/third-party-notices.md`, `docs/licenses/echolocal-MIT.txt`, `docs/wake-models.md`
- Modify: `Makefile`, `.github/workflows/ci.yml`, `.gitignore`

**Concrete changes:**

- `docs/third-party-notices.md`: one section per third-party source. EchoLocal (MIT, code adapted rather than imported because its packages are `internal/`, the licence file location, the verbatim per-file header, and the list of adapted paths including the `.s` files). openWakeWord model weights (Apache-2.0, not committed, see `docs/wake-models.md`). A statement that the repository is otherwise MIT-only.
- `docs/wake-models.md`: the vetted-asset table — model ID, kind, phrase, languages, upstream URL, licence, expected SHA-256 — plus the host-side download, verify and push procedure and the `echoctl wake install` invocation. States that `echod` never fetches a model, that `wake install` never accepts a URL, and that the **fully-connected classifier export is preferred** over the RNN variant.
- `Makefile`: `build-device-ctl` (`GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o .bin/linux_arm64/echoctl ./cmd/echoctl`); `check-portability` (`GOOS=darwin GOARCH=arm64 go build ./...` then `GOOS=linux GOARCH=arm64 go build ./...`); `build-device-noasm` (as `build-device` with `-tags noasm`, output `.bin/linux_arm64/echod-noasm`); `bench` (`go test -bench . -benchmem ./...`). All added to `.PHONY`.
- `.github/workflows/ci.yml`: a `check portability` step before the test step; build both device variants; upload an `echoctl-linux-arm64` artifact alongside the existing `echod` artifact.
- `.gitignore`: `*.tflite` and `*.onnx` with a negated allowlist for `testdata/wake/synthetic/`, so committing a third-party model binary by accident is hard.

**Expected outcome:** Licensing obligations are recorded before any adapted code lands, and CI builds the darwin stub path, both device inference variants, and the device `echoctl`.

**Verification:**

```sh
make check-portability build-device-ctl build-device-noasm && file .bin/linux_arm64/echoctl
```

Expected: exit 0; `file` reports a statically linked ARM aarch64 Linux ELF; `docs/third-party-notices.md` names EchoLocal with its per-file header text.

### Task 3: `internal/device/vec` — NEON kernels with a portable fallback

**Status:** completed 2026-08-20

**Purpose:** The interpreter's hot loops are dot products and scaled accumulations. EchoLocal already has hand-written NEON for exactly these, and Go assembly requires no cgo, so this is measurable speed at no cost to the pure-Go constraint. The `noasm` tag is what lets Task 17 measure both paths on the hardware that matters.

**Dependencies:** Task 2.

**Hardware required:** no to build and test; measured on device in Task 17.

**Files or components:**

- Create: `internal/device/vec/{vec.go,vec_arm64.go,dot_arm64.s,axpy_arm64.s,vec_noasm.go}`
- Test: `internal/device/vec/vec_test.go`

**Concrete changes:**

- `vec_arm64.go` (`//go:build arm64 && !noasm`): `Dot(a, b []float32) float32` and `AXPY(dst []float32, gain float32, x []float32)`, each guarding the empty-slice case and reslicing the second argument to the first's length before calling into `//go:noescape` assembly. Preserve the original comments explaining that an empty slice must never reach the assembly and that the reslice reproduces Go's own panic when the second slice is short.
- `dot_arm64.s`, `axpy_arm64.s`: the adapted NEON implementations with attribution headers.
- `vec_noasm.go` (`//go:build !arm64 || noasm`): pure-Go `Dot` and `AXPY` delegating to `dotGo`/`axpyGo`, with the comment recording that the tag exists so both implementations can be benchmarked on the same device.
- `vec.go`: the shared reference implementations and a package doc stating that Go assembly needs no cgo, so `CGO_ENABLED=0` is unaffected.

**Expected outcome:** A NEON-accelerated dot product and AXPY on arm64, a portable fallback everywhere else, and both paths proven numerically identical.

**Verification:**

```sh
go test ./internal/device/vec/... -v
GOOS=linux GOARCH=arm64 go build ./internal/device/vec/...
GOOS=linux GOARCH=arm64 go build -tags noasm ./internal/device/vec/...
go test ./internal/device/vec -bench . -benchmem
```

Expected: exit 0 on all four. `TestDot_MatchesReferenceImplementation` and `TestAXPY_MatchesReferenceImplementation` compare against a naive loop over pseudo-random vectors at many lengths **including non-multiples of the SIMD width and length 0**; `TestDot_EmptySliceReturnsZero` and `TestDot_ShortSecondSlicePanicsLikeGo` pin the documented edge behaviour.

### Task 4: Portable audio core and the `testdata/audio` fixture generator

**Status:** completed 2026-08-20

**Purpose:** Every later audio and wake task depends on canonical frames, 24-bit decoding, channel selection, the pre-roll ring, WAV I/O and deterministic fixtures. All of it is pure and must be provably correct with no hardware.

**Dependencies:** Task 2.

**Hardware required:** no.

**Files or components:**

- Create: `internal/device/audio/{format.go,frame.go,convert.go,ring.go,wav.go}`
- Test: one `_test.go` each, plus `internal/device/audio/{fixtures_test.go,fixtures_gen_test.go}`
- Create: `testdata/audio/{tone_1k_16k_mono.wav,sweep_16k_mono.wav,silence_16k_mono.wav,noise_16k_mono.wav,dot_mic_9ch_s24le.raw}`

**Concrete changes:**

- `format.go`: `Format{SampleRate, Channels int; Layout SampleLayout}`, `SampleLayout` with `LayoutS16LE` and `LayoutS24_3LE`, `BytesPerFrame()`, `Validate()`; `ErrUnsupportedLayout`, `ErrShortBuffer`.
- `frame.go`: `Frame{Offset int64; Samples []int16}` — the canonical 16 kHz mono block — and `Duration()`.
- `convert.go`: `DecodeS24_3LE(dst []int16, src []byte) (int, error)` reading 3 bytes little-endian, sign-extending bit 23 and right-shifting by 8; `DecodeS16LE`; `SelectChannels(dst, src []int16, channels int, sel []int) (int, error)` rejecting an index at or beyond the channel count; `MonoDownmix`. The package doc records that on this device channels 0–6 are physical microphones and 7–8 are the playback loopback used as the AEC reference, and that the loopback must not enter the wake path.
- `ring.go`: `Ring` fixed-capacity overwrite-oldest buffer; `NewRing(Format, time.Duration)`, `Write([]int16)`, `Tail(time.Duration) []int16` returning at most the requested duration and never more than has been written, `Reset()`.
- `wav.go`: `WriteWAV`, `ReadWAV`, and a streaming `WAVWriter` that patches the RIFF sizes on `Close`; 16-bit PCM only, with `ErrNotRIFF` and `ErrUnsupportedWAV`.
- `fixtures_gen_test.go` mirrors `internal/release/fixtures_gen_test.go`: an `-update-fixtures` flag, `buildFixtures(t)` producing exact bytes (1 kHz sine; 100 Hz→7 kHz log sweep; silence; white noise from a fixed `math/rand/v2` seed; and a 2-second 9-channel S24_3LE raw capture where channel *n* carries an *n*·500 Hz tone at a distinct amplitude), `TestFixtures_MatchGenerator` as the drift guard, `TestFixtures_Regenerate` gated on the flag.

**Expected outcome:** 24-bit decoding, channel selection, the pre-roll ring and WAV I/O are correct and fixture-backed, and the fixtures are deterministic and drift-guarded.

**Verification:**

```sh
go test ./internal/device/audio/... -run 'TestDecode|TestSelect|TestRing|TestWAV|TestFixtures' -v
go test ./internal/device/audio -run TestFixtures_Regenerate -update-fixtures && git diff --stat testdata/audio
golangci-lint run ./internal/device/audio/...
```

Expected: all tests pass, including `TestDecodeS24_3LE_SignExtendsNegativeSamples`, `TestDecodeS24_3LE_RejectsTruncatedFinalSample`, `TestSelectChannels_ExcludesLoopbackChannels`, `TestRing_TailReturnsMostRecentPreRollOnly`, `TestRing_TailIsBoundedByWrittenAudio`, `TestReadWAV_RejectsNonRIFFWithErrNotRIFF`. Regeneration leaves `git diff` on `testdata/audio` empty. Lint reports 0 issues.

### Task 5: Wake VAD — the adapted level/AGC speech-over-floor scorer

**Status:** completed 2026-08-20

**Purpose:** §25 task 4 and the §16 VAD gate. EchoLocal has no VAD, so this is genuinely new work, and Silero is unavailable under `CGO_ENABLED=0`. The interface lands before the implementation so the scorer is swappable.

**Dependencies:** Task 4.

**Hardware required:** no; real-room behaviour is Task 23.

**Files or components:**

- Create: `internal/device/wake/vad.go`, `internal/device/wake/vadlevel/{detector.go,scorer.go}`
- Test: `internal/device/wake/vad_test.go`, `internal/device/wake/vadlevel/{detector_test.go,scorer_test.go}`

**Concrete changes:**

- `wake/vad.go`: `VAD interface { Score(step []int16) (float64, error); Reset(); Close() error }`, plus `AlwaysSpeech` returning a constant 1.0 so the gate never needs a nil branch when VAD is disabled, and `ErrVADUnavailable`. The doc states that this is the seam a Silero implementation would plug into without changing the gate or pipeline.
- `vadlevel/detector.go`: the adapted level logic — a noise floor that falls fast and climbs slowly, `speechOverFloorDB = 12.0`, `targetDBFS`, `maxGainDB` — as a pure, clock-free `Detector` with `Observe(frame []int16)`, `SpeechScore() float64`, `GainDB() float64`, `Reset()`. Constants keep the original rationale comments; the file carries the attribution header.
- `vadlevel/scorer.go`: `Scorer` implementing `wake.VAD` by mapping decibels-over-floor onto 0..1, rejecting a step whose length is not `wake.StepSamples`.

**Expected outcome:** A working, swappable wake VAD with a reproducible score trace over committed fixtures.

**Verification:**

```sh
go test ./internal/device/wake/vadlevel/... ./internal/device/wake -run 'TestDetector|TestScorer|TestAlwaysSpeech' -v
golangci-lint run ./internal/device/wake/...
```

Expected: exit 0, with `TestDetector_SilenceScoresBelowSpeechFixture` asserting a stated separability margin, `TestDetector_FloorTracksRisingNoiseBed`, `TestDetector_GainNeverExceedsCeiling`, `TestScorer_RejectsWrongStepLength`, and `TestScorer_ResetClearsFloorState` proving two passes over one fixture after `Reset` produce identical score sequences.

### Task 6: Resamplers and the playback path

**Status:** completed 2026-08-20

**Purpose:** The speaker accepts only 48 kHz stereo S16_LE while everything internal is 16 kHz mono, and §26 asks where resampling belongs. Answer it in portable code with measurable quality.

**Dependencies:** Tasks 3, 4.

**Hardware required:** no; audibility is Task 12.

**Files or components:**

- Create: `internal/device/audio/{resample.go,sink.go,playback.go}`
- Test: one `_test.go` each

**Concrete changes:**

- `resample.go`: `Resampler interface { Resample(dst, src []int16) int; Ratio() float64 }` with `HoldResampler`, `LinearResampler` and `SincResampler` (windowed-sinc polyphase, the default), allocation-free on the hot path, the sinc filter accumulation routed through `vec.Dot`.
- `sink.go`: the consumer-declared `PCMSink interface { WriteInterleaved(buf []byte) (frames int, err error); Format() Format; Close() error }`, plus `WAVSink` (the off-device verification path) and `NullSink`.
- `playback.go`: `Player.Play(ctx, []int16) error` — resample to the sink rate, duplicate mono to the sink channel count, encode to the sink layout, write in period-sized chunks. Resampling happens on the device inside `Player`, immediately before the sink; that is the §26 answer recorded in Task 25.

**Expected outcome:** A tone played through `Player` into a `WAVSink` is verifiably a clean 48 kHz stereo rendition of the 16 kHz mono input.

**Verification:**

```sh
go test ./internal/device/audio -run 'TestSinc|TestLinear|TestHold|TestPlayer' -v
golangci-lint run ./internal/device/audio/...
```

Expected: exit 0, with `TestSincResampler_16kTo48kKeepsToneSNRAbove40dB` comparing against an analytically generated 48 kHz tone and printing the measured value, plus `TestSincResampler_SuppressesAliasingOnSweepFixture`, `TestLinearResampler_ProducesExactRatioLength`, `TestPlayer_MonoIsDuplicatedToBothChannels`, `TestPlayer_WritesPeriodSizedChunks`.

### Task 7: Pure-Go ALSA layer with a non-linux stub

**Status:** completed 2026-08-20

**Purpose:** The only genuinely un-unit-testable code in the milestone. Isolate it, make its hardest part pure and tested everywhere, and guarantee the tree still builds and tests on darwin.

**Dependencies:** Task 4.

**Hardware required:** no to build and unit-test; the real device open is Task 12.

**Files or components:**

- Create: `internal/device/audio/alsa/{config.go,hwparams.go,errors.go,ioctl_linux.go,pcm_linux.go,pcm_other.go}`
- Test: `internal/device/audio/alsa/{config_test.go,hwparams_test.go,pcm_other_test.go}`
- Modify: `go.mod` — promote `golang.org/x/sys` to a direct dependency

**Concrete changes:**

- `config.go` (untagged): `Config{Card, Device, Rate, Channels int; Format Format; PeriodFrames, Periods int; Capture bool}` with `Validate()`; the measured constants as named values — `MicCard=0`, `MicDevice=24`, `MicRate=16000`, `MicChannels=9`, `MicFormat=FormatS243LE`, `MicPeriodFrames=320`, `MicPeriods=8`, `SpeakerDevice=23`, `SpeakerRate=48000`, `SpeakerChannels=2`, `SpeakerFormat=FormatS16LE`, `SpeakerPeriodFrames=1024`, `SpeakerPeriods=4`; a `Format` enum carrying ALSA's own format IDs; and `DevicePath(Config) string` producing `/dev/snd/pcmC%dD%dc` or `...p`.
- `hwparams.go` (untagged): the `snd_pcm_hw_params` byte layout and `encodeHWParams(Config) ([]byte, error)` — pure, so the hardest-to-get-right part is unit-tested on every platform.
- `errors.go`: `ErrUnsupportedPlatform`, `ErrDeviceBusy`, `ErrXRun`, `ErrNotConfigured`.
- `ioctl_linux.go` (`//go:build linux`): the ioctl numbers and the `unix.Syscall(unix.SYS_IOCTL, …)` wrapper.
- `pcm_linux.go` (`//go:build linux`): `OpenCapture`, `OpenPlayback`, `ReadInterleaved`, `WriteInterleaved`, `Prepare`, `Start`, `Drop`, `Close`. HW_PARAMS with `accessRWInterleaved = 3`, then PREPARE, then START; `READI`/`WRITEI` for transfer; `EPIPE` mapped to `ErrXRun` and recovered by re-issuing PREPARE; `EBUSY` mapped to `ErrDeviceBusy` with a doc note that a running Alexa service is the usual cause. Attribution header on each adapted file.
- `pcm_other.go` (`//go:build !linux`): identical exported signatures returning `ErrUnsupportedPlatform`, so `audio` and both commands compile and link on darwin.

**Expected outcome:** `go build ./...` and `go test ./...` succeed unchanged on darwin/arm64 and linux/amd64, while the linux binary contains a real ALSA implementation.

**Coverage justification:** the `_linux.go` ioctl paths cannot execute off-device and will hold this package below 70% of statements. The untested statements are exactly the `unix.Syscall` wrappers and the device open. `config.go` and `hwparams.go`, where all the arithmetic and all the realistic bugs live, are fully covered.

**Verification:**

```sh
make check-portability
go test ./internal/device/audio/alsa/... -v
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go vet ./internal/device/audio/alsa/...
golangci-lint run ./internal/device/audio/...
```

Expected: exit 0 on all four. `TestHWParams_EncodesS24_3LE9ChannelMicConfig` and `TestHWParams_EncodesS16LEStereoSpeakerConfig` assert the exact byte layout for the two real configurations; `TestConfig_RejectsZeroPeriodFrames`; `TestDevicePath_UsesCaptureSuffixForCapture`; `TestPCM_UnsupportedOnNonLinuxReturnsErrUnsupportedPlatform`.

### Task 8: `Capturer`, `Fanout`, `FileSource` and the DSP bypass seam

**Status:** completed 2026-08-20

**Purpose:** Realize the §7.2 guarantee that one capture path feeds wake processing and, later, active-turn streaming without reopening or reconfiguring ALSA — and make the whole capture chain runnable from a file so wake development needs no hardware.

**Dependencies:** Tasks 4, 7.

**Hardware required:** no.

**Files or components:**

- Create: `internal/device/audio/{source.go,capture.go,preprocess.go,fanout.go}`
- Test: one `_test.go` each

**Concrete changes:**

- `source.go`: the consumer-declared `PCMSource interface { ReadInterleaved(buf []byte) (frames int, err error); Format() Format; Close() error }`, plus `FileSource` reading a raw device-shaped file or a WAV, reporting the device `Format`, with an optional `Pace bool` for real-time pacing. Hand-written rather than moq-generated because it is a real fixture provider.
- `preprocess.go`: `Preprocessor interface { Process(in []int16) []int16; Name() string }` and `Bypass`. The interface doc names the deferred DSP stages — beamforming, noise suppression, AEC — and states that Milestone 1 ships `Bypass` deliberately per the §26 answer recorded in Task 25.
- `capture.go`: `CaptureConfig{Device Format; Channels []int; Preprocessor Preprocessor; StepSamples int}` and `Capturer.Run(ctx, out func(Frame) error) error` — read one period, decode the layout, select channels, downmix, preprocess, emit a canonical `Frame` with a monotonically increasing sample `Offset`. On `alsa.ErrXRun` it logs, counts and continues rather than terminating.
- `fanout.go`: `Fanout`, `Subscription{Frames <-chan Frame; Dropped() uint64}`, `NewFanout(*Capturer)`, `Subscribe(name string, depth int) (*Subscription, error)` returning `ErrFanoutRunning` if called after `Run`, and `Run(ctx) error`. Broadcast is non-blocking: a full subscriber channel drops the frame and increments its counter. The package doc states verbatim that this is the §7.2 mechanism, that the ALSA device is opened exactly once for the process lifetime, and that a Milestone 2 turn streamer must be added as an additional `Subscribe` call and must never open its own PCM device.

**Expected outcome:** The complete capture chain runs end to end from a fixture on any platform, and the single-open guarantee is enforced by a test rather than a comment.

**Verification:**

```sh
go test ./internal/device/audio -run 'TestCapturer|TestFanout|TestFileSource|TestBypass' -v -race
golangci-lint run ./internal/device/audio/...
```

Expected: exit 0, with `TestCapturer_ConvertsDeviceFramesToCanonicalMono16k`, `TestCapturer_OffsetsAreContiguousAcrossPeriods`, `TestCapturer_ContinuesAfterXRun`, `TestFanout_DeliversEveryFrameToEverySubscriber`, `TestFanout_SubscribeAfterRunIsRejected`, and `TestFanout_SlowSubscriberDropsFramesWithoutBlockingCapture` proving the capture loop completed while the slow subscriber's `Dropped()` counter is greater than zero.

### Task 9: `internal/device/system` — identity, resource sampling, bounded logging

**Status:** completed 2026-08-20

**Purpose:** §7.1's first responsibility is a stable device identity, and §7.1 and §20 together require health reporting and device logs that cannot fill storage. All three are pure file I/O over an injected root, so none of it needs a build tag.

**Dependencies:** Task 2.

**Hardware required:** no to implement; which serial source exists on this Dot is confirmed in Task 12.

**Files or components:**

- Create: `internal/device/system/{paths.go,serial.go,identity.go,resources.go,logrotate.go}`
- Test: one `_test.go` each

**Concrete changes:**

- `paths.go`: `EtcDir = "/data/local/etc/echo-satellite"`, `WakeModelDir`, `DeviceIDFile`, `RecordingDir = "/data/local/tmp"`.
- `serial.go`: `SerialReader{Root string}` with `Read() (string, error)` trying, in order and stopping at the first non-empty result, `/sys/devices/soc0/serial_number`, the `androidboot.serialno=` token in `/proc/cmdline`, then `/proc/device-tree/serial-number` with NUL bytes trimmed. All paths are joined under `Root`. Returns `ErrNoSerial` when every source is absent or blank.
- `identity.go`: `Identity{Serial, DeviceID string}` and `Resolve(SerialReader, override, persistPath string) (Identity, error)` — an explicit override wins, then the serial, then a persisted ID, then a newly generated `crypto/rand` ID written once with mode `0600` so identity is stable across reboots either way. `DeviceID` is the sanitized serial, lowercased with `[^a-z0-9-]` collapsed to `-`; a doc comment records that it is deliberately **not** hashed because §19 makes the serial identity rather than a secret, and a readable ID materially helps fleet diagnostics.
- `resources.go`: `Usage{CPUTime time.Duration; RSSBytes uint64}`, `ReadUsage(procRoot string) (Usage, error)` parsing `utime`+`stime` from `stat` and `VmRSS` from `status`, and a `Sampler` computing CPU percentage between successive samples.
- `logrotate.go`: `RotatingWriter` implementing `io.WriteCloser` with a total byte cap and a fixed file count, so §20's rule that device logs must not fill storage is enforced by construction.

**Expected outcome:** A stable, testable device identity with a documented source order and a stable fallback, plus the CPU/RSS instrument and bounded logging both later tasks depend on.

**Verification:**

```sh
go test ./internal/device/system/... -v && golangci-lint run ./internal/device/system/...
```

Expected: exit 0, with `TestSerial_PrefersSoc0OverCmdline`, `TestSerial_ParsesAndroidbootSerialnoFromCmdline`, `TestSerial_TrimsNULFromDeviceTreeValue`, `TestSerial_ReturnsErrNoSerialWhenNoSourceExists`, `TestResolve_ExplicitOverrideBeatsSerial`, `TestResolve_PersistsGeneratedIDForStabilityAcrossRestarts`, `TestResolve_SanitizesSerialIntoDeviceID`, `TestReadUsage_ParsesRSSAndCPUTime`, `TestSampler_ComputesCPUPercentBetweenSamples`, and `TestRotatingWriter_NeverExceedsConfiguredTotalBytes`.

### Task 10: `internal/device/mixer` — the speaker amplifier gate

**Status:** completed 2026-08-20

**Purpose:** `speaker test` is silent unless `Ext_Speaker_Amp_Switch` is on. This is the minimum mixer surface the milestone needs, and it must restore whatever it found.

**Dependencies:** Task 7.

**Hardware required:** no to implement; verified in Task 12.

**Files or components:**

- Create: `internal/device/mixer/{names.go,control_linux.go,control_other.go,errors.go}`
- Test: `internal/device/mixer/{names_test.go,control_other_test.go}`

**Concrete changes:**

- `names.go`: `ControlSpeakerAmp = "Ext_Speaker_Amp_Switch"`, `ValueOn = "On"`, `ValueOff = "Off"`.
- `control_linux.go` (`//go:build linux`): `Open(card int) (*Mixer, error)`, `Get(name string) (string, error)`, `Set(name, value string) error`, `Close() error` over `/dev/snd/controlC%d` ioctls, with attribution headers.
- `control_other.go` (`//go:build !linux`): identical signatures returning `ErrUnsupportedPlatform`.
- `errors.go`: `ErrControlNotFound`, `ErrUnsupportedPlatform`.
- The exported documentation records the read-before-write contract: a caller that flips the amplifier must read the prior value and restore it, so a diagnostic never leaves the device in a changed state.

**Expected outcome:** The speaker gate can be read and set on device, with a portable stub elsewhere.

**Coverage justification:** the linux ioctl path is untestable off-device for the same reason as Task 7; `names.go` and the stub are covered.

**Verification:**

```sh
make check-portability && go test ./internal/device/mixer/... -v && golangci-lint run ./internal/device/mixer/...
```

Expected: exit 0, with `TestNames_SpeakerAmpControlMatchesHardwareName` and `TestOpen_UnsupportedOnNonLinuxReturnsErrUnsupportedPlatform`.

### Task 11: `echoctl mic record` and `echoctl speaker test`

**Status:** completed 2026-08-20

**Purpose:** §18 and §25 task 5 require these two diagnostics before any gateway work. They are also the human-observable proof for Task 12.

**Dependencies:** Tasks 6, 7, 8, 10.

**Hardware required:** no for the file-backed verification here; hardware verification is Task 12.

**Files or components:**

- Create: `cmd/echoctl/{capture.go,mic.go,speaker.go}` and one `_test.go` each
- Modify: `cmd/echoctl/{config.go,main.go,config_test.go}`

**Concrete changes:**

- Extend the `opts` command tree exactly as `release verify` does: `Mic micCommand \`command:"mic"\`` containing `Record micRecordCommand \`command:"record"\``, and `Speaker speakerCommand \`command:"speaker"\`` containing `Test speakerTestCommand \`command:"test"\``.
- `micRecordCommand`: `--seconds` (default 5), `--out` (required), `--channels` accepting `mic0`…`mic6`, `all`, or a comma-separated list (default `mic0`), `--from-file` to replay a fixture instead of ALSA, `--card`, `--device`, `--print-levels`. Writes a 16 kHz mono WAV by default or a multi-channel WAV for `--channels all`, and prints per-channel peak and RMS dBFS so the §26 channel-arrangement question is answerable from one command.
- `speakerTestCommand`: `--in` (a WAV, defaulting to a generated 1 kHz tone), `--seconds`, `--to-file`, `--resampler` (`sinc`|`linear`|`hold`), `--volume`, `--no-amp`. Reads the amplifier control, sets it on, plays, and restores the prior value on every exit path including signals.
- `capture.go`: shared `openCaptureSource(o)` and `openPlaybackSink(o)` helpers used by `mic.go`, `speaker.go` and later `wake.go` and `bench.go`, so `dupl` at threshold 100 does not fire on near-identical wiring blocks.
- Extend `dispatch` with `mic record` and `speaker test`; report through the existing `writeReport` helper so output shape matches `release verify`.

**Expected outcome:** Both commands are fully exercisable off-device through `--from-file` and `--to-file`, and ready to run on the Dot.

**Verification:**

```sh
make build
.bin/echoctl mic record --from-file testdata/audio/dot_mic_9ch_s24le.raw --channels all --seconds 2 --out /tmp/mic9.wav --print-levels
.bin/echoctl speaker test --in testdata/audio/tone_1k_16k_mono.wav --to-file /tmp/spk.wav
go test ./cmd/echoctl/... -v && golangci-lint run ./cmd/echoctl/...
```

Expected: `mic record` writes a 9-channel WAV and prints nine level lines whose peaks match the fixture's per-channel amplitudes; `speaker test` writes a 48 kHz stereo WAV; `TestParseArgs_MicRecordRequiresOut`, `TestMicRecord_WritesWAVFromFixtureSource`, `TestMicRecord_PrintsPerChannelLevels`, `TestSpeakerTest_WritesResampled48kStereoToFileSink` and `TestSpeakerTest_RestoresAmpStateOnFailure` all pass.

### Task 12: HARDWARE — microphone and speaker on the Dot; §26 channel and format answers

**Status:** completed 2026-08-20

**Purpose:** §24's `mic -> WAV` and `WAV/PCM -> speaker` legs, and the measurements answering §26's "which microphone path/channel arrangement", "exact speaker format and best resampling location", and the Task 9 serial-source question.

**Dependencies:** Tasks 9, 11.

**Hardware required:** **yes** — rooted / Magisk Echo Dot Gen 2, ADB reachable, `/data/local/bin` and `/data/local/tmp` writable, no Alexa service holding `/dev/snd/pcmC0D24c` or `pcmC0D23p`. No supervisor and no pairing needed.

**Files or components:**

- Create: `docs/device-diagnostics.md`
- Create: `testdata/audio/dot_mic_9ch_s24le_room.raw` — recorded, at most 2 seconds, exempt from the generator drift guard and documented as such in `fixtures_test.go`
- Modify: `internal/device/audio/alsa/config.go` and `internal/device/system/serial.go` **only if** measurement contradicts the assumed values
- Modify: `internal/device/audio/alsa/pcm_linux.go` and `config_test.go` if hardware exposes an invalid capture/playback ioctl sequence

**Concrete changes:**

- `docs/device-diagnostics.md` records the reproducible session: the push commands, each diagnostic invocation, and the measured results — the working serial source, per-channel mic levels with the ordering of the 7 microphone and 2 loopback channels, confirmation of the mic and speaker `hw_params`, the mic channel chosen for wake, and any `ErrDeviceBusy` workaround including which service was stopped and how.
- Any constant that measurement contradicts is changed here, and the change is called out in the progress log.
- A hardware-exposed ALSA sequencing defect may be corrected here with a focused regression test; broader audio refactoring remains out of scope.

**Expected outcome:** Real audio in and out of the Dot, with the exact microphone channel arrangement recorded and one real device-shaped capture committed as a fixture.

**Verification:**

```sh
export ADB=/mnt/c/Android/platform-tools/adb.exe    # or plain adb
make build-device-ctl build-device
"$ADB" push .bin/linux_arm64/echoctl /data/local/bin/echoctl
"$ADB" push .bin/linux_arm64/echod   /data/local/bin/echod
"$ADB" shell chmod 755 /data/local/bin/echoctl /data/local/bin/echod
"$ADB" shell cat /sys/devices/soc0/serial_number
"$ADB" shell /data/local/bin/echoctl mic record --channels all --seconds 5 \
  --out /data/local/tmp/mic9.wav --print-levels
"$ADB" shell /data/local/bin/echoctl mic record --channels mic0 --seconds 5 \
  --out /data/local/tmp/mic0.wav --print-levels
"$ADB" pull /data/local/tmp/mic0.wav ./mic0.wav
"$ADB" shell /data/local/bin/echoctl speaker test --seconds 3
"$ADB" shell /data/local/bin/echoctl speaker test --in /data/local/tmp/mic0.wav
"$ADB" shell cat /proc/asound/card0/pcm24c/sub0/hw_params
"$ADB" shell cat /proc/asound/card0/pcm23p/sub0/hw_params
```

Expected, and each is the success observation to record:

- the serial command prints a non-empty serial, and `echod --version` prints the git revision;
- `mic9.wav` opens as a 9-channel 16 kHz WAV, and the printed levels show **seven channels tracking room speech** and **two channels near silence while nothing is playing**, and those same two channels **rise during `speaker test`** — that is the loopback identification, and the indices go into `docs/device-diagnostics.md`;
- `mic0.wav` is intelligible speech when played on the host;
- `speaker test --seconds 3` produces an audible 1 kHz tone, and `speaker test --in …/mic0.wav` plays the recorded speech back intelligibly;
- both `hw_params` files report exactly the assumed rate, channel count, format and period size; any difference is recorded and the constants corrected.

**What cannot be verified without hardware:** every item above. The Task 11 file-backed runs prove the conversion, WAV and resampling logic only; they say nothing about ALSA ioctl correctness, channel ordering, or audibility. This task is not complete because a fixture run passed.

### Task 13: `internal/device/led` and `echoctl led test`

**Status:** completed 2026-08-20

**Purpose:** §24's LED access leg and §7.1's semantic LED rendering, with a state map covering every state §7.6 defines.

**Dependencies:** Task 2.

**Hardware required:** no; verified in Task 15.

**Files or components:**

- Create: `internal/device/led/{frame.go,device.go,state.go,animator.go}` plus one `_test.go` each
- Create: `cmd/echoctl/led.go`, `cmd/echoctl/led_test.go`
- Modify: `internal/protocol/messages.go`, `internal/protocol/messages_test.go`, `docs/protocol.md`, `cmd/echoctl/{config.go,main.go,config_test.go}`

**Concrete changes:**

- **Close the protocol gap** (see Ambiguity 1): §7.6 lists nine device states while `internal/protocol` defines six. Add `StateMuted = "muted"`, `StateOffline = "offline"` and `StateUpdateTrial = "update_trial"`, add an `AllDeviceStates()` helper returning a copy in documentation order, and document the three additions in `docs/protocol.md`. Additive, and it lets `Render` be exhaustive over the set §7.6 specifies.
- `frame.go`: `SegmentCount = 12`, `Channels = 36`, `RGB{R,G,B uint8}`, `Frame [SegmentCount]RGB`, `EncodeHex() string` producing 72 hex characters with R,G,B per segment, `ParseHex(string) (Frame, error)`, `Uniform(RGB) Frame`, `ErrBadFrame`.
- `device.go`: `DefaultRoot = "/sys/bus/i2c/devices/0-003f"`, `New(root string) *Device`, `WriteFrame(Frame) error` skipping a frame identical to the last one written, `SetCurrent(uint8) error`, `SetBootAnimation(bool) error`, `Clear() error`. The package doc records that the last frame persists after the process exits, so a shutting-down consumer must call `Clear()` or set a deliberate final state.
- `state.go`: `Render(state protocol.DeviceState, tick int) Frame` covering all nine states with visually distinguishable patterns — idle dim, listening cyan pulse, thinking rotating blue segment, speaking steady blue, muted solid red, offline dim purple, error amber double-blink, updating white chase, update_trial amber chase — as an exhaustive switch with a `default` returning the error pattern so an unknown state is never silently invisible.
- `animator.go`: `Animator` with `Set(protocol.DeviceState)` and `Run(ctx) error`, the tick channel injectable for tests.
- `echoctl led test`: `--root` (default `led.DefaultRoot`), `--state` (any of the nine), `--all-states`, `--seconds` per state, `--current`, `--clear`.

**Expected outcome:** A semantic LED layer testable entirely against a temporary directory, and a protocol state set matching §7.6.

**Verification:**

```sh
go test ./internal/device/led/... ./internal/protocol/... -v
.bin/echoctl led test --root /tmp/ledfake --all-states --seconds 1 && ls /tmp/ledfake
golangci-lint run ./internal/device/led/... ./internal/protocol/...
```

Expected: exit 0. `TestRender_CoversEveryDeviceState` iterates `protocol.AllDeviceStates()` so adding a state without a pattern fails the tests; `TestFrame_EncodeHexWrites36ChannelsInRGBOrder`, `TestFrame_ParseHexRejectsWrongLength`, `TestDevice_WriteFrameSkipsIdenticalFrame`, `TestRender_MutedIsVisuallyDistinctFromError` and `TestAnimator_AdvancesPatternOnEachTick` pass. The fake-root run writes a `frame` file containing 72 hex characters.

### Task 14: `internal/device/buttons` and `echoctl buttons test`

**Status:** completed 2026-08-20

**Purpose:** §24's button access leg, with the tap and hold semantics Action and Mute need. The Action button is Milestone 2's manual turn trigger (§16), so its semantics must be right now.

**Dependencies:** Task 2.

**Hardware required:** no; verified in Task 15.

**Files or components:**

- Create: `internal/device/buttons/{codes.go,evdev.go,discover.go,semantics.go,watcher.go}` plus one `_test.go` each
- Create: `cmd/echoctl/buttons.go`, `cmd/echoctl/buttons_test.go`
- Create: `testdata/buttons/*.bin` event fixtures, produced by this task's generator
- Modify: `cmd/echoctl/{config.go,main.go,config_test.go}`

**Concrete changes:**

- `codes.go`: `Key` with `KeyMute = 113`, `KeyVolumeDown = 114`, `KeyVolumeUp = 115`, `KeyAction = 138`, plus `String()` and `evTypeKey = 1`.
- `evdev.go`: `eventSize = 24`, `decodeEvent([]byte) (rawEvent, error)` reading two 64-bit kernel-long timeval words, a `uint16` type, a `uint16` code and an `int32` value, and `Reader{r io.Reader}` with `Read() (rawEvent, error)`; `ErrShortEvent`. The doc records that both build targets are 64-bit, which is why no build tag is needed, and `Reader` works over any `io.Reader` so `bytes.Reader` is a complete test double. This corrects the original plan's internally inconsistent 16-byte size: those fields occupy 24 bytes in the 64-bit Linux ABI used by the Dot.
- `discover.go`: `FindDevices(inputDir, sysClassDir string, wantNames []string) ([]string, error)` globbing `event*` and reading `<sysClassDir>/<node>/device/name`, matching case-insensitively; `DefaultInputDir`, `DefaultSysClassDir`, `ErrNoInputDevice`. Both roots are injected so `t.TempDir()` tests the whole discovery path.
- `semantics.go`: `HoldThreshold = 700 * time.Millisecond`; `Action` with `ActionTap`, `ActionHoldStart`, `ActionHoldEnd`, `ActionRepeat`; `Press{Key; Action; Held time.Duration; At time.Time}`; and `Recognizer.Feed(k Key, value int32, at time.Time) []Press` where value 1 is press, 0 release and 2 the kernel auto-repeat. Mute and Action emit `ActionTap` on release when held under the threshold, otherwise `ActionHoldStart` at the threshold and `ActionHoldEnd` on release with the duration; volume keys emit on press and then one `ActionRepeat` per auto-repeat, which is the ramp. The clock is a parameter and `time.Now()` is never called inside, so the state machine is table-testable.
- `watcher.go`: `Watcher` with `Run(ctx, chan<- Press) error`.
- `echoctl buttons test`: `--input-dir`, `--sys-class-dir`, `--from-file`, `--seconds`, printing one line per recognized `Press` with key, action and held duration.

**Expected outcome:** Button decoding and tap/hold semantics are fully covered off-device, and the command is ready for a human on the Dot.

**Verification:**

```sh
go test ./internal/device/buttons/... -v -race
.bin/echoctl buttons test --from-file testdata/buttons/action_tap.bin --seconds 1
golangci-lint run ./internal/device/buttons/...
```

Expected: exit 0, with `TestDecodeEvent_ParsesTypeCodeValueFrom24ByteRecord`, `TestDecodeEvent_RejectsShortRecordWithErrShortEvent`, `TestFindDevices_MatchesByDeviceName`, `TestFindDevices_ReturnsErrNoInputDeviceWhenNoNameMatches`, `TestRecognizer_ActionHeldBelow700msEmitsTapOnRelease`, `TestRecognizer_ActionHeldAbove700msEmitsHoldStartThenHoldEnd`, `TestRecognizer_MuteFollowsTapHoldSemantics`, `TestRecognizer_VolumeKeyRepeatRamps`, `TestWatcher_IgnoresNonKeyEventTypes` and `TestWatcher_StopsOnContextCancel`.

#### Task 14 review remediation: generate volume repeats on the Dot

**Status:** completed 2026-08-20

**Dependencies:** Task 14 and Task 15 hardware evidence.

**Files or components:** `internal/device/buttons/{semantics.go,watcher.go}` and their tests.

**Concrete changes:** The real keypad emits press and release but no evdev autorepeat. Preserve support for kernel value `2`, but also generate `ActionRepeat` every 200 ms while Volume Up or Volume Down remains pressed, matching EchoLocal's measured implementation. Stop repeats immediately on release or cancellation.

**Verification:** `go test -race ./internal/device/buttons/... -v`; then rerun Task 15's live Volume Up hold and confirm repeats appear about every 200 ms and stop on release.

### Task 15: HARDWARE — LED ring and buttons on the Dot

**Status:** completed 2026-08-20

**Purpose:** §24's `LED/button access` leg. Confirms the sysfs path, the 12-segment RGB mapping, and the real key codes on this device revision.

**Dependencies:** Tasks 12, 13, 14.

**Hardware required:** **yes** — same device state as Task 12.

**Files or components:**

- Modify: `docs/device-diagnostics.md`
- Modify: `internal/device/led/{device.go,state.go}`, `cmd/echoctl/{config.go,led.go}` and their tests, or `internal/device/buttons/codes.go` **only if** measurement contradicts the assumed values

**Concrete changes:** Record in `docs/device-diagnostics.md` the confirmed LED sysfs root, whether all 12 segments respond and in what physical order, the visible effect of `led_current`, whether `boot_animation` must be disabled, the discovered `/dev/input/event*` node and its `device/name`, and the observed key codes for all four buttons. Correct any measured hardware contract that contradicts the implementation, including the diagnostic CLI when it prevents the planned hardware verification.

**Expected outcome:** All 12 LED segments and all four buttons confirmed working through the new packages.

**Verification:**

```sh
"$ADB" shell ls -l /sys/bus/i2c/devices/0-003f/
"$ADB" shell cat /sys/class/input/event*/device/name
"$ADB" shell /data/local/bin/echoctl led test --state muted --seconds 3
"$ADB" shell /data/local/bin/echoctl led test --all-states --seconds 2
"$ADB" shell /data/local/bin/echoctl led test --clear
"$ADB" shell /data/local/bin/echoctl buttons test --seconds 30
# during those 30 s, physically: tap Action, hold Action 2 s, tap Mute,
# hold Mute 2 s, tap Volume+, hold Volume+ 2 s, tap Volume-
```

Expected, each an explicit observation to record:

- `frame`, `led_current` and `boot_animation` all exist under the i2c path;
- `--state muted` turns the ring solid red; `--all-states` shows nine patterns a human can tell apart; `--clear` leaves the ring dark and it **stays** dark after the process exits;
- `buttons test` prints exactly `action tap`, then `action hold-start` and `action hold-end held≈2s`, then `mute tap`, `mute hold-start`/`hold-end`, then `volume-up` followed by several `volume-up repeat`, then `volume-down` — nothing spurious, and no key mapped to the wrong name.

**What cannot be verified without hardware:** the sysfs path, segment ordering, current behaviour, frame persistence after exit, the input node name, and the real key codes. The Task 13 and 14 temporary-directory tests prove encoding and semantics only.

### Task 16: Ported pure-Go TFLite interpreter, opcode inventory, reference proof

**Status:** completed 2026-08-20

**Purpose:** openWakeWord needs three TFLite models and the device binary must stay CGO-free, so the interpreter is adapted from EchoLocal. Its correctness must be provable in CI, without hardware and without shipping any third-party model binary. Its opcode inventory is also what makes an unsupported wake model fail on the host rather than on the device.

**Dependencies:** Tasks 3, 4.

**Hardware required:** no; arm64 numeric agreement is checked in Tasks 17 and 23.

**Files or components:**

- Create: `internal/device/wake/tflite/{interp.go,flatbuffers.go,schema.go,tensor.go,kernels_math.go,kernels_nn.go,stream.go,opcodes.go,errors.go}` plus one `_test.go` each
- Create: `internal/device/wake/tflite/{fixtures_test.go,fixtures_gen_test.go}`
- Create: `testdata/wake/reference/{mel_reference.txt,embedding_reference.txt}`, `testdata/wake/synthetic/*.tflite`, `testdata/wake/ATTRIBUTION.md`

**Concrete changes:**

- Adapt the interpreter: hand-written FlatBuffers model parsing, a kernel registry, and `Model`, `Interpreter`, `Tensor{Shape []int; F32 []float32; I16 []int16}`, `Invoke()`, `InputShape(i int) []int`, `Output(i int) *Tensor`. Imports are restricted to `encoding/binary`, `fmt`, `math`, `os` and `internal/device/vec`; the dot-product and accumulate hot loops route through `vec` so the NEON path is genuinely exercised. Every file carries the attribution header, and `testdata/wake/ATTRIBUTION.md` records the provenance of the two reference-vector files.
- Sentinels `ErrUnsupportedOp` naming the opcode, `ErrBadModel`, `ErrShapeMismatch`, all wrapped with `%w` at the package boundary.
- `opcodes.go`: `SupportedOpcodes() []string`, `Model.Opcodes() []string` and `Model.UnsupportedOpcodes() []string`. This is what lets `echoctl wake install` reject a model whose operators cannot run — including the two-layer-RNN classifier export openWakeWord permits — on the host at install time.
- `fixtures_gen_test.go` emits **synthetic minimal `.tflite` models** using a tiny in-repository FlatBuffers writer: one per supported kernel family (fully connected, reshape, logistic, conv2d, and the arithmetic operators the mel and embedding graphs use) with known weights and analytically computed expected outputs, so the kernel registry is fully covered in CI with no third-party binary. Same `-update-fixtures`, drift-guard and regenerator shape as `internal/release`.

**Expected outcome:** A pure-Go interpreter whose kernels are proved analytically in CI, whose mel and embedding outputs are proved against the committed reference vectors when the real models are present, and which can report the operators a candidate model needs.

**Verification:**

```sh
go test ./internal/device/wake/tflite/... -v
ECHO_WAKE_MODEL_DIR=$PWD/models go test ./internal/device/wake/tflite -run Reference -v
go test -tags noasm ./internal/device/wake/tflite/... -v
go test ./internal/device/wake/tflite -run TestFixtures_Regenerate -update-fixtures && git diff --stat testdata/wake
golangci-lint run ./internal/device/wake/...
```

Expected: the synthetic-kernel table passes in CI with no models present. With `ECHO_WAKE_MODEL_DIR` set, `TestInterpreter_MelOutputMatchesReferenceVectors` and `TestInterpreter_EmbeddingOutputMatchesReferenceVectors` pass within ±1e-3 absolute and ±1e-2 relative, asserted per element, reporting the first mismatching index and printing the maximum deviation. The `noasm` run agrees with the default build. `TestInterpreter_RejectsUnknownOpcodeWithErrUnsupportedOp`, `TestInterpreter_RejectsTruncatedFlatBuffer` and `TestModel_ReportsUnsupportedOpcodes` pass. Regeneration produces no diff. **This is the correctness proof for the whole port.**

### Task 17: HARDWARE BENCHMARK — on-device inference budget and runtime decision gate

**Status:** completed

**Purpose:** The plan's default is pure Go plus NEON. That is a budget decision, so it is measured on the real device **before** the pipeline is built on the interpreter, while changing course is still cheap.

**Dependencies:** Tasks 12, 16.

**Hardware required:** **yes** — Dot as in Task 12, plus the vetted `melspectrogram.tflite`, `embedding_model.tflite` and `okay_nabu.tflite`.

**Files or components:**

- Create: `cmd/echoctl/bench.go`, `cmd/echoctl/bench_test.go`
- Modify: `cmd/echoctl/{config.go,main.go}`, `docs/device-diagnostics.md`

**Concrete changes:**

- `echoctl bench`: `--model`, `--model-dir`, `--steps` (default 500), `--from-file`, `--json`. Runs N steps of mel → embedding → classifier plus the VAD over a fixture and reports **p50/p95/max per stage in milliseconds**, total per-step time, and CPU percentage and RSS from `system.Sampler`, as human-readable text or JSON.
- Record both the NEON and the `noasm` results.
- **Budget:** each 80 ms step must complete well inside 80 ms of wall clock; the target is **≤ 20 ms combined wake + VAD**, leaving headroom for capture, playback, LED and the future turn stream.
- If the budget is missed, escalate per the plan's stated ladder — more NEON kernels, then int8 quantisation, then cgo with `libtensorflowlite` and XNNPACK (which breaks the pure-Go constraint), then a Python `tflite_runtime` sidecar (which breaks pure Go **and** the single-binary A/B model) — and record the decision in the progress log and in §26 **before writing further code**.

**Expected outcome:** A measured per-step inference cost on the target hardware, for both inference paths, with a recorded pass or fail against the budget and an explicit decision if escalation is needed.

**Verification:**

```sh
make build-device-ctl && "$ADB" push .bin/linux_arm64/echoctl /data/local/bin/echoctl
"$ADB" shell /data/local/bin/echoctl bench --model okay_nabu --steps 500 --json > bench-neon.json
make build-device-ctl TAGS=noasm && "$ADB" push .bin/linux_arm64/echoctl /data/local/bin/echoctl-noasm
"$ADB" shell /data/local/bin/echoctl-noasm bench --model okay_nabu --steps 500 --json > bench-noasm.json
```

Expected: both runs complete and print per-stage p50/p95/max, CPU percentage and RSS. The NEON build is recorded as faster than `noasm`, or the discrepancy is investigated and explained. The pass or fail against the 20 ms target is written into `docs/device-diagnostics.md` together with any escalation decision.

**What cannot be verified without hardware:** the entire point of the task. An amd64 benchmark says nothing about Echo Dot Gen 2 headroom.

### Task 18: `wake/oww` — mel, embedding, classifier and engine-kind detection

**Status:** completed 2026-08-21

**Purpose:** §25 task 3 — the local wake-model layer and one known-working model. This is the wake scorer itself.

**Dependencies:** Tasks 16, 17 (the budget is known before code is built on the interpreter).

**Hardware required:** no.

**Files or components:**

- Create: `internal/device/wake/{engine.go,model.go}` plus `engine_test.go`
- Create: `internal/device/wake/oww/{features.go,classifier.go,engine.go,errors.go}` plus one `_test.go` each
- Modify: `internal/device/wake/tflite/fixtures_gen_test.go`; create `testdata/wake/synthetic/oww_embedding.tflite`

**Concrete changes:**

- `wake/engine.go`: `SampleRate = 16000`, `StepSamples = 1280`, `Kind` with `KindOpenWakeWord` and `KindMicroWakeWord` plus `String`/`ParseKind`, the `Engine interface { ID() string; Kind() Kind; Score(step []int16) (float64, error); Reset(); Close() error }`, and `ErrStepLength`. The doc states that `Pipeline` holds a slice of engines so §16's "should not prevent multiple simultaneously active models later" is structurally satisfied, and that microWakeWord will implement this same interface without a change here.
- `wake/model.go`: the minimal `Model{ID, Path string; Kind Kind}` contract consumed by `oww.New`. Task 19 extends this type with sidecar and installation metadata, validation and storage; moving this small contract removes Task 18's otherwise circular dependency without changing its public constructor.
- `oww/features.go`: streaming mel and embedding computation with named, commented constants — `melBins = 32`, `melLookback = 3 * 160 = 480`, `embedStride = 8`, `embedFrames = 76`, `embedDims = 96`, `melScale = 0.1`, `melOffset = 2`. The mel model takes `[1, len(window)]` int16-derived float input and its output is scaled `v*melScale + melOffset`; the embedding model takes `[1, 76, 32, 1]` and slides 8 mel frames per step, producing 96 dimensions. The doc records that mel and embedding are the frozen shared backbone and are never per-wake-word, which is why adding a wake word needs no code change.
- `oww/classifier.go`: `Classifier` feeding `[1, frames, 96]` over the newest embeddings and reading the scalar `out.F32[0]`, with `frames` read from the model's own input shape rather than hard-coded.
- `oww/engine.go`: `Engine` implementing `wake.Engine`; `New(shared SharedModels, model wake.Model) (*Engine, error)`; `Score` rejecting any step whose length is not `wake.StepSamples`; `Reset()` clearing the mel and embedding ring state.
- `DetectKind(*tflite.Model) (wake.Kind, error)`: a classifier input shape of `[1, frames, 96]` means `KindOpenWakeWord`, and anything else returns `wake.ErrUnknownModelKind` naming the observed shape. This is the §16 auto-detection, reused by `wake install`.
- The synthetic embedding fixture plus an in-memory zero-weight mel model exercise scale/offset, lookback, incremental embedding, and reset in CI without committing third-party model weights. Real-model tests remain supplemental through `ECHO_WAKE_MODEL_DIR`.

**Expected outcome:** An `Engine` that scores 80 ms steps of 16 kHz mono s16 and auto-detects model kind, with the streaming arithmetic covered in CI.

**Verification:**

```sh
go test ./internal/device/wake/oww/... -v
ECHO_WAKE_MODEL_DIR=$PWD/models go test ./internal/device/wake/oww -v
golangci-lint run ./internal/device/wake/...
```

Expected: exit 0, with `TestFeatures_ProducesEightMelFramesPerStep`, `TestFeatures_AppliesMelScaleAndOffset`, `TestFeatures_UsesMelLookbackContextOnFirstStep`, `TestEngine_RejectsWrongStepLengthWithErrStepLength`, `TestEngine_ResetClearsStreamingState`, `TestOWW_DetectsKindFromClassifierInputShape`, `TestOWW_RejectsNonOWWInputShapeWithErrUnknownModelKind`, `TestClassifier_ReadsFrameCountFromModelInputShape`. With real models present, the silence fixture scores near zero over every step.

### Task 19: Model store, sidecar metadata, configuration, and `echoctl wake list|install`

**Status:** not started

**Purpose:** §16's model-distribution and model-loading requirements — stable model IDs, sidecar metadata, engine detection, configurable threshold, backend-independent configuration — plus the trust rule that models are never fetched from arbitrary unauthenticated locations, and the opcode check that keeps an unsupported model off the device.

**Dependencies:** Tasks 9, 18.

**Hardware required:** no; the on-device install is exercised in Tasks 23 and 24.

**Files or components:**

- Create: `internal/device/wake/{store.go,config.go}` plus one `_test.go` each; extend `internal/device/wake/model.go`
- Create: `cmd/echoctl/wake.go`, `cmd/echoctl/wake_test.go`
- Modify: `cmd/echoctl/{config.go,main.go,config_test.go}`, `docs/wake-models.md`

**Concrete changes:**

- `model.go`: extend Task 18's `Model` with `Phrase string; Languages []string; SampleRate int; SHA256 string; Size int64; Source, License string`, add a `Sidecar` type parsed with `json.Decoder.DisallowUnknownFields` mirroring `release.ParseManifest`'s strictness and requiring `schema`, `id`, `kind` and `sha256`, plus `ParseSidecar` and `Model.Validate()`; `ErrSidecarMismatch`.
- `store.go`: `Store{Root string}` with `DefaultRoot`, `List()`, `Get(id)`, `Install(InstallRequest)`, `SharedPath(name string)`; `ErrModelNotFound`, `ErrDigestMismatch`, `ErrUnknownModelKind`, `ErrUnsupportedModelOps`. `Store` takes an injected `Root` so every path is testable from `t.TempDir()`.
- `Install(InstallRequest{ID, SourcePath, SidecarPath, ExpectedSHA256 string, Overwrite bool})` performs, in order: the source must be an existing local file, and any value parseable as a URL with a scheme is rejected outright; the expected digest is required and verified before anything is written; the file must parse as a TFLite model; `oww.DetectKind` must agree with the sidecar `kind`; and `Model.UnsupportedOpcodes()` must be empty, which is the check that catches an RNN-based classifier export on the host with a message naming the offending operators. Only then is the file staged as `<id>.tflite.part`, fsynced, renamed, and `index.json` rewritten atomically — the same never-destroy-before-proven discipline `internal/release` uses.
- `config.go`: `Config{Enabled bool; Engine, Model string; Threshold float64; VAD VADConfig; PreRollMS, MinIntervalMS int; AlwaysScoreWake bool}` and `VADConfig{Enabled bool; Threshold float64}`, with `Defaults()` matching §16's configuration block field for field. **Thresholds stay at §16's example values and are explicitly commented as untuned placeholders to be replaced by Task 23**; no tuned default is invented here. Plus `Validate()` and `ToProtocol() protocol.WakeConfig`. The configuration file format is ini, matching the existing `echod --config` and §27's precedence model; §16's YAML is illustrative of structure only.
- `echoctl wake list`: `--model-dir`; prints model ID, kind, phrase, languages, size, digest prefix, and whether the shared mel and embedding assets are present. `echoctl wake install <id>`: `--from` (required, local path), `--metadata`, `--sha256` (required unless the sidecar carries it), `--model-dir`, `--overwrite`.

**Expected outcome:** Models install only from a local file with a verified digest, a kind matching the model's own tensor shape, and operators the interpreter can actually run; the installed set is listable with stable IDs.

**Verification:**

```sh
go test ./internal/device/wake -run 'TestParseSidecar|TestStore|TestConfig' -v
.bin/echoctl wake install okay_nabu --from ./models/okay_nabu.tflite \
  --metadata ./models/okay_nabu.json --sha256 <expected> --model-dir /tmp/wm
.bin/echoctl wake install okay_nabu --from ./models/okay_nabu.tflite \
  --sha256 0000000000000000000000000000000000000000000000000000000000000000 --model-dir /tmp/wm2
.bin/echoctl wake list --model-dir /tmp/wm
golangci-lint run ./internal/device/wake/... ./cmd/echoctl/...
```

Expected: the first install succeeds and `wake list` shows `okay_nabu` with kind `openwakeword`, its phrase and languages. The deliberately wrong digest exits non-zero with a digest-mismatch message and leaves `/tmp/wm2` containing **no** `.tflite` and no `.part` file. Tests include `TestParseSidecar_RejectsUnknownFields`, `TestStore_InstallRejectsURLSourceBecauseModelsAreNeverFetched`, `TestStore_InstallRejectsSidecarKindMismatch`, `TestStore_InstallRejectsModelWithUnsupportedOpcodes`, `TestStore_InstallLeavesNoPartFileOnFailure`, `TestConfig_DefaultsMatchDesignSection16` and `TestConfig_ValidateRejectsThresholdOutsideUnitRange`.

### Task 20: Accept gate, pipeline and the §16 diagnostics

**Status:** not started

**Purpose:** §16's accept rule, pre-roll and full diagnostics list. This is where the wake vertical slice becomes one runnable thing.

**Dependencies:** Tasks 5, 8, 18, 19.

**Hardware required:** no; real-room behaviour is Task 23.

**Files or components:**

- Create: `internal/device/wake/{gate.go,stats.go,pipeline.go,diag.go}` plus one `_test.go` each

**Concrete changes:**

- `gate.go`: `Thresholds{Wake, VAD float64}`, `Candidate{ModelID string; WakeScore, VADScore float64; VADEnabled bool; At time.Time}`, `Decision` with `DecisionBelowWake`, `DecisionRejectedLowVAD`, `DecisionRefractory` and `DecisionAccepted`, and `Gate.Decide(Candidate) Decision` implementing §16's pseudocode as a pure function — accept only when the wake score is at or above the wake threshold **and** either VAD is disabled or the VAD score is at or above the VAD threshold **and** the time since the last acceptance exceeds `MinInterval`. The four decision values exist precisely so `DecisionRejectedLowVAD` can drive §16's rejected high-wake/low-VAD counter as a distinct, testable outcome rather than a silent branch.
- `stats.go`: a mutex-guarded `Stats` with `Observe(...)` per step and `Snapshot()`, carrying every §16 diagnostic — active model ID, model kind, languages, wake threshold, VAD enabled, VAD threshold, last wake score, last VAD score, max wake score, wake count, rejected low-VAD count, steps processed, frames dropped, and wake and VAD inference timing as p50, p95 and max rather than a mean that hides stalls.
- `pipeline.go`: the consumer-declared `FrameSource interface { Frames() <-chan audio.Frame }`, `Event{ModelID string; WakeScore, VADScore float64; PreRoll []int16; At time.Time}`, `Pipeline{Engines []Engine; VAD VAD; Gate Gate; Ring *audio.Ring; Stats *Stats}` and `Run(ctx, FrameSource, chan<- Event) error`. Per step it appends the frame's samples to the pre-roll ring and to a step accumulator; once `StepSamples` are available it times and takes the VAD score, then times and takes each engine's wake score, builds a `Candidate`, calls `Decide`, records statistics, and on `DecisionAccepted` emits an `Event` whose `PreRoll` is `Ring.Tail(PreRollMS)`. `Config.AlwaysScoreWake`, default true, controls whether wake inference runs when the VAD score is below threshold; true keeps `LastWakeScore` honest for diagnostics, and false is the CPU optimization measured in Tasks 17 and 23.
- `diag.go`: the `Snapshot` type shared by `echoctl status` and `echod`'s periodic logging.
- Engine and VAD are exercised through hand-written stubs with scripted score sequences, so the gate and counter behaviour is proved with no model files at all.

**Expected outcome:** The complete wake pipeline runs from a fixture on any platform, the §16 accept rule is enforced by a pure tested function, and every §16 diagnostic is observable.

**Verification:**

```sh
go test ./internal/device/wake -run 'TestGate|TestPipeline|TestStats' -v -race
golangci-lint run ./internal/device/wake/...
```

Expected: exit 0. `TestGate_RejectsHighWakeWithLowVAD` proves a 0.99 wake score with a 0.01 VAD score is rejected and increments the counter; `TestGate_AcceptsWhenWakeAndVADBothExceedThresholds`, `TestGate_IgnoresVADWhenDisabled`, `TestGate_RefractoryWindowSuppressesDuplicateWake`, `TestPipeline_EmitsEventWithConfiguredPreRollLength`, `TestPipeline_CountsRejectedHighWakeLowVADCandidates`, `TestPipeline_SilenceFixtureProducesNoWakeEvent`, `TestPipeline_RecordsInferenceTimings`, `TestPipeline_StopsOnContextCancel`, `TestStats_SnapshotReportsEverySection16Field` and `TestStats_TimingPercentilesAreOrdered` all pass.

### Task 21: Diagnostics surface — `echoctl wake test`, `wake vad-test`, `status --json`

**Status:** not started

**Purpose:** §25 task 5's `echoctl wake test` and VAD diagnostics, plus the machine-readable health snapshot §7.1 requires and Task 23 depends on.

**Dependencies:** Task 20.

**Hardware required:** no; used throughout Task 23.

**Files or components:**

- Modify: `cmd/echoctl/wake.go`, `cmd/echoctl/wake_test.go`
- Create: `cmd/echoctl/status.go`, `cmd/echoctl/status_test.go`
- Modify: `cmd/echoctl/{config.go,main.go}`

**Concrete changes:**

- `echoctl wake test`: `--from-file` or live capture, `--model`, `--model-dir`, `--threshold`, `--vad-threshold`, `--no-vad`, `--preroll-ms`, `--print-steps`, `--seconds`, `--save-preroll <dir>`. On each accepted wake it prints the model ID, wake score, VAD score, elapsed time and pre-roll length; at exit it prints the full `Stats.Snapshot()` — every §16 field — plus CPU and RSS from `system.ReadUsage`. `--save-preroll` is explicitly opt-in and its help text says so, because raw audio is not stored by default.
- `echoctl wake vad-test`: `--from-file` or live, `--threshold`, `--print-steps`, printing per-step VAD score with its timestamp and then a summary of step count, mean, max and the fraction of steps above the threshold.
- `echoctl status [--json]`: one snapshot combining device identity and serial source, hardware probe results (mic, speaker, LED and buttons reachable; amplifier state), wake configuration, the installed-model inventory with digests, `Stats`, and resource usage. Field names deliberately anticipate Milestone 2's `wake.status`, `health` and `log` messages so those wire types become a rename rather than a redesign.

**Expected outcome:** Wake and VAD behaviour is observable from one command each, and a single JSON snapshot captures everything needed for a bug report.

**Verification:**

```sh
.bin/echoctl wake test --from-file testdata/audio/silence_16k_mono.wav --print-steps
.bin/echoctl wake test --from-file testdata/audio/noise_16k_mono.wav
.bin/echoctl wake vad-test --from-file testdata/audio/silence_16k_mono.wav --print-steps
.bin/echoctl status --json | python3 -m json.tool
go test ./cmd/echoctl/... -v && golangci-lint run ./cmd/echoctl/...
```

Expected: the silence and noise fixtures produce **zero** wake events and print a statistics block containing every §16 field; `status --json` is valid JSON containing the device ID, model inventory and CPU/RSS, with `TestStatus_JSONContainsEverySection16DiagnosticField` enforcing that.

### Task 22: `echod --wake-only` composition root

**Status:** not started

**Purpose:** §24's criterion is that the Dot **repeatedly** detects the wake word, and §7.1 makes continuous capture and the wake pipeline `echod`'s responsibility. Failure modes that matter — ALSA overruns after minutes, RSS growth, CPU headroom, idle false triggers — appear only in a long-running daemon.

**Dependencies:** Tasks 9, 13, 14, 20.

**Hardware required:** no to build and smoke-test with a file source; the real run is Task 23.

**Files or components:**

- Modify: `cmd/echod/{main.go,config.go,config_test.go}`

**Concrete changes:**

- `config.go` gains, with `ECHOD_`-prefixed environment tags and ini support to match the existing precedence model: `--wake-only`, `--wake-model`, `--wake-model-dir`, `--wake-threshold`, `--vad-threshold`, `--vad-enabled`, `--preroll-ms`, `--min-wake-interval-ms`, `--mic-channels`, `--mic-from-file`, `--led-root`, `--stats-interval`, `--always-score-wake`, `--log-file`, `--log-max-bytes`. A `wakeConfig() wake.Config` method mirrors the existing `discoveryConfig()` shape.
- `main.go`: when `--wake-only` is set, `run` wires the composition root — `system.Resolve` for identity, `alsa.OpenCapture` or `audio.FileSource` when `--mic-from-file` is given, into `audio.Capturer`, into `audio.Fanout` with a single `wake` subscription, then `wake.Store` into `oww.Engine` plus `vadlevel.Scorer` into `wake.Pipeline`, alongside `led.Animator`, `buttons.Watcher` and a `system.Sampler` — and runs them under `signal.NotifyContext`. Logging is structured `slog` with stable keys `device_id`, `model_id`, `wake_score`, `vad_score`, `step_ms` and `rss_bytes`; a `Stats.Snapshot()` plus resource usage is logged every `--stats-interval`; output goes through `system.RotatingWriter` when `--log-file` is set and to stderr otherwise. Button presses are logged, and an Action tap logs that it would start a turn in Milestone 2 and briefly sets the LED to `listening`, exercising the §7.6 local-feedback path with no protocol traffic. **No socket is opened, and `logGatewayTarget` is skipped in this mode.** On shutdown the LED is set to a deliberate final state and the microphone is closed.
- Without `--wake-only`, the existing Milestone 0 behaviour is unchanged, and the startup log states which mode is active so an inert binary is never mistaken for a working one.

**Expected outcome:** `echod --wake-only` is the always-on local wake daemon, verifiable off-device against a fixture and ready for the Dot.

**Verification:**

```sh
make build build-device
.bin/echod --wake-only --mic-from-file testdata/audio/dot_mic_9ch_s24le.raw \
  --wake-model-dir /tmp/wm --led-root /tmp/ledfake --stats-interval 2s --dbg
go test ./cmd/echod/... -v && make lint check-portability
```

Expected: `echod` logs its resolved device ID, the wake configuration it will use, one statistics block every 2 seconds, and **no wake events** on the synthetic fixture; SIGINT exits 0 and leaves a final LED frame in `/tmp/ledfake`. `TestParseArgs_WakeOnlyDefaultsMatchWakeConfigDefaults`, `TestParseArgs_WakeThresholdOutsideUnitRangeIsRejected`, `TestParseArgs_MicChannelsAcceptsCommaList` and `TestParseArgs_FlagBeatsIniForWakeThreshold` pass.

### Task 23: HARDWARE — on-device wake and VAD session; §26 measurements

**Status:** not started

**Purpose:** This is §24's success criterion and §25 task 6. Everything before it is scaffolding.

**Dependencies:** Tasks 12, 15, 17, 19, 21, 22.

**Hardware required:** **yes** — rooted Dot as in Task 12, plus the vetted `okay_nabu.tflite`, `melspectrogram.tflite` and `embedding_model.tflite` verified on the host per `docs/wake-models.md`. A reasonably quiet room **and** a noisier condition such as music or television at conversational level.

**Files or components:**

- Modify: `docs/device-diagnostics.md`
- Create: `testdata/wake/recorded/okay_nabu_16k_mono.wav` — one short accepted utterance, at most 3 seconds, generator-exempt and documented as such
- Modify: `internal/device/wake/config.go` (`Defaults()` set to the measured thresholds and pre-roll) and `internal/device/audio/alsa/config.go` only if measurement requires

**Concrete changes:** Run a scripted measurement session and record in `docs/device-diagnostics.md` every §26 answer this milestone owns: the chosen mic channel; the measured wake and VAD thresholds with their false-accept and false-reject counts in both room conditions; the pre-roll length that avoids clipping without carrying the wake phrase; per-step wake and VAD inference times and total CPU and RSS while idle; whether `AlwaysScoreWake` can remain true within the CPU budget; whether the mute key gates capture in hardware; and the default model choice. Only then set `wake.Config.Defaults()` to the measured values, replacing the §16 placeholders.

**Expected outcome:** The Dot repeatedly detects `okay_nabu` locally with observable wake and VAD scores, no gateway involved, and every threshold in the code is a measured value rather than a guess.

**Verification:**

```sh
"$ADB" push ./models /data/local/tmp/models
"$ADB" shell /data/local/bin/echoctl wake install okay_nabu \
  --from /data/local/tmp/models/okay_nabu.tflite \
  --metadata /data/local/tmp/models/okay_nabu.json --sha256 <expected>
"$ADB" shell /data/local/bin/echoctl wake list
"$ADB" shell /data/local/bin/echoctl status --json > status-before.json

# A. VAD alone, live
"$ADB" shell /data/local/bin/echoctl wake vad-test --seconds 30 --print-steps

# B. wake + VAD threshold sweep
for t in 0.5 0.6 0.7 0.8 0.9; do
  "$ADB" shell /data/local/bin/echoctl wake test --model okay_nabu \
    --threshold $t --vad-threshold 0.5 --seconds 120 --print-steps
done

# C. VAD gate proof
"$ADB" shell /data/local/bin/echoctl wake test --model okay_nabu \
  --threshold <chosen> --vad-threshold 0.99 --seconds 60

# D. pre-roll sweep
for p in 100 250 400; do
  "$ADB" shell /data/local/bin/echoctl wake test --model okay_nabu \
    --threshold <chosen> --preroll-ms $p --seconds 60 --save-preroll /data/local/tmp/pre$p
done
"$ADB" pull /data/local/tmp/pre250 ./pre250

# E. arm64 reference agreement
"$ADB" shell 'ECHO_WAKE_MODEL_DIR=/data/local/etc/echo-satellite/wake-models \
  /data/local/bin/echoctl wake test --from-file /data/local/tmp/models/ref.wav'

# F. the success criterion
"$ADB" shell /data/local/bin/echod --wake-only --wake-model okay_nabu \
  --wake-threshold <chosen> --vad-threshold <chosen> --preroll-ms <chosen> \
  --stats-interval 60s --log-file /data/local/tmp/echod.log --log-max-bytes 1048576 --dbg &
# say the wake phrase 20 times spread over the run, then leave it idle 15 minutes
"$ADB" shell /data/local/bin/echoctl status --json > status-after.json
"$ADB" shell ls -l /data/local/tmp/echod.log*
```

Expected, each an explicit recorded observation:

- **A** — VAD scores rise clearly above the floor while speaking and fall back during silence, with a separable margin.
- **B** — at the chosen threshold, at least 18 of 20 deliberate utterances are accepted, each printing the model ID and both scores. The full sweep is recorded.
- **C** — with the VAD threshold at 0.99 the same utterances produce **zero** accepted wakes while the rejected low-VAD counter climbs. This is the direct proof the §16 gate is live and not decoration.
- **D** — the pulled pre-roll WAVs let a human hear where the wake phrase ends; the chosen value keeps the first command word intact without carrying the whole phrase.
- **E** — mel and embedding outputs on arm64 match the committed reference vectors within the same tolerance CI uses, proving no platform-specific numeric drift.
- **F** — over 30 minutes at least 18 of 20 utterances are accepted, there are **zero** wake events during the 15 idle minutes, no ALSA overrun log growth, RSS flat within 10% between `status-before.json` and `status-after.json`, CPU below the Task 17 budget, and the rotated log never exceeds `--log-max-bytes`.

**What cannot be verified without hardware:** all of A through F. The fixture-based tests in Tasks 16 through 22 prove the arithmetic and the gate logic; they say nothing about real-room false accepts or rejects, arm64 numeric agreement, CPU headroom, or long-run stability. **This task is not complete because a fixture run passed.**

### Task 24: Model lifecycle — replacing and training wake models

**Status:** not started

**Purpose:** After this milestone, installing or changing a wake model is the operation a user will perform most often. §16 requires stable model IDs, sidecar metadata and backend-independent wake configuration precisely so this is cheap, and that claim needs to be documented and demonstrated.

**Dependencies:** Tasks 19, 23.

**Hardware required:** **yes**, in part — the install and switch path is exercised on the device.

**Files or components:**

- Create: `docs/wake-model-training.md`
- Modify: `docs/wake-models.md`

**Concrete changes:** Document the whole lifecycle:

- **Replacing and switching**: `echoctl wake install` followed by `--wake-model` or the ini `wake.model` key. The shared frozen backbone, the interpreter, the pipeline and the gate are untouched, so no rebuild or redeploy of `echod` is required. Include the on-device commands and the `echoctl wake test` acceptance check to run before trusting a new model.
- **Training a new wake word**: the upstream openWakeWord pipeline — synthetic TTS clips, the automatic-training Colab notebook, roughly an hour, no local GPU — producing a small classifier trained on the **frozen** melspectrogram plus Google speech-embedding backbone and exported as `.tflite`. State explicitly that only the classifier is trained and the backbone is never retrained, which is why a new wake word costs no device change.
- **Constraints this runtime imposes**: the classifier must consume `[1, frames, 96]`; the **fully-connected export is preferred**, because the two-layer-RNN variant upstream permits requires recurrent kernels this interpreter does not implement, and `echoctl wake install` rejects it on the host naming the unsupported operators. Include that exact failure output.
- **Sidecar authoring**: the required `schema`, `id`, `kind` and `sha256` fields plus `phrase` and `languages`, and how to compute the digest.
- A note that gateway-driven asset synchronization is Milestone 4, that until then `echoctl` is the only install path, and that the Dot never fetches a model.

**Expected outcome:** A documented, demonstrated path for installing, switching and training wake models, proving §16's model-loading design actually delivers backend-independent wake configuration.

**Verification:**

```sh
"$ADB" shell /data/local/bin/echoctl wake install hey_jarvis \
  --from /data/local/tmp/models/hey_jarvis.tflite \
  --metadata /data/local/tmp/models/hey_jarvis.json --sha256 <expected>
"$ADB" shell /data/local/bin/echoctl wake list
"$ADB" shell /data/local/bin/echoctl wake test --model hey_jarvis --seconds 60 --print-steps
"$ADB" shell /data/local/bin/echoctl wake test --model okay_nabu --seconds 60 --print-steps
```

Expected: `wake list` shows both models with their own phrases and kinds; the new phrase is detected at its own threshold while the old one is not detected by the new model; switching between them requires **no rebuild and no redeploy of `echod`**. Additionally, attempting to install a model with an unsupported operator exits non-zero with a message naming the operator, matching the output documented in `docs/wake-model-training.md`.

### Task 25: Record the answers in `docs/DESIGN.md` and the developer documentation

**Status:** not started

**Purpose:** `AGENTS.md` requires a changed design assumption to be recorded in the same change, and §26 requires resolved questions to be written back rather than living in a plan.

**Dependencies:** Tasks 12, 15, 17, 23, 24.

**Hardware required:** no.

**Files or components:**

- Modify: `docs/DESIGN.md` (§16, §18, §26, §27), `docs/protocol.md`, `AGENTS.md`, `README.md`, `docs/device-diagnostics.md`

**Concrete changes:**

- §16: replace the example threshold values in the configuration block with the measured ones, note that they are measured and where the evidence is, record the default model, the pre-roll value, and whether wake inference runs when VAD is below threshold; add a pointer to `docs/wake-model-training.md`.
- §26: convert each resolved question into an answer line pointing at `docs/device-diagnostics.md` — the microphone path and channel arrangement; how directly EchoLocal was reusable (copied and adapted rather than imported, because its packages are `internal/`, with the NEON assembly borrowed under a `noasm` fallback); the cleanest local Silero-VAD implementation for the Go/ARM64 path (**blocked — record the evidence table, and that the shipped VAD is the adapted level detector and is not Silero-equivalent**); the CPU and memory impact, with Task 17's NEON and `noasm` figures and the pass or fail against the step budget; whether wake VAD runs before all engines (deferred — only one engine exists, and the per-engine flag lives in `wake.Config`); the default model; the wake and VAD thresholds; the pre-roll length; **beamforming initially bypassed**; AEC deferred to barge-in; and the exact speaker format with the resampling location. Questions this milestone does not own stay open and unchanged.
- §18: record that the diagnostic commands are cross-built for `linux/arm64` and run on the Dot over `adb shell`, that `echoctl wake install` takes a local path with a required digest, and that `echoctl status --json` is the bug-report artifact.
- §27: note that Go assembly is permitted while cgo is not, so "pure Go" is not read as "no `.s` files" (Ambiguity 8).
- `docs/protocol.md`: add `muted`, `offline` and `update_trial` to the device-state list.
- `AGENTS.md`: update "Repository state" to say Milestone 1 has landed with the device hardware and local wake stack; add `build-device-ctl`, `build-device-noasm`, `check-portability` and `bench` to the build and test commands; add a short note that hardware access is confined to `internal/device/audio/alsa` and `internal/device/mixer` behind build tags with `!linux` stubs, while LED, buttons and system use injected roots instead of tags.
- `README.md`: a short local-wake-stack paragraph, how to install and switch a wake model, and a pointer to `docs/device-diagnostics.md`.

**Expected outcome:** No §26 question this milestone touched is still listed as open, and no threshold in the code or the design document is an unmeasured guess.

**Verification:**

```sh
grep -n "tune on real device\|example only" docs/DESIGN.md
grep -n "beamforming" docs/DESIGN.md
grep -n "muted\|offline\|update_trial" docs/protocol.md
make fmt-check lint test check-portability build build-device build-device-noasm build-device-ctl
```

Expected: no `example only` or `tune on real device` placeholder remains in §16; §26 records "beamforming initially bypassed" and the Silero evidence; the three new device states appear in `docs/protocol.md`; every build and check target exits 0.

## Cross-task risks

- **Pure-Go inference misses the real-time budget.** Impact: the milestone fails on CPU and the whole wake path needs a different runtime. Mitigation: Task 17 measures it on the device **before** the pipeline is built on the interpreter, with a stated target and a pre-agreed escalation ladder, so the response is a recorded decision rather than a late scramble.
- **NEON assembly is subtly wrong on unusual lengths.** Impact: silent numeric corruption that looks like a bad model. Mitigation: Task 3 tests both build tags against a naive loop across many lengths including non-multiples of the SIMD width and zero, and Task 16 runs the reference vectors under `noasm` as well.
- **The Dot's mic channel layout differs from the researched 7 + 2.** Impact: the wake path could be fed a loopback channel and never detect anything. Mitigation: Task 12 measures the layout with `--print-levels` and the speaker-playback cross-check that makes the loopback channels self-identifying, before any wake work runs on hardware; channel selection is configuration, not a constant.
- **The ported interpreter is subtly wrong.** Impact: wake never fires or fires randomly, and it presents as a threshold problem. Mitigation: Task 16 gates on the reference vectors before anything depends on it, and Task 23 step E re-checks on arm64.
- **A newly trained or third-party model uses operators the interpreter lacks.** Impact: a model that installs cleanly and then fails on the device. Mitigation: `Model.UnsupportedOpcodes()` plus install-time rejection naming the operators (Tasks 16 and 19), and `docs/wake-model-training.md` directing users to the fully-connected export.
- **The level-based VAD rejects real speech or admits noise.** Impact: false rejects, or no gating benefit at all. Mitigation: the threshold is configurable, the rejected low-VAD counter makes the gate observable, Task 23 measures both room conditions, and the `wake.VAD` interface allows a Silero swap later without touching the gate or pipeline.
- **On-device logs fill storage.** Impact: a wedged device. Mitigation: `system.RotatingWriter` with a byte cap, asserted by test and verified in Task 23 step F.
- **ALSA capture blocked by a running Alexa service.** Impact: `ErrDeviceBusy` and a blocked milestone. Mitigation: a distinct sentinel with a doc note naming the likely cause, and Task 12 records the exact service and stop procedure.
- **A third-party model binary or a raw recording gets committed.** Impact: licence violation, repository bloat, or a privacy breach. Mitigation: `.gitignore` blocks `*.tflite` and `*.onnx` with a narrow allowlist for synthetic fixtures; committed recordings are limited to three short files, individually named as generator-exempt, and reviewed.
- **`dupl` at threshold 100 and `gocyclo` at 20 firing on the new command files.** Impact: pressure to add blanket suppressions. Mitigation: the shared `cmd/echoctl/capture.go` helpers exist specifically to keep the diagnostic commands from being near-identical wiring blocks, and `make lint` runs per task rather than at the end.
- **Coverage below 70% in `alsa` and `mixer`.** Impact: an acceptance item fails. Mitigation: both tasks pre-declare the justification and name exactly which statements are untestable off-device, while keeping all arithmetic in untagged, fully covered files.
- **Scope creep into Milestone 2.** Impact: a WSS client or a `turn.start` send lands without a plan. Mitigation: `wake.Event` stops at a local event with a pre-roll buffer, `echod --wake-only` opens no socket, the `Fanout` doc names the Milestone 2 turn streamer as an additional subscriber, and `status --json` field names anticipate the Milestone 2 wire types. Adding any of it is a plan amendment.
- **The LED's last frame persists after exit.** Impact: a crashed daemon leaves a misleading ring, for example red for muted when it is not. Mitigation: documented in the `led` package, `Clear()` on shutdown in Task 22, and `led test --clear` verified in Task 15.

## Rollback or recovery

All work is additive on a `milestone-1-hardware-wake` branch off `master`. Host-side recovery is `git checkout master` and deleting the branch. `internal/device/*` is empty today, so reverting a task cannot regress the Milestone 0 contracts. The only pre-existing files modified are `Makefile`, `.github/workflows/ci.yml`, `.gitignore`, `cmd/echoctl/*`, `cmd/echod/*`, `internal/protocol/messages.go` (additive constants only) and the documentation set — each a separately revertible edit.

Device-side recovery is deliberately trivial because this milestone installs no supervisor, creates no symlinks, and never writes `/system`:

```sh
"$ADB" shell /data/local/bin/echoctl led test --clear   # before removing the binary, if the ring is lit
"$ADB" shell pkill -f /data/local/bin/echod
"$ADB" shell rm -f /data/local/bin/echod /data/local/bin/echoctl /data/local/bin/echoctl-noasm
"$ADB" shell rm -rf /data/local/etc/echo-satellite /data/local/tmp/models \
  /data/local/tmp/pre* /data/local/tmp/echod.log*
```

Two device-state caveats: `speaker test` changes `Ext_Speaker_Amp_Switch` and must restore the prior value on every exit path including signals, which `TestSpeakerTest_RestoresAmpStateOnFailure` asserts; and the LED ring keeps whatever frame was written last, so `led test --clear` is part of leaving the device as found. Any Alexa service stopped to free the PCM device is restored by a reboot, and the procedure is recorded in `docs/device-diagnostics.md`.

## Final acceptance criteria

- [ ] `make fmt-check lint test check-portability build build-device build-device-noasm build-device-ctl` all pass locally, with `make lint` reporting 0 issues and no blanket suppressions.
- [ ] `go test ./...` passes on darwin/arm64 **and** linux/amd64, with no test requiring `/dev/snd`, a wake model, or a device; model-dependent tests skip cleanly when `ECHO_WAKE_MODEL_DIR` is unset.
- [ ] GitHub Actions is green, including the `check-portability` step, both device variants, and the `echoctl-linux-arm64` artifact.
- [ ] **No cgo anywhere.** `file .bin/linux_arm64/echod` reports a statically linked ARM aarch64 ELF, and the NEON and `noasm` builds agree numerically on the reference vectors.
- [ ] `mic -> WAV`: `echoctl mic record` on the Dot produces an intelligible 16 kHz mono WAV, and the 9-channel capture identifies which channels are microphones and which are the playback loopback.
- [ ] `WAV/PCM -> speaker`: `echoctl speaker test` produces an audible tone and plays back a recorded WAV on the Dot, and restores the amplifier control to its prior value.
- [ ] `LED/button access`: `echoctl led test --all-states` shows nine distinguishable patterns and `--clear` leaves the ring dark; `echoctl buttons test` reports correct tap, hold and repeat semantics for Action, Mute, VolumeUp and VolumeDown.
- [ ] `mic -> local VAD -> wake engine -> wake event`: `echod --wake-only` on the Dot accepts at least 18 of 20 deliberate `okay_nabu` utterances over 30 minutes with **zero** wake events during 15 idle minutes, logging wake score, VAD score and pre-roll length for each acceptance — **§24's success criterion**.
- [ ] The VAD gate is proved live: with `--vad-threshold 0.99` the same utterances yield zero acceptances while the rejected high-wake/low-VAD counter increases.
- [ ] **Per-step inference fits the budget on real hardware**, with Task 17's NEON and `noasm` p50/p95/max, CPU percentage and RSS recorded in `docs/device-diagnostics.md`, and any escalation decision documented. **Currently failing:** the corrected `tflite.Stream` re-run measured 42.616 ms NEON / 65.342 ms `noasm` p50 and 80.361 ms NEON / 125.880 ms `noasm` p95 against the 20 ms combined wake + VAD budget. The streaming measurement is about 9.8x faster than the earlier erroneous full-window NEON embedding measurement, but it confirms that the streaming embedding hot path still needs optimization before the budget is met (see `docs/device-diagnostics.md`, 2026-08-21 corrected re-run).
- [ ] Every §16 diagnostic field appears in `echoctl wake test`, in `echoctl status --json`, and in `echod`'s periodic statistics, with inference timing reported as percentiles.
- [ ] On-device logging is bounded: the rotated log never exceeds `--log-max-bytes`.
- [ ] The ported interpreter's mel and embedding outputs match the reference vectors within tolerance **on the Dot's arm64**, not only in CI.
- [ ] `echoctl wake install` rejects a digest mismatch, a sidecar/tensor-shape kind mismatch, **a model with unsupported operators**, and a URL source, and leaves no partial file behind on failure.
- [ ] **A second wake model can be installed and switched to with no rebuild or redeploy of `echod`**, and `docs/wake-model-training.md` documents replacing and training models end to end.
- [ ] `internal/device/wake/config.go` defaults are the values measured in Task 23, and `docs/DESIGN.md` §16 records them with a pointer to the evidence.
- [ ] `docs/DESIGN.md` §26 records answers for the microphone channel arrangement, EchoLocal reuse including the NEON borrow, the Go/ARM64 VAD choice **with the Silero-runtime evidence**, CPU and memory impact **with the benchmark numbers**, the default model, both thresholds, the pre-roll length, **beamforming initially bypassed**, AEC deferral, and the speaker format with the resampling location.
- [ ] `docs/third-party-notices.md` records EchoLocal (MIT, adapted, per-file headers, assembly included), no third-party model binary is committed, and the repository remains MIT-only.
- [ ] Boundaries intact: no gateway wake or VAD surface anywhere; `echod --wake-only` opens no socket; no automatic recording and no raw-audio storage without an explicit flag; the PCM device is opened exactly once per process, proved by the `Fanout` test.
- [ ] New packages are at or above roughly 70% statement coverage, except `internal/device/audio/alsa` and `internal/device/mixer`, whose shortfall is justified with the untestable statements named.

## Progress log

- 2026-08-20: Task 16 claimed for inline implementation. Scope is confined to `internal/device/wake/tflite`, generated synthetic TFLite fixtures, committed reference vectors and attribution, third-party notices, and this plan document. The interpreter is adapted from EchoLocal commit `be6b0b00d7d5d765d859b3cbe0e19e127a0c2031`.
- 2026-08-20: Task 16 completed. Added the attributed pure-Go FlatBuffers/TFLite interpreter, row-safe streaming evaluator, `vec.Dot`/`vec.AXPY` hot paths, bad-model/shape/unsupported-op sentinels, custom-op-preserving opcode inventory, 18 generated analytical model fixtures covering every operator family used by the pinned mel and embedding graphs, and TensorFlow Lite reference-vector proofs. The pinned models matched at maximum absolute deviations `0.000551224` (mel) and `0.0000257492` (embedding), and streaming matched windowed inference. Fresh review found malformed-model panic paths, over-broad stream admission, incomplete model-free operator coverage, and custom-op identity loss; all were fixed with structural/constant validation, conservative row-local checks, expanded fixtures, and exact names. Re-review found constant-size, dynamic stream-parameter/final-output, and one-input `RESHAPE` gaps; all were fixed and the final re-review found no remaining issues. No findings were declined or postponed.

- 2026-08-20: Task 15 hardware execution confirmed the IS31FL3236 sysfs attributes, all 12 RGB segments, segment 0 between the microphone and Volume Down buttons, clockwise ordering, `led_current` attenuation indices 0 through 3, and the two real input nodes/key codes. Hardware contradicted three assumptions: sysfs writes require trailing newlines; Amazon's `ledcontroller` must be stopped in addition to disabling `boot_animation`; and the keypad emits no autorepeat. LED control and diagnostics were corrected to the measured contract, animation pacing and comet smoothing were aligned with EchoLocal's 40 ms approach after human feedback, and Task 14 remediation added local 200 ms volume repeats plus per-node key filtering. Focused race tests and lint pass; final repository checks and fresh-context review remain before completion.
- 2026-08-20: Task 15 and the Task 14 hardware remediation completed. `make fmt-check`, `make lint`, and `make test` passed; touched-package race coverage is 90.4% for `internal/device/buttons` and 98.0% for `internal/device/led`. Fresh-context review found two medium timer issues: stale callbacks could attach to a rapid repress, and kernel value-2 repeats could overlap synthetic repeats. Both were fixed with per-press generation tokens, kernel-repeat takeover, and deterministic release/repress regression coverage for volume and Action. Re-review found one test-quality issue, which was fixed by routing DOWN/UP/DOWN through the event handler and checking observable output; final re-review found no remaining issues. No findings were declined or postponed. The final all-zero frame persisted after process exit, and Amazon's `ledcontroller` was restored to its original running state.

- 2026-08-20: Task 14 claimed for inline implementation. Scope is confined to `internal/device/buttons`, `echoctl buttons test`, generated button fixtures, CLI wiring and tests, and this plan document.
- 2026-08-20: Task 14 corrected the planned evdev record size from 16 to 24 bytes. Two 64-bit timeval words plus type, code, and value total 24 bytes on the Dot's 64-bit Linux ABI; keeping 16 would truncate the value field and make real device decoding impossible.
- 2026-08-20: Task 14 completed. Added 64-bit evdev decoding, injected-root case-insensitive input discovery, deterministic Action/Mute tap and real-time hold semantics, VolumeUp/VolumeDown press-repeat semantics, cancelable closable-stream watchers, generated event fixtures, and `echoctl buttons test` for fixture and live multi-device input. Fresh-context review found three medium issues: hold-start was initially delayed until release, blocked reads were not interrupted by cancellation, and an early multi-device error could be suppressed at timeout. All three were fixed with threshold timers, watcher-owned closable streams, concurrent error handling with peer cancellation, and requirement-level regression tests. Re-review resolved every finding; none were declined or postponed. Repository formatting, lint, and race/coverage tests passed; `internal/device/buttons` reports 87.4% statement coverage. No hardware verification ran; real device names, nodes, and key codes remain Task 15.

- 2026-08-20: Task 13 completed. Added the 12-segment/36-channel RGB frame codec, injected-root sysfs device with duplicate-write suppression and current/boot-animation controls, exhaustive semantic-state rendering with visible unknown-state fallback, tick-driven animator, and `echoctl led test` with single/all-state, current, duration, clear, and absent fake-root support. Added the missing `muted`, `offline`, and `update_trial` protocol states plus a copy-returning documentation-order inventory and updated the wire protocol documentation. Focused race tests, fake-root CLI verification, package lint, and repository formatting/lint/race tests passed; `internal/device/led` reports 97.6% statement coverage. Fresh-context review found two medium acceptance gaps: the initial exhaustiveness test could not distinguish a missing state case from the unknown-state fallback, and the exact fake-root command failed when its directory was absent. Both were fixed with an explicit known-case signal covered against `AllDeviceStates()` and safe creation of non-default diagnostic roots. No findings were declined or postponed. Hardware was not required; visual and sysfs verification on the Dot remains Task 15.
- 2026-08-20: Task 13 claimed for inline implementation. Scope is confined to `internal/device/led`, the additive protocol state constants and documentation, `echoctl led test`, their tests, and this plan document. Implementation and repository-wide checks passed; fresh-context review is in progress before the task is marked complete.
- 2026-08-20: Task 12 completed. After the physical mute GPIO was cleared, spoken capture raised all seven physical channels to peaks between -34.36 and -28.69 dBFS while loopback channels 7–8 remained digital zero. A separate five-second mic0 capture peaked at -37.00 dBFS, played 240,000 frames at 48 kHz stereo, and a nearby human confirmed the speech was clear and intelligible. Together with the previously confirmed audible tone, PCM formats, loopback identification, serial source, real fixture, ALSA playback-start correction, and review remediation, every Task 12 verification item is complete.
- 2026-08-20: Task 12 confirmed the red mute ring represented EchoLocal's physical microphone-cut line, not ALSA's `MFP Gpio Mute` control. The GPIO controller base measured 357; MTK pin 87 therefore resolved to GPIO 444. It was initially unexported and high. Exporting it and driving it low connected the microphones, and a nearby human confirmed the red ring went out. The reproducible diagnostic records the measurement and warns production code to resolve the base rather than hardcode GPIO 444; Task 23 retains ownership of mute-button behavior.
- 2026-08-20: Task 12 human verification confirmed the three-second 1 kHz speaker test was clearly audible. Remaining human checks are speech raising the physical microphone channels and intelligible recorded-speech playback.
- 2026-08-20: Task 12 hardware execution completed except for human audibility and intelligibility observations, so the task remains in progress. Device `G090LF0964060EHP` confirmed the `/proc/cmdline` serial fallback, seven physical mic channels at indices 0–6, playback-loopback channels at 7–8, 16 kHz/9-channel/S24_3LE capture with 320-frame periods and eight periods, and 48 kHz/stereo/S16_LE playback with 1024-frame periods and four periods. The real two-second room fixture and reproducible session notes were added. Hardware exposed unconditional playback `START` as invalid (`EPIPE` on an empty prepared stream); fixed by explicitly starting capture only and letting playback autostart on write. Fresh-context review found that XRun recovery also needed to restart capture and that the real fixture needed an integrity assertion; both were fixed with direction-specific recovery tests and a pinned SHA-256. The review's remaining acceptance finding is retained: a nearby human must confirm the tone is audible, speech raises channels 0–6, and recorded speech is intelligible on host and device before Task 12 can be completed. No findings were declined or postponed.
- 2026-08-20: Task 11 completed. Added the `echoctl mic record` and `echoctl speaker test` command groups, shared ALSA/file source and sink wiring, selected/all-channel S24_3LE capture into 16 kHz WAV, per-channel peak/RMS dBFS reporting, generated or WAV-backed playback, selectable sinc/linear/hold resampling into 48 kHz stereo, volume control, and amplifier read/enable/restore with signal cancellation. File-backed CLI verification, focused race tests, package lint, and repository formatting/lint/race tests passed; `cmd/echoctl` reports 72.6% statement coverage. Fresh-context review found three issues: simultaneous playback and amplifier-restore failures hid the restoration error, final close errors could be discarded, and the fixture-level test checked labels without checking amplitudes. All were fixed by joining errors and retrying failed restoration from the deferred safety path, explicitly closing successful outputs/sources, and asserting all nine known fixture peaks. No findings were declined or postponed. Hardware was not required; real ALSA capture, playback, and amplifier behavior remain Task 12.
- 2026-08-20: Task 10 completed. Added the EchoLocal-attributed, cgo-free Linux ALSA mixer control wrapper for `Ext_Speaker_Amp_Switch`, supporting Boolean and enumerated `On`/`Off` controls, explicit control-not-found errors, and a signature-compatible non-Linux stub. The exported API documents the read-before-write and restore-on-every-exit-path contract used by diagnostics. Focused race tests, 64-bit ALSA UAPI size/ioctl assertions, package lint, Darwin/arm64 and Linux/arm64 portability builds, and repository formatting/lint/race tests passed. Fresh-context review found two issues: the ALSA value union was initially undersized, producing invalid READ/WRITE ioctl numbers, and syscall argument liveness was not explicit. Both were fixed with the correct 1024-byte union, ABI regression assertions, and `runtime.KeepAlive`; no findings were declined or postponed. The Linux ioctl path remains deliberately hardware-untested here and reports 1.2% statement coverage; real amplifier read/set/restore verification remains Task 12.
- 2026-08-20: Task 9 completed. Added injected-root device serial resolution with the required three-source precedence; stable explicit, serial-derived, persisted, or atomically generated identity; `/proc` CPU/RSS parsing and successive CPU-percentage sampling; canonical device paths; and a fixed-count rotating writer that bounds both new and pre-existing numeric log generations. Focused race tests, package lint, and repository formatting/lint/race tests passed; `internal/device/system` reports 78.4% statement coverage. Fresh-context review found three issues: generated IDs were initially written directly to the final path, valid repeated hyphens were collapsed, and reducing the log file count left stale generations. All three were fixed with fsynced same-directory temporary-file publication, regex-faithful sanitization, and stale numeric-generation cleanup; no findings were declined or postponed. Hardware was not required; the real Dot serial source remains Task 12.
- 2026-08-20: Task 9 claimed for inline implementation. Scope is confined to `internal/device/system/{paths.go,serial.go,identity.go,resources.go,logrotate.go}`, their one-per-source tests, and this plan document.
- 2026-08-20: Task 8 completed. Added the consumer-owned `PCMSource` contract and paced raw/WAV `FileSource`; the DSP `Preprocessor` seam with deliberate `Bypass`; canonical capture conversion with selected-channel downmix, immutable frame ownership, contiguous offsets, XRun logging/counting/recovery, and provider frame-count validation; and non-blocking single-source `Fanout` with pre-run subscriptions and per-subscriber drop counters. Focused race tests, package lint, and repository formatting/lint/race tests passed; `internal/device/audio` reports 83.7% statement coverage. Fresh-context review found unchecked source frame counts, insufficiently explicit proof that one source read sequence feeds all subscribers, and an undocumented shared-sample contract. All three were fixed with bounds validation/tests, a read-count assertion across subscribers, and an immutable `Frame.Samples` contract; no findings were declined or postponed. Hardware was not required; the composition root that opens ALSA once lands in Task 22.
- 2026-08-20: Task 7 completed. Added the cgo-free ALSA PCM provider with validated measured microphone and speaker configurations, fixed `/dev/snd` path construction, exact 64-bit `snd_pcm_hw_params` encoding, Linux/arm64 ioctl-backed capture/playback, busy and xrun error mapping, whole-frame transfers, driver-grant validation, and signature-compatible non-Linux stubs. Promoted `golang.org/x/sys` to a direct dependency. Focused tests, arm64 vet, package lint, Darwin/arm64 and Linux/arm64 portability builds, and repository formatting/lint/race tests passed; the ALSA package reports 39.5% statement coverage because the Linux open/ioctl paths require hardware, as anticipated by the task's coverage exception. Fresh-context review found one medium-severity unsafe-pointer lifetime issue; fixed with `runtime.KeepAlive` after transfer ioctls and reran all checks. No findings were declined or postponed. Hardware was not required; real device capture/open remains Task 12.
- 2026-08-20: Task 6 completed. Added allocation-free-after-warmup hold, linear, and 32-tap/1024-phase Blackman-windowed sinc resamplers, with the sinc accumulation routed through `vec.Dot`; consumer-owned `PCMSink`, `WAVSink`, and `NullSink`; and device-side `Player` conversion from canonical 16 kHz mono into validated sink-rate, interleaved S16_LE period writes. The 16-to-48 kHz 1 kHz tone measured 80.07 dB SNR both directly and end to end through `Player` into `WAVSink`; the sinc downsampling path reduced above-Nyquist sweep RMS versus sample-and-hold. Focused tests, package lint, repository formatting/lint/race tests, and darwin/arm64 plus linux/arm64 portability builds passed; `internal/device/audio` reports 87.0% statement coverage. Fresh-context review found three issues: cancellation was checked only after resampling, the resampler ratio was not validated against the sink rate, and the required end-to-end Player-to-WAVSink quality test was absent. All three were fixed with pre/post-resample cancellation checks, finite/exact ratio validation, and a 48 kHz stereo WAV integration test; none were declined or postponed. Hardware was not required; speaker audibility remains Task 12.
- 2026-08-20: Task 6 claimed for inline implementation. Scope is confined to `internal/device/audio/{resample.go,sink.go,playback.go}`, their one-per-source tests, and this plan document.
- 2026-08-20: Task 5 completed. Added the swappable `wake.VAD` contract, `AlwaysSpeech`, the canonical 16 kHz/1280-sample wake step, and an EchoLocal-adapted clock-free level detector with fast-falling/slow-rising room-floor tracking, a 12 dB speech margin, bounded AGC gain, and a 0..1 speech score. The `vadlevel.Scorer` validates fixed-size wake steps and supports deterministic reset/close behavior. Focused tests, package lint, repository formatting/lint/race tests, and darwin/arm64 plus linux/arm64 portability builds passed; both new wake packages report 100% statement coverage. Fresh-context review found one medium-severity acceptance gap: separability and reset tests used ideal generated frames instead of committed fixtures. Fixed by reading the checked-in silence and 1 kHz tone WAVs and asserting their score trace and margin; no findings were declined or postponed. Hardware was not required; real-room behavior remains Task 23.
- 2026-08-20: Task 4 completed. Added the portable `internal/device/audio` core with validated PCM formats, canonical 16 kHz mono frames, S24_3LE/S16LE decoding, physical-channel selection, mono downmix, a bounded overwrite-oldest pre-roll ring, validated 16-bit PCM WAV reading/writing, and deterministic two-second WAV/raw fixtures. Focused verification, fixture regeneration with before/after SHA-256 comparison, package lint, portability builds, and repository-wide formatting/lint/race tests all passed; the new package reports 86.4% statement coverage. Fresh-context review found three issues: the raw device fixture used 48 kHz instead of the verified 16 kHz format, malformed WAV chunk sizes could cause excessive allocation, and PCM byte-rate/block-alignment/whole-frame consistency was not checked. All three findings were fixed and covered by tests; none were declined or postponed. Hardware was not required for this task.
- 2026-08-20: Execution resumed by Codex (/root) after the physical-device workflow prerequisite completed. Task 2 claimed for implementation.
- 2026-08-20: Task 2 completed. Added third-party notices, EchoLocal MIT license text, wake-model asset documentation with verified upstream URLs and SHA-256 digests, the device `echoctl` build, `noasm` device build, portability and benchmark Make targets, CI portability/device artifact steps, and model-binary ignore rules. Verification passed: `make check-portability build-device-ctl build-device-noasm`; `file .bin/linux_arm64/echoctl` reports a statically linked ARM aarch64 Linux ELF. `make fmt-check`, `make lint`, and `make test` also passed. Local `go build` emitted non-fatal sandbox warnings about a read-only module stat cache, but each command exited 0. Self-review found one low-severity mutable provenance issue for `okay_nabu`; fixed by pinning the rhasspy/pyopen-wakeword raw URL to commit `6bc5c5f5c9c71e46a723b6c9277b1d50f2ba13fd`.
- 2026-08-20: Task 3 claimed for inline implementation. Scope is confined to `internal/device/vec` and this plan document.
- 2026-08-20: Task 3 completed. Added `internal/device/vec` with EchoLocal-adapted arm64 NEON `Dot` and `AXPY`, portable `noasm` fallbacks, and tests covering empty slices, short-slice panics, SIMD-boundary lengths, exact inputs, bounds preservation, and benchmarks. Verification passed: `go test ./internal/device/vec/... -v`; `GOOS=linux GOARCH=arm64 go build ./internal/device/vec/...`; `GOOS=linux GOARCH=arm64 go build -tags noasm ./internal/device/vec/...`; `go test ./internal/device/vec -bench . -benchmem`; `make fmt-check`; `make lint`; `make test`; `make check-portability build-device build-device-noasm build-device-ctl`. The new package reports 100% statement coverage on the portable path; arm64 assembly is build-checked here and measured later on hardware in Task 17.
- 2026-08-20: Self-review found two low-severity documentation drift issues after Task 3: the `okay_nabu` install example still used the old mutable-source model ID, and the third-party notice omitted the adapted Go wrapper files. Both were fixed. The review also asked whether CI should upload `echod-noasm`; declined for Task 2 because the plan requires CI to build both variants but only requires uploading `echoctl` alongside the existing `echod` artifact.
- 2026-08-20: Task 1 workflow remediation completed on physical device
  `G090LF0964060EHP`. Windows ADB 37.0.1, invoked both directly and from WSL2,
  reported `biscuit`, `arm64-v8a`, Magisk UID 0 and permissive SELinux. The new
  Make targets built, pushed and executed the stamped binary from
  `/data/local/tmp`; foreground logs showed startup, capabilities and gateway
  resolution, and Ctrl+C ended with the postcondition `echod stopped`. Native
  WSL ADB 34.0.4 saw no USB device and remains excluded from the workflow.
  Review found that preflight initially printed rather than enforced device
  identity and that ADB exit 58 was accepted without a process postcondition;
  both findings were fixed and reverified. `make fmt-check`, `make lint` (0
  issues), and `make test` (race enabled, 74.5% total coverage) passed after LF
  enforcement corrected Windows checkout corruption of byte-sensitive release
  fixtures. LED and speaker hardware checks remain Tasks 12 and 15.
- 2026-08-19: Plan created from `docs/DESIGN.md` §24 Milestone 1 and §25 tasks 2–6, after a research pass over EchoLocal, openWakeWord and the available CGO-free ONNX runtimes. Decisions recorded before execution: openWakeWord only, with microWakeWord deferred behind an unchanged `Engine` interface; **wake VAD via the adapted EchoLocal level/AGC speech-over-floor detector**, because Silero is blocked under `CGO_ENABLED=0` (evidence table in "Why the wake VAD is new work"); DSP bypassed with a `Preprocessor` seam; diagnostics delivered by a cross-built `linux/arm64` `echoctl` run over `adb shell`, plus `status --json`; `echod --wake-only` runs the real pipeline because §24 requires *repeated* detection; **NEON assembly borrowed with a `noasm` fallback**, since Go assembly needs no cgo; build tags confined to `internal/device/audio/alsa` and `internal/device/mixer`, while LED, buttons and system use injected roots; EchoLocal code copied and adapted with MIT attribution because its packages are `internal/` and cannot be imported; **no new Go module dependencies**, only `golang.org/x/sys` promoted to direct.
- 2026-08-21, Task 18: Added the portable `wake.Engine` contract and minimal `wake.Model` contract, moving the latter forward from Task 19 to resolve Task 18's circular dependency. Implemented the openWakeWord shared mel/embedding stream, classifier ring and model-kind detection. Fresh review found missing scalar and float-input validation, shared-model input validation, obscured embedding preparation errors, and CI coverage gaps; all were fixed. A 12 KiB synthetic embedding fixture with time carry and zero-weight in-memory mel model now cover streaming scale/offset, lookback and reset in CI; real model compatibility remains an explicit supplemental test. The CI-style `oww` package coverage is 60.5%; lower than the usual 70% guideline because `New`'s filesystem/TFLite classifier-loading paths can only be exercised by non-committed third-party model weights, and the real-model run below exercises them. No findings were declined or postponed.

## Completion evidence

To be filled in during execution: the exact commands from each task's Verification block, their outcomes, and the date, with hardware and non-hardware checks recorded separately per `docs/plans/README.md`.

- 2026-08-20, Task 16: `go test ./internal/device/wake/tflite/... -v`; `ECHO_WAKE_MODEL_DIR=/tmp/echo-task16-models go test -race ./internal/device/wake/tflite/... -v`; `go test -tags noasm ./internal/device/wake/tflite/...`; fixture regeneration plus before/after SHA-256 comparison; `golangci-lint run ./internal/device/wake/...`; `make check-portability build-device build-device-noasm`; `make fmt-check`; `make lint`; and `make test` passed. The three downloaded temporary models matched the documented SHA-256 digests, all required only supported opcodes, mel/embedding maximum deviations were `0.000551224`/`0.0000257492`, and stream/windowed inference agreed. CI-style race coverage for `internal/device/wake/tflite` was 77.2%. Synthetic regeneration was byte-identical. Fresh review and two remediation re-reviews resolved every finding; none were declined or postponed. No hardware verification applies; arm64 numeric agreement and timing remain Tasks 17 and 23.
- 2026-08-21, Task 18: `go test ./internal/device/wake/oww -v -cover` passed (60.5% coverage); `ECHO_WAKE_MODEL_DIR=$PWD/.assets/wake-models go test ./internal/device/wake/oww -v` passed with real mel, embedding and `okay_nabu` classifier assets; `go test ./internal/device/wake/tflite -run TestFixtures_Regenerate -update-fixtures` passed with no post-generation diff; and `golangci-lint run ./internal/device/wake/...` passed with 0 issues. No hardware verification applies. Repository-wide `make fmt-check`, `make lint` and `make test` were also attempted but remain blocked by unrelated Windows/baseline failures: `fmt-check` invokes an unavailable `out` command, lint reports six existing findings outside Task 18, and tests fail in existing `/proc` resource, directory-sync and CLI paths.
- 2026-08-21, Task 17: Added `echoctl bench`, reproducing the mel/embedding/classifier streaming shapes from each model's own input tensors (only the mel lookback constant, 480 samples, is hard-coded) plus the level-detector VAD, and reporting p50/p95/max per stage, CPU and RSS. `Makefile`'s `build-device-ctl` gained a `TAGS` variable so the same target builds both the default and `noasm` device binaries. Vetted `melspectrogram.tflite`, `embedding_model.tflite` and `okay_nabu.tflite` were downloaded per `docs/wake-models.md` and their SHA-256 digests confirmed. On the rooted `G090LF0964060EHP` Dot, 500-step runs of both the NEON and `noasm` `echoctl bench` builds **failed** the ≤ 20 ms combined wake+VAD budget by roughly 17× (NEON, 543.9 ms p95) to 27× (`noasm`, 753.8 ms p95). The embedding model's `CONV2D`-heavy backbone dominates (337.5 ms/499.2 ms p50); mel and classifier are individually within budget and NEON already helps them ~2–2.6×, but `internal/device/vec`'s NEON kernels only cover `Dot`/`AXPY`, not convolution. **Recorded escalation decision, per the plan's ladder:** extend the NEON kernels to accelerate `CONV2D` next; if that is insufficient, proceed to int8 quantization, then cgo with `libtensorflowlite`/XNNPACK, then a Python `tflite_runtime` sidecar. Verification: `go build ./...`; `gofmt -l -w .`; `go vet ./...`; `golangci-lint run ./cmd/echoctl/...` (0 issues); `go test ./cmd/echoctl/... -race -v` (including an end-to-end bench run against the real downloaded models); `make test` (all packages, race, coverage); `make check-portability build build-device build-device-noasm build-device-ctl`; `make build-device-ctl && make build-device-ctl TAGS=noasm` producing distinct NEON/`noasm` binaries confirmed via `file`; on-device `bench --model okay_nabu --model-dir wake-models --steps 500 --json` for both binaries, recorded above and in `docs/device-diagnostics.md`. Full findings, raw numbers and the reproduction commands are in `docs/device-diagnostics.md`'s 2026-08-21 section. No findings were declined or postponed; the budget-fail escalation decision is recorded rather than silently worked around, and implementing the `CONV2D` NEON kernel itself is explicitly out of Task 17's scope and left for Task 18/a follow-up.

- 2026-08-21, Task 17 review remediation: A pre-commit review comparing this milestone against EchoLocal's reference implementation found that `bench.go`'s embedding-stage measurement did not match Task 18's specified architecture: it ran the embedding model through a plain `Interpreter.Invoke()` over the full 76-row window every step, while EchoLocal's actual `oww.Engine` (and this repository's own `internal/device/wake/tflite/stream.go`, already ported from EchoLocal and proven bit-identical via `TestStreamMatchesWindowedModel`) evaluates it incrementally through `tflite.Stream`, recomputing only the rows the 8 new mel frames touch. `bench.go` was corrected to use `tflite.Stream` for the embedding stage (`loadBenchModels`/`runBenchStep`), and `bench_test.go`'s end-to-end test step count was raised from 8 to 16 so it clears the stream's warmup and still exercises the classifier. A host-side (amd64) comparison of `Interpreter.Invoke()` against `tflite.Stream.Write()` over the same real `embedding_model.tflite` measured 22.38 ms/step vs 2.91 ms/step — a ~7.7× reduction — which, applied as a rough ratio to the device's NEON p50, suggests the true on-device embedding cost is closer to ~44 ms than the recorded 337.5 ms, though this is not itself an on-device measurement. `go build ./...`, `gofmt -l -w .`, `golangci-lint run` (0 issues) and `make test` (78.3% total coverage, no failures) passed after the fix. **This does not change Task 17's on-device result, which stands as recorded above until re-run**: the 2026-08-21 hardware numbers in `docs/device-diagnostics.md` were produced by the uncorrected tool and must be reproduced with the corrected `bench.go` before the escalation decision (extend NEON to `CONV2D`) is acted on. See `docs/device-diagnostics.md`'s correction note for detail. Not declined or postponed: fixing the benchmark tool itself required no hardware and was done directly; re-running it on the Dot is recorded as the outstanding hardware-dependent verification.

- 2026-08-21, Task 17 review remediation hardware verification: Rebuilt the corrected `tflite.Stream` benchmark and ran both ARM64 variants for 500 steps on rooted Dot `G090LF0964060EHP` with the vetted `okay_nabu` assets. NEON measured mel/embedding/classifier/VAD/total p50 of 5.686/34.332/0.200/0.020/**42.616** ms and p95 of 11.658/68.313/0.336/0.039/**80.361** ms, at 100.6% CPU and 14,553,088 bytes RSS. `noasm` measured 12.103/47.653/0.347/0.020/**65.342** ms p50 and 25.876/99.321/0.670/0.037/**125.880** ms p95, at 100.6% CPU and 16,019,456 bytes RSS. The corrected streaming result is roughly 9.8x faster than the invalid full-window NEON embedding p50, but still misses the <= 20 ms combined wake + VAD target and has NEON p95 slightly above the 80 ms step interval. The escalation decision is now based on the correct pipeline: optimize the streaming embedding path's small-vector hot loops; the int8, cgo/XNNPACK, and Python-sidecar rungs remain fallback options. No findings were declined or postponed.


- 2026-08-20, Task 14: `go test ./internal/device/buttons/... -v -race`; `.bin/echoctl buttons test --from-file testdata/buttons/action_tap.bin --seconds 1`; `golangci-lint run ./internal/device/buttons/... ./cmd/echoctl/...`; `make fmt-check`; `make lint`; and `make test` all passed. The fixture CLI printed `action tap held=250ms`; generated-fixture drift, 24-byte event decoding, injected-root discovery, tap/hold/repeat semantics, real-time hold-start delivery, in-flight cancellation, and multi-device error preservation are covered. Race-enabled `internal/device/buttons` statement coverage was 87.4%. Fresh-context review's three findings were fixed and re-reviewed; none were declined or postponed. No hardware verification ran; Task 15 owns real input-node discovery and physical key-code confirmation.

- 2026-08-20, Task 15 and Task 14 remediation: Windows ADB against `G090LF0964060EHP` confirmed `/sys/bus/i2c/devices/0-003f/{frame,led_current,boot_animation}`, newline-terminated writes, the required `stop ledcontroller`, attenuation indices 0 and 3, all 12 RGB segments with segment 0 between microphone and Volume Down and clockwise ordering, all nine semantic patterns at the revised 40 ms cadence, and persistent clear-after-exit. `/dev/input/event1` (`mtk-kpd`) supplied Mute 113 and Action 138; `/dev/input/event2` (`keys`) supplied Volume Down 114 and Volume Up 115. Live runs confirmed Action/Mute tap and hold, generated Volume Up repeats at 200 ms, and one prompt Volume Down tap; raw `getevent` confirmed its DOWN/UP pair. `go test -race -count=10 ./internal/device/buttons/...`, `make fmt-check`, `make lint`, and `make test` passed; coverage was 90.4% for buttons and 98.0% for LED. Fresh review's two timer findings and one test-quality finding were fixed and re-reviewed; none were declined or postponed. `ledcontroller` was restored to `running` after verification.

- 2026-08-20, Task 13: `go test -race ./internal/device/led/... ./internal/protocol/... ./cmd/echoctl/...`; `golangci-lint run ./internal/device/led/... ./internal/protocol/... ./cmd/echoctl/...`; `.bin/echoctl led test --root <absent-temporary-root>/ledfake --all-states --seconds 0.01`; `make fmt-check`; `make lint`; and `make test` all passed. The CLI rendered all nine states and left a `frame` file containing exactly 72 hexadecimal characters. Race-enabled `internal/device/led` statement coverage was 97.6%. Fresh-context review's two findings were fixed and reverified; none were declined or postponed. No hardware verification ran; Task 15 owns real LED controller access and visual pattern confirmation.
- 2026-08-20, Task 12: `make device-check ADB=/mnt/c/tools/android-platform-tools/adb.exe DEVICE_SERIAL=G090LF0964060EHP`; `make build-device-ctl build-device`; device `mic record` for all channels and mic0; concurrent all-channel capture plus `speaker test`; live capture/playback `hw_params`; recorded-input playback; focused `go test -race ./internal/device/audio/... -count=1`; package lint; `make fmt-check`; `make lint`; and `make test` passed. Hardware confirmed serial fallback and both PCM formats/channel counts/period and buffer sizes; concurrent playback raised channels 7–8 from digital zero to -12.07 dBFS peak while 0–6 remained physical-mic room channels. EchoLocal's MTK pin 87 physical mute line resolved to GPIO 444, read high, and was driven low; the red ring went out. Spoken capture then raised channels 0–6 to peaks from -34.36 through -28.69 dBFS while 7–8 remained digital zero. Playback completed 144,000 generated-tone frames and 240,000 recorded-input frames after the playback-start fix; a nearby human confirmed both the tone and recorded speech were clear and audible. Repository-wide coverage was 72.0%; `internal/device/audio` was 83.7% and `internal/device/audio/alsa` was 41.6%. Task 12 verification is complete.
- 2026-08-20, Task 11: `make build`; `.bin/echoctl mic record --from-file testdata/audio/dot_mic_9ch_s24le.raw --channels all --seconds 2 --out /tmp/mic9.wav --print-levels`; `.bin/echoctl speaker test --in testdata/audio/tone_1k_16k_mono.wav --to-file /tmp/spk.wav`; `file /tmp/mic9.wav /tmp/spk.wav`; `go test ./cmd/echoctl/... -race -count=1`; `golangci-lint run ./cmd/echoctl/...`; `make fmt-check`; `make lint`; and `make test` all passed. The capture produced a 32,000-frame, 16 kHz, nine-channel 16-bit PCM WAV and reported the expected nine fixture peaks from -24.29 through -10.31 dBFS; playback produced a 48,000-frame, 48 kHz stereo 16-bit PCM WAV. Race-enabled `cmd/echoctl` statement coverage was 72.6%. Build commands emitted non-fatal read-only Go module stat-cache warnings and exited 0. No hardware verification ran; Task 12 owns the real Dot checks.
- 2026-08-20, Task 10: `make check-portability`; `go test -race ./internal/device/mixer/... -v`; `golangci-lint run ./internal/device/mixer/...`; Darwin/arm64 non-Linux test-binary cross-compilation; `make fmt-check`; `make lint`; and `make test` all passed. ABI tests assert the 64-bit Linux UAPI structure sizes and exact INFO/READ/WRITE ioctl request values. Race-enabled package coverage was 1.2%, matching the task's explicit hardware-ioctl coverage exception. No hardware verification ran; amplifier read/set/restore on the Dot remains Task 12 as planned. Cross-builds emitted non-fatal read-only Go module stat-cache warnings and exited 0.
- 2026-08-20, Task 9: `go test ./internal/device/system/... -race -count=1 -v` passed, including serial precedence/absence/error behavior, explicit/serial/persisted/generated identity and blank-file recovery, regex-faithful sanitization, `/proc` CPU/RSS parsing, CPU percentage sampling, oversized log writes, pre-existing file bounds, and stale-generation cleanup; `golangci-lint run ./internal/device/system/...` and `make lint` reported 0 issues; `make fmt-check` and `make test` passed. Race-enabled package coverage was 78.4%. No hardware verification applies; identifying the actual Dot serial source is deferred to Task 12 as planned.
- 2026-08-20, Task 8: `go test ./internal/device/audio -run 'TestCapturer|TestFanout|TestFileSource|TestBypass' -v -race` passed, including S24_3LE conversion, channel selection/downmix, contiguous offsets, XRun recovery, invalid provider frame-count rejection, raw/WAV replay, every-subscriber delivery from one source read sequence, late-subscription rejection, and non-blocking slow-subscriber drops; `golangci-lint run ./internal/device/audio/...` and `make lint` reported 0 issues; `make fmt-check` and `make test` passed. Race-enabled `internal/device/audio` statement coverage was 83.7%. No hardware verification applies.
- 2026-08-20, Task 4: `go test ./internal/device/audio/... -run 'TestDecode|TestSelect|TestRing|TestWAV|TestFixtures' -v` passed; `go test ./internal/device/audio -run TestFixtures_Regenerate -update-fixtures` produced byte-identical fixture SHA-256 hashes; `golangci-lint run ./internal/device/audio/...` reported 0 issues; `make check-portability`, `make fmt-check`, `make lint`, and `make test` passed. Race-enabled package coverage was 86.4%. No hardware verification applies.
- 2026-08-20, Task 5: `go test ./internal/device/wake/vadlevel/... ./internal/device/wake -run 'TestDetector|TestScorer|TestAlwaysSpeech' -v` passed, including committed-fixture separability and reset-trace coverage; `golangci-lint run ./internal/device/wake/...` and `make lint` reported 0 issues; `make fmt-check`, `make test`, and `make check-portability` passed. Race-enabled statement coverage was 100.0% for `internal/device/wake` and `internal/device/wake/vadlevel`. No hardware verification applies; real-room tuning is deferred to Task 23 as planned.
- 2026-08-20, Task 6: `go test ./internal/device/audio -run 'TestSinc|TestLinear|TestHold|TestPlayer' -v` passed, including 80.07 dB direct and Player-to-WAVSink tone SNR, stereo equality, exact 3x length, period chunking, cancellation/error propagation, sink-rate ratio validation, and sweep anti-aliasing coverage; `golangci-lint run ./internal/device/audio/...` and `make lint` reported 0 issues; `make fmt-check`, `make test`, and `make check-portability` passed. Race-enabled `internal/device/audio` statement coverage was 87.0%. No hardware verification applies; audible device playback is deferred to Task 12 as planned.

### Limitations and follow-up work (expected at completion)

- **The wake VAD is a room-adaptive level detector, not Silero** — a deliberate deviation from §27's "Silero/openWakeWord-compatible" wording, recorded in §26 with the runtime evidence. Revisit when a CGO-free ONNX runtime covers `Pad`, `Pow`, `ReduceMean` and `Sqrt` **and** opset 15 or later, or hand-port `silero_vad_openvino_16k.onnx` (167 nodes, 6 conv plus 1 LSTM) behind the existing `wake.VAD` interface.
- **The interpreter implements only the operators openWakeWord's shipped models need.** An RNN-based classifier export is rejected at install time; adding recurrent kernels is a scoped follow-up.
- **Training tooling is documented, not implemented here.** New models are produced by the upstream Colab pipeline on a host.
- **NEON kernels cover dot product and AXPY only.** The corrected Task 17 hardware re-run measures the streaming embedding path at 34.332 ms p50 / 68.313 ms p95 with NEON and 47.653 ms p50 / 99.321 ms p95 with `noasm`; total NEON is 42.616 ms p50 / 80.361 ms p95 against the 20 ms combined wake+VAD budget. This supersedes the invalid full-window embedding measurement (337.5 ms p50 NEON / 499.2 ms `noasm`) and confirms the remaining gap is materially smaller but still real. The next escalation is to extend `internal/device/vec` for the small-vector dot products used by the streaming path; if that is insufficient, use int8 quantization, then cgo with `libtensorflowlite`/XNNPACK, then a Python `tflite_runtime` sidecar. See `docs/device-diagnostics.md`'s 2026-08-21 corrected re-run for the full measurement.
- No gateway of any kind: no WSS, no mDNS, no `turn.start` on the wire, no binary audio framing. The pipeline stops at a local `wake.Event` with a pre-roll buffer (Milestone 2).
- No supervisor and no A/B slots: `echod` is launched by hand over ADB and does not survive a reboot (Milestone 3).
- DSP bypassed: no beamforming, noise suppression or AEC. Far-field behaviour is unoptimized by design.
- One engine and one active model. microWakeWord is unimplemented; the interface does not preclude it.
- Wake model binaries are not in the repository or in CI, so end-to-end model-dependent tests run only where `ECHO_WAKE_MODEL_DIR` is provided.
- The `internal/device/audio/alsa` and `internal/device/mixer` linux paths have no unit tests by necessity; their correctness rests on the hardware tasks.
- Command endpointing, STT, TTS and `dotsim` behaviour are untouched.

## Ambiguities in `docs/DESIGN.md` — flagged, not silently decided

1. **Device state set mismatch.** §7.6 lists nine states — `idle`, `listening`, `thinking`, `speaking`, `muted`, `offline`, `error`, `updating`, `update_trial` — but `internal/protocol` defines only six; `muted`, `offline` and `update_trial` are missing. LED rendering needs the full set to be exhaustive. Task 13 adds the three constants additively and updates `docs/protocol.md`. If the intent was that those three are device-internal and never on the wire, §7.6 should say so and the LED layer needs its own state type.
2. **Does wake inference run when the VAD score is below threshold?** §16's pseudocode computes both scores every frame and ANDs them, which implies always. Short-circuiting is observationally identical for the accept decision and saves CPU, but it makes `LastWakeScore` stale and degrades the §16 diagnostics. Handled as the `AlwaysScoreWake` configuration flag, default true, and measured in Tasks 17 and 23 rather than decided silently.
3. **"Should wake VAD run before all wake engines or only where supported or beneficial?" (§26.)** Unanswerable in this milestone because only one engine exists. Decision taken: keep `wake.Engine` entirely VAD-agnostic and put the per-engine VAD-gating flag in `wake.Config`, so the answer can be recorded when microWakeWord lands. §26 keeps this question open.
4. **Whether the hardware mute key gates the microphone in hardware.** Not addressed anywhere in the design, and it directly determines whether wake detection can survive a mute. Task 23 measures it; the answer belongs in §7.6 or §16 and is recorded in `docs/device-diagnostics.md`.
5. **Where §18's diagnostic commands execute.** §18 lists `echoctl mic record` and friends without saying whether they run on the host over ADB or on the device. Decision taken: cross-build and run on the device, because these commands need direct `/dev/snd` and sysfs access. Task 25 records it in §18.
6. **Pre-roll ownership.** §7.1 assigns the ring buffer to `echod` and §16 describes the behaviour, but neither says which component owns it. Decision taken and documented: `audio.Ring` is the mechanism, and `wake.Pipeline` owns the instance because only it knows the trigger instant. Stated so Milestone 2 does not add a second ring.
7. **S24 to S16 reduction.** No document specifies whether the 24-bit sample is truncated or scaled. Decision taken: right-shift by 8, taking the top 16 bits, matching EchoLocal and openWakeWord's int16 input expectation, with optional gain available through the `Preprocessor` seam. Recorded in the `convert.go` doc comment.
8. **Is Go assembly permitted?** §22 and `docs/development-windows-wsl.md` say "pure Go" and `CGO_ENABLED=0`, which Go assembly satisfies, but neither mentions assembly explicitly. Decision taken: permitted, with a `noasm` fallback so every path has a portable equivalent. Task 25 records this in §27 so "pure Go" is not later read as "no `.s` files".
