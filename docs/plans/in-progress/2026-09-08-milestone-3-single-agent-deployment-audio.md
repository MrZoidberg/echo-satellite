# Milestone 3 — Single-agent deployment and command-audio conditioning

**Status:** in-progress
**Owner or active agent:** Codex (Tasks 1–7 completed)
**Created:** 2026-09-08
**Updated:** 2026-09-15
**Started:** 2026-09-08
**Completed:** not completed

## Objective

Deliver two recovery-aware device capabilities before assistant integration:

1. A signed single-agent deployment path that stages and verifies a replacement
   `echod`, atomically installs it, and restarts it through a minimal Magisk
   launcher.
2. A measured Echo Dot Gen 2 audio-conditioning profile that preserves quiet
   commands, avoids clipping, and retains correct wake and command-endpointing
   behavior.

The replacement design deliberately removes A/B slots, the stable supervisor,
trial health, automatic rollback, and preserved rollback binaries. If an
installed agent cannot reconnect, recovery requires ADB. When the client remains
operational, deploying an older signed release is the gateway rollback mechanism.

## Non-goals

- A/B slots, trial markers, automatic rollback, or a stable recovery supervisor.
- Retaining the prior agent executable after a successful replacement.
- FireOS, bootloader, boot-image, recovery, or system-partition updates.
- The Milestone 4 Gateway Update Manager, release discovery/cache, deployment
  persistence, staged fleet rollouts, or management APIs.
- Automatic supervisor or launcher updates through the agent deployment path.
- Full production provisioning, credential rotation, wake-model synchronization,
  or unbrick tooling.
- STT, TTS, assistant backends, conversations, and management UI.
- Gateway-side wake detection, idle microphone streaming, or gateway command
  endpointing.
- AEC, full-duplex playback, or barge-in.

## Source references and constraints

- `docs/DESIGN.md` currently specifies A/B recovery throughout §§2–3, 5–7,
  9–12, 17–18, and 20–28. Task 1 replaces those decisions before implementation.
- `docs/protocol.md` and `internal/protocol` must change together.
- Existing `internal/release` manifest, digest, signature, trust-policy, and
  eligibility primitives are retained where applicable.
- The active capture path remains one ALSA source feeding preprocessing, wake
  detection, pre-roll, command endpointing, and active-turn streaming.
- Wake VAD and command endpointing remain separate device-local components with
  independent settings.
- Production manifests require Ed25519 verification. Unsigned development
  releases remain disabled by default.
- A staged or rejected release must never damage the currently installed
  executable.
- After atomic installation, there is intentionally no automatic recovery
  guarantee.
- All new filesystem components use injected roots and remain portable in host
  tests.
- Hardware conclusions must be measured on the qualified rooted Dot rather than
  inferred.
- Historical finished plans remain unchanged; the revised `docs/DESIGN.md`
  records that their A/B contracts were superseded before implementation.

## Dependencies and prerequisites

- Milestones 1 and 2 and the operational startup work are complete.
- No active plan currently claims the deployment, preprocessing, or affected
  protocol scope.
- Hardware tasks require rooted Dot `G090LF0964060EHP`, Magisk root, working ADB,
  the qualified `okay_nabu` assets, gateway pairing, and physical microphone cut
  GPIO 444 read back as `0`.
- The first live diagnostic in each ADB session must stop `ledcontroller`, disable
  `boot_animation`, and confirm GPIO 444 as prescribed by `AGENTS.md`.
- A known-good current `echod`, its exact build revision, and its
  configuration/credentials must be backed up before enabling the new launcher.
- Execution begins in a clean branch or worktree. The orchestrator owns plan
  status, shared documentation, cross-cutting checks, and review triage.

## Architecture and high-level plan

### Single-agent deployment

```text
authenticated update.offer
  -> fetch signed manifest and signature over HTTPS
  -> validate manifest and device eligibility
  -> require offer metadata to match signed manifest
  -> verify free space
  -> download artifact to echod.part
  -> verify exact size and SHA-256
  -> chmod + fsync staged executable
  -> atomically rename echod.part over echod
  -> fsync containing directory where supported
  -> persist installed-release metadata
  -> report restarting
  -> echod exits with the controlled update exit code
  -> Magisk launcher starts the newly installed echod
  -> new echod reconnects and reports its version/build
```

`/data/local/bin/echod` is the only installed agent. The old process may continue
executing its unlinked inode until it exits, but its executable is not retained
as a rollback slot.

The launcher is a small `service.d` shell script, not a recovery supervisor. It
starts `echod`, restarts controlled update exits immediately, and applies
exponential backoff to unexpected repeated exits. It does not inspect update
health, alter installed files, select versions, or perform rollback.

A rollback is another deployment targeting an older signed compatible manifest:

- if `echod` is connected, the future Gateway Update Manager can offer the older
  release;
- if `echod` cannot connect, an operator uses ADB and `echoctl update install`;
- no semantic-version monotonicity rule prevents an authenticated, verified
  downgrade.

### Command-audio conditioning

```text
nine-channel ALSA capture
  -> select seven physical microphone channels
  -> qualified conditioning profile
       channel 0, unsteered mix, or delay-and-sum
       bounded gain and output leveling
  -> canonical 16-kHz mono PCM
  -> existing fanout
       -> wake VAD + wake model
       -> pre-roll
       -> command endpointing
       -> active-turn streaming
```

All candidates are compared from simultaneous recordings. The winner is
selected by fixed safety and quality rules, not implementation preference. The
selected hardware-specific algorithm is exposed to gateway configuration only
through a stable profile name.

## Public interfaces and protocol changes

- Replace capability `update.ab` with `update.single`.
- Remove `Hello.SupervisorVersion`.
- Remove `Manifest.SupervisorMin` and its eligibility check. The pre-v0.1
  manifest remains schema 1; old repository fixtures containing the removed
  field become invalid and are regenerated.
- Remove `trial` and `rolled_back` from device update phases, and remove
  `update.trial` and `update.rolled_back` messages.
- Retain phases needed for single deployment: `idle`, `available`, `queued`,
  `downloading`, `verifying`, `staged`, `restarting`, `confirmed`, `failed`, and
  `cancelled`.
- Require `deployment_id`, `artifact_url`, `manifest_url`, and `signature_url` in
  `update.offer`.
- Require the offer's version, build ID, size, and SHA-256 to equal the verified
  manifest.
- Add typed update decision/progress/failure payloads carrying `deployment_id`,
  phase, progress, stable error code, and sanitized detail.
- Add `AudioConfig.ConditioningProfile`. Initially accepted values are
  `bypass-v1` and, after qualification, `dot-gen2-qualified-v1`.
- Do not expose channel delays, beamformer weights, or adaptive-gain internals
  over the protocol.
- Protocol version remains 1 because the project is pre-v0.1 and no deployed
  compatibility promise exists.

## Planned file map

- `internal/device/update`: single-agent staging, validation, installation
  metadata, downloader policy, and update state machine.
- `internal/release`: revised single-agent manifest and eligibility rules.
- `internal/protocol`: revised capability, hello, update messages/phases, and
  audio configuration.
- `internal/device/audio`: channel mixing, delay-and-sum processing, bounded
  gain/leveling, and diagnostic metrics.
- `internal/device/client`: update-offer handling and update-status transmission.
- `cmd/echod`: deployment lifecycle, controlled restart, installed-build
  reporting, and selected audio profile composition.
- `cmd/echoctl`: minimal bootstrap, local signed installation, status, and audio
  comparison diagnostics.
- `cmd/dotsim`: deterministic single-agent download, restart, reconnect, and
  failure simulation.
- `device_payloads/launcher`: minimal Magisk `service.d` launcher.
- `testdata/updates` and `testdata/audio`: regenerated release fixtures and
  deterministic multichannel/conditioning fixtures.
- `docs/DESIGN.md`, `docs/protocol.md`, `AGENTS.md`, and operational diagnostics:
  revised boundary and measured evidence.

## Numbered tasks

### Task 1: Replace the A/B design with the single-agent boundary

**Status:** completed 2026-09-08

**Purpose:** Make repository sources of truth match the newly approved recovery
model before code is changed.

**Dependencies:** None.

**Hardware required:** no.

**Files or components:**

- Modify `docs/DESIGN.md`.
- Modify `AGENTS.md`.
- Modify `docs/protocol.md` only for the architectural overview; Task 3 completes
  the wire details.

**Concrete changes:**

- Replace all A/B, supervisor, trial, automatic-rollback, inactive-slot, and
  supervisor-version requirements.
- State that the gateway owns desired agent version while the device owns safe
  pre-install verification and atomic replacement.
- State explicitly that recovery after a bad committed agent requires ADB when
  the client cannot reconnect.
- Rewrite the update diagrams, repository layout, agent responsibilities,
  deployment lifecycle, simulator responsibilities, observability, Milestones
  3–4, first engineering tasks, target stack, and core update boundary.
- Remove the obsolete §26 supervisor/slot questions. Add hardware questions for
  Magisk launcher reliability, atomic replacement under `/data`, controlled
  restart, free-space margin, and ADB recovery.
- Preserve the prohibition on writing FireOS/system/boot/recovery partitions.
- Preserve signed production manifests and the disabled-by-default unsigned
  development escape hatch.
- Define gateway rollback as deploying a previous release, not switching a
  preserved slot.

**Expected outcome:** All authoritative documents consistently describe
single-agent deployment and its reduced recovery guarantee.

**Verification:**

```sh
rg -n "A/B|inactive slot|trial health|automatic rollback|supervisor_min|update\.ab" \
  AGENTS.md docs/DESIGN.md docs/protocol.md
```

Expected: no live design requirement retains the removed architecture; any
historical mention is clearly labeled superseded or out of scope.

### Task 2: Qualify launcher and filesystem behavior on FireOS

**Status:** completed 2026-09-08

**Purpose:** Resolve hardware-dependent deployment assumptions before
implementing the installer.

**Dependencies:** Task 1.

**Hardware required:** yes — rooted qualified Dot, current known-good agent
backed up, gateway reachable.

**Files or components:**

- Modify `docs/device-diagnostics.md`.
- Modify `docs/DESIGN.md` if measurements alter an assumption.

**Concrete changes:**

- Inspect `/data` filesystem type, mount flags, free space, inode availability,
  executable permissions, atomic same-directory rename, directory fsync
  behavior, and persistence across reboot.
