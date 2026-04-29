package gitops

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

var (
	// ErrGitFetch indicates `git fetch origin` failed in a task workspace.
	ErrGitFetch = errors.New("git fetch failed")
	// ErrGitCheckout indicates `git checkout -B` failed in a task workspace.
	ErrGitCheckout = errors.New("git checkout failed")
)

// TaskBranchRunner is the minimal git surface needed by D03 task-execution
// (Step 4 in R06). It is intentionally narrower than Runner so callers cannot
// accidentally reach for clone during task execution.
type TaskBranchRunner interface {
	// Fetch runs `git fetch origin` in repoDir. Wraps the error with ErrGitFetch.
	Fetch(ctx context.Context, repoDir string) error
	// CheckoutBranch runs `git checkout -B <branch> <base>` in repoDir.
	// `-B` force-resets the branch to base if it already exists (R06 Step 4).
	CheckoutBranch(ctx context.Context, repoDir, branch, base string) error
}

type defaultTaskBranchRunner struct{}

// NewTaskBranchRunner returns a TaskBranchRunner that shells out to git CLI.
func NewTaskBranchRunner() TaskBranchRunner {
	return &defaultTaskBranchRunner{}
}

func (r *defaultTaskBranchRunner) Fetch(ctx context.Context, repoDir string) error {
	cmd := exec.CommandContext(ctx, "git", "fetch", "origin")
	cmd.Dir = repoDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s: %v\n%s", ErrGitFetch, repoDir, err, truncate(output, 1024))
	}
	return nil
}

func (r *defaultTaskBranchRunner) CheckoutBranch(ctx context.Context, repoDir, branch, base string) error {
	cmd := exec.CommandContext(ctx, "git", "checkout", "-B", branch, base)
	cmd.Dir = repoDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s -> %s on %s: %v\n%s",
			ErrGitCheckout, base, branch, repoDir, err, truncate(output, 1024))
	}
	return nil
}

func truncate(b []byte, n int) string {
	s := strings.TrimSpace(string(b))
	if len(s) > n {
		return s[:n] + "..."
	}
	return s
}

// Runner provides minimal git operations needed by D02 at startup.
type Runner interface {
	// Clone clones a git repository into the given directory.
	Clone(ctx context.Context, url, dir string) error

	// Fetch runs "git fetch origin" inside the given directory.
	Fetch(ctx context.Context, dir string) error
}

type defaultRunner struct{}

// NewRunner creates a Runner that shells out to git CLI.
func NewRunner() Runner {
	return &defaultRunner{}
}

func (r *defaultRunner) Clone(ctx context.Context, url, dir string) error {
	cmd := exec.CommandContext(ctx, "git", "clone", url, dir)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to git clone %s: %w\n%s", url, err, strings.TrimSpace(string(output)))
	}
	return nil
}

func (r *defaultRunner) Fetch(ctx context.Context, dir string) error {
	cmd := exec.CommandContext(ctx, "git", "fetch", "origin")
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to git fetch in %s: %w\n%s", dir, err, strings.TrimSpace(string(output)))
	}
	return nil
}

// IsGitRepo checks whether the given directory is inside a git work tree.
func IsGitRepo(dir string) bool {
	cleanDir := filepath.Clean(dir)
	info, err := os.Stat(filepath.Join(cleanDir, ".git"))
	if err != nil {
		return false
	}
	return info.IsDir()
}
