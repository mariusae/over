package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/meriksen/over/internal/overlay"
	"github.com/spf13/cobra"
)

var unlinkCmd = &cobra.Command{
	Use:   "unlink <file>",
	Short: "Intentionally unlink a file from an overlay",
	Long: `Unlink a file from an overlay, creating a new inode.

This breaks the hardlink to the repository, allowing the file to be
modified independently. The file will remain unlinked during sync operations.`,
	Args: cobra.ExactArgs(1),
	RunE: runUnlink,
}

func runUnlink(cmd *cobra.Command, args []string) error {
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

	// Unlink the file
	if err := overlay.UnlinkFile(*targetOverlay, manifest, relPath); err != nil {
		return fmt.Errorf("failed to unlink file: %w", err)
	}

	// Save updated manifest
	if err := overlay.SaveManifest(targetOverlay.TargetDir, targetOverlay.Name, manifest); err != nil {
		return fmt.Errorf("failed to save manifest: %w", err)
	}

	// Add negative pattern to linklist
	registry, err := overlay.LoadRegistry(targetOverlay.TargetDir)
	if err != nil {
		return fmt.Errorf("failed to load registry: %w", err)
	}

	ov := registry.FindOverlay(targetOverlay.Name)
	if ov != nil {
		ov.LinkList = overlay.AddNegativePattern(ov.LinkList, relPath)
		if err := overlay.SaveRegistry(targetOverlay.TargetDir, registry); err != nil {
			return fmt.Errorf("failed to save registry: %w", err)
		}
	}

	fmt.Printf("Unlinked: %s\n", relPath)
	fmt.Println("The file now has its own inode and will not be linked during future operations.")
	fmt.Printf("Added exclusion pattern to linklist: !%s\n", relPath)

	return nil
}
