package over

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mariusae/over/internal/config"
	"github.com/mariusae/over/internal/gitrepo"
	"github.com/mariusae/over/internal/spec"
	"github.com/mariusae/over/internal/state"
)

// Sync applies changes: it writes the layers' copies over the local
// files that are behind, and commits the local files that are ahead back
// to their layers. Conflicts and shadowed files are left alone.
//
// Pulls are applied first and recorded as they go, so that an error part
// way through leaves the state describing what actually happened.
// Pushes are gathered into one commit per repository, and recorded only
// once that commit has been pushed.
func (o *Over) Sync(ctx context.Context, layers []*Layer, changes []Change, origin Origin) error {
	for i := range changes {
		c := &changes[i]
		switch c.Status {
		case Pull:
			if err := pull(c); err != nil {
				return err
			}
		case PullDelete:
			if err := pullDelete(c); err != nil {
				return err
			}
		case Adopt:
			record(c)
		}
	}
	if err := o.push(ctx, changes, origin); err != nil {
		return err
	}
	now := time.Now().UTC().Truncate(time.Second)
	for _, l := range layers {
		head, err := l.Repo.Head(ctx)
		if err != nil {
			return err
		}
		l.State.Commit, l.State.Synced = head, now
	}
	return nil
}

// pull writes the layer's copy of a file over the local one.
func pull(c *Change) error {
	data, err := os.ReadFile(c.Layer.RepoPath(c.Path))
	if err != nil {
		return err
	}
	if err := writeFile(c.Local, data, c.Remote.Exec); err != nil {
		return err
	}
	record(c)
	return nil
}

// pullDelete removes a file the layer has deleted, and any directories
// that are left empty beneath the layer's root.
func pullDelete(c *Change) error {
	if err := os.Remove(c.Local); err != nil && !os.IsNotExist(err) {
		return err
	}
	pruneDirs(filepath.Dir(c.Local), c.Layer.Root)
	record(c)
	return nil
}

// push stages every outgoing change in its layer's checkout, then
// commits and pushes each repository once.
func (o *Over) push(ctx context.Context, changes []Change, origin Origin) error {
	var (
		repos   []*gitrepo.Repo
		byRepo  = map[*gitrepo.Repo][]*Change{}
		specOf  = map[*gitrepo.Repo]spec.Spec{}
		layers  []*Layer
		touched = map[*Layer]bool{}
	)
	for i := range changes {
		c := &changes[i]
		if c.Status != Push && c.Status != PushDelete {
			continue
		}
		if err := stage(c); err != nil {
			return err
		}
		if _, ok := byRepo[c.Layer.Repo]; !ok {
			repos = append(repos, c.Layer.Repo)
			specOf[c.Layer.Repo] = c.Layer.Spec
		}
		byRepo[c.Layer.Repo] = append(byRepo[c.Layer.Repo], c)
		if !touched[c.Layer] {
			touched[c.Layer] = true
			layers = append(layers, c.Layer)
		}
	}
	for _, l := range layers {
		if err := l.SaveTombstones(); err != nil {
			return err
		}
	}
	for _, repo := range repos {
		cs := byRepo[repo]
		if _, err := repo.Commit(ctx, CommitMessage(cs, origin)); err != nil {
			return err
		}
		if err := o.PushRepo(ctx, specOf[repo], repo); err != nil {
			return err
		}
		for _, c := range cs {
			record(c)
		}
	}
	return nil
}

// stage applies one outgoing change to the layer's checkout.
func stage(c *Change) error {
	switch c.Status {
	case Push:
		data, err := os.ReadFile(c.Local)
		if err != nil {
			return err
		}
		if err := writeFile(c.Layer.RepoPath(c.Path), data, c.LocalExec); err != nil {
			return err
		}
		c.Layer.Tombstones.Remove(c.Path)
	case PushDelete:
		path := c.Layer.RepoPath(c.Path)
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		pruneDirs(filepath.Dir(path), c.Layer.Dir)
		c.Layer.Tombstones.Add(c.Path, time.Now())
	}
	return nil
}

