package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/meriksen/over/internal/git"
	"github.com/meriksen/over/internal/overlay"
	"github.com/spf13/cobra"
)

var (
	rmForce bool
)

var rmCmd = &cobra.Command{
	Use:   "rm <file>",
	Short: "Stop tracking a file in the overlay",
	Long: `Remove a file from overlay tracking. The file will be deleted from both
the target directory and the overlay repository.

By default, refuses to remove files with uncommitted changes.
Use -f to force removal even if the file is dirty.`,
	Args: cobra.ExactArgs(1),
	RunE: runRm,
}

func init() {
	rmCmd.Flags().BoolVarP(&rmForce, "force", "f", false, "Force removal even if file has uncommitted changes")
}

func runRm(cmd *cobra.Command, args []string) error {
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
	var manifestEntry *overlay.FileEntry

	for i := range overlays {
		ov := &overlays[i]
		manifest, err := overlay.LoadManifest(ov.TargetDir, ov.Name)
		if err != nil {
			continue
		}

		// Check if file is in this overlay
		for j, entry := range manifest.Files {
			targetFilePath := filepath.Join(ov.TargetDir, entry.RelativePath)
			if targetFilePath == filePath {
				foundOverlay = ov
				relPath = entry.RelativePath
				manifestEntry = &manifest.Files[j]
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

	// Check if file is dirty (has uncommitted changes)
	if !rmForce {
		changes, err := git.Status(foundOverlay.RepoPath)
		if err != nil {
			return fmt.Errorf("failed to get git status: %w", err)
		}

		for _, change := range changes {
			if change.Path == relPath {
				return fmt.Errorf("file %q has uncommitted changes (status: %s). Use -f to force removal", relPath, change.Status)
			}
		}
	}

	// Remove the file from the repository
	repoFilePath := filepath.Join(foundOverlay.RepoPath, relPath)
	if err := os.Remove(repoFilePath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove file from repository: %w", err)
	}

	// Remove the file from the target directory (if it's different from repo)
	targetFilePath := filepath.Join(foundOverlay.TargetDir, relPath)
	if err := os.Remove(targetFilePath); err != nil && !os.IsNotExist(err) {
		// If already removed, that's fine
		fmt.Fprintf(os.Stderr, "Warning: failed to remove target file: %v\n", err)
	}

	// Stage the deletion in git
	if err := git.Add(foundOverlay.RepoPath, relPath); err != nil {
		// Try to restore the file if git add fails
		fmt.Fprintf(os.Stderr, "Warning: failed to stage deletion in git: %v\n", err)
	}

	// Update manifest - remove the file entry
	manifest, err := overlay.LoadManifest(foundOverlay.TargetDir, foundOverlay.Name)
	if err != nil {
		return fmt.Errorf("failed to load manifest: %w", err)
	}

	// Remove the entry from manifest
	var updatedFiles []overlay.FileEntry
	for _, entry := range manifest.Files {
		if entry.RelativePath != manifestEntry.RelativePath {
			updatedFiles = append(updatedFiles, entry)
		}
	}
	manifest.Files = updatedFiles

	// Save updated manifest
	if err := overlay.SaveManifest(foundOverlay.TargetDir, foundOverlay.Name, manifest); err != nil {
		return fmt.Errorf("failed to save manifest: %w", err)
	}

	fmt.Printf("Removed %s from overlay %s\n", relPath, foundOverlay.Name)
	return nil
}
