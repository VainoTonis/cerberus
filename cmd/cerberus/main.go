package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/tonis/cerberus/internal/agent"
	"github.com/tonis/cerberus/internal/config"
	"github.com/tonis/cerberus/internal/docker"
	"github.com/tonis/cerberus/internal/event"
	"github.com/tonis/cerberus/internal/git"
	"github.com/tonis/cerberus/internal/stream"
)

// version is set at build time via -ldflags "-X main.version=..."
var version = "dev"

func main() {
	rootCmd := &cobra.Command{
		Use:   "cerberus",
		Short: "Run an agent in isolation, commit the result",
		Long:  "cerberus - single-agent executor with auto-commit and stats tracking",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	rootCmd.AddCommand(
		cmdStartCommand(),
		cmdRerunCommand(),
		cmdChatCommand(),
		cmdMessageCommand(),
		cmdCloseCommand(),
		cmdStatusCommand(),
		cmdLogsCommand(),
		cmdReviewCommand(),
		cmdCleanCommand(),
		cmdApplyCommand(),
		cmdStatsCommand(),
		cmdPsCommand(),
		cmdVersionCommand(),
	)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func cmdStartCommand() *cobra.Command {
	var name, prompt, promptFile, agentFlag, modelFlag, imageFlag, profileFile string
	var outputFlag, callbackFlag, invokerFlag string

	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start a single agent run in an isolated worktree",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmdStart(name, prompt, promptFile, agentFlag, modelFlag, imageFlag, profileFile, outputFlag, callbackFlag, invokerFlag)
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "session name (auto-generated if empty)")
	cmd.Flags().StringVar(&prompt, "prompt", "", "prompt to send to agent (required)")
	cmd.Flags().StringVar(&promptFile, "prompt-file", "", "read prompt from file instead of -prompt")
	cmd.Flags().StringVar(&agentFlag, "agent", "pi", "agent to use (default: pi)")
	cmd.Flags().StringVar(&modelFlag, "model", "", "model to use (default from config or agent)")
	cmd.Flags().StringVar(&imageFlag, "image", "", "docker image (default from config)")
	cmd.Flags().StringVar(&profileFile, "profile-file", "", "path to a profile JSON file to override model, image, and env vars")
	cmd.Flags().StringVar(&outputFlag, "output", "text", "output format: text (default) or jsonl")
	cmd.Flags().StringVar(&callbackFlag, "callback", "", "URL to POST events to as they happen")
	cmd.Flags().StringVar(&invokerFlag, "invoker", "cli", "invoker identifier for stats tracking")

	cmd.RegisterFlagCompletionFunc("name", completionSessions)

	return cmd
}

func cmdRerunCommand() *cobra.Command {
	var name, prompt, promptFile, profileFile string
	var outputFlag, callbackFlag, invokerFlag string

	cmd := &cobra.Command{
		Use:   "rerun",
		Short: "Run the agent again in an existing session worktree",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmdRerun(name, prompt, promptFile, profileFile, outputFlag, callbackFlag, invokerFlag)
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "session name (required if multiple sessions exist)")
	cmd.Flags().StringVar(&prompt, "prompt", "", "follow-up prompt for agent (required)")
	cmd.Flags().StringVar(&promptFile, "prompt-file", "", "read prompt from file instead of -prompt")
	cmd.Flags().StringVar(&profileFile, "profile-file", "", "path to a profile JSON file to override model, image, and env vars (overrides stored profile)")
	cmd.Flags().StringVar(&outputFlag, "output", "text", "output format: text (default) or jsonl")
	cmd.Flags().StringVar(&callbackFlag, "callback", "", "URL to POST events to as they happen")
	cmd.Flags().StringVar(&invokerFlag, "invoker", "cli", "invoker identifier for stats tracking")

	cmd.RegisterFlagCompletionFunc("name", completionSessions)

	return cmd
}

func cmdStatusCommand() *cobra.Command {
	var name string

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show the status of a session",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmdStatus(name)
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "session name (required if multiple sessions exist)")

	cmd.RegisterFlagCompletionFunc("name", completionSessions)

	return cmd
}

func cmdLogsCommand() *cobra.Command {
	var name string

	cmd := &cobra.Command{
		Use:   "logs",
		Short: "Print session logs",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmdLogs(name)
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "session name (required if multiple sessions exist)")

	cmd.RegisterFlagCompletionFunc("name", completionSessions)

	return cmd
}

func cmdReviewCommand() *cobra.Command {
	var name string
	var diffFlag bool

	cmd := &cobra.Command{
		Use:   "review",
		Short: "Show what changed in a session",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmdReview(name, diffFlag)
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "session name (required if multiple sessions exist)")
	cmd.Flags().BoolVar(&diffFlag, "diff", false, "print full unified diffs")

	cmd.RegisterFlagCompletionFunc("name", completionSessions)

	return cmd
}

func cmdCleanCommand() *cobra.Command {
	var name string
	var all bool

	cmd := &cobra.Command{
		Use:   "clean",
		Short: "Remove a session's worktree, branch, and state",
		RunE: func(cmd *cobra.Command, args []string) error {
			repoRoot, err := resolveRepoRoot()
			if err != nil {
				return err
			}
			if all {
				return cmdCleanAll(repoRoot)
			}
			return cmdClean(repoRoot, name)
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "session name (required if multiple sessions exist)")
	cmd.Flags().BoolVar(&all, "all", false, "clean all sessions")
	cmd.RegisterFlagCompletionFunc("name", completionSessions)

	return cmd
}

func cmdApplyCommand() *cobra.Command {
	var name string

	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Merge a done session's branch into the current branch and clean up",
		RunE: func(cmd *cobra.Command, args []string) error {
			repoRoot, err := resolveRepoRoot()
			if err != nil {
				return err
			}
			return cmdApply(repoRoot, name)
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "session name (required if multiple sessions exist)")
	cmd.RegisterFlagCompletionFunc("name", completionSessions)

	return cmd
}

func cmdApply(repoRoot, name string) error {
	sessionName, err := resolveSession(repoRoot, name)
	if err != nil {
		return err
	}

	state, err := config.Load(repoRoot, sessionName)
	if err != nil {
		return err
	}

	if state.Run.Status != config.StatusDone {
		return fmt.Errorf("session %q is not done (status: %s)", sessionName, state.Run.Status)
	}

	// Get current branch
	out, err := exec.Command("git", "-C", repoRoot, "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err != nil {
		return fmt.Errorf("get current branch: %w", err)
	}
	currentBranch := strings.TrimSpace(string(out))

	sessionBranch := state.Run.Branch

	// Fast-forward merge
	mergeCmd := exec.Command("git", "-C", repoRoot, "merge", "--ff-only", sessionBranch)
	if mergeOut, err := mergeCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("merge %s into %s: %s", sessionBranch, currentBranch, strings.TrimSpace(string(mergeOut)))
	}

	fmt.Printf("merged %s into %s\n", sessionBranch, currentBranch)

	return cmdClean(repoRoot, sessionName)
}

func cmdStatsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "stats",
		Short: "Show performance statistics",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmdStats()
		},
	}

	return cmd
}

func cmdVersionCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "version",
		Short: "Print the build version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println(version)
		},
	}

	return cmd
}

// relativeTime formats a time as a relative duration like "5m ago" or "2h ago".
func relativeTime(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	duration := time.Since(t)
	if duration < 0 {
		duration = -duration
	}

	switch {
	case duration < time.Minute:
		return fmt.Sprintf("%ds ago", int(duration.Seconds()))
	case duration < time.Hour:
		return fmt.Sprintf("%dm ago", int(duration.Minutes()))
	case duration < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(duration.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(duration.Hours()/24))
	}
}

// cmdPs lists all sessions in a table format.
type psEntry struct {
	repo        string
	sessionName string
	state       *config.State
	loadErr     error
}

