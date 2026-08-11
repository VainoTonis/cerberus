package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateAgentsMDDefaultIncludesToolUseGuidance(t *testing.T) {
	agentDir := t.TempDir()

	err := generateAgentsMD(agentDir, PIAgentConfig{})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(agentDir, "AGENTS.md"))
	if err != nil {
		t.Fatalf("expected AGENTS.md to exist, got: %v", err)
	}
	content := string(data)

	if !strings.Contains(content, "## Tool Use") {
		t.Fatalf("expected default AGENTS.md to contain Tool Use section, got: %s", content)
	}
	for _, snippet := range []string{
		"Batch independent tool calls into a single turn",
		"Never scan from the filesystem root",
		"Always bound command output",
		"Do not re-read a file that has not changed",
	} {
		if !strings.Contains(content, snippet) {
			t.Fatalf("expected default AGENTS.md to contain %q, got: %s", snippet, content)
		}
	}

	// Existing rules and communication section still present.
	if !strings.Contains(content, "## Core Rules") || !strings.Contains(content, "## Communication") {
		t.Fatalf("expected default AGENTS.md to retain Core Rules and Communication sections, got: %s", content)
	}
}

func TestGenerateAgentsMDCustomDefaultUnaffected(t *testing.T) {
	agentDir := t.TempDir()
	custom := "# Custom AGENTS\n\nCustom instructions.\n"

	cfg := PIAgentConfig{}
	cfg.AGENTS.Default = custom
	err := generateAgentsMD(agentDir, cfg)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(agentDir, "AGENTS.md"))
	if err != nil {
		t.Fatalf("expected AGENTS.md to exist, got: %v", err)
	}
	if string(data) != custom {
		t.Fatalf("expected custom AGENTS.md content to be unaffected, got: %s", string(data))
	}
}

func TestGenerateAgentsMDCustomSourceUnaffected(t *testing.T) {
	agentDir := t.TempDir()
	sourceDir := t.TempDir()
	sourcePath := filepath.Join(sourceDir, "AGENTS.md")
	custom := "# Source AGENTS\n\nSource instructions.\n"
	if err := os.WriteFile(sourcePath, []byte(custom), 0o644); err != nil {
		t.Fatalf("failed to write source file: %v", err)
	}

	cfg := PIAgentConfig{}
	cfg.AGENTS.Source = sourcePath
	err := generateAgentsMD(agentDir, cfg)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(agentDir, "AGENTS.md"))
	if err != nil {
		t.Fatalf("expected AGENTS.md to exist, got: %v", err)
	}
	if string(data) != custom {
		t.Fatalf("expected source AGENTS.md content to be unaffected, got: %s", string(data))
	}
}

func TestCopyConfigFileDefaultPath(t *testing.T) {
	// Test: default path (empty configPath) should create empty JSON if not found
	tmpDir := t.TempDir()
	homeDir := t.TempDir()

	agentDir := filepath.Join(tmpDir, "agent")
	os.MkdirAll(agentDir, 0o755)

	// Call with empty configPath (default)
	err := copyConfigFile(agentDir, "auth.json", "", homeDir)
	if err != nil {
		t.Fatalf("expected no error for default path, got: %v", err)
	}

	// Verify empty JSON was created
	authPath := filepath.Join(agentDir, "auth.json")
	data, err := os.ReadFile(authPath)
	if err != nil {
		t.Fatalf("expected auth.json to exist, got: %v", err)
	}
	if string(data) != "{}" {
		t.Fatalf("expected empty JSON, got: %s", string(data))
	}
}

func TestCopyConfigFileExplicitPathNotFound(t *testing.T) {
	// Test: explicit path (non-empty configPath) should fail with clear error if not found
	tmpDir := t.TempDir()
	homeDir := t.TempDir()

	agentDir := filepath.Join(tmpDir, "agent")
	os.MkdirAll(agentDir, 0o755)

	// Call with non-existent explicit path
	nonExistentPath := filepath.Join(tmpDir, "missing.json")
	err := copyConfigFile(agentDir, "models.json", nonExistentPath, homeDir)
	if err == nil {
		t.Fatal("expected error for missing explicit path, got nil")
	}

	// Verify error message is clear
	if !os.IsNotExist(os.NewFile(0, "").Close()) { // Just checking error handling
		// Error message should mention "path does not exist"
		errMsg := err.Error()
		if errMsg != "models.json path does not exist: "+nonExistentPath {
			t.Fatalf("unexpected error message: %s", errMsg)
		}
	}
}

func TestCopyConfigFileExplicitPathFound(t *testing.T) {
	// Test: explicit path that exists should be copied
	tmpDir := t.TempDir()
	homeDir := t.TempDir()

	agentDir := filepath.Join(tmpDir, "agent")
	os.MkdirAll(agentDir, 0o755)

	// Create a source config file
	sourceDir := filepath.Join(tmpDir, "configs")
	os.MkdirAll(sourceDir, 0o755)
	sourcePath := filepath.Join(sourceDir, "models.json")
	testContent := `{"test": "data"}`
	err := os.WriteFile(sourcePath, []byte(testContent), 0o644)
	if err != nil {
		t.Fatalf("failed to create test source file: %v", err)
	}

	// Call with explicit path that exists
	err = copyConfigFile(agentDir, "models.json", sourcePath, homeDir)
	if err != nil {
		t.Fatalf("expected no error for existing explicit path, got: %v", err)
	}

	// Verify content was copied
	destPath := filepath.Join(agentDir, "models.json")
	data, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatalf("expected models.json to exist, got: %v", err)
	}
	if string(data) != testContent {
		t.Fatalf("expected content %q, got: %q", testContent, string(data))
	}
}

func TestCopyConfigFileDefaultPathExpansion(t *testing.T) {
	// Test: default path with tilde expansion should work
	tmpDir := t.TempDir()
	homeDir := t.TempDir()

	agentDir := filepath.Join(tmpDir, "agent")
	os.MkdirAll(agentDir, 0o755)

	// Create source file at ~/.pi/agent/settings.json
	piAgentDir := filepath.Join(homeDir, ".pi", "agent")
	os.MkdirAll(piAgentDir, 0o755)
	settingsPath := filepath.Join(piAgentDir, "settings.json")
	testContent := `{"theme": "dark"}`
	err := os.WriteFile(settingsPath, []byte(testContent), 0o644)
	if err != nil {
		t.Fatalf("failed to create test settings file: %v", err)
	}

	// Call with empty configPath (will look in default ~/.pi/agent)
	err = copyConfigFile(agentDir, "settings.json", "", homeDir)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	// Verify content was copied from default location
	destPath := filepath.Join(agentDir, "settings.json")
	data, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatalf("expected settings.json to exist, got: %v", err)
	}
	if string(data) != testContent {
		t.Fatalf("expected content %q, got: %q", testContent, string(data))
	}
}
