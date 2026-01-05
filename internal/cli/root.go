// Package cli provides the command-line interface for over.
package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "over",
	Short: "Manage Git repository overlays with hardlinks",
	Long: `over is a tool for managing "overlays" - Git repositories whose files
are hardlinked into your working directory.

Repositories are cloned to $HOME/.local/over/<repository> and files are
hardlinked to the directory where 'over' is invoked.`,
}

// Execute runs the root command.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.AddCommand(cloneCmd)
	rootCmd.AddCommand(syncCmd)
	rootCmd.AddCommand(statusCmd)
	rootCmd.AddCommand(addCmd)
	rootCmd.AddCommand(commitCmd)
	rootCmd.AddCommand(infoCmd)
	rootCmd.AddCommand(diffCmd)
	rootCmd.AddCommand(lsCmd)
	rootCmd.AddCommand(unlinkCmd)
	rootCmd.AddCommand(linkCmd)
}
