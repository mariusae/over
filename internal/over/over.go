// Package over implements the overlay engine: resolving the configured
// layers, comparing them against the local file system, and applying the
// resulting changes.
package over

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mariusae/over/internal/config"
	"github.com/mariusae/over/internal/gitrepo"
	"github.com/mariusae/over/internal/pathspec"
	"github.com/mariusae/over/internal/spec"
	"github.com/mariusae/over/internal/state"
)

// Over is a client: a configuration, a cache of repository checkouts,
// and the sync state of each configured layer.
type Over struct {
	// Config is the client configuration, as loaded from ConfigPath.
	Config *config.Config

	home  string
	cache string
	url   func(spec.Spec) string

	repos map[string]*gitrepo.Repo // by repository spec
}

// Options configure a client.
type Options struct {
	// Home is over's configuration directory, holding config.yaml and
	// the per-layer state files.
	Home string

	// Cache is the directory repository checkouts are kept in.
	Cache string

	// URL returns the git URL of a repository. It defaults to HTTPS on
	// the spec's host.
	URL func(spec.Spec) string
}

// ErrNoLayers is returned by commands that need a configured layer and
// find none.
var ErrNoLayers = errors.New("no layers configured; add one with 'over add <owner>/<repo>:<layer>'")

// Open loads the client configuration described by opts.
func Open(opts Options) (*Over, error) {
	if opts.Home == "" {
		return nil, fmt.Errorf("no over home directory")
	}
	if opts.Cache == "" {
		return nil, fmt.Errorf("no over cache directory")
	}
	if opts.URL == nil {
		opts.URL = DefaultURL
	}
	o := &Over{
		home:  opts.Home,
		cache: opts.Cache,
		url:   opts.URL,
		repos: map[string]*gitrepo.Repo{},
	}
	cfg, err := config.Load(o.ConfigPath())
	if err != nil {
		return nil, err
	}
	if cfg.Exclude == nil {
		// A configuration that has never named its exclusions gets
		// over's own directories. An empty list is left as it is:
		// saying "none" explicitly is a choice over does not override.
		cfg.Exclude = DefaultExclusions(o.home, o.cache)
	}
	o.Config = cfg
	return o, nil
}

// DefaultURL returns the HTTPS git URL of a repository.
func DefaultURL(s spec.Spec) string {
	return fmt.Sprintf("https://%s/%s/%s.git", s.Host, s.Owner, s.Repo)
}

// Home returns the configuration directory.
func (o *Over) Home() string { return o.home }

// ConfigPath returns the path of the client configuration file.
func (o *Over) ConfigPath() string { return filepath.Join(o.home, "config.yaml") }

// StateDir returns the directory holding the per-layer state files.
func (o *Over) StateDir() string { return filepath.Join(o.home, "state") }

// SaveConfig writes the client configuration back to disk.
func (o *Over) SaveConfig() error { return o.Config.Save(o.ConfigPath()) }

// Repo returns the checkout of the repository named by s, cloning it if
// necessary. Repositories are opened once per client, so that layers
// sharing a repository also share its checkout.
func (o *Over) Repo(ctx context.Context, s spec.Spec) (*gitrepo.Repo, error) {
	key := s.Repository().String()
	if r, ok := o.repos[key]; ok {
		return r, nil
	}
	r, err := gitrepo.Open(ctx, o.repoDir(s), o.url(s))
	if err != nil {
		return nil, err
	}
	o.repos[key] = r
	return r, nil
}

// A Fetched reports the effect of updating one repository.
type Fetched struct {
	// Repo is the repository, without a layer name.
	Repo spec.Spec

	// Before and After are the checked-out revisions either side of
	// the fetch. They are equal when nothing arrived.
	Before, After string

	// Cloned reports that the repository was not in the cache at all,
	// and has just been fetched in full.
	Cloned bool
}

// Changed reports whether the fetch brought anything new.
func (f Fetched) Changed() bool { return f.Cloned || f.Before != f.After }

