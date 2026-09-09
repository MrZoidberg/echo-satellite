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