func cmdPs() error {
	// Collect repos to scan: registry + current repo (if resolvable).
	seen := map[string]bool{}
	var repos []string

	registered, _ := config.LoadRepoRegistry()
	for _, r := range registered {
		if !seen[r] {
			seen[r] = true
			repos = append(repos, r)
		}
	}
	if cur, err := resolveRepoRoot(); err == nil {
		if !seen[cur] {
			seen[cur] = true
			repos = append(repos, cur)
		}
	}

	var entries []psEntry
	for _, repo := range repos {
		sessions, err := config.ListSessions(repo)
		if err != nil {
			continue
		}
		for _, sessionName := range sessions {
			state, err := config.Load(repo, sessionName)
			entries = append(entries, psEntry{repo: repo, sessionName: sessionName, state: state, loadErr: err})
		}
	}

	if len(entries) == 0 {
		fmt.Println("no sessions")
		return nil
	}

	// Print table header
	fmt.Printf("%-20s  %-22s  %-15s  %-10s  %-12s  %s\n",
		"NAME", "STATUS", "MODEL", "AGENT", "STARTED", "REPO")
	fmt.Println(strings.Repeat("-", 100))

	for _, e := range entries {
		if e.loadErr != nil {
			fmt.Printf("%-20s  error: %v\n", e.sessionName, e.loadErr)
			continue
		}

		r := &e.state.Run

		orphanedStr := ""
		if r.Status == config.StatusRunning || r.Status == config.StatusWaiting {
			if r.Interactive {
				if r.ContainerID != "" && !docker.IsContainerRunning(r.ContainerID) {
					orphanedStr = "[orphaned]"
				}
			} else {
				if r.Status == config.StatusRunning {
					orphanedStr = "[orphaned]"
				}
			}
		}

		name := truncate(e.sessionName, 20)
		modelStr := truncate(r.Model, 15)
		agent := truncate(r.Agent, 10)
		relTime := relativeTime(r.StartedAt)

		status := string(r.Status)
		if orphanedStr != "" {
			status = status + " " + orphanedStr
		}

		fmt.Printf("%-20s  %-22s  %-15s  %-10s  %-12s  %s\n",
			name, status, modelStr, agent, relTime, e.repo)
	}

	return nil
}

func cmdPsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ps",
		Short: "List all sessions",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmdPs()
		},
	}

	return cmd
}

func completionSessions(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	repoRoot, err := resolveRepoRoot()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	sessions, err := config.ListSessions(repoRoot)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return sessions, cobra.ShellCompDirectiveNoFileComp
}

// resolveRepoRoot finds the git repo root from current directory.
func resolveRepoRoot() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("get working directory: %w", err)
	}
	return git.RepoRoot(cwd)
}

// resolveSession resolves the session name to use.
// If name is non-empty it is returned as-is.
// If name is empty and exactly one session exists, that session is returned.
// Otherwise an error is returned.
func resolveSession(repoRoot, name string) (string, error) {
	if name != "" {
		return name, nil
	}
	sessions, err := config.ListSessions(repoRoot)
	if err != nil {
		return "", err
	}
	switch len(sessions) {
	case 0:
		return "", fmt.Errorf("no cerberus sessions found; run 'cerberus start' first")
	case 1:
		return sessions[0], nil
	default:
		return "", fmt.Errorf("multiple sessions active (%s); specify one with --name", strings.Join(sessions, ", "))
	}
}

// generateSessionName creates an adjective-noun style name.
func generateSessionName() string {
	adjectives := []string{"swift", "clever", "bold", "keen", "wise", "quick", "sharp", "bright", "strong", "brave"}
	nouns := []string{"hawk", "fox", "wolf", "eagle", "lion", "tiger", "bear", "raven", "otter", "lynx"}
	return adjectives[rand.Intn(len(adjectives))] + "-" + nouns[rand.Intn(len(nouns))]
}

// createWorktreePath creates a worktree at a specific path with a given branch.
func createWorktreePath(repoRoot, wtPath, branchName, baseCommit string) (string, error) {
	// Remove if it already exists (idempotent).
	cmd := exec.Command("git", "-C", repoRoot, "worktree", "remove", "--force", wtPath)
	cmd.Run() // Ignore error

	// Create new worktree
	cmd = exec.Command("git", "-C", repoRoot, "worktree", "add", "-b", branchName, wtPath, baseCommit)
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git worktree add: %w", err)
	}

	return wtPath, nil
}

