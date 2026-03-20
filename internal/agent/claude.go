package agent

// ClaudeCode builds commands for the claude CLI agent.
// Uses `claude -p` (print/non-interactive mode).
type ClaudeCode struct{}

func (ClaudeCode) Name() string { return "claude" }

func (ClaudeCode) Args(prompt, model string) ([]string, error) {
	args := []string{"claude", "-p"}
	if model != "" {
		args = append(args, "--model", model)
	}
	args = append(args, prompt)
	return args, nil
}
