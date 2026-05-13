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

const (
	DefaultMaxTurns        = 30
	DefaultMaxOutputTokens = 10000
)

// UserConfig holds user-level defaults loaded from ~/.config/cerberus/config.json.
type UserConfig struct {
	Instructions    string            `json:"instructions"`
	DefaultModel    string            `json:"default_model,omitempty"`
	DefaultImage    string            `json:"default_image,omitempty"`
	AWSProfile      string            `json:"aws_profile,omitempty"`
	AWSRegion       string            `json:"aws_region,omitempty"`
	MaxTurns        int               `json:"max_turns,omitempty"`
	MaxOutputTokens int               `json:"max_output_tokens,omitempty"`
	ExtraEnv        map[string]string `json:"extra_env,omitempty"`
}

// ProfileFile holds provider-specific overrides that replace fields in UserConfig.
// It is loaded from a file passed via --profile-file at runtime.
type ProfileFile struct {
	DefaultModel string            `json:"default_model,omitempty"`
	DefaultImage string            `json:"default_image,omitempty"`
	AWSProfile   string            `json:"aws_profile,omitempty"`
	AWSRegion    string            `json:"aws_region,omitempty"`
	ExtraEnv     map[string]string `json:"extra_env,omitempty"`
}

// LoadProfileFile reads a ProfileFile from the given path.
func LoadProfileFile(path string) (ProfileFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return ProfileFile{}, fmt.Errorf("read profile file %s: %w", path, err)
	}
	var p ProfileFile
	if err := json.Unmarshal(data, &p); err != nil {
		return ProfileFile{}, fmt.Errorf("parse profile file %s: %w", path, err)
	}
	return p, nil
}

// ApplyProfile overwrites UserConfig fields with non-empty values from the ProfileFile.
// ExtraEnv is always replaced wholesale when the profile sets it (even if empty map).
func ApplyProfile(cfg *UserConfig, p ProfileFile) {
	if p.DefaultModel != "" {
		cfg.DefaultModel = p.DefaultModel
	}
	if p.DefaultImage != "" {
		cfg.DefaultImage = p.DefaultImage
	}
	if p.AWSProfile != "" {
		cfg.AWSProfile = p.AWSProfile
	}
	if p.AWSRegion != "" {
		cfg.AWSRegion = p.AWSRegion
	}
	cfg.ExtraEnv = p.ExtraEnv
}

// EffectiveMaxTurns returns MaxTurns if set, otherwise the default.
func (c UserConfig) EffectiveMaxTurns() int {
	if c.MaxTurns > 0 {
		return c.MaxTurns
	}
	return DefaultMaxTurns
}

// EffectiveMaxOutputTokens returns MaxOutputTokens if set, otherwise the default.
func (c UserConfig) EffectiveMaxOutputTokens() int {
	if c.MaxOutputTokens > 0 {
		return c.MaxOutputTokens
	}
	return DefaultMaxOutputTokens
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

type RunStatus string

const (
	StatusPending RunStatus = "pending"
	StatusRunning RunStatus = "running"
	StatusWaiting RunStatus = "waiting"
	StatusDone    RunStatus = "done"
	StatusFailed  RunStatus = "failed"
)

type Run struct {
	Branch           string    `json:"branch"`
	Worktree         string    `json:"worktree"`
	Agent            string    `json:"agent"`
	Model            string    `json:"model"`
	Image            string    `json:"image"`
	ProfileFile      string    `json:"profile_file,omitempty"`
	ContainerID      string    `json:"container_id,omitempty"`
	Status           RunStatus `json:"status"`
	Interactive      bool      `json:"interactive,omitempty"`
	PID              int       `json:"pid,omitempty"`
	LogFile          string    `json:"log_file,omitempty"`
	ExitCode         int       `json:"exit_code,omitempty"`
	SessionID        string    `json:"session_id,omitempty"`
	CommitHash       string    `json:"commit_hash,omitempty"`
	StartedAt        time.Time `json:"started_at,omitempty"`
	FinishedAt       time.Time `json:"finished_at,omitempty"`
	InputTokens      int       `json:"input_tokens,omitempty"`
	OutputTokens     int       `json:"output_tokens,omitempty"`
	CacheReadTokens  int       `json:"cache_read_tokens,omitempty"`
	CacheWriteTokens int       `json:"cache_write_tokens,omitempty"`
	CostUSD          float64   `json:"cost_usd,omitempty"`
}

type State struct {
	Name       string `json:"name"`
	BaseBranch string `json:"base_branch"`
	BaseCommit string `json:"base_commit"`
	Prompt     string `json:"prompt"`
	Run        Run    `json:"run"`
}

// sessionDir returns the directory for a named session's state.
func sessionDir(repoRoot, name string) string {
	return filepath.Join(repoRoot, StateDir, "sessions", name)
}

// StatePath returns the state file path for a named session.
func StatePath(repoRoot, name string) string {
	return filepath.Join(sessionDir(repoRoot, name), StateFile)
}

// LogPath returns the log file path for a session (single run, no index).
func LogPath(repoRoot, sessionName string) string {
	return filepath.Join(sessionDir(repoRoot, sessionName), "logs", "solve.log")
}

// PiSessionDir returns the host path where pi session state is persisted for a session.
// This directory is mounted into the container so conversation history survives container restarts.
func PiSessionDir(repoRoot, sessionName string) string {
	return filepath.Join(sessionDir(repoRoot, sessionName), "pi-sessions")
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

// StatsRecord is one entry appended to the global stats file each time
// cerberus records a session outcome.
type StatsRecord struct {
	SessionDate      time.Time `json:"session_date"`
	SessionName      string    `json:"session_name"`
	PromptSnippet    string    `json:"prompt_snippet"`
	BaseBranch       string    `json:"base_branch"`
	Model            string    `json:"model"`
	Image            string    `json:"image"`
	Status           string    `json:"status"`
	DurationS        float64   `json:"duration_s,omitempty"`
	InputTokens      int       `json:"input_tokens,omitempty"`
	OutputTokens     int       `json:"output_tokens,omitempty"`
	CacheReadTokens  int       `json:"cache_read_tokens,omitempty"`
	CacheWriteTokens int       `json:"cache_write_tokens,omitempty"`
	CostUSD          float64   `json:"cost_usd,omitempty"`
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
