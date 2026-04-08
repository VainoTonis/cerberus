package agent

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

// AskForCommitMessage runs opencode in worktreePath to generate a commit
// message for the given diff. It returns a single subject line (≤72 chars).
// On any failure it returns a safe fallback message rather than an error,
// so callers can always proceed with a commit.
func AskForCommitMessage(worktreePath, diff string) string {
	prompt := fmt.Sprintf(
		"Write a git commit message for the following diff using the Conventional Commits format.\n"+
			"\n"+
			"Format: <type>(<scope>): <description>\n"+
			"\n"+
			"Rules:\n"+
			"- type: feat, fix, refactor, perf, test, docs, chore — pick the most accurate\n"+
			"- scope: a short noun describing what was changed (e.g. graph, auth, worktree, config) — infer it from the files or code changed\n"+
			"- description: imperative mood, lowercase, no period, max 72 chars total including type and scope\n"+
			"- output the subject line only, no body, no markdown, no quotes\n"+
			"\n"+
			"Diff:\n%s",
		diff,
	)

	args := []string{"opencode", "run", "--format", "json", prompt}
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = worktreePath

	pr, pw := io.Pipe()
	cmd.Stdout = pw
	cmd.Stderr = pw

	if err := cmd.Start(); err != nil {
		return "chore(cerberus): agent solution"
	}

	var collected strings.Builder
	done := make(chan struct{})
	go func() {
		defer close(done)
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
				collected.WriteString(event.Part.Text)
			}
		}
	}()

	_ = cmd.Wait()
	pw.Close()
	<-done

	msg := extractFirstLine(collected.String())
	if msg == "" {
		return "chore(cerberus): agent solution"
	}
	if len(msg) > 72 {
		msg = msg[:72]
	}
	return msg
}

func extractFirstLine(s string) string {
	s = strings.TrimSpace(s)
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return line
		}
	}
	return ""
}