// cmdStart creates a git worktree and runs an agent inside a docker container,
// then commits the result.
func cmdStart(sessionName, prompt, promptFile, agentFlag, modelFlag, imageFlag, profileFile, output, callback, invoker string) error {
	userCfg, err := config.LoadUserConfig()
	if err != nil {
		return err
	}

	if profileFile != "" {
		p, err := config.LoadProfileFile(profileFile)
		if err != nil {
			return err
		}
		config.ApplyProfile(&userCfg, p)
	}

	// Auto-generate session name if empty
	if sessionName == "" {
		sessionName = generateSessionName()
	}

	// Resolve prompt
	resolvedPrompt := strings.TrimSpace(prompt)
	if promptFile != "" {
		data, err := os.ReadFile(promptFile)
		if err != nil {
			return fmt.Errorf("read prompt file: %w", err)
		}
		resolvedPrompt = strings.TrimSpace(string(data))
	}
	if resolvedPrompt == "" {
		return fmt.Errorf("--prompt or --prompt-file is required")
	}

	// Prepend user instructions if configured
	if userCfg.Instructions != "" {
		resolvedPrompt = userCfg.Instructions + "\n\n" + resolvedPrompt
	}

	// Validate agent
	agentImpl, err := agent.Get(agentFlag)
	if err != nil {
		return err
	}

	// Resolve model (use --model flag, fallback to config default)
	model := modelFlag
	if model == "" {
		model = userCfg.DefaultModel
	}

	// Resolve image (use --image flag, fallback to config default)
	image := imageFlag
	if image == "" {
		image = userCfg.DefaultImage
	}
	if image == "" {
		return fmt.Errorf("no docker image configured; use --image or set default_image in ~/.config/cerberus/config.json")
	}

	repoRoot, err := resolveRepoRoot()
	if err != nil {
		return err
	}

	// Refuse to start if this session name is already in use
	if _, err := config.Load(repoRoot, sessionName); err == nil {
		return fmt.Errorf("session %q already exists; run 'cerberus clean --name %s' first", sessionName, sessionName)
	}

	baseBranch, err := git.CurrentBranch(repoRoot)
	if err != nil {
		return err
	}
	baseCommit, err := git.CurrentCommit(repoRoot)
	if err != nil {
		return err
	}

	// Create worktree at .cerberus/sessions/<name>/worktrees/solve
	repoStateDir, err := config.RepoStateDir(repoRoot)
	if err != nil {
		return fmt.Errorf("get repo state dir: %w", err)
	}
	wtPath := filepath.Join(repoStateDir, "sessions", sessionName, "worktrees", "solve")
	branchName := "cerberus/" + sessionName

	if _, err := createWorktreePath(repoRoot, wtPath, branchName, baseCommit); err != nil {
		return fmt.Errorf("create worktree: %w", err)
	}

	// Initialize state.json
	logPath, err := config.LogPath(repoRoot, sessionName)
	if err != nil {
		return fmt.Errorf("get log path: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		return fmt.Errorf("create log dir: %w", err)
	}

	workDir, err := os.Getwd()
	if err != nil {
		workDir = ""
	}

	state := &config.State{
		Name:       sessionName,
		BaseBranch: baseBranch,
		BaseCommit: baseCommit,
		Prompt:     resolvedPrompt,
		Run: config.Run{
			Branch:      branchName,
			Worktree:    wtPath,
			Agent:       agentFlag,
			Model:       model,
			Image:       image,
			ProfileFile: profileFile,
			Status:      config.StatusPending,
			LogFile:     logPath,
			StartedAt:   time.Now(),
			WorkDir:     workDir,
			InvokedBy:   invoker,
		},
	}

	if err := config.Save(repoRoot, state); err != nil {
		return fmt.Errorf("save state: %w", err)
	}
	if err := config.RegisterRepo(repoRoot); err != nil {
		fmt.Fprintf(os.Stderr, "warn: register repo: %v\n", err)
	}

	fmt.Printf("session: %s\n", sessionName)
	fmt.Printf("branch:  %s (%s)\n", baseBranch, baseCommit[:8])
	fmt.Printf("agent:   %s\n\n", agentFlag)

	// Run agent in docker
	exitCode, err := runAgentInDocker(repoRoot, state, resolvedPrompt, agentImpl, model, userCfg, output, callback)
	if err != nil {
		return err
	}

	if exitCode != 0 {
		state.Run.Status = config.StatusFailed
		state.Run.FailReason = fmt.Sprintf("exit code %d", exitCode)
		state.Run.ExitCode = exitCode
		state.Run.FinishedAt = time.Now()
		if err := config.Save(repoRoot, state); err != nil {
			fmt.Fprintf(os.Stderr, "save state: %v\n", err)
		}
		if err := appendStats(state); err != nil {
			fmt.Fprintf(os.Stderr, "append stats: %v\n", err)
		}
		return fmt.Errorf("agent exited with code %d", exitCode)
	}

	// Check for changes
	hasChanges, err := git.HasChanges(wtPath)
	if err != nil {
		return fmt.Errorf("check changes: %w", err)
	}

	if !hasChanges {
		state.Run.Status = config.StatusDone
		state.Run.FinishedAt = time.Now()
		if err := config.Save(repoRoot, state); err != nil {
			return fmt.Errorf("save state: %w", err)
		}
		if err := appendStats(state); err != nil {
			fmt.Fprintf(os.Stderr, "warning: append stats: %v\n", err)
		}
		fmt.Printf("[%s] no changes to commit\n", sessionName)
		printJSONSummary(state)
		return nil
	}

	// Ask for commit message and commit
	fmt.Printf("[%s] committing...\n", sessionName)

	diff, err := git.StageAndDiff(wtPath, baseCommit)
	if err != nil {
		diff = ""
	}

	commitMsg := agent.AskForCommitMessage(wtPath, diff, model, invoker)
	commitHash, err := git.CommitAndGetHash(wtPath, commitMsg)
	if err != nil {
		return fmt.Errorf("commit failed: %w", err)
	}

	state.Run.CommitHash = commitHash
	state.Run.Status = config.StatusDone
	state.Run.FinishedAt = time.Now()
	if err := config.Save(repoRoot, state); err != nil {
		return fmt.Errorf("save state: %w", err)
	}

	if err := appendStats(state); err != nil {
		fmt.Fprintf(os.Stderr, "warning: append stats: %v\n", err)
	}

	fmt.Printf("[%s] commit %s  %s\n", sessionName, commitHash[:8], commitMsg)
	printJSONSummary(state)
	return nil
}

// runAgentInDocker executes the agent in a docker container and streams output.
func runAgentInDocker(repoRoot string, state *config.State, prompt string, agentImpl agent.Agent, model string, userCfg config.UserConfig, output, callback string) (int, error) {
	sessionName := state.Name
	wtPath := state.Run.Worktree
	logPath := state.Run.LogFile

	cmdArgs, err := agentImpl.Args(agent.RunArgs{
		Prompt: prompt,
		Model:  model,
	})
	if err != nil {
		return 1, fmt.Errorf("build command: %w", err)
	}

	logFile, err := os.Create(logPath)
	if err != nil {
		return 1, fmt.Errorf("create log file: %w", err)
	}
	defer logFile.Close()

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return 1, fmt.Errorf("get home dir: %w", err)
	}

	mounts := []docker.Mount{
		{Host: wtPath, Container: "/workspace"},
		{Host: filepath.Join(homeDir, ".pi", "agent"), Container: "/home/agent/.pi/agent"},
	}

	gradleInitD := filepath.Join(homeDir, ".gradle", "init.d")
	if _, err := os.Stat(gradleInitD); err == nil {
		mounts = append(mounts, docker.Mount{Host: gradleInitD, Container: "/home/agent/.gradle/init.d", ReadOnly: true})
	}

	awsDir := filepath.Join(homeDir, ".aws")
	if _, err := os.Stat(awsDir); err == nil {
		mounts = append(mounts, docker.Mount{Host: awsDir, Container: "/home/agent/.aws", ReadOnly: true})
	}

	if err := ensureCopilotToken(); err != nil {
		return 0, fmt.Errorf("copilot token refresh: %w", err)
	}

	if err := requireProxyNetwork(); err != nil {
		return 0, err
	}

	envFile := agentEnvFilePath()

	pipeR, pipeW := io.Pipe()

	ctx, cancelRun := context.WithCancel(context.Background())
	defer cancelRun()

	emitter := buildEmitter(sessionName, output, callback)

	proc := stream.NewProcessor(sessionName, emitter, logFile, stream.Limits{
		MaxTurns:        userCfg.EffectiveMaxTurns(),
		MaxOutputTokens: userCfg.EffectiveMaxOutputTokens(),
	}, cancelRun)

	go func() {
		defer pipeR.Close()
		proc.Process(pipeR)
	}()

	awsEnv := map[string]string{
		"AWS_PROFILE":           userCfg.AWSProfile,
		"AWS_REGION":            userCfg.AWSRegion,
		"AWS_DEFAULT_REGION":    userCfg.AWSRegion,
		"AWS_ACCESS_KEY_ID":     "",
		"AWS_SECRET_ACCESS_KEY": "",
		"AWS_SESSION_TOKEN":     "",
	}
	var envVars []string
	for key, cfgVal := range awsEnv {
		if val := os.Getenv(key); val != "" {
			envVars = append(envVars, key+"="+val)
		} else if cfgVal != "" {
			envVars = append(envVars, key+"="+cfgVal)
		}
	}
	envVars = append(envVars, "PI_CODING_AGENT_SESSION_DIR=/tmp/pi-sessions")
	for k, v := range userCfg.ExtraEnv {
		envVars = append(envVars, k+"="+v)
	}

	runArgs := docker.RunArgs{
		Image:    state.Run.Image,
		Workdir:  "/workspace",
		Mounts:   mounts,
		Cmd:      cmdArgs,
		Env:      envVars,
		EnvFile:  envFile,
		Networks: []string{"sandbox-internal"},
		Stdout:   pipeW,
		Stderr:   pipeW,
	}

	containerID, exitCode, err := docker.Run(ctx, runArgs)
	pipeW.Close()

	if containerID != "" {
		state.Run.ContainerID = containerID
	}

	stats := proc.Stats()
	if stats.SessionID != "" {
		state.Run.SessionID = stats.SessionID
	}
	state.Run.InputTokens = stats.InputTokens
	state.Run.OutputTokens = stats.OutputTokens
	state.Run.CacheReadTokens = stats.CacheReadTokens
	state.Run.CacheWriteTokens = stats.CacheWriteTokens
	state.Run.CostUSD = stats.CostUSD
	config.Save(repoRoot, state)

	emitter.Close()

	if err != nil {
		return exitCode, fmt.Errorf("docker run: %w", err)
	}

	state.Run.Status = config.StatusRunning
	config.Save(repoRoot, state)

	return exitCode, nil
}

