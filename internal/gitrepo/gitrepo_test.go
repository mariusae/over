package gitrepo

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// newRemote creates an empty bare repository and returns its URL.
func newRemote(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := filepath.Join(t.TempDir(), "remote.git")
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	run("init", "--quiet", "--bare", dir)
	run("-C", dir, "symbolic-ref", "HEAD", "refs/heads/main")
	return "file://" + dir
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestEmptyRepository checks the case over meets on a fresh layer
// repository: a clone with no commits, which it must still be able to
// commit to and push.
func TestEmptyRepository(t *testing.T) {
	ctx := context.Background()
	url := newRemote(t)
	dir := filepath.Join(t.TempDir(), "checkout")

	r, err := Open(ctx, dir, url)
	if err != nil {
		t.Fatal(err)
	}
	empty, err := r.Empty(ctx)
	if err != nil || !empty {
		t.Fatalf("Empty = %v, %v; want true", empty, err)
	}
	if err := r.Update(ctx); err != nil {
		t.Fatalf("Update of an empty repository: %v", err)
	}
	write(t, filepath.Join(dir, "editors", ".emacs"), "hello\n")
	committed, err := r.Commit(ctx, "first")
	if err != nil || !committed {
		t.Fatalf("Commit = %v, %v", committed, err)
	}
	if err := r.Push(ctx); err != nil {
		t.Fatal(err)
	}
	if head, err := r.Head(ctx); err != nil || head == "" {
		t.Fatalf("Head = %q, %v", head, err)
	}
}

// TestUpdateFastForwards checks that a change published by someone else
// arrives in the checkout.
func TestUpdateFastForwards(t *testing.T) {
	ctx := context.Background()
	url := newRemote(t)
	first := seed(t, ctx, url, "one\n")

	second, err := Open(ctx, filepath.Join(t.TempDir(), "b"), url)
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(first.Dir(), "file"), "two\n")
	if _, err := first.Commit(ctx, "second"); err != nil {
		t.Fatal(err)
	}
	if err := first.Push(ctx); err != nil {
		t.Fatal(err)
	}
	if err := second.Update(ctx); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(second.Dir(), "file"))
	if err != nil || string(data) != "two\n" {
		t.Errorf("after Update, file = %q, %v", data, err)
	}
}

// TestUpdateDiscardsUncommittedWork checks that the checkout is treated
// as over's staging area: whatever is left lying about is cleaned up,
// rather than getting in the way of the next fetch.
func TestUpdateDiscardsUncommittedWork(t *testing.T) {
	ctx := context.Background()
	url := newRemote(t)
	r := seed(t, ctx, url, "one\n")

	write(t, filepath.Join(r.Dir(), "file"), "scribble\n")
	write(t, filepath.Join(r.Dir(), "junk"), "junk\n")
	if dirty, err := r.Dirty(ctx); err != nil || !dirty {
		t.Fatalf("Dirty = %v, %v", dirty, err)
	}
	if err := r.Update(ctx); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(filepath.Join(r.Dir(), "file")); err != nil || string(data) != "one\n" {
		t.Errorf("file = %q, %v; want the committed contents", data, err)
	}
	if _, err := os.Stat(filepath.Join(r.Dir(), "junk")); !os.IsNotExist(err) {
		t.Errorf("untracked file survived Update: %v", err)
	}
}

// TestUpdatePushesPendingCommits checks that work committed but not
// pushed -- because the network was down, say -- goes out on the next
// run rather than blocking the fetch.
func TestUpdatePushesPendingCommits(t *testing.T) {
	ctx := context.Background()
	url := newRemote(t)
	r := seed(t, ctx, url, "one\n")

	write(t, filepath.Join(r.Dir(), "file"), "two\n")
	if _, err := r.Commit(ctx, "unpushed"); err != nil {
		t.Fatal(err)
	}
	if unpushed, err := r.Unpushed(ctx); err != nil || !unpushed {
		t.Fatalf("Unpushed = %v, %v", unpushed, err)
	}
	if err := r.Update(ctx); err != nil {
		t.Fatal(err)
	}
	if unpushed, err := r.Unpushed(ctx); err != nil || unpushed {
		t.Errorf("Unpushed after Update = %v, %v", unpushed, err)
	}
	other, err := Open(ctx, filepath.Join(t.TempDir(), "c"), url)
	if err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(filepath.Join(other.Dir(), "file")); err != nil || string(data) != "two\n" {
		t.Errorf("the pending commit was not published: %q, %v", data, err)
	}
}

