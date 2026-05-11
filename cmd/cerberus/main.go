package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/tonis/cerberus/internal/agent"
	"github.com/tonis/cerberus/internal/config"
	"github.com/tonis/cerberus/internal/docker"
	"github.com/tonis/cerberus/internal/git"
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
		cmdStatusCommand(),
		cmdLogsCommand(),
		cmdReviewCommand(),
		cmdCleanCommand(),
		cmdStatsCommand(),
		cmdVersionCommand(),
	)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func cmdStartCommand() *cobra.Command {
	var name, prompt, promptFile, agentFlag, modelFlag, imageFlag string

	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start a single agent run in an isolated worktree",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmdStart(name, prompt, promptFile, agentFlag, modelFlag, imageFlag)
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "session name (auto-generated if empty)")
	cmd.Flags().StringVar(&prompt, "prompt", "", "prompt to send to agent (required)")
	cmd.Flags().StringVar(&promptFile, "prompt-file", "", "read prompt from file instead of -prompt")
	cmd.Flags().StringVar(&agentFlag, "agent", "opencode", "agent to use (default: opencode)")
	cmd.Flags().StringVar(&modelFlag, "model", "", "model to use (default from config or agent)")
	cmd.Flags().StringVar(&imageFlag, "image", "", "docker image (default from config)")

	cmd.RegisterFlagCompletionFunc("name", completionSessions)

	return cmd
}

func cmdRerunCommand() *cobra.Command {
	var name, prompt, promptFile string

	cmd := &cobra.Command{
		Use:   "rerun",
		Short: "Run the agent again in an existing session worktree",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmdRerun(name, prompt, promptFile)
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "session name (required if multiple sessions exist)")
	cmd.Flags().StringVar(&prompt, "prompt", "", "follow-up prompt for agent (required)")
	cmd.Flags().StringVar(&promptFile, "prompt-file", "", "read prompt from file instead of -prompt")

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

	cmd := &cobra.Command{
		Use:   "clean",
		Short: "Remove a session's worktree, branch, and state",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmdClean(name)
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "session name (required if multiple sessions exist)")

	cmd.RegisterFlagCompletionFunc("name", completionSessions)

	return cmd
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
	return adjectives[len(adjectives)%len(os.Args)] + "-" + nouns[(len(adjectives)+len(os.Args))%len(nouns)]
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
func cmdStart(sessionName, prompt, promptFile, agentFlag, modelFlag, imageFlag string) error {
	userCfg, err := config.LoadUserConfig()
	if err != nil {
		return err
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
	wtPath := filepath.Join(repoRoot, ".cerberus", "sessions", sessionName, "worktrees", "solve")
	branchName := "cerberus/" + sessionName

	if _, err := createWorktreePath(repoRoot, wtPath, branchName, baseCommit); err != nil {
		return fmt.Errorf("create worktree: %w", err)
	}

	// Initialize state.json
	logPath := config.LogPath(repoRoot, sessionName)
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		return fmt.Errorf("create log dir: %w", err)
	}

	state := &config.State{
		Name:       sessionName,
		BaseBranch: baseBranch,
		BaseCommit: baseCommit,
		Prompt:     resolvedPrompt,
		Run: config.Run{
			Branch:    branchName,
			Worktree:  wtPath,
			Agent:     agentFlag,
			Model:     model,
			OcAgent:   "", // Not set for start
			Image:     image,
			Status:    config.StatusPending,
			LogFile:   logPath,
			StartedAt: time.Now(),
		},
	}

	if err := config.Save(repoRoot, state); err != nil {
		return fmt.Errorf("save state: %w", err)
	}

	fmt.Printf("session: %s\n", sessionName)
	fmt.Printf("branch:  %s (%s)\n", baseBranch, baseCommit[:8])
	fmt.Printf("agent:   %s\n\n", agentFlag)

	// Run agent in docker
	exitCode, err := runAgentInDocker(repoRoot, state, resolvedPrompt, agentImpl, model)
	if err != nil {
		return err
	}

	if exitCode != 0 {
		state.Run.Status = config.StatusFailed
		state.Run.ExitCode = exitCode
		state.Run.FinishedAt = time.Now()
		if err := config.Save(repoRoot, state); err != nil {
			fmt.Fprintf(os.Stderr, "save state: %w\n", err)
		}
		if err := appendStats(state); err != nil {
			fmt.Fprintf(os.Stderr, "append stats: %w\n", err)
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
			fmt.Fprintf(os.Stderr, "warning: append stats: %w\n", err)
		}
		fmt.Printf("[%s] no changes to commit\n", sessionName)
		printJSONSummary(state)
		return nil
	}

	// Ask for commit message and commit
	fmt.Printf("[%s] committing...\n", sessionName)

	diff, err := git.Diff(wtPath, baseCommit)
	if err != nil {
		diff = ""
	}

	commitMsg := agent.AskForCommitMessage(wtPath, diff, model)
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
		fmt.Fprintf(os.Stderr, "warning: append stats: %w\n", err)
	}

	fmt.Printf("[%s] commit %s  %s\n", sessionName, commitHash[:8], commitMsg)
	printJSONSummary(state)
	return nil
}

