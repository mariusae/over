package cli

import (
	"context"
	"flag"
	"fmt"
	"strings"

	"github.com/mariusae/over/internal/over"
	"github.com/mariusae/over/internal/spec"
)

func init() {
	Register(setCmd)
}

var setFlagRm bool

var setCmd = &Command{
	Name:  "set",
	Usage: "set [-rm] <set> [layer...]",
	Short: "show or change a set of layers",
	Long: `Set prints a set's members, or adds to them when given
layers. A set is named with a doubled colon, which is what distinguishes
it from a layer:

	over set mariusae/env::mac editors defaults mariusae/work:overrides
	over add mariusae/env::mac

A set is a name for a group of layers, and may be used wherever a layer
is expected: adding one adds its members, in order, so the order they are
written in is the precedence they get. That makes a set the thing to name
when bringing up a machine -- one argument standing for the arrangement a
machine should have.

A member is written relative to the repository the set is declared in: a
bare name is one of its layers, and "::name" is another of its sets. A
whole specification names a layer or a set in another repository, so one
set can gather an arrangement that no single repository holds. Sets
expand recursively, and a set that reaches itself is an error rather than
a hang.

The -rm flag drops the named layers from the set. With no layers it
removes the set itself; either way the layers are untouched, since a set
is a name for a group and not the group.

A set belongs to the repository that declares it, so it is kept in
config.yaml there and changing one changes it for everybody: set commits
and pushes, as "over init" does. It edits config.yaml in place, so
comments and anything else in the file are kept.

Nothing about a set is guessed. Layers and sets are separate namespaces,
and a repository may hold a layer and a set of the same name without
either shadowing the other.`,
	Flags: func(fs *flag.FlagSet) {
		fs.BoolVar(&setFlagRm, "rm", false, "remove the layers from the set, or the set itself when given none")
	},
	Run: runSet,
}

func runSet(ctx context.Context, env *Env, args []string) error {
	if len(args) == 0 {
		return Usagef("expected a set")
	}
	setArg, members := args[0], args[1:]
	ref, err := spec.ParseRef(setArg)
	if err != nil {
		return Usagef("%v; the first argument names the set", err)
	}
	if !ref.Set {
		name := ref.Spec.Name
		if name == "" {
			name = "<set>"
		}
		return Usagef("%s names a layer; a set is written with a doubled colon, as %s",
			setArg, spec.SetRef(ref.Spec, name))
	}
	o, err := openOver(env)
	if err != nil {
		return err
	}
	origin := over.LocalOrigin(Version())

	switch {
	case len(members) == 0 && setFlagRm:
		removed, err := o.DeleteSet(ctx, ref, origin)
		if err != nil {
			return err
		}
		if removed {
			env.Printf("removed %s\n", ref)
		}
		return nil
	case len(members) == 0:
		return listMembers(ctx, env, o, ref)
	}

	changed, err := o.EditSet(ctx, ref, members, setFlagRm, origin)
	if err != nil {
		return err
	}
	if len(changed) == 0 {
		if setFlagRm {
			return fmt.Errorf("%s: not a member of %s; run 'over set %s' for the list",
				members[0], ref, ref)
		}
		env.Printf("already in %s\n", ref)
		return nil
	}
	for _, member := range changed {
		if setFlagRm {
			env.Printf("%s dropped from %s\n", member, ref)
		} else {
			env.Printf("%s added to %s\n", member, ref)
		}
	}
	return nil
}

// listMembers prints a set's members, and the layers they come to.
func listMembers(ctx context.Context, env *Env, o *over.Over, ref spec.Ref) error {
	members, err := o.Members(ctx, ref)
	if err != nil {
		return err
	}
	env.Printf("%s  %s\n", ref, strings.Join(members, ", "))

	// A set may name sets, and sets in other repositories, so what it
	// comes to is not always what it says. Print the layers too, which
	// is what adding it would do.
	specs, err := o.ResolveSpecs(ctx, []string{ref.String()})
	if err != nil {
		return err
	}
	for _, s := range specs {
		env.Printf("    %s\n", s)
	}
	return nil
}
