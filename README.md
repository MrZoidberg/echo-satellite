# Echo Satellite

Turn a rooted Amazon Echo Dot into a voice satellite for a self-hosted assistant
gateway: the Dot listens for its wake word locally and streams a voice turn to a
gateway that owns speech recognition, the assistant backend and the fleet.

**Status: Milestone 0.** This repository currently delivers the foundation —
module layout, build and CI, and the shared contracts (protocol, discovery,
release trust). The four binaries build and are wired, but no microphone, wake
model, WebSocket endpoint or mDNS advertisement is implemented yet; each binary
says so when it starts. See `docs/DESIGN.md` §24 for the milestone sequence.

## The two boundaries

**Voice.** Wake detection, including the VAD that decides whether a wake score is
credible speech, is always device-local. The gateway never scores wake words and
never receives a continuous microphone stream; it sees audio only after the
device has opened a turn. Command endpointing — deciding when the user stopped
talking — is a separate gateway-side concern with its own configuration.

**Updates.** Agent updates are application-level A/B slots under `/data`, never
FireOS OTA. The gateway owns the desired software state; the device owns its own
recovery. A small supervisor outside both slots decides whether a new build
survived its trial and can roll back with the gateway unreachable, so a bad
release never requires ADB to recover.

Both boundaries are stated in full in [AGENTS.md](AGENTS.md) and
[docs/DESIGN.md](docs/DESIGN.md) §28.

## Quick start

```sh
make test          # race detector + coverage
make lint          # golangci-lint
make build         # host binaries into .bin/
make build-device  # static linux/arm64 echod for the Dot
```

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
- [docs/protocol.md](docs/protocol.md) — the wire contract, tracking `internal/protocol`.
- [docs/development-windows-wsl.md](docs/development-windows-wsl.md) — the reference dev environment and device build.
- [docs/plans/README.md](docs/plans/README.md) — how implementation work is planned and tracked.
- [AGENTS.md](AGENTS.md) — instructions for AI coding agents, including build commands and code conventions.

## License

See [LICENSE](LICENSE).
