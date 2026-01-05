package overlay

import (
	"testing"
	"time"
)

func TestOverlay(t *testing.T) {
	ov := Overlay{
		Name:        "test-repo",
		SourceURL:   "https://github.com/test/repo.git",
		RepoPath:    "/home/user/.local/over/test-repo",
		TargetDir:   "/home/user/project",
		InstalledAt: time.Now(),
	}

	if ov.Name != "test-repo" {
		t.Errorf("Expected name %q, got %q", "test-repo", ov.Name)
	}

	if ov.SourceURL != "https://github.com/test/repo.git" {
		t.Errorf("Expected source URL %q, got %q", "https://github.com/test/repo.git", ov.SourceURL)
	}
}

func TestFileEntry(t *testing.T) {
	entry := FileEntry{
		RelativePath: "test/file.txt",
		Inode:        12345,
		Size:         1024,
		Mode:         0644,
	}

	if entry.RelativePath != "test/file.txt" {
		t.Errorf("Expected path %q, got %q", "test/file.txt", entry.RelativePath)
	}

	if entry.Inode != 12345 {
		t.Errorf("Expected inode %d, got %d", 12345, entry.Inode)
	}

	if entry.Size != 1024 {
		t.Errorf("Expected size %d, got %d", 1024, entry.Size)
	}

	if entry.Mode != 0644 {
		t.Errorf("Expected mode %o, got %o", 0644, entry.Mode)
	}
}

func TestManifest_AddFile(t *testing.T) {
	manifest := &Manifest{
		Version: 1,
		Files:   []FileEntry{},
	}

	entry1 := FileEntry{RelativePath: "file1.txt", Inode: 1}
	entry2 := FileEntry{RelativePath: "file2.txt", Inode: 2}

	manifest.AddFile(entry1)
	manifest.AddFile(entry2)

	if len(manifest.Files) != 2 {
		t.Fatalf("Expected 2 files, got %d", len(manifest.Files))
	}

	if manifest.Files[0].RelativePath != "file1.txt" {
		t.Errorf("Expected first file %q, got %q", "file1.txt", manifest.Files[0].RelativePath)
	}

	if manifest.Files[1].RelativePath != "file2.txt" {
		t.Errorf("Expected second file %q, got %q", "file2.txt", manifest.Files[1].RelativePath)
	}
}

func TestRegistry_AddOverlay(t *testing.T) {
	registry := &Registry{
		Version:  1,
		Overlays: []Overlay{},
	}

	ov1 := Overlay{Name: "repo1"}
	ov2 := Overlay{Name: "repo2"}

	registry.AddOverlay(ov1)
	registry.AddOverlay(ov2)

	if len(registry.Overlays) != 2 {
		t.Fatalf("Expected 2 overlays, got %d", len(registry.Overlays))
	}

	if registry.Overlays[0].Name != "repo1" {
		t.Errorf("Expected first overlay %q, got %q", "repo1", registry.Overlays[0].Name)
	}

	if registry.Overlays[1].Name != "repo2" {
		t.Errorf("Expected second overlay %q, got %q", "repo2", registry.Overlays[1].Name)
	}
}

func TestRegistry_FindOverlay(t *testing.T) {
	registry := &Registry{
		Version: 1,
		Overlays: []Overlay{
			{Name: "repo1"},
			{Name: "repo2"},
			{Name: "repo3"},
		},
	}

	// Find existing overlay
	found := registry.FindOverlay("repo2")
	if found == nil {
		t.Fatal("Expected to find repo2")
	}
	if found.Name != "repo2" {
		t.Errorf("Expected to find repo2, got %q", found.Name)
	}

	// Find non-existing overlay
	notFound := registry.FindOverlay("nonexistent")
	if notFound != nil {
		t.Error("Expected nil for non-existent overlay")
	}
}
