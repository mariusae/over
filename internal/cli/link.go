package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/meriksen/over/internal/overlay"
	"github.com/spf13/cobra"
)

var linkCmd = &cobra.Command{
	Use:   "link <layer> <file>",
	Short: "Link a file from a specific layer",
	Long: `Link a file from the specified layer.

This copies the current file content to the repository (your version wins),
then creates a hardlink back. The file will be synchronized during future
sync operations.

If a higher precedence layer already has this file linked, a warning will
be displayed and no action will be taken.`,
	Args: cobra.ExactArgs(2),
	RunE: runLink,
}

func runLink(cmd *cobra.Command, args []string) error {
	layerName := args[0]
	filePath := args[1]

	// Get current directory
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("could not determine current directory: %w", err)
	}

	// Load registry to find the layer
	registry, err := overlay.LoadRegistry(cwd)
	if err != nil {
		return fmt.Errorf("failed to load registry: %w", err)
	}

	targetOverlay := registry.FindOverlay(layerName)
	if targetOverlay == nil {
		return fmt.Errorf("layer %q not found", layerName)
	}

	// Make file path relative to the target directory
	var relPath string
	if filepath.IsAbs(filePath) {
		relPath, err = filepath.Rel(targetOverlay.TargetDir, filePath)
		if err != nil {
			return fmt.Errorf("file is not in target directory: %w", err)
		}
	} else {
		relPath = filePath
	}

	// Get all overlays sorted by precedence
	overlays, err := overlay.FindReachableOverlays(cwd)
	if err != nil {
		return fmt.Errorf("failed to find overlays: %w", err)
	}

	// Check if a higher precedence layer has this file
	for _, ov := range overlays {
		if ov.Name == targetOverlay.Name {
			break // We've reached our layer, stop checking
		}
		// Check if this file exists in the higher precedence layer's repository
		higherRepoPath := filepath.Join(ov.RepoPath, relPath)
		if _, err := os.Stat(higherRepoPath); err == nil {
			// File exists in higher layer, check if it's linked
			manifest, err := overlay.LoadManifest(ov.TargetDir, ov.Name)
			if err == nil {
				for _, entry := range manifest.Files {
					if entry.RelativePath == relPath && !entry.Unlinked {
						fmt.Fprintf(os.Stderr, "Warning: Layer %q (higher precedence) already has this file linked\n", ov.Name)
						fmt.Fprintf(os.Stderr, "The file from layer %q will not be visible.\n", layerName)
						return nil
					}
				}
			}
		}
	}

	// Load manifest
	manifest, err := overlay.LoadManifest(targetOverlay.TargetDir, targetOverlay.Name)
	if err != nil {
		manifest = &overlay.Manifest{Version: 1, Files: []overlay.FileEntry{}}
	}

	// Check if file exists in the layer's repository
	repoFilePath := filepath.Join(targetOverlay.RepoPath, relPath)
	if _, err := os.Stat(repoFilePath); os.IsNotExist(err) {
		return fmt.Errorf("file %q does not exist in layer %q", relPath, layerName)
	}

	// Check if the file is already in the manifest but marked as unlinked
	alreadyInManifest := false
	for _, entry := range manifest.Files {
		if entry.RelativePath == relPath {
			alreadyInManifest = true
			if !entry.Unlinked {
				fmt.Printf("File %q is already linked to layer %q\n", relPath, layerName)
				return nil
			}
			break
		}
	}

	// If the file was previously unlinked, relink it
	if alreadyInManifest {
		if err := overlay.RelinkFile(*targetOverlay, manifest, relPath); err != nil {
			return fmt.Errorf("failed to relink file: %w", err)
		}
	} else {
		// File not in manifest, create new hardlink
		targetPath := filepath.Join(targetOverlay.TargetDir, relPath)

		// Ensure parent directory exists
		if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
			return fmt.Errorf("failed to create directory: %w", err)
		}

		// If target file exists, copy it to repo first (user version wins)
		if _, err := os.Stat(targetPath); err == nil {
			if err := overlay.CopyFile(targetPath, repoFilePath); err != nil {
				return fmt.Errorf("failed to copy file to repository: %w", err)
			}
			if err := os.Remove(targetPath); err != nil {
				return fmt.Errorf("failed to remove target file: %w", err)
			}
		}

		// Create hardlink
		if err := os.Link(repoFilePath, targetPath); err != nil {
			return fmt.Errorf("failed to create hardlink: %w", err)
		}

		// Add to manifest
		info, err := os.Stat(repoFilePath)
		if err != nil {
			return err
		}
		manifest.AddFile(overlay.FileEntry{
			RelativePath: relPath,
			Inode:        0, // Will be set by manifest
			Size:         info.Size(),
			Mode:         uint32(info.Mode()),
		})
	}

	// Save updated manifest
	if err := overlay.SaveManifest(targetOverlay.TargetDir, targetOverlay.Name, manifest); err != nil {
		return fmt.Errorf("failed to save manifest: %w", err)
	}

	// Remove negative pattern from linklist if it exists
	negativePattern := "!" + relPath
	targetOverlay.LinkList = overlay.RemovePattern(targetOverlay.LinkList, negativePattern)

	// Ensure the file is covered by the linklist
	if !overlay.IsFileIncluded(targetOverlay.LinkList, relPath) {
		targetOverlay.LinkList = overlay.AddPattern(targetOverlay.LinkList, relPath)
		fmt.Printf("Added pattern to linklist: %s\n", relPath)
	}

	// Save updated registry
	if err := overlay.SaveRegistry(targetOverlay.TargetDir, registry); err != nil {
		return fmt.Errorf("failed to save registry: %w", err)
	}

	fmt.Printf("Linked: %s from layer %q\n", relPath, layerName)
	fmt.Println("The file is now hardlinked to the repository and will be synchronized during sync.")

	return nil
}