- Use isolated files under `/data/local/tmp/echo-satellite-m3-diagnostic`; do not
  replace the active `echod`.
- Verify Magisk `service.d` execution timing and environment with a diagnostic
  hook that writes only a timestamp/version marker.
- Detect the service directory actually consumed by the installed Magisk;
  hardware evidence may replace the initially assumed modern path.
- Measure at least 20 current-agent starts from process launch through
  authenticated `welcome`.
- Validate controlled exit-code propagation and launcher restart behavior.
- Adopt a staging free-space requirement of artifact size plus
  `max(16 MiB, 10% of artifact size)` unless hardware evidence requires a
  documented plan amendment.
- Confirm that replacing the executable directory entry while the current agent
  runs leaves the old process alive until controlled exit.
- Remove the diagnostic hook and temporary files after recording results.

**Expected outcome:** The exact launcher, rename, fsync, restart, and free-space
assumptions are supported by real-device evidence.

**Verification:**

Run the inventory, executable-mode, staged-file/directory fsync, live rename,
reboot persistence, root-owned modern/legacy boot-hook A/B, controlled-exit,
complete crash-backoff, 20-start timing, installed-binary readlink, and cleanup
commands recorded under **Reproduction record** in
[`docs/device-diagnostics.md`](../../device-diagnostics.md).

Expected: the diagnostic record includes filesystem observations, boot-hook
proof, 20 startup timings, controlled restart proof, and cleanup confirmation.

### Task 3: Revise release and protocol contracts

**Status:** completed 2026-09-09

**Purpose:** Provide strict contracts for signed single-agent deployment and
named audio profiles.

**Dependencies:** Tasks 1–2.

**Hardware required:** no.

**Files or components:**

- Modify `internal/release`.
- Modify `internal/protocol`.
- Modify `docs/protocol.md`.
- Regenerate `testdata/updates`.

**Concrete changes:**

- Remove `supervisor_min` from manifest parsing, validation, canonical signing
  bytes, eligibility, fixtures, and CLI output.
- Replace the A/B capability and remove trial/rolled-back phases and messages.
- Define strict typed payloads for offers, decisions, progress, confirmation,
  cancellation, and failure.
- Require a nonempty deployment ID and all three HTTPS resource URLs.
- Define stable failure codes: `busy`, `invalid_offer`, `ineligible`,
  `insufficient_space`, `download_failed`, `signature_invalid`, `size_mismatch`,
  `digest_mismatch`, `stage_failed`, `install_failed`, and `restart_failed`.
- Add conditioning-profile configuration with strict unknown-field and
  unsupported-profile rejection.
- Update envelope tests and protocol documentation in the same change.
- Preserve capability negotiation; do not introduce behavior gates based on
  agent version.

**Expected outcome:** Wire and release packages expose only the new single-agent
model.

**Verification:**

```sh
go test -race ./internal/release/... ./internal/protocol/...
go test ./internal/release -run TestFixtures_Regenerate -update-fixtures
git diff --check
```

Expected: tests pass, regenerated fixtures are stable, and a second
fixture-regeneration run produces no diff.

### Task 4: Implement transactional single-agent staging

**Status:** completed 2026-09-10

**Purpose:** Ensure every failure before the atomic rename leaves the installed
agent unchanged.

**Dependencies:** Task 3.

**Hardware required:** no.

**Files or components:**

- Create implementation and tests under `internal/device/update`.

**Concrete changes:**

- Declare consumer-side interfaces for downloader, filesystem operations,
  free-space query, clock, and release trust.
- Accept only HTTPS URLs on the authenticated gateway authority; reject userinfo
  and cross-authority redirects.
- Reuse the current gateway TLS/auth settings without logging complete URLs,
  query values, bearer tokens, signatures, or credentials.
- Serialize updates and reject offers while another update or voice turn owns
  the device.
- Download to a uniquely created same-directory `.part` file with restrictive
  permissions.
- Stream-enforce the declared maximum size, verify the signed manifest, check
  architecture/protocol eligibility, match offer metadata, then verify artifact
  size and SHA-256.
- Apply executable permissions, fsync the file, atomically rename it over
  `/data/local/bin/echod`, and fsync the directory where Task 2 proved support.
- Treat the rename as the installation commit point. Failures before it remove
  staging data; failures after it report committed-but-restart-failed and require
  manual recovery.
- Persist strict installed-release metadata for diagnostics. On startup,
  reconcile stale metadata against the binary's link-time revision rather than
  treating metadata as recovery authority.
- Allow an older signed compatible version; this is how manual or gateway
  rollback works.
- Ensure cancellation is accepted only before the rename commit point.

**Expected outcome:** The device has a reusable, host-tested single-agent
installer with explicit pre- and post-commit failure semantics.

**Verification:**

```sh
go test -race -count=20 ./internal/device/update/... ./internal/release/...
```

Expected: fault injection at every write, verify, chmod, fsync, rename, and
metadata step proves pre-commit failures preserve the original executable.

### Task 5: Add the Magisk launcher and minimal deployment CLI

**Status:** completed 2026-09-10

**Purpose:** Provide repeatable bootstrap, restart, status, and ADB recovery
without building a recovery supervisor.

**Dependencies:** Tasks 2 and 4.

**Hardware required:** no for implementation; real-device proof is Task 8.

**Files or components:**

- Create `device_payloads/launcher/echo-satellite.sh`.
- Modify `cmd/echoctl`, Makefile, and installation documentation.

**Concrete changes:**

- Add a minimal launcher that starts `/data/local/bin/echod`.
- Install it in the version-qualified Magisk service directory. On the
  qualified Magisk v17.3 Dot this is
  `/sbin/.core/img/.core/service.d`, not `/data/adb/service.d`; fail closed on
  an unrecognized layout.
- Reserve exit code 75 for controlled update restart.
- Restart the controlled update exit code immediately.
- For unexpected exits, back off 1, 2, 4, 8, 16, 32, then 60 seconds; reset the
  backoff after 60 seconds of continuous runtime.
- Keep the launcher free of JSON parsing, release validation, health judgments,
  version selection, and rollback logic.
- Add `echoctl update bootstrap` for host-side ADB installation of the launcher
  and initial known-good agent. Require explicit ADB path, device serial, agent
  path, and launcher path.
- Back up an existing launcher or direct-start hook before replacement. Never
  modify configuration, credentials, wake assets, or unrelated Magisk files.
- Make bootstrap idempotent when installed bytes match and fail safely on an
  unrecognized conflicting installation.
- Add on-device `echoctl update install` using the Task 4 installer and
  `echoctl update status` using installed-release metadata.
- Require `--allow-unsigned-dev-builds` explicitly for an unsigned local install;
  default remains rejection.
- Document manual rollback as pushing a previous signed bundle, installing it,
  and requesting a controlled restart.

**Expected outcome:** A fresh or recovered Dot can install the launcher and
agent repeatably, while the launcher remains non-authoritative recovery plumbing.

**Verification:**

```sh
go test -race ./cmd/echoctl/...
make build-device-ctl
make check-portability
```

Expected: fake-ADB tests cover fresh install, idempotence, backup, interruption,
conflicting files, signed downgrade, and unsigned rejection.

#### Task 5 hardware remediation: qualify bootstrap comparison utility

**Status:** completed 2026-09-10

**Purpose:** Correct a FireOS-specific bootstrap defect discovered during the
isolated real-device check without changing the launcher/update boundary.

**Dependencies:** Task 5.

**Concrete changes:**

- Use `/data/adb/magisk/busybox cmp -s` in the remote bootstrap script rather
  than the incompatible FireOS `/system/bin/cmp`.
- Add a regression test for the qualified comparator command.

**Verification:**

```sh
go test -race ./cmd/echoctl/...
```

Expected: host tests pass and a same-byte real-device bootstrap removes its
staged agent file while preserving the installed digest.

#### Task 5 hardware remediation: launch the provisioned runtime configuration

**Status:** completed 2026-09-15

**Purpose:** Correct the Task 8 reboot finding that the minimal launcher starts
`echod` without the required gateway-token configuration, causing a bounded
crash loop even when the device already has its configuration and credentials.

**Dependencies:** Task 5 and the Task 8 rooted-Dot reboot observation.

**Files or components:**

- Modify `device_payloads/launcher/echo-satellite.sh`.
- Modify launcher tests and `docs/device-installation.md`.

**Concrete changes:**

- Invoke `echod` with its typed, provisioned INI path
  `/data/local/etc/echo-satellite/echod.ini`; do not source an arbitrary shell
  configuration or copy credentials during bootstrap.
- Require initial provisioning to create the root-owned INI separately, with
  the gateway token path and any development TLS override where explicitly
  intended.
- Cover the exact launcher command in host tests and re-bootstrap/reboot the
  qualified Dot with its existing token and paired gateway state.
- Pass the fixed remote bootstrap script as the single quoted argument to the
  Dot's `su -c`; do not rely on ADB preserving separate `su` argument tokens.

**Verification:**

```sh
go test -race ./cmd/echoctl/...
make build-device
make build-device-ctl
```

Expected: the launcher reaches the configured `echod` process after reboot;
missing or malformed runtime configuration remains an observable agent failure,
not a launcher recovery decision.

#### Task 5 hardware remediation: compare bootstrap payloads by SHA-256

**Status:** completed 2026-09-15

**Purpose:** Correct the Task 8 observation that the qualified Magisk BusyBox
`cmp` reports success for distinct files, causing bootstrap to report success
without replacing a recognized older launcher.

**Dependencies:** Task 5 and the Task 8 rooted-Dot comparator probe.

**Files or components:**

- Modify `cmd/echoctl/update.go` and its tests.
- Modify this plan and the Task 8 hardware evidence.

**Concrete changes:**

- Compare staged and installed launcher/agent SHA-256 digests through the
  qualified `/data/adb/magisk/busybox sha256sum`, rather than its unreliable
  `cmp` applet; use the same BusyBox for digest formatting and marker reads
  because FireOS lacks the corresponding system applets.
- Keep replacement, backup, conflict, and staged-file cleanup rules unchanged.
- Re-bootstrap the qualified Dot, verify the old launcher is backed up and the
  new launcher bytes are installed, then reboot and verify configured startup.

**Verification:**

```sh
go test -race ./cmd/echoctl/...
make build
```

