# Windows and WSL development workflow

The reference environment is Windows 11 with VS Code Remote WSL. Build, test,
lint, and live-device commands run in WSL2. The Echo Dot is attached to WSL
with USB/IP and is controlled with Linux `adb`. Run a native Windows gateway
when testing mDNS on the physical LAN: WSL and Docker multicast are not an
mDNS acceptance path for the Dot.

## Topology

```text
Windows 11
  +-- native gateway.exe -> physical LAN mDNS + WSS
  +-- usbipd -> WSL2 Ubuntu -> Linux adb -> Echo Dot over USB
  +-- VS Code Remote WSL -> Go, make, uv, source checkout
```

Install Go, `golangci-lint`, `uv`, and Android Platform Tools (`adb`) in WSL.
The repository enforces LF line endings through `.gitattributes`; keep that
setting even for a Windows-hosted checkout.

## Attach the Dot to WSL

Disconnect any Windows ADB client that currently owns the Dot. In an elevated
Windows PowerShell, locate the Dot's bus ID, bind it when required, then attach
it to the WSL distribution:

```powershell
usbipd list
usbipd bind --busid <BUSID>
usbipd attach --wsl --busid <BUSID>
```

`usbipd attach` is not persistent across a physical disconnect, reboot, or
WSL shutdown; repeat it after those events. Do not run Windows `adb.exe` while
the device is attached to WSL.

In WSL, confirm that Linux ADB owns the attached device and select its serial
explicitly:

```sh
export ADB=adb
"$ADB" version
"$ADB" devices -l
export DEVICE_SERIAL=<serial-from-adb-devices>
make device-check
```

The Dot must report `device`, product `biscuit`, ABI `arm64-v8a`, Magisk `su`
at UID 0, and permissive SELinux. Its normal ADB shell is UID 2000, so
privileged device commands use `su -c`; never assume `adb root` is available.

If ADB reports `offline`, detach/attach the USB/IP device and re-run `adb
devices -l`. For `unauthorized`, resolve the debugging authorization rather
than deleting ADB keys. Set `DEVICE_SERIAL` whenever more than one target is
listed.

## Build and foreground iteration

Run this in WSL. It stages a disposable binary at `/data/local/tmp/echod`; it
does not install a launcher or change `/data/local/bin/echod`.

```sh
make test
make build-device
make push-device
make run-device DEVICE_ARGS='--dbg --device-id bench-dot'
```

`run-device` owns a foreground terminal. Ctrl+C stops it. For a networked
experiment, supply an explicit WSS URL and the existing device token path;
`--tls-skip-verify` is development-only and must not appear in production
configuration.

```sh
make run-device DEVICE_ARGS='--dbg --device-id bench-dot \
  --gateway-url wss://<WINDOWS-LAN-IP>:8770/device \
  --gateway-token-file /data/local/tmp/echo-satellite-token \
  --tls-skip-verify'
```

Use isolated `--pairing-state` and `--config-state` paths for disposable
experiments. See [device installation](device-installation.md) for the
installed-agent and recovery workflow, and [device-lab](device-lab.md) for any
live microphone, LED, button, reboot, or qualification work.

## Run a gateway

Build the gateway in WSL, then launch the Windows binary from PowerShell on the
physical LAN. Use the Windows host's LAN address in any explicit device URL;
do not use `localhost` or a WSL address for the Dot.

```sh
make build-windows
```

```powershell
.\.bin\gateway.exe --listen :8770 --server-id home-gateway `
  --tls-cert .gateway-secrets\dev-cert.pem `
  --tls-key .gateway-secrets\dev-key.pem `
  --device-token-file .gateway-secrets\device-token `
  --device-config .gateway-secrets\devices.toml --dbg
```

This native process advertises `_echo-satellite._tcp.local.` on the physical
LAN. Ensure the Windows firewall permits the selected TCP port and multicast
DNS on the active private network. Increment the profile's top-level `version`
when its effective desired configuration changes; `SIGHUP` reloads a valid,
increasing profile without replacing the active snapshot.

Docker Compose deliberately runs with `--no-mdns` and publishes localhost-only
WSS for host-side simulator smoke tests. Use [gateway deployment]
(gateway-deployment.md) for that path. `dotsim` persists its state under
`.dotsim` by default, and `--once` exits after one transmitted fixture turn.

## VS Code and troubleshooting

Open the repository through **Remote - WSL**. The committed `device: check`,
`device: push`, and `device: run` tasks call the same Make targets with Linux
ADB and prompt for the explicit device serial; the run task owns a foreground
terminal.

- **`adb` missing in WSL:** install Android Platform Tools in WSL and use
  `ADB=adb`; do not fall back to `adb.exe` through `/mnt/c`.
- **USB device absent:** run `usbipd list` in elevated PowerShell and attach
  the correct bus ID again.
- **root check fails:** run `adb -s "$DEVICE_SERIAL" shell 'su -c id'`.
  This workflow requires an already rooted, Magisk-enabled Dot.
- **old shell utilities missing:** FireOS 5.1 lacks many GNU tools. Keep
  device commands to verified Android/Magisk BusyBox tools and process output
  on the WSL host.
- **mDNS does not resolve:** use the native Windows gateway for LAN testing or
  an explicit URL. Docker, WSL, VLANs, VPNs, and multicast filtering can all
  block discovery.

Source-level debugging on the Dot remains experimental: it requires an
unstripped ARM64 build, a compatible Delve server, port forwarding, and a
successful ptrace experiment on this FireOS kernel.
