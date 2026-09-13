// Package config reads and writes over's configuration files.
//
// There are two kinds. The client configuration, kept in the over home
// directory, lists the layers the client tracks, in order. The
// repository configuration, kept in config.yaml at the root of a layer
// repository, describes the layers the repository provides and the sets
// that group them.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mariusae/over/internal/pathspec"
	"gopkg.in/yaml.v3"
)

// Config is over's client configuration. Layers are listed in
// increasing order of precedence: when two layers provide the same file,
// the last one wins.
type Config struct {
	// Layers are the configured layers, lowest precedence first.
	Layers []Entry `yaml:"layers"`

	// Exclude lists path patterns that no layer may manage, whatever
	// its root, in the form the commands take path arguments and
	// subject to environment variable expansion. A missing key is
	// seeded with over's own directories; an empty list means no
	// exclusions at all, which is why the key is always written.
	Exclude []string `yaml:"exclude"`

	// Hosts configures the hosts the layer repositories live on,
	// keyed by host name. A host that says nothing gets the defaults.
	Hosts map[string]Host `yaml:"hosts,omitempty"`
}

// A Host is the client's configuration for one repository host.
type Host struct {
	// Transport is how over reaches the host: "https", the default, or
	// "ssh".
	//
	// HTTPS is the default because reading a public repository over it
	// needs no credential at all, and because the credential it does
	// need for anything else is one over can obtain by sending the
	// user to the host -- where a key is something over can only hope
	// is already there. SSH is for the machine that has one and would
	// rather use it.
	Transport string `yaml:"transport,omitempty"`
}

// Transports are the values [Host.Transport] may take.
const (
	TransportHTTPS = "https"
	TransportSSH   = "ssh"
)

// Transport returns how over should reach a host.
func (c *Config) Transport(host string) string {
	if h, ok := c.Hosts[host]; ok && h.Transport != "" {
		return h.Transport
	}
	return TransportHTTPS
}

// CheckTransport rejects a transport over does not know.
func CheckTransport(t string) error {
	switch t {
	case TransportHTTPS, TransportSSH:
		return nil
	}
	return fmt.Errorf("%q is not a transport; use %q or %q", t, TransportHTTPS, TransportSSH)
}

// An Entry is one configured layer.
type Entry struct {
	// Layer is the layer specification, e.g.
	// "mariusae/config:editors".
	Layer string `yaml:"layer"`

	// Root overrides the root directory the layer is materialized
	// under. It is subject to environment variable expansion. An empty
	// Root means the layer's own default applies.
	Root string `yaml:"root,omitempty"`
}

// Load reads the configuration stored at path. A missing file yields an
// empty configuration.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return new(Config), nil
	} else if err != nil {
		return nil, err
	}
	cfg := new(Config)
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	for i, e := range cfg.Layers {
		if e.Layer == "" {
			return nil, fmt.Errorf("%s: layer %d has no layer key", path, i+1)
		}
	}
	return cfg, nil
}

// Save writes the configuration to path, creating its directory if
// needed. The write is atomic.
func (c *Config) Save(path string) error {
	data, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	return WriteFile(path, data, 0o644)
}

// Index returns the position of the layer named by spec, or -1.
func (c *Config) Index(spec string) int {
	for i, e := range c.Layers {
		if e.Layer == spec {
			return i
		}
	}
	return -1
}

// Repo is the configuration a layer repository keeps in its top-level
// config.yaml. Both fields are optional: a repository that declares
// nothing still provides every top-level directory as a layer, rooted at
// $HOME.
type Repo struct {
	// Layers configures the repository's layers, keyed by layer name.
	// A layer need not appear here; the defaults are imputed.
	Layers map[string]RepoLayer `yaml:"layers,omitempty"`

	// Sets names groups of layers, keyed by set name. A set is named
	// with a doubled colon, as "owner/repo::mac", so sets and layers
	// do not share a namespace and neither can shadow the other.
	//
	// A member is written relative to this repository -- a bare name
	// is one of its layers, "::name" another of its sets -- or as a
	// whole specification, which is how a set reaches into another
	// repository. Members expand in order, and a set may name a set.
	Sets map[string][]string `yaml:"sets,omitempty"`
}

