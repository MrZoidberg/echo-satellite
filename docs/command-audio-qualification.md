# Command-audio qualification on the Dot

This reusable procedure exercises local wake, device-local endpointing, terminal
telemetry, and optional gateway-side WAV evidence. The diagnostic agent is staged
only beneath a token-owned device-lab root; it never replaces
`/data/local/bin/echod`.

## Prerequisites

Follow [device-lab](device-lab.md) for the required session safety model and
[Windows and WSL development](development-windows-wsl.md) for USB/IP, Linux ADB,
and the Windows-hosted gateway. Build the agent in WSL, reuse the provisioned
device token without printing it, and use a Windows LAN address that the Dot can
reach. The selected development certificate requires the existing visible
`tls-skip-verify` setting on the diagnostic configuration; production
qualification uses normal certificate validation.

Start the native gateway in Windows PowerShell. Keep telemetry in a restricted
operator-owned directory and do not enable raw WAV retention unless it is needed
for an approved check.

Build the Windows gateway from WSL first:

```sh
make build-windows
```

```powershell
New-Item -ItemType Directory -Force .gateway-secrets\command-audio-evidence
.\.bin\gateway.exe --listen :8770 --server-id command-audio-gateway `
  --tls-cert .gateway-secrets\telemetry-cert.pem `
  --tls-key .gateway-secrets\telemetry-key.pem `
  --device-token-file .gateway-secrets\device-token `
  --device-config .gateway-secrets\devices.toml `
  --diagnostic-evidence-dir .gateway-secrets\command-audio-evidence
```

Run the payload from WSL with explicit Linux ADB and serial:

```sh
make build-device
uv run --no-project --script tools/device-lab/device_lab.py run-payload \
  --adb "$ADB" --serial "$DEVICE_SERIAL" \
  --gateway-url wss://<WINDOWS-LAN-IP>:8770/device \
  --payload command_audio_qualification.sh \
  --diagnostic .bin/linux_arm64/echod --stop-known-launcher
```

The payload performs the device-lab preparation/cleanup lifecycle, uses isolated
pairing and configuration state, disables update offers, emits visible operator
cues, and restores the known launcher. Gateway turn records, not payload
schedule lines, are the authority for trigger, stop reason, duration, and PCM
evidence.

## Schedule and acceptance

The standard schedule is 20 cyan-green wake windows, 15 minutes of idle/music,
three continuous-quiet-speech windows, three command-then-silence windows, and
three yellow Action-button silence windows.

A fully qualified run requires all of the following:

- At least 18 accepted wakes and zero idle/music false wakes.
- Continuous quiet speech stops only as `timeout` at 59.9--60.2 seconds.
- Command-plus-silence stops as `endpointed` 1.3--2.2 seconds after speech
  ends, and Action-button silence stops as `no_speech` 2.8--3.3 seconds after
  turn start.
- No XRun, dropped-frame, or clipping-bound failure. Record Task 10 processing
  metrics separately.

This procedure is reusable, but a successful schedule alone is not acoustic
qualification. The outstanding Task 10 matrix and complete gateway-correlated
results remain required before the selected conditioning profile can be called
fully qualified.

## Optional WAV evidence and cleanup

Raw turn audio is discarded by default. If one approved endpointed turn needs
WAV correlation, use the guarded diagnostic override and cleanup procedure in
[gateway deployment](gateway-deployment.md) immediately afterward. Keep only the
sanitized telemetry evidence needed for the result, stop the diagnostics
gateway, remove all approved raw files, verify the directory is empty, and
restart without the WAV override.
