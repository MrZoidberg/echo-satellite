# Device-lab review-remediation design

**Status:** approved for implementation
**Owner:** Codex (/root)
**Created:** 2026-09-09

## Goal

Close the fresh-review findings for the device-lab harness without widening its authority beyond token-owned diagnostic files and reversible service/GPIO changes.

## Design

The runner will treat a session as an owned transaction. It will only resume a canonical child of `.bin/device-lab`, fsync both state files and their parent directory, and hold both a local serial lock and a root-owned remote lock. A root-created, mode-0700 remote root contains a private token and owner sidecars; their root ownership and restrictive modes are checked before any payload runs.

Preparation captures the installed-agent digest, relevant service state, boot animation, GPIO state, microphone holders, and boot ID before making reversible changes. It refuses a busy microphone or an unknown lock. Cleanup reads only that captured state, restores it, removes only token-owned content, checks that no owned process remains, and `verify-clean` compares the final installed digest with the initial digest as well as checking the remote root and lock are gone.

Qualification operates only on diagnostic copies. Its payload records permission enforcement, same-directory staged-file fsync/rename/directory fsync, old-process lifetime, Magisk layout/hook behavior, controlled exit 75, crash-backoff samples, restart timing samples and nearest-rank percentiles. Reboot/resume is represented by the recorded boot ID and an explicit resumed phase; it cannot be claimed until a distinct post-reboot boot ID is observed. The Windows launcher will wait for an HTTPS/TLS health probe and record exact PID stop success in evidence.

Plan lint will parse per-task blocks rather than global substrings. Completed tasks require their own verification/evidence, hardware tasks require explicit provenance and cleanup, repository-relative links resolve from repository root, and active-plan task status/supersession is checked.

## Verification

Use deterministic runner and linter fixtures for ownership, containment, directory durability, state restoration, digest mismatch, gateway readiness/PID outcome, qualification observations, and plan-lint failures. A later operator-authorized live run is still required for physical hardware claims.
