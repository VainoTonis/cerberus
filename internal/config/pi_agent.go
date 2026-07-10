package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// copyConfigFile copies a config file from host to agentDir.
// If configPath is empty (default), creates empty {} if the default path (~/.pi/agent/<filename>) is not found.
// If configPath is explicitly set (non-empty), fails with a clear error if the file is unreadable or missing.
func copyConfigFile(agentDir, filename, configPath string, homeDir string) error {
	destPath := filepath.Join(agentDir, filename)

	// Determine source path
	sourcePath := configPath
	isExplicit := sourcePath != ""

	if sourcePath == "" {
		sourcePath = filepath.Join(homeDir, ".pi", "agent", filename)
	}

	// Expand ~ in path
	if strings.HasPrefix(sourcePath, "~") {
		sourcePath = filepath.Join(homeDir, strings.TrimPrefix(sourcePath, "~/"))
	}

	// Try to copy from source if it exists
	data, err := os.ReadFile(sourcePath)
	if err == nil {
		return os.WriteFile(destPath, data, 0o644)
	}

	// If explicitly configured but not found, fail with clear error
	if isExplicit {
		if os.IsNotExist(err) {
			return fmt.Errorf("%s path does not exist: %s", filename, sourcePath)
		}
		return fmt.Errorf("read %s from %s: %w", filename, sourcePath, err)
	}

	// Default path not found: silently create empty JSON
	return os.WriteFile(destPath, []byte("{}"), 0o644)
}

// linkOrCopyPath copies a resource path into agentDir (not symlink, to work in containers).
// Returns error if resource does not exist or cannot be copied.
func linkOrCopyPath(agentDir, name, resourcePath string) error {
	destPath := filepath.Join(agentDir, name)

	// Expand ~ in path
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	expandedPath := resourcePath
	if strings.HasPrefix(expandedPath, "~") {
		expandedPath = filepath.Join(homeDir, strings.TrimPrefix(expandedPath, "~/"))
	}

	// Check if source exists
	stat, err := os.Stat(expandedPath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("%s does not exist: %s", name, expandedPath)
		}
		return fmt.Errorf("stat %s: %w", name, err)
	}

	// Copy (never symlink - symlinks break inside containers)
	if stat.IsDir() {
		return copyDir(expandedPath, destPath)
	}

	// Copy single file
	data, err := os.ReadFile(expandedPath)
	if err != nil {
		return fmt.Errorf("read %s: %w", name, err)
	}
	return os.WriteFile(destPath, data, 0o644)
}

// copyDir recursively copies a directory from src to dst, preserving file permissions.
func copyDir(src, dst string) error {
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}

	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())

		if entry.IsDir() {
			if err := copyDir(srcPath, dstPath); err != nil {
				return err
			}
		} else {
			// Preserve file permissions
			info, err := entry.Info()
			if err != nil {
				return err
			}
			data, err := os.ReadFile(srcPath)
			if err != nil {
				return err
			}
			if err := os.WriteFile(dstPath, data, info.Mode()); err != nil {
				return err
			}
		}
	}

	return nil
}

// generateAgentsMD creates an AGENTS.md file from the configured AGENTS settings.
// If no AGENTS config is provided, generates a safe Cerberus default.
func generateAgentsMD(agentDir string, agentsConfig PIAgentConfig) error {
	var content string

	if agentsConfig.AGENTS.Default != "" {
		content = agentsConfig.AGENTS.Default
	} else if agentsConfig.AGENTS.Source != "" {
		// Read AGENTS.Source as a filesystem path
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("get home dir: %w", err)
		}

		sourcePath := agentsConfig.AGENTS.Source
		if strings.HasPrefix(sourcePath, "~") {
			sourcePath = filepath.Join(homeDir, strings.TrimPrefix(sourcePath, "~/"))
		}

		data, err := os.ReadFile(sourcePath)
		if err != nil {
			return fmt.Errorf("read AGENTS source file %s: %w", agentsConfig.AGENTS.Source, err)
		}
		content = string(data)
	} else {
		// No config provided: generate generic container-workspace default
		content = `# Container Agent Instructions

You are working inside an isolated container workspace.

## Core Rules

- Treat this environment as self-contained. Do not assume host-only tools or orchestration commands exist.
- Edit files directly in the current workspace using available file tools.
- Keep changes small and focused on the requested task.
- Do not perform unrequested refactors.
- Run relevant tests or explain exactly why they were not run.
- If a needed command or file is missing, report it clearly instead of inventing a workaround.

## Communication

Be concise. Report files changed, tests run, and any risks.
`
	}

	agentsMDPath := filepath.Join(agentDir, "AGENTS.md")
	return os.WriteFile(agentsMDPath, []byte(content), 0o644)
}

// GeneratePIAgentDirWithConfig creates and initializes the PI agent config directory for a session.
// It populates config from UserConfig.PIAgent, copying/defaulting files from host ~/.pi/agent when present.
// Returns the path to the generated directory.
func GeneratePIAgentDirWithConfig(repoRoot, sessionName string, cfg *UserConfig) (string, error) {
	agentDir, err := PiAgentDir(repoRoot, sessionName)
	if err != nil {
		return "", err
	}

	// Create the agent directory
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		return "", fmt.Errorf("create pi agent dir: %w", err)
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("get home dir: %w", err)
	}

	// Copy or create auth.json, models.json, settings.json with real content if available
	if err := copyConfigFile(agentDir, "auth.json", cfg.PIAgent.AuthPath, homeDir); err != nil {
		return "", fmt.Errorf("copy auth.json: %w", err)
	}
	if err := copyConfigFile(agentDir, "models.json", cfg.PIAgent.ModelsPath, homeDir); err != nil {
		return "", fmt.Errorf("copy models.json: %w", err)
	}
	if err := copyConfigFile(agentDir, "settings.json", cfg.PIAgent.SettingsPath, homeDir); err != nil {
		return "", fmt.Errorf("copy settings.json: %w", err)
	}

	// Generate AGENTS.md (always, with fallback to safe default if no config)
	if err := generateAgentsMD(agentDir, cfg.PIAgent); err != nil {
		return "", fmt.Errorf("generate agents.md: %w", err)
	}

	// Copy/create extension/skill/prompt/theme paths (required to work in containers)
	if cfg.PIAgent.ExtensionsPath != "" {
		if err := linkOrCopyPath(agentDir, "extensions", cfg.PIAgent.ExtensionsPath); err != nil {
			return "", fmt.Errorf("copy extensions: %w", err)
		}
	}
	if cfg.PIAgent.SkillsPath != "" {
		if err := linkOrCopyPath(agentDir, "skills", cfg.PIAgent.SkillsPath); err != nil {
			return "", fmt.Errorf("copy skills: %w", err)
		}
	}
	if cfg.PIAgent.PromptsPath != "" {
		if err := linkOrCopyPath(agentDir, "prompts", cfg.PIAgent.PromptsPath); err != nil {
			return "", fmt.Errorf("copy prompts: %w", err)
		}
	}
	if cfg.PIAgent.ThemesPath != "" {
		if err := linkOrCopyPath(agentDir, "themes", cfg.PIAgent.ThemesPath); err != nil {
			return "", fmt.Errorf("copy themes: %w", err)
		}
	}

	return agentDir, nil
}