// Fetch updates the checkout of every repository the configured layers
// live in. It touches no local file: fetching only makes over's idea of
// the layers current, so that "over status" reports against them as they
// are now rather than as they were at the last sync.
func (o *Over) Fetch(ctx context.Context) ([]Fetched, error) {
	var out []Fetched
	seen := map[string]bool{}
	for _, entry := range o.Config.Layers {
		s, err := spec.Parse(entry.Layer)
		if err != nil {
			return nil, err
		}
		s = s.Repository()
		if seen[s.String()] {
			continue
		}
		seen[s.String()] = true

		f := Fetched{Repo: s}
		if _, err := os.Stat(o.repoDir(s)); os.IsNotExist(err) {
			f.Cloned = true
		} else if err != nil {
			return nil, err
		}
		repo, err := o.Repo(ctx, s)
		if err != nil {
			return nil, err
		}
		if !f.Cloned {
			if f.Before, err = repo.Head(ctx); err != nil {
				return nil, err
			}
		}
		if err := repo.Update(ctx); err != nil {
			return nil, err
		}
		if f.After, err = repo.Head(ctx); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, nil
}

// repoDir returns the directory a repository is checked out in.
func (o *Over) repoDir(s spec.Spec) string {
	return filepath.Join(o.cache, "repo", s.Host, s.Owner, s.Repo)
}

// A Layer is a configured layer, resolved against its repository.
type Layer struct {
	// Spec identifies the layer.
	Spec spec.Spec

	// Root is the absolute directory the layer is materialized under.
	Root string

	// Index is the layer's position in the configuration. Higher
	// indices win: the last layer providing a file owns it.
	Index int

	// Repo is the checkout the layer lives in.
	Repo *gitrepo.Repo

	// Dir is the layer's directory within the checkout. It need not
	// exist; a layer with no directory simply provides no files.
	Dir string

	// State is the layer's sync state.
	State *state.State

	// Tombstones records the layer's deleted files.
	Tombstones *config.Tombstones

	// Exclude holds the patterns of paths the layer must not manage,
	// whatever its root.
	Exclude []pathspec.Pattern

	tombPath string
}

// Excluded reports whether an absolute local path is one over refuses to
// manage.
func (l *Layer) Excluded(local string) bool {
	return pathspec.Patterns(l.Exclude).MatchAny(local)
}

// String returns the layer's specification.
func (l *Layer) String() string { return l.Spec.String() }

// LocalPath returns the absolute path of a layer-relative path on the
// local file system.
func (l *Layer) LocalPath(rel string) string { return filepath.Join(l.Root, rel) }

// RepoPath returns the absolute path of a layer-relative path within the
// checkout.
func (l *Layer) RepoPath(rel string) string { return filepath.Join(l.Dir, rel) }

// TombstonePath returns the path of the layer's tombstone file within
// the checkout.
func (l *Layer) TombstonePath() string { return l.tombPath }

// SaveTombstones writes the layer's tombstone file.
func (l *Layer) SaveTombstones() error { return l.Tombstones.Save(l.tombPath) }

// Layers resolves the configured layers. When update is set, each
// distinct repository is fetched first; otherwise the cached checkouts
// are used as they are, which keeps read-only commands off the network.
func (o *Over) Layers(ctx context.Context, update bool) ([]*Layer, error) {
	exclusions, err := o.Exclusions()
	if err != nil {
		return nil, err
	}
	layers := make([]*Layer, 0, len(o.Config.Layers))
	updated := map[string]bool{}
	for i, entry := range o.Config.Layers {
		s, err := spec.Parse(entry.Layer)
		if err != nil {
			return nil, err
		}
		if s.Name == "" {
			return nil, fmt.Errorf("%s: configured layer has no layer name", entry.Layer)
		}
		repo, err := o.Repo(ctx, s)
		if err != nil {
			return nil, err
		}
		if update && !updated[repo.Dir()] {
			if err := repo.Update(ctx); err != nil {
				return nil, err
			}
			updated[repo.Dir()] = true
		}
		rc, err := config.LoadRepo(filepath.Join(repo.Dir(), "config.yaml"))
		if err != nil {
			return nil, err
		}
		rootSpec := entry.Root
		if rootSpec == "" {
			rootSpec = rc.Root(s.Name)
		}
		root, err := config.ExpandRoot(rootSpec)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", s, err)
		}
		st, err := state.Load(state.FilePath(o.StateDir(), s.Host, s.Owner, s.Repo, s.Name), s.String())
		if err != nil {
			return nil, err
		}
		st.Root = root
		tombPath := filepath.Join(repo.Dir(), s.Name+".tombstones.yaml")
		tombs, err := config.LoadTombstones(tombPath)
		if err != nil {
			return nil, err
		}
		layers = append(layers, &Layer{
			Spec:       s,
			Exclude:    exclusions,
			Root:       root,
			Index:      i,
			Repo:       repo,
			Dir:        filepath.Join(repo.Dir(), s.Name),
			State:      st,
			Tombstones: tombs,
			tombPath:   tombPath,
		})
	}
	return layers, nil
}

// SaveStates writes back the state of every layer.
func SaveStates(layers []*Layer) error {
	for _, l := range layers {
		if err := l.State.Save(); err != nil {
			return err
		}
	}
	return nil
}

// Content describes a file in a layer's directory.
type Content struct {
	Hash string
	Exec bool
}

// Scan returns the files the layer's directory provides, keyed by
// layer-relative path. A layer whose directory is missing provides
// nothing.
func (l *Layer) Scan() (map[string]Content, error) {
	files := map[string]Content{}
	err := filepath.WalkDir(l.Dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) && path == l.Dir {
				return filepath.SkipAll
			}
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			// Symbolic links and the like are not overlay content.
			return nil
		}
		rel, err := filepath.Rel(l.Dir, path)
		if err != nil {
			return err
		}
		hash, exec, err := state.HashFile(path)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(rel)] = Content{Hash: hash, Exec: exec}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return files, nil
}

