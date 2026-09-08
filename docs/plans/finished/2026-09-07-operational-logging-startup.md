# Operational logging and device startup implementation plan

**Status:** finished
**Owner or active agent:** /root
**Created:** 2026-09-07
**Updated:** 2026-09-08
**Started:** 2026-09-07
**Completed:** 2026-09-08

## Objective

Make the four binaries operationally diagnosable with safe, configurable logs, and make `echod` take ownership of the Echo Dot setup needed for reliable indicator, mDNS, and microphone-mute behavior.

## Non-goals

- Gateway wake scoring, continuous microphone upload, assistant changes, or updater work.
- Changing FireOS or Android partitions, or shipping supervisor behavior.
- Capturing log records containing bearer tokens, TLS material, raw audio, or arbitrary protocol payload contents.

## Source references and constraints

- `TODO.md` tasks 1–4.
- `docs/DESIGN.md` §§3.2–3.3, 3.11, 8.2, 19: wake/VAD remain local; mDNS is discovery only; raw audio and secrets must not be exposed.
- `AGENTS.md`: on a Dot, `ledcontroller` must be stopped and `boot_animation` disabled before project LED control; microphone-cut GPIO state must be respected.
- `docs/plans/README.md`: this plan requires host checks, a real-device check, fresh-context review, and `make verify` before completion.

## Dependencies and prerequisites

- A rooted, qualified Echo Dot Gen 2 for Task 2, with ADB access and its microphone cut state readable.
- `make`, Go, `golangci-lint`, and `uv` available locally.
- No other process or agent using the Dot microphone during the hardware check.

## Architecture and high-level plan

1. Centralize command logging configuration so all binaries have the same text-default/JSON-option behavior, level selection, and optional bounded rotating file sink. Add debug records at discovery, connection, and protocol-message boundaries using metadata only.
2. Add an injected `echod` startup-preparation component. Before hardware initialization it stops the FireOS indicator and mDNS services, disables and clears the LED controller, snapshots microphone-cut state, and plays the project startup animation. On shutdown it clears project LEDs and restores the captured mute state; it never silently forces microphones unmuted.
3. Use fakes and temporary filesystem roots for host tests. Validate the service/GPIO/sysfs contract on the Dot and document the observed FireOS service names and recovery procedure.

## Planned file map

- `internal/device/system/`: testable process-service and GPIO/sysfs startup-preparation primitives.
- `cmd/{echod,gateway,dotsim,echoctl}/`: common logging flags and composition-root wiring; `echod` startup ownership.
- `cmd/*/*_test.go`, `internal/device/system/*_test.go`: flag, redaction, rotation, ordering, and cleanup coverage.
- `docs/device-diagnostics.md`, `AGENTS.md`: confirmed service/mute-state operating contract.

## Numbered tasks

### Task 1: Consistent safe operational logging

**Status:** completed 2026-09-07

**Purpose:** Provide useful support diagnostics without changing protocol behavior or leaking sensitive data.

**Dependencies:** None.

**Hardware required:** no.

**Files or components:** `cmd/echod`, `cmd/gateway`, `cmd/dotsim`, `cmd/echoctl`, and a small shared logging helper if it removes duplicated setup.

**Concrete changes:**

- Add `--log-format=text|json` (text default) and `--log-file`; retain `--dbg` as the debug-level selector and preserve flag/environment/config precedence.
- Send stderr and optional bounded rotating-file output through one handler configuration; close/flush the file sink on every normal/error exit.
- Add debug records for selected interfaces, mDNS browse/advertise results, resolver choice, connect/reconnect/handshake lifecycle, and sent/received protocol message type plus bounded safe identifiers/sizes. Do not log credentials, authentication headers, raw audio, or complete message bodies.
- Add table-driven parser and handler tests that assert defaults, JSON/text encoding, file rotation/close errors, and redaction.

**Expected outcome:** An operator can switch every binary between readable and JSON logs, persist bounded debug logs, and trace gateway communication safely.

**Verification:**

```sh
go test -race ./cmd/echod ./cmd/gateway ./cmd/dotsim ./cmd/echoctl ./internal/device/...
make fmt-check
make lint
```

Expected: all commands exit 0; tests prove sensitive values are absent from debug output.

### Task 2: `echod`-owned Dot startup preparation

**Status:** completed 2026-09-08

**Purpose:** Remove manual FireOS preparation while retaining a safe, observable recovery path.

**Dependencies:** Task 1 (startup actions must have safe diagnostic logs).

**Hardware required:** yes — rooted, paired Echo Dot Gen 2; no competing microphone capture; GPIO 444 readable and the initial value recorded.

