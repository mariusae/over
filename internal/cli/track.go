package cli

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mariusae/over/internal/over"
	"github.com/mariusae/over/internal/pathspec"
	"github.com/mariusae/over/internal/spec"
)

func init() {
	Register(trackCmd, untrackCmd)
}

var trackCmd = &Command{
	Name:  "track",
	Usage: "track <layer> <path>...",
	Short: "add local files to a layer",
	Long: `Track puts local files under over's management, so that the next
sync writes them to a layer and later syncs keep them there. It does not
copy anything yet, and it does not touch the files.

The layer comes first, and is named in full:

	over track mariusae/config:editors .config/ion/config

Which layer a file belongs in decides where it is published and who
else receives it, so over does not guess: the layer must be one that is
already configured, and every path must lie under its root. Files that
over already tracks are left as they are.

A path argument with a wildcard in it names no particular file, so it is
recorded as a tracking rule rather than expanded once:

	over track mariusae/config:editors .config/ion/...

The layer then claims everything under .config/ion, including files made
later, on every machine that adds it. Use "over rule" to see a layer's
rules, to take one back, or to add the ignore rules that carve exceptions
out of it.

Track is the counterpart of sync's other direction: sync discovers new
files in a layer by itself, but a new local file is only over's business
once it has been tracked or claimed.
` + pathArgs,
	Run: runTrack,
}

func runTrack(ctx context.Context, env *Env, args []string) error {
	if len(args) < 2 {
		return Usagef("expected a layer and at least one path")
	}
	layerArg, paths := args[0], args[1:]
	if _, err := spec.Parse(layerArg); err != nil {
		return Usagef("%v; the first argument names the layer to track in", err)
	}
	o, err := openOver(env)
	if err != nil {
		return err
	}
	layers, err := o.Layers(ctx, false)
	if err != nil {
		return err
	}
	if len(layers) == 0 {
		return over.ErrNoLayers
	}
	l := findLayer(layers, layerArg)
	if l == nil {
		return fmt.Errorf("%s: not a configured layer; run 'over status' for the list", layerArg)
	}

	// A wildcard names no particular file, so it becomes a rule; a
	// plain path names one, and is tracked as it stands.
	var rules, named []string
	var rulePats []pathspec.Pattern
	for _, arg := range paths {
		pat, err := pathspec.Parse(env.Dir, arg)
		if err != nil {
			return err
		}
		if !pat.Wild() {
			named = append(named, arg)
			continue
		}
		rule, err := over.RelativeRule(l, pat)
		if err != nil {
			return err
		}
		rules = append(rules, rule)
		rulePats = append(rulePats, pat)
	}

	var n int
	if len(rules) > 0 {
		added, err := o.Rules(ctx, l.Spec, over.RuleTrack, rules, false, over.LocalOrigin(Version()))
		if err != nil {
			return err
		}
		for _, rule := range added {
			env.Printf("%s tracked in %s (rule, %s)\n", rule, l, plural(ruleMatches(l, rulePats), "file"))
		}
		n += len(added)
	}
	if len(named) == 0 {
		if n == 0 {
			return fmt.Errorf("%s: nothing new to track in %s", strings.Join(paths, " "), l)
		}
		return over.SaveStates(layers)
	}

	matched, err := walkArgs(env.Dir, named)
	if err != nil {
		return err
	}
	for _, path := range matched {
		rel, err := filepath.Rel(l.Root, path)
		if err != nil {
			return err
		}
		if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return fmt.Errorf("%s: not under %s, the root of %s", over.RelTo(env.Dir, path), l.Root, l)
		}
		rel = filepath.ToSlash(rel)
		if l.Excluded(path) {
			// over's own configuration and cache are never overlay
			// content, however the pattern reached them.
			env.Logf("%s: over's own files are never overlay content", path)
			continue
		}
		if l.State.Get(rel) != nil {
			continue
		}
		if err := over.Track(l, rel); err != nil {
			return err
		}
		env.Printf("%s tracked in %s\n", over.RelTo(env.Dir, path), l)
		n++
	}
	if n == 0 {
		return fmt.Errorf("%s: nothing new to track in %s", strings.Join(paths, " "), l)
	}
	return over.SaveStates(layers)
}

