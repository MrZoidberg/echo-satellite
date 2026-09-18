# Device bootstrap and ADB recovery

This procedure installs only the Echo Satellite launcher and agent under
`/data`. It does not write FireOS, boot, recovery, or system partitions. Use
Linux ADB attached through USB/IP as described in
[Windows and WSL development](development-windows-wsl.md).

## Bootstrap

Build the host and device commands. The launcher is the repository payload;
`--agent` is the operator's known-good ARM64 `echod` binary.

```sh
make build build-device build-device-ctl
.bin/echoctl update bootstrap \
  --adb "$ADB" --serial "$DEVICE_SERIAL" \
  --agent .bin/linux_arm64/echod \
  --launcher device_payloads/launcher/echo-satellite.sh
```

Bootstrap accepts only the qualified Magisk v17.3 service directory
`/sbin/.core/img/.core/service.d`. It does not create or use
`/data/adb/service.d`. It installs `echo-satellite.sh` there and `echod` at
`/data/local/bin/echod`. If the launcher and agent bytes already match, the
operation is idempotent. It preserves a recognized earlier launcher or
direct-start hook as an Echo Satellite backup; an unknown hook stops the
operation without replacement.

Keep the known-good agent, manifest, detached signature, public key, and saved
launcher hook in an operator-controlled recovery bundle. Bootstrap does not
copy configuration, credentials, or wake assets.

Before rebooting, provisioning must create root-owned mode-0600
`/data/local/etc/echo-satellite/echod.ini`. The launcher passes this typed INI
to `echod`; it never sources shell configuration. A development configuration
names the existing token file and visibly opts into an untrusted certificate:

```ini
gateway-token-file = /data/local/tmp/echo-satellite-state/device-token
tls-skip-verify = true
```

Production provisioning uses a trusted gateway certificate and omits the TLS
bypass. An explicit `gateway-url` takes precedence over persisted pairing and
mDNS discovery.

## Connected downgrade and ADB recovery

While an agent remains connected, gateway rollback is a normal audited
deployment of an older signed compatible release. The device does not preserve
a fallback executable. If a committed agent cannot start or reconnect, recover
over ADB by pushing the cross-built `echoctl` once and installing a known-good
signed bundle as root:

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

The installer verifies compatibility, signature, exact size, and SHA-256 before
atomically replacing `/data/local/bin/echod`. Every failure before the rename
leaves the installed executable unchanged. Exit `echod` with status 75 for a
controlled restart, or reboot when the existing agent cannot be controlled; the
launcher restarts status 75 immediately and backs off unexpected exits.

Unsigned bundles are rejected by default. A temporary development recovery may
add `--allow-unsigned-dev-builds`; the command prints an explicit warning.

Inspect diagnostic metadata with:

```sh
/data/local/bin/echoctl update status
```

Metadata is diagnostic only: it never chooses an executable, performs rollback,
or substitutes for signed-bundle verification.
