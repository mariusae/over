package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestExtractRepoName(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		expected string
	}{
		{
			name:     "HTTPS URL with .git",
			url:      "https://github.com/user/repo.git",
			expected: "repo",
		},
		{
			name:     "HTTPS URL without .git",
			url:      "https://github.com/user/repo",
			expected: "repo",
		},
		{
			name:     "SSH URL",
			url:      "git@github.com:user/repo.git",
			expected: "repo",
		},
		{
			name:     "Local path",
			url:      "/path/to/repo",
			expected: "repo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ExtractRepoName(tt.url)
			if result != tt.expected {
				t.Errorf("ExtractRepoName(%q) = %q, want %q", tt.url, result, tt.expected)
			}
		})
	}
}

func TestChangeStatus(t *testing.T) {
	tests := []struct {
		name     string
		status   ChangeStatus
		expected string
	}{
		{name: "Added", status: StatusAdded, expected: "A"},
		{name: "Modified", status: StatusModified, expected: "M"},
		{name: "Deleted", status: StatusDeleted, expected: "D"},
		{name: "Untracked", status: StatusUntracked, expected: "?"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.status) != tt.expected {
				t.Errorf("Status constant mismatch: got %q, want %q", tt.status, tt.expected)
			}
		})
	}
}

// setupTestRepo creates a temporary git repository for testing
func setupTestRepo(t *testing.T) string {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", "git-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}

	// Initialize git repo
	cmd := exec.Command("git", "init")
	cmd.Dir = tmpDir
	if err := cmd.Run(); err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("Failed to init git repo: %v", err)
	}

	// Configure git
	cmd = exec.Command("git", "config", "user.email", "test@example.com")
	cmd.Dir = tmpDir
	if err := cmd.Run(); err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("Failed to configure git email: %v", err)
	}

	cmd = exec.Command("git", "config", "user.name", "Test User")
	cmd.Dir = tmpDir
	if err := cmd.Run(); err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("Failed to configure git name: %v", err)
	}

	// Disable GPG signing for tests
	cmd = exec.Command("git", "config", "commit.gpgsign", "false")
	cmd.Dir = tmpDir
	if err := cmd.Run(); err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("Failed to disable GPG signing: %v", err)
	}

	return tmpDir
}

func TestStatus(t *testing.T) {
	repoPath := setupTestRepo(t)
	defer os.RemoveAll(repoPath)

	// Create and commit a file
	testFile := filepath.Join(repoPath, "test.txt")
	if err := os.WriteFile(testFile, []byte("initial content"), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	cmd := exec.Command("git", "add", "test.txt")
	cmd.Dir = repoPath
	if err := cmd.Run(); err != nil {
		t.Fatalf("Failed to add file: %v", err)
	}

	cmd = exec.Command("git", "commit", "-m", "Initial commit")
	cmd.Dir = repoPath
	if err := cmd.Run(); err != nil {
		t.Fatalf("Failed to commit: %v", err)
	}

	// Modify the file
	if err := os.WriteFile(testFile, []byte("modified content"), 0644); err != nil {
		t.Fatalf("Failed to modify file: %v", err)
	}

	// Get status
	changes, err := Status(repoPath)
	if err != nil {
		t.Fatalf("Status failed: %v", err)
	}

	if len(changes) != 1 {
		t.Fatalf("Expected 1 change, got %d", len(changes))
	}

	if changes[0].Status != StatusModified {
		t.Errorf("Expected status %q, got %q", StatusModified, changes[0].Status)
	}

	if changes[0].Path != "test.txt" {
		t.Errorf("Expected path %q, got %q", "test.txt", changes[0].Path)
	}
}

func TestHasUncommittedChanges(t *testing.T) {
	repoPath := setupTestRepo(t)
	defer os.RemoveAll(repoPath)

	// Initially clean
	hasChanges, err := HasUncommittedChanges(repoPath)
	if err != nil {
		t.Fatalf("HasUncommittedChanges failed: %v", err)
	}
	if hasChanges {
		t.Error("Expected no uncommitted changes in clean repo")
	}

	// Create a new file
	testFile := filepath.Join(repoPath, "test.txt")
	if err := os.WriteFile(testFile, []byte("content"), 0644); err != nil {
		t.Fatalf("Failed to write file: %v", err)
	}

	// Should have uncommitted changes
	hasChanges, err = HasUncommittedChanges(repoPath)
	if err != nil {
		t.Fatalf("HasUncommittedChanges failed: %v", err)
	}
	if !hasChanges {
		t.Error("Expected uncommitted changes after creating file")
	}
}

func TestListFiles(t *testing.T) {
	repoPath := setupTestRepo(t)
	defer os.RemoveAll(repoPath)

	// Create and commit files
	files := []string{"file1.txt", "dir/file2.txt", "dir/subdir/file3.txt"}
	for _, file := range files {
		fullPath := filepath.Join(repoPath, file)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			t.Fatalf("Failed to create dir: %v", err)
		}
		if err := os.WriteFile(fullPath, []byte("content"), 0644); err != nil {
			t.Fatalf("Failed to write file: %v", err)
		}
	}

	cmd := exec.Command("git", "add", ".")
	cmd.Dir = repoPath
	if err := cmd.Run(); err != nil {
		t.Fatalf("Failed to add files: %v", err)
	}

	cmd = exec.Command("git", "commit", "-m", "Add files")
	cmd.Dir = repoPath
	if err := cmd.Run(); err != nil {
		t.Fatalf("Failed to commit: %v", err)
	}

	// List files
	listedFiles, err := ListFiles(repoPath)
	if err != nil {
		t.Fatalf("ListFiles failed: %v", err)
	}

	if len(listedFiles) != len(files) {
		t.Errorf("Expected %d files, got %d", len(files), len(listedFiles))
	}

	// Check that all expected files are present
	fileSet := make(map[string]bool)
	for _, f := range listedFiles {
		fileSet[f] = true
	}

	for _, expectedFile := range files {
		if !fileSet[expectedFile] {
			t.Errorf("Expected file %q not found in listed files", expectedFile)
		}
	}
}

func TestAdd(t *testing.T) {
	repoPath := setupTestRepo(t)
	defer os.RemoveAll(repoPath)

	// Create a file
	testFile := filepath.Join(repoPath, "test.txt")
	if err := os.WriteFile(testFile, []byte("content"), 0644); err != nil {
		t.Fatalf("Failed to write file: %v", err)
	}

	// Add the file
	if err := Add(repoPath, "test.txt"); err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	// Check that file is staged
	changes, err := Status(repoPath)
	if err != nil {
		t.Fatalf("Status failed: %v", err)
	}

	if len(changes) != 1 {
		t.Fatalf("Expected 1 staged file, got %d changes", len(changes))
	}

	if changes[0].Status != StatusAdded {
		t.Errorf("Expected status %q, got %q", StatusAdded, changes[0].Status)
	}
}
