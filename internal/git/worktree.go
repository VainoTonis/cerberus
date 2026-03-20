package git

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// RepoRoot returns the absolute path of the git repo root for the given directory.
func RepoRoot(dir string) (string, error) {
	out, err := run(dir, "git", "rev-parse", "--show-toplevel")
	if err != nil {
		return "", fmt.Errorf("not inside a git repository: %w", err)
	}
	return strings.TrimSpace(out), nil
}

// CurrentBranch returns the name of the currently checked-out branch.
func CurrentBranch(repoRoot string) (string, error) {
	out, err := run(repoRoot, "git", "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "", fmt.Errorf("get current branch: %w", err)
	}
	return strings.TrimSpace(out), nil
}

// CurrentCommit returns the full SHA of HEAD.
func CurrentCommit(repoRoot string) (string, error) {
	out, err := run(repoRoot, "git", "rev-parse", "HEAD")
	if err != nil {
		return "", fmt.Errorf("get current commit: %w", err)
	}
	return strings.TrimSpace(out), nil
}

// WorktreePath returns the expected path for a solution's worktree.
func WorktreePath(repoRoot string, index int) string {
	return filepath.Join(repoRoot, ".cerberus", "worktrees", fmt.Sprintf("solve-%d", index))
}

// BranchName returns the branch name for a given solution index.
func BranchName(index int) string {
	return fmt.Sprintf("cerberus/solve-%d", index)
}

// CreateWorktree creates a new worktree and branch for the given solution index
// checked out at the given commit.
func CreateWorktree(repoRoot string, index int, baseCommit string) (path string, branch string, err error) {
	path = WorktreePath(repoRoot, index)
	branch = BranchName(index)

	// Remove if it already exists (idempotent).
	_ = removeWorktree(repoRoot, path)

	_, err = run(repoRoot, "git", "worktree", "add", "-b", branch, path, baseCommit)
	if err != nil {
		return "", "", fmt.Errorf("create worktree %d: %w", index, err)
	}
	return path, branch, nil
}

// RemoveWorktree removes the worktree and deletes its branch.
func RemoveWorktree(repoRoot string, index int) error {
	path := WorktreePath(repoRoot, index)
	branch := BranchName(index)

	if err := removeWorktree(repoRoot, path); err != nil {
		return err
	}

	// Delete the branch; ignore error if it doesn't exist.
	_, _ = run(repoRoot, "git", "branch", "-D", branch)
	return nil
}

// Diff returns the unified diff of all changes in a worktree relative to the
// base commit, including both committed and uncommitted (working tree) changes.
func Diff(worktreePath, baseCommit string) (string, error) {
	out, err := run(worktreePath, "git", "diff", baseCommit)
	if err != nil {
		return "", fmt.Errorf("diff worktree %s: %w", worktreePath, err)
	}
	return out, nil
}

// ChangedFiles returns the list of files changed in a worktree relative to the
// base commit, including both committed and uncommitted (working tree) changes.
func ChangedFiles(worktreePath, baseCommit string) ([]string, error) {
	out, err := run(worktreePath, "git", "diff", "--name-only", baseCommit)
	if err != nil {
		return nil, fmt.Errorf("changed files in %s: %w", worktreePath, err)
	}
	out = strings.TrimSpace(out)
	if out == "" {
		return nil, nil
	}
	return strings.Split(out, "\n"), nil
}

// CheckoutFile copies a single file from srcWorktree into destRepoRoot.
// It reads the file via `git show HEAD:<path>` from the source worktree and
// writes it to the equivalent path in the destination.
func CheckoutFile(srcWorktree, destRepoRoot, relPath string) error {
	content, err := run(srcWorktree, "git", "show", "HEAD:"+relPath)
	if err != nil {
		return fmt.Errorf("read %s from worktree: %w", relPath, err)
	}

	destPath := filepath.Join(destRepoRoot, relPath)
	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return fmt.Errorf("create parent dirs for %s: %w", relPath, err)
	}
	if err := os.WriteFile(destPath, []byte(content), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", relPath, err)
	}
	return nil
}

func removeWorktree(repoRoot, path string) error {
	_, err := run(repoRoot, "git", "worktree", "remove", "--force", path)
	if err != nil {
		return nil
	}
	return nil
}

func run(dir string, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("%s %v: %s", name, args, strings.TrimSpace(string(ee.Stderr)))
		}
		return "", err
	}
	return string(out), nil
}
