package overlay

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// WatcherConfig holds configuration for the autosync watcher.
type WatcherConfig struct {
	// PollInterval is how often to check for remote changes.
	PollInterval time.Duration
	// DebounceDelay is how long to wait after a change before syncing.
	DebounceDelay time.Duration
	// OnLocalChange is called when local files change.
	OnLocalChange func(overlay Overlay, paths []string)
	// OnRemoteChange is called when remote changes are detected.
	OnRemoteChange func(overlay Overlay)
	// OnConfigChange is called when configuration changes (new layers, linklist changes).
	OnConfigChange func()
	// OnError is called when an error occurs.
	OnError func(error)
	// Logger for debug output.
	Logger func(format string, args ...interface{})
}

// Watcher watches for file changes and triggers sync operations.
type Watcher struct {
	config      WatcherConfig
	fsWatcher   *fsnotify.Watcher
	overlays    []Overlay
	startDir    string
	stopCh      chan struct{}
	stoppedCh   chan struct{}
	mu          sync.Mutex
	watchedDirs map[string]bool // Track which directories we're watching
}

// NewWatcher creates a new Watcher for the given overlays.
func NewWatcher(startDir string, overlays []Overlay, config WatcherConfig) (*Watcher, error) {
	fsWatcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("failed to create fsnotify watcher: %w", err)
	}

	// Set defaults
	if config.PollInterval == 0 {
		config.PollInterval = 30 * time.Second
	}
	if config.DebounceDelay == 0 {
		config.DebounceDelay = 500 * time.Millisecond
	}
	if config.Logger == nil {
		config.Logger = func(format string, args ...interface{}) {}
	}

	w := &Watcher{
		config:      config,
		fsWatcher:   fsWatcher,
		overlays:    overlays,
		startDir:    startDir,
		stopCh:      make(chan struct{}),
		stoppedCh:   make(chan struct{}),
		watchedDirs: make(map[string]bool),
	}

	return w, nil
}

// Start begins watching for changes.
func (w *Watcher) Start() error {
	// Add watches for all relevant paths
	if err := w.setupWatches(); err != nil {
		return err
	}

	// Start the event loop
	go w.run()

	return nil
}

// Stop stops the watcher.
func (w *Watcher) Stop() {
	close(w.stopCh)
	<-w.stoppedCh
	w.fsWatcher.Close()
}

// Refresh reloads overlays and updates watches.
func (w *Watcher) Refresh() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	// Reload overlays
	overlays, err := FindReachableOverlays(w.startDir)
	if err != nil {
		return err
	}
	w.overlays = overlays

	// Rebuild watches
	return w.setupWatches()
}

// setupWatches adds filesystem watches for all relevant paths.
func (w *Watcher) setupWatches() error {
	// Track new directories to watch
	newWatches := make(map[string]bool)

	// Watch .over directories for config changes
	dir := w.startDir
	for {
		overDir := filepath.Join(dir, OverDirName)
		if info, err := os.Stat(overDir); err == nil && info.IsDir() {
			newWatches[overDir] = true

			// Also watch layer-specific manifest directories
			registry, err := LoadRegistry(dir)
			if err == nil {
				for _, ov := range registry.Overlays {
					manifestDir := filepath.Join(overDir, ov.Name)
					if info, err := os.Stat(manifestDir); err == nil && info.IsDir() {
						newWatches[manifestDir] = true
					}
				}
			}
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	// Watch target directories and repo directories for each overlay
	for _, ov := range w.overlays {
		// Watch the target directory (where hardlinks live)
		if err := w.addDirRecursively(ov.TargetDir, newWatches); err != nil {
			w.config.Logger("Warning: failed to watch target dir %s: %v", ov.TargetDir, err)
		}

		// Watch the repo directory
		if err := w.addDirRecursively(ov.RepoPath, newWatches); err != nil {
			w.config.Logger("Warning: failed to watch repo dir %s: %v", ov.RepoPath, err)
		}
	}

	// Remove watches that are no longer needed
	for dir := range w.watchedDirs {
		if !newWatches[dir] {
			w.fsWatcher.Remove(dir)
		}
	}

	// Add new watches
	for dir := range newWatches {
		if !w.watchedDirs[dir] {
			if err := w.fsWatcher.Add(dir); err != nil {
				w.config.Logger("Warning: failed to add watch for %s: %v", dir, err)
			}
		}
	}

	w.watchedDirs = newWatches
	w.config.Logger("Watching %d directories", len(w.watchedDirs))

	return nil
}

// addDirRecursively adds a directory and all subdirectories to the watch map.
func (w *Watcher) addDirRecursively(root string, watches map[string]bool) error {
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip errors
		}

		// Skip .git directories
		if info.IsDir() && info.Name() == ".git" {
			return filepath.SkipDir
		}

		if info.IsDir() {
			watches[path] = true
		}

		return nil
	})
}

