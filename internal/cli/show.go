package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/mariusae/over/internal/over"
	"github.com/mariusae/over/internal/pathspec"
	"github.com/mariusae/over/internal/state"
)

func init() {
	Register(showCmd)
}

var showCmd = &Command{
	Name:  "show",
	Usage: "show <path>...",
	Short: "report everything over knows about a path",
	Long: `Show reports everything over knows about the named paths: the
local file, what over recorded at the last sync, and every layer that
has something to say about it, highest precedence first. The layer that
owns the file is marked, and the layers beneath it are listed with what
they hold, so it is plain both where a file comes from and what it is
covering up.

For each layer, show also names the last commit to touch the file in
that layer, which is usually the answer to where a change came from.

A path that no layer provides is reported too, along with the reason:
it may be excluded, it may lie outside every layer's root, or it may
simply be a local file that has never been tracked.

Show reads from over's cache and does not fetch, so it describes the
layers as of the last sync. Run "over sync" first to see them as they
are now.
` + pathArgs,
	Run: runShow,
}

func runShow(ctx context.Context, env *Env, args []string) error {
	if len(args) == 0 {
		return Usagef("expected at least one path")
	}
	s, err := loadAll(ctx, env, false, args)
	if err != nil {
		return err
	}
	exclusions, err := s.Over.Exclusions()
	if err != nil {
		return err
	}

	// Everything over knows about, keyed by where it lives on disk.
	match := pathspec.Patterns(s.Patterns)
	byPath := map[string][]*over.Change{}
	for i := range s.All {
		c := &s.All[i]
		if match.Match(c.Local) {
			byPath[c.Local] = append(byPath[c.Local], c)
		}
	}
	// A path argument naming one place is reported even when over knows
	// nothing about it: that it knows nothing is the answer.
	for _, p := range s.Patterns {
		if lit, ok := p.Path(); ok {
			if _, known := byPath[lit]; !known {
				byPath[lit] = nil
			}
		}
	}
	if len(byPath) == 0 {
		return fmt.Errorf("%s: nothing matches", strings.Join(args, " "))
	}
	paths := make([]string, 0, len(byPath))
	for p := range byPath {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	for i, p := range paths {
		if i > 0 {
			env.Printf("\n")
		}
		if err := showPath(ctx, env, s, exclusions, p, byPath[p]); err != nil {
			return err
		}
	}
	return nil
}

// showPath writes the report for one path.
func showPath(ctx context.Context, env *Env, s *session, exclusions []pathspec.Pattern, local string, changes []*over.Change) error {
	// Highest precedence first: the layer a file comes from is the one
	// the reader wants at the top.
	sort.Slice(changes, func(i, j int) bool {
		return changes[i].Layer.Index > changes[j].Layer.Index
	})

	env.Printf("%s\n", over.RelTo(env.Dir, local))

	// One writer for the whole block, so that the file's own fields and
	// every layer's line up in a single column.
	tw := tabwriter.NewWriter(env.Stdout, 0, 4, 2, ' ', 0)
	defer tw.Flush()
	fmt.Fprintf(tw, "    path\t%s\n", local)
	fmt.Fprintf(tw, "    local\t%s\n", describeLocal(local))

	if len(changes) == 0 {
		fmt.Fprintf(tw, "    status\tnot managed\n")
		for _, why := range whyUnmanaged(env, s, exclusions, local) {
			fmt.Fprintf(tw, "    reason\t%s\n", why)
		}
		return nil
	}

	// The owner's verdict is the file's; the rest are shadowed by it.
	status, owner := changes[0].Status, ""
	for _, c := range changes {
		if c.Owner {
			status, owner = c.Status, c.Layer.String()
		}
	}
	fmt.Fprintf(tw, "    status\t%s\n", status)
	if owner != "" {
		fmt.Fprintf(tw, "    owner\t%s\n", owner)
	}
	for _, c := range changes {
		if err := showLayer(ctx, tw, c); err != nil {
			return err
		}
	}
	return nil
}

// showLayer writes what one layer has to say about a path.
func showLayer(ctx context.Context, tw io.Writer, c *over.Change) error {
	role := "shadowed"
	if c.Owner {
		role = "owner"
	}
	fmt.Fprintf(tw, "    layer\t%s (%s)\n", c.Layer, role)
	fmt.Fprintf(tw, "      root\t%s\n", c.Layer.Root)
	repoFile := path.Join(c.Layer.Spec.Name, c.Path)
	fmt.Fprintf(tw, "      file\t%s\n", repoFile)

	switch {
	case c.RemotePresent():
		fmt.Fprintf(tw, "      content\t%s  mode %s\n", short(c.Remote.Hash), mode(c.Remote.Exec))
	case c.RemoteTombstone:
		fmt.Fprintf(tw, "      content\t(deleted)\n")
	default:
		fmt.Fprintf(tw, "      content\t(absent)\n")
	}
	if when, ok := c.Layer.Tombstones.At(c.Path); ok {
		fmt.Fprintf(tw, "      tombstone\t%s\n", stamp(when))
	}
	switch base := c.Base; {
	case base == nil:
		fmt.Fprintf(tw, "      synced\t(never)\n")
	case base.Deleted:
		fmt.Fprintf(tw, "      synced\t(absent)  %s\n", stamp(base.Synced))
	default:
		fmt.Fprintf(tw, "      synced\t%s  mode %s  %s\n", short(base.Hash), mode(base.Exec), stamp(base.Synced))
	}

	commit, err := c.Layer.Repo.LastCommit(ctx, repoFile)
	switch {
	case err != nil:
		fmt.Fprintf(tw, "      commit\t(unavailable: %v)\n", err)
	case commit == nil:
		fmt.Fprintf(tw, "      commit\t(none)\n")
	default:
		fmt.Fprintf(tw, "      commit\t%s  %s  %s\n", commit.Short, stamp(commit.Date), commit.Author)
		fmt.Fprintf(tw, "      \t%s\n", commit.Subject)
	}
	return nil
}

// whyUnmanaged explains why over has nothing to say about a path.
func whyUnmanaged(env *Env, s *session, exclusions []pathspec.Pattern, local string) []string {
	var why []string
	// Exclusions are parsed in configuration order, so the pattern that
	// matched can be named as the user wrote it.
	for i, pat := range exclusions {
		if pat.Match(local) {
			why = append(why, fmt.Sprintf("excluded by %s", s.Over.Config.Exclude[i]))
		}
	}
	var roots []string
	for _, l := range s.Layers {
		if under(local, l.Root) {
			roots = append(roots, l.String())
		}
	}
	switch {
	case len(roots) == 0:
		why = append(why, "outside the root of every configured layer")
	case len(why) == 0:
		why = append(why, fmt.Sprintf("no layer provides it; track it with 'over track %s %s'",
			roots[len(roots)-1], over.RelTo(env.Dir, local)))
	}
	return why
}

// under reports whether a path lies beneath a root.
func under(path, root string) bool {
	return path == root || strings.HasPrefix(path, root+string(os.PathSeparator))
}

// describeLocal reports what is at a path on the local file system.
func describeLocal(local string) string {
	info, err := os.Lstat(local)
	switch {
	case os.IsNotExist(err):
		return "(absent)"
	case err != nil:
		return fmt.Sprintf("(%v)", err)
	case info.Mode()&os.ModeSymlink != 0:
		target, err := os.Readlink(local)
		if err != nil {
			return "(symbolic link)"
		}
		return fmt.Sprintf("(symbolic link to %s)", target)
	case !info.Mode().IsRegular():
		return fmt.Sprintf("(%s)", info.Mode().Type())
	}
	hash, exec, err := state.HashFile(local)
	if err != nil {
		return fmt.Sprintf("(%v)", err)
	}
	return fmt.Sprintf("%s  mode %s  %d bytes  %s", short(hash), mode(exec), info.Size(), stamp(info.ModTime()))
}

// short abbreviates a hash to something an eye can compare.
func short(hash string) string {
	if len(hash) > 12 {
		return hash[:12]
	}
	if hash == "" {
		return "(none)"
	}
	return hash
}

// mode renders the only file mode over tracks.
func mode(exec bool) string {
	if exec {
		return "755"
	}
	return "644"
}

// stamp formats a time in the local zone, or reports that there is none.
func stamp(t time.Time) string {
	if t.IsZero() {
		return "(never)"
	}
	return t.Local().Format("2006-01-02 15:04:05")
}
