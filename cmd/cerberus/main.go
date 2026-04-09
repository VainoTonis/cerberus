package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/tonis/cerberus/internal/agent"
	"github.com/tonis/cerberus/internal/config"
	"github.com/tonis/cerberus/internal/git"
)

// version is set at build time via -ldflags "-X main.version=..."
var version = "dev"

func main() {
	rootCmd := &cobra.Command{
		Use:   "cerberus",
		Short: "multi-agent parallel problem solver",
		Long:  "cerberus - run multiple AI agents in parallel to solve problems",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	rootCmd.AddCommand(
		cmdStartCommand(),
		cmdRerunCommand(),
		cmdListCommand(),
		cmdStatusCommand(),
		cmdReviewCommand(),
		cmdApplyCommand(),
		cmdMergeCommand(),
		cmdMergeApplyCommand(),
		cmdLogsCommand(),
		cmdCleanCommand(),
		cmdStatsCommand(),
		cmdVersionCommand(),
	)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func cmdStartCommand() *cobra.Command {
	var sessionName, prompt, promptFile, agentFlag, modelFlag string
	var nInt int

	cmd := &cobra.Command{
		Use:   "start",
		Short: "Create a session worktree and run agents",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmdStart(sessionName, nInt, prompt, promptFile, agentFlag, modelFlag)
		},
	}

	cmd.Flags().StringVar(&sessionName, "session", "", "session name (required; must be unique within the repo)")
	cmd.Flags().IntVar(&nInt, "n", 0, "number of solutions (default: number of runners in config)")
	cmd.Flags().StringVar(&prompt, "prompt", "", "prompt to send to each agent (required)")
	cmd.Flags().StringVar(&promptFile, "prompt-file", "", "read prompt from file instead of -prompt")
	cmd.Flags().StringVar(&agentFlag, "agent", "opencode", "agent to use when not set in config")
	cmd.Flags().StringVar(&modelFlag, "model", "", "model to use when not set in config")

	cmd.RegisterFlagCompletionFunc("session", completionSessions)

	return cmd
}

func cmdRerunCommand() *cobra.Command {
	var sessionFlag string
	var solution int
	var prompt, promptFile string

	cmd := &cobra.Command{
		Use:   "rerun",
		Short: "Run the agent again in an existing solution worktree",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmdRerun(sessionFlag, solution, prompt, promptFile)
		},
	}

	cmd.Flags().StringVar(&sessionFlag, "session", "", "session name (required if multiple sessions are active)")
	cmd.Flags().IntVar(&solution, "solution", 0, "solution index to rerun (required)")
	cmd.Flags().StringVar(&prompt, "prompt", "", "follow-up prompt for the agent (required)")
	cmd.Flags().StringVar(&promptFile, "prompt-file", "", "read prompt from file instead of -prompt")

	cmd.RegisterFlagCompletionFunc("session", completionSessions)

	return cmd
}

func cmdListCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all active sessions in the current repo",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmdList()
		},
	}

	return cmd
}

func cmdStatusCommand() *cobra.Command {
	var sessionFlag string

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show the status of a session's solutions",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmdStatus(sessionFlag)
		},
	}

	cmd.Flags().StringVar(&sessionFlag, "session", "", "session name (required if multiple sessions are active)")

	cmd.RegisterFlagCompletionFunc("session", completionSessions)

	return cmd
}

func cmdReviewCommand() *cobra.Command {
	var sessionFlag string
	var diffFlag bool

	cmd := &cobra.Command{
		Use:   "review",
		Short: "Print a summary of what each solution changed",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmdReview(sessionFlag, diffFlag)
		},
	}

	cmd.Flags().StringVar(&sessionFlag, "session", "", "session name (required if multiple sessions are active)")
	cmd.Flags().BoolVar(&diffFlag, "diff", false, "print full unified diffs (default: file names only)")

	cmd.RegisterFlagCompletionFunc("session", completionSessions)

	return cmd
}

func cmdApplyCommand() *cobra.Command {
	var sessionFlag string
	var solution int

	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Cherry-pick a solution's commits onto the current branch",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmdApply(sessionFlag, solution)
		},
	}

	cmd.Flags().StringVar(&sessionFlag, "session", "", "session name (required if multiple sessions are active)")
	cmd.Flags().IntVar(&solution, "solution", 0, "solution index to apply (required)")

	cmd.RegisterFlagCompletionFunc("session", completionSessions)

	return cmd
}

func cmdMergeCommand() *cobra.Command {
	var sessionFlag, model string

	cmd := &cobra.Command{
		Use:   "merge",
		Short: "Ask an LLM to analyse all solutions and produce a merge suggestion",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmdMerge(sessionFlag, model)
		},
	}

	cmd.Flags().StringVar(&sessionFlag, "session", "", "session name (required if multiple sessions are active)")
	cmd.Flags().StringVar(&model, "model", "", "model to use for the merge (default: opencode's configured default)")

	cmd.RegisterFlagCompletionFunc("session", completionSessions)

	return cmd
}

func cmdMergeApplyCommand() *cobra.Command {
	var sessionFlag string

	cmd := &cobra.Command{
		Use:   "merge-apply",
		Short: "Apply the merge suggestion, commit, and clean up the session",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmdMergeApply(sessionFlag)
		},
	}

	cmd.Flags().StringVar(&sessionFlag, "session", "", "session name (required if multiple sessions are active)")

	cmd.RegisterFlagCompletionFunc("session", completionSessions)

	return cmd
}

func cmdLogsCommand() *cobra.Command {
	var sessionFlag string

	cmd := &cobra.Command{
		Use:   "logs",
		Short: "Tail all solution logs, printing only the message field",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmdLogs(sessionFlag)
		},
	}

	cmd.Flags().StringVar(&sessionFlag, "session", "", "session name (required if multiple sessions are active)")

	cmd.RegisterFlagCompletionFunc("session", completionSessions)

	return cmd
}

