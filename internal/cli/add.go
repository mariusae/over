package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/meriksen/over/internal/git"
	"github.com/meriksen/over/internal/overlay"
	"github.com/spf13/cobra"
)

var addCmd = &cobra.Command{
	Use:   "add <layer> <file>",
	Short: "Add a file to a layer",
	Long: `Add a file from the current directory to the specified layer.

The file will be:
1. Copied to the layer's repository location
2. Replaced with a hardlink to the layer copy
3. Staged for commit in the layer`,
	Args: cobra.ExactArgs(2),
	RunE: runAdd,
}

func runAdd(cmd *cobra.Command, args []string) error {
	repoName := args[0]
	filePath := args[1]

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("could not determine current directory: %w", err)
	}

	// Find the overlay
	ov, err := overlay.FindOverlayByName(cwd, repoName)
	if err != nil {
		return fmt.Errorf("failed to find overlay: %w", err)
	}
	if ov == nil {
		return fmt.Errorf("overlay %q not found in current directory or parents", repoName)
	}

	// Resolve the file path
	absFilePath := filePath
	if !filepath.IsAbs(filePath) {
		absFilePath = filepath.Join(cwd, filePath)
	}

	// Determine relative path within overlay target directory
	relPath, err := filepath.Rel(ov.TargetDir, absFilePath)
	if err != nil {
		return fmt.Errorf("could not determine relative path: %w", err)
	}
	if strings.HasPrefix(relPath, "..") {
		return fmt.Errorf("file %q is not within overlay target directory %q", filePath, ov.TargetDir)
	}

	// Target path in repo
	repoFilePath := filepath.Join(ov.RepoPath, relPath)

	// Check if file exists in working directory
	workFileInfo, err := os.Stat(absFilePath)
	if err != nil {
		return fmt.Errorf("file not found: %s", filePath)
	}

	// Check if file already exists in repo and is the same (hardlinked)
	repoFileInfo, repoErr := os.Stat(repoFilePath)
	if repoErr == nil && os.SameFile(repoFileInfo, workFileInfo) {
		// Already hardlinked, just git add
		fmt.Printf("File already hardlinked, staging for commit...\n")
	} else {
		// File is new or different - need to copy and create hardlink
		fmt.Printf("Adding %s to overlay %s...\n", relPath, repoName)

		// Ensure parent directory exists in repo
		if err := os.MkdirAll(filepath.Dir(repoFilePath), 0755); err != nil {
			return fmt.Errorf("failed to create directory in repo: %w", err)
		}

		// Copy file content to repo
		if err := overlay.CopyFile(absFilePath, repoFilePath); err != nil {
			return fmt.Errorf("failed to copy file to repo: %w", err)
		}

		// Remove original and create hardlink back
		if err := os.Remove(absFilePath); err != nil {
			return fmt.Errorf("failed to remove original file: %w", err)
		}

		if err := os.Link(repoFilePath, absFilePath); err != nil {
			// Try to restore the file if hardlink fails
			_ = overlay.CopyFile(repoFilePath, absFilePath)
			return fmt.Errorf("failed to create hardlink: %w", err)
		}
	}

	// Git add
	if err := git.Add(ov.RepoPath, relPath); err != nil {
		return fmt.Errorf("git add failed: %w", err)
	}

	fmt.Printf("Added %s to overlay %s\n", relPath, repoName)
	return nil
}
