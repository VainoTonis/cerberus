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
		"Write a concise git commit message (subject line only, maximum 72 characters, no quotes, no punctuation at end) that describes the following changes:\n\n%s",
		diff,
	)

	args := []string{"opencode", "run", "--format", "json", prompt}
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = worktreePath

	pr, pw := io.Pipe()
	cmd.Stdout = pw
	cmd.Stderr = pw

	if err := cmd.Start(); err != nil {
		return "cerberus: agent solution"
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
		return "cerberus: agent solution"
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