// ensureCopilotToken checks if the GitHub Copilot token is expiring soon and refreshes it if needed.
func ensureCopilotToken() error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("get home dir: %w", err)
	}

	authPath := filepath.Join(homeDir, ".pi", "agent", "auth.json")

	// Check if file exists
	data, err := os.ReadFile(authPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // Not everyone uses copilot
		}
		return fmt.Errorf("read auth.json: %w", err)
	}

	// Parse as map[string]json.RawMessage
	var authMap map[string]json.RawMessage
	if err := json.Unmarshal(data, &authMap); err != nil {
		return nil // Invalid JSON, skip
	}

	// Check github-copilot entry
	copilotRaw, exists := authMap["github-copilot"]
	if !exists {
		return nil // No github-copilot entry
	}

	// Parse github-copilot entry
	var copilotEntry struct {
		Expires int64 `json:"expires"`
	}
	if err := json.Unmarshal(copilotRaw, &copilotEntry); err != nil {
		return nil // Cannot parse, skip
	}

	// Check if expires within 10 minutes
	if copilotEntry.Expires == 0 {
		return nil // No expiry set
	}

	if time.Now().Add(10*time.Minute).UnixMilli() < copilotEntry.Expires {
		return nil // Not expiring soon
	}

	// Need to refresh - acquire exclusive lock
	lockPath := filepath.Join(homeDir, ".pi", "agent", "refresh.lock")

	lockFile, err := os.OpenFile(lockPath, os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open lock file: %w", err)
	}
	defer lockFile.Close()

	// Acquire exclusive lock
	if err := syscall.Flock(int(lockFile.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("acquire lock: %w", err)
	}
	defer syscall.Flock(int(lockFile.Fd()), syscall.LOCK_UN)

	// Re-read auth.json to check if another process already refreshed
	data, err = os.ReadFile(authPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("re-read auth.json: %w", err)
	}

	var authMapRecheck map[string]json.RawMessage
	if err := json.Unmarshal(data, &authMapRecheck); err != nil {
		return nil
	}

	copilotRawRecheck, exists := authMapRecheck["github-copilot"]
	if exists {
		var copilotEntryRecheck struct {
			Expires int64 `json:"expires"`
		}
		if err := json.Unmarshal(copilotRawRecheck, &copilotEntryRecheck); err == nil {
			if copilotEntryRecheck.Expires != 0 && time.Now().Add(10*time.Minute).UnixMilli() < copilotEntryRecheck.Expires {
				return nil // Already refreshed by another process
			}
		}
	}

	// Run pi command with 30s timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "pi", "-p", "say: ok")
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("refresh token: %w", err)
	}

	return nil
}

// networkExists checks if a docker network exists.
func networkExists(networkName string) bool {
	cmd := exec.Command("docker", "network", "inspect", networkName)
	return cmd.Run() == nil
}

// requireProxyNetwork returns an error if the sandbox-internal Docker network
// does not exist. The proxy stack (proxy/docker-compose.yml) must be running.
func requireProxyNetwork() error {
	if !networkExists("sandbox-internal") {
		return fmt.Errorf("proxy network not running — start it with:\n  cd proxy && docker compose up -d")
	}
	return nil
}

// agentEnvFilePath returns the path to ~/.cerberus/agent.env, or empty
// string if the file does not exist.
func agentEnvFilePath() string {
	home, err := config.CerberusHome()
	if err != nil {
		return ""
	}
	p := filepath.Join(home, "agent.env")
	if _, err := os.Stat(p); err != nil {
		return ""
	}
	return p
}

// cmdRerun runs the agent again in an existing worktree with a new prompt.
func cmdRerun(name, prompt, promptFile, profileFile, output, callback, invoker string) error {
	repoRoot, err := resolveRepoRoot()
	if err != nil {
		return err
	}

	sessionName, err := resolveSession(repoRoot, name)
	if err != nil {
		return err
	}

	state, err := config.Load(repoRoot, sessionName)
	if err != nil {
		return err
	}

	// Check if already running
	if state.Run.Status == config.StatusRunning {
		return fmt.Errorf("session is still running; wait for it to finish first")
	}

	// Resolve prompt
	resolvedPrompt := strings.TrimSpace(prompt)
	if promptFile != "" {
		data, err := os.ReadFile(promptFile)
		if err != nil {
			return fmt.Errorf("read prompt file: %w", err)
		}
		resolvedPrompt = strings.TrimSpace(string(data))
	}
	if resolvedPrompt == "" {
		return fmt.Errorf("--prompt or --prompt-file is required")
	}

	// Store invoker if provided
	if invoker != "" && invoker != "cli" {
		state.Run.InvokedBy = invoker
		config.Save(repoRoot, state)
	}
	agentImpl, err := agent.Get(state.Run.Agent)
	if err != nil {
		return err
	}

	// Resolve user config, applying profile. --profile-file flag overrides stored profile.
	userCfg, err := config.LoadUserConfig()
	if err != nil {
		return err
	}
	effectiveProfileFile := state.Run.ProfileFile
	if profileFile != "" {
		effectiveProfileFile = profileFile
		state.Run.ProfileFile = profileFile
	}
	if effectiveProfileFile != "" {
		p, err := config.LoadProfileFile(effectiveProfileFile)
		if err != nil {
			return err
		}
		config.ApplyProfile(&userCfg, p)
	}

	// Append to log file
	logFile, err := os.OpenFile(state.Run.LogFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open log file: %w", err)
	}
	defer logFile.Close()

	fmt.Fprintf(logFile, "\n--- rerun: %s ---\n", time.Now().Format(time.RFC3339))
	fmt.Printf("rerunning session %q with new prompt...\n", sessionName)

	// Run agent again
	exitCode, err := runAgentInDockerRerun(repoRoot, state, resolvedPrompt, agentImpl, logFile, userCfg, output, callback)
	if err != nil {
		return err
	}

	if exitCode != 0 {
		state.Run.Status = config.StatusFailed
		state.Run.FailReason = fmt.Sprintf("exit code %d", exitCode)
		state.Run.ExitCode = exitCode
		state.Run.FinishedAt = time.Now()
		config.Save(repoRoot, state)
		if err := appendStats(state); err != nil {
			fmt.Fprintf(os.Stderr, "warning: append stats: %v\n", err)
		}
		return fmt.Errorf("agent exited with code %d", exitCode)
	}

	// Check for changes
	hasChanges, err := git.HasChanges(state.Run.Worktree)
	if err != nil {
		return fmt.Errorf("check changes: %w", err)
	}

	if !hasChanges {
		state.Run.Status = config.StatusDone
		state.Run.FinishedAt = time.Now()
		config.Save(repoRoot, state)
		if err := appendStats(state); err != nil {
			fmt.Fprintf(os.Stderr, "warning: append stats: %v\n", err)
		}
		fmt.Printf("[%s] no changes to commit\n", sessionName)
		return nil
	}

	// Commit changes
	fmt.Printf("[%s] committing...\n", sessionName)

	diff, _ := git.StageAndDiff(state.Run.Worktree, state.BaseCommit)
	commitMsg := agent.AskForCommitMessage(state.Run.Worktree, diff, state.Run.Model, state.Run.InvokedBy)
	commitHash, err := git.CommitAndGetHash(state.Run.Worktree, commitMsg)
	if err != nil {
		return fmt.Errorf("commit failed: %w", err)
	}

	state.Run.CommitHash = commitHash
	state.Run.Status = config.StatusDone
	state.Run.FinishedAt = time.Now()
	config.Save(repoRoot, state)
	if err := appendStats(state); err != nil {
		fmt.Fprintf(os.Stderr, "warning: append stats: %v\n", err)
	}

	fmt.Printf("[%s] commit %s  %s\n", sessionName, commitHash[:8], commitMsg)
	return nil
}

