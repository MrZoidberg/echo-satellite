# Implementation planning and tracking

This document is the authoritative guide for creating, executing, verifying, and finishing implementation plans in this repository. **Plan location is the single source of truth for lifecycle state.** There is no separate plan index, board, or duplicated status registry.

## Lifecycle directories

- `future/` — proposed or approved work that has not started. Plan status is `future`.
- `in-progress/` — only work under active execution. Plan status is `in-progress`, `in-progress (paused)`, or `in-progress (blocked)`.
- `finished/` — completed work with recorded verification evidence. Plan status is `finished`.
- `superseded/` — plans abandoned or replaced before completion. Plan status is `superseded`, and the plan must name the plan or decision that replaced it.

Use the filename `YYYY-MM-DD-short-topic.md`, based on the plan's creation date and a concise lowercase topic. Move a plan between lifecycle directories **without renaming it** (`git mv`) so its links and Git history stay recognizable.

Directory placement governs lifecycle state. If placement and the plan's status field disagree, correct the status field immediately.

The repository may have multiple active plans. Before starting one, confirm no active plan claims the same task. Plans that touch shared files or overlapping behavior must name that shared scope, its dependencies, owners, and coordination requirements.

## Creating and decomposing a plan

Create plans in `future/`. Each plan must include:

- title, status, owner or active agent, creation date, update date, start date, completion date;
- an objective and explicit non-goals;
- source references, constraints, dependencies, and prerequisites;
- a high-level architecture and approach summary;
- a planned file map when the plan creates or moves more than a few files;
- numbered tasks in dependency order;
- for each task: status, purpose, dependencies, files or components in scope, concrete changes, expected outcome, and **Verification**;
- cross-task risks, and rollback or recovery notes where relevant;
- final acceptance criteria;
- a progress log and a completion evidence section.

Each task should fit one focused agent session where practical and produce one observable, independently verifiable outcome. Do not combine unrelated subsystems to reduce task count. Make dependencies explicit, and give exact commands or concrete manual checks rather than statements such as "test the change."

Plans must be detailed enough to execute without inventing missing behavior. A plan may reference `docs/DESIGN.md` for technical decisions, but it must still identify the exact files or components, changes, outcomes, and verification for each task.

### Hardware-dependent tasks

This project targets physical Echo Dot Gen 2 hardware. A task whose verification requires a real device must:

- state that it is hardware-dependent and name the required device state (rooted, paired, supervisor version);
- give the exact `echoctl`/ADB commands and the observation that constitutes success;
- state what can be verified without hardware (unit tests, `dotsim` fixtures) and what cannot.

A hardware-dependent task is not complete because its simulator equivalent passed. Record which verifications ran against hardware and which against `dotsim`.

### Task status and steps

Every task carries its own status line directly under its heading:

```markdown
**Status:** not started | in progress | completed YYYY-MM-DD | blocked | superseded
```

Long tasks may break into ordered checkbox steps, each independently runnable and each ending with a command or observation:

```markdown
- [ ] **Step 1: Add failing tests for X**
- [ ] **Step 2: Run `go test ./internal/x` and confirm the missing behavior fails**
- [ ] **Step 3: Implement X**
```

Prefer test-first steps: add the failing test, prove it fails, implement, prove it passes.

### Amending an executing plan

Do not rewrite completed tasks. When review or implementation invalidates finished work, append a new subtask instead:

```markdown
#### Task N review remediation: <short outcome>
```

It carries its own status, dependencies, concrete changes, and verification, and it states explicitly which earlier requirement it supersedes. Completed tasks keep their original implementation record and evidence.

## Starting work: `future/` to `in-progress/`

Before execution begins:

1. Confirm the objective, scope, non-goals, prerequisites, tasks, and verification are sufficiently defined.
2. Confirm no active plan or task conflicts with this work. Document shared files and coordination for overlaps.
3. Set the active owner or agent, start date, update date, and status to `in-progress`.
4. Move the plan to `in-progress/` in the same change, using `git mv` when the file is tracked.
5. Begin actual execution. Do not place work in `in-progress/` merely because it is approved or likely to start soon.

