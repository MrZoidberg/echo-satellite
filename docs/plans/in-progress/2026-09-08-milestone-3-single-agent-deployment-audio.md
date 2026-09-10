# Milestone 3 — Single-agent deployment and command-audio conditioning

**Status:** in-progress
**Owner or active agent:** Codex (Tasks 1–5 completed)
**Created:** 2026-09-08
**Updated:** 2026-09-10
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

### Task 6: Integrate deployment with `echod`

**Status:** not started

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

**Status:** not started

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

**Status:** not started

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

**Expected outcome:** Normal replacement, downgrade, pre-commit safety, and the
manual ADB recovery boundary are proven on the target device.

**Verification:**

```sh
make build-device
make build-device-ctl
"$ADB" -s "$DEVICE_SERIAL" shell "su -c '
  readlink /proc/\$(pidof echod)/exe
  sha256sum /data/local/bin/echod
  cat /data/local/etc/echo-satellite/installed-release.json
'"
```

Expected: valid replacement and signed downgrade reconnect; all pre-commit
failures preserve the prior digest; the post-commit bad build requires and
successfully completes documented ADB recovery.

### Task 9: Characterize the seven physical microphone channels

**Status:** not started

**Purpose:** Establish evidence for preprocessing selection using identical
multichannel input.

**Dependencies:** Task 8, so subsequent device iterations use the new deployment
path.

**Hardware required:** yes — qualified Dot with Amazon LED ownership disabled
and GPIO 444 low.

**Files or components:**

- Extend `echoctl mic` diagnostics.
- Modify `docs/device-diagnostics.md`.
- Add operator-approved fixtures only when necessary.

**Concrete changes:**

- Capture all seven physical microphone channels simultaneously; continue
  excluding playback loopback channels 7–8.
- Record controlled silence, steady room noise, normal speech, continuous quiet
  speech, and loud/clipping speech.
- Repeat speech from front, off-axis, and far-field positions at recorded
  distances.
- Measure per-channel polarity, relative delay/correlation, noise floor, RMS/peak
  dBFS, clipping fraction, and speech/noise separation.
- Produce comparable offline inputs for channel 0, an unsteered mix, and
  steerable delay-and-sum processing.
- Keep raw voice recordings only with operator approval; otherwise retain
  derived metrics and delete captures after analysis.

**Expected outcome:** A reproducible seven-channel scorecard identifies channel
behavior and beamformer inputs without guessing array properties.

**Verification:** `echoctl mic` JSON output and the diagnostic record contain all
seven physical channels, test positions, metrics, capture format, zero
XRuns/dropped frames, and raw-audio disposition.

### Task 10: Implement and select the conditioning profile

**Status:** not started

**Purpose:** Produce a bounded, observable preprocessing path chosen from
measured evidence.

**Dependencies:** Task 9.

**Hardware required:** host fixtures first; final selection uses the Dot
recordings from Task 9.

**Files or components:**

- Modify `internal/device/audio`.
- Extend `echoctl` comparison diagnostics.
- Add deterministic audio fixtures.

**Concrete changes:**

- Implement three processors behind the existing `Preprocessor` interface:
  channel 0, polarity-correct unsteered mix, and steerable delay-and-sum.
- Add bounded automatic gain/output leveling with maximum 12 dB gain, at least
  1 dBFS headroom, saturation-safe conversion, and controlled attack/release.
- Report profile name, applied gain, peak/RMS, clipping count/fraction, noise
  level, speech/noise separation, and processing duration.
- Add multichannel impulse, polarity, delay, silence, noise, quiet-speech,
  loud-speech, clipping, and gain-transition tests.
- Disqualify a candidate if it:
  - clips more than 0.1% of output samples;
  - causes an XRun or dropped capture frame;
  - misses the 80 ms processing cadence;
  - accepts fewer than 18 of 20 qualified wakes;
  - endpoints continuous quiet speech before 60 seconds;
  - fails deliberate-silence endpointing.
- Among passing candidates, choose the greatest repeatable improvement in the
  minimum speech/noise separation across measured positions. Treat differences
  below 1 dB as equivalent and prefer the simpler candidate in this order:
  channel 0, unsteered mix, delay-and-sum.
- Publish the winner as `dot-gen2-qualified-v1`. If none passes, retain
  `bypass-v1` and leave the milestone blocked.

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

**Status:** not started

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
- [ ] A valid signed deployment restarts and reconnects as the expected build.
- [ ] A signed older release can be deployed through the same path.
- [ ] Tampered, truncated, wrong-signature, and insufficient-space releases are
  rejected without changing the installed digest.
- [ ] A deliberately bad committed build is recoverable through the documented
  ADB procedure.
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

- 2026-09-09: Task 3 completed. Removed `supervisor_min` and supervisor
  eligibility, regenerated signed release fixtures, replaced the A/B capability
  with `update.single.v1`, and replaced legacy update messages with strict
  deployment-scoped offer, decision, progress, confirmation, cancellation and
  failure payloads. The wire configuration now carries an exact named audio
  conditioning profile (`bypass-v1` or `dot-gen2-qualified-v1`). Scoped race
  tests, fixture regeneration, formatting and lint passed. A broad `go test
  ./...` compiled the touched callers but could not run pre-existing TLS tests
  because this sandbox disallows loopback listeners.
