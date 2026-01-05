package cli

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/meriksen/over/internal/overlay"
	"github.com/spf13/cobra"
)

var diffCmd = &cobra.Command{
	Use:   "diff [layer]",
	Short: "Show diff of changes in layers",
	Long: `Show the diff of uncommitted changes in layers.

If no layer is specified, shows diffs for all reachable layers.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runDiff,
}

func runDiff(cmd *cobra.Command, args []string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("could not determine current directory: %w", err)
	}

	var overlays []overlay.Overlay

	if len(args) == 1 {
		// Specific repository
		ov, err := overlay.FindOverlayByName(cwd, args[0])
		if err != nil {
			return fmt.Errorf("failed to find overlay: %w", err)
		}
		if ov == nil {
			return fmt.Errorf("overlay %q not found", args[0])
		}
		overlays = []overlay.Overlay{*ov}
	} else {
		// All reachable overlays
		overlays, err = overlay.FindReachableOverlays(cwd)
		if err != nil {
			return fmt.Errorf("failed to find overlays: %w", err)
		}
	}

	if len(overlays) == 0 {
		fmt.Println("No overlays found.")
		return nil
	}

	for i, ov := range overlays {
		if len(overlays) > 1 {
			if i > 0 {
				fmt.Println()
			}
			fmt.Printf("=== %s ===\n", ov.Name)
		}

		gitCmd := exec.Command("git", "diff")
		gitCmd.Dir = ov.RepoPath
		gitCmd.Stdout = os.Stdout
		gitCmd.Stderr = os.Stderr
		if err := gitCmd.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: git diff failed for %s: %v\n", ov.Name, err)
		}
	}

	return nil
}
