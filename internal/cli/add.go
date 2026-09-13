package cli

import (
	"context"
	"flag"
)

func init() {
	Register(addCmd, rmCmd, rootCmd)
}

var (
	addFlagBefore string
	addFlagRoot   string
)

var addCmd = &Command{
	Name:  "add",
	Usage: "add [-before layer] [-root dir] <layer>...",
	Short: "add layers to the configuration",
	Long: `Add appends layers to the configuration, so that the next
sync materializes them. A layer is written as

	[host/]owner/repo:layer

for example "mariusae/config:editors", the "editors" directory of the
mariusae/config repository on GitHub. Naming a repository with no layer
adds every layer it provides.

A set of layers is named with a doubled colon:

	over add mariusae/config::mac

which adds the set's members, in the order the set gives them. Sets are
declared in their repository's config.yaml and may name layers in other
repositories, so what a set comes to is not always obvious from the
name: add prints each layer it added. See "over help set".

Layers are ordered, and the last layer providing a file wins. Add puts
new layers last, that is, at the highest precedence. The -before flag
inserts them ahead of an already configured layer instead.

By default a layer is materialized under the root its repository
declares, or $HOME if it declares none. The -root flag overrides that;
it is expanded for environment variables, so -root '$HOME/work' follows
the user it runs as.`,
	Flags: func(fs *flag.FlagSet) {
		fs.StringVar(&addFlagBefore, "before", "", "insert ahead of the configured `layer`")
		fs.StringVar(&addFlagRoot, "root", "", "materialize the layers under `dir` instead of their default root")
	},
	Run: runAdd,
}

func runAdd(ctx context.Context, env *Env, args []string) error {
	if len(args) == 0 {
		return Usagef("expected at least one layer")
	}
	o, err := openOver(env)
	if err != nil {
		return err
	}
	added, err := o.Add(ctx, args, addFlagBefore, addFlagRoot)
	if err != nil {
		return err
	}
	for _, s := range added {
		env.Printf("added %s\n", s)
	}
	if len(added) == 0 {
		env.Printf("already configured\n")
	}
	return nil
}

var rmCmd = &Command{
	Name:  "rm",
	Usage: "rm <layer>...",
	Short: "remove layers from the configuration",
	Long: `Rm drops layers from the configuration. It touches neither
the local files nor the layer's contents: over simply stops managing
them. The layer's sync state is kept, so that adding it back picks up
where it left off.

A set or a repository removes the layers it stands for, as adding one
adds them, so that a machine comes apart the way it went together.`,
	Run: runRm,
}

func runRm(ctx context.Context, env *Env, args []string) error {
	if len(args) == 0 {
		return Usagef("expected at least one layer")
	}
	o, err := openOver(env)
	if err != nil {
		return err
	}
	removed, err := o.Remove(ctx, args)
	if err != nil {
		return err
	}
	for _, s := range removed {
		env.Printf("removed %s\n", s)
	}
	return nil
}

var rootCmd = &Command{
	Name:  "root",
	Usage: "root <layer> [dir]",
	Short: "show or set the root a layer is materialized under",
	Long: `Root prints the root configured for a layer, or sets it
when given a directory. The directory is expanded for environment
variables at sync time, so

	over root mariusae/config:editors '$HOME'

follows the user over runs as. An empty argument clears the override and
restores the root the layer's repository declares.`,
	Run: runRoot,
}

func runRoot(ctx context.Context, env *Env, args []string) error {
	o, err := openOver(env)
	if err != nil {
		return err
	}
	switch len(args) {
	case 1:
		entry, err := o.Entry(args[0])
		if err != nil {
			return err
		}
		if entry.Root == "" {
			env.Printf("(default)\n")
			return nil
		}
		env.Printf("%s\n", entry.Root)
		return nil
	case 2:
		if err := o.SetRoot(args[0], args[1]); err != nil {
			return err
		}
		return nil
	default:
		return Usagef("expected a layer and an optional directory")
	}
}
