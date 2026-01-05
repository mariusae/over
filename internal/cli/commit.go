package cli

import (
	"fmt"
	"os"
	"os/user"
	"strings"
	"time"

	"github.com/meriksen/over/internal/git"
	"github.com/meriksen/over/internal/overlay"
	"github.com/spf13/cobra"
)

var autoMessage bool

var commitCmd = &cobra.Command{
	Use:   "commit <layer> [message]",
	Short: "Commit changes in a layer",
	Long: `Create a commit in the specified layer.

This commits all tracked file changes (equivalent to git commit -am).

Use -a to auto-generate a commit message with metadata.`,
	Args: cobra.RangeArgs(1, 2),
	RunE: runCommit,
}

func init() {
	commitCmd.Flags().BoolVarP(&autoMessage, "auto", "a", false, "Auto-generate commit message with metadata")
}

func runCommit(cmd *cobra.Command, args []string) error {
	repoName := args[0]
	var message string

	if len(args) == 2 {
		message = args[1]
	} else if !autoMessage {
		return fmt.Errorf("either provide a message or use -a for auto-generated message")
	}

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("could not determine current directory: %w", err)
	}

	// Find the overlay
	ov, err := overlay.FindOverlayByName(cwd, repoName)
	if err != nil {
		return fmt.Errorf("failed to find overlay: %w", err)
	}
	if ov == nil {
		return fmt.Errorf("overlay %q not found in current directory or parents", repoName)
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

	// Auto-generate message if -a flag is set
	if autoMessage {
		message, err = generateCommitMessage(ov)
		if err != nil {
			return fmt.Errorf("failed to generate commit message: %w", err)
		}
		fmt.Printf("Generated message:\n%s\n\n", message)
	}

	// Create the commit
	if err := git.Commit(ov.RepoPath, message); err != nil {
		return fmt.Errorf("commit failed: %w", err)
	}

	fmt.Printf("Committed changes in overlay %q\n", repoName)
	return nil
}

func generateCommitMessage(ov *overlay.Overlay) (string, error) {
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

	// Build message
	var sb strings.Builder
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

	sb.WriteString("\n\n")
	sb.WriteString(fmt.Sprintf("User: %s@%s\n", username, hostname))
	sb.WriteString(fmt.Sprintf("Time: %s\n", time.Now().Format(time.RFC3339)))
	sb.WriteString(fmt.Sprintf("Overlay: %s\n", ov.Name))
	sb.WriteString("\nFiles:\n")
	sb.WriteString(strings.Join(fileList, "\n"))

	return sb.String(), nil
}
