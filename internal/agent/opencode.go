package agent

// OpenCode builds commands for the opencode agent using `opencode run`.
type OpenCode struct{}

func (OpenCode) Name() string { return "opencode" }

func (OpenCode) Args(prompt, model string) ([]string, error) {
	args := []string{"opencode", "run", "--format", "json"}
	if model != "" {
		args = append(args, "-m", model)
	}
	args = append(args, prompt)
	return args, nil
}
