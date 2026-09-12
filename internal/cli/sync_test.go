package cli

import (
	"path/filepath"
	"strings"
	"testing"
)

// newLayer sets up a client with one repository providing an "editors"
// layer, adds it, and returns both.
func newLayer(t *testing.T) (*client, *repo) {
	t.Helper()
	c := newClient(t)
	r := c.newRepo("mariusae", "config")
	r.write("editors/.emacs", "(setq inhibit-startup-message t)\n")
	r.write("editors/.zshrc", "set -o vi\n")
	r.commit("init")
	c.mustOver("add", "mariusae/config:editors")
	return c, r
}

func TestSyncPull(t *testing.T) {
	c, _ := newLayer(t)
	out := c.mustOver("sync")
	for _, want := range []string{
		".emacs from mariusae/config:editors",
		".zshrc from mariusae/config:editors",
		"2 updated",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("sync output missing %q:\n%s", want, out)
		}
	}
	if got := c.read(".emacs"); got != "(setq inhibit-startup-message t)\n" {
		t.Errorf(".emacs = %q", got)
	}
	// A second sync has nothing to do.
	if out := c.mustOver("sync"); !strings.Contains(out, "2 unchanged") {
		t.Errorf("second sync = %q, want 2 unchanged", out)
	}
}

func TestSyncPush(t *testing.T) {
	c, r := newLayer(t)
	c.mustOver("sync")
	c.write(".zshrc", "set -o emacs\n")

	out := c.mustOver("sync")
	if !strings.Contains(out, ".zshrc to mariusae/config:editors") {
		t.Errorf("sync output = %q", out)
	}
	if !strings.Contains(out, "1 written") {
		t.Errorf("sync output = %q, want 1 written", out)
	}
	r.pull()
	if got := r.read("editors/.zshrc"); got != "set -o emacs\n" {
		t.Errorf("layer .zshrc = %q", got)
	}
}

func TestConflictResetTakesTheLayer(t *testing.T) {
	c, r := newLayer(t)
	c.mustOver("sync")

	r.write("editors/.emacs", "remote\n")
	r.commit("remote edit")
	c.write(".emacs", "local\n")

	code, out, _ := c.over("sync")
	if code != exitError {
		t.Errorf("sync with a conflict: exit %d, want %d", code, exitError)
	}
	if !strings.Contains(out, ".emacs conflicts") || !strings.Contains(out, "1 conflict") {
		t.Errorf("sync output = %q", out)
	}

	diff := c.mustOver("diff")
	for _, want := range []string{"--- .emacs (local)", "+++ .emacs (mariusae/config:editors)", "-local", "+remote"} {
		if !strings.Contains(diff, want) {
			t.Errorf("diff output missing %q:\n%s", want, diff)
		}
	}

	c.mustOver("reset", ".emacs")
	if got := c.read(".emacs"); got != "remote\n" {
		t.Errorf("after reset, .emacs = %q", got)
	}
	if out := c.mustOver("sync"); !strings.Contains(out, "2 unchanged") {
		t.Errorf("sync after reset = %q", out)
	}
}

func TestConflictAckKeepsLocal(t *testing.T) {
	c, r := newLayer(t)
	c.mustOver("sync")

	r.write("editors/.emacs", "remote\n")
	r.commit("remote edit")
	c.write(".emacs", "local\n")

	if code, _, _ := c.over("sync"); code != exitError {
		t.Fatalf("sync with a conflict: exit %d", code)
	}
	c.mustOver("ack", ".emacs")
	if got := c.read(".emacs"); got != "local\n" {
		t.Errorf("ack changed the local file: %q", got)
	}
	out := c.mustOver("sync")
	if !strings.Contains(out, ".emacs to mariusae/config:editors") {
		t.Errorf("sync after ack = %q", out)
	}
	r.pull()
	if got := r.read("editors/.emacs"); got != "local\n" {
		t.Errorf("layer .emacs = %q, want the local version", got)
	}
}

