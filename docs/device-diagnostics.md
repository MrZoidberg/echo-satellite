# Echo Dot hardware reference

This is the current operational reference for the qualified rooted Echo Dot
Gen 2. It records stable facts and active qualification limits; detailed dated
experiments remain in Git history and the associated implementation plans.

## Qualified device facts

- Device identity: FireOS product `biscuit`, ABI `arm64-v8a`, rooted through
  Magisk, permissive SELinux. The ADB shell is not root; privileged commands
  require `su -c`.
- The qualified Magisk v17.3 launcher directory is
  `/sbin/.core/img/.core/service.d`. Do not create `/data/adb/service.d` and
  assume this device will consume it.
- GPIO 444 (MTK pin 87) is the physical microphone cut. A readback of `0`
  enables microphones; a high value makes wake results unreliable.
- The seven physical microphones are channels 0--6. Channels 7--8 are digital
  playback-loopback references and are never wake, endpointing, or conditioning
  inputs.
- Capture is 16 kHz, nine-channel interleaved S24_3LE with 320-frame periods;
  playback is 48 kHz stereo S16_LE with 1,024-frame periods. The runtime
  converts capture into its canonical processing format before fanout.

## Live-device safety

Use [device-lab](device-lab.md) for every live microphone, LED, button, reboot,
or qualification session. It captures state, coordinates an occupied capture
device, prepares LEDs and GPIO 444, stages diagnostics only in a token-owned
temporary root, restores the known launcher, and verifies the installed-agent
digest. Do not kill an unknown microphone holder or edit an installed agent to
free capture.

The diagnostic runner never replaces `/data/local/bin/echod`. Use
[device installation](device-installation.md) for bootstrap and ADB recovery.

## Audio and qualification status

Wake detection and wake VAD are always local. Command endpointing is a separate
device-local detector that runs only after a wake or Action-button trigger.
The gateway receives active-turn audio only; it never receives continuous
microphone audio for wake scoring.

`dot-gen2-qualified-v1` currently applies bounded channel-0 conditioning once
before `audio.Fanout`, so local wake, pre-roll, endpointing, and transmitted
PCM use the same conditioned frames. This is a provisional operational default,
not a completed acoustic selection: Task 10's remaining position/condition
matrix and full command-audio requalification are outstanding. Do not claim
that unsteered mixing or delay-and-sum was rejected solely from the existing
front/550 mm data.

For the current procedure and acceptance thresholds, see
[command-audio qualification](command-audio-qualification.md). For model
installation and model-specific qualification, see
[wake-model training](wake-model-training.md).

## Operational recovery

`echod` prepares project LED/microphone state while it owns capture. If a
manual recovery is required after stopping it, restore `ledcontroller` and
`mdnsd` only when they were running before the session, or reboot. A busy
capture device is evidence of another process or session; investigate and
coordinate rather than stopping unrelated services.
