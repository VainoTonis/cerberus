---
name: cerberus
description: Run a task against the current repo using multiple AI models in parallel via cerberus, then review and apply the best result
---

## What I do

Use cerberus to run an implementation task in an isolated git worktree. The agent works in a separate branch, commits its changes, and the result can be cherry-picked back onto the main branch. Multiple sessions can run simultaneously on the same repo without conflict — each is fully isolated by name.

## Prerequisites

- `cerberus` binary must be on PATH (`~/.local/bin/cerberus`)
- Must be run from inside a git repo
- Session names must be unique within a repo

## Global config

```bash
cat ~/.config/cerberus/config.json
```

- `runners`: list of `{ agent, model, oc_agent }` — one entry per parallel solution slot
- `instructions`: prepended to every prompt sent to agents

## Writing good prompts

The most common failure mode is giving the agent too much to do. Agents get stuck when a prompt requires too many decisions, touches multiple files, or involves non-trivial logic in more than one place.

Rules:
- **One logical change per session.** If you have two fixes, run two sessions.
- **Keep prompts short and concrete.** Show the exact before/after code when possible — don't describe what to do in prose if you can just show it.
- **Do not ask for tests in the same prompt as the implementation.** If tests are needed, run a second session after the first is applied.
- **Do not ask the agent to explore or understand the codebase.** Give it the file path, function name, and exactly what to change. It should not need to search.
- If a session runs for more than ~60 seconds without completing, assume the prompt was too broad. Kill it and split the work.

## Workflow

### 1. Choose a session name
Pick a short, descriptive name (e.g. `frontend-refactor`, `auth-bugfix`).

### 2. Run the agent

Always pass `--caller` with your own model ID so it is recorded in the commit message.

**Single-agent isolation (primary use case):**
```bash
cerberus --session <name> start --n 1 --caller <your-model-id> --prompt "<prompt>"
```

**Multi-agent parallel comparison:**
```bash
cerberus --session <name> start --caller <your-model-id> --prompt "<prompt>"
```

After each agent finishes, cerberus automatically commits changes inside the worktree and records the commit hash in state.

### 3. Check status

Poll with:
```bash
cerberus --session <name> status
```

Expected terminal states: `done` or `failed`. The status `running (process gone)` means the process exited but cerberus did not record a commit — this usually means the agent finished but failed to commit. Check the worktree directly:

```bash
git -C .cerberus/sessions/<name>/worktrees/solve-1 diff --stat HEAD
git -C .cerberus/sessions/<name>/worktrees/solve-1 log --oneline -3
```

If there are uncommitted changes, commit them manually and then cherry-pick:
```bash
git -C .cerberus/sessions/<name>/worktrees/solve-1 add <files>
git -C .cerberus/sessions/<name>/worktrees/solve-1 commit -m "<message>"
# get the hash from above, then:
git cherry-pick <hash>
cerberus --session <name> clean --force
```

If there are no changes at all, the agent did nothing — kill and retry with a tighter prompt.

### 4. Review
```bash
cerberus --session <name> review --diff
```

### 5a. Apply a solution
Cherry-picks all commits from the solution branch onto the current branch:
```bash
cerberus --session <name> apply --solution <N>
```
On conflict, git is left in cherry-pick state. Resolve then `git cherry-pick --continue`, or abort with `git cherry-pick --abort`.

Then clean up:
```bash
cerberus --session <name> clean --force
```

### 5b. Iterate on a solution
If the solution has a flaw, run the agent again in the same worktree with a follow-up prompt:
```bash
cerberus --session <name> rerun --solution <N> --prompt "<follow-up prompt>"
```
This stacks a new commit on the solution branch. Review again, then apply when satisfied. Each `rerun` adds another commit; `apply` cherry-picks the whole range.

### 5c. Merge solutions (multi-agent only)
```bash
cerberus --session <name> merge
cerberus --session <name> merge-apply
```
`merge-apply` commits the result and automatically cleans up the session — no separate `clean` needed.

### Checking active sessions
```bash
cerberus list
```

## Session name resolution

If only one session is active, `--session` can be omitted from all commands except `start`. If multiple sessions are active, `--session` is required.

## Cerberus CLI reference

All flags use `--` (double dash), not `-`. `--session` is a global flag and goes before the subcommand:

```
cerberus --session <name> start       [--prompt <text>] [--prompt-file <path>] [--n <int>] [--agent <name>] [--model <provider/model>] [--caller <provider/model>]
cerberus --session <name> rerun       --solution <N> --prompt <text> [--prompt-file <path>]
cerberus list
cerberus --session <name> status      [--diff]
cerberus --session <name> review      [--diff]
cerberus --session <name> apply       --solution <N>
cerberus --session <name> merge       [--model <provider/model>]
cerberus --session <name> merge-apply
cerberus --session <name> logs
cerberus --session <name> clean       [--force]
cerberus stats
```

## Important caveats

- `apply` cherry-picks all commits on the solution branch since base — works correctly after multiple `rerun` iterations
- `apply` requires the agent to have committed; if it shows "no commit" but the worktree has changes, commit manually and cherry-pick directly (see status section above)
- `merge-apply` automatically cleans up the session after committing; no separate `clean` needed
- **Never include `cerberus clean` in the prompt sent to the agent.** The worktree must exist after the agent exits for the auto-commit to work
- Worktrees live at `.cerberus/sessions/<name>/worktrees/solve-N/`
- Branches are named `cerberus/<name>/solve-N`
- Agents run as child `opencode run` processes; nested invocations from inside an opencode session work correctly
