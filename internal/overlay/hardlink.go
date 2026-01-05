package overlay

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
)

// CheckConflicts checks if any files from the repo would conflict with existing files.
// Only checks files that would be linked according to the linkList patterns.
func CheckConflicts(repoPath, targetDir string, linkList []string) ([]string, error) {
	var conflicts []string

	err := filepath.WalkDir(repoPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Skip .git directory
		if d.IsDir() && d.Name() == ".git" {
			return filepath.SkipDir
		}

		if d.IsDir() {
			return nil
		}

		relPath, err := filepath.Rel(repoPath, path)
		if err != nil {
			return err
		}

		// Skip files not matching linklist patterns
		if !ShouldLinkFile(linkList, relPath) {
			return nil
		}

		targetPath := filepath.Join(targetDir, relPath)
		if _, err := os.Stat(targetPath); err == nil {
			conflicts = append(conflicts, relPath)
		}

		return nil
	})

	return conflicts, err
}

// CreateHardlinks creates hardlinks from repo files to the target directory.
// Only links files that match the linkList patterns.
func CreateHardlinks(repoPath, targetDir string, linkList []string) (*Manifest, error) {
	manifest := &Manifest{Version: 1, Files: []FileEntry{}}

	err := filepath.WalkDir(repoPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Skip .git directory
		if d.IsDir() && d.Name() == ".git" {
			return filepath.SkipDir
		}

		relPath, err := filepath.Rel(repoPath, path)
		if err != nil {
			return err
		}

		targetPath := filepath.Join(targetDir, relPath)

		if d.IsDir() {
			// Create corresponding directory in target (even if files inside might not be linked)
			return os.MkdirAll(targetPath, 0755)
		}

		// Skip files not matching linklist patterns
		if !ShouldLinkFile(linkList, relPath) {
			return nil
		}

		// Skip symlinks
		if d.Type()&os.ModeSymlink != 0 {
			fmt.Fprintf(os.Stderr, "warning: skipping symlink: %s\n", relPath)
			return nil
		}

		// Ensure parent directory exists
		if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
			return fmt.Errorf("failed to create parent directory: %w", err)
		}

		// Create hardlink
		if err := os.Link(path, targetPath); err != nil {
			return fmt.Errorf("failed to create hardlink %s -> %s: %w", path, targetPath, err)
		}

		// Get file info for manifest
		info, err := os.Stat(path)
		if err != nil {
			return err
		}

		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok {
			return fmt.Errorf("failed to get inode for %s", path)
		}

		manifest.AddFile(FileEntry{
			RelativePath: relPath,
			Inode:        stat.Ino,
			Size:         info.Size(),
			Mode:         uint32(info.Mode()),
		})

		return nil
	})

	return manifest, err
}

// CreateHardlinksKeepLocal creates hardlinks, but for files in keepLocal list,
// copies local content to repo first (preserving local version), then hardlinks.
// Only links files that match the linkList patterns.
func CreateHardlinksKeepLocal(repoPath, targetDir string, linkList []string, keepLocal []string) (*Manifest, error) {
	keepLocalSet := make(map[string]bool)
	for _, s := range keepLocal {
		keepLocalSet[s] = true
	}

	manifest := &Manifest{Version: 1, Files: []FileEntry{}}

	err := filepath.WalkDir(repoPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Skip .git directory
		if d.IsDir() && d.Name() == ".git" {
			return filepath.SkipDir
		}

		relPath, err := filepath.Rel(repoPath, path)
		if err != nil {
			return err
		}

		targetPath := filepath.Join(targetDir, relPath)

		if d.IsDir() {
			// Create corresponding directory in target
			return os.MkdirAll(targetPath, 0755)
		}

		// Skip files not matching linklist patterns
		if !ShouldLinkFile(linkList, relPath) {
			return nil
		}

		// Skip symlinks
		if d.Type()&os.ModeSymlink != 0 {
			fmt.Fprintf(os.Stderr, "warning: skipping symlink: %s\n", relPath)
			return nil
		}

		// Ensure parent directory exists
		if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
			return fmt.Errorf("failed to create parent directory: %w", err)
		}

		// Handle files in keepLocal list: copy local → repo, then hardlink
		if keepLocalSet[relPath] {
			// Copy local file content to repo (overwriting repo version)
			if err := CopyFile(targetPath, path); err != nil {
				return fmt.Errorf("failed to copy local file to repo %s: %w", relPath, err)
			}
			// Remove local file
			if err := os.Remove(targetPath); err != nil {
				return fmt.Errorf("failed to remove local file %s: %w", relPath, err)
			}
		}

		// Create hardlink
		if err := os.Link(path, targetPath); err != nil {
			return fmt.Errorf("failed to create hardlink %s -> %s: %w", path, targetPath, err)
		}

		// Get file info for manifest
		info, err := os.Stat(path)
		if err != nil {
			return err
		}

		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok {
			return fmt.Errorf("failed to get inode for %s", path)
		}

		manifest.AddFile(FileEntry{
			RelativePath: relPath,
			Inode:        stat.Ino,
			Size:         info.Size(),
			Mode:         uint32(info.Mode()),
		})

		return nil
	})

	return manifest, err
}