Expected: a distinct recognized launcher is backed up and replaced on the Dot;
same-byte bootstrap remains idempotent.

### Task 6: Integrate deployment with `echod`

**Status:** completed 2026-09-14

**Purpose:** Make the connected device accept, execute, and report deployments
without pulling the Gateway Update Manager into Milestone 3.

**Dependencies:** Tasks 3–5.

**Hardware required:** no.

**Files or components:**

- Modify `internal/device/client`.
- Modify `cmd/echod`.
- Modify related configuration and tests.

**Concrete changes:**

- Handle typed update offers on the existing authenticated WSS session.
- Accept offers only while idle. Once accepted, block new turns until the update
  fails or the process restarts.
- Report accepted, downloading, verifying, staged, restarting, confirmed,
  cancelled, and failed transitions with the deployment ID.
- Perform download and verification outside the WSS reader loop so heartbeats
  and cancellation remain responsive.
- After atomic installation, report `restarting`, drain the control message,
  then exit with the launcher's controlled restart code.
- On new startup, include installed version/build and pending deployment identity
  in `hello`.
- On startup, call Task 4's metadata reconciliation against the link-time
  revision. Surface stale or malformed metadata only as diagnostics; it must
  never select or recover an executable.
- After authenticated `welcome`, report `confirmed` and clear pending restart
  metadata. This confirmation is observational only and never triggers rollback.
- If the new agent cannot start or reconnect, rely on launcher retries and ADB
  recovery.
- Preserve the existing voice boundary and raw-audio privacy behavior.

**Expected outcome:** `echod` can complete a signed deployment and reconnect as
the installed build.

**Verification:**

```sh
go test -race -count=10 ./cmd/echod/... ./internal/device/client/... ./internal/device/update/...
```

Expected: tests cover active-turn rejection, disconnects, cancellation,
duplicate offers, metadata mismatch, controlled restart ordering, confirmed
reconnect, and post-commit failure reporting.

### Task 7: Simulate single-agent deployments

**Status:** completed 2026-09-14

**Purpose:** Keep future gateway work testable without a physical Dot.

**Dependencies:** Tasks 3–6.

**Hardware required:** no.

**Files or components:**

- Modify `cmd/dotsim` and its tests.

**Concrete changes:**

- Simulate offer acceptance, download progress, verification, installation,
  restart, reconnect, confirmation, cancellation, and terminal failure.
- Remove A/B slot, trial-timeout, crash-rollback, and old-slot reconnect
  simulation.
- Add deterministic controls for interrupted download, invalid signature, digest
  mismatch, insufficient space, restart failure, and failure to reconnect after
  installation.
- Model gateway rollback as another deployment with an older signed version.
- Use an in-process authenticated test server; do not implement release
  persistence or rollout policy.

**Expected outcome:** Milestone 4 can test gateway deployment behavior against
the same single-agent protocol used by `echod`.

**Verification:**

```sh
go test -race -count=20 ./cmd/dotsim/... ./internal/protocol/...
```

Expected: all success, failure, cancellation, downgrade, restart, and reconnect
scenarios are deterministic and race-clean.

### Task 8: Prove installation and ADB recovery on the Dot

**Status:** superseded 2026-09-15

**Superseded by:** `docs/plans/future/2026-09-15-release-signing-and-deployment-qualification.md`,
Tasks 1–2. The release-signing workflow required to create the trusted inputs
does not exist in Milestone 3, so its live signed-release proof cannot be
performed or completed honestly here.

**Purpose:** Validate both the normal deployment path and the deliberately
reduced recovery guarantee.

**Dependencies:** Tasks 5–7.

**Hardware required:** yes — qualified Dot, working gateway, backed-up
known-good build.

**Files or components:**

- Modify `docs/device-diagnostics.md`.
- Modify operational installation/recovery documentation.

**Concrete changes:**

- Bootstrap the launcher and known-good agent and prove startup across a reboot.
- Deploy a valid signed replacement through a narrow authenticated test gateway,
  then verify controlled restart, reconnect, matching build identity, and
  confirmation.
- Attempt truncated, tampered, wrong-signature, and insufficient-space
  deployments; each must leave the installed executable digest unchanged.
- Deploy an older signed build and prove downgrade/rollback is treated as a
  normal deployment.
- With explicit operator acknowledgement, install an immediate-exit diagnostic
  build, observe launcher backoff, and recover by pushing and installing the
  known-good signed bundle through ADB.
- Do not describe the last scenario as automatic rollback. Record the period of
  unavailability and every manual recovery command.
- Confirm no boot, recovery, system, or supervisor path was written.

**Expected outcome:** Superseded before completion; the successor plan proves
normal replacement, downgrade, pre-commit safety, and the manual ADB recovery
boundary on the target device after it establishes signing.

**Verification:**

```sh
make build-device
make build-device-ctl
uv run --no-project --script tools/device-lab/device_lab.py preflight \
  --adb "$ADB" --serial "$DEVICE_SERIAL"
uv run --no-project --script tools/device-lab/device_lab.py prepare \
  --adb "$ADB" --serial "$DEVICE_SERIAL" --resume .bin/device-lab/<session-id>
"$ADB" -s "$DEVICE_SERIAL" shell "su -c '
  readlink /proc/\$(pidof echod)/exe
  /data/adb/magisk/busybox sha256sum /data/local/bin/echod
  cat /data/local/etc/echo-satellite/installed-release.json
'"
uv run --no-project --script tools/device-lab/device_lab.py cleanup \
  --adb "$ADB" --serial "$DEVICE_SERIAL" --resume .bin/device-lab/<session-id>
uv run --no-project --script tools/device-lab/device_lab.py verify-clean \
  --adb "$ADB" --serial "$DEVICE_SERIAL" --resume .bin/device-lab/<session-id>
```

Expected: superseded verification. No on-Dot signed deployment, downgrade, or
ADB recovery result is claimed by Milestone 3.

#### Task 8 remediation: disposable authenticated update-offer gateway

**Status:** completed 2026-09-15

**Purpose:** Supply the narrowly scoped, operable authenticated offer gateway
that Task 8 requires without expanding the normal gateway into a release or
rollout controller.

**Dependencies:** Tasks 6–7 and the approved
`docs/plans/2026-09-15-task8-test-gateway-design.md` design.

**Hardware required:** no for implementation; it is consumed by successor-plan
Task 2's rooted-Dot verification.

**Files or components:**

- Create `cmd/task8gateway` and its tests.
- Modify `Makefile` and Task 8 operational documentation.

**Concrete changes:**

- Add a development-only TLS server that authenticates a device bearer token,
  performs the normal `hello`/`welcome` exchange, serves one explicit signed
  offer, and records sanitized update decisions and transitions across a
  controlled-restart reconnect.
- Serve the explicitly selected artifact, manifest, and detached signature over
  absolute HTTPS URLs on the same listener. Support explicit test-only artifact
  truncation, body mutation, and selected invalid-signature response modes.
- Do not generate signatures, accept private signing keys, persist releases,
  modify the production `gateway` binary, or send duplicate offers after a
  reconnect.

**Expected outcome:** An operator can run one auditable successor-plan scenario
at a time against a physical Dot using its existing authenticated WSS and HTTPS
paths.

**Verification:**

```sh
go test -race ./cmd/task8gateway/...
GOOS=windows GOARCH=amd64 go build -o .bin/task8gateway.exe ./cmd/task8gateway
```

Expected: authentication, absolute offer URLs, artifact modes, one-shot offer,
and update-result recording are covered by host tests; the Windows binary
builds for the operator-run gateway host.

### Task 9: Adapt the EchoLocal conditioning baseline

**Status:** completed 2026-09-15

**Purpose:** Reuse a proven Go-native microphone mixing and steerable
beamforming implementation without importing upstream assistant behavior or
assuming its hardware calibration wins on this Dot.

**Dependencies:** Task 5's qualified launcher/bootstrap path. This audio work
does not depend on deferred signed-release qualification.

**Hardware required:** yes — qualified Dot with Amazon LED ownership disabled
and GPIO 444 low.

**Files or components:**

- Modify `internal/device/audio`.
- Extend `echoctl` comparison diagnostics.
- Modify `docs/third-party-notices.md`, `docs/device-diagnostics.md`, and this
  plan.
- Add deterministic multichannel fixtures and operator-approved recordings only
  when necessary for qualification.

**Concrete changes:**

- Copy and adapt only EchoLocal's microphone mixing and steerable beamforming
  primitives behind the existing `Preprocessor` seam. Do not copy its
  assistant, controller, wake-routing, service, configuration, update, or
  hardware-lifecycle behavior.
- Pin the upstream source revision; retain the local MIT license and add the
  required adapted-file headers and third-party-notice entries.
- Preserve the seven physical microphone inputs and exclude playback loopback
  channels 7–8 in every candidate.
- Add deterministic impulse, fixed-delay, polarity, channel-exclusion,
  silence, saturation, and portable-`noasm` tests. The tests establish
  algorithm behavior, not a claim about this Dot's array geometry.
- Retain the local scorecard to verify capture format, physical channel order,
  polarity/delay observations, and clean capture health before qualification.
- Keep raw voice recordings only with operator approval; otherwise retain
  derived metrics and delete captures after analysis.

**Expected outcome:** EchoLocal-derived candidate processors are attributable,
deterministic, portable, and ready for an evidence-based qualification run.

**Verification:** deterministic host tests cover every adapted primitive and
`echoctl mic` JSON verifies an exactly-seven-channel, clean physical capture
with an accurate raw-audio disposition. Real-device acoustics remain Task 10.

### Task 10: Implement and select the conditioning profile

**Status:** in progress

#### Task 10 front/550 mm combined-capture design

**Status:** completed 2026-09-16

**Purpose:** Reduce operator interaction for the first real-Dot acoustic matrix
batch without weakening the per-cell capture-health, matched-noise, or
raw-audio-retention rules.

**Scope and ownership:** A new token-owned `tools/device-lab/payloads/`
diagnostic payload and its Python runner tests. It is not an `echoctl` command,
does not modify `/data/local/bin/echod`, and must not be run until the operator
starts a separately requested hardware session.

