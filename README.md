# cerberus

Run an AI agent in an isolated Docker container with auto-commit and stats tracking.

Each run gets its own git worktree and branch. The agent runs headlessly and changes are automatically committed and tracked. Sessions can be interactive (long-lived container) or one-shot.

## Quick Start

```bash
# Single one-shot run
cerberus start -prompt "add input validation to the signup handler"

# Check what changed
cerberus review        # files only
cerberus review -diff  # full diffs

# View logs or status
cerberus logs
cerberus status

# Clean up
cerberus clean
```

## Install

```bash
git clone https://github.com/tonis/cerberus
cd cerberus
go build -o cerberus ./cmd/cerberus
cp cerberus ~/.local/bin/cerberus
```

Requires:
- Go 1.24+
- Docker
- Git

## Configuration

Create `~/.config/cerberus/config.json`:

```json
{
  "default_model": "amazon-bedrock/anthropic.claude-haiku-4-5-20251001-v1:0",
  "default_image": "ghcr.io/myorg/cerberus-agent:latest",
  "instructions": "Focus only on the task. Make minimal changes. Do not explore beyond what is needed."
}
```

| Field | Description |
|---|---|
| `default_model` | Model to use if `-model` is not specified |
| `default_image` | Docker image for agent containers (required) |
| `instructions` | Prepended to every prompt |
| `aws_profile` | AWS profile for Bedrock API calls (optional) |
| `aws_region` | AWS region for Bedrock (defaults to us-east-1) |
| `max_turns` | Max conversation turns per session (default: 10) |
| `max_output_tokens` | Max output tokens before killing agent (default: 50000) |

## Commands

### `start`

Run the agent once and commit the result.

```
cerberus start [flags]
  -prompt string       prompt to send to agent (required)
  -prompt-file string  read prompt from file
  -name string         session name (auto-generated if omitted)
  -model string        model to use (overrides config)
  -image string        docker image (overrides config)
  -agent string        agent CLI (default: pi)
```

If changes are made, you'll be prompted for a commit message. Session state and logs are saved in `.cerberus/sessions/<name>/`.

### `rerun`

Send another prompt to an existing session's worktree.

```
cerberus rerun [flags]
  -name string    session name (required if multiple active)
  -prompt string  follow-up prompt (required)
```

Commits any new changes after the second run.

### `chat`

Start an interactive session with a long-lived container.

```
cerberus chat [flags]
  -prompt string  initial prompt (required)
  -name string    session name (auto-generated if omitted)
  -model string   model to use
  -image string   docker image
  -agent string   agent CLI (default: pi)
```

Container stays alive; use `cerberus message` for follow-up prompts.

### `message`

Send a message to a waiting interactive session.

```
cerberus message [flags]
  -name string    session name (required if multiple active)
  -message string message to send (required)
```

### `close`

Commit any changes in an interactive session and clean up.

```
cerberus close [flags]
  -name string  session name (required if multiple active)
```

### `review`

Show what changed in a session.

```
cerberus review [flags]
  -name string  session name (required if multiple active)
  -diff         print full unified diffs
```

### `status`

Print session state: branch, status, tokens used, cost.

```
cerberus status [flags]
  -name string  session name (required if multiple active)
```

### `logs`

Print the agent's full output log.

```
cerberus logs [flags]
  -name string  session name (required if multiple active)
```

### `clean`

Remove session worktree, branch, and state. Stops any running container.

```
cerberus clean [flags]
  -name string  session name (required if multiple active)
  -all          clean all sessions
```

### `stats`

Show performance statistics (tokens, cost, duration) for recent runs.

```
cerberus stats
```

Reads from `~/.config/cerberus/stats.json`.

### `version`

Print build version.

## Network Isolation (Optional)

By default, agent containers have direct internet access. For an isolated setup:

### Proxy Stack

Edit or deploy `proxy/docker-compose.yml`:

```bash
cd proxy
docker-compose up -d
```

This starts:
- **Nexus** (port 8081) — caches Go, npm, pip, Maven packages
- **Squid** (port 3128) — forwards API calls with an allowlist (defaults to `*.amazonaws.com`, `*.openai.com`)

### Agent Configuration

Copy or symlink `proxy/agent.env` to `~/.config/cerberus/agent.env`:

```bash
cp proxy/agent.env ~/.config/cerberus/agent.env
```

This file is automatically injected into agent containers and routes:
- Package manager traffic → Nexus (cached)
- API traffic → Squid (allowlisted)

Agent containers attach only to `sandbox-internal` network (internal: true) and cannot reach the internet directly.

For details, see `PROXY_PLAN.md`.

## State and Logs

- **Session state** — `.cerberus/sessions/<name>/state.json`
- **Session logs** — `.cerberus/sessions/<name>/logs/solve.log`
- **Global stats** — `~/.config/cerberus/stats.json`

Add `.cerberus/` to `.gitignore`.

## Examples

### One-shot fix

```bash
cerberus start -prompt "fix the null pointer exception in auth.go"
cerberus review -diff
cerberus clean
```

### Multi-turn exploration

```bash
cerberus start -name research -prompt "survey the codebase for rate limiting implementations"
cerberus rerun -name research -prompt "now add token bucket rate limiting to the API"
cerberus review -name research
cerberus clean -name research
```

### Interactive debugging

```bash
cerberus chat -name debug -prompt "start with the failing test case" -image myimage
cerberus message -name debug -message "add debug output in function X"
cerberus message -name debug -message "commit what you have"
cerberus close -name debug
```

## Architecture

**Host** runs cerberus (create worktrees, manage state, commit).

**Docker container** runs the agent (read-only mount of agent config, writable mount of worktree).

**Git worktree** per session (isolated branch, isolated view of the repo).

**Proxy network** (optional) enforces traffic isolation via Docker networking + Nexus/Squid.

## Agents Supported

- `pi` — Uses `pi run --mode json` command

To add support for another agent, implement the `Agent` interface in `internal/agent/`:

```go
type Agent interface {
    Name() string
    Args(r RunArgs) ([]string, error)
}
```

## License

Experimental. Use at your own risk.
