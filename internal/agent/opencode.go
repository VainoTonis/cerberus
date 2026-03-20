package agent

// OpenCode builds commands for the opencode agent using `opencode run`.
type OpenCode struct{}

func (OpenCode) Name() string { return "opencode" }

func (OpenCode) Args(r RunArgs) ([]string, error) {
	args := []string{"opencode", "run", "--format", "json"}
	if r.Model != "" {
		args = append(args, "-m", r.Model)
	}
	if r.OcAgent != "" {
		args = append(args, "--agent", r.OcAgent)
	}
	args = append(args, r.Prompt)
	return args, nil
}
