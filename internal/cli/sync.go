package cli

import (
	"fmt"
	"os"

	"github.com/meriksen/over/internal/git"
	"github.com/meriksen/over/internal/overlay"
	"github.com/spf13/cobra"
)

var syncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Synchronize all overlays reachable from current directory",
	Long: `Synchronize all overlays that are reachable from the current directory.

For each overlay:
1. Pulls changes from the remote repository
2. Pushes local commits (if any)
3. Re-syncs hardlinks (adds new files, removes deleted files)`,
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
		fmt.Println("No overlays found in current directory or parents.")
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

		// Git pull
		fmt.Println("  Pulling...")
		if err := git.Pull(ov.RepoPath); err != nil {
			// Check if it's a conflict
			if hasConflicts, _ := git.HasConflicts(ov.RepoPath); hasConflicts {
				fmt.Printf("  Error: merge conflicts detected in %s\n", ov.Name)
				fmt.Printf("  Resolve conflicts manually in: %s\n", ov.RepoPath)
				continue // Skip to next overlay
			}
			fmt.Printf("  Warning: pull failed: %v\n", err)
		}

		// Git push (only if there are local commits)
		hasUnpushed, err := git.HasUnpushedCommits(ov.RepoPath)
		if err != nil {
			fmt.Printf("  Warning: could not check for unpushed commits: %v\n", err)
		} else if hasUnpushed {
			fmt.Println("  Pushing...")
			if err := git.Push(ov.RepoPath); err != nil {
				fmt.Printf("  Warning: push failed: %v\n", err)
			}
		}

		// Get list of files AFTER pull
		filesAfter, err := git.ListFiles(ov.RepoPath)
		if err != nil {
			fmt.Printf("  Warning: could not list files: %v\n", err)
			filesAfter = []string{}
		}

		// Re-sync hardlinks
		if err := overlay.SyncHardlinks(ov, filesBefore, filesAfter); err != nil {
			fmt.Printf("  Warning: hardlink sync failed: %v\n", err)
		}

		fmt.Println("  Done")
	}

	return nil
}
