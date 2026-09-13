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

	"github.com/mariusae/over/internal/auth"
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

	prompt func(auth.Prompt) error
	exe    string
	store  *auth.Store

	repos map[string]*gitrepo.Repo // by repository spec
}

// Options configure a client.
type Options struct {
	// Home is over's configuration directory, holding config.yaml and
	// the per-layer state files.
	Home string

	// Cache is the directory repository checkouts are kept in.
	Cache string

	// URL returns the git URL of a repository, overriding the
	// transport the configuration asks for. It is how $OVER_URL
	// reaches in; a nil URL leaves the choice to the configuration.
	URL func(spec.Spec) string

	// Prompt shows the user what they have to do to authorize over at
	// a host, and returns once it has been shown; over waits for the
	// host to say it was done. A nil Prompt means over cannot ask, and
	// a command that needs a credential it has not got fails with the
	// command to run rather than hanging on a person who may not be
	// there.
	Prompt func(auth.Prompt) error

	// Executable is the path to over's own binary, which git is
	// pointed at as a credential helper. It defaults to
	// os.Executable.
	Executable string
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
	o := &Over{
		home:   opts.Home,
		cache:  opts.Cache,
		url:    opts.URL,
		prompt: opts.Prompt,
		exe:    opts.Executable,
		repos:  map[string]*gitrepo.Repo{},
	}
	o.store = auth.NewStore(filepath.Join(opts.Home, "auth"))
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

// HTTPSURL returns the HTTPS git URL of a repository. This is over's
// default transport: reading a public repository over it needs no
// credential at all, and the credential anything else needs is one over
// can go and get, by sending the user to the host to authorize it.
func HTTPSURL(s spec.Spec) string {
	return fmt.Sprintf("https://%s/%s/%s.git", s.Host, s.Owner, s.Repo)
}

// SSHURL returns the SSH git URL of a repository, which is what a
// machine with a key already on it may prefer:
//
//	over host github.com ssh
func SSHURL(s spec.Spec) string {
	return fmt.Sprintf("git@%s:%s/%s.git", s.Host, s.Owner, s.Repo)
}

// URL returns the git URL over will use for a repository: the override
// $OVER_URL installs, or the transport the configuration asks for.
func (o *Over) URL(s spec.Spec) string {
	if o.url != nil {
		return o.url(s)
	}
	if o.Config.Transport(s.Host) == config.TransportSSH {
		return SSHURL(s)
	}
	return HTTPSURL(s)
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
	cfg, err := o.gitConfig(s)
	if err != nil {
		return nil, err
	}
	r, err := gitrepo.Open(ctx, o.repoDir(s), o.URL(s), cfg)
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
		if !f.Cloned {
			repo, err := o.Repo(ctx, s)
			if err != nil {
				return nil, err
			}
			if f.Before, err = repo.Head(ctx); err != nil {
				return nil, err
			}
		}
		repo, err := o.UpdateRepo(ctx, s)
		if err != nil {
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

	// Track and Ignore are the layer's tracking rules, parsed against
	// its root: the local files it claims, and those it will not.
	// See [Layer.Claims].
	Track  []pathspec.Pattern
	Ignore []pathspec.Pattern

	// IncludeBin lets the layer claim binary files. Without it a rule
	// takes only text, so that a rule over a directory of scripts does
	// not sweep up the compiled programs beside them.
	IncludeBin bool

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
	if err == nil {
		exclusions = append(exclusions, o.Vetoes()...)
	}
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
			dir := repo.Dir()
			if repo, err = o.UpdateRepo(ctx, s); err != nil {
				return nil, err
			}
			updated[dir] = true
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
		track, err := ParseRules(root, rc.Track(s.Name))
		if err != nil {
			return nil, fmt.Errorf("%s: track: %w", s, err)
		}
		ignore, err := ParseRules(root, rc.Ignore(s.Name))
		if err != nil {
			return nil, fmt.Errorf("%s: ignore: %w", s, err)
		}
		tombPath := filepath.Join(repo.Dir(), s.Name+".tombstones.yaml")
		tombs, err := config.LoadTombstones(tombPath)
		if err != nil {
			return nil, err
		}
		layers = append(layers, &Layer{
			Spec:       s,
			Exclude:    exclusions,
			Track:      track,
			Ignore:     ignore,
			IncludeBin: rc.IncludeBin(s.Name),
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

// ResolveSpecs expands layer specifications into the layers they name,
// in the order the arguments put them. A bare repository becomes every
// layer it provides, and a set becomes its members.
//
// Sets may name sets, and may name layers and sets in other
// repositories, so resolution is recursive and may open repositories the
// arguments never mentioned. It is also the only place in over where one
// short argument expands into something the user did not type, which is
// why the callers print what came back. A layer reached twice -- two sets
// sharing a member -- is emitted once, at its first position.
func (o *Over) ResolveSpecs(ctx context.Context, args []string) ([]spec.Spec, error) {
	r := &resolver{o: o, repos: map[string]*resolvedRepo{}, seen: map[string]bool{}, open: map[string]bool{}}
	for _, arg := range args {
		ref, err := spec.ParseRef(arg)
		if err != nil {
			return nil, err
		}
		if err := r.resolve(ctx, ref, nil); err != nil {
			return nil, err
		}
	}
	return r.out, nil
}

// A resolver expands references into layers. It remembers what it has
// emitted, so that a diamond among sets yields one layer rather than
// two, and which sets it is in the middle of expanding, so that a cycle
// is an error rather than a hang.
type resolver struct {
	o     *Over
	out   []spec.Spec
	repos map[string]*resolvedRepo // by repository, so each is opened once
	seen  map[string]bool          // layers already emitted
	open  map[string]bool          // sets being expanded, by canonical form
}

// A resolvedRepo is what resolution needs to know about one repository.
type resolvedRepo struct {
	config *config.Repo
	names  []string // the layers it provides, sorted
}

// resolve expands one reference. The path is the chain of sets that led
// here, for the error a cycle deserves.
func (r *resolver) resolve(ctx context.Context, ref spec.Ref, path []string) error {
	rr, err := r.repo(ctx, ref.Spec)
	if err != nil {
		return err
	}
	switch {
	case ref.Set:
		return r.resolveSet(ctx, ref, rr, path)
	case ref.Spec.Name == "":
		if len(rr.names) == 0 {
			return fmt.Errorf("%s: repository provides no layers; create one with 'over init %s:<layer>'", ref.Spec, ref.Spec)
		}
		for _, name := range rr.names {
			r.emit(ref.Spec.WithName(name))
		}
		return nil
	default:
		if !contains(rr.names, ref.Spec.Name) {
			return noLayer(ref, rr.config, rr.names)
		}
		r.emit(ref.Spec)
		return nil
	}
}

// resolveSet expands a set into its members.
func (r *resolver) resolveSet(ctx context.Context, ref spec.Ref, rr *resolvedRepo, path []string) error {
	key := ref.String()
	if r.open[key] {
		return fmt.Errorf("%s: set refers to itself: %s", key, strings.Join(append(path, key), " -> "))
	}
	members, ok := rr.config.Set(ref.Spec.Name)
	if !ok {
		return noSet(ref, rr.config, rr.names)
	}
	if len(members) == 0 {
		return fmt.Errorf("%s: set is empty", key)
	}
	r.open[key] = true
	defer delete(r.open, key)
	// Copy rather than share the backing array, so that one member's
	// chain does not show up in the next one's.
	path = append(path[:len(path):len(path)], key)
	for _, member := range members {
		m, err := spec.ParseMember(member, ref.Spec)
		if err != nil {
			return fmt.Errorf("%s: %w", key, err)
		}
		if err := r.resolve(ctx, m, path); err != nil {
			return err
		}
	}
	return nil
}

// emit appends a layer, unless it is already there.
func (r *resolver) emit(s spec.Spec) {
	key := s.String()
	if r.seen[key] {
		return
	}
	r.seen[key] = true
	r.out = append(r.out, s)
}

// repo opens a repository, updates it, and reads what resolution needs
// from it. Repositories are read once per resolution, so a set naming
// three layers of one repository fetches it once.
func (r *resolver) repo(ctx context.Context, s spec.Spec) (*resolvedRepo, error) {
	key := s.Repository().String()
	if rr, ok := r.repos[key]; ok {
		return rr, nil
	}
	_, rc, names, err := r.o.OpenRepoConfig(ctx, s)
	if err != nil {
		return nil, err
	}
	rr := &resolvedRepo{config: rc, names: names}
	r.repos[key] = rr
	return rr, nil
}

// OpenRepoConfig opens the repository a specification names, brings it up
// to date, and reads its configuration and the layers it provides. The
// repository need not be one of the configured layers': declaring a set
// is a thing one does to a repository before adding anything from it.
func (o *Over) OpenRepoConfig(ctx context.Context, s spec.Spec) (*gitrepo.Repo, *config.Repo, []string, error) {
	repo, err := o.UpdateRepo(ctx, s)
	if err != nil {
		return nil, nil, nil, err
	}
	rc, err := config.LoadRepo(filepath.Join(repo.Dir(), "config.yaml"))
	if err != nil {
		return nil, nil, nil, err
	}
	names, err := rc.LayerNames(repo.Dir())
	if err != nil {
		return nil, nil, nil, err
	}
	return repo, rc, names, nil
}

// noLayer explains a layer that is not there. A set of the same name is
// almost certainly what was meant, so it is named, spelled the way that
// would have worked.
func noLayer(ref spec.Ref, rc *config.Repo, names []string) error {
	if _, ok := rc.Set(ref.Spec.Name); ok {
		return fmt.Errorf("%s: %s is a set in %s, not a layer; name it as %s",
			ref, ref.Spec.Name, ref.Spec.Repository(), spec.SetRef(ref.Spec, ref.Spec.Name))
	}
	if len(names) == 0 {
		return fmt.Errorf("%s: %s provides no layers; create one with 'over init %s'",
			ref, ref.Spec.Repository(), ref)
	}
	return fmt.Errorf("%s: no such layer in %s (have %s); create it with 'over init %s'",
		ref, ref.Spec.Repository(), strings.Join(names, ", "), ref)
}

// noSet explains a set that is not there, and likewise points at the
// layer of the same name if there is one.
func noSet(ref spec.Ref, rc *config.Repo, names []string) error {
	if contains(names, ref.Spec.Name) {
		return fmt.Errorf("%s: %s is a layer in %s, not a set; name it as %s",
			ref, ref.Spec.Name, ref.Spec.Repository(), ref.Spec)
	}
	sets := rc.SetNames()
	if len(sets) == 0 {
		return fmt.Errorf("%s: %s declares no sets; create one with 'over set %s <layer>...'",
			ref, ref.Spec.Repository(), ref)
	}
	return fmt.Errorf("%s: no such set in %s (have %s)",
		ref, ref.Spec.Repository(), strings.Join(sets, ", "))
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
//
// A set or a bare repository removes what it stands for, so that a
// machine can be taken apart the way it was put together. That costs a
// fetch, since only the repository knows what a set means; a single layer
// is matched against the configuration literally, and so can be removed
// even when its repository has gone away.
func (o *Over) Remove(ctx context.Context, args []string) ([]spec.Spec, error) {
	var removed []spec.Spec
	for _, arg := range args {
		ref, err := spec.ParseRef(arg)
		if err != nil {
			return nil, err
		}
		if !ref.Set && ref.Spec.Name != "" {
			i := o.Config.Index(ref.Spec.String())
			if i < 0 {
				return nil, fmt.Errorf("%s: not a configured layer", ref.Spec)
			}
			o.Config.Layers = append(o.Config.Layers[:i], o.Config.Layers[i+1:]...)
			removed = append(removed, ref.Spec)
			continue
		}
		specs, err := o.ResolveSpecs(ctx, []string{arg})
		if err != nil {
			return nil, err
		}
		// A set whose members were not all added, or were removed one
		// at a time, still removes the ones that are there.
		var any bool
		for _, s := range specs {
			i := o.Config.Index(s.String())
			if i < 0 {
				continue
			}
			o.Config.Layers = append(o.Config.Layers[:i], o.Config.Layers[i+1:]...)
			removed = append(removed, s)
			any = true
		}
		if !any {
			return nil, fmt.Errorf("%s: none of its layers are configured", ref)
		}
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