func cmdCleanCommand() *cobra.Command {
	var sessionFlag string
	var force bool
	var all bool

	cmd := &cobra.Command{
		Use:   "clean",
		Short: "Remove a session's worktrees, branches, and state",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmdClean(sessionFlag, force, all)
		},
	}

	cmd.Flags().StringVar(&sessionFlag, "session", "", "session name (required if multiple sessions are active)")
	cmd.Flags().BoolVar(&force, "force", false, "skip confirmation prompt")
	cmd.Flags().BoolVar(&all, "all", false, "clean all sessions in the repo")

	cmd.RegisterFlagCompletionFunc("session", completionSessions)

	return cmd
}

func cmdStatsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "stats",
		Short: "Show model performance statistics across all sessions",
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

// resolveRepoRoot finds the git repo root from the current directory.
func resolveRepoRoot() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("get working directory: %w", err)
	}
	return git.RepoRoot(cwd)
}

// resolveSession resolves the session name to use for a command.
// If name is non-empty it is returned as-is.
// If name is empty and exactly one session exists, that session is returned.
// If name is empty and multiple sessions exist, an error listing them is returned.
// If name is empty and no sessions exist, an error is returned.
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
		return "", fmt.Errorf("no cerberus sessions found; run 'cerberus start -session <name>' first")
	case 1:
		return sessions[0], nil
	default:
		return "", fmt.Errorf("multiple sessions active (%s); specify one with -session <name>", strings.Join(sessions, ", "))
	}
}

