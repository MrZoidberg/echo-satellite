# On-device health telemetry test

This is the operator guide for the prepared Task 11 command-audio run. It
qualifies the telemetry path on the rooted Gen 2 Dot while keeping the
installed `/data/local/bin/echod` untouched. The run is approximately 23
minutes and needs one person present for the spoken/button trials.

## Recommended Windows setup

Run the gateway and device-lab commands inside WSL2 Ubuntu. The Dot must be
able to route to the WSL2 address. In WSL:

```sh
cd /mnt/c/projects/echo-satellite
sudo apt-get update && sudo apt-get install -y adb openssl
make build build-device
```

Use the WSL2 address, not `localhost`, for the Dot:

```sh
WSL_IP=$(hostname -I | awk '{print $1}')
printf '%s\n' "$WSL_IP"
```

The Windows host firewall must allow inbound TCP `8770` on the active private
network. If the Dot cannot connect to `$WSL_IP:8770`, use Windows port
forwarding or WSL mirrored networking; do not switch the device to an
unbounded or unauthenticated endpoint.

## Start a temporary gateway

Create a development certificate, token, and profile in WSL. The token must be
the same value as the already-provisioned device token; do not print it or put
it in Git.

```sh
mkdir -p .gateway-secrets/telemetry-wav .gateway-secrets/telemetry-evidence
openssl req -x509 -newkey rsa:2048 -nodes -days 1 \
  -keyout .gateway-secrets/telemetry-key.pem \
  -out .gateway-secrets/telemetry-cert.pem \
  -subj '/CN=echo-telemetry-gateway'
cp .gateway-secrets/device-token .gateway-secrets/telemetry-token
cp deploy/devices.example.toml .gateway-secrets/telemetry-devices.toml
chmod 600 .gateway-secrets/telemetry-key.pem .gateway-secrets/telemetry-token
```

Start the host gateway in a second WSL terminal:

```sh
cd /mnt/c/projects/echo-satellite
.bin/gateway \
  --listen=:8770 --server-id=echo-telemetry-gateway \
  --tls-cert=.gateway-secrets/telemetry-cert.pem \
  --tls-key=.gateway-secrets/telemetry-key.pem \
  --device-token-file=.gateway-secrets/telemetry-token \
  --device-config=.gateway-secrets/telemetry-devices.toml \
  --diagnostic-wav-dir=.gateway-secrets/telemetry-wav \
  --diagnostic-evidence-dir=.gateway-secrets/telemetry-evidence \
  --no-mdns --log-format=json \
  --log-file=.gateway-secrets/telemetry-gateway.jsonl
```

The temporary certificate is not trusted by the Dot. Verify the provisioned
`echod.ini` uses the explicit WSS URL `wss://<WSL_IP>:8770/device` and the
visible development setting `tls-skip-verify=true`. Verify the token path
without printing its contents. The payload uses this provisioned INI and an
isolated state directory.

## Run the Dot test

Connect the qualified rooted Dot by USB, ensure the physical microphone mute
button is not red, and identify its serial. The qualified serial is shown
below; always pass it explicitly.

```sh
adb devices
uv run --no-project --script tools/device-lab/device_lab.py preflight \
  --adb /usr/bin/adb --serial G090LF0964060EHP
```

Only continue when preflight reports ADB, root access, the expected `biscuit`
product/`arm64-v8a` ABI, permissive SELinux, and a captured installed-agent
digest. Then run:

```sh
uv run --no-project --script tools/device-lab/device_lab.py run-payload \
  --adb /usr/bin/adb --serial G090LF0964060EHP \
  --gateway-url wss://<WINDOWS-LAN-IP>:8770/device \
  --payload task11_command_audio.sh \
  --diagnostic .bin/linux_arm64/echod \
  --stop-known-launcher
```

The payload first waits 35 seconds for the first periodic health sample, then
runs five wake trials and pauses for a 60-second telemetry checkpoint. During
that checkpoint, verify the gateway hello includes `health.telemetry.v1` and
that at least one health observation is visible before allowing the schedule
to continue. It then runs one minute of idle/music, three continuous-speech
timeout trials, three endpointed trials, and three Action-button no-speech
trials. The ring is cyan-green when speech is requested and yellow when the
Action button should be pressed. Watch the gateway log for `device connected`,
`device health`, `device turn started`, and `device turn ended`. Evidence is
written to:

```sh
wc -l .gateway-secrets/telemetry-evidence/turns.jsonl
rg 'device_id|turn_id|reason|pcm_bytes|telemetry' \
  .gateway-secrets/telemetry-evidence/turns.jsonl
```

The gateway evidence is authoritative for received PCM bytes and gateway
timestamps. The telemetry object is authoritative only for device-observed
values. Do not count schedule lines as accepted wakes.

## Required cleanup

When the runner returns, it should have executed cleanup, `verify-clean`, and
launcher restoration. Stop the temporary gateway and remove only its temporary
diagnostic files:

```sh
rm -f .gateway-secrets/telemetry-wav/* \
  .gateway-secrets/telemetry-evidence/turns.jsonl \
  .gateway-secrets/telemetry-gateway.jsonl
test -z "$(find .gateway-secrets/telemetry-wav -mindepth 1 -maxdepth 1 -print -quit)"
test -z "$(find .gateway-secrets/telemetry-evidence -mindepth 1 -maxdepth 1 -print -quit)"
```

Do not claim hardware acceptance until the records prove the Task 11 thresholds:
at least 18/20 wake accepts, zero idle/music false wakes, the specified
timeout/endpoint/no-speech windows, zero XRuns/dropped frames, and clipping at
or below 0.1%. Record the session ID and keep no WAVs after the run.
