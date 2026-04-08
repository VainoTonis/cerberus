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

// CurrentCommit returns the full SHA of HEAD in the given directory.
func CurrentCommit(dir string) (string, error) {
	out, err := run(dir, "git", "rev-parse", "HEAD")
	if err != nil {
		return "", fmt.Errorf("get current commit: %w", err)
	}
	return strings.TrimSpace(out), nil
}

// WorktreePath returns the expected path for a solution's worktree within a session.
func WorktreePath(repoRoot, sessionName string, index int) string {
	return filepath.Join(repoRoot, ".cerberus", "sessions", sessionName, "worktrees", fmt.Sprintf("solve-%d", index))
}

// BranchName returns the branch name for a given session and solution index.
func BranchName(sessionName string, index int) string {
	return fmt.Sprintf("cerberus/%s/solve-%d", sessionName, index)
}

// CreateWorktree creates a new worktree and branch for the given session and
// solution index, checked out at the given commit.
func CreateWorktree(repoRoot, sessionName string, index int, baseCommit string) (path string, branch string, err error) {
	path = WorktreePath(repoRoot, sessionName, index)
	branch = BranchName(sessionName, index)

	// Remove if it already exists (idempotent).
	_ = removeWorktree(repoRoot, path)

	_, err = run(repoRoot, "git", "worktree", "add", "-b", branch, path, baseCommit)
	if err != nil {
		return "", "", fmt.Errorf("create worktree %d: %w", index, err)
	}
	return path, branch, nil
}

// RemoveWorktree removes the worktree and deletes its branch.
func RemoveWorktree(repoRoot, sessionName string, index int) error {
	path := WorktreePath(repoRoot, sessionName, index)
	branch := BranchName(sessionName, index)

	if err := removeWorktree(repoRoot, path); err != nil {
		return err
	}

	// Delete the branch; ignore error if it doesn't exist.
	_, _ = run(repoRoot, "git", "branch", "-D", branch)
	return nil
}

// HasChanges returns true if the worktree has any staged or unstaged changes
// (including untracked files) relative to its index.
func HasChanges(worktreePath string) (bool, error) {
	out, err := run(worktreePath, "git", "status", "--porcelain")
	if err != nil {
		return false, fmt.Errorf("git status in %s: %w", worktreePath, err)
	}
	return strings.TrimSpace(out) != "", nil
}

// CommitAll stages all changes in the worktree and creates a commit with the
// given message. It is the caller's responsibility to ensure there are changes
// to commit (see HasChanges).
func CommitAll(worktreePath, message string) error {
	if _, err := run(worktreePath, "git", "add", "-A"); err != nil {
		return fmt.Errorf("git add in %s: %w", worktreePath, err)
	}
	if _, err := run(worktreePath, "git", "commit", "-m", message); err != nil {
		return fmt.Errorf("git commit in %s: %w", worktreePath, err)
	}
	return nil
}

// CommitsBetween returns the list of commit SHAs in the range (baseCommit, tipCommit],
// oldest first. Used to count how many commits will be cherry-picked.
func CommitsBetween(dir, baseCommit, tipCommit string) ([]string, error) {
	out, err := run(dir, "git", "log", "--format=%H", baseCommit+".."+tipCommit)
	if err != nil {
		return nil, fmt.Errorf("log %s..%s: %w", baseCommit[:8], tipCommit[:8], err)
	}
	out = strings.TrimSpace(out)
	if out == "" {
		return nil, nil
	}
	hashes := strings.Split(out, "\n")
	// git log returns newest-first; reverse to oldest-first.
	for i, j := 0, len(hashes)-1; i < j; i, j = i+1, j-1 {
		hashes[i], hashes[j] = hashes[j], hashes[i]
	}
	return hashes, nil
}

// CherryPick applies a single commit onto the current branch in repoRoot.
// On conflict it returns an error; the repo is left in cherry-pick conflict
// state for the caller to resolve or abort.
func CherryPick(repoRoot, commitHash string) error {
	if _, err := run(repoRoot, "git", "cherry-pick", commitHash); err != nil {
		return fmt.Errorf("cherry-pick %s: %w", commitHash[:8], err)
	}
	return nil
}

// CherryPickRange applies all commits in the range (baseCommit, tipCommit] onto
// the current branch in repoRoot. On conflict the repo is left in cherry-pick
// conflict state.
func CherryPickRange(repoRoot, baseCommit, tipCommit string) error {
	if _, err := run(repoRoot, "git", "cherry-pick", baseCommit+".."+tipCommit); err != nil {
		return fmt.Errorf("cherry-pick %s..%s: %w", baseCommit[:8], tipCommit[:8], err)
	}
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

// CommittedDiff returns the unified diff of all commits in the worktree on top
// of baseCommit (i.e. committed changes only, not the working tree).
func CommittedDiff(worktreePath, baseCommit string) (string, error) {
	out, err := run(worktreePath, "git", "diff", baseCommit+"..HEAD")
	if err != nil {
		return "", fmt.Errorf("committed diff worktree %s: %w", worktreePath, err)
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

// CommittedChangedFiles returns the list of files changed by commits in the
// worktree on top of baseCommit (committed changes only, not the working tree).
func CommittedChangedFiles(worktreePath, baseCommit string) ([]string, error) {
	out, err := run(worktreePath, "git", "diff", "--name-only", baseCommit+"..HEAD")
	if err != nil {
		return nil, fmt.Errorf("committed changed files in %s: %w", worktreePath, err)
	}
	out = strings.TrimSpace(out)
	if out == "" {
		return nil, nil
	}
	return strings.Split(out, "\n"), nil
}

// CommitAndPush stages all changes and creates a commit, then returns the
// resulting HEAD commit SHA.
func CommitAndGetHash(worktreePath, message string) (string, error) {
	if err := CommitAll(worktreePath, message); err != nil {
		return "", err
	}
	return CurrentCommit(worktreePath)
}

func removeWorktree(repoRoot, path string) error {
	_, err := run(repoRoot, "git", "worktree", "remove", "--force", path)
	if err != nil {
		return nil
	}
	return nil
}

// WriteFile writes content to a path relative to destRoot, creating parent
// directories as needed. Used by merge-apply to write LLM-produced file contents.
func WriteFile(destRoot, relPath, content string) error {
	destPath := filepath.Join(destRoot, relPath)
	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return fmt.Errorf("create parent dirs for %s: %w", relPath, err)
	}
	if err := os.WriteFile(destPath, []byte(content), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", relPath, err)
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