**Design:** At recorded front distance **550 mm**, the payload writes seven
sequential, simultaneous seven-microphone WAV captures with a `--health-out`
sidecar for each: deliberate silence; room noise then normal speech; a fresh
room-noise baseline then quiet speech; and a fresh room-noise baseline then
loud speech. Each window is ten seconds. A three-second `thinking` (blue comet)
cue separates windows; `listening` (cyan-green) begins one second before and
remains active through each ten-second speech window.
The payload records its capture schedule and echoctl reports in the session
root. It then writes a silence scorecard and three `mic compare` reports,
passing the corresponding health sidecars; the silence sidecar is also checked
for zero XRuns and dropped frames. The comparison command deletes raw
paired WAVs after output publication; the payload scorecards and removes the
standalone silence WAV after its scorecard. It fails immediately on capture,
health, scorecard, or comparison error and leaves the device-lab cleanup path
available.

**Alternatives rejected:** A manual command sheet can mistime the operator
windows and gives weaker provenance. A new `echoctl mic sequence` production
command would add unsupported diagnostic surface for one qualification batch.

**Verification before hardware:** Unit-test payload staging and its exact
argument/order contract; run the relevant device-lab tests. Hardware execution
later requires the standard preflight, prepare, cleanup, and verify-clean
sequence, then operator observation of every cue and recording of the session
evidence. This subtask does not select `dot-gen2-qualified-v1`; it only
prepares the first front/550 mm evidence batch.

**Purpose:** Qualify and select a bounded, observable profile among channel 0,
an unsteered mix, and the EchoLocal-derived steerable beamformer.

**Dependencies:** Task 9.

**Hardware required:** host fixtures first; final selection uses the Dot
recordings from Task 9.

**Files or components:**

- Modify `internal/device/audio`.
- Extend `echoctl` comparison diagnostics.
- Add deterministic audio fixtures.

**Concrete changes:**

- Complete the three processors behind the existing `Preprocessor` interface:
  channel 0, polarity-correct unsteered mix, and the Task 9 EchoLocal-derived
  steerable delay-and-sum baseline.
- Add bounded automatic gain/output leveling with maximum 12 dB gain, at least
  1 dBFS headroom, saturation-safe conversion, and controlled attack/release.
- Report profile name, applied gain, peak/RMS, clipping count/fraction, noise
  level, speech/noise separation, and processing duration.
- Add gain-transition tests and qualify every candidate with the Task 9
  deterministic fixtures plus simultaneous real-Dot captures.
- Disqualify a candidate if it:
  - clips more than 0.1% of output samples;
  - causes an XRun or dropped capture frame;
  - misses the 80 ms processing cadence;
  - accepts fewer than 18 of 20 qualified wakes;
  - endpoints continuous quiet speech before 60 seconds;
  - fails deliberate-silence endpointing.
- Capture controlled silence, steady room noise, normal speech, continuous
  quiet speech, and loud/clipping speech from front, off-axis, and far-field
  positions at recorded distances. Among passing candidates, choose the
  greatest repeatable improvement in the
  minimum speech/noise separation across measured positions. Treat differences
  below 1 dB as equivalent and prefer the simpler candidate in this order:
  channel 0, unsteered mix, delay-and-sum.
- Publish the winner as `dot-gen2-qualified-v1`. EchoLocal-derived beamforming
  is not presumed to win; if none passes, retain `bypass-v1` and leave the
  milestone blocked.

**Expected outcome:** The selected profile has objective evidence, bounded gain,
and explanatory diagnostics.

**Verification:**

```sh
go test -race -count=20 ./internal/device/audio/...
make build-device
make build-device-noasm
```

Expected: tests pass with zero data races; fixture metrics satisfy clipping and
timing bounds.

### Task 11: Integrate and requalify command audio

**Status:** completed 2026-09-17 (user-directed)

#### Task 11 operator-run hardware procedure

**Status:** completed 2026-09-16

**Purpose:** Make the real-Dot qualification reproducible without installing
an unverified diagnostic binary or retaining raw audio by default.

**Scope and ownership:** `tools/device-lab/payloads/task11_command_audio.sh`,
the runner's host tests, and `docs/device-diagnostics.md`. The payload stages
the current ARM64 `echod` only beneath its token-owned root and relies on the
existing paired INI with an isolated device config state; it does not install
an agent or modify gateway state.

**Procedure:** Build the ARM64 agent, add the gateway's temporary opt-in WAV
and log directories, then invoke `device_lab.py run-payload` with explicit
`/usr/bin/adb`, `G090LF0964060EHP`, the Task 11 payload, the built diagnostic,
and `--stop-known-launcher`. The payload records all human trial windows in a
sanitized UTC schedule, deletes local agent logs, and lets the runner cleanup,
verify the installed digest, and restart the known launcher.

**Verification before hardware:** `sh -n` validates the shell payload and
`uv run --no-project --script tools/device-lab/device_lab_test.py` performs
static contract checks for its isolated paths, phase schedule, no-raw-audio
intent, bounded signal cleanup, and result manifest. The runner buffers the
payload output and the current runtime lacks task-attributable capture-health
and conditioning metrics; add a real-time phase cue and an attributable
metric/health collector before using this procedure as qualification evidence.
Real execution remains the parent Task 11 acceptance evidence.

**Purpose:** Prove that the selected conditioning improves audio without
regressing local wake or endpointing.

**Dependencies:** Task 10.

**Hardware required:** yes — qualified Dot and the Task 9 acoustic setup.

**Files or components:**

- Modify `cmd/echod` composition and configuration.
- Modify `docs/device-diagnostics.md`.
- Update `docs/DESIGN.md` with the selected default.

**Concrete changes:**

- Apply the selected profile once before `audio.Fanout`, ensuring wake, pre-roll,
  endpointing, and transmitted command PCM share the same conditioned frames.
- Preserve independently configured wake VAD and endpointing detectors.
- Re-run 20 `okay_nabu` trials and require at least 18 accepted wakes.
- Run a 15-minute idle/music test and require zero false wake accepts.
- Run three continuous quiet-speech trials with no 1.5-second pause; each must
  stop only as `timeout` at 59.9–60.2 seconds.
- Run three spoken-command-plus-silence trials; each must stop as `endpointed`
  1.3–2.2 seconds after speech ends.
- Run three silence-only trials; each must stop as `no_speech` 2.8–3.3 seconds
  after turn start.
- Require zero dropped frames/XRuns and no more than 0.1% clipped output samples.
- Record processing latency, CPU/RSS, gain, peak/RMS, clipping, and speech/noise
  metrics.
- Confirm gateway diagnostic WAV duration and stop reason when opt-in recording
  is enabled, then restore raw-audio storage to disabled.

**Expected outcome:** The qualified command path meets the Milestone 3
quiet-speech and deliberate-silence criteria without weakening local wake
behavior.

**Verification:** All stated real-device trials and numeric thresholds pass and
are recorded. Simulator or fixture success cannot complete this task.

### Task 12: Cross-cutting verification, documentation, and fresh review

**Status:** not started

**Purpose:** Reconcile the new design, close stale A/B assumptions, and obtain
independent review.

**Dependencies:** Tasks 1–11.

**Hardware required:** no additional run, but Tasks 8–11 evidence must exist.

**Files or components:**

- All touched code and documentation.
- This plan's progress and completion-evidence sections.

**Concrete changes:**

- Reconcile final protocol payloads, manifest format, launcher behavior, CLI
  commands, recovery limitations, audio profile, and hardware defaults across
  all documentation.
- Scan code, tests, configuration, examples, and current docs for stale A/B,
  supervisor, trial, rollback-slot, and `supervisor_min` behavior.
- Preserve historical plan records unchanged.
- Review coverage for every touched function and document deliberately untested
  composition or hardware-only paths.
- Dispatch a fresh-context review agent with the complete diff, this plan,
  `docs/DESIGN.md`, and `docs/protocol.md`.
- Triage every finding as fix, decline with reason, or postpone into a durable
  follow-up. Correctness and security findings are not silently postponed.
- Re-run focused checks after every review fix, followed by final repository
  verification.

**Expected outcome:** Implementation, sources of truth, tests, and operational
instructions consistently describe and verify the simplified milestone.

**Verification:**

```sh
git diff --check
make fmt-check
make lint
make test
make check-portability
make build-device
make build-device-noasm
make build-device-ctl
make verify
```

Expected: all commands succeed, the fresh review is fully triaged, and
completion evidence names which checks ran on real hardware.

## Cross-task risks

- **Committed bad binary:** Once rename succeeds, no local rollback binary
  exists. Mitigation: strict pre-install verification, simulator coverage,
  signed artifacts, a tested launcher, and a documented ADB recovery bundle.
- **Launcher crash loop:** A bad agent may repeatedly exit. Mitigation:
  exponential backoff capped at 60 seconds and explicit ADB recovery.
- **Credential leakage during download:** Artifact URLs or headers may contain
  secrets. Mitigation: same-authority HTTPS, disabled redirects, sanitized logs,
  and no URL query logging.
- **Interrupted installation:** Power loss before rename must preserve the old
  path; power loss after rename may leave the new binary installed without
  matching metadata. Mitigation: same-directory staging/fsync/rename and startup
  metadata reconciliation.
- **Unsafe downgrade:** Gateway rollback permits installing an older signed
  version. Mitigation: retain architecture/protocol compatibility checks and
  trusted signatures; the gateway decides desired version.
- **CPU regression from conditioning:** Wake inference already has limited
  device headroom. Mitigation: enforce the 80 ms cadence, benchmark both NEON
  and `noasm`, and retain the simple channel-0 profile when more complex
  candidates do not clearly pass.
- **Audio overfitting:** One room or direction may favor a misleading candidate.
  Mitigation: simultaneous multichannel capture across front, off-axis,
  far-field, quiet, loud, silence, and noise conditions.
- **Privacy:** Diagnostic audio may contain voice. Mitigation: explicit opt-in
  retention, derived metrics by default, and recorded deletion/retention
  disposition.

## Rollback or recovery

- Before enabling the launcher, preserve the current known-good `echod`, existing
  startup hook, configuration, credentials, and wake assets in an
  operator-controlled host recovery bundle.
- Pre-commit deployment failures delete only the `.part` file and leave
  `/data/local/bin/echod` unchanged.
- After commit, rollback means installing a previous signed compatible release:
  - through the gateway if the current client connects;
  - through `echoctl update install` over ADB if it does not.
- The launcher can restart processes but cannot restore versions.
- Disable the project launcher by moving only its exact backed-up service hook,
  then restore the recorded previous hook during recovery.