// runAgentInDockerRerun is similar to runAgentInDocker but appends to an existing log file.
func runAgentInDockerRerun(repoRoot string, state *config.State, prompt string, agentImpl agent.Agent, logFile *os.File, rerunUserCfg config.UserConfig, output, callback string) (int, error) {
	sessionName := state.Name
	wtPath := state.Run.Worktree

	cmdArgs, err := agentImpl.Args(agent.RunArgs{
		Prompt: prompt,
		Model:  state.Run.Model,
	})
	if err != nil {
		return 1, fmt.Errorf("build command: %w", err)
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return 1, fmt.Errorf("get home dir: %w", err)
	}

	mounts := []docker.Mount{
		{Host: wtPath, Container: "/workspace"},
		{Host: filepath.Join(homeDir, ".pi", "agent"), Container: "/home/agent/.pi/agent"},
	}

	gradleInitD := filepath.Join(homeDir, ".gradle", "init.d")
	if _, err := os.Stat(gradleInitD); err == nil {
		mounts = append(mounts, docker.Mount{Host: gradleInitD, Container: "/home/agent/.gradle/init.d", ReadOnly: true})
	}

	awsDir := filepath.Join(homeDir, ".aws")
	if _, err := os.Stat(awsDir); err == nil {
		mounts = append(mounts, docker.Mount{Host: awsDir, Container: "/home/agent/.aws", ReadOnly: true})
	}

	if err := ensureCopilotToken(); err != nil {
		return 0, fmt.Errorf("copilot token refresh: %w", err)
	}

	if err := requireProxyNetwork(); err != nil {
		return 0, err
	}

	envFile := agentEnvFilePath()

	pipeR, pipeW := io.Pipe()

	rerunCtx, cancelRerun := context.WithCancel(context.Background())
	defer cancelRerun()

	emitter := buildEmitter(sessionName, output, callback)

	proc := stream.NewProcessor(sessionName, emitter, logFile, stream.Limits{
		MaxTurns:        rerunUserCfg.EffectiveMaxTurns(),
		MaxOutputTokens: rerunUserCfg.EffectiveMaxOutputTokens(),
	}, cancelRerun)

	go func() {
		defer pipeR.Close()
		proc.Process(pipeR)
	}()

	awsEnvRerun := map[string]string{
		"AWS_PROFILE":           rerunUserCfg.AWSProfile,
		"AWS_REGION":            rerunUserCfg.AWSRegion,
		"AWS_DEFAULT_REGION":    rerunUserCfg.AWSRegion,
		"AWS_ACCESS_KEY_ID":     "",
		"AWS_SECRET_ACCESS_KEY": "",
		"AWS_SESSION_TOKEN":     "",
	}
	var envVarsRerun []string
	for key, cfgVal := range awsEnvRerun {
		if val := os.Getenv(key); val != "" {
			envVarsRerun = append(envVarsRerun, key+"="+val)
		} else if cfgVal != "" {
			envVarsRerun = append(envVarsRerun, key+"="+cfgVal)
		}
	}
	envVarsRerun = append(envVarsRerun, "PI_CODING_AGENT_SESSION_DIR=/tmp/pi-sessions")
	for k, v := range rerunUserCfg.ExtraEnv {
		envVarsRerun = append(envVarsRerun, k+"="+v)
	}

	runArgs := docker.RunArgs{
		Image:    state.Run.Image,
		Workdir:  "/workspace",
		Mounts:   mounts,
		Cmd:      cmdArgs,
		Env:      envVarsRerun,
		EnvFile:  envFile,
		Networks: []string{"sandbox-internal"},
		Stdout:   pipeW,
		Stderr:   pipeW,
	}

	containerID, exitCode, err := docker.Run(rerunCtx, runArgs)
	pipeW.Close()

	if containerID != "" {
		state.Run.ContainerID = containerID
	}

	stats := proc.Stats()
	state.Run.InputTokens += stats.InputTokens
	state.Run.OutputTokens += stats.OutputTokens
	state.Run.CacheReadTokens += stats.CacheReadTokens
	state.Run.CacheWriteTokens += stats.CacheWriteTokens
	state.Run.CostUSD += stats.CostUSD
	config.Save(repoRoot, state)

	emitter.Close()

	if err != nil {
		return exitCode, fmt.Errorf("docker run: %w", err)
	}

	state.Run.Status = config.StatusRunning
	config.Save(repoRoot, state)

	return exitCode, nil
}

// cmdStatus prints the status of a session.
func cmdStatus(name string) error {
	repoRoot, err := resolveRepoRoot()
	if err != nil {
		return err
	}

	sessionName, err := resolveSession(repoRoot, name)
	if err != nil {
		return err
	}

	state, err := config.Load(repoRoot, sessionName)
	if err != nil {
		return err
	}

	fmt.Printf("session: %s\n", state.Name)
	fmt.Printf("branch:  %s (%s)\n", state.BaseBranch, state.BaseCommit[:8])
	fmt.Printf("prompt:  %s\n\n", truncate(state.Prompt, 72))

	r := &state.Run
	status := string(r.Status)
	if r.CommitHash != "" {
		fmt.Printf("status:  %-10s  %s\n", status, r.CommitHash[:8])
	} else {
		fmt.Printf("status:  %s\n", status)
	}

	if r.InputTokens > 0 || r.OutputTokens > 0 {
		fmt.Printf("tokens:  input=%d, output=%d, cache_read=%d, cache_write=%d\n",
			r.InputTokens, r.OutputTokens, r.CacheReadTokens, r.CacheWriteTokens)
	}

	if r.CostUSD > 0 {
		fmt.Printf("cost:    $%.4f\n", r.CostUSD)
	}

	return nil
}