// CopyFile copies a file from src to dst, preserving permissions.
func CopyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	srcInfo, err := srcFile.Stat()
	if err != nil {
		return err
	}

	dstFile, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, srcInfo.Mode())
	if err != nil {
		return err
	}
	defer dstFile.Close()

	_, err = io.Copy(dstFile, srcFile)
	return err
}

// SyncHardlinks synchronizes hardlinks after a git pull.
// It adds new files and removes deleted files, but skips unlinked files and respects linklist patterns.
func SyncHardlinks(overlay Overlay, manifest *Manifest, filesBefore, filesAfter []string) error {
	beforeSet := make(map[string]bool)
	for _, f := range filesBefore {
		beforeSet[f] = true
	}

	afterSet := make(map[string]bool)
	for _, f := range filesAfter {
		afterSet[f] = true
	}

	// Build map of unlinked files
	unlinkedSet := make(map[string]bool)
	if manifest != nil {
		for _, entry := range manifest.Files {
			if entry.Unlinked {
				unlinkedSet[entry.RelativePath] = true
			}
		}
	}

	// Handle deleted files
	for _, f := range filesBefore {
		if !afterSet[f] {
			// Skip if file is unlinked
			if unlinkedSet[f] {
				continue
			}
			// Skip if file doesn't match linklist patterns
			if !ShouldLinkFile(overlay.LinkList, f) {
				continue
			}
			targetPath := filepath.Join(overlay.TargetDir, f)
			if err := os.Remove(targetPath); err != nil && !os.IsNotExist(err) {
				fmt.Fprintf(os.Stderr, "warning: failed to remove deleted file %s: %v\n", f, err)
			}
		}
	}

	// Handle new files
	for _, f := range filesAfter {
		if !beforeSet[f] {
			// Skip if file is unlinked
			if unlinkedSet[f] {
				continue
			}
			// Skip if file doesn't match linklist patterns
			if !ShouldLinkFile(overlay.LinkList, f) {
				continue
			}
			sourcePath := filepath.Join(overlay.RepoPath, f)
			targetPath := filepath.Join(overlay.TargetDir, f)

			// Ensure parent directory exists
			if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
				return err
			}

			// Create hardlink (skip if target exists)
			if _, err := os.Stat(targetPath); os.IsNotExist(err) {
				if err := os.Link(sourcePath, targetPath); err != nil {
					fmt.Fprintf(os.Stderr, "warning: failed to hardlink %s: %v\n", f, err)
				}
			}
		}
	}

	return nil
}

