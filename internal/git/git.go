// Package git provides Git operations using the system git command.
package git

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// ChangeStatus represents the status of a file change.
type ChangeStatus string

const (
	StatusAdded     ChangeStatus = "A"
	StatusModified  ChangeStatus = "M"
	StatusDeleted   ChangeStatus = "D"
	StatusUntracked ChangeStatus = "?"
)

// FileChange represents a changed file in the repository.
type FileChange struct {
	Path   string
	Status ChangeStatus
}

// Clone clones a git repository to the specified destination.
func Clone(url, destPath string) error {
	cmd := exec.Command("git", "clone", url, destPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// Pull performs a git pull in the specified repository.
func Pull(repoPath string) error {
	cmd := exec.Command("git", "pull")
	cmd.Dir = repoPath
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// Push performs a git push in the specified repository.
func Push(repoPath string) error {
	cmd := exec.Command("git", "push")
	cmd.Dir = repoPath
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// HasUnpushedCommits checks if the repository has local commits that haven't been pushed.
func HasUnpushedCommits(repoPath string) (bool, error) {
	// Get the current branch
	branchCmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	branchCmd.Dir = repoPath
	branchOut, err := branchCmd.Output()
	if err != nil {
		return false, err
	}
	branch := strings.TrimSpace(string(branchOut))

	// Check if there are commits ahead of origin
	cmd := exec.Command("git", "rev-list", "--count", fmt.Sprintf("origin/%s..HEAD", branch))
	cmd.Dir = repoPath
	out, err := cmd.Output()
	if err != nil {
		// If origin/branch doesn't exist, assume no remote tracking
		return false, nil
	}

	count := strings.TrimSpace(string(out))
	return count != "0", nil
}

// Status returns the status of files in the repository.
func Status(repoPath string) ([]FileChange, error) {
	cmd := exec.Command("git", "status", "--porcelain")
	cmd.Dir = repoPath
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	var changes []FileChange
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		line := scanner.Text()
		if len(line) < 3 {
			continue
		}

		status := line[:2]
		path := strings.TrimSpace(line[3:])

		var changeStatus ChangeStatus
		switch {
		case status == "??":
			changeStatus = StatusUntracked
		case status[0] == 'A' || status[1] == 'A':
			changeStatus = StatusAdded
		case status[0] == 'D' || status[1] == 'D':
			changeStatus = StatusDeleted
		case status[0] == 'M' || status[1] == 'M':
			changeStatus = StatusModified
		default:
			changeStatus = StatusModified
		}

		changes = append(changes, FileChange{
			Path:   path,
			Status: changeStatus,
		})
	}

	return changes, scanner.Err()
}

// ListFiles lists all tracked files in the repository (excluding .git).
func ListFiles(repoPath string) ([]string, error) {
	cmd := exec.Command("git", "ls-files")
	cmd.Dir = repoPath
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	var files []string
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		files = append(files, scanner.Text())
	}

	return files, scanner.Err()
}

// Add stages a file for commit.
func Add(repoPath, filePath string) error {
	cmd := exec.Command("git", "add", filePath)
	cmd.Dir = repoPath
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// ExtractRepoName extracts the repository name from a git URL.
// For example: https://github.com/user/repo.git -> repo
func ExtractRepoName(url string) string {
	// Remove trailing .git if present
	url = strings.TrimSuffix(url, ".git")

	// Get the last path component
	name := filepath.Base(url)

	return name
}

// Commit creates a commit with all tracked changes (git commit -am).
func Commit(repoPath, message string) error {
	cmd := exec.Command("git", "commit", "-am", message)
	cmd.Dir = repoPath
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// CommitStaged creates a commit with staged changes only (git commit -m).
func CommitStaged(repoPath, message string) error {
	cmd := exec.Command("git", "commit", "-m", message)
	cmd.Dir = repoPath
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// HasUncommittedChanges checks if there are uncommitted changes in the repository.
func HasUncommittedChanges(repoPath string) (bool, error) {
	cmd := exec.Command("git", "status", "--porcelain")
	cmd.Dir = repoPath
	out, err := cmd.Output()
	if err != nil {
		return false, err
	}
	return len(strings.TrimSpace(string(out))) > 0, nil
}

// HasConflicts checks if the repository has merge conflicts.
func HasConflicts(repoPath string) (bool, error) {
	cmd := exec.Command("git", "diff", "--name-only", "--diff-filter=U")
	cmd.Dir = repoPath
	out, err := cmd.Output()
	if err != nil {
		return false, err
	}
	return len(strings.TrimSpace(string(out))) > 0, nil
}

// Fetch performs a git fetch in the specified repository.
func Fetch(repoPath string) error {
	cmd := exec.Command("git", "fetch")
	cmd.Dir = repoPath
	// Suppress output to avoid cluttering the console
	cmd.Stderr = nil
	cmd.Stdout = nil
	return cmd.Run()
}

// DiffRemote returns a list of files that differ between HEAD and the remote tracking branch.
func DiffRemote(repoPath string) ([]string, error) {
	// Get the current branch
	branchCmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	branchCmd.Dir = repoPath
	branchOut, err := branchCmd.Output()
	if err != nil {
		return nil, err
	}
	branch := strings.TrimSpace(string(branchOut))

	// Get files that differ from remote
	remoteBranch := fmt.Sprintf("origin/%s", branch)
	cmd := exec.Command("git", "diff", "--name-only", "HEAD", remoteBranch)
	cmd.Dir = repoPath
	out, err := cmd.Output()
	if err != nil {
		// If the remote branch doesn't exist, return empty list
		return []string{}, nil
	}

	var files []string
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		file := strings.TrimSpace(scanner.Text())
		if file != "" {
			files = append(files, file)
		}
	}

	return files, scanner.Err()
}

// FileLog returns the git log history for a specific file.
func FileLog(repoPath, filePath string) ([]LogEntry, error) {
	// Format: %H (commit hash) | %an (author name) | %ae (author email) | %ai (author date ISO) | %s (subject)
	cmd := exec.Command("git", "log", "--follow", "--format=%H|%an|%ae|%ai|%s", "--", filePath)
	cmd.Dir = repoPath
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	var entries []LogEntry
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.SplitN(line, "|", 5)
		if len(parts) != 5 {
			continue
		}

		date, err := time.Parse("2006-01-02 15:04:05 -0700", parts[3])
		if err != nil {
			// Try without timezone
			date, _ = time.Parse("2006-01-02 15:04:05", parts[3][:19])
		}

		entries = append(entries, LogEntry{
			Hash:        parts[0][:8], // Short hash
			Author:      parts[1],
			AuthorEmail: parts[2],
			Date:        date,
			Subject:     parts[4],
		})
	}

	return entries, scanner.Err()
}

// LogEntry represents a single entry in the git log.
type LogEntry struct {
	Hash        string
	Author      string
	AuthorEmail string
	Date        time.Time
	Subject     string
}
