#!/usr/bin/env -S uv run --no-project --script
# /// script
# requires-python = ">=3.11"
# dependencies = []
# ///
"""Standard-library tests for the repository agent-loop hook dispatcher."""

from __future__ import annotations

import concurrent.futures
import importlib.util
import io
import json
import os
import subprocess
import sys
import tempfile
import unittest
from contextlib import redirect_stdout
from pathlib import Path
from unittest import mock


SCRIPT = Path(__file__).with_name("agent_loop.py")
SPEC = importlib.util.spec_from_file_location("agent_loop", SCRIPT)
assert SPEC is not None and SPEC.loader is not None
agent_loop = importlib.util.module_from_spec(SPEC)
sys.modules[SPEC.name] = agent_loop
SPEC.loader.exec_module(agent_loop)


class PostToolUseTests(unittest.TestCase):
    def setUp(self) -> None:
        self.temporary_directory = tempfile.TemporaryDirectory()
        self.root = Path(self.temporary_directory.name)
        subprocess.run(("git", "init", "-q", str(self.root)), check=True)
        self.session_id = "session-test"

    def tearDown(self) -> None:
        self.temporary_directory.cleanup()

    def write(self, relative_path: str, contents: str = "package example\n") -> Path:
        path = self.root / relative_path
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(contents, encoding="utf-8")
        return path

    def event(self, patch: str) -> dict[str, object]:
        return {
            "hook_event_name": "PostToolUse",
            "tool_name": "apply_patch",
            "tool_input": patch,
            "cwd": str(self.root),
            "session_id": self.session_id,
        }

    def journals(self) -> list[dict[str, object]]:
        directory = Path(
            subprocess.run(
                ("git", "rev-parse", "--git-path", "codex-hooks"),
                cwd=self.root,
                capture_output=True,
                check=True,
                text=True,
            ).stdout.strip()
        )
        if not directory.is_absolute():
            directory = self.root / directory
        return [
            record
            for path in directory.glob("*.json")
            if isinstance(record := json.loads(path.read_text(encoding="utf-8")), dict) and "session_id" in record
        ]

    def invoke(self, event: dict[str, object]) -> tuple[int, str]:
        output = io.StringIO()
        with redirect_stdout(output):
            result = agent_loop.handle_post_tool_use(event)
        return result, output.getvalue()

    def establish_baseline(self) -> None:
        agent_loop.write_baseline(self.root, self.session_id, agent_loop.session_snapshot(self.root))

    def test_exec_journals_only_files_changed_after_session_baseline(self) -> None:
        dirty = self.write("dirty.go", "package example\nfunc Dirty(){ }\n")
        changed = self.write("changed.go", "package example\nfunc Changed(){ }\n")
        self.establish_baseline()
        changed.write_text("package example\nfunc Changed() { }\n", encoding="utf-8")
        event = self.event("")
        event["tool_name"] = "exec"
        result, output = self.invoke(event)
        self.assertEqual(0, result, output)
        self.assertEqual("package example\nfunc Dirty(){ }\n", dirty.read_text())
        self.assertEqual("package example\n\nfunc Changed() {}\n", changed.read_text())
        self.assertEqual([{"paths": ["changed.go"], "session_id": self.session_id}], self.journals())

    def test_formats_added_updated_and_renamed_go_files(self) -> None:
        added = self.write("added.go", "package example\nfunc Added(){ }\n")
        updated = self.write("updated.go", "package example\nfunc Updated(){ }\n")
        renamed = self.write("renamed.go", "package example\nfunc Renamed(){ }\n")
        result, output = self.invoke(
            self.event(
                "*** Begin Patch\n*** Add File: added.go\n*** Update File: updated.go\n"
                "*** Update File: old.go\n*** Move to: renamed.go\n*** End Patch"
            )
        )
        self.assertEqual(0, result, output)
        self.assertEqual("package example\n\nfunc Added() {}\n", added.read_text())
        self.assertEqual("package example\n\nfunc Updated() {}\n", updated.read_text())
        self.assertEqual("package example\n\nfunc Renamed() {}\n", renamed.read_text())
        self.assertEqual(
            [{"paths": ["added.go", "updated.go", "renamed.go"], "session_id": self.session_id}],
            self.journals(),
        )

    def test_skips_deleted_missing_non_go_and_preserves_unrelated_dirty_file(self) -> None:
        dirty = self.write("dirty.go", "package example\nfunc Dirty(){ }\n")
        text = self.write("notes.txt", "untouched\n")
        self.write("deleted.go")
        (self.root / "deleted.go").unlink()
        result, output = self.invoke(
            self.event(
                "*** Begin Patch\n*** Delete File: deleted.go\n*** Update File: missing.go\n"
                "*** Update File: notes.txt\n*** End Patch"
            )
        )
        self.assertEqual(0, result, output)
        self.assertEqual("package example\nfunc Dirty(){ }\n", dirty.read_text())
        self.assertEqual("untouched\n", text.read_text())
        self.assertEqual([], self.journals())

    def test_accepts_windows_paths_and_spaces(self) -> None:
        path = self.write("directory with spaces/example.go", "package example\nfunc Space(){ }\n")
        result, output = self.invoke(
            self.event("*** Begin Patch\n*** Update File: directory with spaces\\example.go\n*** End Patch")
        )
        self.assertEqual(0, result, output)
        self.assertEqual("package example\n\nfunc Space() {}\n", path.read_text())
        self.assertEqual([{"paths": ["directory with spaces/example.go"], "session_id": self.session_id}], self.journals())

    def test_accepts_legacy_object_payloads(self) -> None:
        path = self.write("legacy.go", "package example\nfunc Legacy(){ }\n")
        event = self.event("unused")
        event["tool_input"] = {"patch": "*** Begin Patch\n*** Update File: legacy.go\n*** End Patch"}
        result, output = self.invoke(event)
        self.assertEqual(0, result, output)
        self.assertEqual("package example\n\nfunc Legacy() {}\n", path.read_text())

    def test_accepts_namespaced_apply_patch_tool_name(self) -> None:
        path = self.write("namespaced.go", "package example\nfunc Namespaced(){ }\n")
        event = self.event("*** Begin Patch\n*** Update File: namespaced.go\n*** End Patch")
        event["tool_name"] = "functions.apply_patch"
        result, output = self.invoke(event)
        self.assertEqual(0, result, output)
        self.assertEqual("package example\n\nfunc Namespaced() {}\n", path.read_text())
        self.assertEqual([{"paths": ["namespaced.go"], "session_id": self.session_id}], self.journals())

    def test_rejects_traversal_and_symlink_escape(self) -> None:
        outside = self.root.parent / "outside.go"
        outside.write_text("package outside\nfunc Outside(){ }\n", encoding="utf-8")
        link = self.root / "linked.go"
        link.symlink_to(outside)
        result, output = self.invoke(
            self.event("*** Begin Patch\n*** Update File: ../outside.go\n*** Update File: linked.go\n*** End Patch")
        )
        self.assertEqual(0, result, output)
        self.assertEqual("package outside\nfunc Outside(){ }\n", outside.read_text())
        self.assertEqual([], self.journals())

    def test_malformed_and_incomplete_payloads_do_not_write(self) -> None:
        path = self.write("safe.go", "package example\nfunc Safe(){ }\n")
        original_stdin = sys.stdin
        try:
            sys.stdin = io.StringIO("{")
            malformed_result, malformed_output = self._main_output()
        finally:
            sys.stdin = original_stdin
        incomplete_result, incomplete_output = self.invoke(
            {"hook_event_name": "PostToolUse", "tool_name": "apply_patch", "cwd": str(self.root)}
        )
        self.assertEqual(0, malformed_result)
        self.assertIn("malformed JSON", malformed_output)
        self.assertEqual(0, incomplete_result)
        self.assertIn("without patch text", incomplete_output)
        self.assertEqual("package example\nfunc Safe(){ }\n", path.read_text())
        self.assertEqual([], self.journals())

    def _main_output(self) -> tuple[int, str]:
        output = io.StringIO()
        with redirect_stdout(output):
            result = agent_loop.main()
        return result, output.getvalue()

    def test_formatter_failure_is_actionable(self) -> None:
        self.write("broken.go")
        with mock.patch.object(
            agent_loop,
            "run_command",
            side_effect=[
                subprocess.CompletedProcess(("git",), 0, str(self.root), ""),
                subprocess.CompletedProcess(("gofmt",), 1, "", "intentional failure"),
            ],
        ):
            result, output = self.invoke(self.event("*** Begin Patch\n*** Update File: broken.go\n*** End Patch"))
        self.assertEqual(1, result)
        response = json.loads(output)
        self.assertIn("systemMessage", response)
        self.assertIn("gofmt -w broken.go failed: intentional failure", response["systemMessage"])

    def test_concurrent_invocations_create_independent_journals(self) -> None:
        paths = [self.write(f"concurrent-{number}.go", "package example\nfunc F(){ }\n") for number in range(8)]

        def invoke(path: Path) -> tuple[int, str]:
            return self.invoke(self.event(f"*** Begin Patch\n*** Update File: {path.name}\n*** End Patch"))

        with concurrent.futures.ThreadPoolExecutor(max_workers=len(paths)) as executor:
            results = list(executor.map(invoke, paths))
        self.assertTrue(all(result == 0 and output == "" for result, output in results))
        records = self.journals()
        self.assertEqual(len(paths), len(records))
        self.assertEqual({path.name for path in paths}, {record["paths"][0] for record in records})

    def test_session_identity_cannot_escape_journal_directory(self) -> None:
        path = self.write("session.go", "package example\nfunc Session(){ }\n")
        event = self.event("*** Begin Patch\n*** Update File: session.go\n*** End Patch")
        event["session_id"] = "../../outside"
        result, output = self.invoke(event)
        self.assertEqual(0, result, output)
        self.assertEqual("package example\n\nfunc Session() {}\n", path.read_text())
        self.assertEqual([{"paths": ["session.go"], "session_id": "../../outside"}], self.journals())