// ruleMatches counts the local files a set of new rules claims right
// now. A rule that matches nothing is usually a rule with a typo in it,
// and saying so is cheaper than waiting for the sync that does nothing.
func ruleMatches(l *over.Layer, pats []pathspec.Pattern) int {
	probe := &over.Layer{Root: l.Root, Exclude: l.Exclude, Track: pats, Ignore: l.Ignore}
	claimed, err := probe.ClaimedPaths()
	if err != nil {
		return 0
	}
	return len(claimed)
}

var untrackCmd = &Command{
	Name:  "untrack",
	Usage: "untrack <path>...",
	Short: "stop managing files, leaving them in place",
	Long: `Untrack forgets the named files. Neither the local copy nor
the layer's is touched: over simply stops comparing them, and a later
sync will treat the file as one it has never seen.
` + pathArgs,
	Run: runUntrack,
}

func runUntrack(ctx context.Context, env *Env, args []string) error {
	if len(args) == 0 {
		return Usagef("expected at least one path")
	}
	s, err := load(ctx, env, false, args)
	if err != nil {
		return err
	}
	var n int
	var claimed []*over.Change
	for i := range s.Changes {
		c := &s.Changes[i]
		if c.Base == nil {
			continue
		}
		over.Untrack(c.Layer, c.Path)
		env.Printf("%s untracked in %s\n", over.RelTo(env.Dir, c.Local), c.Layer)
		if c.Claimed {
			claimed = append(claimed, c)
		}
		n++
	}
	if n == 0 {
		return fmt.Errorf("%s: no tracked files match", strings.Join(args, " "))
	}
	// Untracking is a one-time edit; a rule is standing, and will claim
	// the file straight back. Say so once, however many were untracked.
	if len(claimed) > 0 {
		env.Printf("\n%s\n", stillClaimed(env, claimed))
	}
	return s.Save()
}

// stillClaimed explains that untracking will not stick against a rule,
// naming the layer where they all share one and the file where there is
// only one.
func stillClaimed(env *Env, claimed []*over.Change) string {
	layer := "<layer>"
	for i, c := range claimed {
		if i == 0 {
			layer = c.Layer.String()
		} else if layer != c.Layer.String() {
			layer = "<layer>"
			break
		}
	}
	if len(claimed) == 1 {
		return fmt.Sprintf("a rule still claims it; carve it out with 'over rule -ignore %s %s'",
			layer, over.RelTo(env.Dir, claimed[0].Local))
	}
	return fmt.Sprintf("%s still claimed by a rule; carve them out with 'over rule -ignore %s <pattern>'",
		plural(len(claimed), "file"), layer)
}

// findLayer returns the configured layer named by arg, or nil.
func findLayer(layers []*over.Layer, arg string) *over.Layer {
	s, err := spec.Parse(arg)
	if err != nil {
		return nil
	}
	for _, l := range layers {
		if l.Spec == s {
			return l
		}
	}
	return nil
}

// walkArgs expands path arguments against the file system, returning the
// regular files they name, sorted and deduplicated. Unlike the other
// commands, track works from what is on disk rather than from what over
// already knows, so it has to look.
func walkArgs(dir string, args []string) ([]string, error) {
	seen := map[string]bool{}
	for _, arg := range args {
		pat, err := pathspec.Parse(dir, arg)
		if err != nil {
			return nil, err
		}
		base := pat.Base()
		if base == "" {
			return nil, fmt.Errorf(`%s: too broad; name the files or directories to track`, arg)
		}
		info, err := os.Lstat(base)
		if err != nil {
			return nil, err
		}
		if !info.IsDir() {
			if !info.Mode().IsRegular() {
				return nil, fmt.Errorf("%s: not a regular file", arg)
			}
			if pat.Match(base) {
				seen[base] = true
			}
			continue
		}
		err = filepath.WalkDir(base, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if d.Name() == ".git" {
					return filepath.SkipDir
				}
				return nil
			}
			if d.Type().IsRegular() && pat.Match(path) {
				seen[path] = true
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	paths := make([]string, 0, len(seen))
	for p := range seen {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	if len(paths) == 0 {
		return nil, fmt.Errorf("%s: no files match", strings.Join(args, " "))
	}
	return paths, nil
}