// TestUnsyncedLocalFileIsAConflict is over's central promise: a file it
// has never synced is never overwritten, even on the very first sync.
func TestUnsyncedLocalFileIsAConflict(t *testing.T) {
	c, _ := newLayer(t)
	c.write(".emacs", "mine, from before over\n")

	code, out, _ := c.over("sync")
	if code != exitError {
		t.Errorf("exit %d, want %d", code, exitError)
	}
	if !strings.Contains(out, ".emacs conflicts") {
		t.Errorf("sync output = %q", out)
	}
	if got := c.read(".emacs"); got != "mine, from before over\n" {
		t.Errorf(".emacs was overwritten: %q", got)
	}
	// The other file, which was not in the way, is synced as usual.
	if !c.exists(".zshrc") {
		t.Error(".zshrc was not synced")
	}
}

// TestUnsyncedIdenticalFileIsAdopted checks the happy version of the
// same case: a local file that already matches the layer is simply taken
// over.
func TestUnsyncedIdenticalFileIsAdopted(t *testing.T) {
	c, _ := newLayer(t)
	c.write(".emacs", "(setq inhibit-startup-message t)\n")
	out := c.mustOver("sync")
	if !strings.Contains(out, "1 updated") || !strings.Contains(out, "1 unchanged") {
		t.Errorf("sync output = %q, want .emacs adopted and .zshrc pulled", out)
	}
}

func TestTrack(t *testing.T) {
	c, r := newLayer(t)
	c.mustOver("sync")
	c.write(".config/ion/config", "plugin = 1\n")

	out := c.mustOver("track", "mariusae/config:editors", ".config/ion/config")
	if !strings.Contains(out, ".config/ion/config tracked in mariusae/config:editors") {
		t.Errorf("track output = %q", out)
	}
	c.mustOver("sync")
	r.pull()
	if got := r.read("editors/.config/ion/config"); got != "plugin = 1\n" {
		t.Errorf("layer copy = %q", got)
	}

	// Untracking leaves both copies in place.
	c.mustOver("untrack", ".config/ion/config")
	if !c.exists(".config/ion/config") {
		t.Error("untrack removed the local file")
	}
	if out := c.mustOver("status"); strings.Contains(out, ".config/ion/config") {
		t.Errorf("status still mentions the untracked file:\n%s", out)
	}
}

// TestTrackRequiresALayer checks that over does not guess which layer a
// file belongs in: the layer decides where the file is published.
func TestTrackRequiresALayer(t *testing.T) {
	c, _ := newLayer(t)
	c.mustOver("sync")
	c.write(".config/ion/config", "plugin = 1\n")

	code, _, stderr := c.over("track", ".config/ion/config")
	if code != exitUsage {
		t.Errorf("track with no layer: exit %d, want %d", code, exitUsage)
	}
	if !strings.Contains(stderr, "Usage:") {
		t.Errorf("stderr = %q, want the usage message", stderr)
	}

	code, _, stderr = c.over("track", "mariusae/config:nosuch", ".config/ion/config")
	if code != exitError || !strings.Contains(stderr, "not a configured layer") {
		t.Errorf("track in an unconfigured layer: exit %d, stderr %q", code, stderr)
	}
}

// TestTrackRefusesOutsideTheRoot checks that a file has to lie under the
// root of the layer it is tracked in.
func TestTrackRefusesOutsideTheRoot(t *testing.T) {
	c, _ := newMultiLayer(t)
	etc := filepath.Join(c.root, "etc")
	mkdir(t, etc)
	t.Setenv("OVERTESTETC", etc)
	c.mustOver("add", "mariusae/config:etc")
	c.mustOver("sync")
	c.write(".zshrc", "local\n")

	code, _, stderr := c.over("track", "mariusae/config:etc", ".zshrc")
	if code != exitError || !strings.Contains(stderr, "not under") {
		t.Errorf("exit %d, stderr %q", code, stderr)
	}
}

