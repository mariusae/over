package over

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mariusae/over/internal/config"
	"github.com/mariusae/over/internal/pathspec"
)

// Exclusions are paths that no layer manages, whatever its root. They
// are written as path patterns, in the same form the commands take, and
// are expanded for environment variables:
//
//	$HOME/.config/over/...
//
// An exclusion is a standing rule rather than a one-time edit: over
// skips these paths on every sync, in every layer. That is what
// separates it from untracking a file, which forgets it once and leaves
// the next sync free to pick it up again.
//
// A new configuration is seeded with over's own home and cache, which
// hold the state sync writes as it runs. A layer carrying them would
// dirty its own input on every sync and never settle.

// Exclusions returns the configured exclusions, parsed.
func (o *Over) Exclusions() ([]pathspec.Pattern, error) {
	pats := make([]pathspec.Pattern, 0, len(o.Config.Exclude))
	for _, raw := range o.Config.Exclude {
		pat, err := ExclusionPattern(raw)
		if err != nil {
			return nil, err
		}
		pats = append(pats, pat)
	}
	return pats, nil
}

// ExpandExclusion returns the absolute path pattern an exclusion names,
// with environment variables expanded.
func ExpandExclusion(raw string) (string, error) {
	expanded, err := config.ExpandVars(raw)
	if err != nil {
		return "", fmt.Errorf("exclude %w", err)
	}
	if !filepath.IsAbs(expanded) {
		return "", fmt.Errorf("exclude %s: %s is not an absolute path", raw, expanded)
	}
	return expanded, nil
}

// ExclusionPattern parses an exclusion into a matcher.
func ExclusionPattern(raw string) (pathspec.Pattern, error) {
	expanded, err := ExpandExclusion(raw)
	if err != nil {
		return pathspec.Pattern{}, err
	}
	return pathspec.Parse("/", expanded)
}

// DefaultExclusions returns the exclusions a new configuration starts
// with: over's own home and cache directories, written against $HOME
// where they lie under it, so that the configuration reads the same on
// every machine.
func DefaultExclusions(dirs ...string) []string {
	out := make([]string, 0, len(dirs))
	for _, dir := range dirs {
		out = append(out, underHome(dir)+"/...")
	}
	return out
}

// underHome rewrites a path beneath the home directory to use $HOME.
func underHome(dir string) string {
	home := os.Getenv("HOME")
	if home == "" {
		return dir
	}
	home = pathspec.Resolve(filepath.Clean(home))
	rel, err := filepath.Rel(home, dir)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return dir
	}
	if rel == "." {
		return "$HOME"
	}
	return "$HOME/" + filepath.ToSlash(rel)
}

// AddExclude adds patterns to the exclusion list, skipping those already
// excluded. It returns the entries it added.
func (o *Over) AddExclude(paths []string) ([]string, error) {
	var added []string
	for _, raw := range paths {
		if _, err := ExclusionPattern(raw); err != nil {
			return nil, err
		}
		if i, _ := o.findExclude(raw); i >= 0 {
			continue
		}
		o.Config.Exclude = append(o.Config.Exclude, raw)
		added = append(added, raw)
	}
	if len(added) == 0 {
		return nil, nil
	}
	return added, o.SaveConfig()
}

// RemoveExclude drops patterns from the exclusion list. A pattern may be
// given as it was written or as it expands.
func (o *Over) RemoveExclude(paths []string) ([]string, error) {
	var removed []string
	for _, raw := range paths {
		i, _ := o.findExclude(raw)
		if i < 0 {
			return nil, fmt.Errorf("%s: not an exclusion; run 'over exclude' for the list", raw)
		}
		removed = append(removed, o.Config.Exclude[i])
		o.Config.Exclude = append(o.Config.Exclude[:i], o.Config.Exclude[i+1:]...)
	}
	if len(removed) == 0 {
		return nil, nil
	}
	return removed, o.SaveConfig()
}

// findExclude returns the position of a configured exclusion, matching
// either the text as written or the path it expands to, or -1. An
// exclusion whose variables are no longer set can still be matched by
// its literal text, which is the only way to remove one.
func (o *Over) findExclude(raw string) (int, bool) {
	expanded, err := ExpandExclusion(raw)
	if err != nil {
		expanded = ""
	}
	for i, have := range o.Config.Exclude {
		if have == raw {
			return i, true
		}
		if expanded == "" {
			continue
		}
		if h, err := ExpandExclusion(have); err == nil && h == expanded {
			return i, true
		}
	}
	return -1, false
}
