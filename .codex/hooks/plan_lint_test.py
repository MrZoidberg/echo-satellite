#!/usr/bin/env -S uv run --no-project --script
# /// script
# requires-python = ">=3.11"
# dependencies = []
# ///
"""Tests for deterministic plan lifecycle validation."""

from __future__ import annotations

import importlib.util
import sys
import tempfile
import unittest
from pathlib import Path


SPEC = importlib.util.spec_from_file_location("plan_lint", Path(__file__).with_name("plan_lint.py"))
assert SPEC and SPEC.loader
plan_lint = importlib.util.module_from_spec(SPEC)
sys.modules[SPEC.name] = plan_lint
SPEC.loader.exec_module(plan_lint)


class PlanLintTests(unittest.TestCase):
    def test_detects_lifecycle_and_owner_errors(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "future" / "plan.md"
            path.parent.mkdir()
            path.write_text("# Plan\n\n**Status:** in-progress\n\n### Task 1: x\n\n## Completion evidence\n", encoding="utf-8")
            errors = plan_lint.lint(path)
            self.assertIn("status must match future/ lifecycle", errors)
            self.assertIn("owner is missing", errors)

    def test_accepts_minimal_active_plan(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "in-progress" / "plan.md"
            path.parent.mkdir()
            path.write_text("# Plan\n\n**Status:** in-progress\n**Owner or active agent:** agent\n\n### Task 1: x\n\n**Status:** in progress\n\n## Completion evidence\n", encoding="utf-8")
            self.assertEqual([], plan_lint.lint(path))

    def test_rejects_completed_task_without_verification_and_broken_link(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / "docs" / "plans" / "in-progress").mkdir(parents=True)
            path = root / "docs" / "plans" / "in-progress" / "plan.md"
            path.write_text("# Plan\n\n**Status:** in-progress\n**Owner or active agent:** agent\n\n### Task 1: x\n\n**Status:** completed 2026-09-09\n\n[missing](docs/nope.md)\n\n## Completion evidence\n", encoding="utf-8")
            errors = plan_lint.lint(path)
            self.assertIn("Task 1 completed without task verification", errors)
            self.assertIn("broken relative link: docs/nope.md", errors)

    def test_rejects_hardware_task_without_cleanup_or_provenance(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "in-progress" / "plan.md"
            path.parent.mkdir()
            path.write_text("# Plan\n\n**Status:** in-progress\n**Owner or active agent:** agent\n\n### Task 1: x\n\n**Status:** in progress\n\n**Hardware required:** yes\n\n**Verification:**\n\n--adb adb --serial dot\n\n## Completion evidence\n", encoding="utf-8")
            errors = plan_lint.lint(path)
            self.assertIn("Task 1 hardware requirement lacks cleanup", errors)
            self.assertIn("Task 1 hardware requirement lacks provenance", errors)


if __name__ == "__main__":
    unittest.main()