// ResolveSpecs expands layer specifications against their repositories,
// turning a bare repository into all of its layers and a set name into
// its members. The results are in the order the user gave them.
func (o *Over) ResolveSpecs(ctx context.Context, args []string) ([]spec.Spec, error) {
	var out []spec.Spec
	for _, arg := range args {
		s, err := spec.Parse(arg)
		if err != nil {
			return nil, err
		}
		repo, err := o.Repo(ctx, s)
		if err != nil {
			return nil, err
		}
		if err := repo.Update(ctx); err != nil {
			return nil, err
		}
		rc, err := config.LoadRepo(filepath.Join(repo.Dir(), "config.yaml"))
		if err != nil {
			return nil, err
		}
		names, err := rc.LayerNames(repo.Dir())
		if err != nil {
			return nil, err
		}
		switch {
		case s.Name == "":
			if len(names) == 0 {
				return nil, fmt.Errorf("%s: repository provides no layers", s)
			}
			for _, name := range names {
				out = append(out, s.WithName(name))
			}
		default:
			if members, ok := rc.Set(s.Name); ok {
				if len(members) == 0 {
					return nil, fmt.Errorf("%s: set %s is empty", s, s.Name)
				}
				for _, name := range members {
					out = append(out, s.WithName(name))
				}
				continue
			}
			if !contains(names, s.Name) {
				return nil, fmt.Errorf("%s: no such layer or set in %s (have %s)",
					s, s.Repository(), strings.Join(names, ", "))
			}
			out = append(out, s)
		}
	}
	return out, nil
}

// Add inserts the given layers into the configuration. They are appended
// unless before names a configured layer, in which case they are
// inserted ahead of it. A non-empty root overrides the root each added
// layer is materialized under. Layers already configured are skipped.
// Add returns the specifications it added.
func (o *Over) Add(ctx context.Context, args []string, before, root string) ([]spec.Spec, error) {
	specs, err := o.ResolveSpecs(ctx, args)
	if err != nil {
		return nil, err
	}
	if root != "" {
		if _, err := config.ExpandRoot(root); err != nil {
			return nil, err
		}
	}
	at := len(o.Config.Layers)
	if before != "" {
		bs, err := spec.Parse(before)
		if err != nil {
			return nil, err
		}
		if at = o.Config.Index(bs.String()); at < 0 {
			return nil, fmt.Errorf("%s: not a configured layer", bs)
		}
	}
	var added []spec.Spec
	var entries []config.Entry
	for _, s := range specs {
		if o.Config.Index(s.String()) >= 0 {
			continue
		}
		entries = append(entries, config.Entry{Layer: s.String(), Root: root})
		added = append(added, s)
	}
	if len(entries) == 0 {
		return nil, nil
	}
	layers := o.Config.Layers
	merged := make([]config.Entry, 0, len(layers)+len(entries))
	merged = append(merged, layers[:at]...)
	merged = append(merged, entries...)
	merged = append(merged, layers[at:]...)
	o.Config.Layers = merged
	return added, o.SaveConfig()
}

// Remove deletes the named layers from the configuration. It leaves the
// local files and the layer's state alone: removing a layer stops over
// from managing its files, it does not unmake them.
func (o *Over) Remove(args []string) ([]spec.Spec, error) {
	var removed []spec.Spec
	for _, arg := range args {
		s, err := spec.Parse(arg)
		if err != nil {
			return nil, err
		}
		i := o.Config.Index(s.String())
		if i < 0 {
			return nil, fmt.Errorf("%s: not a configured layer", s)
		}
		o.Config.Layers = append(o.Config.Layers[:i], o.Config.Layers[i+1:]...)
		removed = append(removed, s)
	}
	if len(removed) == 0 {
		return nil, nil
	}
	return removed, o.SaveConfig()
}

// Entry returns the configuration entry for a layer.
func (o *Over) Entry(layer string) (config.Entry, error) {
	s, err := spec.Parse(layer)
	if err != nil {
		return config.Entry{}, err
	}
	i := o.Config.Index(s.String())
	if i < 0 {
		return config.Entry{}, fmt.Errorf("%s: not a configured layer", s)
	}
	return o.Config.Layers[i], nil
}

// SetRoot overrides the root of a configured layer. An empty root clears
// the override, restoring the layer's own default.
func (o *Over) SetRoot(layer, root string) error {
	s, err := spec.Parse(layer)
	if err != nil {
		return err
	}
	i := o.Config.Index(s.String())
	if i < 0 {
		return fmt.Errorf("%s: not a configured layer", s)
	}
	if root != "" {
		if _, err := config.ExpandRoot(root); err != nil {
			return err
		}
	}
	o.Config.Layers[i].Root = root
	return o.SaveConfig()
}

// contains reports whether the sorted slice ss holds s.
func contains(ss []string, s string) bool {
	i := sort.SearchStrings(ss, s)
	return i < len(ss) && ss[i] == s
}
