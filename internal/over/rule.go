package over

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mariusae/over/internal/config"
	"github.com/mariusae/over/internal/pathspec"
	"github.com/mariusae/over/internal/spec"
)

// A layer's tracking rules answer, once and for every machine, the
// question "over track" otherwise answers one file at a time: which
// local files does this layer claim? A file matching a track rule is
// written to the layer on the next sync though nobody named it, which is
// what makes a layer a standing arrangement rather than a list.
//
// Rules bear on claiming only. What a layer holds is what a layer holds:
// a file already in it is synced whatever the rules say, because
// somebody published it deliberately. Ignore rules are the counterweight
// to track rules, not a way to disown content.
//
// Rules live with the layer, in the repository's config.yaml, and are
// written relative to the layer's root so that they mean the same thing
// wherever the layer is materialized.
//
// The client's own exclusions outrank all of this: a path over refuses
// to manage is never claimed, whatever a layer asks for.

// The kinds of rule, as they are named in the configuration and on the
// command line.
const (
	RuleTrack  = "track"
	RuleIgnore = "ignore"
)

// Claims reports whether the layer's rules claim a local path, and so
// whether over will write it to the layer without being told to.
func (l *Layer) Claims(local string) bool {
	if l.Excluded(local) {
		return false
	}
	if !pathspec.Patterns(l.Track).MatchAny(local) {
		return false
	}
	return !pathspec.Patterns(l.Ignore).MatchAny(local)
}

// ClaimedPaths returns the layer-relative paths of the local files the
// layer's rules claim. Each rule is anchored at a literal prefix, so the
// walk starts there rather than at the root.
func (l *Layer) ClaimedPaths() ([]string, error) {
	var paths []string
	for _, base := range walkRoots(l.Track) {
		err := filepath.WalkDir(base, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				if os.IsNotExist(err) && path == base {
					// A rule may name a directory this machine does
					// not have; that is not an error, just nothing.
					return filepath.SkipAll
				}
				return err
			}
			if d.IsDir() {
				if d.Name() == ".git" {
					// A repository's own innards are never a layer's
					// content, whatever the rule says.
					return filepath.SkipDir
				}
				return nil
			}
			if !d.Type().IsRegular() || !l.Claims(path) {
				return nil
			}
			rel, err := filepath.Rel(l.Root, path)
			if err != nil {
				return err
			}
			paths = append(paths, filepath.ToSlash(rel))
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return paths, nil
}

// walkRoots returns the directories that need walking to find everything
// a set of patterns could match, dropping any that lie under another.
func walkRoots(pats []pathspec.Pattern) []string {
	bases := make([]string, 0, len(pats))
	for _, p := range pats {
		if base := p.Base(); base != "" {
			bases = append(bases, base)
		}
	}
	sort.Strings(bases)
	roots := bases[:0]
	for _, base := range bases {
		if n := len(roots); n > 0 {
			if last := roots[n-1]; base == last || strings.HasPrefix(base, last+string(filepath.Separator)) {
				continue
			}
		}
		roots = append(roots, base)
	}
	return roots
}

// ParseRules parses a layer's rules, which are written relative to its
// root.
func ParseRules(root string, rules []string) ([]pathspec.Pattern, error) {
	pats := make([]pathspec.Pattern, 0, len(rules))
	for _, rule := range rules {
		pat, err := ParseRule(root, rule)
		if err != nil {
			return nil, err
		}
		pats = append(pats, pat)
	}
	return pats, nil
}

// ParseRule parses one rule against a layer root. A rule is relative to
// the root and must stay under it: a rule is a property of the layer,
// and has to mean the same thing on every machine that adds it.
func ParseRule(root, rule string) (pathspec.Pattern, error) {
	switch {
	case rule == "":
		return pathspec.Pattern{}, fmt.Errorf("empty rule")
	case rule == "...":
		return pathspec.Pattern{}, fmt.Errorf(`%s: too broad; anchor the rule at a directory, as in ".config/..."`, rule)
	case filepath.IsAbs(rule):
		return pathspec.Pattern{}, fmt.Errorf("%s: a rule is relative to the layer's root", rule)
	}
	pat, err := pathspec.Parse(root, rule)
	if err != nil {
		return pathspec.Pattern{}, err
	}
	if !under(pat.Base(), root) {
		return pathspec.Pattern{}, fmt.Errorf("%s: outside the layer's root, %s", rule, root)
	}
	return pat, nil
}

// RelativeRule turns a path argument into the rule that expresses it:
// relative to the layer's root, with its wildcards intact.
func RelativeRule(l *Layer, pat pathspec.Pattern) (string, error) {
	abs := pat.Abs()
	if abs == "..." {
		// The pattern anchored to nowhere, so there is nothing to make
		// relative; say why rather than complain about the root.
		return "", fmt.Errorf(`...: too broad; anchor the rule at a directory, as in ".config/..."`)
	}
	rel, err := filepath.Rel(l.Root, abs)
	if err != nil || !under(abs, l.Root) {
		return "", fmt.Errorf("%s: outside %s, the root of %s", abs, l.Root, l)
	}
	return filepath.ToSlash(rel), nil
}

// under reports whether a path lies at or beneath a directory.
func under(path, dir string) bool {
	return path == dir || strings.HasPrefix(path, dir+string(filepath.Separator))
}

// LayerRules returns a layer's rules of the given kind, as they are
// written in its repository.
func (o *Over) LayerRules(s spec.Spec, kind string) []string {
	rc, err := config.LoadRepo(filepath.Join(o.repoDir(s.Repository()), "config.yaml"))
	if err != nil {
		return nil
	}
	if kind == RuleIgnore {
		return rc.Ignore(s.Name)
	}
	return rc.Track(s.Name)
}

// Rules adds patterns to a layer's track or ignore list, or, when remove
// is set, drops them from either. The change is made in the layer's
// repository and pushed: rules belong to the layer, so changing one
// changes it for every machine that adds it.
func (o *Over) Rules(ctx context.Context, s spec.Spec, kind string, patterns []string, remove bool, origin Origin) ([]string, error) {
	repo, err := o.Repo(ctx, s)
	if err != nil {
		return nil, err
	}
	if err := repo.Update(ctx); err != nil {
		return nil, err
	}
	path := filepath.Join(repo.Dir(), "config.yaml")

	var (
		changed []string
		verb    string
	)
	if remove {
		verb = "drop"
		changed, err = config.RemoveRule(path, s.Name, patterns)
	} else {
		verb = kind
		changed, err = config.AddRule(path, s.Name, kind, patterns)
	}
	if err != nil {
		return nil, err
	}
	if len(changed) == 0 {
		return nil, nil
	}

	lines := make([]string, 0, len(changed))
	for _, pattern := range changed {
		lines = append(lines, fmt.Sprintf("%s %s in %s", verb, pattern, s.Name))
	}
	subject := fmt.Sprintf("config: %s %s in %s", verb, strings.Join(changed, ", "), s.Name)
	if _, err := repo.Commit(ctx, Message(subject, lines, origin)); err != nil {
		return nil, err
	}
	if err := repo.Push(ctx); err != nil {
		return nil, err
	}
	return changed, nil
}