## While work is in progress

- Claim each task through the plan; do not execute a task already claimed by another agent.
- Mark a task complete only after its stated Verification succeeds.
- Append dated material discoveries, decisions, blockers, deviations, and verification outcomes to the progress log.
- Keep the update date, status, ownership, and remaining work accurate.
- Review and update the plan **before** implementing a material scope expansion.
- When implementation invalidates a documented design assumption, update `docs/DESIGN.md` in the same change and note it in the progress log.
- Keep failed, paused, or blocked plans in `in-progress/` with status `in-progress (paused)` or `in-progress (blocked)`, an honest blocker description, and the remaining work. Do not archive partial execution as finished.

## Finishing work: `in-progress/` to `finished/`

Before moving a plan, confirm that:

1. Every required task and final acceptance criterion is complete.
2. Every relevant test, command, and manual check has passed, including hardware checks where the plan requires them.
3. Completion evidence records the exact commands or checks and concise outcomes.
4. Unresolved limitations and follow-up work are explicitly documented.
5. The completion date and final update date are recorded.
6. The status is changed to `finished`.

Only after all six conditions hold, move the plan to `finished/` without renaming it, using `git mv`. Read back the destination and metadata after the move. A failed check leaves the plan in `in-progress/` with an accurate status, evidence, blocker, and remaining work.

## Abandoning a plan: any directory to `superseded/`

Set status to `superseded`, record the date and the reason, name the replacing plan or decision, and state what (if anything) was already implemented and whether it was reverted. Then `git mv` the file into `superseded/`. Never delete a plan that reached `in-progress/`.

## Reusable plan template

Copy this template into `future/YYYY-MM-DD-short-topic.md` and replace the instructional text with concrete information. Remove optional sections only when they genuinely do not apply.

````markdown
# <Feature> implementation plan

**Status:** future
**Owner or active agent:** unassigned
**Created:** YYYY-MM-DD
**Updated:** YYYY-MM-DD
**Started:** not started
**Completed:** not completed

## Objective

State the single outcome this plan will deliver.

## Non-goals

- State work explicitly excluded from this plan.

## Source references and constraints

- `docs/DESIGN.md`, section N: identify the governing requirement or design decision.
- State technical, operational, security, hardware, and scope constraints.

## Dependencies and prerequisites

- List required decisions, services, tools, hardware, repository state, and preceding plans.

## Architecture and high-level plan

- Concise, ordered summary of the design and implementation approach, architecture decisions, and user value.

## Planned file map

- `exact/path`: what it owns and why it exists.

## Numbered tasks

### Task 1: Observable outcome

**Status:** not started

**Purpose:** Explain why this task exists.

**Dependencies:** None, or earlier task numbers and external prerequisites.

**Hardware required:** no | yes — state the required device state.

**Files or components:**

- Create: `exact/path`
- Modify: `exact/path`
- Test: `exact/path`

**Concrete changes:**

- Describe the exact behavior, interfaces, data, and error handling to implement.

**Expected outcome:** State the observable completion boundary.

**Verification:**

Run:

```sh
exact command
```

Expected: state the required exit status, output, or manual observation.

## Cross-task risks

- Describe each risk, its impact, and mitigation.

## Rollback or recovery

- Describe how to recover safely from partial execution or reverse the change.

## Final acceptance criteria

- [ ] State a concrete, verifiable condition for overall completion.

## Progress log

- YYYY-MM-DD: Record task starts, results, decisions, deviations, and blockers.

## Completion evidence

- `exact verification command` — concise result and date.
- Record unresolved limitations or follow-up work, or explicitly state that none remain.
````

The literal placeholder dates, paths, commands, and instructional sentences must be replaced before a plan is approved or moved to `in-progress/`.
