package cli

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"time"

	"github.com/meriksen/over/internal/git"
	"github.com/meriksen/over/internal/overlay"
	"github.com/spf13/cobra"
)

var commitAll bool

var commitCmd = &cobra.Command{
	Use:   "commit <file|layer>",
	Short: "Commit a file or all files in a layer",
	Long: `Commit changes in a layer.

# Commit a single file to its layer:
  over commit <file>

# Commit all files in a layer:
  over commit -a <layer>

Commit messages are auto-generated with metadata.`,
	Args: cobra.ExactArgs(1),
	RunE: runCommit,
}

func init() {
	commitCmd.Flags().BoolVarP(&commitAll, "all", "a", false, "Commit all files in the specified layer")
}

func runCommit(cmd *cobra.Command, args []string) error {
	arg := args[0]

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("could not determine current directory: %w", err)
	}

	var ov *overlay.Overlay
	var filePath string

	if commitAll {
		// -a flag: arg is a layer name, commit all files
		ov, err = overlay.FindOverlayByName(cwd, arg)
		if err != nil {
			return fmt.Errorf("failed to find overlay: %w", err)
		}
		if ov == nil {
			return fmt.Errorf("overlay %q not found in current directory or parents", arg)
		}
	} else {
		// No -a flag: arg is a file path, commit just that file
		// Find which overlay owns this file
		ov, err = overlay.FindOverlayForFile(cwd, arg)
		if err != nil {
			return fmt.Errorf("failed to find overlay for file: %w", err)
		}
		if ov == nil {
			return fmt.Errorf("file %q is not tracked by any overlay", arg)
		}
		filePath = arg
	}

	// Check if there are changes to commit
	hasChanges, err := git.HasUncommittedChanges(ov.RepoPath)
	if err != nil {
		return fmt.Errorf("failed to check for changes: %w", err)
	}
	if !hasChanges {
		fmt.Println("Nothing to commit, working tree clean")
		return nil
	}

	var message string
	if commitAll {
		// Generate message for all files
		message, err = generateCommitMessage(ov, nil)
		if err != nil {
			return fmt.Errorf("failed to generate commit message: %w", err)
		}
		fmt.Printf("Generated message:\n%s\n\n", message)

		// Create the commit (all tracked changes)
		if err := git.Commit(ov.RepoPath, message); err != nil {
			return fmt.Errorf("commit failed: %w", err)
		}

		fmt.Printf("Committed all changes in overlay %q\n", ov.Name)
	} else {
		// Commit single file
		// Convert to relative path from repo
		absFilePath := filePath
		if !filepath.IsAbs(filePath) {
			absFilePath = filepath.Join(cwd, filePath)
		}
		relPath, err := filepath.Rel(ov.TargetDir, absFilePath)
		if err != nil {
			return fmt.Errorf("failed to compute relative path: %w", err)
		}

		// Generate message for single file
		message, err = generateCommitMessage(ov, &relPath)
		if err != nil {
			return fmt.Errorf("failed to generate commit message: %w", err)
		}
		fmt.Printf("Generated message:\n%s\n\n", message)

		// Stage the file
		if err := git.Add(ov.RepoPath, relPath); err != nil {
			return fmt.Errorf("failed to stage file: %w", err)
		}

		// Create the commit (staged changes only)
		if err := git.CommitStaged(ov.RepoPath, message); err != nil {
			return fmt.Errorf("commit failed: %w", err)
		}

		fmt.Printf("Committed %q in overlay %q\n", relPath, ov.Name)
	}

	return nil
}

func generateCommitMessage(ov *overlay.Overlay, singleFile *string) (string, error) {
	// Get current user
	currentUser, err := user.Current()
	username := "unknown"
	if err == nil {
		username = currentUser.Username
	}

	// Get hostname
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "unknown"
	}

	// Get file changes
	changes, err := git.Status(ov.RepoPath)
	if err != nil {
		return "", err
	}

	// Build summary
	var added, modified, deleted int
	var fileList []string

	if singleFile != nil {
		// Single file mode: only include the specified file
		for _, c := range changes {
			if c.Path == *singleFile {
				switch c.Status {
				case git.StatusAdded:
					added++
				case git.StatusModified:
					modified++
				case git.StatusDeleted:
					deleted++
				}
				fileList = append(fileList, fmt.Sprintf("  %s %s", c.Status, c.Path))
				break
			}
		}
	} else {
		// All files mode
		for _, c := range changes {
			switch c.Status {
			case git.StatusAdded:
				added++
			case git.StatusModified:
				modified++
			case git.StatusDeleted:
				deleted++
			}
			fileList = append(fileList, fmt.Sprintf("  %s %s", c.Status, c.Path))
		}
	}

	// Build message
	var sb strings.Builder
	if singleFile != nil {
		sb.WriteString(fmt.Sprintf("Update %s: %s", ov.Name, *singleFile))
	} else {
		sb.WriteString(fmt.Sprintf("Update %s: ", ov.Name))

		var parts []string
		if added > 0 {
			parts = append(parts, fmt.Sprintf("%d added", added))
		}
		if modified > 0 {
			parts = append(parts, fmt.Sprintf("%d modified", modified))
		}
		if deleted > 0 {
			parts = append(parts, fmt.Sprintf("%d deleted", deleted))
		}
		sb.WriteString(strings.Join(parts, ", "))
	}

	sb.WriteString("\n\n")
	sb.WriteString(fmt.Sprintf("User: %s@%s\n", username, hostname))
	sb.WriteString(fmt.Sprintf("Time: %s\n", time.Now().Format(time.RFC3339)))
	sb.WriteString(fmt.Sprintf("Overlay: %s\n", ov.Name))
	sb.WriteString("\nFiles:\n")
	sb.WriteString(strings.Join(fileList, "\n"))

	return sb.String(), nil
}
