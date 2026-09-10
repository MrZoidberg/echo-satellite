#!/usr/bin/env -S uv run --no-project --script
# /// script
# requires-python = ">=3.11"
# dependencies = []
# ///
"""Safe, resumable Echo Dot diagnostic-session runner.

This tool deliberately stages diagnostics below a token-owned path.  It never
writes the installed agent; release installation remains ``echoctl`` work.
"""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import re
import secrets
import subprocess
import sys
import time
from dataclasses import dataclass
from pathlib import Path
from typing import Any, Sequence


SCHEMA_VERSION = 1
MAX_OUTPUT = 8_192
SECRET_NAME = re.compile(r"(?:token|authorization|credential|private.?key|secret|signed.?url)", re.I)
URL_QUERY = re.compile(r"https?://[^\s?]+\?[^\s]+", re.I)
SAFE_REMOTE_ROOT = re.compile(r"^/data/local/tmp/echo-device-lab/[A-Za-z0-9_-]+$")
SAFE_SERIAL = re.compile(r"^[A-Za-z0-9._:-]+$")
SAFE_ARTIFACT_NAME = re.compile(r"^[A-Za-z0-9][A-Za-z0-9._-]*$")
SAFE_PROBE_PATH = re.compile(r"^/(?:data/adb/service\.d|sbin/\.core/img/\.core/service\.d)/[A-Za-z0-9._-]+$")
DEFAULT_METADATA_PATH = "/data/local/etc/echo-satellite/installed-release.json"
DEFAULT_HOOK_PATH = "/sbin/.core/img/.core/service.d/echo-satellite.sh"
SESSION_PARENT = (Path.cwd() / ".bin" / "device-lab").resolve()
COMMANDS = ("preflight", "prepare", "cleanup", "verify-clean", "render-evidence", "stage-external", "record-external-action")


class LabError(RuntimeError):
    """A safety condition that must be reported without attempting recovery."""


def digest(path: Path) -> str:
    return hashlib.sha256(path.read_bytes()).hexdigest()


def redact(value: Any, *, key: str = "") -> Any:
    """Return an evidence-safe value or fail closed for unsafe structured input."""

    if SECRET_NAME.search(key):
        return "[redacted]"
    if isinstance(value, str):
        if URL_QUERY.search(value):
            raise LabError("evidence rejects URLs with query strings")
        return value
    if isinstance(value, dict):
        return {str(name): redact(item, key=str(name)) for name, item in value.items()}
    if isinstance(value, list):
        return [redact(item, key=key) for item in value]
    return value


def assert_safe_evidence(value: Any, *, key: str = "") -> None:
    """Reject secrets even if a caller bypassed the normal redaction route."""

    if SECRET_NAME.search(key):
        raise LabError(f"evidence contains prohibited field {key!r}")
    if isinstance(value, str) and URL_QUERY.search(value):
        raise LabError("evidence contains a URL query string")
    if isinstance(value, dict):
        for name, item in value.items():
            assert_safe_evidence(item, key=str(name))
    elif isinstance(value, list):
        for item in value:
            assert_safe_evidence(item, key=key)


def atomic_json(path: Path, value: dict[str, Any]) -> None:
    assert_safe_evidence(value)
    path.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
    temporary = path.with_suffix(path.suffix + ".part")
    with temporary.open("w", encoding="utf-8") as file:
        json.dump(value, file, sort_keys=True, indent=2)
        file.write("\n")
        file.flush()
        os.fsync(file.fileno())
    os.replace(temporary, path)
    descriptor = os.open(path.parent, os.O_RDONLY)
    try:
        os.fsync(descriptor)
    finally:
        os.close(descriptor)


def atomic_state(path: Path, value: dict[str, Any]) -> None:
    """Persist private state; unlike evidence it deliberately retains ownership tokens."""

    path.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
    temporary = path.with_suffix(path.suffix + ".part")
    with temporary.open("w", encoding="utf-8") as file:
        json.dump(value, file, sort_keys=True, indent=2)
        file.write("\n")
        file.flush()
        os.fsync(file.fileno())
    os.replace(temporary, path)
    descriptor = os.open(path.parent, os.O_RDONLY)
    try:
        os.fsync(descriptor)
    finally:
        os.close(descriptor)