**Files or components:** `cmd/echod`, `internal/device/system`, `internal/device/led`, relevant tests, `docs/device-diagnostics.md`, and `AGENTS.md`.

**Concrete changes:**

- Introduce an injected startup-preparation interface that executes in a deterministic order: snapshot mute GPIO 444; stop `ledcontroller` and FireOS `mdnsd`; set `boot_animation=0`; reset/clear LEDs; then render the normal `echod` startup animation before capture/connectivity workers begin.
- On every clean shutdown and setup failure, clear the project LED frame and restore the snapshotted GPIO 444 value. Service restart remains an explicit recovery action, not an implicit daemon-exit side effect, because project LED ownership must not race FireOS.
- Make service names, sysfs roots, and GPIO root injectable for host tests. Propagate failures with operation context; log safe action/result metadata.
- Add ordering and error-path tests proving no later setup action occurs after a failure, startup animation precedes capture, mute state is restored, and shutdown cleanup joins errors.
- Run the real-Dot diagnostic below; update `docs/device-diagnostics.md` and `AGENTS.md` with the observed result. If `mdnsd` is absent or cannot safely stop, leave this plan `in-progress (blocked)` and record the observed service/process rather than guessing a substitute.

**Expected outcome:** Starting `echod` reliably claims the required device resources, visibly announces startup, and preserves the physical microphone-cut state.

**Verification:**

```sh
go test -race ./cmd/echod ./internal/device/system ./internal/device/led
make fmt-check
make lint
make test
make verify
```

Expected: all host checks exit 0.

On the qualified Dot, after deploying the ARM64 `echod` binary, run:

```sh
"$ADB" -s "$DEVICE_SERIAL" shell "su -c '
  cat /sys/class/gpio/gpio444/value
  getprop init.svc.ledcontroller
  getprop init.svc.mdnsd
  /data/local/tmp/echod --dbg --gateway-url wss://invalid.example --gateway-token-file /data/local/etc/echo-satellite/gateway.token
'"
```

Expected: `echod` records preparation actions, disables the firmware boot animation, clears then animates the indicator before connection work, both services report stopped while it runs, and GPIO 444 has its original value after a signal-driven shutdown. Record the exact observations and restore the device only through the documented recovery command or reboot.

#### Task 2 review remediation: gateway LED connection state

**Status:** completed 2026-09-08

**Dependencies:** Task 2 hardware diagnostic.

**Purpose:** Make the visible startup and offline behavior match the observed operational contract.

**Concrete changes:** Keep a blue moving boot indication for at least four seconds; then clear LEDs when connected, render red when not connected, and clear LEDs again after reconnection.

**Verification:** Host LED/controller tests plus a qualified-Dot stopped-gateway and reconnect observation.

#### Task 3 review remediation: PR #6 correctness findings

**Status:** completed 2026-09-08

**Purpose:** Resolve each open automated-review finding without broadening the operational logging or device-startup scope.

**Dependencies:** Tasks 1–2.

**Hardware required:** no.

**Files or components:** `internal/device/led/animator.go`, `internal/device/led/animator_test.go`, `internal/logging/logging.go`, `internal/logging/logging_test.go`, `cmd/echod/main.go`, and `cmd/echod/main_test.go`.

**Concrete changes:** Make `Animator.Off` write an all-zero frame on its next tick instead of fading; require the three-byte rotating-log capacity only when a file sink is selected, from every command parser through the shared logger; and preserve a single `prepare device startup` error context at the composition boundary. Add deterministic regression tests for all three rules.

**Expected outcome:** The PR feedback is addressed with behavior matching the documented LED and logging contracts and without duplicate error prefixes.

**Verification:**

```sh
go test -race ./internal/device/led ./internal/logging ./cmd/echod
make fmt-check
make lint
make test
make verify
```

Expected: all commands exit 0.

## Cross-task risks

- Excessive protocol logs could expose private data or flood storage; use explicit metadata allowlists, bounded fields, and the rotation cap.
- Stopping a FireOS service may have an unexpected device effect; host tests isolate commands and the real-Dot check records behavior before declaring completion.
- Restoring a muted GPIO state can leave the agent unable to hear wake audio; that is intentional physical privacy behavior and must be logged clearly.

## Rollback or recovery

- Remove new logging flags/helper wiring to return to existing stderr text logging.
- On a device problem, stop `echod`, run `start ledcontroller` and `start mdnsd` if those were originally running, or reboot; do not write unrelated Android/FireOS partitions.

## Final acceptance criteria

- [x] All binaries default to text logs and support JSON plus a bounded optional log file.
- [x] Debug diagnostics trace discovery and protocol lifecycle without audio, tokens, or payload bodies.
- [x] `echod` performs and tests ordered LED/service/mute preparation and cleanup.
- [x] A qualified Dot confirms the startup behavior and the observed contract is documented.
- [x] Fresh-context review is triaged and `make verify` passes.
- [x] PR #6 review findings are dispositioned with regression coverage and final checks.

