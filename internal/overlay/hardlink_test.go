package overlay

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestCopyFile(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "overlay-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	srcFile := filepath.Join(tmpDir, "source.txt")
	dstFile := filepath.Join(tmpDir, "dest.txt")

	content := []byte("test content")
	if err := os.WriteFile(srcFile, content, 0644); err != nil {
		t.Fatalf("Failed to create source file: %v", err)
	}

	// Copy file
	if err := CopyFile(srcFile, dstFile); err != nil {
		t.Fatalf("CopyFile failed: %v", err)
	}

	// Verify destination exists
	dstContent, err := os.ReadFile(dstFile)
	if err != nil {
		t.Fatalf("Failed to read destination file: %v", err)
	}

	if string(dstContent) != string(content) {
		t.Errorf("Content mismatch: got %q, want %q", dstContent, content)
	}

	// Verify permissions
	srcInfo, _ := os.Stat(srcFile)
	dstInfo, _ := os.Stat(dstFile)

	if srcInfo.Mode() != dstInfo.Mode() {
		t.Errorf("Mode mismatch: got %v, want %v", dstInfo.Mode(), srcInfo.Mode())
	}
}

func TestCheckConflicts(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "overlay-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	repoDir := filepath.Join(tmpDir, "repo")
	targetDir := filepath.Join(tmpDir, "target")

	if err := os.MkdirAll(repoDir, 0755); err != nil {
		t.Fatalf("Failed to create repo dir: %v", err)
	}
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		t.Fatalf("Failed to create target dir: %v", err)
	}

	// Create files in repo
	os.WriteFile(filepath.Join(repoDir, "file1.txt"), []byte("content"), 0644)
	os.WriteFile(filepath.Join(repoDir, "file2.txt"), []byte("content"), 0644)
	os.MkdirAll(filepath.Join(repoDir, "subdir"), 0755)
	os.WriteFile(filepath.Join(repoDir, "subdir", "file3.txt"), []byte("content"), 0644)

	// Create .git directory (should be ignored)
	os.MkdirAll(filepath.Join(repoDir, ".git"), 0755)
	os.WriteFile(filepath.Join(repoDir, ".git", "config"), []byte("git config"), 0644)

	// Create conflicting file in target
	os.WriteFile(filepath.Join(targetDir, "file2.txt"), []byte("existing"), 0644)

	// Check conflicts
	conflicts, err := CheckConflicts(repoDir, targetDir)
	if err != nil {
		t.Fatalf("CheckConflicts failed: %v", err)
	}

	// Should find file2.txt as conflict, but not file1.txt or file3.txt
	if len(conflicts) != 1 {
		t.Fatalf("Expected 1 conflict, got %d: %v", len(conflicts), conflicts)
	}

	if conflicts[0] != "file2.txt" {
		t.Errorf("Expected conflict %q, got %q", "file2.txt", conflicts[0])
	}
}

func TestCreateHardlinks(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "overlay-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	repoDir := filepath.Join(tmpDir, "repo")
	targetDir := filepath.Join(tmpDir, "target")

	if err := os.MkdirAll(repoDir, 0755); err != nil {
		t.Fatalf("Failed to create repo dir: %v", err)
	}
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		t.Fatalf("Failed to create target dir: %v", err)
	}

	// Create test files in repo
	testFiles := []string{"file1.txt", "dir/file2.txt"}
	for _, file := range testFiles {
		fullPath := filepath.Join(repoDir, file)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			t.Fatalf("Failed to create dir: %v", err)
		}
		if err := os.WriteFile(fullPath, []byte("content"), 0644); err != nil {
			t.Fatalf("Failed to write file: %v", err)
		}
	}

	// Create hardlinks
	manifest, err := CreateHardlinks(repoDir, targetDir)
	if err != nil {
		t.Fatalf("CreateHardlinks failed: %v", err)
	}

	// Verify manifest
	if len(manifest.Files) != len(testFiles) {
		t.Errorf("Expected %d files in manifest, got %d", len(testFiles), len(manifest.Files))
	}

	// Verify hardlinks
	for _, file := range testFiles {
		repoPath := filepath.Join(repoDir, file)
		targetPath := filepath.Join(targetDir, file)

		// Check that target file exists
		if _, err := os.Stat(targetPath); err != nil {
			t.Errorf("Target file %q does not exist: %v", file, err)
			continue
		}

		// Check that files are hardlinked (same inode)
		repoInfo, _ := os.Stat(repoPath)
		targetInfo, _ := os.Stat(targetPath)

		if !os.SameFile(repoInfo, targetInfo) {
			t.Errorf("Files %q are not hardlinked", file)
		}
	}
}

