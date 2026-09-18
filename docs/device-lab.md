# Echo Dot device-lab

`tools/device-lab/device_lab.py` is the required safe runner for live Echo Dot
diagnostics and qualification. Use it for ADB, Magisk, reboot, live audio, LED,
button, microphone, and command-audio work. Ordinary Go development and
`dotsim` do not need it.

The runner requires explicit Linux ADB and serial inputs, creates a host session
under `.bin/device-lab/`, and owns a token-specific temporary root on the Dot.
It never writes the installed agent at `/data/local/bin/echod`.

## Before a session

Follow [Windows and WSL development](development-windows-wsl.md) to attach the
Dot with USB/IP and set `ADB=adb` and `DEVICE_SERIAL`. Read the active plan and
the applicable design section. Only one operator or agent may control a
physical Dot; reviewers are read-only.

```sh
uv run --no-project --script tools/device-lab/device_lab.py preflight \
  --adb "$ADB" --serial "$DEVICE_SERIAL"
```

Preflight records sanitized device identity, root access, service/GPIO state,
installed-agent digest, and capture holders. A busy holder, unknown lock, or
unexpected target is a coordination failure; do not kill a process or remove a
lock to continue.

## Session lifecycle

For preparation-only work, run the lifecycle explicitly:

```sh
uv run --no-project --script tools/device-lab/device_lab.py prepare \
  --adb "$ADB" --serial "$DEVICE_SERIAL"
# perform only the approved diagnostic work
uv run --no-project --script tools/device-lab/device_lab.py cleanup \
  --adb "$ADB" --serial "$DEVICE_SERIAL" --resume .bin/device-lab/<session>
uv run --no-project --script tools/device-lab/device_lab.py verify-clean \
  --adb "$ADB" --serial "$DEVICE_SERIAL" --resume .bin/device-lab/<session>
```

`prepare` stops `ledcontroller`, disables boot animation, and verifies GPIO 444
is low before microphone interpretation. It uses a remote script passed as one
quoted `su -c` argument, so privileged redirections stay privileged. Always run
`cleanup` and `verify-clean`, including after a failure or disconnect; resume
the existing session rather than creating a competing one.

## Version-controlled payloads

Use `run-payload` only with a repository payload and a built diagnostic. It
performs the safe lifecycle and attempts cleanup on both success and failure.

```sh
make build-device
uv run --no-project --script tools/device-lab/device_lab.py run-payload \
  --adb "$ADB" --serial "$DEVICE_SERIAL" \
  --payload command_audio_qualification.sh \
  --diagnostic .bin/linux_arm64/echod \
  --gateway-url wss://<WINDOWS-LAN-IP>:8770/device \
  --stop-known-launcher
```

`task10_front_550mm.sh` instead uses `.bin/linux_arm64/echoctl` as its
diagnostic. See the owning qualification procedure before invoking either
payload.

## Evidence and boundaries

The runner records sanitized evidence and rejects secrets, bearer tokens,
private keys, URL query strings, raw logs, and arbitrary result files. Keep raw
audio only through an explicitly approved gateway diagnostic directory and
remove it immediately after review. The device session's result manifest names
the artifacts it may retain.

Device-lab is diagnostic infrastructure, not deployment infrastructure. Signed
agent installation, rollback by redeploying an older compatible release, and
ADB recovery are described in [device installation](device-installation.md).