// A RepoLayer is the repository's configuration for a single layer.
type RepoLayer struct {
	// Root is the directory the layer is materialized under, subject
	// to environment variable expansion. It defaults to "$HOME".
	Root string `yaml:"root,omitempty"`

	// Track holds the layer's tracking rules: patterns, relative to
	// the layer's root, naming the local files the layer claims. A
	// file matching one of them is written to the layer even though
	// nobody named it, which is what makes a layer a standing
	// arrangement rather than a list.
	Track []string `yaml:"track,omitempty"`

	// Ignore holds patterns the layer will not claim, whatever Track
	// says. It is the counterweight to Track, and applies only to
	// claiming: a file the layer already holds is still synced, since
	// somebody published it deliberately.
	Ignore []string `yaml:"ignore,omitempty"`

	// IncludeBin lets the layer claim binary files. Without it a rule
	// takes only text, so that a rule over a directory of scripts does
	// not sweep up the compiled programs beside them.
	IncludeBin bool `yaml:"includebin,omitempty"`
}

// LoadRepo reads a repository configuration from path. A missing file
// yields an empty configuration.
func LoadRepo(path string) (*Repo, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return new(Repo), nil
	} else if err != nil {
		return nil, err
	}
	r := new(Repo)
	if err := yaml.Unmarshal(data, r); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return r, nil
}

// Root returns the configured root of the named layer, or "$HOME" if the
// layer is not configured.
func (r *Repo) Root(name string) string {
	if l, ok := r.Layers[name]; ok && l.Root != "" {
		return l.Root
	}
	return "$HOME"
}

// Track returns the named layer's tracking rules.
func (r *Repo) Track(name string) []string { return r.Layers[name].Track }

// Ignore returns the patterns the named layer will not claim.
func (r *Repo) Ignore(name string) []string { return r.Layers[name].Ignore }

// IncludeBin reports whether the named layer claims binary files.
func (r *Repo) IncludeBin(name string) bool { return r.Layers[name].IncludeBin }

// Set returns the members of the named set, and whether it exists.
func (r *Repo) Set(name string) ([]string, bool) {
	members, ok := r.Sets[name]
	return members, ok
}

// SetNames returns the names of the sets the repository declares,
// sorted. Unlike layers, a set exists only where it is declared: nothing
// on disk implies one.
func (r *Repo) SetNames() []string {
	names := make([]string, 0, len(r.Sets))
	for name := range r.Sets {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// LayerNames returns the names of the layers the repository provides:
// every layer declared in the configuration, plus every top-level
// directory of dir that is not hidden. The result is sorted.
func (r *Repo) LayerNames(dir string) ([]string, error) {
	seen := map[string]bool{}
	for name := range r.Layers {
		seen[name] = true
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	for _, ent := range ents {
		if !ent.IsDir() || strings.HasPrefix(ent.Name(), ".") {
			continue
		}
		seen[ent.Name()] = true
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

// ExpandVars expands environment variables in path, reporting any that
// are not set.
func ExpandVars(path string) (string, error) {
	var missing []string
	expanded := os.Expand(path, func(key string) string {
		v, ok := os.LookupEnv(key)
		if !ok {
			missing = append(missing, key)
		}
		return v
	})
	if len(missing) > 0 {
		return "", fmt.Errorf("%s: undefined environment variable %s", path, strings.Join(missing, ", "))
	}
	return expanded, nil
}

// ExpandPath expands environment variables in path and returns the
// canonical absolute path they name.
func ExpandPath(path string) (string, error) {
	expanded, err := ExpandVars(path)
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(expanded) {
		return "", fmt.Errorf("%s: %s is not an absolute path", path, expanded)
	}
	return pathspec.Resolve(filepath.Clean(expanded)), nil
}

// ExpandRoot is ExpandPath for a layer root, where an empty value means
// $HOME.
func ExpandRoot(root string) (string, error) {
	if root == "" {
		root = "$HOME"
	}
	return ExpandPath(root)
}

// WriteFile writes data to path atomically, creating the parent
// directory if it does not exist.
func WriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), perm); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
