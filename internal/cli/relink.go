package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/meriksen/over/internal/overlay"
	"github.com/spf13/cobra"
)

var relinkCmd = &cobra.Command{
	Use:   "relink <file>",
	Short: "Relink a broken hardlink",
	Long: `Relink a file that has a broken hardlink.

This copies the current checked out file content to the repository (your version
replaces the repository version), then creates a new hardlink. Use this when a
file's hardlink has been broken (e.g., by an editor that replaces files instead
of modifying in place).`,
	Args: cobra.ExactArgs(1),
	RunE: runRelink,
}

func runRelink(cmd *cobra.Command, args []string) error {
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
			// Check if this file is in this overlay's manifest
			manifest, err := overlay.LoadManifest(ov.TargetDir, ov.Name)
			if err != nil {
				continue
			}
			for _, entry := range manifest.Files {
				if entry.RelativePath == rel {
					targetOverlay = &ov
					relPath = rel
					break
				}
			}
			if targetOverlay != nil {
				break
			}
		}
	}

	if targetOverlay == nil {
		return fmt.Errorf("file is not tracked by any overlay")
	}

	// Load manifest
	manifest, err := overlay.LoadManifest(targetOverlay.TargetDir, targetOverlay.Name)
	if err != nil {
		return fmt.Errorf("failed to load manifest: %w", err)
	}

	// Check if the file has a broken hardlink
	if !overlay.IsBrokenHardlink(*targetOverlay, manifest, relPath) {
		return fmt.Errorf("file %q does not have a broken hardlink", relPath)
	}

	// Relink the file (checked out version replaces repository version)
	if err := overlay.RelinkBrokenFile(*targetOverlay, manifest, relPath); err != nil {
		return fmt.Errorf("failed to relink file: %w", err)
	}

	// Save updated manifest
	if err := overlay.SaveManifest(targetOverlay.TargetDir, targetOverlay.Name, manifest); err != nil {
		return fmt.Errorf("failed to save manifest: %w", err)
	}

	fmt.Printf("Relinked: %s\n", relPath)
	fmt.Println("The checked out version has been copied to the repository and hardlinked.")

	return nil
}
