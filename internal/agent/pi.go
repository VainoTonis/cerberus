package agent

// Pi builds commands for the pi coding agent (github.com/earendil-works/pi).
// It runs pi in JSON event stream mode for structured output.
type Pi struct{}

func (Pi) Name() string { return "pi" }

func (Pi) Args(r RunArgs) ([]string, error) {
	args := []string{"pi", "--mode", "json"}
	if r.Interactive {
		args = append(args, "--session-dir", "/tmp/pi-sessions")
		if r.ContinueSession {
			args = append(args, "--continue")
		}
	} else {
		args = append(args, "--no-session")
	}
	if r.Model != "" {
		args = append(args, "--model", r.Model)
	}
	args = append(args, r.Prompt)
	return args, nil
}