// runAgentInDocker executes the agent in a docker container and streams output.
func runAgentInDocker(repoRoot string, state *config.State, prompt string, agentImpl agent.Agent, model string) (int, error) {
	sessionName := state.Name
	wtPath := state.Run.Worktree
	logPath := state.Run.LogFile

	// Get opencode args
	cmdArgs, err := agentImpl.Args(agent.RunArgs{
		Prompt:  prompt,
		Model:   model,
		OcAgent: state.Run.OcAgent,
	})
	if err != nil {
		return 1, fmt.Errorf("build command: %w", err)
	}

	// Create log file
	logFile, err := os.Create(logPath)
	if err != nil {
		return 1, fmt.Errorf("create log file: %w", err)
	}
	defer logFile.Close()

	// Build docker mounts
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return 1, fmt.Errorf("get home dir: %w", err)
	}

	mounts := []docker.Mount{
		{
			Host:      wtPath,
			Container: "/workspace",
			ReadOnly:  false,
		},
		{
			Host:      filepath.Join(homeDir, ".config", "opencode"),
			Container: "/root/.config/opencode",
			ReadOnly:  true,
		},
	}

	// Add ~/.gradle/init.d if it exists
	gradleInitD := filepath.Join(homeDir, ".gradle", "init.d")
	if _, err := os.Stat(gradleInitD); err == nil {
		mounts = append(mounts, docker.Mount{
			Host:      gradleInitD,
			Container: "/root/.gradle/init.d",
			ReadOnly:  true,
		})
	}

	// Determine networks
	var networks []string
	if networkExists("sandbox-internal") {
		networks = append(networks, "sandbox-internal")
	}

	// Resolve env file if it exists
	envFile := ""
	envFilePath := filepath.Join(repoRoot, "proxy", "agent.env")
	if _, err := os.Stat(envFilePath); err == nil {
		envFile = envFilePath
	}

	// Create output file for JSON lines
	pipeR, pipeW := io.Pipe()

	// Stream handler
	var accumulatedTokens, accumulatedCost struct {
		input      int
		output     int
		cacheRead  int
		cacheWrite int
		costUSD    float64
	}

	go func() {
		defer pipeR.Close()
		scanner := bufio.NewScanner(pipeR)
		scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
		for scanner.Scan() {
			line := scanner.Text()
			fmt.Fprintln(logFile, line)

			// Parse JSON event
			var event struct {
				SessionID string `json:"sessionID"`
				Type      string `json:"type"`
				Part      struct {
					Text   string `json:"text"`
					Tokens struct {
						Input  int `json:"input"`
						Output int `json:"output"`
						Cache  struct {
							Read  int `json:"read"`
							Write int `json:"write"`
						} `json:"cache"`
					} `json:"tokens"`
					Cost float64 `json:"cost"`
				} `json:"part"`
				Message string `json:"message"`
			}

			if err := json.Unmarshal([]byte(line), &event); err == nil {
				// Extract sessionID on first event
				if event.SessionID != "" && state.Run.SessionID == "" {
					state.Run.SessionID = event.SessionID
					config.Save(repoRoot, state)
				}

				// Accumulate tokens
				if event.Type == "step_finish" {
					accumulatedTokens.input += event.Part.Tokens.Input
					accumulatedTokens.output += event.Part.Tokens.Output
					accumulatedTokens.cacheRead += event.Part.Tokens.Cache.Read
					accumulatedTokens.cacheWrite += event.Part.Tokens.Cache.Write
					accumulatedCost.costUSD += event.Part.Cost
				}

				// Print text to stdout
				if event.Type == "text" && event.Part.Text != "" {
					fmt.Printf("[%s] %s", sessionName, event.Part.Text)
				} else if event.Message != "" {
					fmt.Printf("[%s] %s\n", sessionName, event.Message)
				}
			} else {
				// Not JSON, print as-is
				fmt.Printf("[%s] %s\n", sessionName, line)
			}
		}
	}()

	// Run docker
	runArgs := docker.RunArgs{
		Image:    state.Run.Image,
		Workdir:  "/workspace",
		Mounts:   mounts,
		Cmd:      cmdArgs,
		EnvFile:  envFile,
		Networks: networks,
		Stdout:   pipeW,
		Stderr:   pipeW,
	}

	ctx := context.Background()
	containerID, exitCode, err := docker.Run(ctx, runArgs)
	pipeW.Close()

	if containerID != "" {
		state.Run.ContainerID = containerID
	}

	// Update state with accumulated tokens
	state.Run.InputTokens = accumulatedTokens.input
	state.Run.OutputTokens = accumulatedTokens.output
	state.Run.CacheReadTokens = accumulatedTokens.cacheRead
	state.Run.CacheWriteTokens = accumulatedTokens.cacheWrite
	state.Run.CostUSD = accumulatedCost.costUSD
	config.Save(repoRoot, state)

	if err != nil {
		return exitCode, fmt.Errorf("docker run: %w", err)
	}

	state.Run.Status = config.StatusRunning
	config.Save(repoRoot, state)

	return exitCode, nil
}