// cmdLogs prints the log file for a session.
func cmdLogs(name string) error {
	repoRoot, err := resolveRepoRoot()
	if err != nil {
		return err
	}

	sessionName, err := resolveSession(repoRoot, name)
	if err != nil {
		return err
	}

	state, err := config.Load(repoRoot, sessionName)
	if err != nil {
		return err
	}

	logPath := state.Run.LogFile
	f, err := os.Open(logPath)
	if err != nil {
		return fmt.Errorf("open log: %w", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		raw := scanner.Text()
		var event struct {
			Message string `json:"message"`
		}
		if err := json.Unmarshal([]byte(raw), &event); err == nil && event.Message != "" {
			fmt.Println(event.Message)
		} else {
			fmt.Println(raw)
		}
	}

	return nil
}

// cmdReview prints the changed files for a session.
func cmdReview(name string, diffFlag bool) error {
	repoRoot, err := resolveRepoRoot()
	if err != nil {
		return err
	}

	sessionName, err := resolveSession(repoRoot, name)
	if err != nil {
		return err
	}

	state, err := config.Load(repoRoot, sessionName)
	if err != nil {
		return err
	}

	r := &state.Run
	if r.CommitHash != "" {
		fmt.Printf("commit  %s  %s\n", r.CommitHash[:8], r.Branch)
	} else {
		fmt.Printf("status  %s  %s\n", r.Status, r.Branch)
	}

	var files []string
	var filesErr error
	if r.CommitHash != "" {
		files, filesErr = git.CommittedChangedFiles(r.Worktree, state.BaseCommit)
	} else {
		files, filesErr = git.ChangedFiles(r.Worktree, state.BaseCommit)
	}

	if filesErr != nil {
		fmt.Printf("(error reading changes: %s)\n\n", filesErr)
		return nil
	}

	if len(files) == 0 {
		fmt.Printf("(no changes)\n\n")
		return nil
	}

	for _, f := range files {
		fmt.Printf("  %s\n", f)
	}

	if diffFlag {
		fmt.Println()
		var diffErr error
		if r.CommitHash != "" {
			diffErr = git.ShowCommittedDiff(r.Worktree, state.BaseCommit)
		} else {
			diffErr = git.ShowDiff(r.Worktree, state.BaseCommit)
		}
		if diffErr != nil {
			fmt.Printf("(error reading diff: %s)\n", diffErr)
		}
	}
	fmt.Println()

	return nil
}

// cmdClean removes a session's worktree, branch, and state.
func cmdCleanAll(repoRoot string) error {
	sessions, err := config.ListSessions(repoRoot)
	if err != nil {
		return err
	}

	if len(sessions) == 0 {
		fmt.Println("no sessions to clean")
		return nil
	}

	var errs []error
	for _, s := range sessions {
		if err := cmdClean(repoRoot, s); err != nil {
			errs = append(errs, fmt.Errorf("session %q: %w", s, err))
		}
	}

	return errors.Join(errs...)
}

func cmdClean(repoRoot, name string) error {
	sessionName, err := resolveSession(repoRoot, name)
	if err != nil {
		return err
	}

	state, err := config.Load(repoRoot, sessionName)
	if err != nil {
		return err
	}

	// Stop running container if any
	if state.Run.ContainerID != "" {
		if err := docker.Stop(state.Run.ContainerID); err != nil {
			fmt.Fprintf(os.Stderr, "warning: stop container: %v\n", err)
		}
	}

	// Remove worktree by removing the entire session directory
	wtPath := state.Run.Worktree
	// Extract parent dirs to know where to remove
	// wtPath is .cerberus/sessions/<name>/worktrees/solve
	// We want to remove the worktree using git
	branchName := state.Run.Branch

	// Get path to the worktree for git worktree remove
	// We'll build it directly: .cerberus/sessions/<name>/worktrees/solve
	if err := removeWorktreeViaGit(repoRoot, wtPath, branchName); err != nil {
		fmt.Fprintf(os.Stderr, "warning: remove worktree: %v\n", err)
	}

	// Remove session directory
	if err := config.Remove(repoRoot, sessionName); err != nil {
		fmt.Fprintf(os.Stderr, "warning: remove session dir: %v\n", err)
	}

	fmt.Printf("cleaned session %q\n", sessionName)
	return nil
}

// removeWorktreeViaGit removes a worktree and deletes its branch.
func removeWorktreeViaGit(repoRoot, wtPath, branchName string) error {
	// Use git worktree remove to clean up properly
	cmd := exec.Command("git", "-C", repoRoot, "worktree", "remove", "--force", wtPath)
	if err := cmd.Run(); err != nil {
		// Ignore error, try to at least delete the branch
	}

	// Delete the branch
	cmd = exec.Command("git", "-C", repoRoot, "branch", "-D", branchName)
	cmd.Run() // Ignore error

	return nil
}

// cmdStats reads and prints statistics.
func cmdStats() error {
	records, err := config.LoadStats()
	if err != nil {
		return err
	}

	if len(records) == 0 {
		fmt.Println("no stats recorded yet")
		return nil
	}

	// Print flat per-session table with new columns
	fmt.Printf("%-20s  %-16s  %-10s  %-11s  %-20s  %-20s  %-15s  %8s  %10s\n",
		"Session", "Date", "Invoker", "Mode", "Status", "WorkDir", "Model", "Duration", "Cost")
	fmt.Println(strings.Repeat("-", 147))

	// Reverse records to show most recent first, cap at 20
	var displayRecords []config.StatsRecord
	for i := len(records) - 1; i >= 0 && len(displayRecords) < 20; i-- {
		displayRecords = append(displayRecords, records[i])
	}

	for _, rec := range displayRecords {
		sessionDate := rec.SessionDate.Format("2006-01-02 15:04")
		sessionName := truncate(rec.SessionName, 20)
		invoker := rec.InvokedBy
		if invoker == "" {
			invoker = "cli"
		}
		invoker = truncate(invoker, 10)

		mode := "oneshot"
		if rec.Interactive {
			mode = "interactive"
		}

		status := rec.Status
		if rec.FailReason != "" {
			status = "failed: " + truncate(rec.FailReason, 11)
		}
		status = truncate(status, 20)

		workDir := rec.WorkDir
		if workDir == "" {
			workDir = "-"
		}
		workDir = truncate(workDir, 20)

		modelAgent := rec.Model
		if modelAgent == "" {
			modelAgent = "(default)"
		}
		modelAgent = truncate(modelAgent, 15)

		durationStr := "-"
		if rec.DurationS > 0 {
			durationStr = fmt.Sprintf("%.0fs", rec.DurationS)
		}

		costStr := "-"
		if rec.CostUSD > 0 {
			costStr = fmt.Sprintf("$%.4f", rec.CostUSD)
		}

		fmt.Printf("%-20s  %-16s  %-10s  %-11s  %-20s  %-20s  %-15s  %8s  %10s\n",
			sessionName, sessionDate, invoker, mode, status, workDir, modelAgent, durationStr, costStr)
	}

	fmt.Printf("\n%d session(s) recorded\n", len(records))
	return nil
}

// appendStats appends a StatsRecord to the global stats file.
func appendStats(state *config.State) error {
	r := &state.Run
	duration := 0.0
	if !r.StartedAt.IsZero() && !r.FinishedAt.IsZero() {
		duration = r.FinishedAt.Sub(r.StartedAt).Seconds()
	}

	// Determine working directory basename
	workDirBasename := r.WorkDir
	if workDirBasename != "" {
		workDirBasename = filepath.Base(workDirBasename)
	}

	rec := config.StatsRecord{
		SessionDate:      time.Now(),
		SessionName:      state.Name,
		PromptSnippet:    truncate(state.Prompt, 80),
		BaseBranch:       state.BaseBranch,
		Model:            r.Model,
		Image:            r.Image,
		Status:           string(r.Status),
		FailReason:       r.FailReason,
		DurationS:        duration,
		InputTokens:      r.InputTokens,
		OutputTokens:     r.OutputTokens,
		CacheReadTokens:  r.CacheReadTokens,
		CacheWriteTokens: r.CacheWriteTokens,
		CostUSD:          r.CostUSD,
		WorkDir:          workDirBasename,
		InvokedBy:        r.InvokedBy,
		Interactive:      r.Interactive,
	}

	return config.AppendStats(rec)
}

// printJSONSummary prints a machine-readable summary line.
func printJSONSummary(state *config.State) {
	r := &state.Run
	summary := map[string]interface{}{
		"session":  state.Name,
		"status":   r.Status,
		"branch":   r.Branch,
		"commit":   r.CommitHash,
		"cost_usd": r.CostUSD,
	}
	data, _ := json.Marshal(summary)
	fmt.Println(string(data))
}

// buildEmitter constructs the appropriate event emitter based on --output and --callback flags.
func buildEmitter(session, output, callback string) event.Emitter {
	var emitters []event.Emitter

	switch output {
	case "jsonl":
		emitters = append(emitters, event.NewJSONLEmitter(os.Stdout))
	default:
		emitters = append(emitters, event.NewTextEmitter(session))
	}

	if callback != "" {
		emitters = append(emitters, event.NewCallbackEmitter(callback))
	}

	if len(emitters) == 1 {
		return emitters[0]
	}
	return event.NewMultiEmitter(emitters...)
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}

// buildInteractiveEnvVars returns the env vars to pass to docker exec for interactive turns.
func buildInteractiveEnvVars(userCfg config.UserConfig) []string {
	awsEnv := map[string]string{
		"AWS_PROFILE":           userCfg.AWSProfile,
		"AWS_REGION":            userCfg.AWSRegion,
		"AWS_DEFAULT_REGION":    userCfg.AWSRegion,
		"AWS_ACCESS_KEY_ID":     "",
		"AWS_SECRET_ACCESS_KEY": "",
		"AWS_SESSION_TOKEN":     "",
	}
	var envVars []string
	for key, cfgVal := range awsEnv {
		if val := os.Getenv(key); val != "" {
			envVars = append(envVars, key+"="+val)
		} else if cfgVal != "" {
			envVars = append(envVars, key+"="+cfgVal)
		}
	}
	// The pi session dir is mounted at /tmp/pi-sessions in the container via the volume mount.
	envVars = append(envVars, "PI_CODING_AGENT_SESSION_DIR=/tmp/pi-sessions")
	for k, v := range userCfg.ExtraEnv {
		envVars = append(envVars, k+"="+v)
	}
	return envVars
}

// runTurnViaExec runs a single agent turn inside an already-running container via docker exec.
// It streams output through the emitter, accumulates token usage into state, and saves state.
func runTurnViaExec(repoRoot string, state *config.State, prompt string, userCfg config.UserConfig, emitter event.Emitter) (int, error) {
	agentImpl, err := agent.Get(state.Run.Agent)
	if err != nil {
		return 1, err
	}

	cmdArgs, err := agentImpl.Args(agent.RunArgs{
		Prompt:          prompt,
		Model:           state.Run.Model,
		Interactive:     true,
		ContinueSession: state.Run.SessionID != "",
	})
	if err != nil {
		return 1, fmt.Errorf("build command: %w", err)
	}

	logFile, err := os.OpenFile(state.Run.LogFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return 1, fmt.Errorf("open log file: %w", err)
	}
	defer logFile.Close()

	fmt.Fprintf(logFile, "\n--- turn: %s ---\n", time.Now().Format(time.RFC3339))

	envVars := buildInteractiveEnvVars(userCfg)

	pipeR, pipeW := io.Pipe()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	proc := stream.NewProcessor(state.Name, emitter, logFile, stream.Limits{
		MaxTurns:        userCfg.EffectiveMaxTurns(),
		MaxOutputTokens: userCfg.EffectiveMaxOutputTokens(),
	}, cancel)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer pipeR.Close()
		proc.Process(pipeR)
	}()

	exitCode, err := docker.Exec(ctx, state.Run.ContainerID, cmdArgs, envVars, pipeW, pipeW)
	pipeW.Close()
	wg.Wait()

	stats := proc.Stats()
	if stats.SessionID != "" && state.Run.SessionID == "" {
		state.Run.SessionID = stats.SessionID
	}
	state.Run.InputTokens += stats.InputTokens
	state.Run.OutputTokens += stats.OutputTokens
	state.Run.CacheReadTokens += stats.CacheReadTokens
	state.Run.CacheWriteTokens += stats.CacheWriteTokens
	state.Run.CostUSD += stats.CostUSD
	config.Save(repoRoot, state)

	if err != nil {
		return exitCode, fmt.Errorf("docker exec: %w", err)
	}

	return exitCode, nil
}

