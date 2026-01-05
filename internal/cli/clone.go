package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/meriksen/over/internal/git"
	"github.com/meriksen/over/internal/overlay"
	"github.com/spf13/cobra"
)

var overwriteMode string

var cloneCmd = &cobra.Command{
	Use:   "clone [name] <giturl>",
	Short: "Clone a git repository as an overlay",
	Long: `Clone a git repository and hardlink its files to the current directory.

The repository is stored in $HOME/.local/over/<name> and files are hardlinked
to the current directory.

If no name is provided, it defaults to the repository name from the URL.

Use -o to handle file conflicts:
  -o=theirs  Overwrite local files with repository versions
  -o=ours    Keep local files, skip conflicting repository files`,
	Args: cobra.RangeArgs(1, 2),
	RunE: runClone,
}

func init() {
	cloneCmd.Flags().StringVarP(&overwriteMode, "overwrite", "o", "", "Conflict resolution: 'theirs' (use repo) or 'ours' (keep local)")
}

func runClone(cmd *cobra.Command, args []string) error {
	var repoName, gitURL string

	if len(args) == 1 {
		gitURL = args[0]
		repoName = git.ExtractRepoName(gitURL)
	} else {
		repoName = args[0]
		gitURL = args[1]
	}

	if repoName == "" {
		return fmt.Errorf("could not determine repository name from URL: %s", gitURL)
	}

	// Validate overwrite mode if provided
	if overwriteMode != "" && overwriteMode != "theirs" && overwriteMode != "ours" {
		return fmt.Errorf("invalid overwrite mode %q: must be 'theirs' or 'ours'", overwriteMode)
	}

	// Get the over storage directory
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("could not determine home directory: %w", err)
	}
	overStorageDir := filepath.Join(homeDir, ".local", "over")

	// Ensure the storage directory exists
	if err := os.MkdirAll(overStorageDir, 0755); err != nil {
		return fmt.Errorf("failed to create storage directory: %w", err)
	}

	repoPath := filepath.Join(overStorageDir, repoName)

	// Check if repo already exists
	if _, err := os.Stat(repoPath); err == nil {
		return fmt.Errorf("repository %q already exists at %s\nUse 'over clone <name> <url>' to specify a different name", repoName, repoPath)
	}

	// Get current working directory
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("could not determine current directory: %w", err)
	}

	fmt.Printf("Cloning %s to %s...\n", gitURL, repoPath)

	// Clone the repository
	if err := git.Clone(gitURL, repoPath); err != nil {
		return fmt.Errorf("failed to clone repository: %w", err)
	}

	// Check for file conflicts
	conflicts, err := overlay.CheckConflicts(repoPath, cwd)
	if err != nil {
		os.RemoveAll(repoPath)
		return fmt.Errorf("failed to check for conflicts: %w", err)
	}

	// Handle conflicts based on mode
	if len(conflicts) > 0 {
		if overwriteMode == "" {
			os.RemoveAll(repoPath)
			return fmt.Errorf("files already exist in target directory:\n  %v\nUse -o=theirs to overwrite or -o=ours to keep local files", conflicts)
		}

		if overwriteMode == "theirs" {
			fmt.Printf("Overwriting %d local files with repository versions...\n", len(conflicts))
			for _, conflict := range conflicts {
				targetPath := filepath.Join(cwd, conflict)
				if err := os.Remove(targetPath); err != nil {
					os.RemoveAll(repoPath)
					return fmt.Errorf("failed to remove local file %s: %w", conflict, err)
				}
			}
		} else if overwriteMode == "ours" {
			fmt.Printf("Keeping %d local files, copying to repository...\n", len(conflicts))
		}
	}

	fmt.Println("Creating hardlinks...")

	// Create hardlinks with conflict handling
	var manifest *overlay.Manifest
	if overwriteMode == "ours" {
		manifest, err = overlay.CreateHardlinksKeepLocal(repoPath, cwd, conflicts)
	} else {
		manifest, err = overlay.CreateHardlinks(repoPath, cwd)
	}
	if err != nil {
		os.RemoveAll(repoPath)
		return fmt.Errorf("failed to create hardlinks: %w", err)
	}

	// Save metadata
	ov := overlay.Overlay{
		Name:        repoName,
		SourceURL:   gitURL,
		RepoPath:    repoPath,
		TargetDir:   cwd,
		InstalledAt: time.Now(),
	}

	// Load existing registry and add this overlay
	registry, err := overlay.LoadRegistry(cwd)
	if err != nil {
		return fmt.Errorf("failed to load registry: %w", err)
	}
	registry.AddOverlay(ov)

	if err := overlay.SaveRegistry(cwd, registry); err != nil {
		return fmt.Errorf("failed to save registry: %w", err)
	}

	if err := overlay.SaveManifest(cwd, repoName, manifest); err != nil {
		return fmt.Errorf("failed to save manifest: %w", err)
	}

	fmt.Printf("Overlay %q installed successfully (%d files)\n", repoName, len(manifest.Files))
	return nil
}