// cmdStart creates git worktrees and runs N agents in parallel, blocking until
// all finish. After each agent completes successfully, any changes are
// committed inside the worktree and the commit hash is stored in state.
func cmdStart(sessionName string, n int, prompt, promptFile, agentFlag, modelFlag string) error {
	userCfg, err := config.LoadUserConfig()
	if err != nil {
		return err
	}

	if sessionName == "" {
		return fmt.Errorf("-session is required (e.g. -session frontend-refactor)")
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
		return fmt.Errorf("-prompt or -prompt-file is required")
	}

	// Prepend global instructions from config if set.
	if userCfg.Instructions != "" {
		resolvedPrompt = userCfg.Instructions + "\n\n" + resolvedPrompt
	}

	// Build the runner list.
	runners := userCfg.Runners
	if len(runners) == 0 {
		count := n
		if count == 0 {
			count = 2
		}
		for range count {
			runners = append(runners, config.Runner{Agent: agentFlag, Model: modelFlag})
		}
	} else if n > 0 {
		for len(runners) < n {
			runners = append(runners, runners[len(runners)-1])
		}
		runners = runners[:n]
	}

	if len(runners) == 0 {
		return fmt.Errorf("no runners configured; add runners to ~/.config/cerberus/config.json or pass -agent/-model")
	}

	for i, r := range runners {
		if _, err := agent.Get(r.Agent); err != nil {
			return fmt.Errorf("runner %d: %w", i+1, err)
		}
	}

	repoRoot, err := resolveRepoRoot()
	if err != nil {
		return err
	}

	// Refuse to start if this session name is already in use.
	if _, err := config.Load(repoRoot, sessionName); err == nil {
		return fmt.Errorf("session %q already exists; run 'cerberus clean -session %s' first", sessionName, sessionName)
	}

	baseBranch, err := git.CurrentBranch(repoRoot)
	if err != nil {
		return err
	}
	baseCommit, err := git.CurrentCommit(repoRoot)
	if err != nil {
		return err
	}

	fmt.Printf("session: %s\n", sessionName)
	fmt.Printf("branch:  %s (%s)\n", baseBranch, baseCommit[:8])
	fmt.Printf("agents:  %d\n\n", len(runners))

	state := &config.State{
		Name:       sessionName,
		BaseBranch: baseBranch,
		BaseCommit: baseCommit,
		Prompt:     resolvedPrompt,
		Selections: map[string]int{},
	}

	for i, r := range runners {
		idx := i + 1
		wtPath, branch, err := git.CreateWorktree(repoRoot, sessionName, idx, baseCommit)
		if err != nil {
			for j := 1; j < idx; j++ {
				_ = git.RemoveWorktree(repoRoot, sessionName, j)
			}
			return err
		}
		logPath := config.LogPath(repoRoot, sessionName, idx)
		state.Solutions = append(state.Solutions, config.Solution{
			Index:    idx,
			Branch:   branch,
			Worktree: wtPath,
			Agent:    r.Agent,
			Model:    r.Model,
			OcAgent:  r.OcAgent,
			Status:   config.StatusPending,
			LogFile:  logPath,
		})
		fmt.Printf("  solve-%d  %s\n", idx, branch)
	}
	fmt.Println()

	if err := config.Save(repoRoot, state); err != nil {
		return err
	}

	// Ensure log directories exist.
	for _, sol := range state.Solutions {
		if err := os.MkdirAll(strings.TrimSuffix(sol.LogFile, fmt.Sprintf("solve-%d.log", sol.Index)), 0o755); err != nil {
			return fmt.Errorf("create log dir: %w", err)
		}
	}

	var mu sync.Mutex
	var wg sync.WaitGroup

	for i := range state.Solutions {
		wg.Add(1)
		sol := &state.Solutions[i]

		go func(sol *config.Solution) {
			defer wg.Done()

			a, _ := agent.Get(sol.Agent)
			cmdArgs, err := a.Args(agent.RunArgs{
				Prompt:  resolvedPrompt,
				Model:   sol.Model,
				OcAgent: sol.OcAgent,
			})
			if err != nil {
				mu.Lock()
				sol.Status = config.StatusFailed
				_ = config.Save(repoRoot, state)
				mu.Unlock()
				fmt.Fprintf(os.Stderr, "[%s/solve-%d] build command: %s\n", state.Name, sol.Index, err)
				return
			}

			logFile, err := os.Create(sol.LogFile)
			if err != nil {
				mu.Lock()
				sol.Status = config.StatusFailed
				_ = config.Save(repoRoot, state)
				mu.Unlock()
				fmt.Fprintf(os.Stderr, "[%s/solve-%d] create log file: %s\n", state.Name, sol.Index, err)
				return
			}
			defer logFile.Close()

			cmd := exec.Command(cmdArgs[0], cmdArgs[1:]...)
			cmd.Dir = sol.Worktree

			pr, pw := io.Pipe()
			cmd.Stdout = pw
			cmd.Stderr = pw

			// agentPrefix is shown on streamed agent output lines.
			agentPrefix := fmt.Sprintf("[%s] ", state.Name)
			var fanWg sync.WaitGroup
			fanWg.Add(1)
			go func() {
				defer fanWg.Done()
				scanner := bufio.NewScanner(pr)
				scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
				firstLine := true
				for scanner.Scan() {
					line := scanner.Text()
					fmt.Fprintln(logFile, line)

					// Try to parse as a JSON event from opencode run --format json.
					var event struct {
						SessionID string `json:"sessionID"`
						Type      string `json:"type"`
						Part      struct {
							Text   string `json:"text"`
							Tokens struct {
								Total  int `json:"total"`
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
					if jsonErr := json.Unmarshal([]byte(line), &event); jsonErr == nil {
						if firstLine && event.SessionID != "" {
							mu.Lock()
							sol.SessionID = event.SessionID
							_ = config.Save(repoRoot, state)
							mu.Unlock()
						}
						// Print only meaningful text content, not raw JSON.
						switch {
						case event.Type == "text" && event.Part.Text != "":
							fmt.Print(agentPrefix + event.Part.Text)
						case event.Message != "":
							fmt.Println(agentPrefix + event.Message)
						case event.Type == "step_finish":
							mu.Lock()
							sol.InputTokens += event.Part.Tokens.Input
							sol.OutputTokens += event.Part.Tokens.Output
							sol.CacheReadTokens += event.Part.Tokens.Cache.Read
							sol.CacheWriteTokens += event.Part.Tokens.Cache.Write
							sol.CostUSD += event.Part.Cost
							_ = config.Save(repoRoot, state)
							mu.Unlock()
						}
					} else {
						// Not JSON — print as-is (e.g. plain stderr from the agent).
						fmt.Println(agentPrefix + line)
					}
					firstLine = false
				}
			}()

			// label is used for cerberus's own status lines (not agent output).
			label := fmt.Sprintf("[%s/solve-%d] ", state.Name, sol.Index)

			if err := cmd.Start(); err != nil {
				pw.Close()
				fanWg.Wait()
				mu.Lock()
				sol.Status = config.StatusFailed
				_ = config.Save(repoRoot, state)
				mu.Unlock()
				fmt.Fprintf(os.Stderr, "%sstart agent: %s\n", label, err)
				return
			}

			mu.Lock()
			sol.PID = cmd.Process.Pid
			sol.Status = config.StatusRunning
			sol.StartedAt = time.Now()
			_ = config.Save(repoRoot, state)
			mu.Unlock()

			err = cmd.Wait()
			pw.Close()
			fanWg.Wait()

			mu.Lock()
			sol.FinishedAt = time.Now()
			if err != nil {
				sol.Status = config.StatusFailed
				if exitErr, ok := err.(*exec.ExitError); ok {
					sol.ExitCode = exitErr.ExitCode()
				}
				_ = config.Save(repoRoot, state)
				mu.Unlock()
				fmt.Fprintf(os.Stderr, "%sagent failed (exit code %d)\n", label, sol.ExitCode)
				return
			}
			sol.Status = config.StatusDone
			_ = config.Save(repoRoot, state)
			mu.Unlock()

			// Auto-commit any changes the agent left in the worktree.
			hasChanges, err := git.HasChanges(sol.Worktree)
			if err != nil {
				fmt.Fprintf(os.Stderr, "%scheck changes: %s\n", label, err)
				return
			}
			if !hasChanges {
				fmt.Printf("%sno changes to commit\n", label)
				return
			}

			fmt.Printf("%scommitting...\n", label)

			diff, err := git.Diff(sol.Worktree, baseCommit)
			if err != nil {
				diff = ""
			}

			commitMsg := agent.AskForCommitMessage(sol.Worktree, diff)
			commitHash, err := git.CommitAndGetHash(sol.Worktree, commitMsg)
			if err != nil {
				fmt.Fprintf(os.Stderr, "%scommit failed: %s\n", label, err)
				return
			}

			mu.Lock()
			sol.CommitHash = commitHash
			_ = config.Save(repoRoot, state)
			mu.Unlock()

			fmt.Printf("%scommit %s  %s\n", label, commitHash[:8], commitMsg)
		}(sol)
	}

	wg.Wait()

	failed := 0
	fmt.Println()
	for _, sol := range state.Solutions {
		if sol.Status == config.StatusFailed {
			failed++
			fmt.Printf("  solve-%d  FAILED\n", sol.Index)
		} else if sol.CommitHash != "" {
			fmt.Printf("  solve-%d  done  %s  %s\n", sol.Index, sol.CommitHash[:8], sol.Branch)
		} else {
			fmt.Printf("  solve-%d  done  (no changes)\n", sol.Index)
		}
	}

	if failed > 0 {
		return fmt.Errorf("%d solution(s) failed; check logs above", failed)
	}
	fmt.Printf("\nrun 'cerberus review -session %s' to see what changed\n", sessionName)
	return nil
}

// cmdRerun runs the agent again in an existing session's worktree with a new
// prompt, then auto-commits the result. Use this to iterate on a solution
// without starting a fresh session — the worktree already has the previous
// agent's work, so the follow-up prompt can reference and build on it.
// Each rerun stacks a new commit on the solution branch; apply cherry-picks
// the whole range.
func cmdRerun(sessionFlag string, solution int, prompt, promptFile string) error {
	if solution == 0 {
		return fmt.Errorf("-solution is required")
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
		return fmt.Errorf("-prompt or -prompt-file is required")
	}

	repoRoot, err := resolveRepoRoot()
	if err != nil {
		return err
	}
	sessionName, err := resolveSession(repoRoot, sessionFlag)
	if err != nil {
		return err
	}

	state, err := config.Load(repoRoot, sessionName)
	if err != nil {
		return err
	}

	var sol *config.Solution
	for i := range state.Solutions {
		if state.Solutions[i].Index == solution {
			sol = &state.Solutions[i]
			break
		}
	}
	if sol == nil {
		return fmt.Errorf("solution %d not found in session %q", solution, sessionName)
	}
	if sol.Status == config.StatusRunning && processAlive(sol.PID) {
		return fmt.Errorf("solve-%d is still running; wait for it to finish first", sol.Index)
	}

	a, err := agent.Get(sol.Agent)
	if err != nil {
		return err
	}
	cmdArgs, err := a.Args(agent.RunArgs{
		Prompt:  resolvedPrompt,
		Model:   sol.Model,
		OcAgent: sol.OcAgent,
	})
	if err != nil {
		return fmt.Errorf("build command: %w", err)
	}

	logFile, err := os.OpenFile(sol.LogFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open log file: %w", err)
	}
	defer logFile.Close()

	fmt.Printf("rerunning [%s/solve-%d] with new prompt...\n", sessionName, solution)
	fmt.Fprintf(logFile, "\n--- rerun: %s ---\n", time.Now().Format(time.RFC3339))

	cmd := exec.Command(cmdArgs[0], cmdArgs[1:]...)
	cmd.Dir = sol.Worktree

	pr, pw := io.Pipe()
	cmd.Stdout = pw
	cmd.Stderr = pw

	agentPrefix := fmt.Sprintf("[%s] ", sessionName)
	var fanWg sync.WaitGroup
	fanWg.Add(1)
	go func() {
		defer fanWg.Done()
		scanner := bufio.NewScanner(pr)
		scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
		for scanner.Scan() {
			line := scanner.Text()
			fmt.Fprintln(logFile, line)
			var event struct {
				Type string `json:"type"`
				Part struct {
					Text   string `json:"text"`
					Tokens struct {
						Total  int `json:"total"`
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
			if jsonErr := json.Unmarshal([]byte(line), &event); jsonErr == nil {
				switch {
				case event.Type == "text" && event.Part.Text != "":
					fmt.Print(agentPrefix + event.Part.Text)
				case event.Message != "":
					fmt.Println(agentPrefix + event.Message)
				case event.Type == "step_finish":
					sol.InputTokens += event.Part.Tokens.Input
					sol.OutputTokens += event.Part.Tokens.Output
					sol.CacheReadTokens += event.Part.Tokens.Cache.Read
					sol.CacheWriteTokens += event.Part.Tokens.Cache.Write
					sol.CostUSD += event.Part.Cost
					_ = config.Save(repoRoot, state)
				}
			} else {
				fmt.Println(agentPrefix + line)
			}
		}
	}()

	if err := cmd.Start(); err != nil {
		pw.Close()
		fanWg.Wait()
		return fmt.Errorf("start agent: %w", err)
	}

	sol.Status = config.StatusRunning
	sol.PID = cmd.Process.Pid
	_ = config.Save(repoRoot, state)

	runErr := cmd.Wait()
	pw.Close()
	fanWg.Wait()

	label := fmt.Sprintf("[%s/solve-%d] ", sessionName, solution)

	if runErr != nil {
		sol.Status = config.StatusFailed
		_ = config.Save(repoRoot, state)
		return fmt.Errorf("%sagent failed: %w", label, runErr)
	}
	sol.Status = config.StatusDone
	_ = config.Save(repoRoot, state)

	// Auto-commit changes.
	hasChanges, err := git.HasChanges(sol.Worktree)
	if err != nil {
		return fmt.Errorf("%scheck changes: %w", label, err)
	}
	if !hasChanges {
		fmt.Printf("%sno changes to commit\n", label)
		return nil
	}

	fmt.Printf("%scommitting...\n", label)

	diff, _ := git.Diff(sol.Worktree, state.BaseCommit)
	commitMsg := agent.AskForCommitMessage(sol.Worktree, diff)
	commitHash, err := git.CommitAndGetHash(sol.Worktree, commitMsg)
	if err != nil {
		return fmt.Errorf("%scommit failed: %w", label, err)
	}

	sol.CommitHash = commitHash
	_ = config.Save(repoRoot, state)

	fmt.Printf("%scommit %s  %s\n", label, commitHash[:8], commitMsg)
	fmt.Printf("\nrun 'cerberus apply -session %s -solution %d' when ready\n", sessionName, solution)
	return nil
}

// cmdList lists all active sessions in the current repo.
func cmdList() error {
	repoRoot, err := resolveRepoRoot()
	if err != nil {
		return err
	}

	sessions, err := config.ListSessions(repoRoot)
	if err != nil {
		return err
	}
	if len(sessions) == 0 {
		fmt.Println("no active sessions")
		return nil
	}

	for _, name := range sessions {
		state, err := config.Load(repoRoot, name)
		if err != nil {
			fmt.Printf("  %-20s  (error: %s)\n", name, err)
			continue
		}
		running := 0
		done := 0
		failed := 0
		for _, sol := range state.Solutions {
			switch sol.Status {
			case config.StatusRunning:
				running++
			case config.StatusDone:
				done++
			case config.StatusFailed:
				failed++
			}
		}
		statusSummary := fmt.Sprintf("%d done", done)
		if running > 0 {
			statusSummary = fmt.Sprintf("%d running, %d done", running, done)
		}
		if failed > 0 {
			statusSummary += fmt.Sprintf(", %d failed", failed)
		}
		fmt.Printf("  %-20s  %-25s  %s\n", name, state.BaseBranch, statusSummary)
		fmt.Printf("  %20s  %s\n", "", truncate(state.Prompt, 60))
	}
	return nil
}

// cmdStatus prints the current state of a session's solutions.
func cmdStatus(sessionFlag string) error {
	repoRoot, err := resolveRepoRoot()
	if err != nil {
		return err
	}
	sessionName, err := resolveSession(repoRoot, sessionFlag)
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

	for _, sol := range state.Solutions {
		status := string(sol.Status)
		if sol.PID > 0 && sol.Status == config.StatusRunning {
			if !processAlive(sol.PID) {
				status = "running (process gone)"
			}
		}
		if sol.CommitHash != "" {
			fmt.Printf("  solve-%d  %-10s  %s\n", sol.Index, status, sol.CommitHash[:8])
		} else {
			fmt.Printf("  solve-%d  %s\n", sol.Index, status)
		}
	}
	return nil
}

// cmdLogs tails all solution log files for a session.
func cmdLogs(sessionFlag string) error {
	repoRoot, err := resolveRepoRoot()
	if err != nil {
		return err
	}
	sessionName, err := resolveSession(repoRoot, sessionFlag)
	if err != nil {
		return err
	}

	state, err := config.Load(repoRoot, sessionName)
	if err != nil {
		return err
	}

	if len(state.Solutions) == 0 {
		fmt.Println("no solutions found")
		return nil
	}

	var wg sync.WaitGroup
	for i := range state.Solutions {
		sol := &state.Solutions[i]
		if sol.LogFile == "" {
			continue
		}
		wg.Add(1)
		go func(sol *config.Solution) {
			defer wg.Done()
			f, err := os.Open(sol.LogFile)
			if err != nil {
				fmt.Fprintf(os.Stderr, "[%s/solve-%d] open log: %s\n", state.Name, sol.Index, err)
				return
			}
			defer f.Close()

			prefix := fmt.Sprintf("[%s/solve-%d] ", state.Name, sol.Index)
			scanner := bufio.NewScanner(f)
			for scanner.Scan() {
				raw := scanner.Text()
				var event struct {
					Message string `json:"message"`
				}
				if jsonErr := json.Unmarshal([]byte(raw), &event); jsonErr == nil && event.Message != "" {
					fmt.Println(prefix + event.Message)
				} else {
					fmt.Println(prefix + raw)
				}
			}
		}(sol)
	}
	wg.Wait()
	return nil
}

// cmdReview prints the changed files and diffs for each solution in a session.
func cmdReview(sessionFlag string, diffFlag bool) error {
	repoRoot, err := resolveRepoRoot()
	if err != nil {
		return err
	}
	sessionName, err := resolveSession(repoRoot, sessionFlag)
	if err != nil {
		return err
	}

	state, err := config.Load(repoRoot, sessionName)
	if err != nil {
		return err
	}

	for _, sol := range state.Solutions {
		if sol.CommitHash != "" {
			fmt.Printf("solve-%d  %s  %s\n", sol.Index, sol.CommitHash[:8], sol.Branch)
		} else {
			fmt.Printf("solve-%d  %s  %s\n", sol.Index, sol.Status, sol.Branch)
		}

		var files []string
		var filesErr error
		if sol.CommitHash != "" {
			files, filesErr = git.CommittedChangedFiles(sol.Worktree, state.BaseCommit)
		} else {
			files, filesErr = git.ChangedFiles(sol.Worktree, state.BaseCommit)
		}
		if filesErr != nil {
			fmt.Printf("  (error reading changes: %s)\n\n", filesErr)
			continue
		}
		if len(files) == 0 {
			fmt.Printf("  (no changes)\n\n")
			continue
		}

		for _, f := range files {
			fmt.Printf("  %s\n", f)
		}

		if diffFlag {
			fmt.Println()
			var diffErr error
			if sol.CommitHash != "" {
				diffErr = git.ShowCommittedDiff(sol.Worktree, state.BaseCommit)
			} else {
				diffErr = git.ShowDiff(sol.Worktree, state.BaseCommit)
			}
			if diffErr != nil {
				fmt.Printf("  (error reading diff: %s)\n", diffErr)
			}
		}
		fmt.Println()
	}
	return nil
}

// cmdApply cherry-picks the chosen solution's commit onto the current branch.
func cmdApply(sessionFlag string, solution int) error {
	repoRoot, err := resolveRepoRoot()
	if err != nil {
		return err
	}
	sessionName, err := resolveSession(repoRoot, sessionFlag)
	if err != nil {
		return err
	}

	state, err := config.Load(repoRoot, sessionName)
	if err != nil {
		return err
	}

	if solution == 0 {
		if len(state.Solutions) == 1 {
			solution = 1
		} else {
			return fmt.Errorf("-solution is required")
		}
	}

	var sol *config.Solution
	for i := range state.Solutions {
		if state.Solutions[i].Index == solution {
			sol = &state.Solutions[i]
			break
		}
	}
	if sol == nil {
		return fmt.Errorf("solution %d not found in session %q", solution, sessionName)
	}

	if sol.CommitHash == "" {
		return fmt.Errorf("solve-%d has no commit; either the agent made no changes or the session predates auto-commit", sol.Index)
	}

	// Count commits being applied for the user.
	commits, err := git.CommitsBetween(sol.Worktree, state.BaseCommit, sol.CommitHash)
	if err != nil || len(commits) == 0 {
		// Fall back to single cherry-pick if range query fails.
		commits = []string{sol.CommitHash}
	}

	if len(commits) == 1 {
		fmt.Printf("cherry-picking %s (%s)...\n", sol.CommitHash[:8], sol.Branch)
		if err := git.CherryPick(repoRoot, sol.CommitHash); err != nil {
			fmt.Fprintf(os.Stderr, "\ncherry-pick failed (likely a conflict).\n")
			fmt.Fprintf(os.Stderr, "Resolve conflicts then run: git cherry-pick --continue\n")
			fmt.Fprintf(os.Stderr, "To abort:                   git cherry-pick --abort\n")
			if _, err := exec.LookPath("lazygit"); err == nil {
				fmt.Fprintf(os.Stderr, "launching lazygit to resolve conflicts...\n")
				cmd := exec.Command("lazygit")
				cmd.Dir = repoRoot
				cmd.Stdin = os.Stdin
				cmd.Stdout = os.Stdout
				cmd.Stderr = os.Stderr
				if runErr := cmd.Run(); runErr == nil {
					return nil
				}
			}
			return err
		}
	} else {
		fmt.Printf("cherry-picking %d commits from %s...\n", len(commits), sol.Branch)
		if err := git.CherryPickRange(repoRoot, state.BaseCommit, sol.CommitHash); err != nil {
			fmt.Fprintf(os.Stderr, "\ncherry-pick failed (likely a conflict).\n")
			fmt.Fprintf(os.Stderr, "Resolve conflicts then run: git cherry-pick --continue\n")
			fmt.Fprintf(os.Stderr, "To abort:                   git cherry-pick --abort\n")
			if _, err := exec.LookPath("lazygit"); err == nil {
				fmt.Fprintf(os.Stderr, "launching lazygit to resolve conflicts...\n")
				cmd := exec.Command("lazygit")
				cmd.Dir = repoRoot
				cmd.Stdin = os.Stdin
				cmd.Stdout = os.Stdout
				cmd.Stderr = os.Stderr
				if runErr := cmd.Run(); runErr == nil {
					return nil
				}
			}
			return err
		}
	}

	fmt.Println("done.")

	if err := recordStats(state, sol.Index); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not record stats: %s\n", err)
	}

	return nil
}

// cmdMerge collects all solution diffs for a session and sends them to an LLM.
func cmdMerge(sessionFlag, model string) error {
	repoRoot, err := resolveRepoRoot()
	if err != nil {
		return err
	}
	sessionName, err := resolveSession(repoRoot, sessionFlag)
	if err != nil {
		return err
	}

	state, err := config.Load(repoRoot, sessionName)
	if err != nil {
		return err
	}

	type solutionDiff struct {
		sol  config.Solution
		diff string
	}
	var diffs []solutionDiff
	for _, sol := range state.Solutions {
		var diff string
		var diffErr error
		if sol.CommitHash != "" {
			diff, diffErr = git.CommittedDiff(sol.Worktree, state.BaseCommit)
		} else {
			diff, diffErr = git.Diff(sol.Worktree, state.BaseCommit)
		}
		if diffErr != nil {
			return fmt.Errorf("get diff for solve-%d: %w", sol.Index, diffErr)
		}
		if strings.TrimSpace(diff) == "" {
			fmt.Fprintf(os.Stderr, "solve-%d has no changes, skipping\n", sol.Index)
			continue
		}
		diffs = append(diffs, solutionDiff{sol: sol, diff: diff})
	}

	if len(diffs) == 0 {
		return fmt.Errorf("no solutions have changes to merge")
	}

	var b strings.Builder
	fmt.Fprintf(&b, "I ran the following task against a codebase using %d AI agents in parallel, each in its own git worktree.\n\n", len(diffs))
	fmt.Fprintf(&b, "Original task:\n%s\n\n", state.Prompt)
	fmt.Fprintf(&b, "Below are the unified diffs produced by each solution. Please analyse them, identify the strengths and weaknesses of each approach, and produce a single best merged solution.\n\n")
	fmt.Fprintf(&b, "For each file that needs to change, output the COMPLETE file content inside a fenced code block annotated with the file path, like this:\n\n")
	fmt.Fprintf(&b, "```path/to/file.go\n<full file content here>\n```\n\n")
	fmt.Fprintf(&b, "After all file blocks, write a single line starting with exactly 'COMMIT_MESSAGE:' followed by a commit message in Conventional Commits format: <type>(<scope>): <description> — subject line only, max 72 chars total, imperative mood, lowercase, no period.\n\n")
	fmt.Fprintf(&b, "---\n\n")

	for _, d := range diffs {
		modelLabel := d.sol.Model
		if modelLabel == "" {
			modelLabel = "opencode default"
		}
		commitNote := ""
		if d.sol.CommitHash != "" {
			commitNote = fmt.Sprintf(" commit=%s", d.sol.CommitHash[:8])
		}
		fmt.Fprintf(&b, "## Solution %d (agent=%s model=%s%s)\n\n", d.sol.Index, d.sol.Agent, modelLabel, commitNote)
		fmt.Fprintf(&b, "```diff\n%s\n```\n\n", d.diff)
	}

	mergePrompt := b.String()

	ocArgs := []string{"opencode", "run", "--format", "json"}
	if model != "" {
		ocArgs = append(ocArgs, "-m", model)
	}
	ocArgs = append(ocArgs, mergePrompt)

	cmd := exec.Command(ocArgs[0], ocArgs[1:]...)
	cmd.Dir = repoRoot

	pr, pw := io.Pipe()
	cmd.Stdout = pw
	cmd.Stderr = pw

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start opencode: %w", err)
	}

	var collected strings.Builder
	var scanWg sync.WaitGroup
	scanWg.Add(1)
	go func() {
		defer scanWg.Done()
		scanner := bufio.NewScanner(pr)
		scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
		for scanner.Scan() {
			var event struct {
				Type string `json:"type"`
				Part struct {
					Text string `json:"text"`
				} `json:"part"`
			}
			if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
				continue
			}
			if event.Type == "text" && event.Part.Text != "" {
				fmt.Print(event.Part.Text)
				collected.WriteString(event.Part.Text)
			}
		}
		fmt.Println()
	}()

	err = cmd.Wait()
	pw.Close()
	scanWg.Wait()

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return fmt.Errorf("opencode exited with code %d", exitErr.ExitCode())
		}
		return fmt.Errorf("opencode: %w", err)
	}

	suggPath := config.MergeSuggestionPath(repoRoot, sessionName)
	if writeErr := os.WriteFile(suggPath, []byte(collected.String()), 0o644); writeErr != nil {
		fmt.Fprintf(os.Stderr, "warning: could not save merge suggestion: %s\n", writeErr)
	} else {
		fmt.Printf("\nmerge suggestion saved to %s\n", suggPath)
		fmt.Printf("run 'cerberus merge-apply -session %s' to apply and commit it\n", sessionName)
	}

	return nil
}

// cmdMergeApply reads the merge suggestion for a session, writes files, and commits.
func cmdMergeApply(sessionFlag string) error {
	repoRoot, err := resolveRepoRoot()
	if err != nil {
		return err
	}
	sessionName, err := resolveSession(repoRoot, sessionFlag)
	if err != nil {
		return err
	}

	state, err := config.Load(repoRoot, sessionName)
	if err != nil {
		return err
	}

	suggPath := config.MergeSuggestionPath(repoRoot, sessionName)
	data, err := os.ReadFile(suggPath)
	if err != nil {
		return fmt.Errorf("read merge suggestion (%s): %w — run 'cerberus merge -session %s' first", suggPath, err, sessionName)
	}

	suggestion := string(data)

	commitMsg := ""
	for _, line := range strings.Split(suggestion, "\n") {
		if strings.HasPrefix(line, "COMMIT_MESSAGE:") {
			commitMsg = strings.TrimSpace(strings.TrimPrefix(line, "COMMIT_MESSAGE:"))
			break
		}
	}
	if commitMsg == "" {
		commitMsg = "chore(cerberus): merged solutions"
	}

	files, err := extractFileBlocks(suggestion)
	if err != nil {
		return fmt.Errorf("parse merge suggestion: %w", err)
	}
	if len(files) == 0 {
		return fmt.Errorf("no file blocks found in merge suggestion; check %s", suggPath)
	}

	fmt.Printf("applying %d file(s)...\n", len(files))
	for relPath, content := range files {
		if err := git.WriteFile(repoRoot, relPath, content); err != nil {
			return fmt.Errorf("write %s: %w", relPath, err)
		}
		fmt.Printf("  wrote %s\n", relPath)
	}

	if _, err := git.CommitAndGetHash(repoRoot, commitMsg); err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	fmt.Printf("committed: %s\n", commitMsg)

	if err := recordStats(state, 0); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not record stats: %s\n", err)
	}

	fmt.Printf("cleaning up session %q...\n", sessionName)
	if err := cleanSession(repoRoot, state); err != nil {
		fmt.Fprintf(os.Stderr, "warning: cleanup incomplete: %s\n", err)
	}

	return nil
}

