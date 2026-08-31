#!/usr/bin/env -S uv run --no-project --script
# /// script
# requires-python = ">=3.11"
# dependencies = []
# ///
"""Codex project-hook dispatcher for the repository agent edit loop."""

from __future__ import annotations

import json
import os
import re
import secrets
import subprocess
import sys
from hashlib import sha256
from dataclasses import dataclass
from pathlib import Path, PurePosixPath, PureWindowsPath
from typing import Any, Iterable, Sequence


PATCH_HEADER = re.compile(r"^\*\*\* (Add|Update|Delete) File: (.+)$")
MOVE_TO_HEADER = re.compile(r"^\*\*\* Move to: (.+)$")
TASK_PROMPT = re.compile(r"\bimplement\s+task\s+(\d+)\b(?:\s+(?:from|in)\s+([^\n]+))?$", re.IGNORECASE)
TASK_HEADING = re.compile(r"^#{2,3}\s+Task\s+(\d+)(?:\s|:)", re.MULTILINE)
TOP_LEVEL_HEADING = re.compile(r"^#\s+", re.MULTILINE)
LIFECYCLE_DIRECTORIES = ("in-progress", "future", "finished", "superseded")
REPOSITORY_SCOPE_FILES = {"go.mod", "go.sum", "Makefile", ".golangci.yml"}


@dataclass(frozen=True)
class PatchPath:
    """A patch path and whether the patch declares it deleted."""

    path: str
    deleted: bool


def run_command(args: Sequence[str], *, cwd: Path | None = None) -> subprocess.CompletedProcess[str]:
    """Run a fixed command without invoking a shell."""

    return subprocess.run(args, cwd=cwd, capture_output=True, check=False, text=True)


def git_output(cwd: Path, *args: str) -> str:
    """Return one Git query result, or raise a concise operational error."""

    result = run_command(("git", *args), cwd=cwd)
    if result.returncode != 0:
        detail = result.stderr.strip() or result.stdout.strip() or "unknown Git error"
        raise RuntimeError(f"git {' '.join(args)} failed: {detail}")
    return result.stdout.strip()


def worktree_for(cwd: Path) -> Path:
    """Find the canonical worktree root for this hook invocation."""

    return Path(git_output(cwd, "rev-parse", "--show-toplevel")).resolve()


def journal_dir_for(worktree: Path) -> Path:
    """Find Git-private hook storage rather than creating worktree state."""

    git_path = Path(git_output(worktree, "rev-parse", "--git-path", "codex-hooks"))
    if not git_path.is_absolute():
        git_path = worktree / git_path
    return git_path.resolve()


def parse_patch_paths(patch: str) -> list[PatchPath]:
    """Extract file-header paths from an apply_patch payload without evaluation."""

    paths: list[PatchPath] = []
    for line in patch.splitlines():
        match = PATCH_HEADER.match(line)
        if match:
            paths.append(PatchPath(match.group(2), deleted=match.group(1) == "Delete"))
            continue
        match = MOVE_TO_HEADER.match(line)
        if match:
            paths.append(PatchPath(match.group(1), deleted=False))
    return paths


def normalized_relative_path(candidate: str, worktree: Path) -> Path | None:
    """Return an existing safe file inside *worktree*, or ``None``."""

    normalized = candidate.replace("\\", "/")
    if not normalized or PureWindowsPath(normalized).is_absolute():
        return None
    pure_path = PurePosixPath(normalized)
    if pure_path.is_absolute() or ".." in pure_path.parts:
        return None
    path = worktree.joinpath(*pure_path.parts)
    try:
        resolved = path.resolve(strict=True)
        resolved.relative_to(worktree)
    except (OSError, ValueError):
        return None
    if not resolved.is_file():
        return None
    return resolved


def eligible_go_paths(paths: Iterable[PatchPath], worktree: Path) -> list[Path]:
    """Deduplicate safe, changed Go files while ignoring deleted patch paths."""

    eligible: list[Path] = []
    seen: set[Path] = set()
    for patch_path in paths:
        if patch_path.deleted:
            continue
        path = normalized_relative_path(patch_path.path, worktree)
        if path is not None and path.suffix == ".go" and path not in seen:
            seen.add(path)
            eligible.append(path)
    return eligible


