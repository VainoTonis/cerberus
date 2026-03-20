# cerberus

Run the same coding task against a codebase in parallel using multiple AI models, then review and apply the best result.

Each solution gets its own git worktree and branch. Agents run headlessly and concurrently. When they're done you inspect what each one changed, pick the best, or ask an LLM to analyse and merge them.

## How it works

```
cerberus start -prompt "add input validation to the signup handler"
```

1. Creates N git worktrees from the current HEAD, one per configured runner
2. Runs `opencode run <prompt>` in each worktree in parallel
3. Streams output live, prefixed with `[solve-N]`, and writes logs to `.cerberus/logs/`
4. Blocks until all agents finish

```
cerberus review        # see which files each solution changed
cerberus review -diff  # see full unified diffs

cerberus apply -solution 2   # copy solve-2's changes into your working tree
git diff                     # review, then commit as usual

cerberus clean               # remove worktrees, branches, and opencode sessions
```

## Install

```bash
git clone https://github.com/tonis/cerberus
cd cerberus
go build -o cerberus ./cmd/cerberus
cp cerberus ~/.local/bin/cerberus
```

Requires Go 1.24+, git, and at least one supported agent CLI on your PATH.

## Configuration

`~/.config/cerberus/config.json`

```json
{
  "runners": [
    {
      "agent": "opencode",
      "model": "amazon-bedrock/anthropic.claude-haiku-4-5-20251001-v1:0",
      "oc_agent": "build"
    },
    {
      "agent": "opencode",
      "model": "amazon-bedrock/qwen.qwen3-coder-30b-a3b-v1:0",
      "oc_agent": "build"
    }
  ],
  "instructions": "Focus only on the task described. Make the minimal changes required. Do not explore the codebase beyond what is directly needed to complete the task."
}
```

| Field | Description |
|---|---|
| `runners` | Ordered list of agent+model pairs. One solution slot per entry. |
| `runners[].agent` | Agent CLI to use: `opencode` or `claude`. |
| `runners[].model` | Model in `provider/model` format. Uses the agent's default if omitted. |
| `runners[].oc_agent` | opencode agent mode (e.g. `build`, `plan`). Uses opencode's default if omitted. |
| `instructions` | Prepended to every prompt. Use this to constrain agent behaviour. |

## Commands

### `start`

Create worktrees and run all agents in parallel. Blocks until all finish.

```
cerberus start [flags]

  -prompt string       prompt to send to each agent (required)
  -prompt-file string  read prompt from a file instead of -prompt
  -n int               number of solutions (default: number of runners in config)
  -agent string        agent to use when runners not set in config (default: opencode)
  -model string        model to use when runners not set in config
```

If `runners` is set in config, `-n`, `-agent`, and `-model` are ignored unless `-n` is given explicitly to truncate or extend the runner list.

If no config exists, defaults to 2 solutions using the `-agent` and `-model` flags.

### `status`

Print the current state of each solution: branch, status, PID (if still running), and log path.

### `review`

Print which files each solution changed.

```
cerberus review        # file names only
cerberus review -diff  # full unified diffs
```

### `apply`

Copy all changed files from one solution into your main working tree. Does not commit.

```
cerberus apply -solution <n>
```

### `merge`

Collect all solution diffs and send them to an LLM with a prompt asking it to analyse and produce the best merged result. Streams the response to stdout.

```
cerberus merge
cerberus merge -model provider/model   # use a specific model for the merge step
```

### `clean`

Remove all worktrees, their branches, the opencode sessions, and the `.cerberus` state directory.

```
cerberus clean          # prompts for confirmation
cerberus clean -force   # skip confirmation
```

## State

cerberus stores session state in `.cerberus/state.json` inside the git repo. Add `.cerberus/` to your `.gitignore`.

Logs for each solution are written to `.cerberus/logs/solve-N.log`.

## Agents

| Agent | CLI | Non-interactive flag |
|---|---|---|
| `opencode` | `opencode run --format json` | Native |
| `claude` | `claude -p` | Native |

Adding a new agent means implementing the `Agent` interface in `internal/agent/`:

```go
type Agent interface {
    Name() string
    Args(r RunArgs) ([]string, error)
}
```

## Example workflow

```bash
# run 3 models against the same task
cerberus start -n 3 -prompt "refactor the auth middleware to use the new token validator"

# check what changed
cerberus review

# look at full diffs for solution 2
cerberus review -diff 2>/dev/null | grep -A 999 "solve-2"

# ask an LLM to compare and suggest the best merge
cerberus merge

# apply the solution you want
cerberus apply -solution 1

# commit
git add -p
git commit -m "refactor auth middleware"

# clean up
cerberus clean -force
```