// TestTrackRefusesOverOwnHome checks that over never manages its own
// configuration, which would otherwise churn on every sync. It holds for
// a file named outright and for one a rule would otherwise claim.
func TestTrackRefusesOverOwnHome(t *testing.T) {
	c, _ := newLayer(t)
	c.mustOver("sync")
	code, _, stderr := c.over("track", "mariusae/config:editors", ".config/over/config.yaml")
	if code != exitError {
		t.Errorf("exit %d, want %d (stderr %q)", code, exitError, stderr)
	}
	if !strings.Contains(stderr, "nothing new to track") {
		t.Errorf("stderr = %q", stderr)
	}

	// A rule covering over's own home claims nothing from it.
	if out := c.mustOver("track", "mariusae/config:editors", ".config/..."); !strings.Contains(out, "0 files") {
		t.Errorf("a rule over over's own home claimed something: %q", out)
	}
	if out := c.mustOver("sync"); strings.Contains(out, ".config/over") {
		t.Errorf("sync took over's own files:\n%s", out)
	}
}

func TestDeleteTombstone(t *testing.T) {
	c, r := newLayer(t)
	c.mustOver("sync")

	// A second client that has the file too, so that the deletion has
	// somewhere to propagate.
	other := c.sub(t)
	other.mustOver("add", "mariusae/config:editors")
	other.mustOver("sync")
	if got := other.read(".zshrc"); got != "set -o vi\n" {
		t.Fatalf("second client .zshrc = %q", got)
	}

	if err := removeFile(c.path(".zshrc")); err != nil {
		t.Fatal(err)
	}
	out := c.mustOver("sync")
	if !strings.Contains(out, ".zshrc deleted in mariusae/config:editors") {
		t.Errorf("sync output = %q", out)
	}
	r.pull()
	if r.exists("editors/.zshrc") {
		t.Error("layer still holds the deleted file")
	}
	if got := r.read("editors.tombstones.yaml"); !strings.Contains(got, ".zshrc") {
		t.Errorf("tombstones = %q", got)
	}

	out = other.mustOver("sync")
	if !strings.Contains(out, ".zshrc deleted from mariusae/config:editors") {
		t.Errorf("second client sync = %q", out)
	}

	// A second client, which had synced the file before it was
	// deleted, picks the deletion up.
	if other.exists(".zshrc") {
		t.Errorf("second client kept the deleted file")
	}
}

// TestDeletePropagatesToLocal covers the other direction: the layer
// deletes a file, and sync removes the local copy.
func TestDeletePropagatesToLocal(t *testing.T) {
	c, r := newLayer(t)
	c.mustOver("sync")

	r.remove("editors/.zshrc")
	r.write("editors.tombstones.yaml", "tombstones:\n    .zshrc: 2026-01-01T00:00:00Z\n")
	r.commit("delete .zshrc")

	out := c.mustOver("sync")
	if !strings.Contains(out, ".zshrc deleted from mariusae/config:editors") {
		t.Errorf("sync output = %q", out)
	}
	if c.exists(".zshrc") {
		t.Error("local copy survived the layer's deletion")
	}
}

func removeFile(path string) error { return osRemove(path) }

// TestIrregularLocalFile checks that over reports, rather than replaces,
// something at a managed path that is not a regular file.
func TestIrregularLocalFile(t *testing.T) {
	c, _ := newLayer(t)
	if err := symlink("/dev/null", c.path(".emacs")); err != nil {
		t.Fatal(err)
	}

	code, out, _ := c.over("sync")
	if code != exitError || !strings.Contains(out, ".emacs conflicts") {
		t.Errorf("sync: exit %d, out %q", code, out)
	}
	if target, err := readlink(c.path(".emacs")); err != nil || target != "/dev/null" {
		t.Errorf(".emacs = %q, %v; want the symlink left alone", target, err)
	}
	if out := c.mustOver("diff"); !strings.Contains(out, "not a regular file") {
		t.Errorf("diff = %q", out)
	}
}