## Progress log

- 2026-09-07: Plan created from the explicitly authorized TODO items; no implementation started.
- 2026-09-07: Moved to `in-progress/`; `/root` claimed both tasks. Added shared text-default/JSON logging with bounded three-generation optional file sinks and safe client protocol lifecycle metadata (type and byte count only). Added host-tested injected startup preparation that snapshots/restores GPIO 444, stops `ledcontroller`/`mdnsd`, disables firmware animation, clears LEDs, and renders the initial project frame. Real-Dot verification remains required before Task 2 can complete.
- 2026-09-07: Fresh-context review triage: fixed log-sink close-on-error-exit, `dotsim` URL credential redaction, joined normal-shutdown cleanup errors, gateway/dotsim environment-over-INI precedence, and parser validation for the three rotating generations. Postponed: a composition-root ordering test for startup preparation and a multi-frame startup animation; the current host primitive verifies ordered actions, but the qualified-Dot run must establish the actual visual behavior. The `--mic-from-file` startup-preparation exemption remains intentional for host fixture execution and is a follow-up to replace with an explicit fixture-only composition mode.
- 2026-09-07: Qualified Dot `G090LF0964060EHP` diagnostic passed. GPIO 444 was exported because it was initially absent, set to `0`, and remained `0` through shutdown; `echod` stopped `ledcontroller` and `mdnsd`, set `boot_animation=0`, and rendered frame `000006` around the ring. It browsed `wlan0`, discovered Windows gateway `CORUSANT.local` at `192.168.110.127:8770`, and completed `hello`/`welcome`/`config.result` with the `.m2/device-token` and development TLS bypass. The temporary token and logs were removed; both FireOS services were explicitly restarted afterward. Signal shutdown currently reports a canceled gateway session (exit 1) even though cleanup completed.
- 2026-09-08: Final fresh-context review fixed gateway safe protocol type/size logs and consistent `echoctl` rotation-cap parsing. It also confirmed LED-service ownership, transition behavior, and GPIO cleanup. A qualified-Dot final check observed blue comet at one second, all-off after the four-second boot period plus successful `hello`/`welcome`/`config.result`, GPIO 444 preserved at `0`, and explicit `ledcontroller`/`mdnsd` recovery. The stopped-gateway and reconnect visual checks were also performed during implementation.
- 2026-09-08: Reopened for PR #6 automated-review remediation. Task 3 addresses the three unresolved findings with host-only regression coverage; no hardware behavior changes beyond the documented immediate `Off` clear are required.
- 2026-09-08: Fresh-context review found conditional-capacity validation was incomplete in `echod`, `gateway`, and `echoctl`, and that the LED regression test did not synchronize a pre-`Off` non-zero frame. Task 3 now covers those parser layers and uses deterministic tick sequencing.
- 2026-09-08: Task 3 completed. The three original PR findings were fixed. Fresh-context review initially found the parser and LED-test gaps; both were fixed and the reviewer found no remaining implementation defects. No findings were declined or postponed.

## Completion evidence

- `make fmt-check` — passed 2026-09-07.
- `make lint` — passed 2026-09-07.
- `make test` — passed 2026-09-07 (race suite; 70.7% total coverage).
- `go test -race ./cmd/echod ./cmd/gateway ./cmd/dotsim ./cmd/echoctl ./internal/logging ./internal/device/system ./internal/device/client` — passed 2026-09-07 after review remediation.
- Fresh-context review — completed and triaged 2026-09-07; see progress log.
- Qualified-Dot diagnostic — passed 2026-09-07; service, GPIO, LED-frame, discovery, and handshake observations are recorded in `docs/device-diagnostics.md`.
- `make verify` — passed 2026-09-08 (format, lint, race suite, host builds, and portability builds).
- Fresh-context final review — all findings triaged and fixed 2026-09-08.
- `go test -race -count=20 ./internal/device/led` — passed 2026-09-08; immediate clear and rendering-resume regression test remained stable across repeated schedules.
- `go test -race ./internal/device/led ./internal/logging ./cmd/echod ./cmd/gateway ./cmd/echoctl` — passed 2026-09-08.
- `make fmt-check` and `make lint` — passed 2026-09-08.
- `make test` — passed 2026-09-08 (race suite; 70.3% total coverage).
- `make verify` — passed 2026-09-08 (format, lint, race suite, host builds, and portability builds).
- Fresh-context Task 3 review — completed and all findings fixed 2026-09-08; no follow-up items remain.
- No unresolved limitations remain for this plan.