// run is the main event loop.
func (w *Watcher) run() {
	defer close(w.stoppedCh)

	// Debouncing state
	var localChangedOverlays = make(map[string][]string) // overlay name -> changed paths
	var configChanged bool
	var debounceTimer *time.Timer

	// Poll timer for remote changes
	pollTicker := time.NewTicker(w.config.PollInterval)
	defer pollTicker.Stop()

	resetDebounce := func() {
		if debounceTimer != nil {
			debounceTimer.Stop()
		}
		debounceTimer = time.AfterFunc(w.config.DebounceDelay, func() {
			w.mu.Lock()
			changedOverlays := localChangedOverlays
			localChangedOverlays = make(map[string][]string)
			needConfigRefresh := configChanged
			configChanged = false
			w.mu.Unlock()

			// Handle config changes first
			if needConfigRefresh {
				if w.config.OnConfigChange != nil {
					w.config.OnConfigChange()
				}
				// Refresh watches after config change
				if err := w.Refresh(); err != nil && w.config.OnError != nil {
					w.config.OnError(fmt.Errorf("failed to refresh watches: %w", err))
				}
			}

			// Handle local file changes
			if w.config.OnLocalChange != nil {
				for _, ov := range w.overlays {
					if paths, ok := changedOverlays[ov.Name]; ok {
						w.config.OnLocalChange(ov, paths)
					}
				}
			}
		})
	}

	for {
		select {
		case <-w.stopCh:
			if debounceTimer != nil {
				debounceTimer.Stop()
			}
			return

		case event, ok := <-w.fsWatcher.Events:
			if !ok {
				return
			}

			w.mu.Lock()
			w.handleEvent(event, localChangedOverlays, &configChanged)
			w.mu.Unlock()

			resetDebounce()

		case err, ok := <-w.fsWatcher.Errors:
			if !ok {
				return
			}
			if w.config.OnError != nil {
				w.config.OnError(err)
			}

		case <-pollTicker.C:
			// Check for remote changes
			if w.config.OnRemoteChange != nil {
				for _, ov := range w.overlays {
					w.config.OnRemoteChange(ov)
				}
			}
		}
	}
}

// handleEvent processes a single filesystem event.
func (w *Watcher) handleEvent(event fsnotify.Event, changedOverlays map[string][]string, configChanged *bool) {
	path := event.Name

	// Check if this is a config file change
	if w.isConfigPath(path) {
		*configChanged = true
		w.config.Logger("Config change detected: %s", path)
		return
	}

	// Check which overlay this file belongs to
	for _, ov := range w.overlays {
		// Check if it's in the target directory
		if rel, err := filepath.Rel(ov.TargetDir, path); err == nil && !isOutsidePath(rel) {
			// Skip .over directory
			if len(rel) >= len(OverDirName) && rel[:len(OverDirName)] == OverDirName {
				continue
			}
			// Skip if file doesn't match linklist
			if !ShouldLinkFile(ov.LinkList, rel) {
				continue
			}
			changedOverlays[ov.Name] = append(changedOverlays[ov.Name], rel)
			w.config.Logger("Local change in %s: %s (%s)", ov.Name, rel, event.Op)
			return
		}

		// Check if it's in the repo directory
		if rel, err := filepath.Rel(ov.RepoPath, path); err == nil && !isOutsidePath(rel) {
			// Skip .git directory
			if len(rel) >= 4 && rel[:4] == ".git" {
				continue
			}
			changedOverlays[ov.Name] = append(changedOverlays[ov.Name], rel)
			w.config.Logger("Repo change in %s: %s (%s)", ov.Name, rel, event.Op)
			return
		}
	}

	// Check if a new directory was created that we should watch
	if event.Op&fsnotify.Create != 0 {
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			// Add watch for new directory
			w.fsWatcher.Add(path)
			w.watchedDirs[path] = true
			w.config.Logger("Added watch for new directory: %s", path)
		}
	}
}

// isConfigPath checks if the path is a configuration file.
func (w *Watcher) isConfigPath(path string) bool {
	base := filepath.Base(path)

	// Check for registry file
	if base == RegistryFileName {
		return true
	}

	// Check for manifest file
	if base == ManifestFileName {
		return true
	}

	return false
}

// isOutsidePath checks if a relative path goes outside the base directory.
func isOutsidePath(relPath string) bool {
	return len(relPath) >= 2 && relPath[:2] == ".."
}

// GetWatchedPaths returns the list of currently watched paths (for debugging).
func (w *Watcher) GetWatchedPaths() []string {
	w.mu.Lock()
	defer w.mu.Unlock()

	paths := make([]string, 0, len(w.watchedDirs))
	for p := range w.watchedDirs {
		paths = append(paths, p)
	}
	return paths
}
