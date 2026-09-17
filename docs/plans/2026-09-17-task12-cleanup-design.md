# Task 12 cleanup design

**Status:** proposed
**Owner:** Codex
**Created:** 2026-09-17

## Purpose

Refocus Milestone 3 Task 12 on final consistency, removal of obsolete material,
and independent review. The approved single-agent update boundary is settled by
Milestone 3 Task 1 and is not reopened by this work.

## Scope

Task 12 will reconcile current documentation, implementation, tests, examples,
and operational instructions with the existing single-agent contract. It will
remove obsolete legacy references or unreachable legacy implementation only
when those items remain in current product paths. Historical plans and their
evidence are records and remain unchanged.

The task will also classify diagnostic and test-specific material by continuing
purpose before removing it:

- Retain reusable on-device diagnostics and their tests, including `echoctl`
  diagnostics, the device-lab runner and its safe prepare/cleanup/evidence
  path, and supported diagnostic configuration needed to reproduce qualified
  Dot work.
- Retain `cmd/task8gateway`, its documentation, and its tests because the
  active future release-signing/deployment-qualification plan explicitly
  depends on that harness.
- Remove only material demonstrated to be one-off: it has no current product
  caller, no reusable on-device diagnostic purpose, no test value, and no
  active or future plan ownership. Remove its code, tests, fixtures, and
  documentation together.

This cleanup does not remove the ability to run safe real-Dot diagnostics,
does not weaken cleanup/evidence provenance, and does not change the voice or
single-agent update boundaries.

## Approach options

1. Keep every diagnostic artifact. This has the lowest immediate risk but
   leaves unsupported one-off paths and stale instructions in the repository.
2. Remove all milestone-labelled tooling. This is too broad: it would break
   the successor deployment-qualification plan and reduce reproducible
   on-device testing.
3. **Recommended: ownership-based cleanup.** Inventory each candidate's
   callers, tests, documentation, and plan references; retain it if any
   supported purpose remains, otherwise remove the complete unused slice.

## Design

### Documentation and legacy consistency

The audit targets only current sources: root documentation, operational guides,
configuration examples, commands, and code. It verifies that these describe
the settled single-agent installer: verified same-directory staging, atomic
replacement, no local fallback after commit, launcher backoff only, connected
downgrade deployment, and ADB recovery after a failed reconnect. It does not
revise historical plan language merely because it describes the superseded
architecture.

### Test-tooling inventory and deletion rule

For each candidate test-only artifact, record its path, callers, documentation
references, plan owner, and retained purpose. The inventory is made in the Task
12 progress log or completion evidence. A candidate may be deleted only if all
four checks are negative: no product caller, no reusable diagnostic workflow,
no relevant test coverage value, and no active/future plan dependency.

Before deletion, add or retain narrow tests that exercise each retained
on-device command/path. After deletion, search for dangling references and run
the relevant focused tests. Do not delete device-lab payload ownership,
redaction, cleanup, or verify-clean guarantees as incidental cleanup.

### Review and evidence

A fresh-context reviewer receives the complete diff, the Milestone 3 plan,
`docs/DESIGN.md`, and `docs/protocol.md`. Every finding receives a recorded
fix, reasoned decline, or durable follow-up. The final plan evidence separates
host/simulator checks from the already-recorded real-Dot evidence and names
known qualification limitations rather than implying they passed.

## Verification

Run targeted tests for any retained or removed diagnostic component, then:

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

Searches for deleted paths must return no live references. The final
fresh-context review must be fully triaged. No additional hardware run is
required, but cleanup must not invalidate the documented means to perform one.

## Non-goals

- Reopening, reimplementing, or re-evaluating the A/B architecture.
- Removing historical plan records.
- Deleting `task8gateway` before its declared successor-plan dependency ends.
- Replacing hardware qualification evidence with host or simulator results.
