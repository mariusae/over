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
contents differ from the copy in the layer that owns it. The local file
is the left side, so the lines marked "+" are what the layer holds and a
reset would install.

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
		path := over.RelTo(env.Dir, c.Local)
		text := diff.Unified(path+" (local)", path+" ("+c.Layer.String()+")", local, remote)
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
