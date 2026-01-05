package cli

import (
	"fmt"
	"os"

	"github.com/meriksen/over/internal/overlay"
	"github.com/spf13/cobra"
)

var showCmd = &cobra.Command{
	Use:   "show <layer>",
	Short: "Show information about a layer",
	Long: `Display detailed information about a layer including:
  - Layer name and source repository
  - Installation path and target directory
  - Installation time and precedence order
  - Linklist patterns controlling which files are linked`,
	Args: cobra.ExactArgs(1),
	RunE: runShow,
}

func runShow(cmd *cobra.Command, args []string) error {
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

	// Load manifest to get file count
	manifest, err := overlay.LoadManifest(cwd, layerName)
	var linkedCount, unlinkedCount int
	if err == nil {
		for _, entry := range manifest.Files {
			if entry.Unlinked {
				unlinkedCount++
			} else {
				linkedCount++
			}
		}
	}

	// Display layer information
	fmt.Printf("Layer: %s\n", ov.Name)
	fmt.Printf("Source URL: %s\n", ov.SourceURL)
	fmt.Printf("Repository Path: %s\n", ov.RepoPath)
	fmt.Printf("Target Directory: %s\n", ov.TargetDir)
	fmt.Printf("Installed At: %s\n", ov.InstalledAt.Format("2006-01-02 15:04:05"))
	fmt.Printf("Order (Precedence): %d (lower = higher precedence)\n", ov.Order)

	if manifest != nil {
		fmt.Printf("Linked Files: %d\n", linkedCount)
		if unlinkedCount > 0 {
			fmt.Printf("Unlinked Files: %d\n", unlinkedCount)
		}
	}

	// Display linklist
	fmt.Printf("\nLinklist Patterns:\n")
	if len(ov.LinkList) == 0 {
		fmt.Println("  (none - no files will be linked)")
		fmt.Println("  Use 'over edit' to add patterns")
	} else {
		for _, pattern := range ov.LinkList {
			explanation := overlay.ExplainPattern(pattern)
			fmt.Printf("  %s  # %s\n", pattern, explanation)
		}
	}

	return nil
}
