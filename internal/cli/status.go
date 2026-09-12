package cli

import (
	"context"
	"fmt"
	"text/tabwriter"

	"github.com/mariusae/over/internal/over"
)

func init() {
	Register(statusCmd)
}

var statusCmd = &Command{
	Name:  "status",
	Usage: "status [path...]",
	Short: "show the layers and what sync would do",
	Long: `Status lists the configured layers in order, lowest
precedence first, and then every file that a sync would act on: the
conflicts, the files whose layer has moved ahead, and the local changes
waiting to be written back.

Status works from the layers already in over's cache; it does not fetch.
Run "over sync -n" to see the same report against freshly fetched
layers.
` + pathArgs,
	Run: runStatus,
}

func runStatus(ctx context.Context, env *Env, args []string) error {
	s, err := load(ctx, env, false, args)
	if err != nil {
		return err
	}
	tw := tabwriter.NewWriter(env.Stdout, 0, 4, 2, ' ', 0)
	for _, l := range s.Layers {
		synced := "never synced"
		if !l.State.Synced.IsZero() {
			synced = "synced " + l.State.Synced.Local().Format("2006-01-02 15:04:05")
		}
		env.Logf("%s -> %s", l, l.Repo.URL())
		fmt.Fprintf(tw, "%s\t%s\t%s\n", l, l.Root, synced)
	}
	tw.Flush()

	var any bool
	for i := range s.Changes {
		line, ok := describe(env, &s.Changes[i])
		if !ok {
			continue
		}
		if !any {
			env.Printf("\n")
			any = true
		}
		env.Printf("%s\n", line)
	}
	env.Printf("\n%s\n", summarize(over.Count(s.Changes)))
	return nil
}
