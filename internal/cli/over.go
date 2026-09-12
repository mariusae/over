package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mariusae/over/internal/over"
	"github.com/mariusae/over/internal/pathspec"
	"github.com/mariusae/over/internal/spec"
)

// openOver loads the client configuration. The over home and cache
// directories come from the environment: $OVER_HOME and $OVER_CACHE
// override them outright, and otherwise the XDG base directories apply.
func openOver(env *Env) (*over.Over, error) {
	home, err := dirFromEnv("OVER_HOME", "XDG_CONFIG_HOME", ".config")
	if err != nil {
		return nil, err
	}
	cache, err := dirFromEnv("OVER_CACHE", "XDG_CACHE_HOME", ".cache")
	if err != nil {
		return nil, err
	}
	o, err := over.Open(over.Options{Home: home, Cache: cache, URL: urlFunc()})
	if err != nil {
		return nil, err
	}
	env.Logf("over home %s, cache %s", home, cache)
	return o, nil
}

// dirFromEnv resolves one of over's directories: $override if it is set,
// else over/ under $base, else over/ under $HOME/fallback.
func dirFromEnv(override, base, fallback string) (string, error) {
	if dir := os.Getenv(override); dir != "" {
		return pathspec.Resolve(filepath.Clean(dir)), nil
	}
	if dir := os.Getenv(base); dir != "" {
		return pathspec.Resolve(filepath.Join(dir, "over")), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return pathspec.Resolve(filepath.Join(home, fallback, "over")), nil
}

// urlFunc returns the function mapping a layer's repository to a git
// URL. The default is SSH, which works wherever the user's own git
// does. $OVER_URL overrides it; it is a format string taking the host,
// owner, and repository as %[1]s, %[2]s, and %[3]s, so HTTPS is
//
//	OVER_URL='https://%[1]s/%[2]s/%[3]s.git'
func urlFunc() func(spec.Spec) string {
	tmpl := os.Getenv("OVER_URL")
	if tmpl == "" {
		return over.DefaultURL
	}
	return func(s spec.Spec) string {
		return fmt.Sprintf(tmpl, s.Host, s.Owner, s.Repo)
	}
}

// session is a loaded client: its layers, and over's verdict on every
// file they cover.
type session struct {
	Over   *over.Over
	Layers []*over.Layer

	// Changes are the planned changes matching the path arguments.
	Changes []over.Change

	// All is every planned change, before the path arguments are
	// applied.
	All []over.Change

	// Patterns are the parsed path arguments.
	Patterns []pathspec.Pattern
}

// load resolves the configured layers and plans the changes, keeping
// only those whose local path matches one of the path arguments. When
// update is set the layers' repositories are fetched first. It is an
// error for path arguments to match nothing.
func load(ctx context.Context, env *Env, update bool, args []string) (*session, error) {
	s, err := loadAll(ctx, env, update, args)
	if err != nil {
		return nil, err
	}
	if len(args) > 0 && len(s.Changes) == 0 {
		return nil, fmt.Errorf("%s: no tracked files match", strings.Join(args, " "))
	}
	return s, nil
}

// loadAll is load without the requirement that the path arguments match
// something. Commands that report on a path rather than act on it want
// to say so themselves.
func loadAll(ctx context.Context, env *Env, update bool, args []string) (*session, error) {
	o, err := openOver(env)
	if err != nil {
		return nil, err
	}
	if len(o.Config.Layers) == 0 {
		return nil, over.ErrNoLayers
	}
	pats, err := pathspec.ParseAll(env.Dir, args)
	if err != nil {
		return nil, err
	}
	layers, err := o.Layers(ctx, update)
	if err != nil {
		return nil, err
	}
	changes, err := over.Plan(layers)
	if err != nil {
		return nil, err
	}
	return &session{
		Over:     o,
		Layers:   layers,
		Changes:  over.Filter(changes, pathspec.Patterns(pats).Match),
		All:      changes,
		Patterns: pats,
	}, nil
}

// Save writes back the state of every layer.
func (s *session) Save() error { return over.SaveStates(s.Layers) }

// describe renders one change the way sync reports it, or returns false
// for changes that have nothing to report.
func describe(env *Env, c *over.Change) (string, bool) {
	path := over.RelTo(env.Dir, c.Local)
	switch c.Status {
	case over.Pull:
		return fmt.Sprintf("%s from %s", path, c.Layer), true
	case over.PullDelete:
		return fmt.Sprintf("%s deleted from %s", path, c.Layer), true
	case over.Push:
		if c.Base == nil {
			// Nobody named this one: a tracking rule claimed it.
			return fmt.Sprintf("%s to %s (claimed)", path, c.Layer), true
		}
		return fmt.Sprintf("%s to %s", path, c.Layer), true
	case over.PushDelete:
		return fmt.Sprintf("%s deleted in %s", path, c.Layer), true
	case over.Conflict:
		return fmt.Sprintf("%s conflicts", path), true
	case over.Shadowed:
		return fmt.Sprintf("%s shadowed in %s", path, c.Layer), true
	}
	return "", false
}

// hintPath names the path a trailing hint should use. A hint is printed
// once for the whole command, however many paths the arguments expanded
// to, so it can only name one when there was only one.
func hintPath(paths []string) string {
	if len(paths) == 1 {
		return paths[0]
	}
	return "<path>"
}

// describeTracked renders a change for a full listing, where the files
// nothing is happening to are named too. Only the layer that owns a file
// speaks for it, so that each file appears once however many layers hold
// it.
func describeTracked(env *Env, c *over.Change) (string, bool) {
	if line, ok := describe(env, c); ok {
		return line, true
	}
	if !c.Owner || !c.Tracked() {
		return "", false
	}
	return fmt.Sprintf("%s unchanged in %s", over.RelTo(env.Dir, c.Local), c.Layer), true
}

// summarize renders the count line that ends sync and status.
func summarize(n over.Counts) string {
	var parts []string
	add := func(count int, noun string) {
		if count == 0 {
			return
		}
		if count == 1 {
			parts = append(parts, fmt.Sprintf("%d %s", count, noun))
			return
		}
		parts = append(parts, fmt.Sprintf("%d %ss", count, noun))
	}
	add(n.Conflicts, "conflict")
	if n.Updated > 0 {
		parts = append(parts, fmt.Sprintf("%d updated", n.Updated))
	}
	if n.Written > 0 {
		parts = append(parts, fmt.Sprintf("%d written", n.Written))
	}
	if n.Shadowed > 0 {
		parts = append(parts, fmt.Sprintf("%d shadowed", n.Shadowed))
	}
	if n.Unchanged > 0 {
		parts = append(parts, fmt.Sprintf("%d unchanged", n.Unchanged))
	}
	if len(parts) == 0 {
		return "nothing to do"
	}
	return strings.Join(parts, ", ")
}

// pathArgs is the description shared by the commands that take path
// arguments, appended to their help text.
const pathArgs = `
A path argument names files by where they live on disk, relative to the
working directory unless it is absolute. The element "..." matches any
number of path elements, so ".config/..." names everything under
.config; "..." alone names every file over tracks. A path argument with
no "..." names one file, or, if it is a directory, everything under it.`
