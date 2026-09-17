# Gateway Docker deployment

Docker Compose packages the Milestone 2 gateway for explicit, authenticated
WSS connections. It deliberately disables mDNS: Docker/WSL multicast reachability
to the physical LAN requires a separate real-network experiment, and must not
be inferred from this deployment. Its port is published only on localhost, so
host-side simulators can connect without exposing the development service to
the LAN.

## Prepare host files

Keep credentials out of Git. The following ignored directory sits beside the
Compose file's build context, avoiding Docker Desktop's unreliable `/tmp` bind
mounts in some WSL configurations. The service runs as a non-root user and
receives its certificate, private key, bearer token, and profile only as
read-only bind mounts. The token is a development shared bearer token and must
contain at least 32 random bytes.

```sh
mkdir -p .gateway-secrets
openssl req -x509 -newkey rsa:2048 -nodes -days 1 \
  -keyout .gateway-secrets/dev-key.pem \
  -out .gateway-secrets/dev-cert.pem \
  -subj '/CN=localhost'
openssl rand -base64 48 > .gateway-secrets/device-token
cp deploy/devices.example.toml .gateway-secrets/devices.toml
chmod 600 .gateway-secrets/dev-key.pem .gateway-secrets/device-token

export GATEWAY_TLS_CERT_PATH=$PWD/.gateway-secrets/dev-cert.pem
export GATEWAY_TLS_KEY_PATH=$PWD/.gateway-secrets/dev-key.pem
export GATEWAY_DEVICE_TOKEN_PATH=$PWD/.gateway-secrets/device-token
export GATEWAY_DEVICE_CONFIG_PATH=$PWD/.gateway-secrets/devices.toml
export GATEWAY_UID=$(id -u)
export GATEWAY_GID=$(id -g)
```

## Run and smoke test

```sh
docker compose -f deploy/docker-compose.yml config
docker compose -f deploy/docker-compose.yml up --build -d gateway

.bin/dotsim --device-id dotsim-docker --discover disabled \
  --gateway-url wss://localhost:8770/device \
  --gateway-token-file .gateway-secrets/device-token \
  --tls-skip-verify \
  --mic testdata/audio/command_endpointing_16k_mono.wav --once

docker compose -f deploy/docker-compose.yml down
```

`--tls-skip-verify` is appropriate only for this locally generated development
certificate. WSS encryption remains enabled. Compose sends SIGTERM and allows
ten seconds for the gateway's graceful cancellation and WebSocket shutdown.

## Optional diagnostic WAV output

Raw turn audio is discarded by default. To opt in to diagnostic WAVs, create an
existing writable directory owned by `GATEWAY_UID:GATEWAY_GID`, then use the
diagnostic override:

```sh
mkdir -p .gateway-secrets/command-audio-wav
: >.gateway-secrets/command-audio-wav/.echo-satellite-owner
export GATEWAY_DIAGNOSTIC_WAV_DIR=$PWD/.gateway-secrets/command-audio-wav
docker compose -f deploy/docker-compose.yml \
  -f deploy/docker-compose.diagnostics.yml up --build -d gateway
```

The override is intentionally separate so normal deployments cannot persist raw
audio accidentally. Stop it with the same pair of Compose files and `down`.

After an operator-approved diagnostic run, first stop the gateway gracefully.
Then remove only regular files from the explicit, operator-owned diagnostic
directory (including hidden `.turn-*.part` files), verify it is empty, and
restart without the diagnostics override:

```sh
set -eu
dir=${GATEWAY_DIAGNOSTIC_WAV_DIR:?set the diagnostic WAV directory}
case "$dir" in "$PWD"/.gateway-secrets/command-audio-wav) ;; *) exit 2;; esac
test -d "$dir" && test ! -L "$dir" && test -f "$dir/.echo-satellite-owner"
docker compose -f deploy/docker-compose.yml -f deploy/docker-compose.diagnostics.yml down &&
find "$dir" -mindepth 1 -maxdepth 1 -type f ! -name .echo-satellite-owner -delete &&
test -z "$(find "$dir" -mindepth 1 -maxdepth 1 ! -name .echo-satellite-owner -print -quit)" &&
unset GATEWAY_DIAGNOSTIC_WAV_DIR &&
docker compose -f deploy/docker-compose.yml up --build -d gateway
```

Do not use a broad temporary directory or delete subdirectories: a non-empty
directory after the check is a cleanup failure that needs operator inspection.

## Optional telemetry evidence

Telemetry metadata is not persisted unless an explicit existing directory is
configured. Set `GATEWAY_DIAGNOSTIC_EVIDENCE_DIR` to an owner-writable
directory; the gateway appends sanitized turn metadata to `turns.jsonl` and
never writes PCM, credentials, or arbitrary device fields there. This option is
independent of the diagnostic WAV override and should be removed after a
qualification run.
