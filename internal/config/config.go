package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const (
	StateDir   = ".cerberus"
	StateFile  = "state.json"
	ConfigFile = "config.json"
)

// Runner defines a single agent+model pair used for one solution slot.
type Runner struct {
	Agent string `json:"agent"`
	Model string `json:"model"`
}

// UserConfig holds user-level defaults loaded from ~/.config/cerberus/config.json.
// Runners defines the ordered list of agent+model pairs to use, one per solution slot.
// If empty, cerberus falls back to the -agent and -model flags.
type UserConfig struct {
	Runners []Runner `json:"runners"`
}

// LoadUserConfig reads ~/.config/cerberus/config.json.
// If the file does not exist an empty UserConfig is returned without error.
func LoadUserConfig() (UserConfig, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return UserConfig{}, fmt.Errorf("locate config dir: %w", err)
	}
	path := filepath.Join(dir, "cerberus", ConfigFile)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return UserConfig{}, nil
		}
		return UserConfig{}, fmt.Errorf("read user config: %w", err)
	}
	var c UserConfig
	if err := json.Unmarshal(data, &c); err != nil {
		return UserConfig{}, fmt.Errorf("parse user config %s: %w", path, err)
	}
	return c, nil
}

type SolutionStatus string

const (
	StatusPending SolutionStatus = "pending"
	StatusRunning SolutionStatus = "running"
	StatusDone    SolutionStatus = "done"
	StatusFailed  SolutionStatus = "failed"
)

type Solution struct {
	Index     int            `json:"index"`
	Branch    string         `json:"branch"`
	Worktree  string         `json:"worktree"`
	Agent     string         `json:"agent"`
	Model     string         `json:"model"`
	Status    SolutionStatus `json:"status"`
	PID       int            `json:"pid,omitempty"`
	LogFile   string         `json:"log_file,omitempty"`
	ExitCode  int            `json:"exit_code,omitempty"`
	SessionID string         `json:"session_id,omitempty"`
}

type State struct {
	BaseBranch string         `json:"base_branch"`
	BaseCommit string         `json:"base_commit"`
	Prompt     string         `json:"prompt"`
	Solutions  []Solution     `json:"solutions"`
	Selections map[string]int `json:"selections"`
}

func StatePath(repoRoot string) string {
	return filepath.Join(repoRoot, StateDir, StateFile)
}

func LogPath(repoRoot string, index int) string {
	return filepath.Join(repoRoot, StateDir, "logs", fmt.Sprintf("solve-%d.log", index))
}

func Load(repoRoot string) (*State, error) {
	path := StatePath(repoRoot)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("no cerberus session found in %s (run 'cerberus start' first)", repoRoot)
		}
		return nil, fmt.Errorf("read state: %w", err)
	}
	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("parse state: %w", err)
	}
	return &s, nil
}

func Save(repoRoot string, s *State) error {
	dir := filepath.Join(repoRoot, StateDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create state dir: %w", err)
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}
	path := StatePath(repoRoot)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write state: %w", err)
	}
	return nil
}

func Remove(repoRoot string) error {
	dir := filepath.Join(repoRoot, StateDir)
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("remove state dir: %w", err)
	}
	return nil
}
