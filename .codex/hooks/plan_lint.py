#!/usr/bin/env -S uv run --no-project --script
# /// script
# requires-python = ">=3.11"
# dependencies = []
# ///
"""Deterministic validator for repository implementation-plan lifecycle."""

from __future__ import annotations

import re
import sys
from pathlib import Path

LIFECYCLE = {"future": "future", "in-progress": "in-progress", "finished": "finished", "superseded": "superseded"}
TASK = re.compile(r"^### Task (\d+)[^\n]*\n(.*?)(?=^### Task \d+|\Z)", re.M | re.S)
LINK = re.compile(r"\[[^]]+\]\(([^)#]+)(?:#[^)]+)?\)")
TASK_STATUS = re.compile(r"(?:not started|in progress|completed \d{4}-\d{2}-\d{2}|blocked|superseded(?: by Task \d+)?)(?:\s.*)?$", re.I)


def repository_root(path: Path) -> Path:
    for candidate in (path.resolve(), *path.resolve().parents):
        if (candidate / "docs" / "plans").is_dir():
            return candidate
    return Path.cwd()


def lint(path: Path) -> list[str]:
    errors: list[str] = []
    text = path.read_text(encoding="utf-8")
    lifecycle = path.parent.name
    expected = LIFECYCLE.get(lifecycle)
    status = re.search(r"^\*\*Status:\*\*\s*(.+)$", text, re.M)
    if expected is None:
        errors.append("plan is outside a lifecycle directory")
    elif status is None or not status.group(1).strip().startswith(expected):
        errors.append(f"status must match {lifecycle}/ lifecycle")
    if not re.search(r"^\*\*Owner or active agent:\*\*\s*\S", text, re.M):
        errors.append("owner is missing")
    tasks = list(TASK.finditer(text))
    if not tasks:
        errors.append("numbered tasks are missing")
    for task in tasks:
        number, body = task.groups()
        task_status = re.search(r"^\*\*Status:\*\*\s*(.+)$", body, re.M | re.I)
        if task_status is None:
            errors.append(f"Task {number} status is missing")
            continue
        value = task_status.group(1).strip().lower()
        if not TASK_STATUS.fullmatch(value):
            errors.append(f"Task {number} has an invalid status")
        if value.startswith("completed") and "**Verification:**" not in body:
            errors.append(f"Task {number} completed without task verification")
        if value.startswith("completed") and not re.search(rf"Task {number}.*(?:passed|complete|evidence)", text.split("## Completion evidence", 1)[-1], re.I | re.S):
            errors.append(f"Task {number} completed without completion evidence")
        if "**Hardware required:** yes" in body:
            verification = body.split("**Verification:**", 1)[-1].lower()
            for term in ("--adb", "--serial", "cleanup", "provenance"):
                if term not in verification and term not in body.lower():
                    errors.append(f"Task {number} hardware requirement lacks {term}")
        if value.startswith("superseded") and "by task" not in value and "supersed" not in body.lower().replace(value, ""):
            errors.append(f"Task {number} supersession target is missing")
    root = repository_root(path)
    for target in LINK.findall(text):
        if target.startswith(("http:", "https:", "mailto:")):
            continue
        target_path = (root / target.lstrip("/")).resolve() if not target.startswith(".") else (path.parent / target).resolve()
        if not target_path.exists():
            errors.append(f"broken relative link: {target}")
    if "## Completion evidence" not in text:
        errors.append("completion evidence section is missing")
    return errors


def main() -> int:
    paths = [Path(argument) for argument in sys.argv[1:]]
    if not paths:
        print("usage: plan_lint.py docs/plans/<lifecycle>/<plan>.md", file=sys.stderr)
        return 2
    errors = [(path, error) for path in paths for error in lint(path)]
    for path, error in errors:
        print(f"{path}: {error}", file=sys.stderr)
    return int(bool(errors))


if __name__ == "__main__":
    raise SystemExit(main())
