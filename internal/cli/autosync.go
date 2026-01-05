package cli

import (
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/meriksen/over/internal/git"
	"github.com/meriksen/over/internal/overlay"
	"github.com/spf13/cobra"
)

var (
	autosyncPollInterval  time.Duration
	autosyncDebounceDelay time.Duration
	autosyncVerbose       bool
)

var autosyncCmd = &cobra.Command{
	Use:   "autosync",
	Short: "Watch for changes and automatically sync layers",
	Long: `Watch for file changes and automatically sync layers.

This command:
1. Watches all linked files for local changes
2. Periodically polls for remote changes (default: every 30s)
3. Watches configuration files for new layers or linklist changes
4. Automatically syncs when changes are detected

Local file changes in the target directory are detected via filesystem watchers.
Remote changes are detected by periodically fetching and comparing with HEAD.

Press Ctrl+C to stop watching.`,
	RunE: runAutosync,
}

func init() {
	autosyncCmd.Flags().DurationVarP(&autosyncPollInterval, "poll-interval", "p", 30*time.Second, "How often to check for remote changes")
	autosyncCmd.Flags().DurationVarP(&autosyncDebounceDelay, "debounce", "d", 500*time.Millisecond, "How long to wait after a change before syncing")
	autosyncCmd.Flags().BoolVarP(&autosyncVerbose, "verbose", "v", false, "Show detailed output")
}

func runAutosync(cmd *cobra.Command, args []string) error {
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

	fmt.Printf("Starting autosync for %d layer(s):\n", len(overlays))
	for _, ov := range overlays {
		fmt.Printf("  - %s\n", ov.Name)
	}
	fmt.Println()

	// State for tracking pending syncs
	var syncMu sync.Mutex
	pendingSyncs := make(map[string]bool)

	// Logger function
	logger := func(format string, args ...interface{}) {
		if autosyncVerbose {
			timestamp := time.Now().Format("15:04:05")
			fmt.Printf("[%s] %s\n", timestamp, fmt.Sprintf(format, args...))
		}
	}

	// Create watcher configuration
	config := overlay.WatcherConfig{
		PollInterval:  autosyncPollInterval,
		DebounceDelay: autosyncDebounceDelay,
		Logger:        logger,

		OnLocalChange: func(ov overlay.Overlay, paths []string) {
			syncMu.Lock()
			if pendingSyncs[ov.Name] {
				syncMu.Unlock()
				return
			}
			pendingSyncs[ov.Name] = true
			syncMu.Unlock()

			defer func() {
				syncMu.Lock()
				delete(pendingSyncs, ov.Name)
				syncMu.Unlock()
			}()

			timestamp := time.Now().Format("15:04:05")
			uniquePaths := uniqueStrings(paths)
			if len(uniquePaths) == 1 {
				fmt.Printf("[%s] Local change detected in %s: %s\n", timestamp, ov.Name, uniquePaths[0])
			} else {
				fmt.Printf("[%s] Local changes detected in %s: %d files\n", timestamp, ov.Name, len(uniquePaths))
				if autosyncVerbose {
					for _, p := range uniquePaths {
						fmt.Printf("         - %s\n", p)
					}
				}
			}

			// Sync hardlinks to ensure consistency
			syncLocalChanges(ov, logger)
		},

		OnRemoteChange: func(ov overlay.Overlay) {
			syncMu.Lock()
			if pendingSyncs[ov.Name] {
				syncMu.Unlock()
				return
			}
			pendingSyncs[ov.Name] = true
			syncMu.Unlock()

			defer func() {
				syncMu.Lock()
				delete(pendingSyncs, ov.Name)
				syncMu.Unlock()
			}()

			// Check if there are remote changes
			hasRemote, err := checkRemoteChanges(ov, logger)
			if err != nil {
				logger("Error checking remote for %s: %v", ov.Name, err)
				return
			}

			if hasRemote {
				timestamp := time.Now().Format("15:04:05")
				fmt.Printf("[%s] Remote changes detected in %s, syncing...\n", timestamp, ov.Name)
				syncRemoteChanges(ov, logger)
			}
		},

		OnConfigChange: func() {
			timestamp := time.Now().Format("15:04:05")
			fmt.Printf("[%s] Configuration changed, refreshing...\n", timestamp)
		},

		OnError: func(err error) {
			timestamp := time.Now().Format("15:04:05")
			fmt.Fprintf(os.Stderr, "[%s] Error: %v\n", timestamp, err)
		},
	}

	// Create and start the watcher
	watcher, err := overlay.NewWatcher(cwd, overlays, config)
	if err != nil {
		return fmt.Errorf("failed to create watcher: %w", err)
	}

	if err := watcher.Start(); err != nil {
		return fmt.Errorf("failed to start watcher: %w", err)
	}

	if autosyncVerbose {
		paths := watcher.GetWatchedPaths()
		fmt.Printf("Watching %d directories\n", len(paths))
	}

	fmt.Printf("Autosync running (poll interval: %s, debounce: %s)\n", autosyncPollInterval, autosyncDebounceDelay)
	fmt.Println("Press Ctrl+C to stop")
	fmt.Println()

	// Wait for interrupt signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	fmt.Println("\nStopping autosync...")
	watcher.Stop()
	fmt.Println("Autosync stopped")

	return nil
}