- Normal deployment and recovery never write FireOS system, boot, recovery, or
  bootloader partitions.

## Final acceptance criteria

- [ ] `docs/DESIGN.md`, `AGENTS.md`, and `docs/protocol.md` consistently define
  the single-agent recovery boundary.
- [ ] No current code or configuration announces `update.ab`, supervisor
  compatibility, trial, or automatic rollback.
- [ ] Signed release verification covers manifest authenticity, offer
  consistency, eligibility, size, and SHA-256.
- [ ] Every tested failure before atomic rename preserves the installed
  executable.
- [ ] The minimal Magisk launcher starts the agent after reboot and applies
  bounded retry backoff.
- [ ] No deployment path touches bootloader, boot, recovery, system partitions,
  configuration, credentials, wake assets, or unrelated Magisk files.
- [ ] All seven physical microphone channels are characterized on the qualified
  Dot.
- [ ] Channel 0, unsteered mix, and delay-and-sum candidates are compared under
  identical input.
- [ ] The selected profile meets clipping, timing, wake, quiet-speech,
  deliberate-silence, and no-speech criteria.
- [ ] Continuous quiet speech reaches the 60-second cap without premature
  endpointing.
- [ ] Final formatting, lint, race tests, portability builds, device builds, and
  `make verify` pass.
- [ ] Fresh-context findings receive explicit dispositions.
- [ ] Hardware and host/simulator evidence are identified separately.

## Progress log

- 2026-09-17: Task 11 marked completed at the user's explicit direction after
  the shortened on-device run through the Windows gateway at
  `192.168.110.127`. The run completed all scheduled phases (5 wake windows,
  telemetry checkpoint, 1-minute idle, 3 continuous-speech, 3 endpointing, and
  3 Action-button/no-speech windows); cyan-green and yellow operator ring cues
  were used. Gateway evidence contains four turn records with version-1
  terminal telemetry, and the device-lab cleanup/verify-clean checks passed
  with the installed agent unchanged. This completion is recorded despite the
  run not satisfying every original numeric acceptance threshold: the observed
  wake/endpoint/button counts and full Task 11 acoustic qualification remain
  limitations, not claims of threshold compliance. Host `make verify` passed.

- 2026-09-17: Task 11 hardware execution was intentionally stopped before any
  human wake, command, endpointing, idle/music, or diagnostic-WAV acceptance
  evidence was recorded. Device-lab session
  `20260917T104439Z-c5368b3589` exposed FireOS-specific runner defects while
  releasing the known agent: the valid parent is Magisk BusyBox executing
  `sh /sbin/.core/img/.core/service.d/echo-satellite.sh`, rather than
  `/system/bin/sh`, and cleanup used an unqualified `rm`. Both are **fixed**:
  release accepts only that exact hook command with either qualified shell
  executable, cleanup uses preflight-qualified BusyBox, and interrupted
  sessions can remove only a digest-checked, token-owned residual root. The
  runner now restarts the recognized launcher after successful `verify-clean`.
  The final cleanup-residual-root, verify-clean, and launcher-restart phases
  passed with the original installed-agent digest. Host checks: `uv run
  --no-project --script tools/device-lab/device_lab_test.py` (32 tests) and
  `git diff --check`. This is recovery evidence only; Task 11 remains
  in progress and requires a present operator for its spoken/audio trials.
  Fresh-context review then found two high-severity failure-path defects:
  restoration intent was recorded too late if post-TERM release checks failed,
  and a second host interrupt could skip remaining restoration. Both are
  **fixed**: the exact validated launcher identity is persisted before TERM,
  then revalidated before release, and all cleanup/verify/restart actions catch
  `BaseException` before reporting the original failure. The updated device-lab
  host suite has 33 passing tests. Follow-up review found one remaining
  high-severity crash-window risk: pre-TERM restoration intent could relaunch
  alongside an agent that never stopped. This is **fixed**: restoration now
  scans `/proc` on the device and no-ops only with exactly one live installed
  agent, failing closed for multiple agents. The host suite has 34 passing
  tests; final follow-up review is pending.

- 2026-09-16: Task 11 claimed by Codex after the user explicitly directed a
  provisional profile change before the deferred Task 10 acoustic cells are
  complete. `dot-gen2-qualified-v1` is provisionally mapped to bounded
  channel-0 conditioning, the simplest candidate favored by the specified
  tie-breaker for the available front/550 mm results. This is an integration
  decision, not a claim that Task 10 has selected or qualified a profile: its
  remaining off-axis/far-field, wake, and endpointing evidence remains open.
  Task 11 cannot be completed until its real-Dot trials, gateway diagnostic
  recording check, and raw-audio-storage cleanup are recorded.

- 2026-09-16: Fresh-context Task 11 review found two correctness defects, both
  **fixed**. Gateway desired state now defaults to `dot-gen2-qualified-v1`, and
  the capture composition always supplies the seven physical channels to a
  static profile preprocessor. Its `bypass-v1` branch explicitly returns mic0,
  preserving the former fallback rather than changing it into a seven-channel
  average. A gateway-requested profile change is atomically persisted at the
  idle boundary and then requests the existing controlled restart, so the new
  process alone opens the selected pipeline. Follow-up review accepted both
  dispositions with no remaining findings.

- 2026-09-16: Qualified-Dot device-lab session
  `20260916T160204Z-51df06eb0f` passed explicit `/usr/bin/adb` and
  `G090LF0964060EHP` preflight, prepare, cleanup, and `verify-clean`; the
  installed-agent digest was unchanged. An initial cleanup before `prepare`
  failed because the runner's cleanup payload requires the prepared initial
  state; resuming through `prepare` restored its normal cleanup path. No new
  agent was installed, no task acoustic/voice trial was run, and no raw audio
  was retained. This is setup/cleanup evidence only, not Task 11 acceptance.

- 2026-09-16: Prepared the Task 11 operator payload and documentation; it
  stages the diagnostic agent under the token-owned root with isolated config
  and pairing state, selects the provisional profile, records only a sanitized
  schedule/summary, and bounds shutdown after TERM. The runner now performs
  cleanup, `verify-clean`, and launcher restart even if the host receives
  `KeyboardInterrupt`. `sh -n tools/device-lab/payloads/task11_command_audio.sh`,
  `uv run --no-project --script tools/device-lab/device_lab_test.py` (27 tests),
  `git diff --check`, and `make fmt-check` passed. Fresh review findings about
  pairing isolation, interrupt cleanup, unbounded agent shutdown, WAV
  deletion verification, and unsafe gateway deployment offers were **fixed**:
  diagnostics use `--disable-updates`, omit `update.single.v1`, and have no
  deployment handler. Two qualification prerequisites remain
  **postponed**: a host-visible real-time phase cue, and an attributable
  conditioning/capture-health metric collector. No hardware trial was run, and
  Task 11 remains in progress. The follow-up fresh-context review accepted the
  update-offer remediation with no remaining safety or documentation finding.

- 2026-09-15: The approved EchoLocal conditioning-baseline amendment changes
  Task 9 onward. Task 9 now owns copied/adapted, attributable and deterministic
  mixing/beamforming primitives plus capture sanity checks; Task 10 owns the
  physical silence/noise/speech position matrix and objective selection among
  channel 0, unsteered mix, and the adapted beamformer; Task 11 remains the
  selected-profile wake/endpointing integration proof. This does not select
  beamforming or weaken any clipping, cadence, wake, endpointing, privacy, or
  device-local voice-boundary criterion. The accepted design is
  amendment recorded in this plan's Task 9--11 text.

- 2026-09-15: Task 9 claimed by Codex. Scope is `echoctl mic` scorecard
  diagnostics, its host tests, and the physical-Dot evidence record. The task
  uses the qualified Dot only through a device-lab preflight/prepare/cleanup
  session with the explicit Linux ADB path and serial already recorded by Task
  8. Raw voice capture retention remains opt-in; absent operator approval, the
  session retains only derived JSON metrics and deletes each capture after it
  is scored.

- 2026-09-15: Task 9 is blocked before capture. The qualified Linux ADB device
  `G090LF0964060EHP` was attached and device-lab session
  `20260915T132015Z-581b348683` passed its explicit ADB and root preflight
  phases, but its read-only initial-state probe did not complete. Consequently
  `cleanup` could not run its unstaged payload and `verify-clean` correctly
  rejected the session because no initial state exists. No microphone was
  opened, no LED/GPIO state was changed, no audio was retained, and no product
  files were written. Completing the required normal/quiet/loud speech at
  front, off-axis, and far-field positions also requires an operator to supply
  speech and recorded distances. Resume only after investigating the runner
  probe/owned session and arranging that acoustic setup; do not infer channel
  geometry or fabricate its metrics.

- 2026-09-15: With operator authorization, Task 9 stopped the legacy launcher
  process and its child only (no hook or installed-agent file was changed),
  then repeated preflight and prepared session
  `20260915T132015Z-581b348683`. It verified the capture was idle,
  `boot_animation=0`, GPIO 444=`0`, and `ledcontroller=stopped`. An attempted
  front capture was discarded immediately: `echoctl mic record --channels all`
  incorrectly included reference channels 7--8, so it cannot be evidence for
  this task. `all` now means mic0--mic6 and its host tests pass. Cleanup and
  verify-clean passed with the installed-agent digest unchanged; restarting
  `echod` reasserted GPIO 444 low, so it was manually restored to the observed
  pre-session high value. No raw voice audio was retained. The task remains
  blocked pending the scorecard/JSON diagnostics and a complete repeated
  operator-run position matrix.

- 2026-09-15: Task 9 host diagnostic implementation is in progress; hardware
  qualification remains blocked. `echoctl mic scorecard` now accepts only an
  exactly-seven-channel WAV and emits local JSON containing capture format and
  frames; per-channel peak/RMS, clipping, mic0 correlation, relative delay and
  derived polarity; optional matched room-noise floor and speech/noise
  separation; position/distance/condition; verified XRun/dropped-frame values
  when a recorder-produced health sidecar matches the capture; and the
  raw-audio retained/deleted disposition. Raw deletion is the default;
  `--retain-input` is explicit operator-approved retention. The runbook records
  the front/off-axis/
  far-field × silence/noise/normal/quiet/loud matrix and requires observed
  distances. Host verification passed: `go test -race ./cmd/echoctl/...`,
  `make fmt-check`, `make lint`, `make build-device-ctl`, and `git diff
  --check`. The matrix still needs an operator speaking at those measured
  positions inside a prepared device-lab session; no raw voice audio has been
  retained by this implementation work.