def deleted_repository_scope_path(candidate: str) -> str | None:
    """Return a safe canonical repository-scope path without requiring it to exist."""

    normalized = candidate.replace("\\", "/")
    if not normalized or PureWindowsPath(normalized).is_absolute():
        return None
    pure_path = PurePosixPath(normalized)
    if pure_path.is_absolute() or ".." in pure_path.parts:
        return None
    relative = pure_path.as_posix()
    return relative if relative in REPOSITORY_SCOPE_FILES else None


def eligible_journal_paths(paths: Iterable[PatchPath], worktree: Path) -> list[Path | str]:
    """Deduplicate safe Go and verification-scope files for later checks."""

    eligible: list[Path | str] = []
    seen: set[Path | str] = set()
    for patch_path in paths:
        if patch_path.deleted:
            path = deleted_repository_scope_path(patch_path.path)
            if path is not None and path not in seen:
                seen.add(path)
                eligible.append(path)
            continue
        path = normalized_relative_path(patch_path.path, worktree)
        if path is not None and (path.suffix == ".go" or patch_path.path.replace("\\", "/") in REPOSITORY_SCOPE_FILES) and path not in seen:
            seen.add(path)
            eligible.append(path)
    return eligible


def format_go_files(paths: Iterable[Path], worktree: Path) -> None:
    """Format each eligible file directly, surfacing a useful failure."""

    for path in paths:
        result = run_command(("gofmt", "-w", str(path)), cwd=worktree)
        if result.returncode != 0:
            detail = result.stderr.strip() or result.stdout.strip() or "unknown gofmt error"
            raise RuntimeError(f"gofmt -w {path.relative_to(worktree)} failed: {detail}")


