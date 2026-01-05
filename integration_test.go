package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/meriksen/over/internal/git"
	"github.com/meriksen/over/internal/overlay"
)

// setupTestGitRepo creates a test git repository with some files
func setupTestGitRepo(t *testing.T) string {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", "over-integration-repo-*")
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
	cmd.Run()

	cmd = exec.Command("git", "config", "user.name", "Test User")
	cmd.Dir = tmpDir
	cmd.Run()

	cmd = exec.Command("git", "config", "commit.gpgsign", "false")
	cmd.Dir = tmpDir
	cmd.Run()

	// Create some test files
	testFiles := map[string]string{
		"README.md":           "# Test Repository",
		"src/main.go":         "package main\n\nfunc main() {}\n",
		"src/utils/helper.go": "package utils\n",
		"config.yaml":         "key: value\n",
	}

	for file, content := range testFiles {
		fullPath := filepath.Join(tmpDir, file)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			os.RemoveAll(tmpDir)
			t.Fatalf("Failed to create dir: %v", err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
			os.RemoveAll(tmpDir)
			t.Fatalf("Failed to write file: %v", err)
		}
	}

	// Commit files
	cmd = exec.Command("git", "add", ".")
	cmd.Dir = tmpDir
	if err := cmd.Run(); err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("Failed to add files: %v", err)
	}

	cmd = exec.Command("git", "commit", "-m", "Initial commit")
	cmd.Dir = tmpDir
	if err := cmd.Run(); err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("Failed to commit: %v", err)
	}

	return tmpDir
}

// TestIntegrationCloneAndStatus tests cloning an overlay and checking its status
func TestIntegrationCloneAndStatus(t *testing.T) {
	// Setup test repository
	repoPath := setupTestGitRepo(t)
	defer os.RemoveAll(repoPath)

	// Create target directory
	targetDir, err := os.MkdirTemp("", "over-integration-target-*")
	if err != nil {
		t.Fatalf("Failed to create target dir: %v", err)
	}
	defer os.RemoveAll(targetDir)

	// Change to target directory
	originalWd, _ := os.Getwd()
	defer os.Chdir(originalWd)
	os.Chdir(targetDir)

	// Clone the repository
	overlayName := "test-overlay"
	if err := git.Clone(repoPath, filepath.Join(targetDir, ".tmp-repo")); err != nil {
		t.Fatalf("Failed to clone repo: %v", err)
	}
	defer os.RemoveAll(filepath.Join(targetDir, ".tmp-repo"))

	// Create hardlinks
	manifest, err := overlay.CreateHardlinks(filepath.Join(targetDir, ".tmp-repo"), targetDir, overlay.DefaultLinkList())
	if err != nil {
		t.Fatalf("Failed to create hardlinks: %v", err)
	}

	// Verify files were hardlinked
	expectedFiles := []string{"README.md", "src/main.go", "src/utils/helper.go", "config.yaml"}
	for _, file := range expectedFiles {
		targetFile := filepath.Join(targetDir, file)
		if _, err := os.Stat(targetFile); err != nil {
			t.Errorf("Expected file %q to exist in target: %v", file, err)
		}
	}

	// Verify manifest
	if len(manifest.Files) != len(expectedFiles) {
		t.Errorf("Expected %d files in manifest, got %d", len(expectedFiles), len(manifest.Files))
	}

	// Save metadata
	ov := overlay.Overlay{
		Name:      overlayName,
		SourceURL: repoPath,
		RepoPath:  filepath.Join(targetDir, ".tmp-repo"),
		TargetDir: targetDir,
	}

	registry, err := overlay.LoadRegistry(targetDir)
	if err != nil {
		t.Fatalf("Failed to load registry: %v", err)
	}

	registry.AddOverlay(ov)
	if err := overlay.SaveRegistry(targetDir, registry); err != nil {
		t.Fatalf("Failed to save registry: %v", err)
	}

	if err := overlay.SaveManifest(targetDir, overlayName, manifest); err != nil {
		t.Fatalf("Failed to save manifest: %v", err)
	}

	// Check status
	changes, err := git.Status(ov.RepoPath)
	if err != nil {
		t.Fatalf("Failed to get status: %v", err)
	}

	// Should be no changes initially
	if len(changes) != 0 {
		t.Errorf("Expected no changes, got %d", len(changes))
	}

	// Modify a file
	testFile := filepath.Join(targetDir, "README.md")
	if err := os.WriteFile(testFile, []byte("# Modified"), 0644); err != nil {
		t.Fatalf("Failed to modify file: %v", err)
	}

	// Check status again
	changes, err = git.Status(ov.RepoPath)
	if err != nil {
		t.Fatalf("Failed to get status after modification: %v", err)
	}

	// Should have 1 modified file
	if len(changes) != 1 {
		t.Errorf("Expected 1 change, got %d", len(changes))
	}

	if changes[0].Status != git.StatusModified {
		t.Errorf("Expected status %q, got %q", git.StatusModified, changes[0].Status)
	}
}

