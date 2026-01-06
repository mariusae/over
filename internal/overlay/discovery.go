package overlay

import (
	"os"
	"path/filepath"
	"sort"
)

// FindReachableOverlays finds all overlays reachable from the given directory.
// It walks up the directory tree from startDir to the root, collecting overlays
// from any .over/ directories found along the way. Deduplicates by name.
// Returns overlays sorted by order (lower order = higher precedence).
func FindReachableOverlays(startDir string) ([]Overlay, error) {
	var overlays []Overlay
	seen := make(map[string]bool)
	dir := startDir

	for {
		overDir := filepath.Join(dir, OverDirName)
		if info, err := os.Stat(overDir); err == nil && info.IsDir() {
			registry, err := LoadRegistry(dir)
			if err == nil {
				for _, ov := range registry.Overlays {
					if !seen[ov.Name] {
						seen[ov.Name] = true
						overlays = append(overlays, ov)
					}
				}
			}
		}

		parent := filepath.Dir(dir)
		if parent == dir { // Reached root
			break
		}
		dir = parent
	}

	// Sort overlays by order (lower values first = higher precedence)
	sort.Slice(overlays, func(i, j int) bool {
		return overlays[i].Order < overlays[j].Order
	})

	return overlays, nil
}

// FindOverlayByName finds a specific overlay by name in the reachable overlays.
func FindOverlayByName(startDir, name string) (*Overlay, error) {
	overlays, err := FindReachableOverlays(startDir)
	if err != nil {
		return nil, err
	}

	for i := range overlays {
		if overlays[i].Name == name {
			return &overlays[i], nil
		}
	}
	return nil, nil
}

// FindOverlayForFile finds which overlay owns a specific file.
// Returns the overlay with the highest precedence that tracks the file.
// The filePath should be relative to startDir.
func FindOverlayForFile(startDir, filePath string) (*Overlay, error) {
	// Make filePath absolute for comparison
	absFilePath := filePath
	if !filepath.IsAbs(filePath) {
		absFilePath = filepath.Join(startDir, filePath)
	}

	overlays, err := FindReachableOverlays(startDir)
	if err != nil {
		return nil, err
	}

	// Check each overlay in order of precedence (already sorted by FindReachableOverlays)
	for i := range overlays {
		ov := &overlays[i]

		// Load manifest for this overlay
		manifest, err := LoadManifest(ov.TargetDir, ov.Name)
		if err != nil {
			continue // Skip if we can't load manifest
		}

		// Check if this overlay tracks the file
		for _, entry := range manifest.Files {
			entryPath := filepath.Join(ov.TargetDir, entry.RelativePath)
			if entryPath == absFilePath {
				return ov, nil
			}
		}
	}

	return nil, nil
}
