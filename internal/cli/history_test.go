package cli

import (
	"strings"
	"testing"
)

// TestFetch checks that fetch brings the layers up to date without
// touching anything local, which is what makes the status that follows
// worth reading.
func TestFetch(t *testing.T) {
	c, r := newLayer(t)
	c.mustOver("sync")
	r.write("editors/.emacs", "remote\n")
	r.commit("remote edit")

	// Before fetching, over is reporting from the last sync.
	if out := c.mustOver("status"); !strings.Contains(out, "2 unchanged") {
		t.Errorf("status before fetch saw the change:\n%s", out)
	}

	out := c.mustOver("fetch")
	if !strings.Contains(out, "mariusae/config") || !strings.Contains(out, "..") {
		t.Errorf("fetch = %q, want a revision range", out)
	}
	if !strings.Contains(out, "1 repository updated") {
		t.Errorf("fetch = %q", out)
	}
	// Nothing local moved.
	if got := c.read(".emacs"); got != "(setq inhibit-startup-message t)\n" {
		t.Errorf("fetch wrote the local file: %q", got)
	}
	// But status now reports against the layer as it is.
	if out := c.mustOver("status"); !strings.Contains(out, ".emacs from mariusae/config:editors") {
		t.Errorf("status after fetch:\n%s", out)
	}

	if out := c.mustOver("fetch"); !strings.Contains(out, "up to date") {
		t.Errorf("second fetch = %q", out)
	}
}

func TestFetchTakesNoArguments(t *testing.T) {
	c, _ := newLayer(t)
	if code, _, _ := c.over("fetch", "extra"); code != exitUsage {
		t.Errorf("exit %d, want %d", code, exitUsage)
	}
}

// TestLog checks that the history of a file names where each version
// came from and what over was doing.
func TestLog(t *testing.T) {
	c, _ := newLayer(t)
	c.mustOver("sync")
	c.write(".emacs", "local\n")
	c.mustOver("sync")

	out := c.mustOver("log", ".emacs")
	if !strings.Contains(out, "mariusae/config:editors, editors/.emacs") {
		t.Errorf("log does not name the layer and file:\n%s", out)
	}
	// over's own commit carries the machine it ran on and the action.
	if !strings.Contains(out, "@") || !strings.Contains(out, "write") {
		t.Errorf("log does not report the origin and action:\n%s", out)
	}
	// The commit made by the test repository, by hand, falls back to
	// git's author.
	if !strings.Contains(out, "test") || !strings.Contains(out, "init") {
		t.Errorf("log does not report the hand-made commit:\n%s", out)
	}
	if !strings.Contains(out, "over restore <revision> .emacs") {
		t.Errorf("log does not say how to restore:\n%s", out)
	}
}

// TestLogRecordsTheReason checks that a deliberate act -- acking a
// conflict, tracking a new file -- is recorded in the history, because
// it is the explanation for an overwrite that would otherwise look
// wrong.
func TestLogRecordsTheReason(t *testing.T) {
	c, r := newLayer(t)
	c.mustOver("sync")
	r.write("editors/.emacs", "remote\n")
	r.commit("remote edit")
	c.write(".emacs", "local\n")
	if code, _, _ := c.over("sync"); code != exitError {
		t.Fatal("expected a conflict")
	}
	c.mustOver("ack", ".emacs")
	c.mustOver("sync")

	if out := c.mustOver("log", "-n", "1", ".emacs"); !strings.Contains(out, "write (ack)") {
		t.Errorf("log does not record the ack:\n%s", out)
	}

	c.write(".newrc", "new\n")
	c.mustOver("track", "mariusae/config:editors", ".newrc")
	c.mustOver("sync")
	if out := c.mustOver("log", "-n", "1", ".newrc"); !strings.Contains(out, "write (track)") {
		t.Errorf("log does not record the track:\n%s", out)
	}
}

// TestLogWithoutHistory checks a file over knows about but that no layer
// has ever held.
func TestLogWithoutHistory(t *testing.T) {
	c, _ := newLayer(t)
	c.mustOver("sync")
	c.write(".newrc", "new\n")
	c.mustOver("track", "mariusae/config:editors", ".newrc")

	if out := c.mustOver("log", ".newrc"); !strings.Contains(out, "no history in any layer") {
		t.Errorf("log = %q", out)
	}
}

func TestLogRequiresAPath(t *testing.T) {
	c, _ := newLayer(t)
	if code, _, _ := c.over("log"); code != exitUsage {
		t.Errorf("exit %d, want %d", code, exitUsage)
	}
}

// TestRestore checks putting an earlier version back: the local file
// changes, and the next sync publishes it as the current one.
func TestRestore(t *testing.T) {
	c, r := newLayer(t)
	c.mustOver("sync")
	first := revisionOf(t, c, ".emacs", 0)

	c.write(".emacs", "second\n")
	c.mustOver("sync")
	c.write(".emacs", "third\n")
	c.mustOver("sync")

	out := c.mustOver("restore", first, ".emacs")
	if !strings.Contains(out, "restored from") {
		t.Errorf("restore = %q", out)
	}
	if got := c.read(".emacs"); got != "(setq inhibit-startup-message t)\n" {
		t.Errorf("after restore, .emacs = %q", got)
	}
	// The restored version is now the one that has changed, so it is
	// published rather than reverted on the next sync.
	if out := c.mustOver("sync"); !strings.Contains(out, ".emacs to mariusae/config:editors") {
		t.Errorf("sync after restore = %q", out)
	}
	r.pull()
	if got := r.read("editors/.emacs"); got != "(setq inhibit-startup-message t)\n" {
		t.Errorf("layer copy after sync = %q", got)
	}
	if out := c.mustOver("log", "-n", "1", ".emacs"); !strings.Contains(out, "write (restore)") {
		t.Errorf("log does not record the restore:\n%s", out)
	}
}

