# Cerberus Rewrite Plan

## Goal

Strip Cerberus down to its namesake: one tool, one agent, one isolated container per run.
Multi-agent orchestration is the responsibility of a separate orchestrator tool.
Cerberus is the mule — it takes a prompt, runs an agent in isolation, commits the result, and exits.

---

## What Gets Stripped

- All goroutine / parallel runner logic
- `Runner[]` config (replaced by CLI flags)
- `Solutions []Solution` + `Selections` in state
- `CallerModel` / `--caller` flag
- `SubAgentPreamble`
- `skills/SKILL.md`
- Commands: `list`, `apply`, `merge`, `merge-apply`
- Git functions only used by `apply`: `CherryPick`, `CherryPickRange`, `CommitsBetween`
- `Runners []StatsRunner` in stats (replaced by flat single-run record)

---

## Commands (Surviving)

| Command | Notes |
|---|---|
| `start` | Single agent, single container. Add `--name` flag (auto-generated if omitted). |
| `rerun` | Follow-up prompt in same worktree/container. |
| `status` | Single run state, no table of solutions. |
| `logs` | Unchanged. |
| `review` | Unchanged. |
| `clean` | Stops container, removes worktree, branch, session dir. |
| `stats` | Global `~/.config/cerberus/stats.json`, one flat record per run. |
| `version` | Unchanged. |

---

## Isolation Model: Docker + Git Worktree

**Runtime:** Docker (host runs cerberus, agent runs inside container)

**Isolation strategy:**
- Cerberus creates a git worktree on the host per session
- That worktree is bind-mounted into the container at `/workspace`
- The agent (opencode) is baked into the container image
- Opencode config (`~/.config/opencode/`) is bind-mounted read-only from host
- Container is ephemeral (`--rm`), exits when agent exits
- Cerberus (on host) checks for changes, commits, then cleans up

**Package access:**
- A local Nexus instance acts as a proxy for package registries
- This is entirely an image/network concern — cerberus does not configure it

**Image selection:**
- Single base image for now
- `--image` flag on `start` to override (for future per-language images)
- Default image stored in `config.json`

**docker run shape:**
```
docker run --rm \
  -v <worktree>:/workspace \
  -v ~/.config/opencode:/root/.config/opencode:ro \
  -w /workspace \
  <image> \
  opencode run --format json [-m <model>] [--agent <oc_agent>] "<prompt>"
```

**Committing:** Host runs `git add -A && git commit` in the worktree after container exits.
No git tooling needed inside the container.

---

## start Flow

```
cerberus start --model <m> --name <n> --image <img> "<prompt>"
        │
        ▼
Load ~/.config/cerberus/config.json
        │
        ▼
Resolve repo root (git rev-parse)
Resolve session name (--name or auto-generate)
        │
        ▼
Create git worktree on host
  .cerberus/sessions/<name>/worktrees/solve/
  branch: cerberus/<name>
        │
        ▼
Write state.json (status: pending)
        │
        ▼
docker run --rm
  -v <worktree>:/workspace
  -v ~/.config/opencode:/root/.config/opencode:ro
  -w /workspace
  <image>
  opencode run --format json [-m model] [--agent oc_agent] "<prompt>"
        │
        ├── stream JSON lines from docker stdout
        │     • write raw → log file
        │     • parse text → stdout
        │     • accumulate tokens/cost
        │     • extract sessionID
        │     update state.json (status: running, containerID)
        │
        ▼
Container exits
        │
        ├── exit non-zero ──► update state (status: failed)
        │                     append stats.json
        │                     exit with same code
        │
        ▼ exit 0
HasChanges in worktree? (git status on host)
        │
        ├── no changes ──► update state (status: done, no commit)
        │                  append stats.json
        │                  print JSON summary
        │                  exit 0
        │
        ▼ yes
AskForCommitMessage
  (opencode run on HOST in worktree dir)
        │
        ▼
git add -A && git commit (host, in worktree)
        │
        ▼
Update state.json
  status: done, commitHash, finishedAt, cost, tokens
        │
        ▼
Append to ~/.config/cerberus/stats.json
        │
        ▼
Print JSON summary to stdout (machine-readable for orchestrator)
  {"session":"...","status":"done","branch":"...","commit":"...","cost_usd":...}
        │
        ▼
exit 0
```

