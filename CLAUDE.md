# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

The repository's universal agent instructions live in `AGENTS.md` and are imported here:

@AGENTS.md

## Claude Code specifics

- Keep repository-wide rules in `AGENTS.md` so every agent gets them. Add only Claude Code-specific behavior (tool usage, slash commands, subagent conventions) below.
- When a change alters operational behavior or a documented design assumption, update `docs/DESIGN.md` and the active plan in the same change rather than deferring it.