// extractFileBlocks parses fenced code blocks of the form:
//
//	```some/path/file.go
//	<content>
//	```
func extractFileBlocks(text string) (map[string]string, error) {
	result := make(map[string]string)
	lines := strings.Split(text, "\n")

	inBlock := false
	var currentPath string
	var currentContent strings.Builder

	for _, line := range lines {
		if !inBlock {
			if strings.HasPrefix(line, "```") {
				path := strings.TrimPrefix(line, "```")
				path = strings.TrimSpace(path)
				if path != "" && path != "diff" && (strings.Contains(path, "/") || strings.Contains(path, ".")) {
					inBlock = true
					currentPath = path
					currentContent.Reset()
				}
			}
		} else {
			if line == "```" {
				result[currentPath] = currentContent.String()
				inBlock = false
				currentPath = ""
			} else {
				currentContent.WriteString(line)
				currentContent.WriteByte('\n')
			}
		}
	}

	return result, nil
}

// cmdClean removes a session's worktrees, branches, opencode sessions, and state.
// When all is true, it cleans all sessions in the repo instead of a single session.
func cmdClean(sessionFlag string, force bool, all bool) error {
	repoRoot, err := resolveRepoRoot()
	if err != nil {
		return err
	}

	if all {
		// Clean all sessions
		sessions, err := config.ListSessions(repoRoot)
		if err != nil {
			return err
		}

		if len(sessions) == 0 {
			fmt.Println("no sessions to clean")
			return nil
		}

		if !force {
			fmt.Printf("clean all sessions (%d): %d session(s) will be removed.\n", len(sessions), len(sessions))
			fmt.Print("continue? [y/N] ")
			var answer string
			fmt.Scanln(&answer)
			if strings.ToLower(strings.TrimSpace(answer)) != "y" {
				fmt.Println("aborted.")
				return nil
			}
		}

		var errs []string
		for _, name := range sessions {
			state, err := config.Load(repoRoot, name)
			if err != nil {
				errs = append(errs, fmt.Sprintf("load session %s: %s", name, err))
				continue
			}
			if err := cleanSession(repoRoot, state); err != nil {
				errs = append(errs, fmt.Sprintf("clean session %s: %s", name, err))
			}
		}

		if len(errs) > 0 {
			return fmt.Errorf("clean completed with errors:\n  %s", strings.Join(errs, "\n  "))
		}
		return nil
	}

	// Single session clean
	sessionName, err := resolveSession(repoRoot, sessionFlag)
	if err != nil {
		return err
	}

	state, err := config.Load(repoRoot, sessionName)
	if err != nil {
		return err
	}

	if !force {
		fmt.Printf("clean session %q: %d worktree(s) will be removed.\n", sessionName, len(state.Solutions))
		fmt.Print("continue? [y/N] ")
		var answer string
		fmt.Scanln(&answer)
		if strings.ToLower(strings.TrimSpace(answer)) != "y" {
			fmt.Println("aborted.")
			return nil
		}
	}

	return cleanSession(repoRoot, state)
}

