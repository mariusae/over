package cli

import (
	"context"
	"os"

	"github.com/mariusae/over/internal/diff"
	"github.com/mariusae/over/internal/over"
)

func init() {
	Register(diffCmd)
}

var diffCmd = &Command{
	Name:  "diff",
	Usage: "diff [path...]",
	Short: "show how local files differ from their layers",
	Long: `Diff writes a unified diff of every file whose local
contents differ from the copy in the layer that owns it.

The diff runs the way the sync would: what over would replace on the
left, what it would put there on the right. For a file the layer has
moved ahead of, that is the local file against the layer's copy; for a
local change waiting to be written back, it is the other way about. The
lines marked "+" are the ones that would end up in the file over is
about to write, whichever file that is.

A conflict is shown as the push it is not yet allowed to be: the layer's
copy on the left, the local file competing with it on the right. "over
reset" takes the left, "over ack" takes the right.

With no arguments diff covers every tracked file; conflicts are the usual
reason to run it.
` + pathArgs,
	Run: runDiff,
}

func runDiff(ctx context.Context, env *Env, args []string) error {
	s, err := load(ctx, env, false, args)
	if err != nil {
		return err
	}
	for i := range s.Changes {
		c := &s.Changes[i]
		if !c.Owner || c.Status == over.Unchanged {
			continue
		}
		if c.LocalHash == c.Remote.Hash && !c.LocalIrregular {
			continue
		}
		if c.LocalIrregular {
			env.Printf("%s is not a regular file; over will not replace it\n",
				over.RelTo(env.Dir, c.Local))
			continue
		}
		local, err := readOrEmpty(c.Local)
		if err != nil {
			return err
		}
		remote, err := readOrEmpty(c.Layer.RepoPath(c.Path))
		if err != nil {
			return err
		}
		// The diff runs the way the sync would: what over would
		// replace on the left, what it would put there on the right,
		// so that the lines marked "+" are the ones that would end up
		// in the file over is about to write.
		path := over.RelTo(env.Dir, c.Local)
		var from, to string
		var before, after []byte
		switch c.Status {
		case over.Pull, over.PullDelete:
			from, before = path+" (local)", local
			to, after = path+" ("+c.Layer.String()+")", remote
		default:
			// A push, or a conflict, which is a push that over will
			// not make until it is told which side wins.
			from, before = path+" ("+c.Layer.String()+")", remote
			to, after = path+" (local)", local
		}
		text := diff.Unified(from, to, before, after)
		if text == "" {
			continue
		}
		env.Printf("%s", text)
	}
	return nil
}

// readOrEmpty reads a file, treating a missing one as empty so that an
// added or deleted file diffs against nothing.
func readOrEmpty(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	return data, err
}