// TestUpdateReportsDivergence checks that over refuses to guess when the
// checkout and the remote have both moved on.
func TestUpdateReportsDivergence(t *testing.T) {
	ctx := context.Background()
	url := newRemote(t)
	first := seed(t, ctx, url, "one\n")
	second, err := Open(ctx, filepath.Join(t.TempDir(), "b"), url)
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(first.Dir(), "file"), "theirs\n")
	if _, err := first.Commit(ctx, "theirs"); err != nil {
		t.Fatal(err)
	}
	if err := first.Push(ctx); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(second.Dir(), "file"), "ours\n")
	if _, err := second.Commit(ctx, "ours"); err != nil {
		t.Fatal(err)
	}
	if err := second.Update(ctx); err == nil {
		t.Error("Update of a diverged checkout succeeded")
	}
}

// seed returns a checkout of url with one commit in it.
func seed(t *testing.T, ctx context.Context, url, content string) *Repo {
	t.Helper()
	r, err := Open(ctx, filepath.Join(t.TempDir(), "a"), url)
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(r.Dir(), "file"), content)
	if _, err := r.Commit(ctx, "seed"); err != nil {
		t.Fatal(err)
	}
	if err := r.Push(ctx); err != nil {
		t.Fatal(err)
	}
	return r
}

// TestLastCommit checks the history lookup "over show" reports from.
func TestLastCommit(t *testing.T) {
	ctx := context.Background()
	url := newRemote(t)
	r, err := Open(ctx, filepath.Join(t.TempDir(), "a"), url)
	if err != nil {
		t.Fatal(err)
	}

	// An empty repository has no history for anything.
	if c, err := r.LastCommit(ctx, "file"); err != nil || c != nil {
		t.Fatalf("LastCommit of an empty repository = %v, %v", c, err)
	}

	write(t, filepath.Join(r.Dir(), "editors", ".emacs"), "one\n")
	if _, err := r.Commit(ctx, "editors: add .emacs"); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(r.Dir(), "editors", ".zshrc"), "two\n")
	if _, err := r.Commit(ctx, "editors: add .zshrc"); err != nil {
		t.Fatal(err)
	}

	// The lookup is per path, not just the tip of the branch.
	c, err := r.LastCommit(ctx, "editors/.emacs")
	if err != nil {
		t.Fatal(err)
	}
	if c == nil || c.Subject != "editors: add .emacs" {
		t.Fatalf("LastCommit = %+v, want the commit that added .emacs", c)
	}
	if c.Short == "" || !strings.HasPrefix(c.Hash, c.Short) {
		t.Errorf("LastCommit revision = %q, %q", c.Hash, c.Short)
	}
	if c.Author == "" {
		t.Error("LastCommit has no author")
	}
	if c.Date.IsZero() {
		t.Error("LastCommit has no date")
	}

	// A path with no history at all.
	if c, err := r.LastCommit(ctx, "editors/.nosuchfile"); err != nil || c != nil {
		t.Errorf("LastCommit of an untouched path = %v, %v", c, err)
	}
}