// cleanSession removes all worktrees, branches, opencode sessions, and state
// for the given session. Called by cmdClean and automatically by cmdMergeApply.
func cleanSession(repoRoot string, state *config.State) error {
	var errs []string

	for _, sol := range state.Solutions {
		if sol.SessionID == "" {
			continue
		}
		cmd := exec.Command("opencode", "session", "delete", sol.SessionID)
		if out, err := cmd.CombinedOutput(); err != nil {
			errs = append(errs, fmt.Sprintf("delete opencode session %s: %s", sol.SessionID, strings.TrimSpace(string(out))))
		}
	}

	for _, sol := range state.Solutions {
		if err := git.RemoveWorktree(repoRoot, state.Name, sol.Index); err != nil {
			errs = append(errs, fmt.Sprintf("remove worktree solve-%d: %s", sol.Index, err))
		}
	}

	if err := config.Remove(repoRoot, state.Name); err != nil {
		errs = append(errs, fmt.Sprintf("remove state: %s", err))
	}

	if len(errs) > 0 {
		return fmt.Errorf("clean completed with errors:\n  %s", strings.Join(errs, "\n  "))
	}
	return nil
}

// recordStats builds a StatsRecord from state and appends it to the global stats file.
func recordStats(state *config.State, winnerIndex int) error {
	rec := config.StatsRecord{
		SessionDate:   time.Now(),
		SessionName:   state.Name,
		PromptSnippet: truncate(state.Prompt, 80),
		BaseBranch:    state.BaseBranch,
		WinnerIndex:   winnerIndex,
	}
	for _, sol := range state.Solutions {
		r := config.StatsRunner{
			Model:   sol.Model,
			OcAgent: sol.OcAgent,
			Status:  string(sol.Status),
		}
		if !sol.StartedAt.IsZero() && !sol.FinishedAt.IsZero() {
			r.DurationS = sol.FinishedAt.Sub(sol.StartedAt).Seconds()
		}
		r.InputTokens = sol.InputTokens
		r.OutputTokens = sol.OutputTokens
		r.CacheReadTokens = sol.CacheReadTokens
		r.CacheWriteTokens = sol.CacheWriteTokens
		r.CostUSD = sol.CostUSD
		rec.Runners = append(rec.Runners, r)
	}
	for _, r := range rec.Runners {
		rec.TotalCostUSD += r.CostUSD
	}
	return config.AppendStats(rec)
}

