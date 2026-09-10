#!/usr/bin/env -S uv run --no-project --script
# /// script
# requires-python = ">=3.11"
# dependencies = []
# ///
"""Focused standard-library tests for the device-lab safety core."""

from __future__ import annotations

import importlib.util
import argparse
import json
import sys
import tempfile
import unittest
from pathlib import Path


SPEC = importlib.util.spec_from_file_location("device_lab", Path(__file__).with_name("device_lab.py"))
assert SPEC and SPEC.loader
device_lab = importlib.util.module_from_spec(SPEC)
sys.modules[SPEC.name] = device_lab
SPEC.loader.exec_module(device_lab)


class CoreTests(unittest.TestCase):
    def test_redacts_secret_fields_and_rejects_signed_urls(self) -> None:
        self.assertEqual({"token": "[redacted]", "serial": "dot"}, device_lab.redact({"token": "value", "serial": "dot"}))
        with self.assertRaises(device_lab.LabError):
            device_lab.redact("https://gateway.example/artifact?token=value")

    def test_atomic_evidence_and_deterministic_renderer(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            evidence = {"schema_version": 1, "session_id": "test", "serial": "dot", "phases": [{"name": "preflight", "status": "passed", "duration_ms": 1, "provenance": "simulated"}]}
            path = Path(directory) / "evidence.json"
            device_lab.atomic_json(path, evidence)
            self.assertEqual(evidence, json.loads(path.read_text(encoding="utf-8")))
            self.assertIn("preflight: passed", device_lab.render(evidence))

    def test_runner_keeps_token_in_private_state_not_evidence(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            previous_parent = device_lab.SESSION_PARENT
            device_lab.SESSION_PARENT = Path(directory) / "device-lab"
            root = device_lab.SESSION_PARENT / "session"
            root.mkdir(parents=True)
            arguments = argparse.Namespace(adb="adb", serial="dot", resume=str(root))
            runner = device_lab.Runner(arguments)
            self.assertIn("ownership_token", json.loads(runner.state_path.read_text(encoding="utf-8")))
            self.assertNotIn("ownership_token", runner.evidence_path.read_text(encoding="utf-8"))
            device_lab.SESSION_PARENT = previous_parent

    def test_rejects_broad_remote_root_and_blank_serial(self) -> None:
        self.assertIsNone(device_lab.SAFE_REMOTE_ROOT.fullmatch("/data/local/tmp"))
        with self.assertRaises(SystemExit):
            device_lab.main(["preflight", "--adb", "adb", "--serial", " "])
        with self.assertRaises(SystemExit):
            device_lab.main(["preflight", "--adb", "adb", "--serial", "../../other"])

    def test_resume_must_be_owned_session_child(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            with self.assertRaises(device_lab.LabError):
                device_lab.Runner(argparse.Namespace(adb="adb", serial="dot", resume=directory))

    def test_atomic_state_fsyncs_directory(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "state.json"
            device_lab.atomic_state(path, {"ownership_token": "private"})
            self.assertEqual({"ownership_token": "private"}, json.loads(path.read_text(encoding="utf-8")))

    def test_root_shell_stages_a_single_su_target(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            runner = object.__new__(device_lab.Runner)
            runner.root = Path(directory)
            captured = []
            runner.adb = lambda arguments: captured.append(arguments) or device_lab.CommandResult(0, "", "")
            runner.root_shell("printf 'owned token' > /safe/path")
            self.assertEqual("push", captured[0][0])
            self.assertEqual("shell", captured[1][0])
            self.assertTrue(captured[1][1].startswith("su -c '/data/local/tmp/.echo-device-lab-root-"))

    def test_lock_conflict_prints_canonical_owned_resume(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            parent = Path(directory) / "device-lab"
            owner = parent / "owner-session"
            owner.mkdir(parents=True)
            (owner / "state.json").write_text(json.dumps({"ownership_token": "owner-token"}), encoding="utf-8")
            (owner / "evidence.json").write_text(json.dumps({"serial": "dot"}), encoding="utf-8")
            runner = object.__new__(device_lab.Runner)
            runner.root = parent / "new-session"
            runner.root.mkdir()
            runner.args = argparse.Namespace(serial="dot")
            runner.token = "new-token"
            lock = parent / "dot.lock"
            lock.write_text("owner-token", encoding="ascii")
            with self.assertRaisesRegex(device_lab.LabError, r"--resume \.bin/device-lab/owner-session"):
                runner.acquire_host_lock()

    def test_capability_probe_is_observational(self) -> None:
        runner = object.__new__(device_lab.Runner)
        runner.root_shell = lambda _script: device_lab.CommandResult(
            0,
            "busybox_path=/data/adb/magisk/busybox\nbusybox_version=BusyBox v1\nbusybox_cmp=available\nbusybox_sed=available\nbusybox_awk=available\nbusybox_sha256sum=available\nsystem_cmp=available\nsystem_sed=missing\nsystem_awk=missing\nsystem_sha256sum=available\nsystem_cmp_s=rejected\nbusybox_cmp_s=supported\n",
            "",
        )
        capabilities = runner.command_capabilities()
        self.assertEqual("rejected", capabilities["system_cmp_s"])
        self.assertEqual("supported", capabilities["busybox_cmp_s"])
        self.assertEqual("missing", capabilities["system_sed"])
        self.assertEqual("missing", capabilities["system_awk"])
        self.assertEqual("BusyBox v1", capabilities["busybox_version"])

    def test_stage_external_records_identity_without_installing(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            artifact = Path(directory) / "agent.bin"
            artifact.write_bytes(b"operator artifact")
            runner = object.__new__(device_lab.Runner)
            runner.args = argparse.Namespace(artifact=str(artifact), retain=False)
            runner.root = Path(directory) / "session"
            runner.root.mkdir()
            runner.state = {}
            runner.remote_root = "/data/local/tmp/echo-device-lab/session"
            prepared = []
            runner.prepare = lambda: prepared.append(True)
            captured = []
            runner.adb = lambda arguments: captured.append(arguments) or device_lab.CommandResult(0, "", "")
            runner.root_shell = lambda script: device_lab.CommandResult(
                0,
                f"{device_lab.digest(artifact)}  artifact\n0:600:{artifact.stat().st_size}\n" if "$BB sha256sum" in script else "",
                "",
            )
            observations = []
            runner.phase = lambda _name, action: observations.append(action())
            runner.stage_external()
            self.assertEqual([True], prepared)
            self.assertEqual("push", captured[0][0])
            self.assertIn("/data/local/tmp/.echo-device-lab-external-session-", captured[0][-1])
            self.assertTrue(observations[0]["remote_path"].startswith(runner.remote_root + "/external/"))
            self.assertFalse(observations[0]["retained"])
            runner.args.retain = True
            runner.stage_external()
            self.assertEqual([True, True], prepared)
            self.assertNotEqual(captured[0][-1], captured[1][-1])
            self.assertTrue(observations[1]["remote_path"].startswith("/data/local/tmp/echo-device-lab-retained/session/"))
            self.assertTrue(observations[1]["retained"])

    def test_external_action_records_read_only_checkpoints(self) -> None:
        runner = object.__new__(device_lab.Runner)
        runner.args = argparse.Namespace(action="install", checkpoint="after", hook_path=device_lab.DEFAULT_HOOK_PATH, metadata_path=device_lab.DEFAULT_METADATA_PATH, serial="dot")
        runner.state = {"initial_device_state": {"installed_agent_digest": "a" * 64}}
        runner.evidence = {"serial": "dot", "phases": [{"name": "prepare", "status": "passed"}]}
        runner.acquire_host_lock = lambda: None
        runner.installed_digest = lambda: "a" * 64
        runner._path_status = lambda path: "present" if path.endswith(".sh") else "absent"
        runner._save = lambda: None
        recorded = []
        runner.phase = lambda _name, action: recorded.append(action())
        runner.record_external_action()
        self.assertEqual("a" * 64, runner.state["external_actions"]["install"]["after"]["installed_agent_digest"])
        self.assertEqual("present", recorded[0]["hook_status"])
        with self.assertRaisesRegex(device_lab.LabError, "already recorded"):
            runner.record_external_action()

    def test_external_action_rejects_unprepared_or_cross_serial_session(self) -> None:
        runner = object.__new__(device_lab.Runner)
        runner.args = argparse.Namespace(action="install", checkpoint="before", hook_path=device_lab.DEFAULT_HOOK_PATH, metadata_path=device_lab.DEFAULT_METADATA_PATH, serial="dot")
        runner.state = {"initial_device_state": {"installed_agent_digest": "a" * 64}}
        runner.evidence = {"serial": "dot", "phases": [{"name": "preflight-initial-state", "status": "passed"}]}
        with self.assertRaisesRegex(device_lab.LabError, "prepared resumed"):
            runner.record_external_action()
        runner.evidence["phases"].append({"name": "prepare", "status": "passed"})
        runner.evidence["serial"] = "other-dot"
        with self.assertRaisesRegex(device_lab.LabError, "serial does not match"):
            runner.record_external_action()

    def test_external_staging_rejects_transfer_mismatch_and_symlinks(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            artifact = Path(directory) / "agent.bin"
            artifact.write_bytes(b"operator artifact")
            runner = object.__new__(device_lab.Runner)
            runner.args = argparse.Namespace(artifact=str(artifact), retain=False)
            runner.root = Path(directory) / "session"
            runner.root.mkdir()
            runner.state = {}
            runner.remote_root = "/data/local/tmp/echo-device-lab/session"
            runner.prepare = lambda: None
            runner.adb = lambda _arguments: device_lab.CommandResult(0, "", "")
            scripts = []
            runner.root_shell = lambda script: scripts.append(script) or device_lab.CommandResult(0, f"{device_lab.digest(artifact)}  artifact\n0:644:{artifact.stat().st_size}\n", "")
            runner.phase = lambda _name, action: action()
            with self.assertRaisesRegex(device_lab.LabError, "corrupted during staging"):
                runner.stage_external()
            self.assertTrue(any(script.startswith("rm -f /data/local/tmp/echo-device-lab/session/external/") for script in scripts))
            link = Path(directory) / "link.bin"
            link.symlink_to(artifact)
            runner.args.artifact = str(link)
            with self.assertRaisesRegex(device_lab.LabError, "regular file"):
                runner.stage_external()

    def test_digest_drift_records_hashes_and_recovery_without_repair(self) -> None:
        runner = object.__new__(device_lab.Runner)
        runner.state = {"initial_device_state": {"installed_agent_digest": "a" * 64}}
        runner.evidence = {"checks": []}
        runner.installed_digest = lambda: "b" * 64
        runner._save = lambda: None
        runner.acquire_host_lock = lambda: None
        with self.assertRaisesRegex(device_lab.LabError, "No automatic recovery"):
            runner.verify_clean()
        observation = runner.evidence["checks"][0]["observation"]
        self.assertEqual("a" * 64, observation["expected_agent_sha256"])
        self.assertEqual("b" * 64, observation["observed_agent_sha256"])

    def test_new_commands_validate_required_options(self) -> None:
        with self.assertRaises(SystemExit):
            device_lab.main(["stage-external", "--adb", "adb", "--serial", "dot"])
        with self.assertRaises(SystemExit):
            device_lab.main(["record-external-action", "--adb", "adb", "--serial", "dot"])


def _walk(suite: unittest.TestSuite):
    for item in suite:
        if isinstance(item, unittest.TestSuite):
            yield from _walk(item)
        else:
            yield item


if __name__ == "__main__":
    arguments = sys.argv[1:]
    if arguments[:1] == ["-k"]:
        pattern = arguments[1] if len(arguments) > 1 else ""
        suite = unittest.defaultTestLoader.loadTestsFromModule(sys.modules[__name__])
        selected = unittest.TestSuite(test for test in _walk(suite) if pattern.lower() in test.id().lower())
        result = unittest.TextTestRunner(verbosity=2).run(selected)
        raise SystemExit(not result.wasSuccessful())
    unittest.main()
