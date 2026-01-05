package overlay

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const (
	OverDirName      = ".over"
	RegistryFileName = "overlays.json"
	ManifestFileName = "manifest.json"
)

// OverDir returns the path to the .over directory in the given target directory.
func OverDir(targetDir string) string {
	return filepath.Join(targetDir, OverDirName)
}

// EnsureOverDir creates the .over directory if it doesn't exist.
func EnsureOverDir(targetDir string) error {
	overDir := OverDir(targetDir)
	return os.MkdirAll(overDir, 0755)
}

// LoadRegistry loads the overlay registry from the .over directory.
func LoadRegistry(targetDir string) (*Registry, error) {
	registryPath := filepath.Join(OverDir(targetDir), RegistryFileName)
	data, err := os.ReadFile(registryPath)
	if err != nil {
		if os.IsNotExist(err) {
			return &Registry{Version: 1, Overlays: []Overlay{}}, nil
		}
		return nil, fmt.Errorf("failed to read registry: %w", err)
	}

	var registry Registry
	if err := json.Unmarshal(data, &registry); err != nil {
		return nil, fmt.Errorf("failed to parse registry: %w", err)
	}
	return &registry, nil
}

// SaveRegistry saves the overlay registry to the .over directory.
func SaveRegistry(targetDir string, registry *Registry) error {
	if err := EnsureOverDir(targetDir); err != nil {
		return err
	}

	data, err := json.MarshalIndent(registry, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal registry: %w", err)
	}

	registryPath := filepath.Join(OverDir(targetDir), RegistryFileName)
	if err := os.WriteFile(registryPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write registry: %w", err)
	}
	return nil
}

// AddOverlay adds an overlay to the registry.
func (r *Registry) AddOverlay(overlay Overlay) {
	r.Overlays = append(r.Overlays, overlay)
}

// FindOverlay finds an overlay by name in the registry.
func (r *Registry) FindOverlay(name string) *Overlay {
	for i := range r.Overlays {
		if r.Overlays[i].Name == name {
			return &r.Overlays[i]
		}
	}
	return nil
}

// LoadManifest loads the manifest for a specific overlay.
func LoadManifest(targetDir, overlayName string) (*Manifest, error) {
	manifestPath := filepath.Join(OverDir(targetDir), overlayName, ManifestFileName)
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		if os.IsNotExist(err) {
			return &Manifest{Version: 1, Files: []FileEntry{}}, nil
		}
		return nil, fmt.Errorf("failed to read manifest: %w", err)
	}

	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("failed to parse manifest: %w", err)
	}
	return &manifest, nil
}

// SaveManifest saves the manifest for a specific overlay.
func SaveManifest(targetDir, overlayName string, manifest *Manifest) error {
	manifestDir := filepath.Join(OverDir(targetDir), overlayName)
	if err := os.MkdirAll(manifestDir, 0755); err != nil {
		return fmt.Errorf("failed to create manifest directory: %w", err)
	}

	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal manifest: %w", err)
	}

	manifestPath := filepath.Join(manifestDir, ManifestFileName)
	if err := os.WriteFile(manifestPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write manifest: %w", err)
	}
	return nil
}

// AddFile adds a file entry to the manifest.
func (m *Manifest) AddFile(entry FileEntry) {
	m.Files = append(m.Files, entry)
}