def bounded(text: str) -> str:
    return text if len(text) <= MAX_OUTPUT else text[:MAX_OUTPUT] + "\n[output truncated]"


@dataclass
class CommandResult:
    returncode: int
    stdout: str
    stderr: str


class Runner:
    """Owns one host session and its corresponding remote ownership token."""

    def __init__(self, args: argparse.Namespace) -> None:
        self.args = args
        self.root = self._resume_root(args.resume) if args.resume else SESSION_PARENT / self._session_id()
        self.root.mkdir(mode=0o700, parents=True, exist_ok=True)
        self.evidence_path = self.root / "evidence.json"
        self.state_path = self.root / "state.json"
        self.log_path = self.root / "runner.log"
        self.state = self._load_state()
        self.token = str(self.state.setdefault("ownership_token", secrets.token_hex(24)))
        self.remote_root = str(self.state.setdefault("remote_root", f"/data/local/tmp/echo-device-lab/{self.root.name}"))
        if not SAFE_REMOTE_ROOT.fullmatch(self.remote_root):
            raise LabError("remote root is not a session-specific diagnostic path")
        self.evidence = self._load_evidence()
        self.evidence.setdefault("inputs", redact({"adb": args.adb, "serial": args.serial, "remote_root": self.remote_root}))
        self._save()

    @staticmethod
    def _resume_root(resume: str | None) -> Path:
        """Accept only an existing session directory below the owned parent."""

        if resume is None:
            raise LabError("internal: missing resume path")
        root = Path(resume).resolve()
        try:
            root.relative_to(SESSION_PARENT)
        except ValueError as error:
            raise LabError("--resume must be a session below .bin/device-lab") from error
        if root == SESSION_PARENT or not root.is_dir():
            raise LabError("--resume must name an existing device-lab session")
        return root

    @staticmethod
    def _session_id() -> str:
        return time.strftime("%Y%m%dT%H%M%SZ", time.gmtime()) + "-" + secrets.token_hex(5)

    def _load_state(self) -> dict[str, Any]:
        if not self.state_path.exists():
            return {"schema_version": SCHEMA_VERSION}
        return json.loads(self.state_path.read_text(encoding="utf-8"))

    def _load_evidence(self) -> dict[str, Any]:
        if self.evidence_path.exists():
            return json.loads(self.evidence_path.read_text(encoding="utf-8"))
        return {"schema_version": SCHEMA_VERSION, "session_id": self.root.name, "serial": self.args.serial,
                "started_at": time.time(), "harness_revision": self._revision(), "phases": [], "checks": []}

    @staticmethod
    def _revision() -> str:
        result = subprocess.run(("git", "rev-parse", "HEAD"), capture_output=True, check=False, text=True)
        return result.stdout.strip() if result.returncode == 0 else "unknown"

    def _save(self) -> None:
        atomic_state(self.state_path, self.state)
        atomic_json(self.evidence_path, redact(self.evidence))

    def host(self, arguments: Sequence[str]) -> CommandResult:
        result = subprocess.run(arguments, capture_output=True, check=False, text=True)
        outcome = CommandResult(result.returncode, bounded(result.stdout), bounded(result.stderr))
        with self.log_path.open("a", encoding="utf-8") as log:
            log.write(json.dumps({"command": list(arguments), "returncode": outcome.returncode}) + "\n")
        return outcome

    def adb(self, arguments: Sequence[str]) -> CommandResult:
        return self.host((self.args.adb, "-s", self.args.serial, *arguments))

    def run_remote_payload(self, payload: str) -> CommandResult:
        # The script pathname is the sole su -c argument: no host-built nested shell.
        path = f"{self.remote_root}/{payload}"
        return self.root_shell(f"ROOT={self.remote_root}; export ROOT; . {path}")

    def root_shell(self, script: str) -> CommandResult:
        """Run a generated root script as the sole ``su -c`` target.

        ADB does not preserve the boundary of an inline multi-word command after
        ``su -c``.  A private staged script makes redirections and token writes
        deterministic while retaining the device-lab rule that ``su -c`` receives
        exactly one pathname.
        """

        suffix = secrets.token_hex(8)
        local = self.root / f"root-command-{suffix}.sh"
        remote = f"/data/local/tmp/.echo-device-lab-root-{self.root.name}-{suffix}.sh"
        local.write_text(
            "#!/system/bin/sh\nset -u\n( set -e\n" + script +
            "\n)\nstatus=$?\nrm -f \"$0\"\necho __DEVICE_LAB_STATUS=$status\nexit 0\n",
            encoding="utf-8",
        )
        os.chmod(local, 0o700)
        try:
            pushed = self.adb(("push", str(local), remote))
            if pushed.returncode:
                return pushed
            result = self.adb(("shell", f"su -c '{remote}'"))
            marker = re.search(r"^__DEVICE_LAB_STATUS=(\d+)\s*$", result.stdout, re.M)
            if marker is None:
                return CommandResult(1, result.stdout, bounded(result.stderr + "\nmissing root-script status marker"))
            return CommandResult(int(marker.group(1)), re.sub(r"^__DEVICE_LAB_STATUS=\d+\s*$\n?", "", result.stdout, flags=re.M), result.stderr)
        finally:
            local.unlink(missing_ok=True)

    def verify_remote_owner(self, payload: str) -> None:
        result = self.root_shell(f"BB=/data/adb/magisk/busybox; test \"$($BB cat {self.remote_root}/.owner)\" = '{self.token}' && test \"$($BB cat {self.remote_root}/.lock)\" = '{self.token}' && test \"$($BB stat -c %u:%a {self.remote_root})\" = 0:700 && test \"$($BB stat -c %u:%a {self.remote_root}/.owner)\" = 0:600 && test \"$($BB stat -c %u:%a {self.remote_root}/.lock)\" = 0:600 && test \"$($BB stat -c %u:%a {self.remote_root}/{payload})\" = 0:700")
        if result.returncode:
            raise LabError(f"remote ownership for {payload} is missing or belongs to another session")

    def phase(self, name: str, action: callable, provenance: str = "hardware") -> None:  # type: ignore[valid-type]
        completed = {phase["name"] for phase in self.evidence["phases"] if phase["status"] == "passed"}
        if name in completed:
            return
        started = time.monotonic()
        try:
            observation = action()
        except LabError as error:
            self.evidence["phases"].append({"name": name, "status": "failed", "duration_ms": int((time.monotonic()-started)*1000), "observation": str(error), "provenance": provenance})
            self._save()
            raise
        self.evidence["phases"].append({"name": name, "status": "passed", "duration_ms": int((time.monotonic()-started)*1000), "observation": redact(observation), "provenance": provenance})
        self._save()

    def acquire_host_lock(self) -> None:
        lock = self.root.parent / f"{self.args.serial}.lock"
        try:
            descriptor = os.open(lock, os.O_CREAT | os.O_EXCL | os.O_WRONLY, 0o600)
        except FileExistsError as error:
            try:
                owner_token = lock.read_text(encoding="ascii")
            except UnicodeDecodeError:
                raise LabError(f"device lock already exists: {lock}; do not steal it") from error
            if owner_token == self.token:
                return
            recovery = self._recover_lock_command(owner_token)
            if recovery:
                raise LabError(f"device lock already exists: {lock}; resume its owner with: {recovery}") from error
            raise LabError(f"device lock already exists: {lock}; do not steal it") from error
        os.write(descriptor, self.token.encode("ascii"))
        os.close(descriptor)
        self.state["host_lock"] = str(lock)
        self._save()

    def _recover_lock_command(self, token: str) -> str | None:
        """Return a canonical resume command only for a proven local owner."""

        for session in self.root.parent.iterdir():
            if not session.is_dir() or session == self.root:
                continue
            try:
                state = json.loads((session / "state.json").read_text(encoding="utf-8"))
                evidence = json.loads((session / "evidence.json").read_text(encoding="utf-8"))
            except (OSError, json.JSONDecodeError):
                continue
            if state.get("ownership_token") == token and evidence.get("serial") == self.args.serial:
                return f"--resume .bin/device-lab/{session.name}"
        return None

    def release_host_lock(self) -> None:
        lock = self.state.get("host_lock")
        if isinstance(lock, str) and Path(lock).exists() and Path(lock).read_text(encoding="utf-8") == self.token:
            Path(lock).unlink()

    def preflight(self) -> None:
        self.acquire_host_lock()
        self.phase("preflight-adb", lambda: self._must_succeed(self.adb(("get-state",)), "ADB device state"))
        self.phase("preflight-root", lambda: self._must_succeed(self.adb(("shell", "su", "-c", "id")), "root check"))
        def capture() -> dict[str, str]:
            result = self.root_shell("/data/adb/magisk/busybox sha256sum /data/local/bin/echod 2>/dev/null; /data/adb/magisk/busybox cat /proc/sys/kernel/random/boot_id; for p in /proc/[0-9]*/fd/*; do readlink \"$p\" 2>/dev/null; done | /data/adb/magisk/busybox grep -m1 /dev/snd || true; getprop init.svc.ledcontroller; getprop init.svc.mdnsd; /data/adb/magisk/busybox cat /sys/bus/i2c/devices/0-003f/boot_animation 2>/dev/null || true; /data/adb/magisk/busybox cat /sys/class/gpio/gpio444/value 2>/dev/null || true")
            self._must_succeed(result, "capture initial device state")
            lines = result.stdout.splitlines()
            agent_digest = lines[0].split(maxsplit=1)[0] if lines else ""
            if len(lines) < 2 or not re.fullmatch(r"[0-9a-f]{64}", agent_digest):
                raise LabError("could not capture installed-agent digest")
            if any("/dev/snd" in line for line in lines[2:]):
                raise LabError("microphone is busy; coordinate with its owner")
            state = {"installed_agent_digest": agent_digest, "boot_id": lines[1], "services_gpio": lines[2:]}
            self.state["initial_device_state"] = state
            self._save()
            return {"installed_agent_digest": agent_digest, "boot_id": lines[1], "microphone": "idle"}
        self.phase("preflight-initial-state", capture)
        self.phase("preflight-command-capabilities", self.command_capabilities)
        self.phase("preflight-lock", lambda: {"ownership": "acquired"})

    def command_capabilities(self) -> dict[str, str]:
        """Capture FireOS command behavior as diagnostic evidence, never a gate."""

        script = (
            "BB=/data/adb/magisk/busybox; "
            "printf 'busybox_path=%s\\n' \"$BB\"; "
            "printf 'busybox_version='; $BB 2>&1 | $BB head -n 1; "
            "for applet in cmp sed awk sha256sum; do "
            "if $BB \"$applet\" --help >/dev/null 2>&1; then printf 'busybox_%s=available\\n' \"$applet\"; "
            "else printf 'busybox_%s=missing\\n' \"$applet\"; fi; done; "
            "for command in cmp sed awk sha256sum; do "
            "if command -v \"$command\" >/dev/null 2>&1; then printf 'system_%s=available\\n' \"$command\"; "
            "else printf 'system_%s=missing\\n' \"$command\"; fi; done; "
            "probe=/data/local/tmp/.echo-device-lab-cmp-$$; : > \"$probe\"; "
            "if command -v cmp >/dev/null 2>&1; then "
            "if cmp -s \"$probe\" \"$probe\" >/dev/null 2>&1; then echo system_cmp_s=supported; "
            "else echo system_cmp_s=rejected; fi; else echo system_cmp_s=missing; fi; "
            "if $BB cmp -s \"$probe\" \"$probe\" >/dev/null 2>&1; then echo busybox_cmp_s=supported; "
            "else echo busybox_cmp_s=rejected; fi; rm -f \"$probe\""
        )
        result = self.root_shell(script)
        self._must_succeed(result, "probe command capabilities")
        capabilities: dict[str, str] = {}
        for line in result.stdout.splitlines():
            key, separator, value = line.partition("=")
            if separator and re.fullmatch(r"[a-z0-9_]+", key):
                capabilities[key] = bounded(value)
        return capabilities

    def ensure_remote_root(self) -> None:
        script = f"BB=/data/adb/magisk/busybox; umask 077; test ! -e {self.remote_root} || test \"$($BB cat {self.remote_root}/.owner 2>/dev/null)\" = '{self.token}' || exit 73; mkdir -p {self.remote_root}; chown root:root {self.remote_root}; chmod 700 {self.remote_root}; $BB printf '%s' '{self.token}' > {self.remote_root}/.owner; chown root:root {self.remote_root}/.owner; chmod 600 {self.remote_root}/.owner; test ! -e {self.remote_root}/.lock || test \"$($BB cat {self.remote_root}/.lock)\" = '{self.token}' || exit 74; $BB printf '%s' '{self.token}' > {self.remote_root}/.lock; chown root:root {self.remote_root}/.lock; chmod 600 {self.remote_root}/.lock; $BB sync"
        self._must_succeed(self.root_shell(script), "create root-owned diagnostic root and remote lock")

    def stage_payloads(self) -> dict[str, str]:
        """Push immutable repository payloads into the already validated session root."""

        self.ensure_remote_root()
        payload_directory = Path(__file__).with_name("payloads")
        staged: dict[str, str] = {}
        for source in sorted(payload_directory.glob("*.sh")):
            destination = f"{self.remote_root}/{source.name}"
            temporary = f"/data/local/tmp/.echo-device-lab-{self.root.name}-{source.name}"
            self._must_succeed(self.adb(("push", str(source), temporary)), f"push {source.name}")
            self._must_succeed(self.root_shell(f"mv {temporary} {destination}; chown root:root {destination}; chmod 700 {destination}; sync"), f"secure {source.name}")
            staged[source.name] = digest(source)
        return {"payload_digests": staged}

    @staticmethod
    def _must_succeed(result: CommandResult, label: str) -> dict[str, str]:
        if result.returncode:
            raise LabError(f"{label} failed: {result.stderr or result.stdout}")
        return {"command": label, "output": result.stdout}

    def prepare(self) -> None:
        if self.state.get("cleaned"):
            raise LabError("a cleaned session cannot be prepared again; start a new session")
        self.preflight()
        self.phase("stage-payloads", self.stage_payloads)
        self.verify_remote_owner("prepare.sh")
        def prepare_payload() -> dict[str, str]:
            verification = f"BB=/data/adb/magisk/busybox; test -s {self.remote_root}/initial-state && test \"$($BB stat -c %u:%a {self.remote_root}/initial-state)\" = 0:600"
            result = self.root_shell(verification)
            if result.returncode:
                self._must_succeed(self.run_remote_payload("prepare.sh"), "prepare payload")
                result = self.root_shell(verification)
            return self._must_succeed(result, "verify captured initial state")
        self.phase("prepare", prepare_payload)

    def stage_external(self) -> None:
        """Stage an operator artifact without interpreting or installing it."""

        if self.state.get("cleaned"):
            raise LabError("a cleaned session cannot stage an external artifact; start a new session")
        artifact = Path(self.args.artifact)
        if artifact.is_symlink() or not artifact.is_file() or not SAFE_ARTIFACT_NAME.fullmatch(artifact.name):
            raise LabError("--artifact must name a regular file with a safe basename")
        self.prepare()
        artifact_digest = digest(artifact)
        artifact_size = artifact.stat().st_size
        retained = bool(self.args.retain)
        destination_root = (
            f"/data/local/tmp/echo-device-lab-retained/{self.root.name}"
            if retained else f"{self.remote_root}/external"
        )
        destination = f"{destination_root}/{artifact_digest[:12]}-{artifact.name}"
        temporary = f"/data/local/tmp/.echo-device-lab-external-{self.root.name}-{artifact_digest[:12]}-{secrets.token_hex(5)}"

        def stage() -> dict[str, Any]:
            pushed = self.adb(("push", str(artifact), temporary))
            if pushed.returncode:
                self.root_shell(f"rm -f {temporary}")
                self._must_succeed(pushed, "push external artifact")
            try:
                script = f"umask 077; mkdir -p {destination_root}; chown root:root {destination_root}; chmod 700 {destination_root}; mv {temporary} {destination}; chown root:root {destination}; chmod 600 {destination}; sync"
                self._must_succeed(self.root_shell(script), "secure external artifact")
            except LabError:
                self.root_shell(f"rm -f {temporary}")
                raise
            verified = self._must_succeed(self.root_shell(f"BB=/data/adb/magisk/busybox; $BB sha256sum {destination}; $BB stat -c %u:%a:%s {destination}"), "verify external artifact")
            lines = verified["output"].splitlines()
            remote_digest = lines[0].split(maxsplit=1)[0] if lines else ""
            remote_properties = lines[1] if len(lines) > 1 else ""
            if remote_digest != artifact_digest or remote_properties != f"0:600:{artifact_size}":
                self.root_shell(f"rm -f {destination}")
                raise LabError("external artifact changed or was corrupted during staging")
            return {"artifact_basename": artifact.name, "artifact_size": artifact_size, "artifact_sha256": artifact_digest, "remote_path": destination, "retained": retained}

        lifetime = "retained" if retained else "session"
        self.phase(f"stage-external-{artifact_digest[:12]}-{lifetime}", stage)

    def record_external_action(self) -> None:
        """Append read-only checkpoints around an action performed outside device-lab."""

        prepared = any(phase.get("name") == "prepare" and phase.get("status") == "passed" for phase in self.evidence.get("phases", []))
        if not isinstance(self.state.get("initial_device_state"), dict) or not prepared:
            raise LabError("record-external-action requires a prepared resumed session")
        if self.evidence.get("serial") != self.args.serial:
            raise LabError("record-external-action serial does not match the resumed session")
        if not SAFE_PROBE_PATH.fullmatch(self.args.hook_path):
            raise LabError("--hook-path must be beneath a qualified Magisk service directory")
        if self.args.metadata_path != DEFAULT_METADATA_PATH:
            raise LabError(f"--metadata-path must be {DEFAULT_METADATA_PATH}")
        self.acquire_host_lock()
        observation = {
            "action": self.args.action,
            "checkpoint": self.args.checkpoint,
            "installed_agent_digest": self.installed_digest(),
            "hook_path": self.args.hook_path,
            "hook_status": self._path_status(self.args.hook_path),
            "metadata_path": self.args.metadata_path,
            "metadata_status": self._path_status(self.args.metadata_path),
        }
        actions = self.state.setdefault("external_actions", {})
        if self.args.checkpoint in actions.get(self.args.action, {}):
            raise LabError("external action checkpoint is already recorded; start a new session for another observation")
        actions.setdefault(self.args.action, {})[self.args.checkpoint] = observation
        self._save()
        self.phase(f"external-action-{self.args.action}-{self.args.checkpoint}", lambda: observation)

    def installed_digest(self) -> str:
        output = self._must_succeed(self.root_shell("/data/adb/magisk/busybox sha256sum /data/local/bin/echod"), "read installed-agent digest")["output"]
        value = output.split(maxsplit=1)[0]
        if not re.fullmatch(r"[0-9a-f]{64}", value):
            raise LabError("could not read installed-agent digest")
        return value

    def _path_status(self, path: str) -> str:
        result = self.root_shell(f"if test -f {path}; then echo present; elif test -e {path}; then echo non_regular; else echo absent; fi")
        return self._must_succeed(result, f"probe {path}")["output"].strip()

    def cleanup(self) -> None:
        self.acquire_host_lock()
        # Resume may follow a harness repair; refresh only the token-owned
        # payloads before executing cleanup, never device/product files.
        self.stage_payloads()
        self.verify_remote_owner("cleanup.sh")
        self.phase("cleanup", lambda: self._must_succeed(self.run_remote_payload("cleanup.sh"), "cleanup payload"))
        self.release_host_lock()
        self.state["cleaned"] = True
        self._save()

    def verify_clean(self) -> None:
        self.acquire_host_lock()
        initial = self.state.get("initial_device_state")
        if not isinstance(initial, dict) or not isinstance(initial.get("installed_agent_digest"), str):
            raise LabError("verify-clean requires captured initial device state")
        current = self.installed_digest()
        if current != initial["installed_agent_digest"]:
            drift = {"expected_agent_sha256": initial["installed_agent_digest"], "observed_agent_sha256": current, "recovery": "No automatic recovery was attempted. Use ADB with echoctl update install and a known-good signed compatible artifact."}
            self.evidence["checks"].append({"name": "installed-agent-digest", "status": "failed", "observation": drift, "provenance": "hardware"})
            self._save()
            raise LabError(f"installed-agent digest changed during diagnostics (expected {initial['installed_agent_digest']}, observed {current}); {drift['recovery']}")
        absent = self.root_shell(f"test ! -e {self.remote_root}")
        self._must_succeed(absent, "verify removal of token-owned diagnostic root")
        residual = self.root_shell(f"BB=/data/adb/magisk/busybox; for p in /proc/[0-9]*/cmdline; do test -r \"$p\" || continue; text=$($BB tr '\\000' ' ' < \"$p\" 2>/dev/null || true); case \"$text\" in *'{self.remote_root}'*) exit 71;; esac; done")
        self._must_succeed(residual, "verify no token-owned process remains")
        self.phase("verify-clean", lambda: {"installed_agent_digest": current, "remote_root": "absent", "owned_processes": "absent"})
        self.release_host_lock()


