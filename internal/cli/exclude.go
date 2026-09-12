package cli

import (
	"context"
	"flag"
	"fmt"
	"text/tabwriter"

	"github.com/mariusae/over/internal/over"
)

func init() {
	Register(excludeCmd)
}

var excludeFlagRm bool

var excludeCmd = &Command{
	Name:  "exclude",
	Usage: "exclude [-rm] [dir...]",
	Short: "show or change the paths no layer may manage",
	Long: `Exclude prints the paths that no layer manages, or adds to
them when given paths. The -rm flag removes them instead.

An exclusion is a standing rule, not a one-time edit: over skips these
paths on every sync, in every layer, whatever the layer's root. That is
what separates it from "over untrack", which forgets a file once and
leaves the next sync free to pick it up again.

Exclusions are written as path patterns, in the same form as the path
arguments the other commands take, and are expanded for environment
variables at sync time:

	over exclude '$HOME/.local/state/...'

A new configuration is seeded with over's own home and cache
directories. They hold the state sync writes as it runs, so a layer
carrying them would dirty its own input on every sync and never settle;
removing them is a way to make over unstable, not a way to manage more
files.

Excluding a path over already tracks does not take the file out of its
layer; over simply stops looking at it. Use "over untrack" to forget it
as well.`,
	Flags: func(fs *flag.FlagSet) {
		fs.BoolVar(&excludeFlagRm, "rm", false, "remove the paths from the exclusion list")
	},
	Run: runExclude,
}

func runExclude(ctx context.Context, env *Env, args []string) error {
	o, err := openOver(env)
	if err != nil {
		return err
	}
	switch {
	case len(args) == 0 && excludeFlagRm:
		return Usagef("expected at least one path")

	case len(args) == 0:
		return listExclusions(env, o)

	case excludeFlagRm:
		removed, err := o.RemoveExclude(args)
		if err != nil {
			return err
		}
		for _, path := range removed {
			env.Printf("no longer excluded %s\n", path)
		}
		return nil

	default:
		added, err := o.AddExclude(args)
		if err != nil {
			return err
		}
		for _, path := range added {
			env.Printf("excluded %s\n", path)
		}
		if len(added) == 0 {
			env.Printf("already excluded\n")
		}
		return nil
	}
}

// listExclusions prints the exclusion list, each entry as it is written
// and, where they differ, the path it expands to.
func listExclusions(env *Env, o *over.Over) error {
	if len(o.Config.Exclude) == 0 {
		env.Printf("no exclusions\n")
		return nil
	}
	tw := tabwriter.NewWriter(env.Stdout, 0, 4, 2, ' ', 0)
	defer tw.Flush()
	for _, raw := range o.Config.Exclude {
		path, err := over.ExpandExclusion(raw)
		switch {
		case err != nil:
			fmt.Fprintf(tw, "%s\t%v\n", raw, err)
		case path == raw:
			fmt.Fprintf(tw, "%s\t\n", raw)
		default:
			fmt.Fprintf(tw, "%s\t%s\n", raw, path)
		}
	}
	return nil
}
