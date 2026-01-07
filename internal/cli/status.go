package cli

import (
	"fmt"
	"os"

	"github.com/meriksen/over/internal/git"
	"github.com/meriksen/over/internal/overlay"
	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show status of layers reachable from current directory",
	Long: `Show the status of all layers that are reachable from the current directory.

This includes any layers installed in the current directory or any parent directory.
For each layer, it shows changed, new, and deleted files.
Layers are displayed in order of precedence (lower order = higher precedence).`,
	RunE: runStatus,
}

func runStatus(cmd *cobra.Command, args []string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("could not determine current directory: %w", err)
	}

	overlays, err := overlay.FindReachableOverlays(cwd)
	if err != nil {
		return fmt.Errorf("failed to find overlays: %w", err)
	}

	if len(overlays) == 0 {
		fmt.Println("No layers found in current directory or parents.")
		return nil
	}

	for i, ov := range overlays {
		if i > 0 {
			fmt.Println()
		}

		fmt.Printf("Layer: %s\n", ov.Name)
		fmt.Printf("  Repository: %s\n", ov.RepoPath)
		fmt.Printf("  Target: %s\n", ov.TargetDir)
		fmt.Printf("  Order: %d\n", ov.Order)

		changes, err := git.Status(ov.RepoPath)
		if err != nil {
			fmt.Printf("  Error getting status: %v\n", err)
			continue
		}

		if len(changes) == 0 {
			fmt.Println("  Status: clean")
		} else {
			fmt.Println("  Changes:")
			for _, change := range changes {
				fmt.Printf("    %s %s\n", change.Status, change.Path)
			}
		}

		// Check for broken hardlinks and unlinked files
		manifest, err := overlay.LoadManifest(ov.TargetDir, ov.Name)
		if err == nil && len(manifest.Files) > 0 {
			// Show unlinked files
			unlinked := []string{}
			for _, entry := range manifest.Files {
				if entry.Unlinked {
					unlinked = append(unlinked, entry.RelativePath)
				}
			}
			if len(unlinked) > 0 {
				fmt.Println("  Unlinked files:")
				for _, f := range unlinked {
					fmt.Printf("    ~ %s\n", f)
				}
			}

			// Show broken hardlinks
			broken := overlay.CheckBrokenHardlinks(ov, manifest)
			if len(broken) > 0 {
				fmt.Println("  Broken hardlinks:")
				for _, f := range broken {
					fmt.Printf("    ! %s\n", f)
				}
			}
		}
	}

	return nil
}
