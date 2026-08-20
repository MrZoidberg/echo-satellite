# Echo Dot device diagnostics

This document records reproducible hardware measurements for the rooted Echo
Dot Gen 2 used during Milestone 1. Commands that access audio devices run
through Magisk because the normal ADB shell is UID 2000 and the PCM nodes are
owned by `system:audio`.

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