- 2026-09-15: Fresh-context review found five Task 9 host defects, all fixed:
  silent channels now serialize dBFS as JSON `null`; deletion is default and
  scorecard publication occurs only after deletion succeeds; capture-health
  values require a SHA-256-bound `mic record --health-out` sidecar rather than
  being fabricated as zero; correlation uses a fixed overlap and deterministic
  lag tie-break; and its known-delay, periodic, inverted, and silent cases are
  covered. The review also noted that Task 9 needs operator-approved retained
  simultaneous recordings to create the Task 10 channel-0/mix/delay-and-sum
  offline comparisons; no profile implementation was pulled into Task 9.
  Reverification passed `go test -race ./cmd/echoctl/...`, `make fmt-check`,
  `make lint`, `make build-device-ctl`, `make test`, and `git diff --check`.

- 2026-09-15: Task 9 now includes the self-contained EchoLocal delay-and-sum
  baseline adapted from upstream revision
  `1e12085abd91edbf0e8d2e3501d703d006357c08` (`internal/hardware/mic/beam.go`).
  It accepts exactly seven equally sized physical-microphone frames and rejects
  malformed or 7--8-loopback-inclusive input before mutating state. Deterministic
  host coverage exercises impulse/fixed-delay/polarity determinism, channel
  exclusion, silence, PCM saturation, and the portable `noasm` build. This is
  an attributable candidate only: it does not select the upstream geometry,
  channel order, profile, or gain. Task 9 remains blocked on the existing
  operator-run seven-channel capture scorecard and acoustic matrix; Task 10
  owns profile selection and qualification.

- 2026-09-15: Fresh-context Task 9 review dispositions: **fix** five findings.
  `mic record` now rejects premature EOF; a health sidecar binds SHA-256,
  sample rate, exact frame count, and channel map `[0,1,2,3,4,5,6]`; and the
  scorecard rejects empty input and labels absent health `unverified`. The
  capture seam now recognizes a `MultichannelPreprocessor`, routes exactly
  mic0--mic6 before mono reduction, and rejects any other channel map. Tests
  cover that routing plus default candidate behavior, deterministic delay and
  polarity, silence, saturation, and `noasm`; no profile is selected. The
  stale reference to a missing design record was replaced with this plan's
  recorded amendment. Reverification passed focused race/noasm tests,
  `make fmt-check`, `make lint`, `make verify`, and `git diff --check`.

- 2026-09-15: Qualified-Dot session `20260915T155819Z-fdb383acf2` passed
  preflight, prepare, cleanup, and verify-clean with `/usr/bin/adb` and serial
  `G090LF0964060EHP`. A front, 550 mm, nominal normal-speech scorecard verified
  a 16 kHz S16_LE, seven-channel, 160,000-frame capture with zero XRuns and
  dropped frames; raw WAVs were deleted and only the derived JSON was retained
  in the token-owned session evidence. It is not speech-qualification evidence:
  the measured speech/noise separation was -0.75 to -0.81 dB across channels,
  indicating that normal speech was not present during the capture window.
  Repeat the controlled pair with speech synchronized to the ten-second
  speech capture before using any acoustic metric. Task 9 remains blocked on
  that matrix.

- 2026-09-15: Repeated the front, 550 mm normal-speech cell in qualified-Dot
  session `20260915T160243Z-f0237983d4`, using the semantic cyan-green
  `listening` ring during the human speech window. The local scorecard verified
  16 kHz S16_LE, seven channels, 160,000 frames, zero XRuns/dropped frames,
  zero clipping, normal polarity for mic1--mic6, and relative delays of
  0, -1, -2, -2, -1, and -1 samples against mic0. The per-channel
  speech/noise separation was 17.50--20.01 dB. Raw WAVs were deleted after
  local scoring; only the JSON metric artifact remains under the token-owned
  host evidence directory. This completes one normal-speech position cell,
  not Task 9's required silence/noise/quiet/loud and off-axis/far-field matrix.

- 2026-09-15: Qualified-Dot device-lab session `20260915T160603Z-314c638f2b`
  completed the front, 550 mm deliberate-silence cell with the semantic
  cyan-green `listening` ring active. The local verified scorecard recorded a
  16 kHz S16_LE seven-channel, 160,000-frame capture with zero XRuns, dropped
  frames, and clipping; per-channel RMS was -63.84 to -60.07 dBFS. A
  speech/noise-separation figure does not apply to a silence-only capture. The
  raw WAV was confirmed deleted after local scoring, while the derived JSON
  remains in the session's token-owned host evidence directory. Preflight,
  prepare, cleanup, and verify-clean all passed; the installed-agent digest was
  unchanged. Quiet and loud speech plus off-axis/far-field matrix cells remain.

- 2026-09-15: Qualified-Dot device-lab session `20260915T161003Z-09519438e1`
  completed the front, 550 mm continuous-quiet-speech cell. A matched room-noise
  capture immediately preceded the ten-second speech capture, whose semantic
  cyan-green `listening` ring was active. The verified 16 kHz S16_LE,
  seven-channel, 160,000-frame scorecard recorded zero XRuns, dropped frames,
  and clipping, with per-channel speech/noise separation of 3.15--3.23 dB. The
  raw quiet-speech WAV was confirmed deleted after scoring and cleanup removed
  the temporary noise WAV; only derived JSON metrics remain in token-owned host
  evidence. Preflight, prepare, cleanup, and verify-clean passed with the
  installed-agent digest unchanged. Loud speech and off-axis/far-field cells
  remain.

- 2026-09-15: Qualified-Dot device-lab session `20260915T161447Z-3e028ea061`
  completed the front, 550 mm loud-speech cell. A matched room-noise capture
  immediately preceded the ten-second speech capture, whose semantic
  cyan-green `listening` ring was active. The verified 16 kHz S16_LE,
  seven-channel, 160,000-frame scorecard recorded zero XRuns, dropped frames,
  and clipping, with per-channel speech/noise separation of 10.87--12.95 dB.
  The raw loud-speech WAV was confirmed deleted after scoring and cleanup
  removed the temporary noise WAV; only derived JSON metrics remain in
  token-owned host evidence. Preflight, prepare, cleanup, and verify-clean
  passed with the installed-agent digest unchanged. The front 550 mm silence,
  normal, quiet, and loud cells are now complete; off-axis and far-field cells
  remain.

- 2026-09-15: Qualified-Dot device-lab session `20260915T161902Z-8e723ac1dc`
  completed the off-axis, 550 mm normal-speech cell. A matched room-noise
  capture immediately preceded the ten-second speech capture, whose semantic
  cyan-green `listening` ring was active. The verified 16 kHz S16_LE,
  seven-channel, 160,000-frame scorecard recorded zero XRuns, dropped frames,
  and clipping, with per-channel speech/noise separation of 12.39--15.33 dB.
  The raw normal-speech WAV was confirmed deleted after scoring and cleanup
  removed the temporary noise WAV; only derived JSON metrics remain in
  token-owned host evidence. Preflight, prepare, cleanup, and verify-clean
  passed with the installed-agent digest unchanged. Further off-axis conditions
  and all far-field conditions remain.

- 2026-09-15: Qualified-Dot device-lab session `20260915T162419Z-cc45191739`
  completed the far-field normal-speech scorecard at the operator-supplied
  measured distance of 1,000 mm. A matched room-noise capture immediately
  preceded the ten-second speech capture, whose semantic cyan-green `listening`
  ring was active. The verified 16 kHz S16_LE, seven-channel, 160,000-frame
  scorecard recorded zero XRuns, dropped frames, and clipping, with per-channel
  speech/noise separation of 11.34--14.31 dB. The raw normal-speech WAV was
  confirmed deleted after scoring and cleanup removed the temporary noise WAV;
  only derived JSON metrics remain in token-owned host evidence. Preflight,
  prepare, cleanup, and verify-clean passed with the installed-agent digest
  unchanged. This satisfies Task 9's physical-capture scorecard verification;
  Task 10 remains responsible for completing the full position/condition matrix
  and selecting a profile.

- 2026-09-14: Task 6 completed. `echod` now accepts typed offers only at an
  idle turn boundary, blocks new local turns while installation is active, and
  performs the installer work independently of the WSS reader. It reports
  decision/progress/failure/cancellation transitions, binds HTTPS fetches to
  the authenticated WSS gateway authority/TLS/bearer settings, and exits 75
  after the `restarting` control message drains (or immediately after a
  disconnect). Installed metadata supplies diagnostic hello fields and a
  reconnect confirmation is only cleared after socket delivery. Reconciliation
  never selects an executable. Fresh-context findings were all **fixed**:
  pending-turn races, stale metadata confirmation, confirmation delivery,
  post-commit restart behavior, fixed deployment paths, cancellation direction,
  and update state. No hardware was used; `make verify` and the task's
  repeated race command passed.

- 2026-09-14: Task 8 was claimed for live qualification. Host ARM64 builds
  (`make build-device` and `make build-device-ctl`) passed, but the explicitly
  selected Windows ADB at `/mnt/c/tools/android-platform-tools/adb.exe`
  reported no attached devices for qualified serial `G090LF0964060EHP`.
  Required device-lab preflight with those explicit inputs consequently failed
  with `device 'G090LF0964060EHP' not found`; it created no device session or
  remote diagnostic state.
  Therefore no launcher/bootstrap write, reboot, gateway deployment,
  pre-commit failure attempt, downgrade, bad-build installation, or ADB
  recovery action was performed. The task remains blocked pending a rooted,
  connected qualified Dot, a narrow authenticated test gateway, signed test
  releases (including a known-good recovery bundle), and the operator's
  explicit acknowledgement before the intentional immediate-exit build.

