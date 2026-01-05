package cli

import (
	"fmt"
	"os"

	"github.com/meriksen/over/internal/git"
	"github.com/meriksen/over/internal/overlay"
	"github.com/meriksen/over/internal/ui"
	"github.com/spf13/cobra"
)

var syncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Synchronize all layers reachable from current directory",
	Long: `Synchronize all layers that are reachable from the current directory.

For each layer (in order of precedence):
1. Pulls changes from the remote repository
2. Pushes local commits (if any)
3. Re-syncs hardlinks (adds new files, removes deleted files)

Higher precedence layers are synced first to ensure proper file ordering.`,
	RunE: runSync,
}

func runSync(cmd *cobra.Command, args []string) error {
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

	for _, ov := range overlays {
		fmt.Printf("Syncing %s...\n", ov.Name)

		// Check for uncommitted changes and warn
		if hasChanges, _ := git.HasUncommittedChanges(ov.RepoPath); hasChanges {
			fmt.Println("  Warning: uncommitted changes exist, proceeding anyway")
		}

		// Get list of files BEFORE pull
		filesBefore, err := git.ListFiles(ov.RepoPath)
		if err != nil {
			fmt.Printf("  Warning: could not list files: %v\n", err)
			filesBefore = []string{}
		}

		// Git pull with spinner
		spinner := ui.NewBrailleSpinner("Pulling...")
		spinner.Start()
		if err := git.Pull(ov.RepoPath); err != nil {
			// Check if it's a conflict
			if hasConflicts, _ := git.HasConflicts(ov.RepoPath); hasConflicts {
				spinner.Stop("")
				fmt.Printf("  Error: merge conflicts detected in %s\n", ov.Name)
				fmt.Printf("  Resolve conflicts manually in: %s\n", ov.RepoPath)
				continue // Skip to next overlay
			}
			spinner.Stop(fmt.Sprintf("  Warning: pull failed: %v", err))
		} else {
			spinner.Stop("  ✓ Pulled")
		}

		// Git push (only if there are local commits)
		hasUnpushed, err := git.HasUnpushedCommits(ov.RepoPath)
		if err != nil {
			fmt.Printf("  Warning: could not check for unpushed commits: %v\n", err)
		} else if hasUnpushed {
			spinner = ui.NewBrailleSpinner("Pushing...")
			spinner.Start()
			if err := git.Push(ov.RepoPath); err != nil {
				spinner.Stop(fmt.Sprintf("  Warning: push failed: %v", err))
			} else {
				spinner.Stop("  ✓ Pushed")
			}
		}

		// Get list of files AFTER pull
		filesAfter, err := git.ListFiles(ov.RepoPath)
		if err != nil {
			fmt.Printf("  Warning: could not list files: %v\n", err)
			filesAfter = []string{}
		}

		// Load manifest to check for unlinked files
		manifest, err := overlay.LoadManifest(ov.TargetDir, ov.Name)
		if err != nil {
			fmt.Printf("  Warning: could not load manifest: %v\n", err)
			manifest = nil
		}

		// Re-sync hardlinks with spinner
		spinner = ui.NewBrailleSpinner("Syncing hardlinks...")
		spinner.Start()
		if err := overlay.SyncHardlinks(ov, manifest, filesBefore, filesAfter); err != nil {
			spinner.Stop(fmt.Sprintf("  Warning: hardlink sync failed: %v", err))
		} else {
			spinner.Stop("  ✓ Hardlinks synced")
		}

		fmt.Println("  Done")
	}

	return nil
}
