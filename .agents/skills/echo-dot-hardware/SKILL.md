---
name: echo-dot-hardware
description: Use only for live Echo Dot, FireOS, ADB, Magisk, reboot, live audio, or hardware qualification work; do not use for dotsim-only or ordinary Go work.
---

# Echo Dot hardware workflow

Read the active plan and applicable `docs/DESIGN.md` sections before touching a
device. One orchestrator owns a physical Dot session; reviewers are read-only.

1. Run `tools/device-lab/device_lab.py preflight` with explicit `--adb` and
   `--serial`; do not infer either.
2. Capture sanitized evidence, use the runner's token-owned diagnostic root,
   and never kill a busy process or remove an unknown lock.
3. Before microphone diagnostics, stop `ledcontroller`, disable boot animation,
   and verify GPIO 444 is low. A high line physically cuts microphones.
4. Use `prepare`, then always use `cleanup --resume <session>` and
   `verify-clean`. Investigate any changed installed-agent digest manually.

The voice boundary stays local to the device. The update boundary permits only
verified replacement of `/data/local/bin/echod`; this diagnostic runner never
writes it. See `tools/device-lab/` for payloads and evidence format.
