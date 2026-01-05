package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/meriksen/over/internal/git"
	"github.com/meriksen/over/internal/overlay"
	"github.com/spf13/cobra"
)

var resetCmd = &cobra.Command{
	Use:   "reset <sha> <file>",
	Short: "Reset a file to a specific commit",
	Long: `Reset the content of a file to the version from a specific commit.

The SHA can be obtained from 'over info <file>' output.
This will update the file content to match the specified commit,
and since files are hardlinked, both the repository and target
directory versions will be updated.`,
	Args: cobra.ExactArgs(2),
	RunE: runReset,
}

func runReset(cmd *cobra.Command, args []string) error {
	sha := args[0]
	filePath := args[1]

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

	// Reset the file content using git show
	repoFilePath := filepath.Join(foundOverlay.RepoPath, relPath)
	if err := git.ShowFile(foundOverlay.RepoPath, sha, relPath, repoFilePath); err != nil {
		return fmt.Errorf("failed to reset file: %w", err)
	}

	fmt.Printf("Reset %s to commit %s\n", relPath, sha)
	return nil
}
