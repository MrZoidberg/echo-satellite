# `echoctl wifi set` implementation plan

**Status:** finished
**Owner or active agent:** /root
**Created:** 2026-09-01
**Updated:** 2026-09-01
**Started:** 2026-09-01
**Completed:** 2026-09-01

## Objective

Add a device-side `echoctl wifi set` command that safely configures an open or
WPA2-PSK Wi-Fi network through Android's existing `wpa_supplicant` control
socket, without disclosing credentials.

## Non-goals

- Interactive selection, scanning, WPS, enterprise or WPA3 networking.
- Directly editing supplicant configuration files.
- A host-side ADB wrapper, gateway-delivered credentials, or any `echod`
  Wi-Fi credential API.

## Source references and constraints

- `docs/plans/2026-09-01-echoctl-wifi-design.md` is the approved feature
  design and command-sequence source of truth.
- `docs/DESIGN.md` §§3.12 and 18 permit ADB as the development/recovery path;
  supplicant remains the Android-owned network-state authority.
- `AGENTS.md` requires `jessevdk/go-flags`, consumer-owned interfaces,
  `testify`, wrapped package-boundary errors, and no credential leakage.
- Root commands must use `su -c`, with only fixed `wpa_cli` arguments, numeric
  IDs, and hexadecimal SSID/PSK values. The plaintext passphrase must not be
  passed to a root shell or printed.

## Dependencies and prerequisites

- The existing `cmd/echoctl` command tree is the only shared implementation
  surface with the active Milestone 2 plan. This work only adds a new command
  group and does not alter discovery, protocol, simulator, or `echod` files.
- No Dot is required for software acceptance. A hardware join is intentionally
  deferred to the active Milestone 2 LAN task.

## Architecture and high-level plan

1. Create a narrowly scoped Wi-Fi package with a one-method injected command
   runner and fully deterministic supplicant command construction.
2. Configure a new network transactionally: validate inputs; remove duplicate
   SSIDs; add; configure; enable/select; save. Clean up the newly added entry
   after every later failure while returning the original error.
3. Add `echoctl wifi set` parsing, dispatch, and an SSID-only success report.
4. Complete focused and repository-wide checks, then conduct a fresh-context
   review and record every finding disposition.

## Planned file map

- Create: `internal/device/wifi/wifi.go` — supplicant adapter and validation.
- Create: `internal/device/wifi/wifi_test.go` — scripted runner transaction,
  validation, derivation, cleanup, and secrecy tests.
- Create: `cmd/echoctl/wifi.go` — command wiring and report formatting.
- Create: `cmd/echoctl/wifi_test.go` — CLI dispatch/report tests.
- Modify: `cmd/echoctl/config.go` — flags command group.
- Modify: `cmd/echoctl/main.go` — dispatcher entry.

## Numbered tasks

### Task 1: Deterministic Android supplicant Wi-Fi configuration

**Status:** completed 2026-09-01

**Purpose:** Isolate privileged Android supplicant interaction in a testable
device-local package.

**Dependencies:** None.

**Hardware required:** no.

**Files or components:**

- Create: `internal/device/wifi/wifi.go`
- Test: `internal/device/wifi/wifi_test.go`

**Concrete changes:**

- Define the consumer-owned one-method runner interface and production runner
  for `su -c wpa_cli -i wlan0 -p /data/misc/wifi/sockets ...`.
- Validate a non-empty SSID and WPA2 passphrase (8–63 characters or a
  64-character hexadecimal PSK); derive non-hex PSKs with PBKDF2-SHA1/4096.
- List configured networks, remove matching SSIDs, add/configure a new one
  with hex SSID and `scan_ssid=1`, then enable/select and save it.
- On every post-add failure, try to remove the new numeric ID, preserve the
  original wrapped error, and never include a passphrase or PSK in returned
  errors.

**Expected outcome:** Open, WPA2, and hidden network configuration executes
only safe root-shell values and is fully unit-testable from scripted results.

**Verification:**

Run:

```sh
go test -race ./internal/device/wifi/...
```

Expected: exit 0, including transaction sequence, validation, cleanup, and
credential-leakage test cases.

### Task 2: `echoctl wifi set` interface

**Status:** completed 2026-09-01

**Purpose:** Expose the adapter through the existing device-resident CLI.

**Dependencies:** Task 1.

**Hardware required:** no.

