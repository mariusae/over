package cli

import (
	"context"
	"flag"

	"github.com/mariusae/over/internal/over"
)

func init() {
	Register(initCmd)
}

var (
	initFlagRoot  string
	initFlagNoAdd bool
)

var initCmd = &Command{
	Name:  "init",
	Usage: "init [-root dir] [-no-add] <layer>...",
	Short: "create a new layer in its repository",
	Long: `Init creates a layer: it declares the layer in its
repository's config.yaml, commits, and pushes. A layer needs nothing
else to exist -- its directory appears the first time a file is written
to it -- so this is the whole of it:

	over init mariusae/config:editors
	over track mariusae/config:editors .vimrc .emacs
	over sync

The repository must already exist, but it may be empty. A repository
just created and never pushed to has no commits and no branch at all,
and init makes the first commit in it.

The -root flag sets the root the layer declares for itself, which is
where it is materialized on every machine that adds it; it defaults to
$HOME and is expanded for environment variables at sync time. This is
the layer's own root, not a local override: use "over root" for that.

The new layer is added to the configuration as well, at the highest
precedence, as "over add" would. Pass -no-add to create it without
adding it here.

Init edits the repository's config.yaml in place, so comments and
anything else in the file are kept.`,
	Flags: func(fs *flag.FlagSet) {
		fs.StringVar(&initFlagRoot, "root", "", "declare `dir` as the layer's root instead of $HOME")
		fs.BoolVar(&initFlagNoAdd, "no-add", false, "create the layer without adding it to this configuration")
	},
	Run: runInit,
}

func runInit(ctx context.Context, env *Env, args []string) error {
	if len(args) == 0 {
		return Usagef("expected at least one layer")
	}
	o, err := openOver(env)
	if err != nil {
		return err
	}
	created, err := o.Init(ctx, args, initFlagRoot, over.LocalOrigin(Version()))
	if err != nil {
		return err
	}
	specs := make([]string, 0, len(created))
	for _, c := range created {
		env.Printf("created %s (root %s)\n", c.Spec, c.Root)
		specs = append(specs, c.Spec.String())
	}
	if initFlagNoAdd {
		return nil
	}
	added, err := o.Add(ctx, specs, "", "")
	if err != nil {
		return err
	}
	for _, s := range added {
		env.Printf("added %s\n", s)
	}
	if len(created) > 0 {
		env.Printf("track files into it with 'over track %s <path>...'\n", created[0].Spec)
	}
	return nil
}