// CheckBrokenHardlinks checks for files in the manifest that are no longer hardlinked.
func CheckBrokenHardlinks(overlay Overlay, manifest *Manifest) []string {
	var broken []string

	for _, entry := range manifest.Files {
		// Skip intentionally unlinked files
		if entry.Unlinked {
			continue
		}

		repoPath := filepath.Join(overlay.RepoPath, entry.RelativePath)
		targetPath := filepath.Join(overlay.TargetDir, entry.RelativePath)

		repoInfo, err := os.Stat(repoPath)
		if err != nil {
			broken = append(broken, entry.RelativePath)
			continue
		}

		targetInfo, err := os.Stat(targetPath)
		if err != nil {
			broken = append(broken, entry.RelativePath)
			continue
		}

		// Check if they're the same file (hardlinked)
		if !os.SameFile(repoInfo, targetInfo) {
			broken = append(broken, entry.RelativePath)
		}
	}

	return broken
}

// UnlinkFile intentionally unlinks a file from the overlay, creating a new inode.
func UnlinkFile(overlay Overlay, manifest *Manifest, relPath string) error {
	targetPath := filepath.Join(overlay.TargetDir, relPath)

	// Check if file exists
	targetInfo, err := os.Stat(targetPath)
	if err != nil {
		return fmt.Errorf("file does not exist: %w", err)
	}

	// Find the file in the manifest
	var entry *FileEntry
	for i := range manifest.Files {
		if manifest.Files[i].RelativePath == relPath {
			entry = &manifest.Files[i]
			break
		}
	}

	if entry == nil {
		return fmt.Errorf("file %s not found in manifest", relPath)
	}

	if entry.Unlinked {
		return fmt.Errorf("file %s is already unlinked", relPath)
	}

	// Create a temporary copy
	tempPath := targetPath + ".tmp"
	if err := CopyFile(targetPath, tempPath); err != nil {
		return fmt.Errorf("failed to copy file: %w", err)
	}

	// Remove the hardlinked file
	if err := os.Remove(targetPath); err != nil {
		os.Remove(tempPath)
		return fmt.Errorf("failed to remove hardlink: %w", err)
	}

	// Rename temp file to target (creates new inode)
	if err := os.Rename(tempPath, targetPath); err != nil {
		return fmt.Errorf("failed to rename temp file: %w", err)
	}

	// Update manifest to mark as unlinked
	entry.Unlinked = true
	// Update inode info
	stat, ok := targetInfo.Sys().(*syscall.Stat_t)
	if ok {
		entry.Inode = stat.Ino
	}

	return nil
}

// RelinkFile relinks a previously unlinked file, copying content to repo first.
func RelinkFile(overlay Overlay, manifest *Manifest, relPath string) error {
	targetPath := filepath.Join(overlay.TargetDir, relPath)
	repoPath := filepath.Join(overlay.RepoPath, relPath)

	// Check if target file exists
	if _, err := os.Stat(targetPath); err != nil {
		return fmt.Errorf("target file does not exist: %w", err)
	}

	// Find the file in the manifest
	var entry *FileEntry
	for i := range manifest.Files {
		if manifest.Files[i].RelativePath == relPath {
			entry = &manifest.Files[i]
			break
		}
	}

	if entry == nil {
		return fmt.Errorf("file %s not found in manifest", relPath)
	}

	if !entry.Unlinked {
		return fmt.Errorf("file %s is not unlinked", relPath)
	}

	// Copy target file content to repo (user's version wins)
	if err := CopyFile(targetPath, repoPath); err != nil {
		return fmt.Errorf("failed to copy file to repo: %w", err)
	}

	// Remove target file
	if err := os.Remove(targetPath); err != nil {
		return fmt.Errorf("failed to remove target file: %w", err)
	}

	// Create hardlink
	if err := os.Link(repoPath, targetPath); err != nil {
		return fmt.Errorf("failed to create hardlink: %w", err)
	}

	// Update manifest
	entry.Unlinked = false
	info, err := os.Stat(repoPath)
	if err != nil {
		return err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if ok {
		entry.Inode = stat.Ino
	}
	entry.Size = info.Size()
	entry.Mode = uint32(info.Mode())

	return nil
}
