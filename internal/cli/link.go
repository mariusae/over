package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/meriksen/over/internal/overlay"
	"github.com/spf13/cobra"
)

var linkCmd = &cobra.Command{
	Use:   "link <file>",
	Short: "Re-link a previously unlinked file",
	Long: `Re-link a file that was previously unlinked.

This copies the current file content to the repository (your version wins),
then creates a hardlink back. The file will be synchronized during future
sync operations.`,
	Args: cobra.ExactArgs(1),
	RunE: runLink,
}

func runLink(cmd *cobra.Command, args []string) error {
	filePath := args[0]

	// Get current directory
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("could not determine current directory: %w", err)
	}

	// Make file path absolute
	if !filepath.IsAbs(filePath) {
		filePath = filepath.Join(cwd, filePath)
	}

	// Find which overlay this file belongs to
	overlays, err := overlay.FindReachableOverlays(cwd)
	if err != nil {
		return fmt.Errorf("failed to find overlays: %w", err)
	}

	if len(overlays) == 0 {
		return fmt.Errorf("no overlays found")
	}

	// Find the overlay containing this file
	var targetOverlay *overlay.Overlay
	var relPath string
	for _, ov := range overlays {
		rel, err := filepath.Rel(ov.TargetDir, filePath)
		if err == nil && !filepath.IsAbs(rel) && rel != ".." && rel != "." {
			targetOverlay = &ov
			relPath = rel
			break
		}
	}

	if targetOverlay == nil {
		return fmt.Errorf("file is not part of any overlay")
	}

	// Load manifest
	manifest, err := overlay.LoadManifest(targetOverlay.TargetDir, targetOverlay.Name)
	if err != nil {
		return fmt.Errorf("failed to load manifest: %w", err)
	}

	// Relink the file
	if err := overlay.RelinkFile(*targetOverlay, manifest, relPath); err != nil {
		return fmt.Errorf("failed to relink file: %w", err)
	}

	// Save updated manifest
	if err := overlay.SaveManifest(targetOverlay.TargetDir, targetOverlay.Name, manifest); err != nil {
		return fmt.Errorf("failed to save manifest: %w", err)
	}

	fmt.Printf("Linked: %s\n", relPath)
	fmt.Println("The file is now hardlinked to the repository and will be synchronized during sync.")

	return nil
}