- 2026-09-14: Linux ADB subsequently connected to the rooted qualified Dot.
  After replacing a device-lab capability probe's unavailable system `printf`
  with the qualified BusyBox applet, session
  `20260914T110511Z-a6f7fa0ac1` passed preflight, prepare, cleanup, and
  verify-clean with the initial installed digest preserved. The first session
  preceded mandatory LED/mic preparation and is not accepted as Task 8
  evidence; the repeated prepared session `20260914T110945Z-258d9a87f4` passed
  with `boot_animation=0`, GPIO 444=`0`, and `ledcontroller=stopped`. No
  product path was exercised. Task 8 remains blocked on a narrow authenticated
  offer gateway, signed current/older/failure/recovery bundles, and the
  operator's explicit acknowledgement immediately before the intentional
  immediate-exit deployment. The present production gateway does not issue
  `update.offer`, and dotsim's offer server is in-process test code rather than
  an operable live gateway.

- 2026-09-14: With Linux ADB and the operator-run gateway available, Task 8
  backed up the original installed agent to host recovery storage and verified
  its SHA-256 as `41ed22dba37da3d583c257991e3b4d69460fc07265010f189594a4d38184f8c6`.
  It used `echoctl update bootstrap` to install the qualified launcher and the
  current ARM64 agent, then requested a reboot. The Dot did not re-enumerate
  on Linux ADB during bounded post-reboot polling. This is a post-commit
  unavailability observation, not automatic rollback; no recovery command can
  be issued until physical/ADB access returns. The device-lab session remains
  unclean because its pre-bootstrap digest baseline intentionally differs from
  the newly installed agent; do not delete or steal its token-owned root until
  the device reconnects and its ownership can be verified. The signed recovery
  bundle and offer-gateway prerequisites remain outstanding.

- 2026-09-15: Task 8 resumed against Linux ADB `/usr/bin/adb` and qualified
  serial `G090LF0964060EHP`, using the existing prepared device-lab session
  `20260914T122202Z-8ee8b4ed9d`. A read-only bootstrap probe found the stale
  launcher SHA-256 `0d7d385324c9005de65ddfd25d64f39b30b5316f4360c3a9b907b945267da8c1`.
  The qualified BusyBox `cmp`, with and without `-s`, returned status zero for
  distinct payloads, so bootstrap falsely reported the existing launcher as
  unchanged. A first SHA-256 comparison remediation also failed closed
  incorrectly because FireOS lacked system `printf`; the command-substitution
  failure compared two empty strings. Bootstrap now qualifies `sha256sum`,
  `printf`, and `sed` through `/data/adb/magisk/busybox`, with host regression
  coverage. After `go test -race ./cmd/echoctl/...` and `make build`, bootstrap
  replaced the hook with SHA-256
  `5adadfad2eb20bb647fb8f63c6f9e1762a410a02a81bc28adc6a46719f35df9c`, preserved
  the prior hook at `echo-satellite.sh.echo-satellite-backup`, removed its
  staged files, and left the root-owned mode-0600 provisioned INI untouched.
  Reboot completed and, at 24 seconds uptime, PID 339 was
  `/data/local/bin/echod --config /data/local/etc/echo-satellite/echod.ini`;
  its executable link and installed digest matched the bootstrapped agent.
  Thus the bootstrap-replacement and reboot-startup blockers are resolved.
  The pre-bootstrap device-lab cleanup correctly refused to erase its root
  when it observed the intentional agent-digest change; after a token and
  process-ownership inspection established no leaked diagnostic process, only
  that token-owned root and its matching stale host lock were removed. This is
  an explicit cleanup deviation, not a product recovery. Task 8 remains
  blocked on an operable authenticated offer gateway, signed current/older,
  tampered/truncated/insufficient-space, and recovery bundles, plus explicit
  operator acknowledgement immediately before installing the intentional
  immediate-exit diagnostic build.

- 2026-09-15: Task 8 remediation added the disposable `task8gateway` command:
  one TLS listener authenticates both WSS and artifact requests with the
  existing device bearer token, serves one explicit manifest-derived offer,
  supports the three controlled corrupted-response modes, and records only the
  selected deployment's payload-correlated update events. It deliberately does
  not sign releases, retain release state, or change the normal `gateway`.
  Unit coverage includes empty-envelope reports over a controlled-restart
  reconnect with no duplicate offer. Fresh-context review findings were all
  **fixed**: artifact authentication, post-write offer consumption, payload and
  confirmation identity correlation, temporary explicit gateway routing
  documentation, CLI precedence/validation coverage, test validity, and file
  modes. Host scoped race tests, repository tests, formatting, lint, direct
  Windows cross-build of `task8gateway.exe`, and diff checks passed. `make
  build-windows` remains unverified because the operator's running
  `.bin/gateway.exe` holds its output file; it must be rerun after that process
  stops. Task 8 remains blocked on the operator-supplied signed current/older/
  failure/recovery bundles and the explicit just-in-time acknowledgement for
  the immediate-exit drill.

- 2026-09-15: The missing production signing workflow makes Task 8's live
  signed-release acceptance criteria impossible to execute within Milestone 3.
  Task 8 is therefore **superseded**, not completed, by future plan
  `2026-09-15-release-signing-and-deployment-qualification.md`: its Task 1
  establishes secure signing and its Task 2 owns complete rooted-Dot proof.
  The completed disposable offer-gateway remediation remains Milestone 3
  evidence. Milestone 3 retains no claim that signed deployment, downgrade,
  failure safety, ADB recovery, or partition-write proof ran on hardware.

- 2026-09-15: An interrupted workspace change temporarily removed the
  uncommitted offer-harness source while leaving its prior build artifacts.
  The exact Git object content was restored, and scoped race, formatting, lint,
  direct Windows cross-build, and diff checks passed again. The remediation is
  therefore completed; only the deferred signing and physical qualification
  remain in the successor plan.

- 2026-09-10: Task 5 claimed by Codex. Scope is limited to the launcher,
  `echoctl` bootstrap/install/status commands, build wiring, and operator
  documentation; Task 6 retains connected-agent controlled-restart behavior.
- 2026-09-10: Task 5 completed. Added the qualified-Magisk launcher,
  fail-closed host ADB bootstrap, on-device signed local installation and
  diagnostic status commands, payload build validation, and recovery guidance.
  Fresh-context review findings were all **fixed**: hooks are staged/backed up
  before replacement, foreign hooks fail closed, metadata directory setup is
  pre-commit, and fake-ADB shell tests now exercise fresh, idempotent, backup,
  interruption, and conflict scenarios. No findings were declined or postponed.
- 2026-09-10: Task 5 hardware remediation completed. The isolated bootstrap
  check found that FireOS `/system/bin/cmp` rejects `-s`; bootstrap now uses
  Magisk BusyBox. The session proved unsigned rejection preserves the digest,
  explicit unsigned same-byte installation writes metadata, and same-byte
  bootstrap removes staging while preserving the agent digest. It restored the
  pre-test agent and removed the test hook/metadata; device-lab cleanup and
  verify-clean passed. This is partial Task 8 evidence only: no reboot,
  launcher execution, signed bundle, or reconnect test was run.

- 2026-09-08: Task 2 completed on rooted Dot `G090LF0964060EHP`. `/data` ext4
  supported executable-mode enforcement, staged-file and directory fsync,
  atomic same-directory replacement, old-inode execution until exit, and
  reboot persistence. The planned staging margin remained adequate.
- 2026-09-08: Hardware invalidated the assumed modern Magisk hook location.
  Magisk v17.3 ignored `/data/adb/service.d` and executed the root-owned legacy
  hook from `/sbin/.core/img/.core/service.d` at 7.19 seconds uptime. The plan
  and design now require version-qualified service-directory detection.
- 2026-09-08: Twenty current-agent launches all reached authenticated `welcome`
  in 630.968–810.274 ms (median 649.703 ms). Exit 75 produced an immediate
  approximately 20 ms restart; unexpected exit 1 honored the full bounded
  1/2/4/8/16/32/60-second backoff. The hook and isolated files were removed,
  FireOS services restored, and the installed agent digest remained unchanged.
- 2026-09-08: Codex claimed Task 2 and began the rooted-Dot launcher and
  filesystem qualification. The active `echod` path is explicitly excluded
  from diagnostic writes; all test payloads use
  `/data/local/tmp/echo-satellite-m3-diagnostic`.
- 2026-09-08: Plan created. The original A/B supervisor architecture was
  rejected before implementation in favor of signed single-agent replacement,
  a minimal Magisk launcher, gateway redeployment when the client works, and ADB
  recovery otherwise.
- 2026-09-08: Recovery-first sequencing and the full command-audio conditioning
  scope were retained.
- 2026-09-08: Task 1 started. Claimed by Codex; no active-plan scope conflict
  was present. The repository Git metadata was read-only, so `git mv` could not
  create its index lock; the tracked plan was moved to `in-progress/` with a
  filesystem move instead.
- 2026-09-08: Task 1 replaced the live multi-copy recovery architecture in
  `docs/DESIGN.md` and `AGENTS.md`, added the single-agent transition and
  architectural flow to `docs/protocol.md`, and left obsolete wire identifiers
  explicitly labeled as a superseded snapshot for Task 3.
- 2026-09-08: Fresh-context review findings were triaged as **fix**: corrected a
  pre-existing gateway/device endpointing contradiction, narrowed legacy
  protocol labeling to obsolete elements, distinguished supervisor fields from
  the launcher, recorded historical-plan supersession, and made the deployment
  audit trail mandatory. Follow-up review confirmed those fixes and identified
  two wording ambiguities; both were also fixed. No findings were declined or
  postponed.
- 2026-09-08: Task 1 completed after the exact terminology check, a broader
  obsolete-term sweep, `git diff --check`, the required pre-review checks, and
  final `make verify` all passed. No hardware was required or used.
- 2026-09-10: Task 4 completed. Added a host-tested transactional installer
  with paired-authority HTTPS downloads, strict signature/eligibility/offer
  checks, Task-2 free-space margin, unique same-directory staging, atomic
  replacement, diagnostic-only metadata reconciliation, and explicit
  pre-/post-commit results. Fresh-context review findings were all triaged as
  **fix** and resolved; none were declined or postponed. `make verify` and the
  task's repeated update/release race test passed; no hardware was used.
- 2026-09-10: Task 4 targeted re-review found no residual transactional or
  security defect. The claimed trailing-JSON parsing issue was **declined**:
  `ParseMetadata` rejects any second JSON value, as its passing `null` trailing
  document test demonstrates. Startup invocation of the exposed reconciliation
  API is **postponed** to Task 6, which owns the `echod` composition root; Task
  6 now names the required diagnostic-only behavior explicitly.

## Completion evidence