// TestRestoreAbbreviatedRevision checks that the revisions log prints
// can be shortened, as git's can.
func TestRestoreAbbreviatedRevision(t *testing.T) {
	c, _ := newLayer(t)
	c.mustOver("sync")
	first := revisionOf(t, c, ".emacs", 0)
	c.write(".emacs", "second\n")
	c.mustOver("sync")

	c.mustOver("restore", first[:7], ".emacs")
	if got := c.read(".emacs"); got != "(setq inhibit-startup-message t)\n" {
		t.Errorf("after restore, .emacs = %q", got)
	}
}

func TestRestoreErrors(t *testing.T) {
	c, _ := newLayer(t)
	c.mustOver("sync")

	code, _, stderr := c.over("restore", "nosuchrevision", ".emacs")
	if code != exitError || !strings.Contains(stderr, "no such revision") {
		t.Errorf("bad revision: exit %d, stderr %q", code, stderr)
	}
	if code, _, _ := c.over("restore", ".emacs"); code != exitUsage {
		t.Errorf("missing revision: exit %d, want %d", code, exitUsage)
	}

	// A revision that exists but does not hold the file.
	c.write(".newrc", "new\n")
	c.mustOver("track", "mariusae/config:editors", ".newrc")
	c.mustOver("sync")
	first := revisionOf(t, c, ".emacs", 0)
	code, _, stderr = c.over("restore", first, ".newrc")
	if code != exitError || !strings.Contains(stderr, "not present in revision") {
		t.Errorf("file absent from the revision: exit %d, stderr %q", code, stderr)
	}
}

// revisionOf returns the revision of the nth entry, counting back from
// the oldest, in the log of a path.
func revisionOf(t *testing.T, c *client, path string, fromOldest int) string {
	t.Helper()
	var revs []string
	for _, line := range strings.Split(c.mustOver("log", path), "\n") {
		fields := strings.Fields(line)
		if len(fields) > 0 && len(fields[0]) == 12 && strings.Trim(fields[0], "0123456789abcdef") == "" {
			revs = append(revs, fields[0])
		}
	}
	if len(revs) <= fromOldest {
		t.Fatalf("log of %s has %d revisions, want more than %d", path, len(revs), fromOldest)
	}
	return revs[len(revs)-1-fromOldest]
}

// TestLogHintPrintedOnce checks that the trailing hint belongs to the
// command, not to each path the arguments expanded to.
func TestLogHintPrintedOnce(t *testing.T) {
	c, _ := newLayer(t)
	c.mustOver("sync")
	c.write(".apex/attach", "attach\n")
	c.write(".apex/profile", "profile\n")
	c.mustOver("track", "mariusae/config:editors", ".apex/...")
	c.mustOver("sync")

	out := c.mustOver("log", ".apex/...")
	if n := strings.Count(out, "restore an earlier version"); n != 1 {
		t.Errorf("hint printed %d times, want once:\n%s", n, out)
	}
	// It cannot name one path when it covers several.
	if !strings.Contains(out, "over restore <revision> <path>") {
		t.Errorf("hint does not generalize:\n%s", out)
	}
	// Both histories are still reported.
	for _, want := range []string{".apex/attach", ".apex/profile"} {
		if !strings.Contains(out, want) {
			t.Errorf("log output missing %q:\n%s", want, out)
		}
	}

	// With one path there is no reason not to name it.
	out = c.mustOver("log", ".apex/attach")
	if !strings.Contains(out, "over restore <revision> .apex/attach") {
		t.Errorf("hint for a single path = %q", out)
	}
}

// TestUntrackNotePrintedOnce checks the same for the note untrack leaves
// when a rule will claim the files straight back.
func TestUntrackNotePrintedOnce(t *testing.T) {
	c, _ := newLayer(t)
	c.mustOver("sync")
	c.write(".apex/attach", "attach\n")
	c.write(".apex/profile", "profile\n")
	c.mustOver("track", "mariusae/config:editors", ".apex/...")
	c.mustOver("sync")

	out := c.mustOver("untrack", ".apex/...")
	if n := strings.Count(out, "carve"); n != 1 {
		t.Errorf("note printed %d times, want once:\n%s", n, out)
	}
	if !strings.Contains(out, "2 files still claimed by a rule") {
		t.Errorf("note does not say how many:\n%s", out)
	}
	// The layer is still named, since they all share one.
	if !strings.Contains(out, "over rule -ignore mariusae/config:editors <pattern>") {
		t.Errorf("note does not name the layer:\n%s", out)
	}
	// Each file is still reported individually.
	for _, want := range []string{".apex/attach untracked", ".apex/profile untracked"} {
		if !strings.Contains(out, want) {
			t.Errorf("untrack output missing %q:\n%s", want, out)
		}
	}
}