// networkExists checks if a docker network exists.
func networkExists(networkName string) bool {
	// Try docker network inspect
	cmd := exec.Command("docker", "network", "inspect", networkName)
	return cmd.Run() == nil
}

// cmdRerun runs the agent again in an existing worktree with a new prompt.
func cmdRerun(name, prompt, promptFile string) error {
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

	// Get agent
	agentImpl, err := agent.Get(state.Run.Agent)
	if err != nil {
		return err
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
	exitCode, err := runAgentInDockerRerun(repoRoot, state, resolvedPrompt, agentImpl, logFile)
	if err != nil {
		return err
	}

	if exitCode != 0 {
		state.Run.Status = config.StatusFailed
		state.Run.ExitCode = exitCode
		state.Run.FinishedAt = time.Now()
		config.Save(repoRoot, state)
		if err := appendStats(state); err != nil {
			fmt.Fprintf(os.Stderr, "warning: append stats: %w\n", err)
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
			fmt.Fprintf(os.Stderr, "warning: append stats: %w\n", err)
		}
		fmt.Printf("[%s] no changes to commit\n", sessionName)
		return nil
	}

	// Commit changes
	fmt.Printf("[%s] committing...\n", sessionName)

	diff, _ := git.Diff(state.Run.Worktree, state.BaseCommit)
	commitMsg := agent.AskForCommitMessage(state.Run.Worktree, diff, state.Run.Model)
	commitHash, err := git.CommitAndGetHash(state.Run.Worktree, commitMsg)
	if err != nil {
		return fmt.Errorf("commit failed: %w", err)
	}

	state.Run.CommitHash = commitHash
	state.Run.Status = config.StatusDone
	state.Run.FinishedAt = time.Now()
	config.Save(repoRoot, state)
	if err := appendStats(state); err != nil {
		fmt.Fprintf(os.Stderr, "warning: append stats: %w\n", err)
	}

	fmt.Printf("[%s] commit %s  %s\n", sessionName, commitHash[:8], commitMsg)
	return nil
}

// runAgentInDockerRerun is similar to runAgentInDocker but appends to an existing log file.
func runAgentInDockerRerun(repoRoot string, state *config.State, prompt string, agentImpl agent.Agent, logFile *os.File) (int, error) {
	sessionName := state.Name
	wtPath := state.Run.Worktree

	// Get opencode args
	cmdArgs, err := agentImpl.Args(agent.RunArgs{
		Prompt:  prompt,
		Model:   state.Run.Model,
		OcAgent: state.Run.OcAgent,
	})
	if err != nil {
		return 1, fmt.Errorf("build command: %w", err)
	}

	// Build docker mounts
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return 1, fmt.Errorf("get home dir: %w", err)
	}

	mounts := []docker.Mount{
		{
			Host:      wtPath,
			Container: "/workspace",
			ReadOnly:  false,
		},
		{
			Host:      filepath.Join(homeDir, ".config", "opencode"),
			Container: "/root/.config/opencode",
			ReadOnly:  true,
		},
	}

	// Add ~/.gradle/init.d if it exists
	gradleInitD := filepath.Join(homeDir, ".gradle", "init.d")
	if _, err := os.Stat(gradleInitD); err == nil {
		mounts = append(mounts, docker.Mount{
			Host:      gradleInitD,
			Container: "/root/.gradle/init.d",
			ReadOnly:  true,
		})
	}

	// Determine networks
	var networks []string
	if networkExists("sandbox-internal") {
		networks = append(networks, "sandbox-internal")
	}

	// Resolve env file
	envFile := ""
	envFilePath := filepath.Join(repoRoot, "proxy", "agent.env")
	if _, err := os.Stat(envFilePath); err == nil {
		envFile = envFilePath
	}

	// Create output pipe
	pipeR, pipeW := io.Pipe()

	// Stream handler
	var accumulatedTokens struct {
		input      int
		output     int
		cacheRead  int
		cacheWrite int
		costUSD    float64
	}

	go func() {
		defer pipeR.Close()
		scanner := bufio.NewScanner(pipeR)
		scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
		for scanner.Scan() {
			line := scanner.Text()
			fmt.Fprintln(logFile, line)

			var event struct {
				Type string `json:"type"`
				Part struct {
					Text   string `json:"text"`
					Tokens struct {
						Input  int `json:"input"`
						Output int `json:"output"`
						Cache  struct {
							Read  int `json:"read"`
							Write int `json:"write"`
						} `json:"cache"`
					} `json:"tokens"`
					Cost float64 `json:"cost"`
				} `json:"part"`
				Message string `json:"message"`
			}

			if err := json.Unmarshal([]byte(line), &event); err == nil {
				if event.Type == "step_finish" {
					accumulatedTokens.input += event.Part.Tokens.Input
					accumulatedTokens.output += event.Part.Tokens.Output
					accumulatedTokens.cacheRead += event.Part.Tokens.Cache.Read
					accumulatedTokens.cacheWrite += event.Part.Tokens.Cache.Write
					accumulatedTokens.costUSD += event.Part.Cost
				}

				if event.Type == "text" && event.Part.Text != "" {
					fmt.Printf("[%s] %s", sessionName, event.Part.Text)
				} else if event.Message != "" {
					fmt.Printf("[%s] %s\n", sessionName, event.Message)
				}
			} else {
				fmt.Printf("[%s] %s\n", sessionName, line)
			}
		}
	}()

	// Run docker
	runArgs := docker.RunArgs{
		Image:    state.Run.Image,
		Workdir:  "/workspace",
		Mounts:   mounts,
		Cmd:      cmdArgs,
		EnvFile:  envFile,
		Networks: networks,
		Stdout:   pipeW,
		Stderr:   pipeW,
	}

	ctx := context.Background()
	containerID, exitCode, err := docker.Run(ctx, runArgs)
	pipeW.Close()

	if containerID != "" {
		state.Run.ContainerID = containerID
	}

	// Update state with accumulated tokens
	state.Run.InputTokens += accumulatedTokens.input
	state.Run.OutputTokens += accumulatedTokens.output
	state.Run.CacheReadTokens += accumulatedTokens.cacheRead
	state.Run.CacheWriteTokens += accumulatedTokens.cacheWrite
	state.Run.CostUSD += accumulatedTokens.costUSD
	config.Save(repoRoot, state)

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
func cmdClean(name string) error {
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

	// Stop running container if any
	if state.Run.ContainerID != "" {
		if err := docker.Stop(state.Run.ContainerID); err != nil {
			fmt.Fprintf(os.Stderr, "warning: stop container: %w\n", err)
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
		fmt.Fprintf(os.Stderr, "warning: remove worktree: %w\n", err)
	}

	// Remove session directory
	if err := config.Remove(repoRoot, sessionName); err != nil {
		fmt.Fprintf(os.Stderr, "warning: remove session dir: %w\n", err)
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

	// Print flat per-session table
	fmt.Printf("%-20s  %-16s  %-15s  %8s  %10s  %8s  %8s  %10s\n",
		"Session", "Date", "Model/Agent", "Status", "Duration", "Input", "Output", "Cost")
	fmt.Println(strings.Repeat("-", 100))

	// Reverse records to show most recent first, cap at 20
	var displayRecords []config.StatsRecord
	for i := len(records) - 1; i >= 0 && len(displayRecords) < 20; i-- {
		displayRecords = append(displayRecords, records[i])
	}

	for _, rec := range displayRecords {
		sessionDate := rec.SessionDate.Format("2006-01-02 15:04")
		sessionName := truncate(rec.SessionName, 20)
		modelAgent := rec.Model
		if modelAgent == "" {
			modelAgent = "(default)"
		}
		if rec.OcAgent != "" {
			modelAgent = modelAgent + "/" + rec.OcAgent
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

		fmt.Printf("%-20s  %-16s  %-15s  %8s  %10s  %8d  %8d  %10s\n",
			sessionName, sessionDate, modelAgent, rec.Status, durationStr, rec.InputTokens, rec.OutputTokens, costStr)
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

	rec := config.StatsRecord{
		SessionDate:      time.Now(),
		SessionName:      state.Name,
		PromptSnippet:    truncate(state.Prompt, 80),
		BaseBranch:       state.BaseBranch,
		Model:            r.Model,
		OcAgent:          r.OcAgent,
		Image:            r.Image,
		Status:           string(r.Status),
		DurationS:        duration,
		InputTokens:      r.InputTokens,
		OutputTokens:     r.OutputTokens,
		CacheReadTokens:  r.CacheReadTokens,
		CacheWriteTokens: r.CacheWriteTokens,
		CostUSD:          r.CostUSD,
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

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}
