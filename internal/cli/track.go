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

Track is the counterpart of sync's other direction: sync discovers new
files in a layer by itself, but a new local file is only over's business
once it has been tracked.
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

	matched, err := walkArgs(env.Dir, paths)
	if err != nil {
		return err
	}
	var n int
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
	for i := range s.Changes {
		c := &s.Changes[i]
		if c.Base == nil {
			continue
		}
		over.Untrack(c.Layer, c.Path)
		env.Printf("%s untracked in %s\n", over.RelTo(env.Dir, c.Local), c.Layer)
		n++
	}
	if n == 0 {
		return fmt.Errorf("%s: no tracked files match", strings.Join(args, " "))
	}
	return s.Save()
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