// cmdStats reads the global stats file and prints two tables:
// 1. Per-model aggregate statistics
// 2. Per-session history (most recent first, capped at 20)
func cmdStats() error {
	records, err := config.LoadStats()
	if err != nil {
		return err
	}
	if len(records) == 0 {
		fmt.Println("no stats recorded yet. Stats are saved when you run 'cerberus apply'.")
		return nil
	}

	type modelStats struct {
		runs      int
		wins      int
		totalDurS float64
		durationN int
		inputTok  int
		outputTok int
		costUSD   float64
	}
	byModel := map[string]*modelStats{}

	for _, rec := range records {
		for i, r := range rec.Runners {
			key := r.Model
			if key == "" {
				key = "(default)"
			}
			if r.OcAgent != "" {
				key = key + " / " + r.OcAgent
			}
			if _, ok := byModel[key]; !ok {
				byModel[key] = &modelStats{}
			}
			ms := byModel[key]
			ms.runs++
			if rec.WinnerIndex > 0 && rec.WinnerIndex == i+1 {
				ms.wins++
			}
			if r.DurationS > 0 {
				ms.totalDurS += r.DurationS
				ms.durationN++
			}
			ms.inputTok += r.InputTokens
			ms.outputTok += r.OutputTokens
			ms.costUSD += r.CostUSD
		}
	}

	// Table 1: Per-model aggregate
	fmt.Printf("%-40s  %5s  %5s  %5s  %10s  %11s  %11s  %10s\n",
		"Model", "Runs", "Wins", "Win%", "Avg dur", "Input tok", "Output tok", "Cost USD")
	fmt.Println(strings.Repeat("-", 95))
	for model, ms := range byModel {
		winPct := 0.0
		if ms.runs > 0 {
			winPct = float64(ms.wins) / float64(ms.runs) * 100
		}
		avgDur := "-"
		if ms.durationN > 0 {
			avgDur = fmt.Sprintf("%.0fs", ms.totalDurS/float64(ms.durationN))
		}
		cost := "-"
		if ms.costUSD > 0 {
			cost = fmt.Sprintf("$%.4f", ms.costUSD)
		}
		fmt.Printf("%-40s  %5d  %5d  %4.0f%%  %10s  %11d  %11d  %10s\n",
			truncate(model, 40), ms.runs, ms.wins, winPct, avgDur, ms.inputTok, ms.outputTok, cost)
	}

	// Table 2: Per-session history (most recent first, capped at 20)
	fmt.Println()
	fmt.Printf("%-18s  %-16s  %-30s  %8s  %10s  %8s  %8s  %10s\n",
		"Session", "Date", "Runner", "Status", "Duration", "Input", "Output", "Cost")
	fmt.Println(strings.Repeat("-", 110))

	// Reverse records to show most recent first, then cap at 20
	var displayRecords []config.StatsRecord
	for i := len(records) - 1; i >= 0 && len(displayRecords) < 20; i-- {
		displayRecords = append(displayRecords, records[i])
	}

	for _, rec := range displayRecords {
		sessionDate := rec.SessionDate.Format("2006-01-02 15:04")
		sessionName := truncate(rec.SessionName, 18)
		firstRunner := true
		for _, r := range rec.Runners {
			runner := r.Model
			if runner == "" {
				runner = "(default)"
			}
			if r.OcAgent != "" {
				runner = runner + " / " + r.OcAgent
			}
			runner = truncate(runner, 30)

			status := r.Status
			durationStr := "-"
			if r.DurationS > 0 {
				durationStr = fmt.Sprintf("%.0fs", r.DurationS)
			}
			costStr := "-"
			if r.CostUSD > 0 {
				costStr = fmt.Sprintf("$%.4f", r.CostUSD)
			}

			displaySession := ""
			if firstRunner {
				displaySession = sessionName
				firstRunner = false
			}

			fmt.Printf("%-18s  %-16s  %-30s  %8s  %10s  %8d  %8d  %10s\n",
				displaySession, sessionDate, runner, status, durationStr, r.InputTokens, r.OutputTokens, costStr)
		}
	}

	fmt.Printf("\n%d session(s) recorded\n", len(records))
	return nil
}

func processAlive(pid int) bool {
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return p.Signal(syscall.Signal(0)) == nil
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}
