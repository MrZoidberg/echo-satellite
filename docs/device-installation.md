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
does not copy configuration, credentials, or wake assets. Before rebooting the
launcher, provisioning must create root-owned mode-0600
`/data/local/etc/echo-satellite/echod.ini`. The launcher passes that typed INI
to `echod`; it never sources a shell configuration. For a development gateway,
the INI must at least name the already-provisioned token file and explicitly
opt into its development certificate, for example:

```ini
gateway-token-file = /data/local/tmp/echo-satellite-state/device-token
tls-skip-verify = true
```

Production provisioning uses the trusted gateway certificate and omits the TLS
bypass. The INI may also set an explicit `gateway-url`; otherwise `echod` uses
its persisted pairing and then mDNS discovery.

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

## Task 8 disposable update-offer gateway

`task8gateway` is a development-only, one-scenario harness for proving the
physical Dot's actual authenticated WSS and HTTPS update path. It is not the
normal `gateway` binary and it neither signs releases nor stores private keys.
It serves the selected artifact, manifest, and detached signature only to a
request authenticated with the configured device bearer token.

Build it on the Windows gateway host, then run one signed scenario at a time.
`--public-host` must be the host and port reachable by the Dot, rather than
`localhost`; the existing development `echod.ini` TLS bypass is required only
when the selected certificate is not trusted by the Dot.

Before the scenario, back up the Dot's existing
`/data/local/etc/echo-satellite/echod.ini` and make its selected Task 8 runtime
configuration contain these values, replacing any existing values rather than
adding duplicate INI keys:

```ini
gateway-url = wss://192.168.110.127:8770/device
discovery = disabled
```

The URL authority must exactly equal `--public-host`, including the port:
device-side update verification rejects artifact URLs from any other authority.
Restart `echod` (or reboot) after this configuration change, then restore the
backed-up INI and restart again after each Task 8 scenario. Explicit URL wins
over persisted pairing; disabling discovery makes the temporary test route
unambiguous.

```powershell
$secrets = Join-Path $PWD '.gateway-secrets'
go build -o .bin\task8gateway.exe .\cmd\task8gateway

.\.bin\task8gateway.exe `
  --listen ':8770' `
  --public-host '192.168.110.127:8770' `
  --server-id 'task8-gateway' `
  --tls-cert "$secrets\dev-cert.pem" `
  --tls-key "$secrets\dev-key.pem" `
  --device-token-file "$secrets\device-token" `
  --device-config "$secrets\devices.toml" `
  --artifact 'C:\operator-controlled\current\echod' `
  --manifest 'C:\operator-controlled\current\manifest.json' `
  --sig 'C:\operator-controlled\current\manifest.sig' `
  --deployment-id 'task8-current-001' `
  --response-mode normal `
  --dbg
```

The signed manifest supplies the offer's version, build ID, size, and SHA-256;
the command refuses malformed manifests. The harness exits after a terminal
`update.confirmed`, `update.failed`, or `update.cancelled` result, or after its
timeout. Its logs are the scenario record; they include no bearer token or
artifact contents.

Run separate invocations for an older signed artifact and these pre-commit
failure responses: `--response-mode truncate`, `tamper-artifact`, and
`tamper-signature`. Create the insufficient-space condition on the Dot under
the controlled Task 8 procedure, not by changing the gateway response. Do not
select an immediate-exit artifact until the operator has explicitly
acknowledged that drill immediately beforehand.
