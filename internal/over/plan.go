package over

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mariusae/over/internal/state"
)

// A Status is what over has decided to do, or has decided it cannot do,
// about one file in one layer.
type Status int

const (
	// Unchanged means the local file, the layer, and the recorded
	// state all agree.
	Unchanged Status = iota

	// Pull means the layer's copy is newer: over will write it locally.
	Pull

	// PullDelete means the layer has deleted the file: over will
	// remove it locally.
	PullDelete

	// Push means the local file has changed: over will write it to the
	// layer.
	Push

	// PushDelete means the local file is gone: over will tombstone it
	// in the layer.
	PushDelete

	// Adopt means both sides changed to the same contents, or both
	// deleted the file. There is nothing to copy, only to record.
	Adopt

	// Conflict means both sides changed, differently, or that a file
	// over has never synced is already present locally with other
	// contents. over does neither side's work for it.
	Conflict

	// Shadowed means a later layer provides the same file, and so owns
	// it. The change is not applied.
	Shadowed
)

// String returns the status's name, as used in "over status".
func (s Status) String() string {
	switch s {
	case Unchanged:
		return "unchanged"
	case Pull:
		return "pull"
	case PullDelete:
		return "pull-delete"
	case Push:
		return "push"
	case PushDelete:
		return "push-delete"
	case Adopt:
		return "adopt"
	case Conflict:
		return "conflict"
	case Shadowed:
		return "shadowed"
	}
	return "unknown"
}

// A Change is what over has to say about one file in one layer.
type Change struct {
	// Layer is the layer the file belongs to.
	Layer *Layer

	// Path is the file's path relative to the layer's root.
	Path string

	// Local is the file's absolute path on the local file system.
	Local string

	// Status is over's decision.
	Status Status

	// Remote is the layer's copy of the file, if it has one.
	Remote Content

	// RemoteTombstone reports that the layer has explicitly deleted the
	// file.
	RemoteTombstone bool

	// LocalHash is the SHA-256 of the local file, empty if it is
	// absent.
	LocalHash string

	// LocalExec reports whether the local file is executable.
	LocalExec bool

	// LocalIrregular reports that something is at the local path that
	// is not a regular file: a directory, a symbolic link, a device.
	// over does not manage such things, and will not replace one.
	LocalIrregular bool

	// Base is the recorded state from the last sync, nil if the file
	// has never been synced.
	Base *state.File

	// Claimed reports that the layer's tracking rules claim the file,
	// so that a local file the layer does not yet hold is written to
	// it without anyone naming it. See [Layer.Claims].
	Claimed bool

	// Owner reports whether this layer is the one that owns the file:
	// the last layer providing it. Only the owner's change is applied;
	// the others are shadowed.
	Owner bool
}

// Tracked reports whether the file is one over is looking after in this
// layer: the layer holds it, claims it, or recorded it at the last sync.
// A path the layer knows only as a tombstone, or a local file that is
// nobody's, is not.
func (c *Change) Tracked() bool {
	return c.RemotePresent() || c.Claimed || (c.Base != nil && !c.Base.Deleted)
}

// RepoFile returns the file's path within its layer's repository, which
// is how git names it.
func (c *Change) RepoFile() string { return c.Layer.Spec.Name + "/" + c.Path }

// RemotePresent reports whether the layer has contents for the file.
func (c *Change) RemotePresent() bool { return c.Remote.Hash != "" }

// LocalPresent reports whether the file exists locally.
func (c *Change) LocalPresent() bool { return c.LocalHash != "" }

// Plan compares every layer against the local file system and the
// recorded state, and returns one change per file per layer that knows
// about it. The result is sorted by local path, then by layer order.
func Plan(layers []*Layer) ([]Change, error) {
	var changes []Change
	for _, l := range layers {
		remote, err := l.Scan()
		if err != nil {
			return nil, err
		}
		claimed, err := l.ClaimedPaths()
		if err != nil {
			return nil, err
		}
		paths := map[string]bool{}
		for _, p := range claimed {
			paths[p] = true
		}
		for p := range remote {
			paths[p] = true
		}
		for p := range l.Tombstones.Tombstones {
			paths[p] = true
		}
		for _, p := range l.State.Paths() {
			paths[p] = true
		}
		for p := range paths {
			if l.Excluded(l.LocalPath(p)) {
				// over's own configuration and cache are never
				// overlay content.
				continue
			}
			c := Change{
				Layer:           l,
				Path:            p,
				Local:           l.LocalPath(p),
				Remote:          remote[p],
				RemoteTombstone: l.Tombstones.Has(p),
				Base:            l.State.Get(p),
			}
			c.Claimed = l.Claims(c.Local)
			info, err := os.Lstat(c.Local)
			switch {
			case os.IsNotExist(err):
				// Nothing local.
			case err != nil:
				return nil, err
			case !info.Mode().IsRegular():
				c.LocalIrregular = true
			default:
				hash, exec, err := state.HashFile(c.Local)
				if err != nil {
					return nil, err
				}
				c.LocalHash, c.LocalExec = hash, exec
			}
			c.Status = classify(&c)
			changes = append(changes, c)
		}
	}
	shadow(changes)
	sort.Slice(changes, func(i, j int) bool {
		if changes[i].Local != changes[j].Local {
			return changes[i].Local < changes[j].Local
		}
		return changes[i].Layer.Index < changes[j].Layer.Index
	})
	return changes, nil
}

