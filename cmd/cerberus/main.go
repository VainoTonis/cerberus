package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"

	"github.com/tonis/cerberus/internal/agent"
	"github.com/tonis/cerberus/internal/config"
	"github.com/tonis/cerberus/internal/git"
)

const usage = `cerberus - multi-agent parallel problem solver

Usage:
  cerberus <command> [flags]

Commands:
  start    Create worktrees and run agents in parallel
  status   Show the status of each solution
  review   Print a summary of what each solution changed
  apply    Apply a solution's changes to the main worktree
  merge    Ask an LLM to analyse all solutions and suggest the best merge
  clean    Remove all worktrees, branches, and state

Run 'cerberus <command> -help' for command-specific flags.
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(1)
	}

	cmd := os.Args[1]
	args := os.Args[2:]

	var err error
	switch cmd {
	case "start":
		err = cmdStart(args)
	case "status":
		err = cmdStatus(args)
	case "review":
		err = cmdReview(args)
	case "apply":
		err = cmdApply(args)
	case "merge":
		err = cmdMerge(args)
	case "clean":
		err = cmdClean(args)
	case "-help", "--help", "help":
		fmt.Fprint(os.Stdout, usage)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", cmd, usage)
		os.Exit(1)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err)
		os.Exit(1)
	}
}

// cmdStart creates git worktrees and runs N agents in parallel, blocking until all finish.
func cmdStart(args []string) error {
	userCfg, err := config.LoadUserConfig()
	if err != nil {
		return err
	}

	fs := flag.NewFlagSet("start", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: cerberus start [flags]

Flags:
`)
		fs.PrintDefaults()
	}

	n := fs.Int("n", 0, "number of solutions (default: number of runners in config)")
	prompt := fs.String("prompt", "", "prompt to send to each agent (required)")
	promptFile := fs.String("prompt-file", "", "read prompt from file instead of -prompt")
	agentFlag := fs.String("agent", "opencode", "agent to use when not set in config")
	modelFlag := fs.String("model", "", "model to use when not set in config")

	if err := fs.Parse(args); err != nil {
		return err
	}

	resolvedPrompt := strings.TrimSpace(*prompt)
	if *promptFile != "" {
		data, err := os.ReadFile(*promptFile)
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
	// Priority: config runners > -n with -agent/-model flags.
	runners := userCfg.Runners
	if len(runners) == 0 {
		count := *n
		if count == 0 {
			count = 2
		}
		for range count {
			runners = append(runners, config.Runner{Agent: *agentFlag, Model: *modelFlag})
		}
	} else if *n > 0 {
		// -n was explicitly set: truncate or repeat runners to match.
		for len(runners) < *n {
			runners = append(runners, runners[len(runners)-1])
		}
		runners = runners[:*n]
	}

	if len(runners) == 0 {
		return fmt.Errorf("no runners configured; add runners to ~/.config/cerberus/config.json or pass -agent/-model")
	}

	// Validate all agents up front before touching the filesystem.
	for i, r := range runners {
		if _, err := agent.Get(r.Agent); err != nil {
			return fmt.Errorf("runner %d: %w", i+1, err)
		}
	}

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get working directory: %w", err)
	}
	repoRoot, err := git.RepoRoot(cwd)
	if err != nil {
		return err
	}

	if _, err := config.Load(repoRoot); err == nil {
		return fmt.Errorf("a cerberus session already exists in %s; run 'cerberus clean' first", repoRoot)
	}

	baseBranch, err := git.CurrentBranch(repoRoot)
	if err != nil {
		return err
	}
	baseCommit, err := git.CurrentCommit(repoRoot)
	if err != nil {
		return err
	}

	fmt.Printf("starting %d solutions from %s (%s)\n\n", len(runners), baseBranch, baseCommit[:8])

	state := &config.State{
		BaseBranch: baseBranch,
		BaseCommit: baseCommit,
		Prompt:     resolvedPrompt,
		Selections: map[string]int{},
	}

	// Create all worktrees before launching any agents.
	for i, r := range runners {
		idx := i + 1
		wtPath, branch, err := git.CreateWorktree(repoRoot, idx, baseCommit)
		if err != nil {
			for j := 1; j < idx; j++ {
				_ = git.RemoveWorktree(repoRoot, j)
			}
			return err
		}
		logPath := config.LogPath(repoRoot, idx)
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
		fmt.Printf("  worktree solve-%d: %s  agent=%s  model=%s  oc_agent=%s\n", idx, branch, r.Agent, or(r.Model, "(default)"), or(r.OcAgent, "(default)"))
	}
	fmt.Println()

	if err := config.Save(repoRoot, state); err != nil {
		return err
	}

	logDir := fmt.Sprintf("%s/.cerberus/logs", repoRoot)
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return fmt.Errorf("create log dir: %w", err)
	}

	// mu protects concurrent state writes.
	var mu sync.Mutex
	var wg sync.WaitGroup

	for i := range state.Solutions {
		wg.Add(1)
		sol := &state.Solutions[i]

		go func(sol *config.Solution) {
			defer wg.Done()

			a, _ := agent.Get(sol.Agent) // already validated above
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
				fmt.Fprintf(os.Stderr, "[solve-%d] build command: %s\n", sol.Index, err)
				return
			}

			logFile, err := os.Create(sol.LogFile)
			if err != nil {
				mu.Lock()
				sol.Status = config.StatusFailed
				_ = config.Save(repoRoot, state)
				mu.Unlock()
				fmt.Fprintf(os.Stderr, "[solve-%d] create log file: %s\n", sol.Index, err)
				return
			}
			defer logFile.Close()

			cmd := exec.Command(cmdArgs[0], cmdArgs[1:]...)
			cmd.Dir = sol.Worktree

			// Pipe stdout+stderr through a line scanner that fans out to the
			// log file and to stdout with a solve-N prefix.
			pr, pw := io.Pipe()
			cmd.Stdout = pw
			cmd.Stderr = pw

			prefix := fmt.Sprintf("[solve-%d] ", sol.Index)
			var fanWg sync.WaitGroup
			fanWg.Add(1)
			go func() {
				defer fanWg.Done()
				scanner := bufio.NewScanner(pr)
				firstLine := true
				for scanner.Scan() {
					line := scanner.Text()
					fmt.Fprintln(logFile, line)

					// Extract the session ID from the first JSON event emitted
					// by `opencode run --format json`.
					if firstLine {
						firstLine = false
						var event struct {
							SessionID string `json:"sessionID"`
						}
						if jsonErr := json.Unmarshal([]byte(line), &event); jsonErr == nil && event.SessionID != "" {
							mu.Lock()
							sol.SessionID = event.SessionID
							_ = config.Save(repoRoot, state)
							mu.Unlock()
						}
					}

					fmt.Println(prefix + line)
				}
			}()

			if err := cmd.Start(); err != nil {
				pw.Close()
				fanWg.Wait()
				mu.Lock()
				sol.Status = config.StatusFailed
				_ = config.Save(repoRoot, state)
				mu.Unlock()
				fmt.Fprintf(os.Stderr, "[solve-%d] start agent: %s\n", sol.Index, err)
				return
			}

			mu.Lock()
			sol.PID = cmd.Process.Pid
			sol.Status = config.StatusRunning
			_ = config.Save(repoRoot, state)
			mu.Unlock()

			err = cmd.Wait()
			pw.Close()
			fanWg.Wait()

			mu.Lock()
			if err != nil {
				sol.Status = config.StatusFailed
				if exitErr, ok := err.(*exec.ExitError); ok {
					sol.ExitCode = exitErr.ExitCode()
				}
			} else {
				sol.Status = config.StatusDone
			}
			_ = config.Save(repoRoot, state)
			mu.Unlock()
		}(sol)
	}

	wg.Wait()

	fmt.Println()
	failed := 0
	for _, sol := range state.Solutions {
		marker := "ok"
		if sol.Status == config.StatusFailed {
			marker = "FAILED"
			failed++
		}
		fmt.Printf("  solve-%d  %-8s  log=%s\n", sol.Index, marker, sol.LogFile)
	}

	if failed > 0 {
		return fmt.Errorf("%d solution(s) failed; check logs above", failed)
	}
	fmt.Printf("\nrun 'cerberus review' to see what changed\n")
	return nil
}

