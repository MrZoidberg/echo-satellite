# Echo Satellite

Turn a rooted Amazon Echo Dot into a voice satellite for a self-hosted assistant
gateway: the Dot listens for its wake word locally and streams a voice turn to a
gateway that owns speech recognition, the assistant backend and the fleet.

**Status: Milestone 3 single-agent deployment and command-audio qualification
are in progress.** The Dot has pure-Go microphone capture, local VAD-gated
openWakeWord detection, speaker playback, LED and button diagnostics, local
wake-model installation, device-local command endpointing, authenticated WSS
device turns, and gateway-managed single-agent staging. See `docs/DESIGN.md`
for the authoritative milestone sequence and current qualification limits.

## The two boundaries

**Voice.** Wake detection, including the VAD that decides whether a wake score is
credible speech, is always device-local. The gateway never scores wake words and
never receives a continuous microphone stream; it sees audio only after the
device has opened a turn. Command endpointing — deciding when the user stopped
talking — is also device-local, after a wake or Action-button trigger, with
configuration independent of wake VAD.

**Updates.** Agent updates replace the single application binary under `/data`,
never FireOS OTA. The gateway owns desired state; the device verifies, stages,
and atomically installs it. A connected device can receive an older signed
release; a committed agent that cannot reconnect requires ADB recovery.

Both boundaries are stated in full in [AGENTS.md](AGENTS.md) and
[docs/DESIGN.md](docs/DESIGN.md) §28.

## Quick start

```sh
make test          # race detector + coverage
make lint          # golangci-lint
make build         # host binaries into .bin/
make build-device  # static linux/arm64 echod for the Dot
```

## Local wake models

Wake detection is entirely local to the Dot. Cross-build `echoctl`, copy it and
a vetted model to the Dot, then install using a local path and pinned digest:

```sh
make build-device-ctl
adb -s <serial> push .bin/linux_arm64/echoctl /data/local/tmp/echoctl
adb -s <serial> shell "su -c '/data/local/tmp/echoctl wake install okay_nabu \\
  --from /data/local/tmp/okay_nabu.tflite \\
  --metadata /data/local/tmp/okay_nabu.json --sha256 <sha256>'"
adb -s <serial> shell "su -c '/data/local/tmp/echoctl wake list'"
```

Switch the active model with the `--wake-model` configuration option after it
has been independently qualified. See [docs/wake-model-training.md](docs/wake-model-training.md)
for the complete install, switch and qualification flow, and
[docs/device-diagnostics.md](docs/device-diagnostics.md) for measured Dot
hardware results and limitations.

Verify a release bundle with the same checks a gateway and a device apply:

```sh
.bin/echoctl release verify \
  --manifest testdata/updates/valid/manifest.json \
  --sig      testdata/updates/valid/manifest.sig \
  --pubkey   testdata/updates/valid/manifest.pub \
  --artifact testdata/updates/valid/echod
```

## Layout

| Path | Contents |
|---|---|
| `cmd/echod` | device agent that runs on the Dot |
| `cmd/gateway` | central API and orchestration service |
| `cmd/echoctl` | provisioning and diagnostics CLI |
| `cmd/dotsim` | simulated Dot: same protocol, files instead of hardware |
| `internal/protocol` | device ↔ gateway wire contract |
| `internal/discovery` | mDNS service record and gateway resolution order |
| `internal/release` | release manifest, digest and Ed25519 trust |
| `testdata/updates` | release fixtures, including tampered and unsigned bundles |

## Documentation

- [docs/DESIGN.md](docs/DESIGN.md) — architecture, boundaries, milestones. Authoritative.
- [docs/README.md](docs/README.md) — documentation index and task-based routing.
- [docs/protocol.md](docs/protocol.md) — the wire contract, tracking `internal/protocol`.
- [docs/development-windows-wsl.md](docs/development-windows-wsl.md) — WSL USB/IP/Linux ADB development and Windows gateway mDNS.
- [docs/device-lab.md](docs/device-lab.md) — safe live-Dot diagnostic sessions.
- [docs/device-diagnostics.md](docs/device-diagnostics.md) — qualified hardware facts and active limits.
- [docs/device-installation.md](docs/device-installation.md) — bootstrap and ADB recovery.
- [docs/gateway-deployment.md](docs/gateway-deployment.md) — Docker Compose gateway deployment and the explicit-WSS smoke test.
- [docs/command-audio-qualification.md](docs/command-audio-qualification.md) — command-audio qualification.
- [docs/plans/README.md](docs/plans/README.md) — how implementation work is planned and tracked.
- [AGENTS.md](AGENTS.md) — instructions for AI coding agents, including build commands and code conventions.

## License

See [LICENSE](LICENSE).
