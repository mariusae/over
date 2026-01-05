package overlay

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindReachableOverlays(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "overlay-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create directory structure:
	// tmpDir/
	//   .over/ (registry with overlay1)
	//   subdir/
	//     .over/ (registry with overlay2)
	//     deep/

	// Create first registry
	registry1 := &Registry{
		Version: 1,
		Overlays: []Overlay{
			{Name: "overlay1", TargetDir: tmpDir},
		},
	}
	if err := SaveRegistry(tmpDir, registry1); err != nil {
		t.Fatalf("Failed to save registry1: %v", err)
	}

	// Create subdirectory and second registry
	subdir := filepath.Join(tmpDir, "subdir")
	if err := os.MkdirAll(subdir, 0755); err != nil {
		t.Fatalf("Failed to create subdir: %v", err)
	}

	registry2 := &Registry{
		Version: 1,
		Overlays: []Overlay{
			{Name: "overlay2", TargetDir: subdir},
		},
	}
	if err := SaveRegistry(subdir, registry2); err != nil {
		t.Fatalf("Failed to save registry2: %v", err)
	}

	// Create deep directory
	deepDir := filepath.Join(subdir, "deep")
	if err := os.MkdirAll(deepDir, 0755); err != nil {
		t.Fatalf("Failed to create deep dir: %v", err)
	}

	// Test from deep directory - should find both overlays
	overlays, err := FindReachableOverlays(deepDir)
	if err != nil {
		t.Fatalf("FindReachableOverlays failed: %v", err)
	}

	if len(overlays) != 2 {
		t.Fatalf("Expected 2 overlays from deep dir, got %d", len(overlays))
	}

	// Verify both overlays are found (order may vary)
	foundNames := make(map[string]bool)
	for _, ov := range overlays {
		foundNames[ov.Name] = true
	}

	if !foundNames["overlay1"] {
		t.Error("overlay1 not found")
	}
	if !foundNames["overlay2"] {
		t.Error("overlay2 not found")
	}

	// Test from subdir - should find both overlays
	overlays, err = FindReachableOverlays(subdir)
	if err != nil {
		t.Fatalf("FindReachableOverlays from subdir failed: %v", err)
	}

	if len(overlays) != 2 {
		t.Fatalf("Expected 2 overlays from subdir, got %d", len(overlays))
	}

	// Test from root - should find only overlay1
	overlays, err = FindReachableOverlays(tmpDir)
	if err != nil {
		t.Fatalf("FindReachableOverlays from root failed: %v", err)
	}

	if len(overlays) != 1 {
		t.Fatalf("Expected 1 overlay from root, got %d", len(overlays))
	}

	if overlays[0].Name != "overlay1" {
		t.Errorf("Expected overlay1, got %q", overlays[0].Name)
	}
}

func TestFindReachableOverlays_Deduplication(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "overlay-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create two registries with overlapping overlay names
	registry1 := &Registry{
		Version: 1,
		Overlays: []Overlay{
			{Name: "shared", TargetDir: tmpDir},
			{Name: "unique1", TargetDir: tmpDir},
		},
	}
	if err := SaveRegistry(tmpDir, registry1); err != nil {
		t.Fatalf("Failed to save registry1: %v", err)
	}

	subdir := filepath.Join(tmpDir, "subdir")
	if err := os.MkdirAll(subdir, 0755); err != nil {
		t.Fatalf("Failed to create subdir: %v", err)
	}

	registry2 := &Registry{
		Version: 1,
		Overlays: []Overlay{
			{Name: "shared", TargetDir: subdir}, // Duplicate name
			{Name: "unique2", TargetDir: subdir},
		},
	}
	if err := SaveRegistry(subdir, registry2); err != nil {
		t.Fatalf("Failed to save registry2: %v", err)
	}

	// Find from subdir - should deduplicate "shared"
	overlays, err := FindReachableOverlays(subdir)
	if err != nil {
		t.Fatalf("FindReachableOverlays failed: %v", err)
	}

	// Should find 3 unique overlays (shared appears only once)
	if len(overlays) != 3 {
		t.Fatalf("Expected 3 unique overlays, got %d", len(overlays))
	}

	foundNames := make(map[string]int)
	for _, ov := range overlays {
		foundNames[ov.Name]++
	}

	if foundNames["shared"] != 1 {
		t.Errorf("Expected 'shared' to appear once, got %d", foundNames["shared"])
	}
	if foundNames["unique1"] != 1 {
		t.Error("Expected 'unique1' to appear once")
	}
	if foundNames["unique2"] != 1 {
		t.Error("Expected 'unique2' to appear once")
	}
}

func TestFindOverlayByName(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "overlay-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create registry with overlays
	registry := &Registry{
		Version: 1,
		Overlays: []Overlay{
			{Name: "repo1", TargetDir: tmpDir},
			{Name: "repo2", TargetDir: tmpDir},
		},
	}
	if err := SaveRegistry(tmpDir, registry); err != nil {
		t.Fatalf("Failed to save registry: %v", err)
	}

	// Find existing overlay
	found, err := FindOverlayByName(tmpDir, "repo1")
	if err != nil {
		t.Fatalf("FindOverlayByName failed: %v", err)
	}

	if found == nil {
		t.Fatal("Expected to find repo1")
	}

	if found.Name != "repo1" {
		t.Errorf("Expected name %q, got %q", "repo1", found.Name)
	}

	// Find non-existing overlay
	notFound, err := FindOverlayByName(tmpDir, "nonexistent")
	if err != nil {
		t.Fatalf("FindOverlayByName failed: %v", err)
	}

	if notFound != nil {
		t.Error("Expected nil for non-existent overlay")
	}
}

func TestFindReachableOverlays_EmptyDirectory(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "overlay-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Find in directory with no .over
	overlays, err := FindReachableOverlays(tmpDir)
	if err != nil {
		t.Fatalf("FindReachableOverlays failed: %v", err)
	}

	if len(overlays) != 0 {
		t.Errorf("Expected 0 overlays in empty directory, got %d", len(overlays))
	}
}
