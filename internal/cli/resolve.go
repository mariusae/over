package cli

import (
	"context"

	"github.com/mariusae/over/internal/over"
)

func init() {
	Register(resetCmd, ackCmd)
}

var resetCmd = &Command{
	Name:  "reset",
	Usage: "reset <path>...",
	Short: "overwrite local files with their layers' copies",
	Long: `Reset writes the layer's copy over the named local files,
discarding whatever is there. A file the layer no longer provides is
removed.

Reset is how a conflict is resolved in the layer's favor, but it does not
require one: it always just overwrites, which also makes it the way to
undo a local edit before it is synced.
` + pathArgs,
	Run: runReset,
}

func runReset(ctx context.Context, env *Env, args []string) error {
	s, err := owned(ctx, env, args)
	if err != nil {
		return err
	}
	for i := range s.Changes {
		c := &s.Changes[i]
		env.Printf("%s reset from %s\n", over.RelTo(env.Dir, c.Local), c.Layer)
	}
	if err := over.Reset(s.Changes); err != nil {
		saveQuietly(env, s)
		return err
	}
	return s.Save()
}

var ackCmd = &Command{
	Name:  "ack",
	Usage: "ack <path>...",
	Short: "accept the layer's version as seen, keeping local changes",
	Long: `Ack records the layer's copy of the named files as seen
without touching the local files. The local version is then the only one
that has changed since over last looked, so the next sync writes it to
the layer.

Ack is how a conflict is resolved in the local file's favor. Inspect the
difference with "over diff" first: acking discards nothing on disk, but
it does mean the next sync overwrites the layer's copy.
` + pathArgs,
	Run: runAck,
}

func runAck(ctx context.Context, env *Env, args []string) error {
	s, err := owned(ctx, env, args)
	if err != nil {
		return err
	}
	for i := range s.Changes {
		c := &s.Changes[i]
		env.Printf("%s acknowledged in %s\n", over.RelTo(env.Dir, c.Local), c.Layer)
	}
	over.Ack(s.Changes)
	return s.Save()
}

// owned loads the session and keeps only the changes of the layer that
// owns each file, which is what reset and ack act on: resolving a file
// means resolving it in the one layer that provides it.
func owned(ctx context.Context, env *Env, args []string) (*session, error) {
	if len(args) == 0 {
		return nil, Usagef("expected at least one path")
	}
	s, err := load(ctx, env, false, args)
	if err != nil {
		return nil, err
	}
	owned := s.Changes[:0:0]
	for _, c := range s.Changes {
		if c.Owner {
			owned = append(owned, c)
		}
	}
	s.Changes = owned
	return s, nil
}

// saveQuietly writes the state back, logging rather than returning a
// failure; it is used on the error path, where the original error is the
// one worth reporting.
func saveQuietly(env *Env, s *session) {
	if err := s.Save(); err != nil {
		env.Logf("saving state: %v", err)
	}
}
