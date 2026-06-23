package platform

import (
	"os/exec"
	"strings"
)

// GitMetadata contains commonly requested git values for a working tree.
type GitMetadata struct {
	Branch            string
	CommitHash        string
	CommitShortHash   string
	CommitAuthorName  string
	CommitAuthorEmail string
	UserName          string
	UserEmail         string
}

// GetGitBranch returns the current git branch name for the given directory.
// Returns empty string if not in a git repository or on error.
func GetGitBranch(cwd string) string {
	return getGitOutput(cwd, "rev-parse", "--abbrev-ref", "HEAD")
}

// GetGitToplevel returns the absolute path of the top-level working tree
// containing cwd. For a git worktree, this is the worktree root (not the
// primary repo). Returns empty string if cwd is not inside a git repository,
// git is not installed, or on any error.
func GetGitToplevel(cwd string) string {
	return getGitOutput(cwd, "rev-parse", "--show-toplevel")
}

// GetGitMetadata returns commonly used git metadata for the given directory.
// Empty fields indicate the directory is not a git repo or the value is unavailable.
func GetGitMetadata(cwd string) GitMetadata {
	if cwd == "" {
		return GitMetadata{}
	}

	metadata := GitMetadata{
		Branch:    GetGitBranch(cwd),
		UserName:  getGitOutput(cwd, "config", "--get", "user.name"),
		UserEmail: getGitOutput(cwd, "config", "--get", "user.email"),
	}

	logOutput := getGitOutput(cwd, "log", "-1", "--pretty=format:%H%n%h%n%an%n%ae")
	if logOutput == "" {
		return metadata
	}

	parts := strings.Split(logOutput, "\n")
	if len(parts) > 0 {
		metadata.CommitHash = strings.TrimSpace(parts[0])
	}
	if len(parts) > 1 {
		metadata.CommitShortHash = strings.TrimSpace(parts[1])
	}
	if len(parts) > 2 {
		metadata.CommitAuthorName = strings.TrimSpace(parts[2])
	}
	if len(parts) > 3 {
		metadata.CommitAuthorEmail = strings.TrimSpace(parts[3])
	}

	return metadata
}

func getGitOutput(cwd string, args ...string) string {
	if cwd == "" {
		return ""
	}

	cmd := exec.Command("git", append([]string{"-C", cwd}, args...)...)
	output, err := cmd.Output()
	if err != nil {
		return ""
	}

	value := strings.TrimSpace(string(output))

	// "HEAD" is returned when in detached HEAD state
	if value == "HEAD" {
		return ""
	}

	return value
}