- Task 8 disposable offer-gateway remediation — completed 2026-09-15:
  `go test -race ./cmd/task8gateway/...`, `make test`, `make fmt-check`, `make
  lint`, `git diff --check`, and direct `GOOS=windows GOARCH=amd64 go build`
  for `.bin/task8gateway.exe` passed. The scoped command reached 53.2% total
  package coverage in the fresh repository test run; all new exported surface
  is command-local. `make build-windows` could not complete because its existing
  `gateway.exe` output was locked by the operator-run gateway before the new
  command target. No hardware was used. Fresh-context review found and all
  fixes were applied for update-result correlation, artifact authentication,
  reachable authority configuration, test coverage, and source modes.

- Task 5 host verification — passed 2026-09-10: `go test -race
  ./cmd/echoctl/...`, `make build-device-ctl`, and `make check-portability`
  passed. `make verify` passed formatting, lint (0 issues), fresh race tests,
  coverage generation (70.2% total), and host builds. Fake-ADB tests execute
  the generated shell script in a temporary filesystem for fresh installation,
  idempotence, recognized-hook backup, interruption, and unrecognized-conflict
  preservation. Hardware proof remains Task 8 and was not run.
- Task 5 isolated hardware remediation — passed 2026-09-10: device-lab session
  `20260910T103421Z-191d2c5599` preflight/prepare passed; a host backup matched
  the initial `41ed22dba37da3d583c257991e3b4d69460fc07265010f189594a4d38184f8c6`
  digest. Bootstrap installed the legacy hook and ARM64 agent; unsigned local
  install rejected by default with its digest preserved, then explicitly
  accepted identical bytes and recorded metadata. The final same-byte bootstrap
  check passed after switching to Magisk BusyBox `cmp -s`. The original agent,
  no project hook, and no test metadata were restored; device-lab cleanup and
  verify-clean passed. No reboot, launcher start, signed install, or gateway
  reconnect was performed.

- Task 4 host verification — passed 2026-09-10: `go test -race -count=20
  ./internal/device/update/... ./internal/release/...` passed. `make verify`
  passed formatting, lint (0 issues), fresh race tests, coverage generation,
  and all host builds. Fresh-context review findings covering fail-closed
  architecture eligibility, free-space margin, cancellation boundary,
  credential-safe downloader errors, strict metadata reconciliation, and
  staging cleanup were fixed. Hardware verification is not applicable.

- Task 2 rooted-Dot filesystem and launcher qualification — passed 2026-09-08;
  `docs/device-diagnostics.md` records filesystem/mount/inode observations,
  executable permissions, file/directory fsync, atomic rename, reboot
  persistence, boot-hook environment, controlled restart and full crash
  backoff measurements.
- Task 2 authenticated startup timing — passed 2026-09-08; 20/20 current-agent
  launches completed authenticated `hello`/`welcome`, with 630.968 ms minimum,
  649.703 ms median, 744.369 ms nearest-rank p95 and 810.274 ms maximum.
- Task 2 cleanup — passed 2026-09-08; diagnostic hooks and
  `/data/local/tmp/echo-satellite-m3-diagnostic` were absent, no agent remained,
  `ledcontroller` and `mdnsd` were running, and `/data/local/bin/echod` retained
  SHA-256 `41ed22dba37da3d583c257991e3b4d69460fc07265010f189594a4d38184f8c6`,
  matching the host backup.
- Task 2 repository verification — `make fmt-check`, `make lint`, `make test`,
  `git diff --check`, and `make verify` passed 2026-09-08. The first sandboxed
  `make test` attempt could not bind loopback `httptest` listeners; its
  permission-enabled rerun passed with race detection and 70.3% total coverage.
- Task 2 fresh-context review and targeted re-reviews — completed 2026-09-08.
  All findings were fixed: hardware verification now contains the exact command
  and harness record, staging paths agree, the timing claim stops at the proven
  authenticated `welcome`, every installed hook is included in cleanup, and
  post-Task-2 repository checks are recorded. No findings were declined or
  postponed; the final reviewer reported no residual defect.
- Task 1 exact `rg -n "A/B|inactive slot|trial health|automatic rollback|supervisor_min|update\\.ab" AGENTS.md docs/DESIGN.md docs/protocol.md` — passed
  2026-09-08; its three matches are only `update.ab` and `supervisor_min` in
  `docs/protocol.md`'s explicitly labeled legacy/superseded wire snapshot,
  retained until Task 3 changes the Go wire types and documentation together.
- Supplemental case-insensitive scan for slot names/fields, pre-commit phases,
  device-local recovery phases, supervisor fields and capability variants —
  passed 2026-09-08; all matches are confined to that same labeled legacy wire
  snapshot.
- `make fmt-check` — passed 2026-09-08.
- `make lint` — passed 2026-09-08 with 0 issues.
- `make test` — passed 2026-09-08 with race detection and 70.3% total coverage.
- `git diff --check` — passed 2026-09-08.
- `make verify` — passed 2026-09-08; formatting, lint, fresh race tests and all
  four host builds succeeded.
- Fresh-context review and targeted re-review — completed 2026-09-08; all five
  original findings and both follow-up wording findings were fixed, with none
  declined or postponed.
- Hardware verification — not applicable to Task 1; no hardware was used.

Remaining plan tasks require later implementation and hardware sessions, so the
plan remains `in-progress`.

- 2026-09-16: Ran the prepared front/550 mm Task 10 batch in qualified-Dot
  device-lab session `20260916T145631Z-0233ef1d7f`. All seven capture windows
  completed (silence; fresh noise plus normal, quiet, and loud speech), with
  canonical 16 kHz S16_LE seven-channel 160,000-frame sidecars, zero XRuns and
  dropped frames, zero clipping, and a worst 4.77 ms processing block. Minimum
  candidate speech/noise separations were 6.09 dB channel-0, 6.74 dB
  unsteered-mix, and 6.91 dB delay-and-sum; because all are within 1 dB at this
  one position, this is not a selection result. Raw audio was deleted by the
  scorecard/comparison operations. Cleanup and `verify-clean` passed with the
  installed-agent digest unchanged; the launcher was restarted. Before this
  successful session, `20260916T143244Z-29058e2294` was cleaned after the
  FireOS shell lacked standalone `printf`; the payload now uses the preflight-
  qualified Magisk BusyBox `printf`. The subsequent session was also cleaned
  and verified before this restart. Off-axis/far-field repetition, 20 wake
  trials, and endpointing trials remain, so Task 10 stays in progress and
  `bypass-v1` remains the only defensible profile.

- 2026-09-16: Prepared, but did not execute, the first Task 10 front/550 mm
  batch. The token-owned device-lab payload captures silence; fresh matched
  room-noise and normal, quiet, and loud speech pairs; displays three-second
  `thinking` cues and a ready-led cyan-green `listening` window; writes the
  silence scorecard and three health-bound comparisons; and deletes raw WAVs
  through the existing scorecard/comparison defaults. The historic distance
  transcription was corrected to the operator-confirmed 550 mm throughout
  the Task 10 evidence. Host checks passed: `sh -n
  tools/device-lab/payloads/task10_front_550mm.sh`, `uv run --no-project
  --script tools/device-lab/device_lab_test.py`, and `git diff --check`.
  Fresh-context review identified three defects, all **fixed**: direct ADB
  execution now derives the token-owned root from the payload pathname; the
  silence sidecar must prove zero XRuns/dropped frames; and listening receives
  a one-second visual ready lead. The review agent exhausted its service quota
  before sending a final completion report; no finding was declined or
  postponed. Hardware qualification remains unrun and Task 10 remains in
  progress with `bypass-v1` the only defensible profile.

- 2026-09-15: Task 10 host implementation is in progress. Added deterministic
  channel-0, polarity-qualified unsteered-mix, and EchoLocal delay-and-sum
  candidate processors behind `Preprocessor`, each with bounded 12 dB output
  leveling, a 1 dBFS ceiling, saturation-safe conversion, and controlled
  gain transitions. `echoctl mic compare` now emits per-candidate profile,
  applied gain, peak/RMS, clipping count/fraction, noise level,
  speech/noise separation, and processing duration from simultaneous
  seven-channel captures, deleting source recordings unless explicitly
  retained. Final selection remains blocked on the required controlled
  real-Dot capture and wake/endpointing trials; no candidate is published as
  `dot-gen2-qualified-v1` from host fixtures alone.
  Fresh-context review found five issues, all fixed: comparison now processes
  fixed 80 ms blocks; it calculates speech/noise separation from raw candidate
  reduction before independent output leveling; reports position, distance,
  condition, and validated input/noise capture health; rejects XRuns/dropped
  frames and non-canonical capture; and has streaming/cadence, gain, clipping,
  SNR, and candidate tests. No findings were declined or postponed.

- 2026-09-14: Task 7 completed. `dotsim` now uses the production single-agent
  installer against simulator-owned files and exposes deterministic controls
  for interrupted download, invalid signature, digest mismatch, insufficient
  space, restart failure, and no replacement reconnect. It reports accepted,
  download (0% and 50%), verification, staging, restart, cancellation, failure
  and confirmed-reconnect states; a controlled restart rebuilds the simulated
  process from persisted installation metadata. Signed downgrade is a fresh
  deployment. Scoped verification `go test -race -count=20 ./cmd/dotsim/...
  ./internal/protocol/...`, plus `make fmt-check`, `make lint`, and `make test`,
  all passed. Fresh review findings about restart, metadata, reconnect, and
  progress were fixed. The reviewer also recommended a future end-to-end
  authenticated WebSocket harness test; direct simulator lifecycle tests use an
  authenticated in-process release server and existing client protocol tests
  cover envelope routing, so this non-correctness coverage expansion is
  postponed to Milestone 4 gateway-harness work.

- 2026-09-09: Task 3 completed. Removed `supervisor_min` and supervisor
  eligibility, regenerated signed release fixtures, replaced the A/B capability
  with `update.single.v1`, and replaced legacy update messages with strict
  deployment-scoped offer, decision, progress, confirmation, cancellation and
  failure payloads. The wire configuration now carries an exact named audio
  conditioning profile (`bypass-v1` or `dot-gen2-qualified-v1`). Scoped race
  tests, fixture regeneration, formatting and lint passed. A broad `go test
  ./...` compiled the touched callers but could not run pre-existing TLS tests
  because this sandbox disallows loopback listeners.
