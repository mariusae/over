package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/meriksen/over/internal/overlay"
	"github.com/spf13/cobra"
)

var infoCmd = &cobra.Command{
	Use:   "info <repository>",
	Short: "Show information about an overlay repository",
	Long: `Display detailed information about an overlay repository including
all tracked files and their metadata (size, modification time, permissions).`,
	Args: cobra.ExactArgs(1),
	RunE: runInfo,
}

func runInfo(cmd *cobra.Command, args []string) error {
	repoName := args[0]

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

	// Print overlay metadata
	fmt.Printf("Overlay: %s\n", ov.Name)
	fmt.Printf("Source:  %s\n", ov.SourceURL)
	fmt.Printf("Repo:    %s\n", ov.RepoPath)
	fmt.Printf("Target:  %s\n", ov.TargetDir)
	fmt.Printf("Cloned:  %s\n", ov.InstalledAt.Format(time.RFC3339))
	fmt.Println()

	// Load manifest
	manifest, err := overlay.LoadManifest(ov.TargetDir, ov.Name)
	if err != nil {
		return fmt.Errorf("failed to load manifest: %w", err)
	}

	fmt.Printf("Files (%d):\n", len(manifest.Files))
	fmt.Println()

	// Print file details
	for _, entry := range manifest.Files {
		repoFilePath := filepath.Join(ov.RepoPath, entry.RelativePath)
		targetFilePath := filepath.Join(ov.TargetDir, entry.RelativePath)

		// Get current file info from repo
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

		// Print file info
		fmt.Printf("  %s\n", entry.RelativePath)

		if repoErr == nil {
			fmt.Printf("    Size:    %d bytes\n", repoInfo.Size())
			fmt.Printf("    Mode:    %s\n", repoInfo.Mode())
			fmt.Printf("    ModTime: %s\n", repoInfo.ModTime().Format(time.RFC3339))
		}

		if status != "ok" {
			fmt.Printf("    Status:  %s\n", status)
		}

		fmt.Println()
	}

	return nil
}