// checkRemoteChanges checks if there are remote changes for an overlay.
func checkRemoteChanges(ov overlay.Overlay, logger func(string, ...interface{})) (bool, error) {
	logger("Fetching remote for %s...", ov.Name)

	if err := git.Fetch(ov.RepoPath); err != nil {
		return false, fmt.Errorf("fetch failed: %w", err)
	}

	diff, err := git.DiffRemote(ov.RepoPath)
	if err != nil {
		return false, fmt.Errorf("diff failed: %w", err)
	}

	return len(diff) > 0, nil
}

// syncLocalChanges handles local file changes by ensuring hardlinks are consistent.
func syncLocalChanges(ov overlay.Overlay, logger func(string, ...interface{})) {
	// Load manifest
	manifest, err := overlay.LoadManifest(ov.TargetDir, ov.Name)
	if err != nil {
		logger("Warning: could not load manifest for %s: %v", ov.Name, err)
		return
	}

	// Check for broken hardlinks
	broken := overlay.CheckBrokenHardlinks(ov, manifest)
	if len(broken) > 0 {
		logger("Found %d broken hardlinks in %s", len(broken), ov.Name)
		// Note: We don't auto-fix broken hardlinks as that might lose user changes
		// The user should commit changes and sync manually
	}

	// Check for unpushed commits
	hasUnpushed, err := git.HasUnpushedCommits(ov.RepoPath)
	if err != nil {
		logger("Warning: could not check unpushed commits: %v", err)
		return
	}

	if hasUnpushed {
		timestamp := time.Now().Format("15:04:05")
		fmt.Printf("[%s] %s has unpushed commits\n", timestamp, ov.Name)
	}
}

// syncRemoteChanges pulls remote changes and syncs hardlinks.
func syncRemoteChanges(ov overlay.Overlay, logger func(string, ...interface{})) {
	// Check for uncommitted changes
	if hasChanges, _ := git.HasUncommittedChanges(ov.RepoPath); hasChanges {
		timestamp := time.Now().Format("15:04:05")
		fmt.Printf("[%s] Warning: %s has uncommitted changes, skipping pull\n", timestamp, ov.Name)
		return
	}

	// Get files before pull
	filesBefore, err := git.ListFiles(ov.RepoPath)
	if err != nil {
		logger("Warning: could not list files: %v", err)
		filesBefore = []string{}
	}

	// Pull
	logger("Pulling %s...", ov.Name)
	if err := git.Pull(ov.RepoPath); err != nil {
		if hasConflicts, _ := git.HasConflicts(ov.RepoPath); hasConflicts {
			timestamp := time.Now().Format("15:04:05")
			fmt.Printf("[%s] Error: merge conflicts in %s, resolve manually\n", timestamp, ov.Name)
			return
		}
		logger("Warning: pull failed for %s: %v", ov.Name, err)
		return
	}

	// Get files after pull
	filesAfter, err := git.ListFiles(ov.RepoPath)
	if err != nil {
		logger("Warning: could not list files: %v", err)
		filesAfter = []string{}
	}

	// Load manifest
	manifest, err := overlay.LoadManifest(ov.TargetDir, ov.Name)
	if err != nil {
		logger("Warning: could not load manifest: %v", err)
		manifest = nil
	}

	// Sync hardlinks
	logger("Syncing hardlinks for %s...", ov.Name)
	if err := overlay.SyncHardlinks(ov, manifest, filesBefore, filesAfter); err != nil {
		logger("Warning: hardlink sync failed for %s: %v", ov.Name, err)
		return
	}

	timestamp := time.Now().Format("15:04:05")
	fmt.Printf("[%s] %s synced successfully\n", timestamp, ov.Name)
}

// uniqueStrings returns unique strings from a slice.
func uniqueStrings(strs []string) []string {
	seen := make(map[string]bool)
	result := make([]string, 0, len(strs))
	for _, s := range strs {
		if !seen[s] {
			seen[s] = true
			result = append(result, s)
		}
	}
	return result
}
