package cli

import (
	"context"
	"flag"
	"fmt"
	"strings"

	"github.com/mariusae/over/internal/over"
)

func init() {
	Register(syncCmd)
}

var syncFlagN bool

var syncCmd = &Command{
	Name:  "sync",
	Usage: "sync [-n] [path...]",
	Short: "synchronize layers with the local file system",
	Long: `Sync fetches each configured layer, writes the layer's copy
over every local file that is behind it, and commits every local file
that is ahead of it back to the layer. With path arguments, only the
named files are considered.

A file that has changed both locally and in its layer is a conflict, and
sync leaves both copies alone. So is a file that over has never synced
and that already exists locally with other contents: over will not
overwrite work it did not put there. Use "over diff" to see the
difference, then "over reset" to take the layer's copy or "over ack" to
keep the local one.

Sync reports one line per file:

	.emacs.conf from mariusae/config:editors    the layer's copy was written locally
	.vimrc to mariusae/config:editors           the local file was committed to the layer
	.oldrc deleted from mariusae/config:editors the layer deleted it, so sync did too
	.tmprc deleted in mariusae/config:editors   it is gone locally, so sync tombstoned it
	.zshrc conflicts                            both sides changed
` + pathArgs,
	Flags: func(fs *flag.FlagSet) {
		fs.BoolVar(&syncFlagN, "n", false, "print what sync would do, but do not do it")
	},
	Run: runSync,
}

func runSync(ctx context.Context, env *Env, args []string) error {
	s, err := load(ctx, env, true, args)
	if err != nil {
		return err
	}
	for i := range s.Changes {
		if s.Changes[i].Status == over.Shadowed {
			// Nothing is done about a shadowed file, and it stays
			// shadowed; reporting it every time is noise. It is
			// counted below, and "over status" names the files.
			continue
		}
		if line, ok := describe(env, &s.Changes[i]); ok {
			env.Printf("%s\n", line)
		}
	}
	counts := over.Count(s.Changes)
	if !syncFlagN {
		if err := over.Sync(ctx, s.Layers, s.Changes, over.LocalOrigin(Version())); err != nil {
			// Save whatever was applied before the failure, so that
			// over's state still describes the file system.
			if serr := s.Save(); serr != nil {
				env.Logf("saving state: %v", serr)
			}
			return err
		}
		if err := s.Save(); err != nil {
			return err
		}
	}
	env.Printf("%s\n", summarize(counts))
	if counts.Conflicts > 0 {
		return fmt.Errorf("%s unresolved; run 'over diff' to see them, then 'over reset' or 'over ack'",
			plural(counts.Conflicts, "conflict"))
	}
	return nil
}

// plural formats a count with its noun, pluralized for the handful of
// nouns over counts.
func plural(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, noun)
	}
	if strings.HasSuffix(noun, "y") {
		return fmt.Sprintf("%d %sies", n, strings.TrimSuffix(noun, "y"))
	}
	return fmt.Sprintf("%d %ss", n, noun)
}