// classify decides what to do about a single file, given the three
// versions over knows: the layer's, the local one, and the one recorded
// at the last sync.
func classify(c *Change) Status {
	if c.LocalIrregular {
		// Whatever over would otherwise do here, it would have to
		// replace something it does not understand. Say so instead.
		if c.RemotePresent() || c.Base != nil {
			return Conflict
		}
		return Unchanged
	}
	remote, local := c.RemotePresent(), c.LocalPresent()
	switch {
	case c.Base == nil:
		// Never synced. over will take the layer's copy only when
		// there is nothing local to lose.
		switch {
		case remote && !local:
			return Pull
		case remote && local && c.Remote.Hash == c.LocalHash:
			return Adopt
		case remote && local:
			return Conflict
		case local && c.Claimed:
			// The layer does not hold the file, but its rules claim
			// it. Nobody had to name this one.
			return Push
		default:
			// The layer does not provide the file, so it is not ours.
			return Unchanged
		}

	case c.Base.Deleted:
		// The layer had nothing here at the last sync, either because
		// the file was deleted or because "over track" staged it.
		switch {
		case remote && !local:
			return Pull
		case remote && local && c.Remote.Hash == c.LocalHash:
			return Adopt
		case remote && local:
			return Conflict
		case local:
			return Push
		default:
			return Unchanged
		}
	}

	localChanged := !local || c.LocalHash != c.Base.Hash || c.LocalExec != c.Base.Exec
	remoteChanged := !remote || c.Remote.Hash != c.Base.Hash || c.Remote.Exec != c.Base.Exec
	switch {
	case !localChanged && !remoteChanged:
		return Unchanged
	case remoteChanged && !localChanged:
		if remote {
			return Pull
		}
		return PullDelete
	case localChanged && !remoteChanged:
		if local {
			return Push
		}
		return PushDelete
	case local && remote && c.LocalHash == c.Remote.Hash && c.LocalExec == c.Remote.Exec:
		return Adopt
	case !local && !remote:
		return Adopt
	default:
		return Conflict
	}
}

// shadow marks the changes of layers that do not own their file. The
// owner is the last layer that provides the file, so that later layers
// win; when no layer provides it any longer, the last layer that knows
// of it takes responsibility for retiring it.
func shadow(changes []Change) {
	type owner struct{ provider, known int }
	owners := map[string]owner{}
	for i, c := range changes {
		o, ok := owners[c.Local]
		if !ok {
			o = owner{provider: -1, known: -1}
		}
		provides := c.RemotePresent() || c.Claimed || (c.Base != nil && !c.Base.Deleted)
		if provides && (o.provider < 0 || changes[o.provider].Layer.Index < c.Layer.Index) {
			o.provider = i
		}
		if o.known < 0 || changes[o.known].Layer.Index < c.Layer.Index {
			o.known = i
		}
		owners[c.Local] = o
	}
	for i := range changes {
		o := owners[changes[i].Local]
		want := o.provider
		if want < 0 {
			want = o.known
		}
		changes[i].Owner = i == want
		if !changes[i].Owner && changes[i].Status != Unchanged {
			changes[i].Status = Shadowed
		}
	}
}

// Filter returns the changes whose local path matches, preserving order.
func Filter(changes []Change, match func(path string) bool) []Change {
	out := changes[:0:0]
	for _, c := range changes {
		if match(c.Local) {
			out = append(out, c)
		}
	}
	return out
}

// Counts summarizes a set of changes.
type Counts struct {
	Conflicts int
	Updated   int
	Written   int
	Unchanged int
	Shadowed  int
}

// Count summarizes changes. Deletions count with the direction they move
// in: a file over removes locally counts as updated, one it tombstones
// in a layer counts as written.
func Count(changes []Change) Counts {
	var n Counts
	for _, c := range changes {
		switch c.Status {
		case Conflict:
			n.Conflicts++
		case Pull, PullDelete:
			n.Updated++
		case Push, PushDelete:
			n.Written++
		case Shadowed:
			n.Shadowed++
		default:
			n.Unchanged++
		}
	}
	return n
}

// RelTo returns path relative to dir when that is shorter and does not
// escape upwards, and path otherwise. It is used to print paths the way
// the user is most likely to have typed them.
func RelTo(dir, path string) string {
	rel, err := filepath.Rel(dir, path)
	if err != nil || len(rel) >= len(path) {
		return path
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return path
	}
	return rel
}
