# Windows / WSL development workflow

The reference development environment for this project is Windows 11 with WSL2,
described in `docs/DESIGN.md` §22. The repository also builds on macOS and Linux;
only the ADB bridge below is Windows-specific.

## Environment

```text
Windows 11
  |
  +-- VS Code + Remote WSL
  |
  +-- WSL2 Ubuntu
  |     +-- Go 1.26
  |     +-- Python + uv          (speech worker, later milestones)
  |     +-- make
  |     +-- Docker CLI
  |     +-- golangci-lint 2.12.x
  |     +-- source checkout
  |
  +-- Docker Desktop / WSL backend
  |     +-- gateway
  |     +-- speech-worker
  |     +-- Hermes / test dependencies
  |
  +-- Windows Android Platform Tools
        +-- adb.exe -> Echo Dot
```

Install the Go toolchain and golangci-lint inside WSL, not on the Windows side:
the build, tests and lint all run from the WSL checkout.

## ADB from WSL

Use the **Windows** `adb.exe` from inside WSL. The WSL kernel does not see the
USB device, and running two ADB servers fights over the connection.

```bash
export ADB=/mnt/c/Android/platform-tools/adb.exe
"$ADB" devices
```

ADB is for one-time rooted bootstrap and low-level recovery. It is not part of
the normal iteration loop once A/B OTA works.

## Building for the device

The device binary stays pure Go so it needs no cross-compiler and no shared
libraries on FireOS:

```bash
make build-device      # GOOS=linux GOARCH=arm64 CGO_ENABLED=0 -> .bin/linux_arm64/echod
file .bin/linux_arm64/echod   # ELF 64-bit, ARM aarch64, statically linked
```

Keep it that way. A dependency that requires cgo turns a one-command build into
a toolchain problem on every developer machine.

## Early iteration loop (before A/B OTA)

```text
make build-device
  -> "$ADB" push .bin/linux_arm64/echod /data/echod/
  -> restart the agent in the foreground
  -> tail logs
  -> run mic / wake / VAD fixture tests
```

## Preferred iteration loop (once Milestone 3 lands)

```text
build a signed or dev release bundle
  -> upload to the local gateway
  -> deploy to a test Dot's inactive slot
  -> observe restart and trial
  -> automatic rollback if the build is bad
```

This becomes the preferred loop because it exercises the same mechanism real
deployments use, including the rollback path.

## mDNS from Docker and WSL

Multicast visibility differs by Docker network mode, and WSL2 sits behind a
virtual switch. **Test mDNS advertisement from the actual deployment you intend
to run**, not from a bare process on the Windows host, and verify the record is
visible on the physical LAN where the Dot lives.

An explicit gateway URL is always the fallback and always wins over discovery:

```yaml
gateway:
  discovery: disabled
  url: "wss://192.168.10.20:8770/device"
  preferred_server_id: "home-gateway"
```

The same settings are available as `echod` flags: `--discovery`, `--gateway-url`,
`--preferred-server-id`.