def render(evidence: dict[str, Any]) -> str:
    assert_safe_evidence(evidence)
    lines = ["# Echo Dot device-lab evidence", "", f"- Session: `{evidence['session_id']}`", f"- Serial: `{evidence['serial']}`", "", "## Phases", ""]
    for phase in evidence.get("phases", []):
        lines.append(f"- {phase['name']}: {phase['status']} ({phase['duration_ms']} ms; {phase['provenance']})")
    return "\n".join(lines) + "\n"


def parser() -> argparse.ArgumentParser:
    argument_parser = argparse.ArgumentParser(description=__doc__)
    argument_parser.add_argument("command", choices=COMMANDS)
    argument_parser.add_argument("--adb", required=True)
    argument_parser.add_argument("--serial", required=True)
    argument_parser.add_argument("--resume")
    argument_parser.add_argument("--artifact")
    argument_parser.add_argument("--retain", action="store_true")
    argument_parser.add_argument("--action", choices=("bootstrap", "install"))
    argument_parser.add_argument("--checkpoint", choices=("before", "after"))
    argument_parser.add_argument("--hook-path", default=DEFAULT_HOOK_PATH)
    argument_parser.add_argument("--metadata-path", default=DEFAULT_METADATA_PATH)
    return argument_parser


def main(argv: Sequence[str] | None = None) -> int:
    args = parser().parse_args(argv)
    if not args.serial.strip():
        parser().error("--serial must not be blank")
    if not SAFE_SERIAL.fullmatch(args.serial):
        parser().error("--serial contains unsafe path characters")
    if args.command == "render-evidence":
        if not args.resume:
            parser().error("render-evidence requires --resume")
        root = Path(args.resume)
        print(render(json.loads((root / "evidence.json").read_text(encoding="utf-8"))), end="")
        return 0
    if args.command == "stage-external" and not args.artifact:
        parser().error("stage-external requires --artifact")
    if args.command == "record-external-action" and (not args.action or not args.checkpoint):
        parser().error("record-external-action requires --action and --checkpoint")
    if args.command == "record-external-action" and not args.resume:
        parser().error("record-external-action requires --resume for a prepared session")
    try:
        runner = Runner(args)
        getattr(runner, args.command.replace("-", "_"))()
    except LabError as error:
        print(f"device-lab: {error}", file=sys.stderr)
        return 2
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
