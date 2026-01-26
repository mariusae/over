// Package cli provides the command-line interface for over.
package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "over",
	Short: "Manage Git repository layers with hardlinks",
	Long: `over is a tool for managing "layers" - Git repositories whose files
are hardlinked into your working directory.

Layers are cloned to $HOME/.local/over/<layer> and files are
hardlinked to the directory where 'over' is invoked. Lower order values
indicate higher precedence when multiple layers contain the same file.`,
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
	rootCmd.AddCommand(editCmd)
	rootCmd.AddCommand(showCmd)
	rootCmd.AddCommand(relinkCmd)
}