func cmdChatCommand() *cobra.Command {
	var name, prompt, promptFile, agentFlag, modelFlag, imageFlag, profileFile string
	var outputFlag, callbackFlag string

	cmd := &cobra.Command{
		Use:   "chat",
		Short: "Start an interactive session with a long-lived container",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmdChat(name, prompt, promptFile, agentFlag, modelFlag, imageFlag, profileFile, outputFlag, callbackFlag)
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "session name (auto-generated if empty)")
	cmd.Flags().StringVar(&prompt, "prompt", "", "initial prompt to send to the agent (required)")
	cmd.Flags().StringVar(&promptFile, "prompt-file", "", "read prompt from file instead of --prompt")
	cmd.Flags().StringVar(&agentFlag, "agent", "pi", "agent to use (default: pi)")
	cmd.Flags().StringVar(&modelFlag, "model", "", "model to use (default from config or agent)")
	cmd.Flags().StringVar(&imageFlag, "image", "", "docker image (default from config)")
	cmd.Flags().StringVar(&profileFile, "profile-file", "", "path to a profile JSON file to override model, image, and env vars")
	cmd.Flags().StringVar(&outputFlag, "output", "text", "output format: text (default) or jsonl")
	cmd.Flags().StringVar(&callbackFlag, "callback", "", "URL to POST events to as they happen")

	return cmd
}

func cmdMessageCommand() *cobra.Command {
	var name, message string
	var outputFlag, callbackFlag string

	cmd := &cobra.Command{
		Use:   "message",
		Short: "Send a follow-up message to a waiting interactive session",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmdMessage(name, message, outputFlag, callbackFlag)
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "session name (required if multiple sessions exist)")
	cmd.Flags().StringVar(&message, "message", "", "message to send to the agent (required)")
	cmd.Flags().StringVar(&outputFlag, "output", "text", "output format: text (default) or jsonl")
	cmd.Flags().StringVar(&callbackFlag, "callback", "", "URL to POST events to as they happen")

	cmd.RegisterFlagCompletionFunc("name", completionSessions)

	return cmd
}