// TestIntegrationAddAndCommit tests adding a file to an overlay and committing
func TestIntegrationAddAndCommit(t *testing.T) {
	// Setup test repository
	repoPath := setupTestGitRepo(t)
	defer os.RemoveAll(repoPath)

	// Create target directory
	targetDir, err := os.MkdirTemp("", "over-integration-target-*")
	if err != nil {
		t.Fatalf("Failed to create target dir: %v", err)
	}
	defer os.RemoveAll(targetDir)

	// Clone and setup overlay
	overlayName := "test-overlay"
	tmpRepoPath := filepath.Join(targetDir, ".tmp-repo")
	if err := git.Clone(repoPath, tmpRepoPath); err != nil {
		t.Fatalf("Failed to clone repo: %v", err)
	}

	// Disable GPG signing in cloned repo
	cmd := exec.Command("git", "config", "commit.gpgsign", "false")
	cmd.Dir = tmpRepoPath
	cmd.Run()

	ov := overlay.Overlay{
		Name:      overlayName,
		RepoPath:  tmpRepoPath,
		TargetDir: targetDir,
	}

	// Create a new file in target
	newFile := filepath.Join(targetDir, "newfile.txt")
	if err := os.WriteFile(newFile, []byte("new content"), 0644); err != nil {
		t.Fatalf("Failed to create new file: %v", err)
	}

	// Copy to repo and create hardlink
	repoFile := filepath.Join(ov.RepoPath, "newfile.txt")
	if err := overlay.CopyFile(newFile, repoFile); err != nil {
		t.Fatalf("Failed to copy file to repo: %v", err)
	}

	os.Remove(newFile)
	if err := os.Link(repoFile, newFile); err != nil {
		t.Fatalf("Failed to create hardlink: %v", err)
	}

	// Add to git
	if err := git.Add(ov.RepoPath, "newfile.txt"); err != nil {
		t.Fatalf("Failed to git add: %v", err)
	}

	// Check that file is staged
	changes, err := git.Status(ov.RepoPath)
	if err != nil {
		t.Fatalf("Failed to get status: %v", err)
	}

	found := false
	for _, change := range changes {
		if change.Path == "newfile.txt" && change.Status == git.StatusAdded {
			found = true
			break
		}
	}

	if !found {
		t.Error("Expected newfile.txt to be staged")
	}

	// Commit
	if err := git.Commit(ov.RepoPath, "Add newfile.txt"); err != nil {
		t.Fatalf("Failed to commit: %v", err)
	}

	// Verify no uncommitted changes
	hasChanges, err := git.HasUncommittedChanges(ov.RepoPath)
	if err != nil {
		t.Fatalf("Failed to check for changes: %v", err)
	}

	if hasChanges {
		t.Error("Expected no uncommitted changes after commit")
	}
}

