package agent

// ClaudeCode builds commands for the claude CLI agent.
// Uses `claude -p` (print/non-interactive mode).
type ClaudeCode struct{}

func (ClaudeCode) Name() string { return "claude" }

func (ClaudeCode) Args(r RunArgs) ([]string, error) {
	args := []string{"claude", "-p"}
	if r.Model != "" {
		args = append(args, "--model", r.Model)
	}
	args = append(args, r.Prompt)
	return args, nil
}