func cmdCloseCommand() *cobra.Command {
	var name string

	cmd := &cobra.Command{
		Use:   "close",
		Short: "Commit changes and clean up an interactive session",
		RunE: func(cmd *cobra.Command, args []string) error {
			repoRoot, err := resolveRepoRoot()
			if err != nil {
				return err
			}
			return cmdClose(repoRoot, name)
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "session name (required if multiple sessions exist)")

	cmd.RegisterFlagCompletionFunc("name", completionSessions)

	return cmd
}

// cmdChat creates a worktree and a long-lived container, then runs the first agent turn.
func cmdChat(sessionName, prompt, promptFile, agentFlag, modelFlag, imageFlag, profileFile, output, callback string) error {
	userCfg, err := config.LoadUserConfig()
	if err != nil {
		return err
	}

	if profileFile != "" {
		p, err := config.LoadProfileFile(profileFile)
		if err != nil {
			return err
		}
		config.ApplyProfile(&userCfg, p)
	}

	if sessionName == "" {
		sessionName = generateSessionName()
	}

	resolvedPrompt := strings.TrimSpace(prompt)
	if promptFile != "" {
		data, err := os.ReadFile(promptFile)
		if err != nil {
			return fmt.Errorf("read prompt file: %w", err)
		}
		resolvedPrompt = strings.TrimSpace(string(data))
	}
	if resolvedPrompt == "" {
		return fmt.Errorf("--prompt or --prompt-file is required")
	}

	if userCfg.Instructions != "" {
		resolvedPrompt = userCfg.Instructions + "\n\n" + resolvedPrompt
	}

	if _, err := agent.Get(agentFlag); err != nil {
		return err
	}

	model := modelFlag
	if model == "" {
		model = userCfg.DefaultModel
	}

	image := imageFlag
	if image == "" {
		image = userCfg.DefaultImage
	}
	if image == "" {
		return fmt.Errorf("no docker image configured; use --image or set default_image in ~/.config/cerberus/config.json")
	}

	repoRoot, err := resolveRepoRoot()
	if err != nil {
		return err
	}

	if _, err := config.Load(repoRoot, sessionName); err == nil {
		return fmt.Errorf("session %q already exists; run 'cerberus clean --name %s' first", sessionName, sessionName)
	}

	baseBranch, err := git.CurrentBranch(repoRoot)
	if err != nil {
		return err
	}
	baseCommit, err := git.CurrentCommit(repoRoot)
	if err != nil {
		return err
	}

	repoStateDir, err := config.RepoStateDir(repoRoot)
	if err != nil {
		return fmt.Errorf("get repo state dir: %w", err)
	}
	wtPath := filepath.Join(repoStateDir, "sessions", sessionName, "worktrees", "solve")
	branchName := "cerberus/" + sessionName

	if _, err := createWorktreePath(repoRoot, wtPath, branchName, baseCommit); err != nil {
		return fmt.Errorf("create worktree: %w", err)
	}

	logPath, err := config.LogPath(repoRoot, sessionName)
	if err != nil {
		return fmt.Errorf("get log path: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		return fmt.Errorf("create log dir: %w", err)
	}

	// Create the pi session dir on the host so the mount source exists.
	piSessDir, err := config.PiSessionDir(repoRoot, sessionName)
	if err != nil {
		return fmt.Errorf("get pi session dir: %w", err)
	}
	if err := os.MkdirAll(piSessDir, 0o755); err != nil {
		return fmt.Errorf("create pi session dir: %w", err)
	}

	state := &config.State{
		Name:       sessionName,
		BaseBranch: baseBranch,
		BaseCommit: baseCommit,
		Prompt:     resolvedPrompt,
		Run: config.Run{
			Branch:      branchName,
			Worktree:    wtPath,
			Agent:       agentFlag,
			Model:       model,
			Image:       image,
			ProfileFile: profileFile,
			Interactive: true,
			Status:      config.StatusPending,
			LogFile:     logPath,
			StartedAt:   time.Now(),
		},
	}

	if err := config.Save(repoRoot, state); err != nil {
		return fmt.Errorf("save state: %w", err)
	}
	if err := config.RegisterRepo(repoRoot); err != nil {
		fmt.Fprintf(os.Stderr, "warn: register repo: %v\n", err)
	}

	fmt.Printf("session: %s\n", sessionName)
	fmt.Printf("branch:  %s (%s)\n", baseBranch, baseCommit[:8])
	fmt.Printf("agent:   %s (interactive)\n\n", agentFlag)

	// Build mounts for the long-lived container.
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("get home dir: %w", err)
	}

	mounts := []docker.Mount{
		{Host: wtPath, Container: "/workspace"},
		{Host: filepath.Join(homeDir, ".pi", "agent"), Container: "/home/agent/.pi/agent"},
		{Host: piSessDir, Container: "/tmp/pi-sessions"},
	}

	gradleInitD := filepath.Join(homeDir, ".gradle", "init.d")
	if _, err := os.Stat(gradleInitD); err == nil {
		mounts = append(mounts, docker.Mount{Host: gradleInitD, Container: "/home/agent/.gradle/init.d", ReadOnly: true})
	}

	awsDir := filepath.Join(homeDir, ".aws")
	if _, err := os.Stat(awsDir); err == nil {
		mounts = append(mounts, docker.Mount{Host: awsDir, Container: "/home/agent/.aws", ReadOnly: true})
	}

	envVars := buildInteractiveEnvVars(userCfg)

	// Ensure Copilot token is refreshed before launching container
	if err := ensureCopilotToken(); err != nil {
		return fmt.Errorf("copilot token refresh: %w", err)
	}

	// Require proxy network — fail early if proxy stack isn't running.
	if err := requireProxyNetwork(); err != nil {
		return err
	}

	containerID, err := docker.Start(context.Background(), docker.StartArgs{
		Image:    image,
		Workdir:  "/workspace",
		Mounts:   mounts,
		Env:      envVars,
		EnvFile:  agentEnvFilePath(),
		Networks: []string{"sandbox-internal"},
	})
	if err != nil {
		return fmt.Errorf("start container: %w", err)
	}

	state.Run.ContainerID = containerID
	state.Run.Status = config.StatusRunning
	if err := config.Save(repoRoot, state); err != nil {
		return fmt.Errorf("save state: %w", err)
	}

	emitter := buildEmitter(sessionName, output, callback)
	defer emitter.Close()

	exitCode, err := runTurnViaExec(repoRoot, state, resolvedPrompt, userCfg, emitter)
	if err != nil {
		state.Run.Status = config.StatusFailed
		config.Save(repoRoot, state)
		return err
	}

	if exitCode != 0 {
		state.Run.Status = config.StatusFailed
		state.Run.ExitCode = exitCode
		config.Save(repoRoot, state)
		return fmt.Errorf("agent exited with code %d", exitCode)
	}

	state.Run.Status = config.StatusWaiting
	if err := config.Save(repoRoot, state); err != nil {
		return fmt.Errorf("save state: %w", err)
	}

	e := event.New(event.TurnComplete, sessionName)
	e.Status = "waiting"
	emitter.Emit(e)

	fmt.Printf("\n[%s] waiting for next message — use 'cerberus message --name %s'\n", sessionName, sessionName)
	return nil
}

// cmdMessage sends a follow-up message to a waiting interactive session.
func cmdMessage(name, message, output, callback string) error {
	if message == "" {
		return fmt.Errorf("--message is required")
	}

	repoRoot, err := resolveRepoRoot()
	if err != nil {
		return err
	}

	sessionName, err := resolveSession(repoRoot, name)
	if err != nil {
		return err
	}

	state, err := config.Load(repoRoot, sessionName)
	if err != nil {
		return err
	}

	if !state.Run.Interactive {
		return fmt.Errorf("session %q is not an interactive session; use 'cerberus rerun' instead", sessionName)
	}

	if state.Run.Status != config.StatusWaiting {
		return fmt.Errorf("session %q is not waiting (status: %s)", sessionName, state.Run.Status)
	}

	userCfg, err := config.LoadUserConfig()
	if err != nil {
		return err
	}

	if state.Run.ProfileFile != "" {
		p, err := config.LoadProfileFile(state.Run.ProfileFile)
		if err != nil {
			return err
		}
		config.ApplyProfile(&userCfg, p)
	}

	emitter := buildEmitter(sessionName, output, callback)
	defer emitter.Close()

	state.Run.Status = config.StatusRunning
	if err := config.Save(repoRoot, state); err != nil {
		return fmt.Errorf("save state: %w", err)
	}

	exitCode, err := runTurnViaExec(repoRoot, state, message, userCfg, emitter)
	if err != nil {
		state.Run.Status = config.StatusFailed
		config.Save(repoRoot, state)
		return err
	}

	if exitCode != 0 {
		state.Run.Status = config.StatusFailed
		state.Run.ExitCode = exitCode
		config.Save(repoRoot, state)
		return fmt.Errorf("agent exited with code %d", exitCode)
	}

	state.Run.Status = config.StatusWaiting
	if err := config.Save(repoRoot, state); err != nil {
		return fmt.Errorf("save state: %w", err)
	}

	e := event.New(event.TurnComplete, sessionName)
	e.Status = "waiting"
	emitter.Emit(e)

	fmt.Printf("\n[%s] waiting — use 'cerberus message' or 'cerberus close' to finish\n", sessionName)
	return nil
}

// cmdClose commits any changes in an interactive session and cleans it up.
func cmdClose(repoRoot, name string) error {
	sessionName, err := resolveSession(repoRoot, name)
	if err != nil {
		return err
	}

	state, err := config.Load(repoRoot, sessionName)
	if err != nil {
		return err
	}

	if !state.Run.Interactive {
		return fmt.Errorf("session %q is not an interactive session; use 'cerberus clean' instead", sessionName)
	}

	if state.Run.Status == config.StatusRunning {
		return fmt.Errorf("session %q is still running; wait for it to finish first", sessionName)
	}

	hasChanges, err := git.HasChanges(state.Run.Worktree)
	if err != nil {
		return fmt.Errorf("check changes: %w", err)
	}

	if hasChanges {
		fmt.Printf("[%s] committing...\n", sessionName)

		diff, _ := git.StageAndDiff(state.Run.Worktree, state.BaseCommit)
		commitMsg := agent.AskForCommitMessage(state.Run.Worktree, diff, state.Run.Model, state.Run.InvokedBy)
		commitHash, err := git.CommitAndGetHash(state.Run.Worktree, commitMsg)
		if err != nil {
			return fmt.Errorf("commit failed: %w", err)
		}

		state.Run.CommitHash = commitHash
		fmt.Printf("[%s] commit %s  %s\n", sessionName, commitHash[:8], commitMsg)
	} else {
		fmt.Printf("[%s] no changes to commit\n", sessionName)
	}

	state.Run.Status = config.StatusDone
	state.Run.FinishedAt = time.Now()
	config.Save(repoRoot, state)

	if err := appendStats(state); err != nil {
		fmt.Fprintf(os.Stderr, "warning: append stats: %v\n", err)
	}

	return cmdClean(repoRoot, sessionName)
}