// record updates a layer's state to reflect an applied change.
func record(c *Change) {
	st := c.Layer.State
	switch c.Status {
	case Pull:
		st.Set(c.Path, c.Remote.Hash, c.Remote.Exec)
	case Push:
		st.Set(c.Path, c.LocalHash, c.LocalExec)
	case PullDelete, PushDelete:
		st.SetDeleted(c.Path)
	case Adopt:
		if c.LocalPresent() {
			st.Set(c.Path, c.LocalHash, c.LocalExec)
		} else {
			st.SetDeleted(c.Path)
		}
	}
}

// Reset overwrites the local files with their layers' copies, whatever
// the local state. A file the layer no longer provides is removed.
func Reset(changes []Change) error {
	for i := range changes {
		c := &changes[i]
		switch {
		case c.RemotePresent():
			data, err := os.ReadFile(c.Layer.RepoPath(c.Path))
			if err != nil {
				return err
			}
			if err := writeFile(c.Local, data, c.Remote.Exec); err != nil {
				return err
			}
			c.Layer.State.Set(c.Path, c.Remote.Hash, c.Remote.Exec)
		default:
			if err := os.Remove(c.Local); err != nil && !os.IsNotExist(err) {
				return err
			}
			pruneDirs(filepath.Dir(c.Local), c.Layer.Root)
			c.Layer.State.SetDeleted(c.Path)
		}
	}
	return nil
}

// Ack records the layer's copy as seen without touching the local file,
// so that the local version is the one that survives: the next sync
// writes it to the layer. The act is remembered, so that the commit
// which overwrites the layer says it came from an ack.
func Ack(changes []Change) {
	for i := range changes {
		c := &changes[i]
		adopt(c, ReasonAck)
	}
}

// adopt records the layer's copy as the base for a path, noting why.
func adopt(c *Change, reason string) {
	if c.RemotePresent() {
		c.Layer.State.Set(c.Path, c.Remote.Hash, c.Remote.Exec)
	} else {
		c.Layer.State.SetDeleted(c.Path)
	}
	c.Layer.State.SetReason(c.Path, reason)
}

// Restore writes the contents a file had at a revision of its layer over
// the local copy, and records the layer's current copy as seen. The
// restored version is then the one that has changed, so the next sync
// publishes it: restoring is a way to make an old version current, not a
// way to go back in time quietly.
func Restore(ctx context.Context, c *Change, rev string) (string, error) {
	hash, err := c.Layer.Repo.Resolve(ctx, rev)
	if err != nil {
		return "", err
	}
	data, exec, err := c.Layer.Repo.Show(ctx, hash, c.RepoFile())
	if err != nil {
		return "", err
	}
	if err := writeFile(c.Local, data, exec); err != nil {
		return "", err
	}
	adopt(c, ReasonRestore)
	return hash, nil
}

// Track adds a local file to a layer, so that the next sync writes it
// there. It is recorded as deleted, which is the truth: the layer has no
// content for it yet.
func Track(l *Layer, rel string) error {
	if f := l.State.Get(rel); f != nil {
		return fmt.Errorf("%s: already tracked in %s", rel, l)
	}
	if _, _, err := state.HashFile(l.LocalPath(rel)); err != nil {
		return err
	}
	l.State.SetDeleted(rel)
	l.State.SetReason(rel, ReasonTrack)
	return nil
}

// Untrack forgets a file, leaving both the local copy and the layer's
// alone.
func Untrack(l *Layer, rel string) { l.State.Delete(rel) }

// writeFile writes data to path atomically, creating parent directories
// and setting the executable bit.
func writeFile(path string, data []byte, exec bool) error {
	perm := os.FileMode(0o644)
	if exec {
		perm = 0o755
	}
	return config.WriteFile(path, data, perm)
}

// pruneDirs removes dir and its parents for as long as they are empty,
// stopping before root.
func pruneDirs(dir, root string) {
	for dir != root && strings.HasPrefix(dir, root+string(filepath.Separator)) {
		if err := os.Remove(dir); err != nil {
			return
		}
		dir = filepath.Dir(dir)
	}
}
