package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

	"github.com/meriksen/over/internal/overlay"
	"github.com/spf13/cobra"
)

var editCmd = &cobra.Command{
	Use:   "edit <layer>",
	Short: "Edit a layer's linklist patterns",
	Long: `Edit the linklist patterns for a layer using $EDITOR.

The linklist controls which files from the layer are linked to the target directory.
After editing, the layer will be automatically relinked according to the new patterns.

Pattern syntax (same as .gitignore):
  *         - Match all files
  *.go      - Match all .go files
  !test.go  - Exclude test.go from previous matches
  docs/**   - Match everything in docs/ directory`,
	Args: cobra.ExactArgs(1),
	RunE: runEdit,
}

func runEdit(cmd *cobra.Command, args []string) error {
	layerName := args[0]

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("could not determine current directory: %w", err)
	}

	// Load registry to find the layer
	registry, err := overlay.LoadRegistry(cwd)
	if err != nil {
		return fmt.Errorf("failed to load registry: %w", err)
	}

	ov := registry.FindOverlay(layerName)
	if ov == nil {
		return fmt.Errorf("layer %q not found", layerName)
	}

	// Get editor from environment
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vi" // Default to vi if EDITOR not set
	}

	// Create temporary file with current linklist
	tempFile, err := os.CreateTemp("", "over-linklist-*.txt")
	if err != nil {
		return fmt.Errorf("failed to create temporary file: %w", err)
	}
	defer os.Remove(tempFile.Name())

	// Write current linklist to temp file with helpful comments
	header := `# Linklist patterns for layer: ` + layerName + `
#
# Pattern syntax (same as .gitignore):
#   *         - Match all files
#   *.go      - Match all .go files
#   !test.go  - Exclude test.go from previous matches
#   docs/**   - Match everything in docs/ directory
#
# Lines starting with # are comments and will be ignored.
# Empty lines will also be ignored.
#
# Current patterns:
`
	if _, err := tempFile.WriteString(header); err != nil {
		return fmt.Errorf("failed to write to temporary file: %w", err)
	}

	linkListStr := overlay.FormatLinkList(ov.LinkList)
	if linkListStr == "" {
		linkListStr = "# No patterns defined (no files will be linked)\n# Add patterns below to link files:\n"
	}
	if _, err := tempFile.WriteString(linkListStr + "\n"); err != nil {
		return fmt.Errorf("failed to write to temporary file: %w", err)
	}
	tempFile.Close()

	// Launch editor
	editorCmd := exec.Command(editor, tempFile.Name())
	editorCmd.Stdin = os.Stdin
	editorCmd.Stdout = os.Stdout
	editorCmd.Stderr = os.Stderr

	if err := editorCmd.Run(); err != nil {
		return fmt.Errorf("editor exited with error: %w", err)
	}

	// Read updated linklist
	content, err := os.ReadFile(tempFile.Name())
	if err != nil {
		return fmt.Errorf("failed to read edited file: %w", err)
	}

	// Parse the linklist (skip comments and empty lines)
	newLinkList := overlay.ParseLinkList(string(content))

	// Validate the linklist
	if err := overlay.ValidateLinkList(newLinkList); err != nil {
		return fmt.Errorf("invalid linklist patterns: %w", err)
	}

	// Update the overlay's linklist
	ov.LinkList = newLinkList

	// Save updated registry
	if err := overlay.SaveRegistry(cwd, registry); err != nil {
		return fmt.Errorf("failed to save registry: %w", err)
	}

	fmt.Println("Linklist updated. Relinking files...")

	// Relink the layer
	if err := relinkLayer(cwd, *ov); err != nil {
		return fmt.Errorf("failed to relink layer: %w", err)
	}

	fmt.Printf("Layer %q relinked successfully\n", layerName)
	return nil
}

