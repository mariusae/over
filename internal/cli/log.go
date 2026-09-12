package cli

import (
	"context"
	"flag"
	"fmt"
	"sort"
	"text/tabwriter"

	"github.com/mariusae/over/internal/gitrepo"
	"github.com/mariusae/over/internal/over"
)

func init() {
	Register(logCmd)
}

var logFlagN int

var logCmd = &Command{
	Name:  "log",
	Usage: "log [-n count] <path>...",
	Short: "show the history of a file in its layers",
	Long: `Log lists the versions of a file its layers have held, most
recent first: the revision, when it was written, where it came from,
what over was doing at the time, and the subject of the commit.

	.vimrc  (mariusae/config:editors, editors/.vimrc)
	    bef94f8dd211  2026-09-12 12:37:48  marius@mariusmac  write (ack)  editors: .vimrc
	    5fce44b2c1a0  2026-09-01 08:00:11  Marius Eriksen                 editors: initial

The origin is the machine and account the sync ran on, which over
records on every commit it makes, along with why the local file was
ahead: an ordinary edit says nothing, while "ack", "track", and
"restore" name the act that put it there. Commits made by hand carry
none of this, and log reports their git author instead.

Any of these revisions can be put back with "over restore".

Log reads from over's cache. Run "over fetch" first to see versions
published since the last sync.
` + pathArgs,
	Flags: func(fs *flag.FlagSet) {
		fs.IntVar(&logFlagN, "n", 20, "show at most `count` versions per layer; 0 means all")
	},
	Run: runLog,
}

func runLog(ctx context.Context, env *Env, args []string) error {
	if len(args) == 0 {
		return Usagef("expected at least one path")
	}
	s, err := load(ctx, env, false, args)
	if err != nil {
		return err
	}

	byPath := map[string][]*over.Change{}
	var paths []string
	for i := range s.Changes {
		c := &s.Changes[i]
		if _, ok := byPath[c.Local]; !ok {
			paths = append(paths, c.Local)
		}
		byPath[c.Local] = append(byPath[c.Local], c)
	}
	sort.Strings(paths)

	for i, local := range paths {
		if i > 0 {
			env.Printf("\n")
		}
		if err := logPath(ctx, env, local, byPath[local]); err != nil {
			return err
		}
	}
	return nil
}

// logPath writes the history of one file, a block per layer that holds
// any, highest precedence first.
func logPath(ctx context.Context, env *Env, local string, changes []*over.Change) error {
	sort.Slice(changes, func(i, j int) bool {
		return changes[i].Layer.Index > changes[j].Layer.Index
	})
	name := over.RelTo(env.Dir, local)

	var found bool
	for _, c := range changes {
		commits, err := c.Layer.Repo.Log(ctx, c.RepoFile(), logFlagN)
		if err != nil {
			return err
		}
		if len(commits) == 0 {
			continue
		}
		found = true
		env.Printf("%s  (%s, %s)\n", name, c.Layer, c.RepoFile())
		tw := tabwriter.NewWriter(env.Stdout, 0, 4, 2, ' ', 0)
		for _, commit := range commits {
			origin, action := describeCommit(commit, c.RepoFile())
			// The subject goes last, so that nothing is padded on its
			// account and a long one simply runs off to the right.
			fmt.Fprintf(tw, "    %s\t%s\t%s\t%s\t%s\n",
				short(commit.Hash), stamp(commit.Date), origin, action, commit.Subject)
		}
		if err := tw.Flush(); err != nil {
			return err
		}
	}
	if !found {
		env.Printf("%s  (no history in any layer)\n", name)
		return nil
	}
	env.Printf("\nrestore an earlier version with 'over restore <revision> %s'\n", name)
	return nil
}

// describeCommit renders where a commit came from and what it did to the
// file. over's own commits carry both; for anything else, git's author
// is the best that can be said.
func describeCommit(commit *gitrepo.Commit, file string) (origin, action string) {
	rec, ok := over.ParseCommit(commit.Body, file)
	if !ok {
		return commit.Author, ""
	}
	origin = rec.Origin.String()
	if origin == "@" {
		origin = commit.Author
	}
	action = rec.Action
	if rec.Reason != "" {
		action += " (" + rec.Reason + ")"
	}
	return origin, action
}
