// Package pathspec matches the path arguments over's commands take.
//
// A path argument names files by their location on disk. It is resolved
// relative to the working directory unless it is absolute. The element
// "..." matches any sequence of characters, including separators, so
//
//	.config/...
//
// names every file under .config, recursively. The argument "..." on its
// own names every file over tracks, wherever it is rooted. A path
// argument with no "..." names one file exactly, or, when it names a
// directory, everything beneath it.
package pathspec

import (
	"fmt"
	"path/filepath"
	"strings"
)

// Resolve returns path with symbolic links resolved as far as they
// exist. Paths reach over from several directions -- the working
// directory, a root expanded from the environment, a command line
// argument -- and they have to compare equal, which on systems where
// the home directory sits behind a link they otherwise would not. The
// part of the path that does not exist yet is kept as written.
func Resolve(path string) string {
	rest := ""
	for p := path; ; {
		if resolved, err := filepath.EvalSymlinks(p); err == nil {
			return filepath.Join(resolved, rest)
		}
		parent := filepath.Dir(p)
		if parent == p {
			return path
		}
		rest = filepath.Join(filepath.Base(p), rest)
		p = parent
	}
}

// A Pattern matches absolute paths.
type Pattern struct {
	arg   string   // as written by the user, for error messages
	all   bool     // matches everything
	parts []string // literal segments between "..." wildcards
	dir   bool     // literal pattern: also match everything beneath
}

// Parse parses a path argument, resolving relative paths against dir,
// which must be absolute.
func Parse(dir, arg string) (Pattern, error) {
	if arg == "" {
		return Pattern{}, fmt.Errorf("empty path argument")
	}
	if arg == "..." {
		return Pattern{arg: arg, all: true}, nil
	}
	abs := arg
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(dir, abs)
	}
	// filepath.Clean folds "..." the same as any other element, which
	// is what we want: it is an ordinary name to the cleaner.
	abs = Resolve(filepath.Clean(abs))
	if !strings.Contains(abs, "...") {
		return Pattern{arg: arg, parts: []string{abs}, dir: true}, nil
	}
	return Pattern{arg: arg, parts: strings.Split(abs, "...")}, nil
}

// ParseAll parses several path arguments.
func ParseAll(dir string, args []string) ([]Pattern, error) {
	pats := make([]Pattern, len(args))
	for i, arg := range args {
		p, err := Parse(dir, arg)
		if err != nil {
			return nil, err
		}
		pats[i] = p
	}
	return pats, nil
}

// String returns the argument the pattern was parsed from.
func (p Pattern) String() string { return p.arg }

// Match reports whether the absolute path matches.
func (p Pattern) Match(path string) bool {
	switch {
	case p.all:
		return true
	case p.dir:
		lit := p.parts[0]
		return path == lit || strings.HasPrefix(path, lit+string(filepath.Separator))
	}
	// Anchored at both ends, with the wildcards absorbing whatever
	// lies between the literal segments.
	first, last := p.parts[0], p.parts[len(p.parts)-1]
	if !strings.HasPrefix(path, first) {
		return false
	}
	rest := path[len(first):]
	for _, part := range p.parts[1 : len(p.parts)-1] {
		i := strings.Index(rest, part)
		if i < 0 {
			return false
		}
		rest = rest[i+len(part):]
	}
	return strings.HasSuffix(rest, last)
}

// Path returns the single location the pattern is anchored to, and
// whether it has one. A pattern with no "..." names exactly one place on
// disk -- a file, or a directory and everything under it -- which lets a
// command report on a path that matches nothing rather than silently
// finding no files.
func (p Pattern) Path() (string, bool) {
	if !p.dir {
		return "", false
	}
	return p.parts[0], true
}

// Base returns the directory the pattern is anchored to: the longest
// literal prefix of the pattern that names a directory. It is the place
// to start a file system walk that could match. Base returns the empty
// string for the pattern "...", which is anchored nowhere.
func (p Pattern) Base() string {
	switch {
	case p.all:
		return ""
	case p.dir:
		return p.parts[0]
	}
	lit := p.parts[0]
	i := strings.LastIndex(lit, string(filepath.Separator))
	if i <= 0 {
		return string(filepath.Separator)
	}
	return lit[:i]
}

// Patterns is a set of path arguments.
type Patterns []Pattern

// Match reports whether path matches any of the patterns, or whether
// there are none. The empty set matches everything, which is what
// commands that default to operating on all files want.
func (ps Patterns) Match(path string) bool {
	return len(ps) == 0 || ps.MatchAny(path)
}

// MatchAny reports whether path matches any of the patterns. Unlike
// Match, the empty set matches nothing, which is what a set of
// exclusions wants.
func (ps Patterns) MatchAny(path string) bool {
	for _, p := range ps {
		if p.Match(path) {
			return true
		}
	}
	return false
}
