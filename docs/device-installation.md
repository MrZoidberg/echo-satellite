# Device bootstrap and ADB recovery

This procedure installs only the Echo Satellite launcher and agent under
`/data`. It does not write FireOS, boot, recovery, or system partitions.

## Bootstrap

Build the host and device commands, then use an explicitly selected ADB binary
and device serial. The launcher path is the repository payload; `--agent` is
the operator's known-good ARM64 `echod` binary.

```sh
make build build-device-ctl
.bin/echoctl update bootstrap \
  --adb /path/to/adb --serial "$DEVICE_SERIAL" \
  --agent .bin/linux_arm64/echod \
  --launcher device_payloads/launcher/echo-satellite.sh
```

Bootstrap accepts only the qualified Magisk v17.3 service directory
`/sbin/.core/img/.core/service.d`. It does not create or use
`/data/adb/service.d`. It installs `echo-satellite.sh` there and `echod` at
`/data/local/bin/echod`. If the exact launcher and agent bytes already match,
the operation is idempotent. A prior recognized Echo Satellite launcher is
saved beside the hook as `echo-satellite.sh.echo-satellite-backup`. A recognized
direct-start hook named `echod` that starts this agent is likewise saved as
`echod.echo-satellite-backup`; unknown hook files stop the operation without
replacement.

Keep the known-good agent, its manifest, detached signature, public key, and
any saved launcher hook in an operator-controlled recovery bundle. Bootstrap
does not copy configuration, credentials, or wake assets.

## ADB recovery and manual rollback

If a committed agent cannot reconnect, push the cross-built `echoctl` to the
Dot once, then run the command on the Dot as root. A signed older compatible
bundle is a normal recovery install; no version-order check blocks it.

```sh
"$ADB" -s "$DEVICE_SERIAL" push .bin/linux_arm64/echoctl /data/local/bin/echoctl
"$ADB" -s "$DEVICE_SERIAL" shell "su -c '
  chmod 0755 /data/local/bin/echoctl
  /data/local/bin/echoctl update install \
    --artifact /data/local/tmp/echod \
    --manifest /data/local/tmp/manifest.json \
    --sig /data/local/tmp/manifest.sig \
    --pubkey /data/local/tmp/manifest.pub
'"
```

The installer verifies manifest compatibility, signature, exact size, and
SHA-256 before it atomically replaces `/data/local/bin/echod`. Request a
controlled restart by exiting `echod` with status 75, or reboot the Dot when
the existing agent cannot be controlled. The launcher restarts status 75
immediately and backs off unexpected exits.

Unsigned local bundles are rejected by default. Only a deliberately temporary
development recovery may add `--allow-unsigned-dev-builds`; the command prints
an explicit warning when it is enabled.

Inspect the diagnostic metadata with:

```sh
/data/local/bin/echoctl update status
```

The metadata is diagnostic only. It never chooses an executable, performs
rollback, or substitutes for the signed bundle verification above.
