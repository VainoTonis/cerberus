package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const (
	StateDir   = ".cerberus"
	StateFile  = "state.json"
	ConfigFile = "config.json"
)

// Runner defines a single agent+model pair used for one solution slot.
// OcAgent is the opencode agent mode to use (e.g. "build", "plan") - passed
// as --agent to opencode run. If empty, opencode uses its default.
type Runner struct {
	Agent   string `json:"agent"`
	Model   string `json:"model"`
	OcAgent string `json:"oc_agent,omitempty"`
}

// UserConfig holds user-level defaults loaded from ~/.config/cerberus/config.json.
// Runners defines the ordered list of agent+model pairs to use, one per solution slot.
// If empty, cerberus falls back to the -agent and -model flags.
// Instructions is prepended to every prompt sent to agents.
type UserConfig struct {
	Runners      []Runner `json:"runners"`
	Instructions string   `json:"instructions"`
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
	Index            int            `json:"index"`
	Branch           string         `json:"branch"`
	Worktree         string         `json:"worktree"`
	Agent            string         `json:"agent"`
	Model            string         `json:"model"`
	OcAgent          string         `json:"oc_agent,omitempty"`
	Status           SolutionStatus `json:"status"`
	PID              int            `json:"pid,omitempty"`
	LogFile          string         `json:"log_file,omitempty"`
	ExitCode         int            `json:"exit_code,omitempty"`
	SessionID        string         `json:"session_id,omitempty"`
	CommitHash       string         `json:"commit_hash,omitempty"`
	StartedAt        time.Time      `json:"started_at,omitempty"`
	FinishedAt       time.Time      `json:"finished_at,omitempty"`
	InputTokens      int            `json:"input_tokens,omitempty"`
	OutputTokens     int            `json:"output_tokens,omitempty"`
	CacheReadTokens  int            `json:"cache_read_tokens,omitempty"`
	CacheWriteTokens int            `json:"cache_write_tokens,omitempty"`
	CostUSD          float64        `json:"cost_usd,omitempty"`
}

type State struct {
	Name        string         `json:"name"`
	BaseBranch  string         `json:"base_branch"`
	BaseCommit  string         `json:"base_commit"`
	Prompt      string         `json:"prompt"`
	CallerModel string         `json:"caller_model,omitempty"`
	Solutions   []Solution     `json:"solutions"`
	Selections  map[string]int `json:"selections"`
}

// sessionDir returns the directory for a named session's state.
func sessionDir(repoRoot, name string) string {
	return filepath.Join(repoRoot, StateDir, "sessions", name)
}

// StatePath returns the state file path for a named session.
func StatePath(repoRoot, name string) string {
	return filepath.Join(sessionDir(repoRoot, name), StateFile)
}

// LogPath returns the log file path for a solution within a named session.
func LogPath(repoRoot, sessionName string, index int) string {
	return filepath.Join(sessionDir(repoRoot, sessionName), "logs", fmt.Sprintf("solve-%d.log", index))
}

// MergeSuggestionPath returns the path where cmdMerge writes its suggestion for a session.
func MergeSuggestionPath(repoRoot, sessionName string) string {
	return filepath.Join(sessionDir(repoRoot, sessionName), "merge-suggestion.txt")
}

// ListSessions returns the names of all sessions that have a state file in the repo.
func ListSessions(repoRoot string) ([]string, error) {
	sessionsDir := filepath.Join(repoRoot, StateDir, "sessions")
	entries, err := os.ReadDir(sessionsDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read sessions dir: %w", err)
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		stateFile := filepath.Join(sessionsDir, e.Name(), StateFile)
		if _, err := os.Stat(stateFile); err == nil {
			names = append(names, e.Name())
		}
	}
	return names, nil
}

// Load reads the state for a named session.
func Load(repoRoot, name string) (*State, error) {
	path := StatePath(repoRoot, name)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("no cerberus session %q found in %s (run 'cerberus start -session %s' first)", name, repoRoot, name)
		}
		return nil, fmt.Errorf("read state: %w", err)
	}
	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("parse state: %w", err)
	}
	return &s, nil
}

// Save writes the state for a named session.
func Save(repoRoot string, s *State) error {
	dir := sessionDir(repoRoot, s.Name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create session dir: %w", err)
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}
	path := StatePath(repoRoot, s.Name)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write state: %w", err)
	}
	return nil
}

// Remove deletes the state directory for a named session.
func Remove(repoRoot, name string) error {
	dir := sessionDir(repoRoot, name)
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("remove session dir: %w", err)
	}
	return nil
}

const StatsFile = "stats.json"

// StatsRunner is a snapshot of a single solution's outcome recorded in the stats file.
type StatsRunner struct {
	Model            string  `json:"model"`
	OcAgent          string  `json:"oc_agent,omitempty"`
	Status           string  `json:"status"`
	DurationS        float64 `json:"duration_s,omitempty"`
	InputTokens      int     `json:"input_tokens,omitempty"`
	OutputTokens     int     `json:"output_tokens,omitempty"`
	CacheReadTokens  int     `json:"cache_read_tokens,omitempty"`
	CacheWriteTokens int     `json:"cache_write_tokens,omitempty"`
	CostUSD          float64 `json:"cost_usd,omitempty"`
}

// StatsRecord is one entry appended to the global stats file each time
// cerberus records a session outcome (on apply, or explicitly when no solution is chosen).
type StatsRecord struct {
	SessionDate   time.Time     `json:"session_date"`
	SessionName   string        `json:"session_name"`
	PromptSnippet string        `json:"prompt_snippet"`
	BaseBranch    string        `json:"base_branch"`
	WinnerIndex   int           `json:"winner_index"` // 0 means no winner was applied; -1 means cleaned without applying
	Cleaned       bool          `json:"cleaned,omitempty"`
	Runners       []StatsRunner `json:"runners"`
	TotalCostUSD  float64       `json:"total_cost_usd,omitempty"`
}

func statsPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("locate config dir: %w", err)
	}
	return filepath.Join(dir, "cerberus", StatsFile), nil
}

// AppendStats adds a StatsRecord to ~/.config/cerberus/stats.json.
// The file is a JSON array; if it does not exist it is created.
func AppendStats(rec StatsRecord) error {
	path, err := statsPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create stats dir: %w", err)
	}

	var records []StatsRecord
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read stats file: %w", err)
	}
	if len(data) > 0 {
		if err := json.Unmarshal(data, &records); err != nil {
			return fmt.Errorf("parse stats file: %w", err)
		}
	}

	records = append(records, rec)
	out, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal stats: %w", err)
	}
	if err := os.WriteFile(path, out, 0o644); err != nil {
		return fmt.Errorf("write stats file: %w", err)
	}
	return nil
}

// LoadStats reads all records from ~/.config/cerberus/stats.json.
// Returns an empty slice if the file does not exist.
func LoadStats() ([]StatsRecord, error) {
	path, err := statsPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read stats file: %w", err)
	}
	var records []StatsRecord
	if err := json.Unmarshal(data, &records); err != nil {
		return nil, fmt.Errorf("parse stats file: %w", err)
	}
	return records, nil
}