def write_journal(worktree: Path, session_id: str, paths: Iterable[Path | str]) -> None:
    """Atomically create one append-only journal record for this invocation."""

    relative_paths = [path if isinstance(path, str) else path.relative_to(worktree).as_posix() for path in paths]
    if not relative_paths:
        return
    journal_dir = journal_dir_for(worktree)
    journal_dir.mkdir(mode=0o700, parents=True, exist_ok=True)
    record = json.dumps(
        {"session_id": session_id, "paths": relative_paths}, sort_keys=True, separators=(",", ":")
    ).encode("utf-8")
    session_label = sha256(session_id.encode("utf-8")).hexdigest()
    for _ in range(10):
        destination = journal_dir / f"{session_label}-{secrets.token_hex(16)}.json"
        try:
            descriptor = os.open(destination, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
        except FileExistsError:
            continue
        with os.fdopen(descriptor, "wb") as file:
            file.write(record)
            file.flush()
            os.fsync(file.fileno())
        return
    raise RuntimeError("could not allocate a unique Codex hook journal record")


def baseline_path(worktree: Path, session_id: str) -> Path:
    """Return the Git-private initial snapshot path for one Codex session."""

    return journal_dir_for(worktree) / f"baseline-{sha256(session_id.encode('utf-8')).hexdigest()}.json"


def session_snapshot(worktree: Path) -> dict[str, str]:
    """Hash eligible working-tree files without following unsafe symlinks."""

    result = run_command(("git", "ls-files", "-co", "--exclude-standard", "-z"), cwd=worktree)
    if result.returncode != 0:
        detail = result.stderr.strip() or result.stdout.strip() or "unknown Git error"
        raise RuntimeError(f"git ls-files failed: {detail}")
    snapshot: dict[str, str] = {}
    for candidate in result.stdout.split("\0"):
        path = normalized_relative_path(candidate, worktree)
        if path is None:
            continue
        relative = path.relative_to(worktree).as_posix()
        if path.suffix == ".go" or relative in REPOSITORY_SCOPE_FILES:
            snapshot[relative] = sha256(path.read_bytes()).hexdigest()
    return snapshot


def write_baseline(worktree: Path, session_id: str, snapshot: dict[str, str]) -> None:
    """Atomically persist the initial relevant-file snapshot for one session."""

    directory = journal_dir_for(worktree)
    directory.mkdir(mode=0o700, parents=True, exist_ok=True)
    destination = baseline_path(worktree, session_id)
    temporary = destination.with_suffix(".tmp")
    temporary.write_text(json.dumps(snapshot, sort_keys=True, separators=(",", ":")), encoding="utf-8")
    os.replace(temporary, destination)


def baseline_snapshot(worktree: Path, session_id: str) -> dict[str, str] | None:
    """Load a valid session baseline, if SessionStart has created one."""

    try:
        snapshot = json.loads(baseline_path(worktree, session_id).read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError):
        return None
    if not isinstance(snapshot, dict) or not all(isinstance(path, str) and isinstance(digest, str) for path, digest in snapshot.items()):
        return None
    return snapshot


def changed_since_baseline(worktree: Path, session_id: str) -> list[str]:
    """Return eligible files changed after SessionStart, including deletions."""

    baseline = baseline_snapshot(worktree, session_id)
    if baseline is None:
        return []
    current = session_snapshot(worktree)
    return sorted(path for path in baseline.keys() | current.keys() if baseline.get(path) != current.get(path))


def response(
    *, event_name: str | None = None, message: str | None = None, failed: bool = False, block: bool = False
) -> int:
    """Emit an event-specific Codex hook response."""

    if message is None:
        return 0
    if event_name in {"SessionStart", "UserPromptSubmit"}:
        payload: dict[str, Any] = {
            "hookSpecificOutput": {"hookEventName": event_name, "additionalContext": message}
        }
    elif event_name == "Stop" and block:
        payload = {"decision": "block", "reason": message}
    else:
        payload = {"systemMessage": message}
    print(json.dumps(payload, separators=(",", ":")))
    return 1 if failed else 0


def patch_text(event: dict[str, Any]) -> str | None:
    """Extract supported freeform apply_patch input without interpreting it."""

    tool_input = event.get("tool_input")
    if isinstance(tool_input, str):
        return tool_input
    if not isinstance(tool_input, dict):
        return None
    for key in ("patch", "command"):
        value = tool_input.get(key)
        if isinstance(value, str):
            return value
    return None


def handle_post_tool_use(event: dict[str, Any]) -> int:
    """Process one PostToolUse outer exec event."""

    tool_name = event.get("tool_name")
    if event.get("hook_event_name") != "PostToolUse" or not isinstance(tool_name, str):
        return response(message="agent-loop hook ignored an unsupported event")
    if tool_name.endswith("apply_patch"):
        patch = patch_text(event)
        if patch is None:
            return response(message="agent-loop hook ignored an apply_patch event without patch text")
    elif not tool_name.endswith("exec"):
        return response(message="agent-loop hook ignored an unsupported event")
    cwd = event.get("cwd")
    if not isinstance(cwd, str) or not cwd:
        return response(message="agent-loop hook ignored an event without a working directory")
    session_id = event.get("session_id")
    if not isinstance(session_id, str) or not session_id:
        return response(message="agent-loop hook ignored an event without a session id")
    try:
        worktree = worktree_for(Path(cwd))
        if tool_name.endswith("apply_patch"):
            patch_paths = parse_patch_paths(patch)
            format_go_files(eligible_go_paths(patch_paths, worktree), worktree)
            write_journal(worktree, session_id, eligible_journal_paths(patch_paths, worktree))
        else:
            changed_paths = changed_since_baseline(worktree, session_id)
            go_paths = [worktree / path for path in changed_paths if path.endswith(".go") and (worktree / path).is_file()]
            format_go_files(go_paths, worktree)
            write_journal(worktree, session_id, changed_paths)
    except RuntimeError as error:
        return response(message=f"agent-loop formatting failed: {error}", failed=True)
    return 0


def event_worktree(event: dict[str, Any]) -> Path:
    """Resolve the event cwd or reject an event that cannot identify a repository."""

    cwd = event.get("cwd")
    if not isinstance(cwd, str) or not cwd:
        raise RuntimeError("event has no working directory")
    return worktree_for(Path(cwd))


def session_id_for(event: dict[str, Any]) -> str:
    session_id = event.get("session_id")
    if not isinstance(session_id, str) or not session_id:
        raise RuntimeError("event has no session id")
    return session_id


def journal_paths(worktree: Path, session_id: str) -> set[str]:
    """Return valid paths recorded only for this session."""

    paths: set[str] = set()
    directory = journal_dir_for(worktree)
    if not directory.exists():
        return paths
    for record_path in directory.glob("*.json"):
        try:
            record = json.loads(record_path.read_text(encoding="utf-8"))
        except (OSError, json.JSONDecodeError):
            continue
        if not isinstance(record, dict) or record.get("session_id") != session_id:
            continue
        values = record.get("paths")
        if isinstance(values, list):
            paths.update(value for value in values if isinstance(value, str))
    return paths


def plans_in_lifecycle(worktree: Path, lifecycle: str | None = None) -> list[Path]:
    directories = (lifecycle,) if lifecycle else LIFECYCLE_DIRECTORIES
    return [path for state in directories for path in sorted((worktree / "docs/plans" / state).glob("*.md"))]


def task_section(plan: Path, number: int) -> str | None:
    """Extract one task section, stopping at the next task or top-level section."""

    contents = plan.read_text(encoding="utf-8")
    headings = list(TASK_HEADING.finditer(contents))
    match = next((heading for heading in headings if int(heading.group(1)) == number), None)
    if match is None:
        return None
    following = [heading.start() for heading in headings if heading.start() > match.start()]
    following.extend(heading.start() for heading in TOP_LEVEL_HEADING.finditer(contents) if heading.start() > match.start())
    end = min(following, default=len(contents))
    return contents[match.start() : end].strip()


def resolve_plan(worktree: Path, reference: str | None) -> list[tuple[str, Path]]:
    """Resolve an exact filename or repository-relative plan path across lifecycle states."""

    if not reference:
        return []
    normalized = reference.strip().replace("\\", "/").rstrip(". ")
    if not normalized:
        return []
    filename = PurePosixPath(normalized).name
    matches: list[tuple[str, Path]] = []
    for state in LIFECYCLE_DIRECTORIES:
        for path in plans_in_lifecycle(worktree, state):
            relative = path.relative_to(worktree).as_posix()
            if normalized == relative or normalized == path.name or filename == path.name:
                matches.append((state, path))
    return matches


def package_patterns(worktree: Path, paths: Iterable[str]) -> tuple[list[str], bool]:
    """Return package patterns for changed Go files, or repository-wide scope."""

    path_set = set(paths)
    if path_set & REPOSITORY_SCOPE_FILES:
        return ["./..."], True
    packages: set[str] = set()
    for relative in path_set:
        if not relative.endswith(".go"):
            continue
        path = normalized_relative_path(relative, worktree)
        if path is not None:
            directory = path.parent.relative_to(worktree).as_posix()
            packages.add("./" if directory == "." else f"./{directory}")
    return sorted(packages), False


def command_failure(result: subprocess.CompletedProcess[str], args: Sequence[str]) -> str:
    detail = (result.stderr or result.stdout).strip()
    rendered = " ".join(args)
    if any(token in detail.lower() for token in ("permission denied", "operation not permitted", "sandbox", "network is unreachable", "not found")):
        return f"incomplete verification ({detail}); rerun normally: {rendered}"
    return f"{rendered} failed: {detail or f'exit {result.returncode}'}"


def handle_session_start(event: dict[str, Any]) -> int:
    try:
        worktree = event_worktree(event)
        write_baseline(worktree, session_id_for(event), session_snapshot(worktree))
        branch = git_output(worktree, "branch", "--show-current") or "detached HEAD"
        dirty = git_output(worktree, "status", "--short").splitlines()
        plans = [path.name for path in plans_in_lifecycle(worktree, "in-progress")]
    except RuntimeError as error:
        return response(event_name="SessionStart", message=f"agent-loop context unavailable: {error}")
    summary = [f"branch: {branch}", f"pre-existing dirty paths: {', '.join(dirty) if dirty else 'none'}", f"active plans: {', '.join(plans) if plans else 'none'}"]
    return response(event_name="SessionStart", message="agent-loop context — " + "; ".join(summary))


def handle_user_prompt_submit(event: dict[str, Any]) -> int:
    prompt = event.get("prompt")
    if not isinstance(prompt, str):
        return response(event_name="UserPromptSubmit", message="agent-loop prompt context unavailable: prompt is missing")
    match = TASK_PROMPT.search(prompt)
    if match is None:
        return 0
    try:
        worktree = event_worktree(event)
        task_number = int(match.group(1))
        candidates = resolve_plan(worktree, match.group(2))
        if not candidates and match.group(2) is None:
            candidates = [(state, path) for state in LIFECYCLE_DIRECTORIES for path in plans_in_lifecycle(worktree, state) if task_section(path, task_number)]
    except RuntimeError as error:
        return response(event_name="UserPromptSubmit", message=f"agent-loop task context unavailable: {error}")
    if len(candidates) != 1:
        guidance = "name the exact plan filename or docs/plans/<state>/<name>.md" if candidates else "check the plan path and task number"
        return response(event_name="UserPromptSubmit", message=f"agent-loop could not uniquely resolve Implement Task {task_number}; {guidance}.")
    state, plan = candidates[0]
    section = task_section(plan, task_number)
    if section is None:
        return response(event_name="UserPromptSubmit", message=f"agent-loop: {plan.relative_to(worktree)} has no Task {task_number}; prompt unchanged.")
    return response(event_name="UserPromptSubmit", message=f"agent-loop task context ({state}): {plan.relative_to(worktree)}\n\n{section}")


def handle_stop(event: dict[str, Any]) -> int:
    if event.get("stop_hook_active"):
        return response(message="agent-loop stop gate already ran; not retrying.")
    try:
        worktree = event_worktree(event)
        session_id = session_id_for(event)
        paths = journal_paths(worktree, session_id)
        paths.update(changed_since_baseline(worktree, session_id))
        packages, repository_wide = package_patterns(worktree, paths)
    except RuntimeError as error:
        return response(message=f"agent-loop stop gate unavailable: {error}")
    if not packages and not repository_wide:
        return response(message="agent-loop stop gate: no touched Go packages to check.")
    go_paths = sorted(path for path in paths if path.endswith(".go") and (worktree / path).is_file())
    commands: list[tuple[str, ...]] = [("gofmt", "-l", *go_paths)] if go_paths else []
    commands.extend((("golangci-lint", "run", *packages), ("go", "test", "-race", *packages)))
    failures: list[str] = []
    for command in commands:
        result = run_command(command, cwd=worktree)
        if result.returncode != 0:
            failures.append(command_failure(result, command))
        elif command[0] == "gofmt" and result.stdout.strip():
            failures.append(f"gofmt required: {result.stdout.strip()}")
    if failures:
        return response(event_name="Stop", message="agent-loop stop gate failed: " + "; ".join(failures), block=True)
    scope = "repository-wide" if repository_wide else ", ".join(packages)
    return response(message=f"agent-loop stop gate passed ({scope}).")


def handle_session_end(event: dict[str, Any]) -> int:
    try:
        worktree = event_worktree(event)
        session_id = session_id_for(event)
        directory = journal_dir_for(worktree)
    except RuntimeError as error:
        return response(message=f"agent-loop cleanup unavailable: {error}")
    if not directory.exists():
        return 0
    baseline_path(worktree, session_id).unlink(missing_ok=True)
    for record_path in directory.glob("*.json"):
        try:
            record = json.loads(record_path.read_text(encoding="utf-8"))
        except (OSError, json.JSONDecodeError):
            continue
        if isinstance(record, dict) and record.get("session_id") == session_id:
            record_path.unlink(missing_ok=True)
    return 0


def main() -> int:
    """Decode hook input without trusting the payload shape."""

    try:
        decoded = json.load(sys.stdin)
    except json.JSONDecodeError:
        return response(message="agent-loop hook ignored malformed JSON input")
    if not isinstance(decoded, dict):
        return response(message="agent-loop hook ignored a non-object JSON input")
    handlers = {
        "PostToolUse": handle_post_tool_use,
        "SessionStart": handle_session_start,
        "UserPromptSubmit": handle_user_prompt_submit,
        "Stop": handle_stop,
        "SessionEnd": handle_session_end,
    }
    handler = handlers.get(decoded.get("hook_event_name"))
    if handler is None:
        return response(message="agent-loop hook ignored an unsupported event")
    return handler(decoded)


if __name__ == "__main__":
    raise SystemExit(main())
