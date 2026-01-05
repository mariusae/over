package overlay

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestOverDir(t *testing.T) {
	targetDir := "/home/user/project"
	expected := filepath.Join(targetDir, OverDirName)
	result := OverDir(targetDir)

	if result != expected {
		t.Errorf("OverDir(%q) = %q, want %q", targetDir, result, expected)
	}
}

func TestEnsureOverDir(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "overlay-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Ensure .over directory is created
	if err := EnsureOverDir(tmpDir); err != nil {
		t.Fatalf("EnsureOverDir failed: %v", err)
	}

	overDir := OverDir(tmpDir)
	info, err := os.Stat(overDir)
	if err != nil {
		t.Fatalf("Expected .over directory to exist: %v", err)
	}

	if !info.IsDir() {
		t.Error("Expected .over to be a directory")
	}
}

func TestLoadRegistry(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "overlay-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Load non-existent registry (should return empty registry)
	registry, err := LoadRegistry(tmpDir)
	if err != nil {
		t.Fatalf("LoadRegistry failed: %v", err)
	}

	if registry.Version != 1 {
		t.Errorf("Expected version 1, got %d", registry.Version)
	}

	if len(registry.Overlays) != 0 {
		t.Errorf("Expected empty overlays, got %d", len(registry.Overlays))
	}
}

func TestSaveAndLoadRegistry(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "overlay-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a registry with some overlays
	registry := &Registry{
		Version: 1,
		Overlays: []Overlay{
			{
				Name:      "test-repo",
				SourceURL: "https://github.com/test/repo.git",
				RepoPath:  "/path/to/repo",
				TargetDir: tmpDir,
			},
		},
	}

	// Save registry
	if err := SaveRegistry(tmpDir, registry); err != nil {
		t.Fatalf("SaveRegistry failed: %v", err)
	}

	// Load registry
	loaded, err := LoadRegistry(tmpDir)
	if err != nil {
		t.Fatalf("LoadRegistry failed: %v", err)
	}

	// Verify loaded data
	if loaded.Version != registry.Version {
		t.Errorf("Expected version %d, got %d", registry.Version, loaded.Version)
	}

	if len(loaded.Overlays) != 1 {
		t.Fatalf("Expected 1 overlay, got %d", len(loaded.Overlays))
	}

	if loaded.Overlays[0].Name != "test-repo" {
		t.Errorf("Expected name %q, got %q", "test-repo", loaded.Overlays[0].Name)
	}
}

func TestSaveAndLoadManifest(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "overlay-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	overlayName := "test-overlay"

	// Create a manifest
	manifest := &Manifest{
		Version: 1,
		Files: []FileEntry{
			{
				RelativePath: "file1.txt",
				Inode:        12345,
				Size:         1024,
				Mode:         0644,
			},
			{
				RelativePath: "dir/file2.txt",
				Inode:        67890,
				Size:         2048,
				Mode:         0755,
			},
		},
	}

	// Save manifest
	if err := SaveManifest(tmpDir, overlayName, manifest); err != nil {
		t.Fatalf("SaveManifest failed: %v", err)
	}

	// Load manifest
	loaded, err := LoadManifest(tmpDir, overlayName)
	if err != nil {
		t.Fatalf("LoadManifest failed: %v", err)
	}

	// Verify loaded data
	if loaded.Version != manifest.Version {
		t.Errorf("Expected version %d, got %d", manifest.Version, loaded.Version)
	}

	if len(loaded.Files) != len(manifest.Files) {
		t.Fatalf("Expected %d files, got %d", len(manifest.Files), len(loaded.Files))
	}

	for i, file := range loaded.Files {
		expected := manifest.Files[i]
		if file.RelativePath != expected.RelativePath {
			t.Errorf("File %d: expected path %q, got %q", i, expected.RelativePath, file.RelativePath)
		}
		if file.Inode != expected.Inode {
			t.Errorf("File %d: expected inode %d, got %d", i, expected.Inode, file.Inode)
		}
		if file.Size != expected.Size {
			t.Errorf("File %d: expected size %d, got %d", i, expected.Size, file.Size)
		}
	}
}

func TestRegistryJSONFormat(t *testing.T) {
	registry := &Registry{
		Version: 1,
		Overlays: []Overlay{
			{
				Name:      "test",
				SourceURL: "https://example.com",
			},
		},
	}

	data, err := json.MarshalIndent(registry, "", "  ")
	if err != nil {
		t.Fatalf("JSON marshal failed: %v", err)
	}

	var decoded Registry
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("JSON unmarshal failed: %v", err)
	}

	if decoded.Version != registry.Version {
		t.Errorf("Version mismatch after JSON round-trip")
	}

	if len(decoded.Overlays) != len(registry.Overlays) {
		t.Errorf("Overlays count mismatch after JSON round-trip")
	}
}