// cmdStatus prints the current state of all solutions.
func cmdStatus(args []string) error {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, "Usage: cerberus status\n\nShow the status of each solution.\n")
	}
	if err := fs.Parse(args); err != nil {
		return err
	}

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get working directory: %w", err)
	}
	repoRoot, err := git.RepoRoot(cwd)
	if err != nil {
		return err
	}

	state, err := config.Load(repoRoot)
	if err != nil {
		return err
	}

	fmt.Printf("base branch: %s\n", state.BaseBranch)
	fmt.Printf("base commit: %s\n", state.BaseCommit[:8])
	fmt.Printf("prompt:      %s\n", truncate(state.Prompt, 72))
	fmt.Println()

	for _, sol := range state.Solutions {
		alive := ""
		if sol.PID > 0 && sol.Status == config.StatusRunning {
			if processAlive(sol.PID) {
				alive = " (running)"
			} else {
				alive = " (process gone)"
			}
		}
		fmt.Printf("  solve-%d  branch=%-30s status=%s%s\n",
			sol.Index, sol.Branch, sol.Status, alive)
		if sol.LogFile != "" {
			fmt.Printf("           log=%s\n", sol.LogFile)
		}
	}
	return nil
}

// cmdReview prints the changed files and diffs for each solution.
func cmdReview(args []string) error {
	fs := flag.NewFlagSet("review", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, "Usage: cerberus review [-diff]\n\nPrint changed files and diffs for each solution.\n\nFlags:\n")
		fs.PrintDefaults()
	}
	diffFlag := fs.Bool("diff", false, "print full unified diffs (default: file names only)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get working directory: %w", err)
	}
	repoRoot, err := git.RepoRoot(cwd)
	if err != nil {
		return err
	}

	state, err := config.Load(repoRoot)
	if err != nil {
		return err
	}

	for _, sol := range state.Solutions {
		fmt.Printf("=== solve-%d  branch=%s  status=%s ===\n", sol.Index, sol.Branch, sol.Status)

		files, err := git.ChangedFiles(sol.Worktree, state.BaseCommit)
		if err != nil {
			fmt.Printf("  (error reading changes: %s)\n\n", err)
			continue
		}
		if len(files) == 0 {
			fmt.Printf("  (no changes)\n\n")
			continue
		}

		for _, f := range files {
			fmt.Printf("  %s\n", f)
		}

		if *diffFlag {
			fmt.Println()
			diff, err := git.Diff(sol.Worktree, state.BaseCommit)
			if err != nil {
				fmt.Printf("  (error reading diff: %s)\n", err)
			} else {
				fmt.Println(diff)
			}
		}
		fmt.Println()
	}
	return nil
}