// relinkLayer recreates all hardlinks for a layer based on its current linklist.
func relinkLayer(targetDir string, ov overlay.Overlay) error {
	// Load current manifest
	manifest, err := overlay.LoadManifest(targetDir, ov.Name)
	if err != nil {
		manifest = &overlay.Manifest{Version: 1, Files: []overlay.FileEntry{}}
	}

	// Build a set of currently linked files (excluding unlinked ones)
	currentlyLinked := make(map[string]bool)
	for _, entry := range manifest.Files {
		if !entry.Unlinked {
			currentlyLinked[entry.RelativePath] = true
		}
	}

	// Get all files in the repository
	var repoFiles []string
	err = filepath.WalkDir(ov.RepoPath, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && d.Name() == ".git" {
			return filepath.SkipDir
		}
		if !d.IsDir() && d.Type()&os.ModeSymlink == 0 {
			relPath, err := filepath.Rel(ov.RepoPath, path)
			if err != nil {
				return err
			}
			repoFiles = append(repoFiles, relPath)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to walk repository: %w", err)
	}

	// Determine which files should be linked according to new linklist
	shouldBeLinked := make(map[string]bool)
	for _, f := range repoFiles {
		if overlay.ShouldLinkFile(ov.LinkList, f) {
			shouldBeLinked[f] = true
		}
	}

	// Remove links that should no longer be linked
	for f := range currentlyLinked {
		if !shouldBeLinked[f] {
			targetPath := filepath.Join(targetDir, f)
			// Check if file exists and is owned by this layer
			if info, err := os.Stat(targetPath); err == nil {
				repoPath := filepath.Join(ov.RepoPath, f)
				if repoInfo, err := os.Stat(repoPath); err == nil {
					// Only remove if it's hardlinked to this layer
					if os.SameFile(info, repoInfo) {
						if err := os.Remove(targetPath); err != nil && !os.IsNotExist(err) {
							fmt.Fprintf(os.Stderr, "warning: failed to remove %s: %v\n", f, err)
						}
					} else {
						fmt.Fprintf(os.Stderr, "warning: %s is not linked to %s, skipping removal\n", f, ov.Name)
					}
				}
			}
		}
	}

	// Create new manifest
	newManifest := &overlay.Manifest{Version: 1, Files: []overlay.FileEntry{}}

	// Add links for files that should be linked
	for _, f := range repoFiles {
		if !shouldBeLinked[f] {
			continue
		}

		sourcePath := filepath.Join(ov.RepoPath, f)
		targetPath := filepath.Join(targetDir, f)

		// Ensure parent directory exists
		if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
			return fmt.Errorf("failed to create directory: %w", err)
		}

		// Check if target already exists
		if info, err := os.Stat(targetPath); err == nil {
			// File exists - check if it's already hardlinked correctly
			if repoInfo, err := os.Stat(sourcePath); err == nil {
				if os.SameFile(info, repoInfo) {
					// Already hardlinked correctly, just add to manifest
					if stat, ok := info.Sys().(*syscall.Stat_t); ok {
						newManifest.AddFile(overlay.FileEntry{
							RelativePath: f,
							Inode:        stat.Ino,
							Size:         info.Size(),
							Mode:         uint32(info.Mode()),
						})
					}
					continue
				} else {
					// File exists but not hardlinked to this layer
					fmt.Fprintf(os.Stderr, "warning: %s exists but is not from layer %s, skipping\n", f, ov.Name)
					continue
				}
			}
		}

		// Create hardlink
		if err := os.Link(sourcePath, targetPath); err != nil {
			if os.IsExist(err) {
				fmt.Fprintf(os.Stderr, "warning: %s already exists, skipping\n", f)
			} else {
				fmt.Fprintf(os.Stderr, "warning: failed to link %s: %v\n", f, err)
			}
			continue
		}

		// Add to manifest
		info, err := os.Stat(sourcePath)
		if err != nil {
			return err
		}
		if stat, ok := info.Sys().(*syscall.Stat_t); ok {
			newManifest.AddFile(overlay.FileEntry{
				RelativePath: f,
				Inode:        stat.Ino,
				Size:         info.Size(),
				Mode:         uint32(info.Mode()),
			})
		}
	}

	// Save updated manifest
	if err := overlay.SaveManifest(targetDir, ov.Name, newManifest); err != nil {
		return fmt.Errorf("failed to save manifest: %w", err)
	}

	return nil
}
