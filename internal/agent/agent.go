package agent

import (
	"fmt"
	"strings"
)

// RunArgs holds the parameters passed to an agent when launching it.
type RunArgs struct {
	Prompt string
	Model  string
	// OcAgent is the opencode agent mode (e.g. "build", "plan").
	// Only used by the OpenCode agent; ignored by others.
	OcAgent string
}

// Agent represents a coding agent that can be launched with a prompt and model.
type Agent interface {
	// Name returns the agent identifier (e.g. "opencode", "claude").
	Name() string
	// Args returns the argv slice to exec for this agent.
	Args(r RunArgs) ([]string, error)
}

// registry maps agent names to their Agent implementations.
var registry = map[string]Agent{
	"opencode": OpenCode{},
	"claude":   ClaudeCode{},
}

// Get returns the Agent for the given name, or an error if unknown.
func Get(name string) (Agent, error) {
	a, ok := registry[name]
	if !ok {
		names := make([]string, 0, len(registry))
		for k := range registry {
			names = append(names, k)
		}
		return nil, fmt.Errorf("unknown agent %q (available: %s)", name, strings.Join(names, ", "))
	}
	return a, nil
}

// Available returns the names of all registered agents.
func Available() []string {
	names := make([]string, 0, len(registry))
	for k := range registry {
		names = append(names, k)
	}
	return names
}
