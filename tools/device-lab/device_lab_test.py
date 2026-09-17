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

    def test_safe_failure_redacts_sensitive_diagnostic_output(self) -> None:
        self.assertEqual("ordinary failure", device_lab.safe_failure("ordinary failure"))
        self.assertEqual("[redacted diagnostic failure]", device_lab.safe_failure("token=not-for-evidence"))
        self.assertEqual("[redacted diagnostic failure]", device_lab.safe_failure("https://example.invalid/x?signed=value"))

    def test_result_bundle_requires_a_confined_manifest_and_no_raw_audio(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / "report.json").write_text('{"score": 1}\n', encoding="utf-8")
            (root / "manifest.json").write_text('{"artifacts":["report.json"]}\n', encoding="utf-8")
            result = device_lab.validate_result_bundle(root)
            self.assertEqual(["report.json"], result["artifacts"])
            (root / "capture.wav").write_bytes(b"raw")
            with self.assertRaisesRegex(device_lab.LabError, "undeclared"):
                device_lab.validate_result_bundle(root)
            (root / "capture.wav").unlink()
            (root / "manifest.json").write_text('{"artifacts":["../report.json"]}\n', encoding="utf-8")
            with self.assertRaisesRegex(device_lab.LabError, "manifest"):
                device_lab.validate_result_bundle(root)

    def test_result_bundle_rejects_links_and_sensitive_content(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / "report.txt").write_text("token=value\n", encoding="utf-8")
            (root / "manifest.json").write_text('{"artifacts":["report.txt"]}\n', encoding="utf-8")
            with self.assertRaisesRegex(device_lab.LabError, "prohibited"):
                device_lab.validate_result_bundle(root)
            (root / "report.txt").write_text("okay\n", encoding="utf-8")
            (root / "linked.txt").symlink_to(root / "report.txt")
            with self.assertRaisesRegex(device_lab.LabError, "unsafe"):
                device_lab.validate_result_bundle(root)

    def test_result_bundle_rejects_binary_declared_artifact(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / "report.txt").write_bytes(b"\xff\x00")
            (root / "manifest.json").write_text('{"artifacts":["report.txt"]}\n', encoding="utf-8")
            with self.assertRaisesRegex(device_lab.LabError, "unreadable"):
                device_lab.validate_result_bundle(root)

    def test_root_shell_stages_a_single_su_target(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            runner = object.__new__(device_lab.Runner)
            runner.root = Path(directory)
            captured = []
            runner.adb = lambda arguments: captured.append(arguments) or device_lab.CommandResult(0, "", "")
            runner.root_shell("printf 'owned token' > /safe/path")
            self.assertEqual("push", captured[0][0])
            self.assertEqual(("shell",), captured[1][:1])
            self.assertTrue(captured[1][1].startswith("chmod 700 /data/local/tmp/.echo-device-lab-root-"))
            self.assertEqual("shell", captured[2][0])
            self.assertTrue(captured[2][1].startswith("su -c '/data/local/tmp/.echo-device-lab-root-"))

    def test_windows_adb_translates_only_local_push_and_pull_paths(self) -> None:
        runner = object.__new__(device_lab.Runner)
        runner.args = argparse.Namespace(adb="C:/platform-tools/adb.exe", serial="dot")
        captured = []
        runner.host = lambda arguments: captured.append(arguments) or device_lab.CommandResult(0, "", "")
        original_os_name = device_lab.os.name
        device_lab.os.name = "posix"
        original_run = device_lab.subprocess.run
        device_lab.subprocess.run = lambda *_args, **_kwargs: argparse.Namespace(returncode=0, stdout="C:\\session\\payload.sh\n")
        try:
            runner.adb(("push", "/mnt/c/session/payload.sh", "/data/local/tmp/payload.sh"))
            runner.adb(("pull", "/data/local/tmp/result.json", "/mnt/c/session/result.json"))
            runner.adb(("shell", "id"))
        finally:
            device_lab.os.name = original_os_name
            device_lab.subprocess.run = original_run
        self.assertEqual("C:\\session\\payload.sh", captured[0][4])
        self.assertEqual("C:\\session\\payload.sh", captured[1][5])
        self.assertEqual("id", captured[2][4])

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
        scripts = []
        runner.root_shell = lambda script: scripts.append(script) or device_lab.CommandResult(
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
        self.assertIn("$BB printf", scripts[0])
        self.assertIn("$BB rm -f", scripts[0])

    def test_preflight_probe_reports_the_microphone_holder(self) -> None:
        runner = object.__new__(device_lab.Runner)
        runner.acquire_host_lock = lambda: None
        runner.phase = lambda _name, action: action()
        runner._save = lambda: None
        runner.state = {}
        runner.adb = lambda _arguments: device_lab.CommandResult(0, "device\n", "")
        runner.root_shell = lambda _script: device_lab.CommandResult(0, "a" * 64 + "  /data/local/bin/echod\nboot\nmic_holder=316:/data/local/bin/echod\n", "")
        with self.assertRaisesRegex(device_lab.LabError, r"316:/data/local/bin/echod"):
            runner.preflight()

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
        with self.assertRaises(SystemExit):
            device_lab.main(["run-payload", "--adb", "adb", "--serial", "dot"])

    def test_run_payload_rejects_unversioned_or_reserved_payloads(self) -> None:
        runner = object.__new__(device_lab.Runner)
        runner.args = argparse.Namespace(payload="../unsafe.sh")
        with self.assertRaisesRegex(device_lab.LabError, "version-controlled"):
            runner.run_payload()

    def test_run_payload_always_cleans_up_after_payload_failure(self) -> None:
        runner = object.__new__(device_lab.Runner)
        runner.args = argparse.Namespace(payload="task10_front_550mm.sh", diagnostic=None)
        calls = []
        runner.prepare = lambda: calls.append("prepare")
        runner.stage_diagnostic = lambda: ""
        runner.verify_remote_owner = lambda payload: calls.append("owner:" + payload)
        runner.run_remote_payload = lambda _payload, _diagnostic: device_lab.CommandResult(1, "", "payload failure")
        runner.cleanup = lambda: calls.append("cleanup")
        runner.verify_clean = lambda: calls.append("verify-clean")
        runner.restart_known_launcher = lambda: calls.append("restart")
        runner.phase = lambda _name, action: action()
        with self.assertRaisesRegex(device_lab.LabError, "payload failure"):
            runner.run_payload()
        self.assertEqual(["prepare", "owner:task10_front_550mm.sh", "cleanup", "verify-clean", "restart"], calls)
        runner.args.payload = "prepare.sh"
        with self.assertRaisesRegex(device_lab.LabError, "version-controlled"):
            runner.run_payload()

    def test_run_payload_attempts_every_restoration_step_after_cleanup_failure(self) -> None:
        runner = object.__new__(device_lab.Runner)
        runner.args = argparse.Namespace(payload="task10_front_550mm.sh", diagnostic=None)
        calls = []
        runner.prepare = lambda: calls.append("prepare")
        runner.stage_diagnostic = lambda: ""
        runner.verify_remote_owner = lambda _payload: None
        runner.run_remote_payload = lambda _payload, _diagnostic: device_lab.CommandResult(1, "", "payload failure")
        runner.cleanup = lambda: (_ for _ in ()).throw(device_lab.LabError("cleanup failure"))
        runner.verify_clean = lambda: calls.append("verify-clean")
        runner.restart_known_launcher = lambda: calls.append("restart")
        runner.phase = lambda _name, action: action()
        with self.assertRaisesRegex(device_lab.LabError, "cleanup failed"):
            runner.run_payload()
        self.assertEqual(["prepare", "verify-clean", "restart"], calls)

    def test_run_payload_cleans_up_after_keyboard_interrupt(self) -> None:
        runner = object.__new__(device_lab.Runner)
        runner.args = argparse.Namespace(payload="task10_front_550mm.sh", diagnostic=None)
        calls = []
        runner.prepare = lambda: calls.append("prepare")
        runner.stage_diagnostic = lambda: ""
        runner.verify_remote_owner = lambda _payload: calls.append("owner")
        runner.run_remote_payload = lambda _payload, _diagnostic: (_ for _ in ()).throw(KeyboardInterrupt())
        runner.cleanup = lambda: calls.append("cleanup")
        runner.verify_clean = lambda: calls.append("verify-clean")
        runner.restart_known_launcher = lambda: calls.append("restart")
        runner.phase = lambda _name, action: action()
        with self.assertRaisesRegex(device_lab.LabError, "payload runner failed"):
            runner.run_payload()
        self.assertEqual(["prepare", "owner", "cleanup", "verify-clean", "restart"], calls)

    def test_run_payload_completes_restoration_after_second_keyboard_interrupt(self) -> None:
        runner = object.__new__(device_lab.Runner)
        runner.args = argparse.Namespace(payload="task10_front_550mm.sh", diagnostic=None)
        calls = []
        runner.prepare = lambda: calls.append("prepare")
        runner.stage_diagnostic = lambda: ""
        runner.verify_remote_owner = lambda _payload: calls.append("owner")
        runner.run_remote_payload = lambda _payload, _diagnostic: (_ for _ in ()).throw(KeyboardInterrupt())
        runner.cleanup = lambda: (_ for _ in ()).throw(KeyboardInterrupt())
        runner.verify_clean = lambda: calls.append("verify-clean")
        runner.restart_known_launcher = lambda: calls.append("restart")
        runner.phase = lambda _name, action: action()
        with self.assertRaises(device_lab.LabError):
            runner.run_payload()
        self.assertEqual(["prepare", "owner", "verify-clean", "restart"], calls)

    def test_task10_front_550mm_payload_has_ordered_health_bound_captures(self) -> None:
        payload = Path(__file__).with_name("payloads") / "task10_front_550mm.sh"
        text = payload.read_text(encoding="utf-8")
        self.assertIn('ROOT=${ROOT:-${0%/*}}', text)
        self.assertIn(': "${ROOT:?device-lab ROOT is required}"', text)
        self.assertIn("BB=/data/adb/magisk/busybox", text)
        self.assertIn('"$BB" printf', text)
        self.assertIn("--channels all", text)
        self.assertIn("--health-out", text)
        self.assertIn("--state thinking --seconds 3 --clear", text)
        self.assertIn("--state listening --seconds 12 --clear", text)
        self.assertIn("sleep 1", text)
        self.assertIn("--position front --distance-mm 550", text)
        self.assertIn("mic scorecard --input \"$OUT/silence.wav\"", text)
        self.assertIn("'^  \"xruns\": 0,$' \"$OUT/silence.health.json\"", text)
        self.assertIn("'^  \"dropped_frames\": 0,$' \"$OUT/silence.health.json\"", text)
        self.assertIn('value + 0 > 0.001', text)
        self.assertIn('value + 0 > 80000000', text)
        self.assertIn('"$RESULTS/manifest.json"', text)
        self.assertIn('SCHEDULE="$OUT/schedule.txt"', text)
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            output = root / "task10-front-550mm"
            output.mkdir()
            (output / "schedule.txt").write_text("schedule\n", encoding="utf-8")
            for name in ("silence.scorecard.json", "normal-speech.comparison.json", "quiet-speech.comparison.json", "loud-speech.comparison.json"):
                (output / name).write_text("{}\n", encoding="utf-8")
            start = text.index('{"artifacts":')
            end = text.index("}' >", start) + 1
            (root / "manifest.json").write_text(text[start:end] + "\n", encoding="utf-8")
            self.assertEqual(5, len(device_lab.validate_result_bundle(root)["artifacts"]))
        for condition in ("normal-speech", "quiet-speech", "loud-speech"):
            self.assertIn(f'compare {condition} room-noise-{condition.removesuffix("-speech")} {condition}', text)
        ordered = (
            "record silence",
            "record room-noise-normal",
            "record_listening normal-speech",
            "record room-noise-quiet",
            "record_listening quiet-speech",
            "record room-noise-loud",
            "record_listening loud-speech",
        )
        offsets = [text.index(line) for line in ordered]
        self.assertEqual(offsets, sorted(offsets))

    def test_task11_command_audio_payload_stages_an_isolated_agent_and_no_raw_audio(self) -> None:
        payload = Path(__file__).with_name("payloads") / "task11_command_audio.sh"
        text = payload.read_text(encoding="utf-8")
        self.assertIn('ROOT=${ROOT:-${0%/*}}', text)
        self.assertIn('ECHOD=${DIAGNOSTIC:?device-lab DIAGNOSTIC is required}', text)
        self.assertIn('CONFIG=/data/local/etc/echo-satellite/echod.ini', text)
        self.assertIn('--config-state "$ROOT/config.json"', text)
        self.assertIn('--pairing-state "$ROOT/paired-gateway.json"', text)
        self.assertIn('--disable-updates', text)
        self.assertIn('--conditioning-profile dot-gen2-qualified-v1', text)
        self.assertIn('"$ROOT/task11-echod.pid"', text)
        self.assertIn('for trial in 01 02 03 04 05 06 07 08 09 10 11 12 13 14 15 16 17 18 19 20', text)
        self.assertIn('wait_phase "idle_music no_speech_to_device" 900', text)
        self.assertIn('raw_audio=disabled', text)
        self.assertIn('rm -f "$OUT/echod.stdout" "$OUT/echod.jsonl"', text)
        self.assertIn("trap 'stop_agent; exit 130' HUP INT TERM", text)
        self.assertIn('test "$tries" -lt 50', text)
        self.assertIn('staged agent did not exit after TERM', text)
        self.assertIn('"$RESULTS/manifest.json"', text)

    def test_known_launcher_release_accepts_shell_hook_without_trailing_arguments(self) -> None:
        runner = object.__new__(device_lab.Runner)
        runner.phase = lambda _name, action: action()
        runner._save = lambda: None
        runner.state = {}
        script = []
        runner.root_shell = lambda value: (script.append(value) or device_lab.CommandResult(0, "agent_pid=318\nlauncher_pid=2\nagent_executable=/data/local/bin/echod\n", ""))
        runner.release_known_launcher(["318:/data/local/bin/echod"])
        self.assertIn("'sh /sbin/.core/img/.core/service.d/echo-satellite.sh'", script[0])
        self.assertIn("'/system/bin/sh /sbin/.core/img/.core/service.d/echo-satellite.sh'", script[0])
        self.assertIn("launcher parent executable=$parent_exe command=$cmd", script[0])
        self.assertIn("/sbin/.core/mirror/bin/busybox", script[0])
        self.assertIn("launcher parent command is not recognized hook: $cmd", script[0])

    def test_verify_clean_restores_a_released_launcher(self) -> None:
        runner = object.__new__(device_lab.Runner)
        runner.acquire_host_lock = lambda: None
        runner.release_host_lock = lambda: None
        runner.phase = lambda _name, action: action()
        runner._save = lambda: None
        runner.args = argparse.Namespace(serial="dot")
        runner.remote_root = "/data/local/tmp/echo-device-lab/session"
        runner.state = {"initial_device_state": {"installed_agent_digest": "a" * 64}, "released_launcher": {"path": device_lab.DEFAULT_HOOK_PATH}}
        runner.installed_digest = lambda: "a" * 64
        runner.root_shell = lambda script: device_lab.CommandResult(0, "absent\n" if "cwd_holder" in script else "", "")
        calls = []
        runner.restart_known_launcher = lambda: calls.append("restart")
        runner.verify_clean()
        self.assertEqual(["restart"], calls)

    def test_launcher_restoration_noops_when_the_validated_agent_is_already_running(self) -> None:
        runner = object.__new__(device_lab.Runner)
        runner.acquire_host_lock = lambda: None
        runner.release_host_lock = lambda: None
        runner.phase = lambda _name, action: action()
        runner._save = lambda: None
        runner.args = argparse.Namespace(serial="dot")
        runner.state = {"released_launcher": {"path": device_lab.DEFAULT_HOOK_PATH}}
        scripts = []
        runner.root_shell = lambda script: (scripts.append(script) or device_lab.CommandResult(0, "launcher_status=already_running\nagent_pid=318\nagent_executable=/data/local/bin/echod\n", ""))
        runner.restart_known_launcher()
        self.assertIn("case $existing_count in 0)", scripts[0])
        self.assertIn("launcher_status=already_running", scripts[0])
        self.assertNotIn("launcher_pid=$!", scripts[0].split("case $existing_count in 0)", 1)[0])

    def test_verify_clean_reports_a_remaining_owned_root(self) -> None:
        runner = object.__new__(device_lab.Runner)
        runner.acquire_host_lock = lambda: None
        runner.release_host_lock = lambda: None
        runner._save = lambda: None
        runner.args = argparse.Namespace(serial="dot")
        runner.remote_root = "/data/local/tmp/echo-device-lab/session"
        runner.state = {"initial_device_state": {"installed_agent_digest": "a" * 64}}
        runner.installed_digest = lambda: "a" * 64
        runner.root_shell = lambda _script: device_lab.CommandResult(0, "root_kind=directory\ncwd_holder=123\n", "")
        with self.assertRaisesRegex(device_lab.LabError, "cwd_holder=123"):
            runner.verify_clean()

    def test_cleanup_after_interruption_removes_only_the_token_owned_residual_root(self) -> None:
        runner = object.__new__(device_lab.Runner)
        runner.acquire_host_lock = lambda: None
        runner.release_host_lock = lambda: None
        runner.phase = lambda _name, action: action()
        runner._save = lambda: None
        runner.args = argparse.Namespace(serial="dot")
        runner.remote_root = "/data/local/tmp/echo-device-lab/session"
        runner.token = "owner-token"
        runner.state = {"cleaned": True, "initial_device_state": {"installed_agent_digest": "a" * 64}}
        runner.installed_digest = lambda: "a" * 64
        scripts = []

        def root_shell(script):
            scripts.append(script)
            if script.startswith("test -f"):
                return device_lab.CommandResult(1, "", "")
            return device_lab.CommandResult(0, "root=removed\n", "")

        runner.root_shell = root_shell
        runner.cleanup()
        self.assertIn('test "$($BB cat "$root/.owner" 2>/dev/null)" = \'owner-token\'', scripts[-1])
        self.assertIn('$BB rm -rf "$root"', scripts[-1])

    def test_cleanup_detects_an_interrupted_remote_cleanup_from_missing_initial_state(self) -> None:
        runner = object.__new__(device_lab.Runner)
        runner.acquire_host_lock = lambda: None
        runner.release_host_lock = lambda: None
        runner._cleanup_residual_root = lambda: setattr(runner, "residual_cleaned", True)
        runner.args = argparse.Namespace(serial="dot")
        runner.remote_root = "/data/local/tmp/echo-device-lab/session"
        runner.state = {"initial_device_state": {"installed_agent_digest": "a" * 64}}
        runner.root_shell = lambda _script: device_lab.CommandResult(1, "", "")
        runner.cleanup()
        self.assertTrue(runner.residual_cleaned)


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
