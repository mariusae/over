package cli

import (
	"context"

	"github.com/mariusae/over/internal/over"
)

func init() {
	Register(restoreCmd)
}

var restoreCmd = &Command{
	Name:  "restore",
	Usage: "restore <revision> <path>...",
	Short: "put back the version of a file from a revision",
	Long: `Restore writes the contents a file had at a revision of its
layer over the local copy. The revisions are the ones "over log" lists,
and may be abbreviated:

	over log .vimrc
	over restore bef94f8 .vimrc

The restored version becomes the one that has changed, so the next sync
publishes it to the layer. Restoring is a way to make an old version
current again, not a way to go back in time quietly; the layer's history
keeps both, and the commit that publishes the restore says it was one.

The revision must name a commit in the repository of the layer that owns
the file, and the file must exist there. To undo a local edit instead,
without reaching into the history, use "over reset".
` + pathArgs,
	Run: runRestore,
}

func runRestore(ctx context.Context, env *Env, args []string) error {
	if len(args) < 2 {
		return Usagef("expected a revision and at least one path")
	}
	rev, paths := args[0], args[1:]
	s, err := owned(ctx, env, paths)
	if err != nil {
		return err
	}
	for i := range s.Changes {
		c := &s.Changes[i]
		hash, err := over.Restore(ctx, c, rev)
		if err != nil {
			saveQuietly(env, s)
			return err
		}
		env.Printf("%s restored from %s in %s\n", over.RelTo(env.Dir, c.Local), short(hash), c.Layer)
	}
	return s.Save()
}
