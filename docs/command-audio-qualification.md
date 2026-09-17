# Command-audio qualification on the Dot

This reusable operator procedure tests local wake through command audio,
device-local endpointing, terminal telemetry, and an optional gateway-side WAV
on the rooted Gen 2 Dot. It never replaces `/data/local/bin/echod`: the
diagnostic agent runs only under a token-owned device-lab root and the runner
restores the known launcher during cleanup.

Build the ARM64 diagnostic and start a native/WSL gateway that the Dot can
reach. Reuse the provisioned device-token file; do not print or copy its
contents. The gateway must use a LAN-reachable `:8770` listener, an explicit
development certificate trusted through the existing visible TLS-bypass
setting, the desired device-config revision/profile, and a restricted
`--diagnostic-evidence-dir` so `turns.jsonl` records terminal telemetry. The
Dot must have a physical microphone cut value of `0`, and device-lab preflight
must pass before the run. Optional WAV retention and its mandatory cleanup are
described in [gateway deployment](gateway-deployment.md).

```sh
mkdir -p .gateway-secrets/command-audio-evidence
.bin/gateway --listen=:8770 --server-id=command-audio-gateway \
  --tls-cert=.gateway-secrets/telemetry-cert.pem --tls-key=.gateway-secrets/telemetry-key.pem \
  --device-token-file=.gateway-secrets/device-token --device-config=.gateway-secrets/devices.toml \
  --diagnostic-evidence-dir=.gateway-secrets/command-audio-evidence --no-mdns
```

```sh
make build-device
uv run --no-project --script tools/device-lab/device_lab.py run-payload \
  --adb /usr/bin/adb --serial G090LF0964060EHP \
  --gateway-url wss://<WINDOWS-LAN-IP>:8770/device \
  --payload command_audio_qualification.sh \
  --diagnostic .bin/linux_arm64/echod --stop-known-launcher
```

The default schedule has 20 cyan-green wake windows, 15 minutes of idle/music,
three continuous-quiet-speech windows, three command-then-silence windows, and
three yellow Action-button silence windows. The payload retains only a
sanitized schedule and summary. Gateway turn records—not schedule lines—are
the authoritative wake, stop-reason, duration, and PCM evidence.

For qualification, require at least 18 accepted wakes, no idle/music false
wakes, `timeout` at 59.9–60.2 seconds for continuous quiet speech,
`endpointed` 1.3–2.2 seconds after speech ends, `no_speech` 2.8–3.3 seconds
after each Action-button turn, and no XRun, dropped-frame, or clipping-bound
failure. Record Task 10 processing metrics separately. If gateway WAV capture
was enabled, follow the gateway-deployment cleanup procedure immediately after
the run; raw audio is otherwise not retained.
