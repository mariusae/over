// Package overlay provides core types and operations for managing overlays.
package overlay

import (
	"time"
)

// Overlay represents a single git repository layer.
// Lower order values have higher precedence (appear "above" higher order layers).
type Overlay struct {
	Name        string    `json:"name"`
	SourceURL   string    `json:"source_url"`
	RepoPath    string    `json:"repo_path"`
	TargetDir   string    `json:"target_dir"`
	InstalledAt time.Time `json:"installed_at"`
	Order       int       `json:"order"`         // Lower values = higher precedence
	LinkList    []string  `json:"link_list,omitempty"` // Gitignore-style patterns for files to link
}

// FileEntry represents a hardlinked file from an overlay.
type FileEntry struct {
	RelativePath string `json:"relative_path"`
	Inode        uint64 `json:"inode"`
	Size         int64  `json:"size"`
	Mode         uint32 `json:"mode"`
	Unlinked     bool   `json:"unlinked,omitempty"` // True if file was intentionally unlinked
}

// Manifest tracks all files from an overlay in a target directory.
type Manifest struct {
	Version int         `json:"version"`
	Files   []FileEntry `json:"files"`
}

// Registry tracks all overlays in a .over/ directory.
type Registry struct {
	Version  int       `json:"version"`
	Overlays []Overlay `json:"overlays"`
}