// TestLog checks the per-file history "over log" reports from.
func TestLog(t *testing.T) {
	ctx := context.Background()
	r, err := Open(ctx, filepath.Join(t.TempDir(), "a"), newRemote(t))
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(r.Dir(), "editors", ".vimrc"), "one\n")
	if _, err := r.Commit(ctx, "editors: first\n\nwrite editors/.vimrc\n\nOver-Host: h\n"); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(r.Dir(), "editors", ".vimrc"), "two\n")
	if _, err := r.Commit(ctx, "editors: second"); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(r.Dir(), "editors", ".zshrc"), "other\n")
	if _, err := r.Commit(ctx, "editors: unrelated"); err != nil {
		t.Fatal(err)
	}

	commits, err := r.Log(ctx, "editors/.vimrc", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(commits) != 2 {
		t.Fatalf("got %d commits, want 2", len(commits))
	}
	if commits[0].Subject != "editors: second" {
		t.Errorf("newest commit = %q, want the second", commits[0].Subject)
	}
	// The body survives, multiple lines and all; the log reads over's
	// own metadata out of it.
	if !strings.Contains(commits[1].Body, "Over-Host: h") {
		t.Errorf("body = %q", commits[1].Body)
	}

	if commits, err := r.Log(ctx, "editors/.vimrc", 1); err != nil || len(commits) != 1 {
		t.Errorf("Log with a limit = %d commits, %v", len(commits), err)
	}
	if commits, err := r.Log(ctx, "editors/.nothing", 0); err != nil || len(commits) != 0 {
		t.Errorf("Log of an untouched path = %d commits, %v", len(commits), err)
	}
}

// TestShow checks reading a file as it was at an earlier revision, which
// is what "over restore" is built on.
func TestShow(t *testing.T) {
	ctx := context.Background()
	r, err := Open(ctx, filepath.Join(t.TempDir(), "a"), newRemote(t))
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(r.Dir(), "editors", ".vimrc"), "one\n")
	if _, err := r.Commit(ctx, "first"); err != nil {
		t.Fatal(err)
	}
	first, err := r.Head(ctx)
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(r.Dir(), "editors", ".vimrc"), "two\n")
	if err := os.Chmod(filepath.Join(r.Dir(), "editors", ".vimrc"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Commit(ctx, "second"); err != nil {
		t.Fatal(err)
	}

	data, exec, err := r.Show(ctx, first, "editors/.vimrc")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "one\n" {
		t.Errorf("content at the first revision = %q", data)
	}
	if exec {
		t.Error("the file was reported executable at the first revision")
	}

	// Abbreviated revisions resolve, and the mode comes back with the
	// content.
	short, err := r.Resolve(ctx, first[:8])
	if err != nil || short != first {
		t.Fatalf("Resolve(%q) = %q, %v", first[:8], short, err)
	}
	head, err := r.Head(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, exec, err := r.Show(ctx, head, "editors/.vimrc"); err != nil || !exec {
		t.Errorf("Show at head: exec = %v, %v", exec, err)
	}

	// A file that is not in the revision, and a revision that does not
	// exist at all.
	if _, _, err := r.Show(ctx, first, "editors/.zshrc"); !errors.Is(err, ErrNotInRevision) {
		t.Errorf("Show of a missing file = %v, want ErrNotInRevision", err)
	}
	if _, err := r.Resolve(ctx, "nosuchrevision"); err == nil {
		t.Error("Resolve of a bad revision succeeded")
	}
}

// TestOpenFollowsTheURL checks that a checkout already in the cache is
// pointed at the URL over currently means to use. The default scheme can
// change under it, and the cache is over's to keep current.
func TestOpenFollowsTheURL(t *testing.T) {
	ctx := context.Background()
	url := newRemote(t)
	dir := filepath.Join(t.TempDir(), "checkout")
	if _, err := Open(ctx, dir, url); err != nil {
		t.Fatal(err)
	}

	// Reopening at a different URL moves the remote rather than
	// leaving the checkout pointed at the old one.
	moved := newRemote(t)
	r, err := Open(ctx, dir, moved)
	if err != nil {
		t.Fatal(err)
	}
	got, err := r.git(ctx, "remote", "get-url", "origin")
	if err != nil {
		t.Fatal(err)
	}
	if got != moved {
		t.Errorf("origin = %q, want %q", got, moved)
	}

	// A checkout whose origin has been removed gets one back.
	if _, err := r.git(ctx, "remote", "remove", "origin"); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(ctx, dir, moved); err != nil {
		t.Fatal(err)
	}
	if got, err := r.git(ctx, "remote", "get-url", "origin"); err != nil || got != moved {
		t.Errorf("origin = %q, %v", got, err)
	}
}
