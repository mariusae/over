package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Tombstones records the files a layer has deleted. It is stored beside
// the layer's directory in the repository, in "<layer>.tombstones.yaml".
// A tombstone is removed when the file comes back.
type Tombstones struct {
	// Tombstones maps a layer-relative path to the time it was
	// deleted.
	Tombstones map[string]time.Time `yaml:"tombstones"`
}

// LoadTombstones reads the tombstone file at path. A missing file yields
// an empty set.
func LoadTombstones(path string) (*Tombstones, error) {
	t := &Tombstones{Tombstones: map[string]time.Time{}}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return t, nil
	} else if err != nil {
		return nil, err
	}
	if err := yaml.Unmarshal(data, t); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if t.Tombstones == nil {
		t.Tombstones = map[string]time.Time{}
	}
	return t, nil
}

// Has reports whether path is tombstoned.
func (t *Tombstones) Has(path string) bool {
	_, ok := t.Tombstones[path]
	return ok
}

// At returns the time path was deleted, and whether it is tombstoned.
func (t *Tombstones) At(path string) (time.Time, bool) {
	when, ok := t.Tombstones[path]
	return when, ok
}

// Add records path as deleted at the given time.
func (t *Tombstones) Add(path string, when time.Time) {
	t.Tombstones[path] = when.UTC().Truncate(time.Second)
}

// Remove drops path's tombstone.
func (t *Tombstones) Remove(path string) { delete(t.Tombstones, path) }

// Save writes the tombstones to path. When there are none, the file is
// removed instead, so that a layer with nothing deleted carries no
// tombstone file.
func (t *Tombstones) Save(path string) error {
	if len(t.Tombstones) == 0 {
		err := os.Remove(path)
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	data, err := yaml.Marshal(t)
	if err != nil {
		return err
	}
	return WriteFile(path, data, 0o644)
}