class TaskContextAndStopTests(PostToolUseTests):
    def hook_event(self, event_name: str, **values: object) -> dict[str, object]:
        return {"hook_event_name": event_name, "cwd": str(self.root), "session_id": self.session_id, **values}

    def invoke_handler(self, handler: object, event: dict[str, object]) -> tuple[int, str]:
        output = io.StringIO()
        with redirect_stdout(output):
            result = handler(event)  # type: ignore[operator]
        return result, output.getvalue()

    def plan(self, lifecycle: str, name: str, contents: str) -> Path:
        return self.write(f"docs/plans/{lifecycle}/{name}", contents)

    def test_session_start_reports_compact_repository_context(self) -> None:
        self.plan("in-progress", "active.md", "# Active\n")
        self.write("dirty.txt", "dirty\n")
        result, output = self.invoke_handler(agent_loop.handle_session_start, self.hook_event("SessionStart"))
        self.assertEqual(0, result)
        response = json.loads(output)
        context = response["hookSpecificOutput"]
        self.assertEqual({"hookEventName": "SessionStart", "additionalContext": context["additionalContext"]}, context)
        self.assertIn("branch:", context["additionalContext"])
        self.assertIn("dirty.txt", context["additionalContext"])
        self.assertIn("active.md", context["additionalContext"])
        self.assertNotIn("# Active", context["additionalContext"])

    def test_task_resolution_extracts_only_requested_section_and_normalizes_windows_path(self) -> None:
        plan = self.plan(
            "in-progress",
            "example.md",
            "# Example\n\n## Task 1: First\n\none\n\n## Task 2: Second\n\ntwo\n\n## Risks\n\nthree\n",
        )
        result, output = self.invoke_handler(
            agent_loop.handle_user_prompt_submit,
            self.hook_event("UserPromptSubmit", prompt="Implement Task 1 from docs\\plans\\in-progress\\example.md."),
        )
        self.assertEqual(0, result)
        context = json.loads(output)["hookSpecificOutput"]
        self.assertEqual("UserPromptSubmit", context["hookEventName"])
        self.assertIn(plan.relative_to(self.root).as_posix(), context["additionalContext"])
        self.assertIn("## Task 1: First", context["additionalContext"])
        self.assertIn("one", context["additionalContext"])
        self.assertNotIn("Task 2", context["additionalContext"])
        self.assertNotIn("Risks", context["additionalContext"])

    def test_task_resolution_reports_ambiguous_and_missing_references(self) -> None:
        text = "# Example\n\n## Task 1: First\n\none\n"
        self.plan("future", "first.md", text)
        self.plan("finished", "second.md", text)
        result, output = self.invoke_handler(
            agent_loop.handle_user_prompt_submit, self.hook_event("UserPromptSubmit", prompt="Implement Task 1")
        )
        self.assertEqual(0, result)
        self.assertIn("could not uniquely resolve", output)
        result, output = self.invoke_handler(
            agent_loop.handle_user_prompt_submit,
            self.hook_event("UserPromptSubmit", prompt="Implement Task 3 from missing.md"),
        )
        self.assertEqual(0, result)
        self.assertIn("could not uniquely resolve", output)

    def test_stop_scopes_packages_expands_configuration_and_prevents_loop(self) -> None:
        self.write("internal/example/example.go")
        agent_loop.write_journal(self.root, self.session_id, [self.root / "internal/example/example.go"])
        original_run = agent_loop.run_command

        def successful_check(args: object, **kwargs: object) -> subprocess.CompletedProcess[str]:
            command = tuple(args)  # type: ignore[arg-type]
            if command[0] == "git":
                return original_run(command, **kwargs)  # type: ignore[arg-type]
            return subprocess.CompletedProcess(command, 0, "ok\n" if command[0] == "go" else "", "")

        with mock.patch.object(agent_loop, "run_command", side_effect=successful_check) as command:
            result, output = self.invoke_handler(agent_loop.handle_stop, self.hook_event("Stop"))
        self.assertEqual(0, result)
        self.assertIn("./internal/example", output)
        checked = [tuple(call.args[0]) for call in command.call_args_list if call.args[0][0] != "git"]
        self.assertEqual([("gofmt", "-l", "internal/example/example.go"), ("golangci-lint", "run", "./internal/example"), ("go", "test", "-race", "./internal/example")], checked)
        go_mod = self.write("go.mod", "module example\n\ngo 1.25\n")
        post_tool_event = self.event("*** Begin Patch\n*** Update File: go.mod\n*** End Patch")
        result, output = self.invoke(post_tool_event)
        self.assertEqual(0, result, output)
        self.assertIn("go.mod", {path for record in self.journals() for path in record["paths"]})
        with mock.patch.object(agent_loop, "run_command", side_effect=successful_check) as command:
            result, output = self.invoke_handler(agent_loop.handle_stop, self.hook_event("Stop"))
        self.assertEqual(0, result)
        self.assertIn("repository-wide", output)
        self.assertIn(("golangci-lint", "run", "./..."), [tuple(call.args[0]) for call in command.call_args_list if call.args[0][0] != "git"])
        result, output = self.invoke_handler(agent_loop.handle_stop, self.hook_event("Stop", stop_hook_active=True))
        self.assertEqual(0, result)
        self.assertIn("not retrying", output)

    def test_stop_falls_back_to_session_baseline_when_post_tool_use_is_unavailable(self) -> None:
        path = self.write("internal/example/example.go", "package example\n")
        self.establish_baseline()
        path.write_text("package example\n\n// Changed after SessionStart.\n", encoding="utf-8")
        original_run = agent_loop.run_command

        def successful_check(args: object, **kwargs: object) -> subprocess.CompletedProcess[str]:
            command = tuple(args)  # type: ignore[arg-type]
            if command[0] == "git":
                return original_run(command, **kwargs)  # type: ignore[arg-type]
            return subprocess.CompletedProcess(command, 0, "", "")

        with mock.patch.object(agent_loop, "run_command", side_effect=successful_check) as command:
            result, output = self.invoke_handler(agent_loop.handle_stop, self.hook_event("Stop"))
        self.assertEqual(0, result, output)
        self.assertIn("./internal/example", output)
        checked = [tuple(call.args[0]) for call in command.call_args_list if call.args[0][0] != "git"]
        self.assertEqual(
            [
                ("gofmt", "-l", "internal/example/example.go"),
                ("golangci-lint", "run", "./internal/example"),
                ("go", "test", "-race", "./internal/example"),
            ],
            checked,
        )

    def test_deleted_repository_scope_configuration_expands_stop_gate(self) -> None:
        original_run = agent_loop.run_command

        def successful_check(args: object, **kwargs: object) -> subprocess.CompletedProcess[str]:
            command = tuple(args)  # type: ignore[arg-type]
            if command[0] == "git":
                return original_run(command, **kwargs)  # type: ignore[arg-type]
            return subprocess.CompletedProcess(command, 0, "", "")

        for configuration in sorted(agent_loop.REPOSITORY_SCOPE_FILES):
            with self.subTest(configuration=configuration):
                path = self.write(configuration, "configuration\n")
                path.unlink()
                result, output = self.invoke(
                    self.event(f"*** Begin Patch\n*** Delete File: {configuration}\n*** End Patch")
                )
                self.assertEqual(0, result, output)
                self.assertIn(configuration, {path for record in self.journals() for path in record["paths"]})
                with mock.patch.object(agent_loop, "run_command", side_effect=successful_check) as command:
                    result, output = self.invoke_handler(agent_loop.handle_stop, self.hook_event("Stop"))
                self.assertEqual(0, result, output)
                self.assertIn("repository-wide", output)
                checked = [tuple(call.args[0]) for call in command.call_args_list if call.args[0][0] != "git"]
                self.assertEqual(
                    [("golangci-lint", "run", "./..."), ("go", "test", "-race", "./...")],
                    checked,
                )
                self.invoke_handler(agent_loop.handle_session_end, self.hook_event("SessionEnd"))

    def test_stop_requests_one_continuation_and_session_end_only_removes_own_records(self) -> None:
        path = self.write("example.go")
        agent_loop.write_journal(self.root, self.session_id, [path])
        agent_loop.write_journal(self.root, "another-session", [path])
        failed = subprocess.CompletedProcess(("go",), 1, "", "permission denied")
        original_run = agent_loop.run_command

        def failed_check(args: object, **kwargs: object) -> subprocess.CompletedProcess[str]:
            command = tuple(args)  # type: ignore[arg-type]
            if command[0] == "git":
                return original_run(command, **kwargs)  # type: ignore[arg-type]
            return failed

        with mock.patch.object(agent_loop, "run_command", side_effect=failed_check):
            result, output = self.invoke_handler(agent_loop.handle_stop, self.hook_event("Stop"))
        self.assertEqual(0, result)
        response = json.loads(output)
        self.assertEqual("block", response["decision"])
        self.assertIn("rerun normally", response["reason"])
        result, output = self.invoke_handler(agent_loop.handle_session_end, self.hook_event("SessionEnd"))
        self.assertEqual(0, result, output)
        self.assertEqual([{"paths": ["example.go"], "session_id": "another-session"}], self.journals())

    def test_session_end_removes_its_baseline(self) -> None:
        self.establish_baseline()
        self.assertTrue(agent_loop.baseline_path(self.root, self.session_id).exists())
        result, output = self.invoke_handler(agent_loop.handle_session_end, self.hook_event("SessionEnd"))
        self.assertEqual(0, result, output)
        self.assertFalse(agent_loop.baseline_path(self.root, self.session_id).exists())


if __name__ == "__main__":
    arguments = sys.argv[1:]
    suite = unittest.defaultTestLoader.loadTestsFromName(arguments[0], module=sys.modules[__name__]) if arguments else unittest.defaultTestLoader.loadTestsFromModule(sys.modules[__name__])
    result = unittest.TextTestRunner(verbosity=2).run(suite)
    raise SystemExit(not result.wasSuccessful())
