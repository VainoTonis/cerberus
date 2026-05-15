# cerberus

Run an AI agent in an isolated Docker container with auto-commit and stats tracking.

Each run gets its own git worktree and branch. The agent runs headlessly and changes are automatically committed and tracked. Sessions can be interactive (long-lived container) or one-shot.

## Quick Start

```bash
# Single one-shot run
cerberus start --prompt "add input validation to the signup handler"

# Check what changed
cerberus review        # files only
cerberus review --diff  # full diffs

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
| `default_model` | Model to use if `--model` is not specified |
| `default_image` | Docker image for agent containers (required) |
| `instructions` | Prepended to every prompt |
| `aws_profile` | AWS profile for Bedrock API calls (optional) |
| `aws_region` | AWS region for Bedrock (defaults to us-east-1) |
| `max_turns` | Max conversation turns per session (default: 10) |
| `max_output_tokens` | Max output tokens before killing agent (default: 50000) |

### Agent (pi) Configuration

The `pi` agent reads its provider and model config from `~/.pi/agent/models.json` on the host. This file is mounted into every agent container at `/root/.pi/agent/models.json`.

For Ollama, the `baseUrl` must point to the container hostname on `sandbox-internal`, not `localhost`:

```json
{
  "providers": {
    "yourProviderName": {
      "api": "openai-completions",
      "apiKey": "ollama",
      "baseUrl": "http://ollama:11434/v1",
      "models": [
        {
          "id": "your-model-tag",
          "contextWindow": 4096,
          "input": ["text"],
          "reasoning": false,
          "cost": { "input": 0, "output": 0, "cacheRead": 0, "cacheWrite": 0 }
        }
      ],
      "compat": {
        "supportsStore": false,
        "supportsDeveloperRole": false,
        "supportsReasoningEffort": false,
        "supportsUsageInStreaming": false,
        "supportsStrictMode": false
      }
    }
  }
}
```

`host.docker.internal` does not work on Linux — use the Ollama container name as the hostname instead.

## Profiles

A profile file overrides model, image, and env vars for a single run without touching your global config. Useful for switching providers (e.g. a local Ollama model vs Bedrock).

Create a JSON file anywhere:

```json
{
  "default_model": "mymodel:tag",
  "default_image": "my-agent-image",
  "extra_env": {
    "SOME_VAR": "value"
  }
}
```

Pass it with `--profile-file`:

```bash
cerberus start --profile-file ./profiles/ollama.json --prompt "add input validation"
```

Profile values take precedence over `~/.config/cerberus/config.json`. The profile path is stored in session state and reused automatically on `rerun`.

A `profiles/` directory in this repo contains ready-made examples.

## Commands

### `start`

Run the agent once and commit the result.

```
cerberus start [flags]
  --prompt string        prompt to send to agent (required)
  --prompt-file string   read prompt from file
  --name string          session name (auto-generated if omitted)
  --model string         model to use (overrides config)
  --image string         docker image (overrides config)
  --agent string         agent CLI (default: pi)
  --profile-file string  path to a profile JSON file
  --output string        output format: text (default) or jsonl
  --callback string      URL to POST events to as they happen
```

If changes are made, you'll be prompted for a commit message. Session state and logs are saved in `.cerberus/sessions/<name>/`.

### `rerun`

Send another prompt to an existing session's worktree.

```
cerberus rerun [flags]
  --name string          session name (required if multiple active)
  --prompt string        follow-up prompt (required)
  --prompt-file string   read prompt from file
  --profile-file string  override profile for this rerun
  --output string        output format: text (default) or jsonl
  --callback string      URL to POST events to as they happen
```

Commits any new changes after the second run.

### `chat`

Start an interactive session with a long-lived container.

```
cerberus chat [flags]
  --prompt string        initial prompt (required)
  --name string          session name (auto-generated if omitted)
  --model string         model to use
  --image string         docker image
  --agent string         agent CLI (default: pi)
  --profile-file string  path to a profile JSON file
  --output string        output format: text (default) or jsonl
  --callback string      URL to POST events to as they happen
```

Container stays alive; use `cerberus message` for follow-up prompts.

### `message`

Send a message to a waiting interactive session.

```
cerberus message [flags]
  --name string      session name (required if multiple active)
  --message string   message to send (required)
  --output string    output format: text (default) or jsonl
  --callback string  URL to POST events to as they happen
```

### `close`

Commit any changes in an interactive session and clean up.

```
cerberus close [flags]
  --name string  session name (required if multiple active)
```

### `review`

Show what changed in a session.

```
cerberus review [flags]
  --name string  session name (required if multiple active)
  --diff         print full unified diffs
```

### `status`

Print session state: branch, status, tokens used, cost.

```
cerberus status [flags]
  --name string  session name (required if multiple active)
```

### `logs`

Print the agent's full output log.

```
cerberus logs [flags]
  --name string  session name (required if multiple active)
```

### `clean`

Remove session worktree, branch, and state. Stops any running container.

```
cerberus clean [flags]
  --name string  session name (required if multiple active)
  --all          clean all sessions
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
cerberus start --prompt "fix the null pointer exception in auth.go"
cerberus review --diff
cerberus clean
```

### Multi-turn exploration

```bash
cerberus start --name research --prompt "survey the codebase for rate limiting implementations"
cerberus rerun --name research --prompt "now add token bucket rate limiting to the API"
cerberus review --name research
cerberus clean --name research
```

### Interactive debugging

```bash
cerberus chat --name debug --prompt "start with the failing test case" --image myimage
cerberus message --name debug --message "add debug output in function X"
cerberus message --name debug --message "commit what you have"
cerberus close --name debug
```

### Local model via Ollama

```bash
cerberus start --profile-file ./profiles/ollama.json --prompt "refactor the auth handler"
```

## Output Modes

By default, cerberus prints human-readable text deltas prefixed with the session name. Two flags control output behavior:

### `--output jsonl`

Emit one JSON object per line to stdout instead of text. Each event has this shape:

```json
{"type":"text_delta","session":"my-session","ts":"...","content":"hello"}
{"type":"message_end","session":"my-session","ts":"...","usage":{"input_tokens":100,"output_tokens":50,"cache_read_tokens":0,"cache_write_tokens":0,"cost_usd":0.001}}
{"type":"turn_complete","session":"my-session","ts":"...","status":"waiting"}
```

Event types: `session_start`, `text_delta`, `tool_use`, `tool_result`, `message_end`, `turn_complete`, `log`, `raw`.

### `--callback <url>`

POST each event as JSON to the given URL. Callback errors are logged to stderr but do not stop the run. Can be combined with `--output`:

```bash
cerberus start --output jsonl --callback https://example.com/hook --prompt "fix the bug"
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
