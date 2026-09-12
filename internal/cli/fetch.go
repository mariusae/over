package cli

import (
	"context"
	"fmt"
	"text/tabwriter"

	"github.com/mariusae/over/internal/over"
)

func init() {
	Register(fetchCmd)
}

var fetchCmd = &Command{
	Name:  "fetch",
	Usage: "fetch",
	Short: "update over's copy of the layers without syncing",
	Long: `Fetch brings over's checkout of every configured layer up to
date with its repository. It writes no local file and commits nothing:
it only makes over's idea of the layers current.

That is what makes "over status" worth reading. Status reports from the
cache rather than the network, so on its own it describes the layers as
they were at the last sync. Fetch first and status answers what a sync
would do now:

	over fetch
	over status

"over sync" fetches on its own, so this is for looking before leaping.`,
	Run: runFetch,
}

func runFetch(ctx context.Context, env *Env, args []string) error {
	if len(args) > 0 {
		return Usagef("too many arguments")
	}
	o, err := openOver(env)
	if err != nil {
		return err
	}
	if len(o.Config.Layers) == 0 {
		return over.ErrNoLayers
	}
	fetched, err := o.Fetch(ctx)
	if err != nil {
		return err
	}
	var changed int
	tw := tabwriter.NewWriter(env.Stdout, 0, 4, 2, ' ', 0)
	for _, f := range fetched {
		switch {
		case f.Cloned:
			fmt.Fprintf(tw, "%s\tcloned at %s\n", f.Repo, short(f.After))
			changed++
		case f.Before != f.After:
			fmt.Fprintf(tw, "%s\t%s..%s\n", f.Repo, short(f.Before), short(f.After))
			changed++
		default:
			fmt.Fprintf(tw, "%s\tup to date\n", f.Repo)
		}
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	if changed > 0 {
		env.Printf("%s updated; run 'over status' to see what a sync would do\n",
			plural(changed, "repository"))
	}
	return nil
}