---

## clean Flow

```
cerberus clean [--name <n>]
  └── docker stop/rm <containerID> if still running (from state.json)
  └── git worktree remove --force (host)
  └── git branch -D cerberus/<name> (host)
  └── rm -rf .cerberus/sessions/<name>/
```

---

## Data Models

### `UserConfig` (`~/.config/cerberus/config.json`)
```go
type UserConfig struct {
    Instructions string `json:"instructions"`
    DefaultModel string `json:"default_model,omitempty"`
    DefaultImage string `json:"default_image,omitempty"`
}
```

### `State` (`.cerberus/sessions/<name>/state.json`)
```go
type State struct {
    Name       string
    BaseBranch string
    BaseCommit string
    Prompt     string
    Run        Run
}

type Run struct {
    Branch           string
    Worktree         string
    Agent            string
    Model            string
    OcAgent          string
    Image            string
    ContainerID      string
    Status           RunStatus   // pending | running | done | failed
    PID              int
    LogFile          string
    ExitCode         int
    SessionID        string
    CommitHash       string
    StartedAt        time.Time
    FinishedAt       time.Time
    InputTokens      int
    OutputTokens     int
    CacheReadTokens  int
    CacheWriteTokens int
    CostUSD          float64
}
```

### `StatsRecord` (`~/.config/cerberus/stats.json`, appended per run)
```go
type StatsRecord struct {
    SessionDate      time.Time
    SessionName      string
    PromptSnippet    string
    BaseBranch       string
    Model            string
    OcAgent          string
    Image            string
    Status           string
    DurationS        float64
    InputTokens      int
    OutputTokens     int
    CacheReadTokens  int
    CacheWriteTokens int
    CostUSD          float64
}
```

---

## Code Structure Changes

### New: `internal/docker/docker.go`
Wraps `docker run` and `docker rm -f`. Keeps docker logic out of `main.go`.

```go
type RunArgs struct {
    Image      string
    Workdir    string
    Mounts     []Mount      // {Host, Container, ReadOnly}
    Cmd        []string
    Stdout     io.Writer
    Stderr     io.Writer
}

func Run(ctx context.Context, args RunArgs) (containerID string, exitCode int, err error)
func Stop(containerID string) error
```

### `internal/agent/opencode.go`
Builds the `opencode run ...` args only (not the docker args).
`main.go` or `docker.go` wraps them in the docker invocation.

### `internal/agent/commit.go`
Unchanged. Runs on host in worktree dir.

### `internal/git/worktree.go`
- Keep: `CreateWorktree`, `RemoveWorktree`, `HasChanges`, `CommitAll`, `CommitAndGetHash`, `Diff`, `ShowDiff`, `ChangedFiles`, `RepoRoot`, `CurrentBranch`, `CurrentCommit`
- Remove: `CherryPick`, `CherryPickRange`, `CommitsBetween` (only used by `apply`)

### `cmd/cerberus/main.go`
- Remove: `cmdList`, `cmdApply`, `cmdMerge`, `cmdMergeApply`, all goroutine/WaitGroup/Mutex code
- Remove: `resolveSession` multi-session logic (each invocation is one session)
- Add: `--name`, `--image` flags on `start`
- `start` becomes a straight sequential function: worktree → docker run → commit → stats

---

## Orchestrator Interface (Exit Behaviour)

Cerberus is designed to be called as a subprocess by an orchestrator.

- Exit code mirrors the agent's exit code
- Final stdout line is always a JSON summary:
  ```json
  {"session":"my-name","status":"done","branch":"cerberus/my-name","commit":"abc123","cost_usd":0.042}
  ```
- Orchestrator passes `--name <id>` to track its children
- Orchestrator reads `state.json` at `.cerberus/sessions/<name>/state.json` for full detail

---

## What Does NOT Change

- Session directory layout: `.cerberus/sessions/<name>/`
- Log file format: raw JSON lines from opencode piped through docker stdout
- Auto-commit after agent finishes (host-side)
- `AskForCommitMessage` (runs on host)
- `stats` persistence path and append behaviour
- Session auto-naming if `--name` is omitted