func TestCheckBrokenHardlinks(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "overlay-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	repoDir := filepath.Join(tmpDir, "repo")
	targetDir := filepath.Join(tmpDir, "target")

	if err := os.MkdirAll(repoDir, 0755); err != nil {
		t.Fatalf("Failed to create repo dir: %v", err)
	}
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		t.Fatalf("Failed to create target dir: %v", err)
	}

	// Create test file and hardlink
	repoFile := filepath.Join(repoDir, "test.txt")
	targetFile := filepath.Join(targetDir, "test.txt")

	if err := os.WriteFile(repoFile, []byte("content"), 0644); err != nil {
		t.Fatalf("Failed to write file: %v", err)
	}

	if err := os.Link(repoFile, targetFile); err != nil {
		t.Fatalf("Failed to create hardlink: %v", err)
	}

	// Get file info for manifest
	info, _ := os.Stat(repoFile)
	stat := info.Sys().(*syscall.Stat_t)

	overlay := Overlay{
		RepoPath:  repoDir,
		TargetDir: targetDir,
	}

	manifest := &Manifest{
		Version: 1,
		Files: []FileEntry{
			{
				RelativePath: "test.txt",
				Inode:        stat.Ino,
				Size:         info.Size(),
				Mode:         uint32(info.Mode()),
			},
		},
	}

	// Check - should be no broken links
	broken := CheckBrokenHardlinks(overlay, manifest)
	if len(broken) != 0 {
		t.Errorf("Expected no broken links, got %d: %v", len(broken), broken)
	}

	// Break the hardlink by replacing the target file
	os.Remove(targetFile)
	os.WriteFile(targetFile, []byte("different content"), 0644)

	// Check again - should find broken link
	broken = CheckBrokenHardlinks(overlay, manifest)
	if len(broken) != 1 {
		t.Fatalf("Expected 1 broken link, got %d", len(broken))
	}

	if broken[0] != "test.txt" {
		t.Errorf("Expected broken link %q, got %q", "test.txt", broken[0])
	}
}

func TestSyncHardlinks(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "overlay-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	repoDir := filepath.Join(tmpDir, "repo")
	targetDir := filepath.Join(tmpDir, "target")

	if err := os.MkdirAll(repoDir, 0755); err != nil {
		t.Fatalf("Failed to create repo dir: %v", err)
	}
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		t.Fatalf("Failed to create target dir: %v", err)
	}

	overlay := Overlay{
		RepoPath:  repoDir,
		TargetDir: targetDir,
	}

	// Initial files
	filesBefore := []string{"file1.txt", "file2.txt"}
	for _, file := range filesBefore {
		repoPath := filepath.Join(repoDir, file)
		targetPath := filepath.Join(targetDir, file)
		os.WriteFile(repoPath, []byte("content"), 0644)
		os.Link(repoPath, targetPath)
	}

	// After: file1.txt remains, file2.txt deleted, file3.txt added
	filesAfter := []string{"file1.txt", "file3.txt"}

	// Delete file2.txt from repo
	os.Remove(filepath.Join(repoDir, "file2.txt"))

	// Add file3.txt to repo
	os.WriteFile(filepath.Join(repoDir, "file3.txt"), []byte("new content"), 0644)

	// Sync hardlinks
	if err := SyncHardlinks(overlay, filesBefore, filesAfter); err != nil {
		t.Fatalf("SyncHardlinks failed: %v", err)
	}

	// Verify file1.txt still exists in target
	if _, err := os.Stat(filepath.Join(targetDir, "file1.txt")); err != nil {
		t.Error("file1.txt should still exist in target")
	}

	// Verify file2.txt was removed from target
	if _, err := os.Stat(filepath.Join(targetDir, "file2.txt")); err == nil {
		t.Error("file2.txt should have been removed from target")
	}

	// Verify file3.txt was added to target
	targetFile3 := filepath.Join(targetDir, "file3.txt")
	if _, err := os.Stat(targetFile3); err != nil {
		t.Error("file3.txt should have been added to target")
	}

	// Verify file3.txt is hardlinked
	repoFile3 := filepath.Join(repoDir, "file3.txt")
	repoInfo, _ := os.Stat(repoFile3)
	targetInfo, _ := os.Stat(targetFile3)
	if !os.SameFile(repoInfo, targetInfo) {
		t.Error("file3.txt should be hardlinked")
	}
}
