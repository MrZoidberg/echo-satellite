# Windows / WSL development workflow

The reference development environment is Windows 11 with VS Code Remote WSL.
Builds, tests, and lint run in WSL2; the Windows Android Platform Tools own the
USB connection to the Echo Dot.

## Environment

```text
Windows 11
  |
  +-- VS Code + Remote WSL
  |
  +-- WSL2 Ubuntu
  |     +-- Go 1.26
  |     +-- make
  |     +-- source checkout
  |     +-- Windows adb.exe invoked through /mnt/c
  |
  +-- Docker Desktop / WSL backend
  |     +-- gateway and later-milestone services
  |
  +-- C:\tools\android-platform-tools\adb.exe -> Echo Dot over USB
```

Install Go and golangci-lint inside WSL. Do not run a second, native-Linux ADB
server in WSL: without an explicit USBIP setup it cannot see the USB device,
and mixing it with Windows ADB can restart the server unexpectedly.

The repository enforces LF line endings through `.gitattributes`. This is
required even on a Windows-hosted checkout: CRLF changes signed fixture sizes
and digests and makes WSL `gofmt` report every Go file.

## Configure and verify ADB

In a WSL terminal opened at the repository:

```bash
export ADB=/mnt/c/tools/android-platform-tools/adb.exe
"$ADB" version
"$ADB" devices -l
```

The device must appear once with state `device`, not `offline` or
`unauthorized`. When more than one device is attached, select one explicitly:

```bash
export DEVICE_SERIAL=G090LF0964060EHP
```

The Make targets translate `DEVICE_SERIAL` into ADB's `-s` argument. ADB also
honours its standard `ANDROID_SERIAL` environment variable, but use
`DEVICE_SERIAL` in repository commands so the selected target is visible.

Confirm the device is the expected rooted Echo Dot before copying anything:

```bash
make device-check
```

This requires ADB state `device`, product `biscuit`, ABI `arm64-v8a`, Magisk
`su` at UID 0, and permissive SELinux, printing every accepted value. This Dot
has a normal UID 2000 ADB shell;
privileged commands must therefore use `su -c`. Do not assume `adb root` or a
root ADB daemon.

From Windows PowerShell, use the same client directly when troubleshooting:

```powershell
$adb = 'C:\tools\android-platform-tools\adb.exe'
& $adb devices -l
& $adb -s G090LF0964060EHP shell id
& $adb -s G090LF0964060EHP shell su -c id
```

## Build, push, and run

The pre-Milestone 3 development loop deliberately stages an ephemeral binary
under `/data/local/tmp`. It does not modify `/system`, install a service, or
survive a reboot.

```bash
export ADB=/mnt/c/tools/android-platform-tools/adb.exe
export DEVICE_SERIAL=G090LF0964060EHP  # optional with exactly one device

make push-device
make run-device
```

`push-device` runs the device checks, builds the static Linux/ARM64 binary,
pushes it to `/data/local/tmp/echod`, makes it executable, and runs `--version`
as an execution check. `run-device` then starts it through Magisk in the
foreground with debug logging. Press Ctrl+C to stop it.

Windows ADB reports transport exit code 58 when Ctrl+C closes a foreground
shell. `run-device` accepts that code only if the device remains online and
`ps` confirms `/data/local/tmp/echod` is no longer running. All other non-zero
exits and a surviving process fail the target.

Pass different arguments without changing the Makefile:

```bash
make run-device DEVICE_ARGS='--dbg --device-id bench-dot'
```

Milestone 0's `echod` only logs its configuration and waits for a signal. The
microphone, wake, speaker, LED, and button diagnostics arrive in Milestone 1;
the same `DEVICE_ARGS` mechanism will run them when they exist.

## VS Code tasks

Open the repository through **Remote - WSL**, then use **Tasks: Run Task**:

- `device: check`
- `device: push`
- `device: run`

The committed tasks call the same Make targets and configure this machine's
Windows ADB path. They do not duplicate deployment logic. The run task owns a
foreground terminal; stop it with Ctrl+C.

If the platform-tools directory moves, either update the task environment or
put `adb.exe` on the WSL command path and set `ADB=adb` in the task environment.

## Troubleshooting

- **`adb` not found:** use the absolute Windows or `/mnt/c` path above. Native
  WSL ADB is not the USB client in this setup.
- **`unauthorized`:** unlock/observe the device if possible, reconnect USB, and
  accept its debugging authorization. Do not delete host ADB keys as a first
  response because that invalidates existing authorizations.
- **`offline`:** reconnect USB, then run `"$ADB" reconnect` and
  `"$ADB" devices -l`. Restart the Windows server only if it remains offline:
  `"$ADB" kill-server`, followed by `"$ADB" start-server`.
- **more than one device:** set `DEVICE_SERIAL`; never deploy to an implicit
  target when ADB lists multiple serials.
- **root check fails:** verify `"$ADB" shell su -c id` directly. This workflow
  requires an already-rooted/Magisk-enabled Dot and does not perform rooting.
- **push source is not found:** when a checkout lives in WSL's ext4 filesystem,
  Windows ADB may need a Windows path, for example
  `"$ADB" push "$(wslpath -w "$(realpath .bin/linux_arm64/echod)")" /data/local/tmp/echod`.
- **old Android shell commands are missing:** FireOS 5.1 does not provide
  common GNU utilities such as `install` or `timeout`. Keep device commands to
  the verified Android toolbox commands; do timeouts and output processing on
  the WSL host.

Codex can invoke the Windows ADB client and WSL toolchain in this setup, but its
sandbox requires host approval for WSL service and device access. Treat an
approval failure separately from an ADB or device failure.

## Debugging

Foreground execution plus structured logs is the supported Milestone 1
debugging workflow. Source-level VS Code debugging on the Dot is a later,
experimental step: it requires an unstripped `-N -l` build, a Linux/ARM64
Delve server, ADB port forwarding, and proof that ptrace works on this FireOS
kernel. Do not use the stripped release binary for Delve and do not treat remote
debugging as available until that hardware check passes.

## Later milestones

Once Milestone 3 lands, signed or explicitly allowed development releases go
through the gateway into the inactive application slot. That becomes the normal
iteration loop because it exercises trial health and rollback. ADB remains the
bootstrap, development, and recovery mechanism.

mDNS must be tested from the actual Docker/WSL deployment because multicast
visibility differs by network mode. An explicit gateway URL always wins over
discovery and remains the fallback.