// cmdApply checks out all changed files from the chosen solution into the main worktree.
func cmdApply(args []string) error {
	fs := flag.NewFlagSet("apply", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: cerberus apply -solution <n>

Apply all changes from solution N into the current worktree.

Flags:
`)
		fs.PrintDefaults()
	}
	solution := fs.Int("solution", 0, "solution index to apply (required)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *solution == 0 {
		return fmt.Errorf("-solution is required")
	}

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get working directory: %w", err)
	}
	repoRoot, err := git.RepoRoot(cwd)
	if err != nil {
		return err
	}

	state, err := config.Load(repoRoot)
	if err != nil {
		return err
	}

	var sol *config.Solution
	for i := range state.Solutions {
		if state.Solutions[i].Index == *solution {
			sol = &state.Solutions[i]
			break
		}
	}
	if sol == nil {
		return fmt.Errorf("solution %d not found", *solution)
	}

	files, err := git.ChangedFiles(sol.Worktree, state.BaseCommit)
	if err != nil {
		return fmt.Errorf("get changed files: %w", err)
	}
	if len(files) == 0 {
		fmt.Printf("solve-%d has no changes to apply\n", sol.Index)
		return nil
	}

	fmt.Printf("applying %d file(s) from solve-%d (%s):\n", len(files), sol.Index, sol.Branch)
	for _, f := range files {
		if err := git.CheckoutFile(sol.Worktree, repoRoot, f); err != nil {
			return fmt.Errorf("checkout %s: %w", f, err)
		}
		fmt.Printf("  %s\n", f)
	}

	fmt.Printf("\napplied solve-%d to %s\n", sol.Index, repoRoot)
	fmt.Println("review the changes, then commit as usual.")
	return nil
}

// cmdMerge collects all solution diffs, sends them to opencode run, and streams
// the LLM's analysis and merge suggestion to stdout.
func cmdMerge(args []string) error {
	fs := flag.NewFlagSet("merge", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: cerberus merge [flags]

Send all solution diffs to an LLM and stream its merge suggestion.

Flags:
`)
		fs.PrintDefaults()
	}
	model := fs.String("model", "", "model to use for the merge (default: opencode's configured default)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get working directory: %w", err)
	}
	repoRoot, err := git.RepoRoot(cwd)
	if err != nil {
		return err
	}

	state, err := config.Load(repoRoot)
	if err != nil {
		return err
	}

	// Collect diffs from all solutions that have changes.
	type solutionDiff struct {
		sol  config.Solution
		diff string
	}
	var diffs []solutionDiff
	for _, sol := range state.Solutions {
		diff, err := git.Diff(sol.Worktree, state.BaseCommit)
		if err != nil {
			return fmt.Errorf("get diff for solve-%d: %w", sol.Index, err)
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

	// Build the merge prompt.
	var b strings.Builder
	fmt.Fprintf(&b, "I ran the following task against a codebase using %d different AI models in parallel, each in its own git worktree.\n\n", len(diffs))
	fmt.Fprintf(&b, "Original task:\n%s\n\n", state.Prompt)
	fmt.Fprintf(&b, "Below are the unified diffs produced by each solution. Please analyse them, identify the strengths and weaknesses of each approach, and produce a single best merged solution as a unified diff.\n\n")
	fmt.Fprintf(&b, "After the diff, briefly explain what you took from each solution and why.\n\n")
	fmt.Fprintf(&b, "---\n\n")

	for _, d := range diffs {
		fmt.Fprintf(&b, "## Solution %d (agent=%s model=%s)\n\n", d.sol.Index, d.sol.Agent, or(d.sol.Model, "default"))
		fmt.Fprintf(&b, "```diff\n%s\n```\n\n", d.diff)
	}

	mergePrompt := b.String()

	// Shell out to opencode run, streaming JSON events and printing text to stdout.
	ocArgs := []string{"opencode", "run", "--format", "json"}
	if *model != "" {
		ocArgs = append(ocArgs, "-m", *model)
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

	// Parse JSON events and print only the text content.
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
	return nil
}

// cmdClean removes all worktrees, branches, opencode sessions, and state.
func cmdClean(args []string) error {
	fs := flag.NewFlagSet("clean", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, "Usage: cerberus clean [-force]\n\nRemove all worktrees, branches, opencode sessions, and cerberus state.\n")
	}
	force := fs.Bool("force", false, "skip confirmation prompt")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get working directory: %w", err)
	}
	repoRoot, err := git.RepoRoot(cwd)
	if err != nil {
		return err
	}

	state, err := config.Load(repoRoot)
	if err != nil {
		return err
	}

	if !*force {
		fmt.Printf("this will remove %d worktree(s), their branches, and any opencode sessions.\n", len(state.Solutions))
		fmt.Print("continue? [y/N] ")
		var answer string
		fmt.Scanln(&answer)
		if strings.ToLower(strings.TrimSpace(answer)) != "y" {
			fmt.Println("aborted.")
			return nil
		}
	}

	var errs []string

	// Delete opencode sessions for each solution that recorded one.
	for _, sol := range state.Solutions {
		if sol.SessionID == "" {
			continue
		}
		cmd := exec.Command("opencode", "session", "delete", sol.SessionID)
		if out, err := cmd.CombinedOutput(); err != nil {
			errs = append(errs, fmt.Sprintf("delete session %s (solve-%d): %s", sol.SessionID, sol.Index, strings.TrimSpace(string(out))))
		} else {
			fmt.Printf("deleted opencode session %s (solve-%d)\n", sol.SessionID, sol.Index)
		}
	}

	for _, sol := range state.Solutions {
		if err := git.RemoveWorktree(repoRoot, sol.Index); err != nil {
			errs = append(errs, fmt.Sprintf("remove worktree %d: %s", sol.Index, err))
		} else {
			fmt.Printf("removed solve-%d\n", sol.Index)
		}
	}

	if err := config.Remove(repoRoot); err != nil {
		errs = append(errs, fmt.Sprintf("remove state: %s", err))
	} else {
		fmt.Println("removed .cerberus state")
	}

	if len(errs) > 0 {
		return fmt.Errorf("clean completed with errors:\n  %s", strings.Join(errs, "\n  "))
	}
	fmt.Println("done.")
	return nil
}

// processAlive returns true if a process with the given PID is still running.
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

func or(s, fallback string) string {
	if s != "" {
		return s
	}
	return fallback
}