**Files or components:**

- Create: `cmd/echoctl/wifi.go`
- Create: `cmd/echoctl/wifi_test.go`
- Modify: `cmd/echoctl/config.go`
- Modify: `cmd/echoctl/main.go`
- Test: `cmd/echoctl/config_test.go`

**Concrete changes:**

- Add `wifi set --ssid` and optional `--passphrase` using the established
  `go-flags` command pattern.
- Dispatch to the package and print only the SSID plus success status.
- Test argument requirements, open/WPA2 wiring, dispatch behavior, and output
  that excludes supplied credentials.

**Expected outcome:** A user can configure an eligible network with `echoctl`
without the command reporting secret material.

**Verification:**

Run:

```sh
go test -race ./cmd/echoctl/...
```

Expected: exit 0 with parse, dispatch, and secret-free output covered.

### Task 3: Cross-cutting validation and independent review

**Status:** completed 2026-09-01

**Purpose:** Prove the finished change meets repository quality and boundary
requirements.

**Dependencies:** Tasks 1–2.

**Hardware required:** no — real Dot joining remains deferred to Milestone 2.

**Files or components:** all Task 1–2 changes and this plan's progress record.

**Concrete changes:**

- Run formatter, lint, fresh race tests, host builds, and package checks.
- Request a fresh-context review of the diff against this plan and
  `docs/DESIGN.md`; fix, decline with rationale, or postpone every finding.
- Record exact outcomes and the absent hardware acceptance in this plan.

**Expected outcome:** The feature is independently reviewed and has honest
software-only verification evidence.

**Verification:**

Run:

```sh
make verify
```

Expected: exit 0.

## Cross-task risks

- Android variations in `wpa_cli` output can make network-list parsing brittle;
  parse only the tabular SSID/ID data required by this command and surface
  unexpected responses as errors.
- Credential exposure in shell text, errors, or reports is a security risk;
  root-shell input is restricted to derived hexadecimal values and tests assert
  that errors/reports omit secrets.

## Rollback or recovery

Each attempted configuration only changes supplicant-managed state. A failed
new-network setup removes its newly created entry and does not call
`save_config`; operators retain ADB recovery access for a network that cannot
join.

## Final acceptance criteria

- [ ] `echoctl wifi set` accepts an SSID and optional passphrase, supporting
  open and WPA2-PSK hidden networks.
- [ ] WPA2 passphrases are validated/derived locally; no plaintext credential
  enters `su -c`, errors, or reports.
- [ ] Duplicate SSIDs are removed and new-entry setup failures clean up.
- [ ] All specified verification passes; no live-device Wi-Fi claim is made.
- [ ] Fresh-context review is complete and every finding is triaged.

## Progress log

- 2026-09-01: Plan created from the user-approved design. It does not overlap
  the active Milestone 2 implementation except for additive `echoctl` command
  registration. Task 1 claimed by /root.
- 2026-09-01: Task 1 completed. `go test -race ./internal/device/wifi/...`
  passed; Task 2 claimed by /root.
- 2026-09-01: Task 2 completed. `go test -race ./cmd/echoctl/...` passed;
  Task 3 claimed by /root.
- 2026-09-01: Fresh-context review found one medium-severity defect: malformed
  `list_networks` output could be treated as an empty list. Fixed by requiring
  the supplicant header and valid records before any mutation; an explicit
  no-mutation regression test was added. No findings were declined or
  postponed.
- 2026-09-01: Task 3 completed. Final software checks passed. No live Dot
  join was attempted; that hardware validation remains deliberately deferred
  to the active Milestone 2 LAN task.

## Completion evidence

- `go test -race ./internal/device/wifi/...` — passed 2026-09-01.
- `go test -race ./cmd/echoctl/...` — passed 2026-09-01.
- `make fmt-check` — passed 2026-09-01.
- `make lint` — passed with 0 issues 2026-09-01.
- `make test` — passed (fresh race suite) 2026-09-01.
- `make verify` — passed (format, lint, fresh race tests, host builds)
  2026-09-01. The command was run with local-loopback permission because the
  sandbox blocks IPv6 `httptest` listeners used by existing TLS integration
  tests.
- Fresh-context review — completed 2026-09-01. The one finding was fixed and
  re-verified; none are unresolved.
- Hardware limitation: no live Wi-Fi join was performed, by plan scope.