// TestIntegrationDiscovery tests finding overlays in parent directories
func TestIntegrationDiscovery(t *testing.T) {
	// Create directory structure
	tmpDir, err := os.MkdirTemp("", "over-integration-discovery-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create nested directories
	level1 := tmpDir
	level2 := filepath.Join(level1, "subdir")
	level3 := filepath.Join(level2, "deep")

	if err := os.MkdirAll(level3, 0755); err != nil {
		t.Fatalf("Failed to create nested dirs: %v", err)
	}

	// Create overlay at level1
	registry1 := &overlay.Registry{
		Version: 1,
		Overlays: []overlay.Overlay{
			{Name: "overlay1", TargetDir: level1},
		},
	}
	if err := overlay.SaveRegistry(level1, registry1); err != nil {
		t.Fatalf("Failed to save registry at level1: %v", err)
	}

	// Create overlay at level2
	registry2 := &overlay.Registry{
		Version: 1,
		Overlays: []overlay.Overlay{
			{Name: "overlay2", TargetDir: level2},
		},
	}
	if err := overlay.SaveRegistry(level2, registry2); err != nil {
		t.Fatalf("Failed to save registry at level2: %v", err)
	}

	// Find from level3 - should find both overlays
	overlays, err := overlay.FindReachableOverlays(level3)
	if err != nil {
		t.Fatalf("Failed to find overlays: %v", err)
	}

	if len(overlays) != 2 {
		t.Errorf("Expected 2 overlays from level3, got %d", len(overlays))
	}

	// Verify overlay names
	foundNames := make(map[string]bool)
	for _, ov := range overlays {
		foundNames[ov.Name] = true
	}

	if !foundNames["overlay1"] {
		t.Error("Expected to find overlay1")
	}
	if !foundNames["overlay2"] {
		t.Error("Expected to find overlay2")
	}

	// Find by name
	found, err := overlay.FindOverlayByName(level3, "overlay1")
	if err != nil {
		t.Fatalf("Failed to find overlay by name: %v", err)
	}

	if found == nil {
		t.Fatal("Expected to find overlay1 by name")
	}

	if found.Name != "overlay1" {
		t.Errorf("Expected overlay1, got %q", found.Name)
	}
}

// TestIntegrationConflictDetection tests conflict detection during clone
func TestIntegrationConflictDetection(t *testing.T) {
	// Setup test repository
	repoPath := setupTestGitRepo(t)
	defer os.RemoveAll(repoPath)

	// Create target directory
	targetDir, err := os.MkdirTemp("", "over-integration-conflict-*")
	if err != nil {
		t.Fatalf("Failed to create target dir: %v", err)
	}
	defer os.RemoveAll(targetDir)

	// Create conflicting file in target
	conflictFile := filepath.Join(targetDir, "README.md")
	if err := os.WriteFile(conflictFile, []byte("existing content"), 0644); err != nil {
		t.Fatalf("Failed to create conflict file: %v", err)
	}

	// Clone repository to temp location
	tmpRepoPath := filepath.Join(targetDir, ".tmp-repo")
	if err := git.Clone(repoPath, tmpRepoPath); err != nil {
		t.Fatalf("Failed to clone repo: %v", err)
	}
	defer os.RemoveAll(tmpRepoPath)

	// Check for conflicts
	conflicts, err := overlay.CheckConflicts(tmpRepoPath, targetDir, overlay.DefaultLinkList())
	if err != nil {
		t.Fatalf("Failed to check conflicts: %v", err)
	}

	// Should find README.md as conflict
	if len(conflicts) != 1 {
		t.Fatalf("Expected 1 conflict, got %d", len(conflicts))
	}

	if conflicts[0] != "README.md" {
		t.Errorf("Expected conflict on README.md, got %q", conflicts[0])
	}
}

// TestIntegrationHardlinkSync tests synchronizing hardlinks after repo changes
func TestIntegrationHardlinkSync(t *testing.T) {
	// Setup test repository
	repoPath := setupTestGitRepo(t)
	defer os.RemoveAll(repoPath)

	// Create target directory
	targetDir, err := os.MkdirTemp("", "over-integration-sync-*")
	if err != nil {
		t.Fatalf("Failed to create target dir: %v", err)
	}
	defer os.RemoveAll(targetDir)

	// Clone and create initial hardlinks
	tmpRepoPath := filepath.Join(targetDir, ".tmp-repo")
	if err := git.Clone(repoPath, tmpRepoPath); err != nil {
		t.Fatalf("Failed to clone repo: %v", err)
	}

	// Get initial file list
	filesBefore, err := git.ListFiles(tmpRepoPath)
	if err != nil {
		t.Fatalf("Failed to list files: %v", err)
	}

	// Create hardlinks
	if _, err := overlay.CreateHardlinks(tmpRepoPath, targetDir, overlay.DefaultLinkList()); err != nil {
		t.Fatalf("Failed to create hardlinks: %v", err)
	}

	// Modify repo: add a new file and delete an existing one
	newFile := filepath.Join(tmpRepoPath, "newfile.txt")
	if err := os.WriteFile(newFile, []byte("new"), 0644); err != nil {
		t.Fatalf("Failed to create new file: %v", err)
	}

	cmd := exec.Command("git", "add", "newfile.txt")
	cmd.Dir = tmpRepoPath
	cmd.Run()

	cmd = exec.Command("git", "commit", "-m", "Add newfile")
	cmd.Dir = tmpRepoPath
	cmd.Run()

	// Delete a file
	os.Remove(filepath.Join(tmpRepoPath, "config.yaml"))
	cmd = exec.Command("git", "add", "config.yaml")
	cmd.Dir = tmpRepoPath
	cmd.Run()

	cmd = exec.Command("git", "commit", "-m", "Remove config.yaml")
	cmd.Dir = tmpRepoPath
	cmd.Run()

	// Get new file list
	filesAfter, err := git.ListFiles(tmpRepoPath)
	if err != nil {
		t.Fatalf("Failed to list files after changes: %v", err)
	}

	// Sync hardlinks
	ov := overlay.Overlay{
		RepoPath:  tmpRepoPath,
		TargetDir: targetDir,
		LinkList:  overlay.DefaultLinkList(), // Link all files
	}

	if err := overlay.SyncHardlinks(ov, nil, filesBefore, filesAfter); err != nil {
		t.Fatalf("Failed to sync hardlinks: %v", err)
	}

	// Verify new file exists in target
	targetNewFile := filepath.Join(targetDir, "newfile.txt")
	if _, err := os.Stat(targetNewFile); err != nil {
		t.Error("Expected newfile.txt to exist in target after sync")
	}

	// Verify deleted file is removed from target
	targetDeletedFile := filepath.Join(targetDir, "config.yaml")
	if _, err := os.Stat(targetDeletedFile); err == nil {
		t.Error("Expected config.yaml to be removed from target after sync")
	}
}

// TestIntegrationBrokenHardlinks tests detecting broken hardlinks
func TestIntegrationBrokenHardlinks(t *testing.T) {
	// Setup
	tmpDir, err := os.MkdirTemp("", "over-integration-broken-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	repoDir := filepath.Join(tmpDir, "repo")
	targetDir := filepath.Join(tmpDir, "target")

	os.MkdirAll(repoDir, 0755)
	os.MkdirAll(targetDir, 0755)

	// Create file in repo
	repoFile := filepath.Join(repoDir, "test.txt")
	targetFile := filepath.Join(targetDir, "test.txt")

	os.WriteFile(repoFile, []byte("content"), 0644)

	// Create manifest by hardlinking
	manifest, _ := overlay.CreateHardlinks(repoDir, targetDir, overlay.DefaultLinkList())

	ov := overlay.Overlay{
		RepoPath:  repoDir,
		TargetDir: targetDir,
	}

	// Initially no broken links
	broken := overlay.CheckBrokenHardlinks(ov, manifest)
	if len(broken) != 0 {
		t.Errorf("Expected no broken links initially, got %d", len(broken))
	}

	// Break the hardlink
	os.Remove(targetFile)
	os.WriteFile(targetFile, []byte("different"), 0644)

	// Should detect broken link
	broken = overlay.CheckBrokenHardlinks(ov, manifest)
	if len(broken) != 1 {
		t.Errorf("Expected 1 broken link, got %d", len(broken))
	} else {
		if !strings.Contains(broken[0], "test.txt") {
			t.Errorf("Expected broken link to be test.txt, got %q", broken[0])
		}
	}
}

// TestIntegrationUnlinkFile tests the UnlinkFile function
func TestIntegrationUnlinkFile(t *testing.T) {
	// Setup
	tmpDir, err := os.MkdirTemp("", "over-integration-unlink-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	repoDir := filepath.Join(tmpDir, "repo")
	targetDir := filepath.Join(tmpDir, "target")

	os.MkdirAll(repoDir, 0755)
	os.MkdirAll(targetDir, 0755)

	// Create file in repo
	repoFile := filepath.Join(repoDir, "test.txt")
	os.WriteFile(repoFile, []byte("original content"), 0644)

	// Create manifest by hardlinking
	manifest, _ := overlay.CreateHardlinks(repoDir, targetDir, overlay.DefaultLinkList())

	ov := overlay.Overlay{
		Name:      "test-overlay",
		RepoPath:  repoDir,
		TargetDir: targetDir,
	}

	targetFile := filepath.Join(targetDir, "test.txt")

	// Verify hardlink exists
	repoInfo, _ := os.Stat(repoFile)
	targetInfo, _ := os.Stat(targetFile)
	if !os.SameFile(repoInfo, targetInfo) {
		t.Fatal("Files should be hardlinked initially")
	}

	// Unlink the file
	if err := overlay.UnlinkFile(ov, manifest, "test.txt"); err != nil {
		t.Fatalf("Failed to unlink file: %v", err)
	}

	// Verify hardlink is broken
	repoInfo, _ = os.Stat(repoFile)
	targetInfo, _ = os.Stat(targetFile)
	if os.SameFile(repoInfo, targetInfo) {
		t.Error("Files should not be hardlinked after unlink")
	}

	// Verify manifest is updated
	var found bool
	for _, entry := range manifest.Files {
		if entry.RelativePath == "test.txt" {
			found = true
			if !entry.Unlinked {
				t.Error("File should be marked as unlinked in manifest")
			}
		}
	}
	if !found {
		t.Error("File not found in manifest")
	}

	// Verify file content is preserved
	content, _ := os.ReadFile(targetFile)
	if string(content) != "original content" {
		t.Errorf("Expected content 'original content', got %q", string(content))
	}

	// Verify attempting to unlink again fails
	if err := overlay.UnlinkFile(ov, manifest, "test.txt"); err == nil {
		t.Error("Expected error when unlinking already unlinked file")
	}
}

// TestIntegrationRelinkFile tests the RelinkFile function
func TestIntegrationRelinkFile(t *testing.T) {
	// Setup
	tmpDir, err := os.MkdirTemp("", "over-integration-relink-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	repoDir := filepath.Join(tmpDir, "repo")
	targetDir := filepath.Join(tmpDir, "target")

	os.MkdirAll(repoDir, 0755)
	os.MkdirAll(targetDir, 0755)

	// Create file in repo
	repoFile := filepath.Join(repoDir, "test.txt")
	os.WriteFile(repoFile, []byte("repo content"), 0644)

	// Create manifest by hardlinking
	manifest, _ := overlay.CreateHardlinks(repoDir, targetDir, overlay.DefaultLinkList())

	ov := overlay.Overlay{
		Name:      "test-overlay",
		RepoPath:  repoDir,
		TargetDir: targetDir,
	}

	targetFile := filepath.Join(targetDir, "test.txt")

	// Unlink the file
	if err := overlay.UnlinkFile(ov, manifest, "test.txt"); err != nil {
		t.Fatalf("Failed to unlink file: %v", err)
	}

	// Modify the target file
	os.WriteFile(targetFile, []byte("user content"), 0644)

	// Relink the file
	if err := overlay.RelinkFile(ov, manifest, "test.txt"); err != nil {
		t.Fatalf("Failed to relink file: %v", err)
	}

	// Verify hardlink exists
	repoInfo, _ := os.Stat(repoFile)
	targetInfo, _ := os.Stat(targetFile)
	if !os.SameFile(repoInfo, targetInfo) {
		t.Error("Files should be hardlinked after relink")
	}

	// Verify user content wins
	repoContent, _ := os.ReadFile(repoFile)
	targetContent, _ := os.ReadFile(targetFile)
	if string(repoContent) != "user content" {
		t.Errorf("Expected repo content 'user content', got %q", string(repoContent))
	}
	if string(targetContent) != "user content" {
		t.Errorf("Expected target content 'user content', got %q", string(targetContent))
	}

	// Verify manifest is updated
	var found bool
	for _, entry := range manifest.Files {
		if entry.RelativePath == "test.txt" {
			found = true
			if entry.Unlinked {
				t.Error("File should not be marked as unlinked in manifest after relink")
			}
		}
	}
	if !found {
		t.Error("File not found in manifest")
	}

	// Verify attempting to relink again fails
	if err := overlay.RelinkFile(ov, manifest, "test.txt"); err == nil {
		t.Error("Expected error when relinking already linked file")
	}
}

// TestIntegrationSyncWithUnlinkedFiles tests that sync skips unlinked files
func TestIntegrationSyncWithUnlinkedFiles(t *testing.T) {
	// Setup
	repoPath := setupTestGitRepo(t)
	defer os.RemoveAll(repoPath)

	// Create target directory
	targetDir, err := os.MkdirTemp("", "over-integration-sync-unlinked-*")
	if err != nil {
		t.Fatalf("Failed to create target dir: %v", err)
	}
	defer os.RemoveAll(targetDir)

	// Clone and create initial hardlinks
	tmpRepoPath := filepath.Join(targetDir, ".tmp-repo")
	if err := git.Clone(repoPath, tmpRepoPath); err != nil {
		t.Fatalf("Failed to clone repo: %v", err)
	}

	// Disable GPG signing
	cmd := exec.Command("git", "config", "commit.gpgsign", "false")
	cmd.Dir = tmpRepoPath
	cmd.Run()

	// Get initial file list
	filesBefore, err := git.ListFiles(tmpRepoPath)
	if err != nil {
		t.Fatalf("Failed to list files: %v", err)
	}

	// Create hardlinks
	manifest, err := overlay.CreateHardlinks(tmpRepoPath, targetDir, overlay.DefaultLinkList())
	if err != nil {
		t.Fatalf("Failed to create hardlinks: %v", err)
	}

	ov := overlay.Overlay{
		Name:      "test-overlay",
		RepoPath:  tmpRepoPath,
		TargetDir: targetDir,
	}

	// Unlink README.md
	if err := overlay.UnlinkFile(ov, manifest, "README.md"); err != nil {
		t.Fatalf("Failed to unlink README.md: %v", err)
	}

	// Modify the unlinked file
	readmePath := filepath.Join(targetDir, "README.md")
	if err := os.WriteFile(readmePath, []byte("# Modified by user"), 0644); err != nil {
		t.Fatalf("Failed to modify unlinked file: %v", err)
	}

	// Modify repo: update README.md in repo
	repoReadme := filepath.Join(tmpRepoPath, "README.md")
	if err := os.WriteFile(repoReadme, []byte("# Updated in repo"), 0644); err != nil {
		t.Fatalf("Failed to update file in repo: %v", err)
	}

	cmd = exec.Command("git", "add", "README.md")
	cmd.Dir = tmpRepoPath
	cmd.Run()

	cmd = exec.Command("git", "commit", "-m", "Update README")
	cmd.Dir = tmpRepoPath
	cmd.Run()

	// Get new file list
	filesAfter, err := git.ListFiles(tmpRepoPath)
	if err != nil {
		t.Fatalf("Failed to list files after changes: %v", err)
	}

	// Sync hardlinks (should skip unlinked README.md)
	if err := overlay.SyncHardlinks(ov, manifest, filesBefore, filesAfter); err != nil {
		t.Fatalf("Failed to sync hardlinks: %v", err)
	}

	// Verify unlinked file was NOT updated
	userContent, _ := os.ReadFile(readmePath)
	if string(userContent) != "# Modified by user" {
		t.Errorf("Expected unlinked file to keep user content, got %q", string(userContent))
	}

	// Verify files are still not hardlinked
	repoInfo, _ := os.Stat(repoReadme)
	targetInfo, _ := os.Stat(readmePath)
	if os.SameFile(repoInfo, targetInfo) {
		t.Error("Unlinked file should remain unlinked after sync")
	}
}

// TestIntegrationCheckBrokenHardlinksSkipsUnlinked tests that CheckBrokenHardlinks skips unlinked files
func TestIntegrationCheckBrokenHardlinksSkipsUnlinked(t *testing.T) {
	// Setup
	tmpDir, err := os.MkdirTemp("", "over-integration-check-unlinked-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	repoDir := filepath.Join(tmpDir, "repo")
	targetDir := filepath.Join(tmpDir, "target")

	os.MkdirAll(repoDir, 0755)
	os.MkdirAll(targetDir, 0755)

	// Create files in repo
	os.WriteFile(filepath.Join(repoDir, "linked.txt"), []byte("content"), 0644)
	os.WriteFile(filepath.Join(repoDir, "unlinked.txt"), []byte("content"), 0644)

	// Create manifest by hardlinking
	manifest, _ := overlay.CreateHardlinks(repoDir, targetDir, overlay.DefaultLinkList())

	ov := overlay.Overlay{
		Name:      "test-overlay",
		RepoPath:  repoDir,
		TargetDir: targetDir,
	}

	// Unlink one file
	if err := overlay.UnlinkFile(ov, manifest, "unlinked.txt"); err != nil {
		t.Fatalf("Failed to unlink file: %v", err)
	}

	// Break the hardlink on linked file
	linkedTarget := filepath.Join(targetDir, "linked.txt")
	os.Remove(linkedTarget)
	os.WriteFile(linkedTarget, []byte("different"), 0644)

	// Check broken hardlinks (should only report linked.txt)
	broken := overlay.CheckBrokenHardlinks(ov, manifest)
	if len(broken) != 1 {
		t.Errorf("Expected 1 broken link, got %d", len(broken))
	}
	if len(broken) > 0 && !strings.Contains(broken[0], "linked.txt") {
		t.Errorf("Expected broken link to be linked.txt, got %q", broken[0])
	}
}
