// Package state records what over knows about a layer's files at the
// time they were last synced.
//
// The state is over's base for three-way comparison: a file whose local
// and remote hashes both differ from the recorded hash has changed on
// both sides and is in conflict. A file with no entry at all has never
// been synced, and over will not overwrite it.
package state

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/mariusae/over/internal/config"
	"gopkg.in/yaml.v3"
)

// State is the sync state of one layer.
type State struct {
	// Layer is the layer specification the state belongs to. It is
	// informational; the file's location determines the layer.
	Layer string `yaml:"layer"`

	// Root is the root the layer was last synced under. A change of
	// root invalidates nothing, but it is recorded so that "over
	// status" can report it.
	Root string `yaml:"root,omitempty"`

	// Commit is the repository revision at the last sync.
	Commit string `yaml:"commit,omitempty"`

	// Synced is the time of the last sync.
	Synced time.Time `yaml:"synced,omitempty"`

	// Files maps layer-relative paths to their state at the last sync.
	Files map[string]*File `yaml:"files,omitempty"`

	path string
}

// A File is the recorded state of a single file.
type File struct {
	// Hash is the SHA-256 of the file's contents as of the last sync,
	// hex encoded. It is empty for a deleted file.
	Hash string `yaml:"hash,omitempty"`

	// Exec reports whether the file is executable.
	Exec bool `yaml:"exec,omitempty"`

	// Deleted reports that the file is known to be absent: over has
	// synced its deletion, or "over track" has staged it for its first
	// write. Either way the layer had no content for it.
	Deleted bool `yaml:"deleted,omitempty"`

	// Synced is the time the entry was last written.
	Synced time.Time `yaml:"synced,omitempty"`

	// Reason records why the local file is ahead of the layer, when it
	// got that way by a deliberate act rather than an ordinary edit:
	// "ack", "track", or "restore". over writes it into the commit
	// that publishes the change, and it is cleared once it has.
	Reason string `yaml:"reason,omitempty"`
}

// Load reads the state stored at path. A missing file yields an empty
// state for the given layer. The path is remembered for Save.
func Load(path, layer string) (*State, error) {
	s := &State{Layer: layer, Files: map[string]*File{}, path: path}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return s, nil
	} else if err != nil {
		return nil, err
	}
	if err := yaml.Unmarshal(data, s); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if s.Files == nil {
		s.Files = map[string]*File{}
	}
	s.path = path
	s.Layer = layer
	return s, nil
}

// Path returns the file the state is stored in.
func (s *State) Path() string { return s.path }

// Save writes the state back to the file it was loaded from.
func (s *State) Save() error {
	data, err := yaml.Marshal(s)
	if err != nil {
		return err
	}
	return config.WriteFile(s.path, data, 0o644)
}

// Get returns the entry for path, or nil if the file is not tracked.
func (s *State) Get(path string) *File { return s.Files[path] }

// Set records path as holding the given contents.
func (s *State) Set(path, hash string, exec bool) {
	s.Files[path] = &File{Hash: hash, Exec: exec, Synced: now()}
}

// SetDeleted records path as absent.
func (s *State) SetDeleted(path string) {
	s.Files[path] = &File{Deleted: true, Synced: now()}
}

// SetReason records why path's local side is ahead. It has no effect on
// an untracked path.
func (s *State) SetReason(path, reason string) {
	if f := s.Files[path]; f != nil {
		f.Reason = reason
	}
}

// Delete drops path from the state entirely, so that over no longer
// tracks it.
func (s *State) Delete(path string) { delete(s.Files, path) }

// Paths returns the tracked paths, sorted.
func (s *State) Paths() []string {
	paths := make([]string, 0, len(s.Files))
	for p := range s.Files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	return paths
}

func now() time.Time { return time.Now().UTC().Truncate(time.Second) }

// HashFile returns the SHA-256 of the file at path and whether it is
// executable. It reports os.ErrNotExist if the file is absent, and an
// error if it is not a regular file.
func HashFile(path string) (hash string, exec bool, err error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", false, err
	}
	if !info.Mode().IsRegular() {
		return "", false, fmt.Errorf("%s: not a regular file", path)
	}
	f, err := os.Open(path)
	if err != nil {
		return "", false, err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", false, err
	}
	return hex.EncodeToString(h.Sum(nil)), info.Mode()&0o100 != 0, nil
}

// HashBytes returns the SHA-256 of data, hex encoded.
func HashBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// File returns the path of the state file for the named layer under
// dir, e.g. dir/mariusae/config/editors.yaml.
func FilePath(dir, host, owner, repo, layer string) string {
	return filepath.Join(dir, host, owner, repo, layer+".yaml")
}
