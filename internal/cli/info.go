package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/meriksen/over/internal/git"
	"github.com/meriksen/over/internal/overlay"
	"github.com/spf13/cobra"
)

var infoCmd = &cobra.Command{
	Use:   "info <file>",
	Short: "Show detailed information about a file",
	Long: `Display detailed information about a specific file including its repository,
modification time, and full git history (shortlog).

The file must be tracked by an overlay repository.`,
	Args: cobra.ExactArgs(1),
	RunE: runInfo,
}

func runInfo(cmd *cobra.Command, args []string) error {
	filePath := args[0]

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("could not determine current directory: %w", err)
	}

	// Make file path absolute
	if !filepath.IsAbs(filePath) {
		filePath = filepath.Join(cwd, filePath)
	}

	// Find which overlay contains this file
	overlays, err := overlay.FindReachableOverlays(cwd)
	if err != nil {
		return fmt.Errorf("failed to find overlays: %w", err)
	}

	var foundOverlay *overlay.Overlay
	var relPath string

	for i := range overlays {
		ov := &overlays[i]
		manifest, err := overlay.LoadManifest(ov.TargetDir, ov.Name)
		if err != nil {
			continue
		}

		// Check if file is in this overlay
		for _, entry := range manifest.Files {
			targetFilePath := filepath.Join(ov.TargetDir, entry.RelativePath)
			if targetFilePath == filePath {
				foundOverlay = ov
				relPath = entry.RelativePath
				break
			}
		}

		if foundOverlay != nil {
			break
		}
	}

	if foundOverlay == nil {
		return fmt.Errorf("file %q is not tracked by any overlay", filePath)
	}

	// Get file info
	repoFilePath := filepath.Join(foundOverlay.RepoPath, relPath)
	targetFilePath := filepath.Join(foundOverlay.TargetDir, relPath)

	repoInfo, repoErr := os.Stat(repoFilePath)
	targetInfo, targetErr := os.Stat(targetFilePath)

	// Determine status
	status := "ok"
	if repoErr != nil {
		status = "missing-repo"
	} else if targetErr != nil {
		status = "missing-target"
	} else if !os.SameFile(repoInfo, targetInfo) {
		status = "unlinked"
	}

	// Print file information
	fmt.Printf("File: %s\n", relPath)
	fmt.Printf("Repository: %s\n", foundOverlay.Name)
	fmt.Printf("Status: %s\n", status)
	fmt.Println()

	if repoErr == nil {
		fmt.Printf("Size: %d bytes\n", repoInfo.Size())
		fmt.Printf("Mode: %s\n", repoInfo.Mode())
		fmt.Printf("Modified: %s\n", repoInfo.ModTime().Format(time.RFC3339))
		fmt.Println()
	}

	// Get git history for the file
	history, err := git.FileLog(foundOverlay.RepoPath, relPath)
	if err != nil {
		return fmt.Errorf("failed to get file history: %w", err)
	}

	if len(history) == 0 {
		fmt.Println("No commit history found for this file.")
		return nil
	}

	fmt.Printf("History (%d commits):\n", len(history))
	fmt.Println()
	for _, entry := range history {
		fmt.Printf("  %s  %s  %s\n",
			entry.Hash,
			entry.Date.Format("2006-01-02 15:04"),
			entry.Author)
		fmt.Printf("      %s\n", entry.Subject)
		fmt.Println()
	}

	return nil
}
