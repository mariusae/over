package over

import (
	"testing"

	"github.com/mariusae/over/internal/state"
)

// TestClassify walks the decision table over uses for one file: what to
// do given the layer's copy, the local copy, and the copy recorded at
// the last sync.
func TestClassify(t *testing.T) {
	const (
		x = "hash-x"
		y = "hash-y"
		z = "hash-z"
	)
	tracked := func(hash string) *state.File { return &state.File{Hash: hash} }
	deleted := &state.File{Deleted: true}

	for _, test := range []struct {
		name          string
		base          *state.File
		remote, local string
		remoteExec    bool
		localExec     bool
		baseExec      bool
		want          Status
	}{
		// Never synced: over takes the layer's copy only when there is
		// nothing local to lose.
		{name: "new from layer", base: nil, remote: x, want: Pull},
		{name: "new, already identical", base: nil, remote: x, local: x, want: Adopt},
		{name: "new, local differs", base: nil, remote: x, local: y, want: Conflict},
		{name: "untracked local file", base: nil, local: x, want: Unchanged},

		// The layer had nothing at the last sync: either it deleted the
		// file, or "over track" staged a local one.
		{name: "tracked, first write", base: deleted, local: x, want: Push},
		{name: "deleted, recreated in layer", base: deleted, remote: x, want: Pull},
		{name: "deleted, recreated identically", base: deleted, remote: x, local: x, want: Adopt},
		{name: "deleted, recreated differently", base: deleted, remote: x, local: y, want: Conflict},
		{name: "deleted on both sides", base: deleted, want: Unchanged},

		// Synced before.
		{name: "unchanged", base: tracked(x), remote: x, local: x, want: Unchanged},
		{name: "layer moved ahead", base: tracked(x), remote: y, local: x, want: Pull},
		{name: "layer deleted it", base: tracked(x), local: x, want: PullDelete},
		{name: "local edit", base: tracked(x), remote: x, local: y, want: Push},
		{name: "local delete", base: tracked(x), remote: x, want: PushDelete},
		{name: "both edited", base: tracked(x), remote: y, local: z, want: Conflict},
		{name: "both edited alike", base: tracked(x), remote: y, local: y, want: Adopt},
		{name: "both deleted", base: tracked(x), want: Adopt},

		// The executable bit is content too.
		{name: "local chmod +x", base: tracked(x), remote: x, local: x, localExec: true, want: Push},
		{name: "layer chmod +x", base: tracked(x), remote: x, remoteExec: true, local: x, want: Pull},
	} {
		t.Run(test.name, func(t *testing.T) {
			base := test.base
			if base != nil {
				b := *base
				b.Exec = test.baseExec
				base = &b
			}
			c := Change{
				Base:            base,
				Remote:          Content{Hash: test.remote, Exec: test.remoteExec},
				RemoteTombstone: test.remote == "",
				LocalHash:       test.local,
				LocalExec:       test.localExec,
			}
			if got := classify(&c); got != test.want {
				t.Errorf("got %s, want %s", got, test.want)
			}
		})
	}
}

// TestShadow checks that the last layer providing a file owns it, and
// that the others are marked shadowed rather than applied.
func TestShadow(t *testing.T) {
	lo := &Layer{Index: 0, Root: "/home/u"}
	hi := &Layer{Index: 1, Root: "/home/u"}
	changes := []Change{
		{Layer: lo, Path: ".emacs", Local: "/home/u/.emacs", Remote: Content{Hash: "a"}, Status: Pull},
		{Layer: hi, Path: ".emacs", Local: "/home/u/.emacs", Remote: Content{Hash: "b"}, Status: Pull},
		{Layer: lo, Path: ".zshrc", Local: "/home/u/.zshrc", Remote: Content{Hash: "c"}, Status: Pull},
	}
	shadow(changes)
	if changes[0].Status != Shadowed || changes[0].Owner {
		t.Errorf("lower layer: got %s owner=%v, want shadowed", changes[0].Status, changes[0].Owner)
	}
	if changes[1].Status != Pull || !changes[1].Owner {
		t.Errorf("higher layer: got %s owner=%v, want pull", changes[1].Status, changes[1].Owner)
	}
	if changes[2].Status != Pull || !changes[2].Owner {
		t.Errorf("sole layer: got %s owner=%v, want pull", changes[2].Status, changes[2].Owner)
	}
}

// TestShadowRetiring checks that when no layer provides a file any
// longer, the last layer that knows of it still gets to retire it.
func TestShadowRetiring(t *testing.T) {
	lo := &Layer{Index: 0, Root: "/home/u"}
	hi := &Layer{Index: 1, Root: "/home/u"}
	changes := []Change{
		{Layer: lo, Path: ".emacs", Local: "/home/u/.emacs", Base: &state.File{Hash: "a"}, Status: PullDelete},
		{Layer: hi, Path: ".emacs", Local: "/home/u/.emacs", Base: &state.File{Hash: "a"}, Status: PullDelete},
	}
	shadow(changes)
	if changes[1].Status != PullDelete || !changes[1].Owner {
		t.Errorf("got %s owner=%v, want the last layer to own the deletion", changes[1].Status, changes[1].Owner)
	}
}
