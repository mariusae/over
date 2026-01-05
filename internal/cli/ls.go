package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/meriksen/over/internal/git"
	"github.com/meriksen/over/internal/overlay"
	"github.com/spf13/cobra"
)

var (
	lsAll bool
)

var lsCmd = &cobra.Command{
	Use:   "ls",
	Short: "List all files from overlays",
	Long: `List all files from overlays reachable from the current directory.
For each file, displays: file path, repository name, status, and modification time.

Status indicators:
  ok          - File is properly hardlinked and unchanged
  modified    - File has local modifications
  unlinked    - File exists but is not hardlinked
  missing     - File is missing from target directory
  remote-mod  - File has been modified remotely (only with -a flag)

Use -a to also check for remote modifications (requires network fetch).`,
	RunE: runLs,
}

func init() {
	lsCmd.Flags().BoolVarP(&lsAll, "all", "a", false, "Include remote modification status")
}

type fileInfo struct {
	RelativePath string
	DisplayPath  string
	RepoName     string
	Status       string
	ModTime      time.Time
}

func runLs(cmd *cobra.Command, args []string) error {
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

	// Collect all files from all overlays
	var files []fileInfo

	for _, ov := range overlays {
		// Load manifest
		manifest, err := overlay.LoadManifest(ov.TargetDir, ov.Name)
		if err != nil {
			return fmt.Errorf("failed to load manifest for %s: %w", ov.Name, err)
		}

		// Get git status for this overlay
		changes, err := git.Status(ov.RepoPath)
		if err != nil {
			return fmt.Errorf("failed to get git status for %s: %w", ov.Name, err)
		}

		// Build a map of changed files
		changedFiles := make(map[string]git.ChangeStatus)
		for _, change := range changes {
			changedFiles[change.Path] = change.Status
		}

		// Check for remote changes if -a flag is set
		var remoteChanges map[string]bool
		if lsAll {
			remoteChanges, err = getRemoteChanges(ov.RepoPath)
			if err != nil {
				// Don't fail, just warn
				fmt.Fprintf(os.Stderr, "Warning: failed to check remote changes for %s: %v\n", ov.Name, err)
				remoteChanges = make(map[string]bool)
			}
		}

		// Process each file
		for _, entry := range manifest.Files {
			repoFilePath := filepath.Join(ov.RepoPath, entry.RelativePath)
			targetFilePath := filepath.Join(ov.TargetDir, entry.RelativePath)

			// Get current file info
			repoInfo, repoErr := os.Stat(repoFilePath)
			targetInfo, targetErr := os.Stat(targetFilePath)

			// Determine status
			status := "ok"
			var modTime time.Time

			if targetErr != nil {
				status = "missing"
				if repoErr == nil {
					modTime = repoInfo.ModTime()
				}
			} else if repoErr != nil {
				status = "missing-repo"
				modTime = targetInfo.ModTime()
			} else {
				modTime = repoInfo.ModTime()

				// Check if hardlinked
				if !os.SameFile(repoInfo, targetInfo) {
					status = "unlinked"
				} else if changeStatus, changed := changedFiles[entry.RelativePath]; changed {
					switch changeStatus {
					case git.StatusModified:
						status = "modified"
					case git.StatusAdded:
						status = "added"
					case git.StatusDeleted:
						status = "deleted"
					case git.StatusUntracked:
						status = "untracked"
					default:
						status = "modified"
					}
				}

				// Check remote changes if -a flag
				if lsAll && remoteChanges[entry.RelativePath] {
					if status == "ok" {
						status = "remote-mod"
					} else {
						status = status + "+remote-mod"
					}
				}
			}

			// Compute display path: absolute by default, relative if invoked from same directory
			displayPath := targetFilePath // Default to absolute path

			// If cwd is within target directory, show relative path from cwd
			if relFromCwd, err := filepath.Rel(cwd, targetFilePath); err == nil && !filepath.IsAbs(relFromCwd) {
				displayPath = relFromCwd
			}

			files = append(files, fileInfo{
				RelativePath: entry.RelativePath,
				DisplayPath:  displayPath,
				RepoName:     ov.Name,
				Status:       status,
				ModTime:      modTime,
			})
		}
	}

	// Sort files by display path
	sort.Slice(files, func(i, j int) bool {
		return files[i].DisplayPath < files[j].DisplayPath
	})

	// Print files
	for _, f := range files {
		fmt.Printf("%-50s %-20s %-20s %s\n",
			f.DisplayPath,
			f.RepoName,
			f.Status,
			f.ModTime.Format("2006-01-02 15:04:05"))
	}

	return nil
}

// getRemoteChanges fetches from remote and returns files that differ from remote
func getRemoteChanges(repoPath string) (map[string]bool, error) {
	// Fetch from remote
	if err := git.Fetch(repoPath); err != nil {
		return nil, fmt.Errorf("failed to fetch: %w", err)
	}

	// Get list of files that differ between HEAD and origin/HEAD
	changes, err := git.DiffRemote(repoPath)
	if err != nil {
		return nil, fmt.Errorf("failed to diff with remote: %w", err)
	}

	result := make(map[string]bool)
	for _, change := range changes {
		result[change] = true
	}
	return result, nil
}
