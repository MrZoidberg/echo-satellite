# Echo Dot device diagnostics

This document records reproducible hardware measurements for the rooted Echo
Dot Gen 2 used during Milestone 1. Commands that access audio devices run
through Magisk because the normal ADB shell is UID 2000 and the PCM nodes are
owned by `system:audio`.

## 2026-08-21 wake inference hardware benchmark (Task 17)

Device: same rooted `G090LF0964060EHP` (`csm_biscuit`/`biscuit`, `arm64-v8a`,
Magisk root) used throughout Milestone 1.

Vetted assets per `docs/wake-models.md` were downloaded to the host and their
SHA-256 digests confirmed against the table before pushing:

```text
c0aea21eb84a4ce90a08c870da41b7a7173b45269e6a3207c71d67c40f3a59d8  embedding_model.tflite
96fa0adccb6e8cf95cb14465409a1a2898ee4a96a85bb9ed3c7eb0e68bf163e8  melspectrogram.tflite
2982cecde4ee81cc7a2573d2602a7d54f0669425c94a7b64af77e0ff92b03a18  okay_nabu.tflite
```

Both `echoctl` variants were built and pushed, then each ran 500 synthetic
steps of `bench --model okay_nabu --model-dir wake-models --json`:

```sh
make build-device-ctl && adb -s G090LF0964060EHP push .bin/linux_arm64/echoctl /data/local/tmp/echoctl-neon
make build-device-ctl TAGS=noasm && adb -s G090LF0964060EHP push .bin/linux_arm64/echoctl /data/local/tmp/echoctl-noasm
adb -s G090LF0964060EHP shell './echoctl-neon  bench --model okay_nabu --model-dir wake-models --steps 500 --json'
adb -s G090LF0964060EHP shell './echoctl-noasm bench --model okay_nabu --model-dir wake-models --steps 500 --json'
```

Measured p50/p95/max per stage, in milliseconds, over 500 steps:

| Stage | NEON p50 | NEON p95 | NEON max | `noasm` p50 | `noasm` p95 | `noasm` max |
|---|---:|---:|---:|---:|---:|---:|
| mel | 6.882 | 11.681 | 12.127 | 17.778 | 25.965 | 26.729 |
| embedding | 337.541 | 532.969 | 543.621 | 499.159 | 729.087 | 773.668 |
| classifier | 0.234 | 0.347 | 0.532 | 0.467 | 0.701 | 0.779 |
| VAD (level detector) | 0.023 | 0.039 | 0.110 | 0.026 | 0.039 | 0.098 |
| **total** | **345.357** | **543.946** | **555.259** | **514.868** | **753.812** | **799.863** |

CPU and RSS during the run: NEON 100.3% CPU, 17,068,032 bytes RSS; `noasm`
100.3% CPU, 16,113,664 bytes RSS (`system.Sampler`/`system.ReadUsage` against
`/proc/self`, single core saturated throughout).

**Result: FAIL against the ≤ 20 ms combined wake + VAD budget**, by roughly
17× (NEON p95) to 27× (`noasm` p95). NEON is faster everywhere it applies
(mel is ~2.6× faster with NEON; classifier and VAD, both light `FULLY_CONNECTED`/
scalar work, are ~2× faster), but the embedding stage — the `CONV2D`-heavy
openWakeWord backbone — dominates total time and only sees a ~1.5× win from
NEON, because `internal/device/vec`'s NEON kernels cover only `Dot` and `AXPY`
(used by `FULLY_CONNECTED`), not the convolution kernel. Mel and classifier
individually are within budget; embedding alone is not, so this is a NEON
convolution-kernel gap rather than a general "pure Go is too slow" result.

**Escalation decision (recorded before further wake-pipeline code is
written, per the plan's stated ladder):** the first escalation step —
**extend `internal/device/vec`'s NEON kernels to accelerate `CONV2D`** in
`internal/device/wake/tflite`'s embedding-model execution path — is the
recorded next action. It is out of scope for Task 17 itself and is tracked as
a follow-up in the plan's Limitations section; Task 18 (the wake pipeline
built on this interpreter) inherits this as a blocking prerequisite for
meeting the per-step budget, though it is not blocked from proceeding with
correctness work using the interpreter as-is. If a `CONV2D` NEON kernel does
not close enough of the gap, the next ladder rungs are int8 quantization,
then cgo with `libtensorflowlite`/XNNPACK (breaks the pure-Go constraint),
then a Python `tflite_runtime` sidecar (breaks pure Go and the single-binary
A/B model).

Raw JSON reports: `bench-neon.json`, `bench-noasm.json` (not committed;
reproduce with the commands above).

**Correction (found during post-Task-17 review, before this task's results
were committed): the embedding stage above was measured with the wrong
evaluation strategy.** `echoctl bench` ran the embedding model through a
plain `Interpreter.Invoke()` over the full 76-row window on every step. That
is not what Task 18's `oww.Engine` is specified to do (§16 of the plan, "the
embedding model ... slides 8 mel frames per step") and it is not what
EchoLocal's own reference `oww.Engine` does: EchoLocal feeds its embedding
model through an incremental `tflite.Stream` that recomputes only the rows
the 8 new mel frames touch, not the whole window — this repository's
`internal/device/wake/tflite/stream.go` already implements the identical
mechanism (ported from EchoLocal) and is proven bit-identical to the windowed
model in `TestStreamMatchesWindowedModel`, but Task 17's `bench.go` did not
use it for the embedding stage.

This matters because the two are not close: on this host (amd64, not the
target hardware, so only the *ratio* is informative), the windowed
`Invoke()` cost was measured at 22.38 ms/step against 2.91 ms/step through
`tflite.Stream` for the same model and the same step shape — roughly a
**7.7× reduction**. Applying that ratio to the NEON on-device p50 above
(337.541 ms) as a rough order-of-magnitude estimate gives roughly **44 ms**,
which is still over the remaining budget (mel + classifier + VAD already
consume ~7.2 ms of the 20 ms combined target, leaving ~12.8 ms for
embedding) but by roughly 2–3×, not 17–27×. This does not replace an
on-device measurement — `bench.go` has been fixed to evaluate the embedding
stage through `tflite.Stream` (matching Task 18's planned architecture). The
corrected on-device re-run below is the measurement used to make the final
decision. The recorded
"extend NEON to CONV2D" escalation step may still be needed to close the
remainder, but a brand-new NEON kernel should not be written against a
benchmark that was measuring a different, more expensive computation than
the pipeline it was meant to gate.

### Corrected on-device re-run

The corrected `tflite.Stream` benchmark was built on 2026-08-21 and run for
500 steps on the same Dot and model assets:

```sh
make build-device-ctl
adb -s G090LF0964060EHP push .bin/linux_arm64/echoctl /data/local/tmp/echoctl-neon
adb -s G090LF0964060EHP shell '/data/local/tmp/echoctl-neon bench --model okay_nabu --model-dir /data/local/tmp/wake-models --steps 500 --json'

make build-device-ctl TAGS=noasm
adb -s G090LF0964060EHP push .bin/linux_arm64/echoctl /data/local/tmp/echoctl-noasm
adb -s G090LF0964060EHP shell '/data/local/tmp/echoctl-noasm bench --model okay_nabu --model-dir /data/local/tmp/wake-models --steps 500 --json'
```

Measured p50/p95/max per stage, in milliseconds:

| Stage | NEON p50 | NEON p95 | NEON max | `noasm` p50 | `noasm` p95 | `noasm` max |
|---|---:|---:|---:|---:|---:|---:|
| mel | 5.686 | 11.658 | 12.128 | 12.103 | 25.876 | 26.777 |
| embedding | 34.332 | 68.313 | 70.391 | 47.653 | 99.321 | 100.806 |
| classifier | 0.200 | 0.336 | 0.748 | 0.347 | 0.670 | 0.830 |
| VAD (level detector) | 0.020 | 0.039 | 1.351 | 0.020 | 0.037 | 0.264 |
| **total** | **42.616** | **80.361** | **82.516** | **65.342** | **125.880** | **126.841** |

CPU and RSS during the run: NEON 100.6% CPU and 14,553,088 bytes RSS;
`noasm` 100.6% CPU and 16,019,456 bytes RSS. The corrected streaming path is
about 9.8x faster at NEON embedding p50 than the prior full-window result, but
it still fails the <= 20 ms combined wake + VAD budget (2.1x at p50 and 4.0x
at p95). NEON is faster than `noasm` for every material stage, but its p95
also exceeds the 80 ms step interval. The escalation decision is therefore
confirmed: optimize the streaming embedding path's small-vector hot loops
before considering the later int8, cgo/XNNPACK, or Python-sidecar rungs.

## 2026-08-20 microphone and speaker session

Device:

- ADB serial: `G090LF0964060EHP`
- product/device: `csm_biscuit` / `biscuit`
- ABI: `arm64-v8a`
- root: Magisk UID 0
- SELinux: permissive
- binary revision: `milestone-1-hardware-wake-7745ba9-20260820T104738`

The binaries were staged under `/data/local/tmp`, the pre-supervisor workflow
documented in `docs/development-windows-wsl.md`:

```sh
export ADB=/mnt/c/tools/android-platform-tools/adb.exe
export DEVICE_SERIAL=G090LF0964060EHP
make device-check ADB="$ADB" DEVICE_SERIAL="$DEVICE_SERIAL"
make build-device-ctl build-device
"$ADB" -s "$DEVICE_SERIAL" push .bin/linux_arm64/echoctl /data/local/tmp/echoctl
"$ADB" -s "$DEVICE_SERIAL" push .bin/linux_arm64/echod /data/local/tmp/echod
"$ADB" -s "$DEVICE_SERIAL" shell \
  "chmod 755 /data/local/tmp/echoctl /data/local/tmp/echod"
"$ADB" -s "$DEVICE_SERIAL" shell /data/local/tmp/echod --version
```

### Serial source

`/sys/devices/soc0/serial_number` and
`/proc/device-tree/serial-number` are absent on this device. The normal shell
cannot read `/proc/cmdline`, but `echod` runs as root and finds
`androidboot.serialno=G090LF0964060EHP` there. This is already the second source
in `system.SerialReader`, so no source or precedence change is required.

Reproduce with:

```sh
"$ADB" -s "$DEVICE_SERIAL" shell "su -c 'cat /proc/cmdline'"
"$ADB" -s "$DEVICE_SERIAL" shell getprop ro.serialno
```

The property command is a diagnostic cross-check only; `getprop` is not an
identity source used by the agent.

### Microphone

The PCM nodes were closed before the session, so no Alexa service had to be
stopped and no `ErrDeviceBusy` workaround was needed.

The red mute ring initially remained lit and captures stayed below the room
noise floor. Changing ALSA's `MFP Gpio Mute` control did not clear it. EchoLocal
identifies the actual physical microphone cut as MediaTek pin 87, resolved
through the GPIO-controller base rather than hardcoded; this device reports
base 357, so the line is GPIO 444. The line was not exported. After exporting
it, its value read `1` (microphones physically cut). Driving it as an output at
`0` connected the microphones and a nearby human confirmed that the red ring
went out:

```sh
"$ADB" -s "$DEVICE_SERIAL" shell "su -c '
  echo 444 > /sys/class/gpio/export
  echo out > /sys/class/gpio/gpio444/direction
  echo 0 > /sys/class/gpio/gpio444/value
  cat /sys/class/gpio/gpio444/value
'"
```

Production code must resolve the controller base and add MTK pin 87, as
EchoLocal does; `444` is only the measured line on this unit. Task 23 owns the
full mute-button behavior and persistence decision.

```sh
"$ADB" -s "$DEVICE_SERIAL" shell \
  "su -c '/data/local/tmp/echoctl mic record --channels all --seconds 5 \
  --out /data/local/tmp/mic9.wav --print-levels'"
```

Quiet-room results:

| Channel | Peak dBFS | RMS dBFS | Identification |
|---:|---:|---:|---|
| 0 | -54.89 | -80.26 | physical microphone |
| 1 | -53.92 | -79.58 | physical microphone |
| 2 | -54.05 | -79.38 | physical microphone |
| 3 | -52.58 | -78.88 | physical microphone |
| 4 | -52.58 | -78.27 | physical microphone |
| 5 | -52.36 | -78.44 | physical microphone |
| 6 | -50.57 | -78.98 | physical microphone |
| 7 | `-Inf` | `-Inf` | playback loopback/reference |
| 8 | `-Inf` | `-Inf` | playback loopback/reference |

During a concurrent three-second 1 kHz speaker test, channels 7 and 8 both
rose to peak -12.07 dBFS and RMS -18.17 dBFS. Channels 0 through 6 remained at
room-noise levels. This identifies the layout as seven physical microphones at
indices 0–6 followed by two playback-loopback/reference channels at indices
7–8. Wake capture uses channel 0 initially, matching the simplest known-good
path; Task 23 owns wake tuning against real speech and noise.

After clearing the physical mute GPIO, a spoken all-channel capture produced
the following peaks: channel 0 -34.36, channel 1 -32.70, channel 2 -30.91,
channel 3 -31.35, channel 4 -30.49, channel 5 -28.69 and channel 6 -32.65 dBFS.
Channels 7 and 8 remained digital zero. This confirms that all seven physical
channels track room speech independently of the two loopback references. A
second five-second mic0 capture peaked at -37.00 dBFS; playback through the Dot
completed 240,000 frames at 48 kHz stereo, and a nearby human confirmed the
recorded speech was clear and intelligible.

The live capture `hw_params` were:

```text
access: RW_INTERLEAVED
format: S24_3LE
subformat: STD
channels: 9
rate: 16000 (16000/1)
period_size: 320
buffer_size: 2560
```

These values confirm the constants in `internal/device/audio/alsa/config.go`:
16 kHz, nine interleaved S24_3LE channels, 320-frame periods, eight periods.

`testdata/audio/dot_mic_9ch_s24le_room.raw` contains the first two seconds of
the real quiet-room capture. `echoctl` writes canonical S16 WAV, so the fixture
was converted back to packed S24_3LE by shifting each retained S16 sample left
eight bits. It preserves the captured channel ordering and retained sample
precision; it does not claim to recover the low eight bits discarded by the
diagnostic conversion.

### Speaker

Initial hardware execution found that issuing `SNDRV_PCM_IOCTL_START` on an
empty prepared playback stream returns `EPIPE`. Capture requires explicit
start; playback starts on its first write. The ALSA provider now applies that
direction-specific sequence. No device constant changed.

```sh
"$ADB" -s "$DEVICE_SERIAL" shell \
  "su -c '/data/local/tmp/echoctl speaker test --seconds 3'"
"$ADB" -s "$DEVICE_SERIAL" shell \
  "su -c '/data/local/tmp/echoctl speaker test \
  --in /data/local/tmp/mic0.wav --seconds 5'"
```

The generated test wrote 144,000 frames and the recorded-input test wrote
240,000 frames, both at 48 kHz stereo. The live playback `hw_params` were:

```text
access: RW_INTERLEAVED
format: S16_LE
subformat: STD
channels: 2
rate: 48000 (48000/1)
period_size: 1024
buffer_size: 4096
```

These values confirm 48 kHz, stereo S16_LE, 1024-frame periods and four
periods. Resampling remains on the device immediately before the PCM sink: the
internal and captured representation stays 16 kHz mono, while only speaker
output is converted to the codec's 48 kHz stereo format.

The commands and frame counts prove successful ALSA playback and amplifier
control. A nearby human confirmed on 2026-08-20 that the generated three-second
1 kHz tone was clearly audible and that spoken mic0 capture played back clearly
and intelligibly.

## 2026-08-20 LED ring and button session

The session used the same rooted device and `/data/local/tmp/echoctl` staging
workflow as the microphone and speaker session.

### LED ring

The controller is `/sys/bus/i2c/devices/0-003f`. Its `frame`, `led_current`,
and `boot_animation` attributes all exist. This FireOS driver requires writes
to be newline terminated; otherwise even valid values fail with `EINVAL`.

Amazon's init-managed `/system/bin/ledcontroller` process continuously rewrites
the frame even when `boot_animation` reads `0`. Direct control therefore
requires both `stop ledcontroller` and writing `0` to `boot_animation`. Stopping
the service is reversible with `start ledcontroller` or a reboot. With it
stopped, `echoctl led test --clear` writes an all-zero frame that remains dark
after the process exits.

`led_current` is an attenuation index, not a PWM value: `0` is full current and
`3` is quarter current. A human confirmed that the same solid-red frame was
visibly dimmer at index 3 than index 0. Values outside 0 through 3 are invalid.

All 12 segments responded. Walking one blue segment established the physical
mapping: segment 0 is at the bottom-right of the top face, between the
microphone and Volume Down buttons, and increasing segment numbers move
clockwise. Channels are consecutive RGB triplets. A human confirmed the solid
red muted state across all segments and the nine semantic patterns. Hardware
feedback found the original 100 ms hard-edged chase visibly stepped; matching
EchoLocal's 40 ms frame interval and two-frames-per-segment comet with a fading
tail produced acceptably smooth motion.

### Buttons

The button-capable nodes and measured key assignments are:

| Node | `device/name` | Consumed keys |
|---|---|---|
| `/dev/input/event1` | `mtk-kpd` | Mute 113, Action/`KEY_HELP` 138 |
| `/dev/input/event2` | `keys` | Volume Down 114, Volume Up 115 |

`mtk-kpd` also advertises Volume Down, but the complete Volume Down
press/release stream is consumed from `keys`; per-node filtering prevents a
duplicate or orphaned press. A raw `keys` capture measured one Volume Down tap
as DOWN followed by UP 180 ms later.

The keypad emits no kernel autorepeat. Following EchoLocal, the watcher emits a
volume tap immediately and generates repeats every 200 ms until release. The
live acceptance run produced Action tap and hold-start/hold-end, Mute tap and
hold-start/hold-end, a Volume Up tap followed by 200 ms repeats during a hold,
and exactly one Volume Down tap in the isolated prompt-release check. No key was
mapped to the wrong name and no spurious event remained after release.
