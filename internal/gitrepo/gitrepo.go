// Package gitrepo drives the git command line to maintain over's local
// checkouts of layer repositories.
//
// over keeps one checkout per repository under its cache directory and
// treats it as disposable: uncommitted changes there are its own staging
// area and may be discarded, but commits are never thrown away.
package gitrepo

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// A Repo is a local checkout of a remote repository.
type Repo struct {
	dir string
	url string
}

// Open returns the checkout of url at dir, cloning it if it is not
// already there.
func Open(ctx context.Context, dir, url string) (*Repo, error) {
	r := &Repo{dir: dir, url: url}
	if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
		return r, nil
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return nil, err
	}
	// Clone into a temporary directory so that an interrupted clone
	// does not leave a half-populated checkout behind.
	tmp, err := os.MkdirTemp(filepath.Dir(dir), ".clone-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	into := filepath.Join(tmp, "repo")
	if _, err := run(ctx, "", "clone", "--quiet", url, into); err != nil {
		return nil, fmt.Errorf("clone %s: %w", url, err)
	}
	if err := os.Rename(into, dir); err != nil {
		return nil, err
	}
	return r, nil
}

// Dir returns the checkout's directory.
func (r *Repo) Dir() string { return r.dir }

// URL returns the remote the checkout was cloned from.
func (r *Repo) URL() string { return r.url }

// Update discards any uncommitted changes in the checkout, fetches the
// remote, and fast-forwards to it. It does not discard local commits; if
// the checkout has diverged from the remote, Update returns an error.
func (r *Repo) Update(ctx context.Context) error {
	if err := r.Reset(ctx); err != nil {
		return err
	}
	// A previous run may have committed but failed to push, e.g.
	// because the network was down. Publish that work before fetching,
	// so that the fast-forward below has a chance of succeeding.
	if unpushed, err := r.Unpushed(ctx); err == nil && unpushed {
		if err := r.Push(ctx); err != nil {
			return err
		}
	}
	if _, err := r.git(ctx, "fetch", "--quiet", "origin"); err != nil {
		return fmt.Errorf("fetch %s: %w", r.url, err)
	}
	branch, err := r.Branch(ctx)
	if err != nil {
		return err
	}
	if _, err := r.git(ctx, "rev-parse", "--verify", "--quiet", "refs/remotes/origin/"+branch); err != nil {
		// The branch does not exist on the remote yet; nothing to
		// fast-forward to.
		return nil
	}
	if _, err := r.git(ctx, "merge", "--ff-only", "--quiet", "origin/"+branch); err != nil {
		return fmt.Errorf("%s: local checkout has diverged from %s; "+
			"resolve it by hand in %s, or delete the directory to re-clone: %w",
			r.url, "origin/"+branch, r.dir, err)
	}
	return nil
}

// Reset discards uncommitted changes, including untracked files.
func (r *Repo) Reset(ctx context.Context) error {
	if empty, err := r.Empty(ctx); err != nil {
		return err
	} else if !empty {
		if _, err := r.git(ctx, "reset", "--quiet", "--hard", "HEAD"); err != nil {
			return err
		}
	}
	_, err := r.git(ctx, "clean", "-qfd")
	return err
}

// Branch returns the name of the checked-out branch. It works in a
// repository with no commits, where the branch is unborn.
func (r *Repo) Branch(ctx context.Context) (string, error) {
	out, err := r.git(ctx, "symbolic-ref", "--short", "HEAD")
	if err != nil {
		return "", fmt.Errorf("%s: cannot determine branch: %w", r.dir, err)
	}
	return out, nil
}

// Head returns the revision of the checked-out commit, or the empty
// string if the repository has no commits.
func (r *Repo) Head(ctx context.Context) (string, error) {
	if empty, err := r.Empty(ctx); err != nil || empty {
		return "", err
	}
	return r.git(ctx, "rev-parse", "HEAD")
}

// Empty reports whether the repository has no commits.
func (r *Repo) Empty(ctx context.Context) (bool, error) {
	if _, err := r.git(ctx, "rev-parse", "--verify", "--quiet", "HEAD"); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			// rev-parse exits non-zero when HEAD is unborn.
			return true, nil
		}
		return false, err
	}
	return false, nil
}

// Dirty reports whether the checkout has uncommitted changes.
func (r *Repo) Dirty(ctx context.Context) (bool, error) {
	out, err := r.git(ctx, "status", "--porcelain")
	if err != nil {
		return false, err
	}
	return out != "", nil
}

// Commit stages every change in the checkout and commits it with the
// given message. It reports whether a commit was made; there is none
// when the checkout is clean.
func (r *Repo) Commit(ctx context.Context, message string) (bool, error) {
	if _, err := r.git(ctx, "add", "--all", "."); err != nil {
		return false, err
	}
	if dirty, err := r.Dirty(ctx); err != nil || !dirty {
		return false, err
	}
	args := []string{"commit", "--quiet", "--message", message}
	if ident, err := r.identity(ctx); err != nil {
		return false, err
	} else if len(ident) > 0 {
		args = append(ident, args...)
	}
	if _, err := r.git(ctx, args...); err != nil {
		return false, err
	}
	return true, nil
}

// identity returns git -c arguments supplying an author identity, for
// the case where the user has configured none.
func (r *Repo) identity(ctx context.Context) ([]string, error) {
	if _, err := r.git(ctx, "config", "--get", "user.email"); err == nil {
		return nil, nil
	}
	host, err := os.Hostname()
	if err != nil || host == "" {
		host = "localhost"
	}
	user := os.Getenv("USER")
	if user == "" {
		user = "over"
	}
	return []string{"-c", "user.name=" + user, "-c", "user.email=" + user + "@" + host}, nil
}

// Push publishes the checked-out branch to the remote.
func (r *Repo) Push(ctx context.Context) error {
	branch, err := r.Branch(ctx)
	if err != nil {
		return err
	}
	if _, err := r.git(ctx, "push", "--quiet", "origin", "HEAD:refs/heads/"+branch); err != nil {
		return fmt.Errorf("push to %s: %w", r.url, err)
	}
	return nil
}

// Unpushed reports whether the checkout has commits the remote does not.
func (r *Repo) Unpushed(ctx context.Context) (bool, error) {
	branch, err := r.Branch(ctx)
	if err != nil {
		return false, err
	}
	if _, err := r.git(ctx, "rev-parse", "--verify", "--quiet", "refs/remotes/origin/"+branch); err != nil {
		empty, err := r.Empty(ctx)
		return !empty, err
	}
	out, err := r.git(ctx, "rev-list", "--count", "origin/"+branch+"..HEAD")
	if err != nil {
		return false, err
	}
	return out != "0", nil
}

// A Commit describes a revision of the repository.
type Commit struct {
	Hash    string    // the full revision
	Short   string    // the abbreviated revision
	Author  string    // the author's name
	Date    time.Time // the author date
	Subject string    // the first line of the message
	Body    string    // the rest of the message
}

// logFormat separates a commit's fields with NUL and the commits
// themselves with the record separator, so that multi-line bodies come
// back unambiguously.
const logFormat = "--format=%H%x00%h%x00%an%x00%aI%x00%s%x00%b%x1e"

// Log returns the commits touching path, which is relative to the root
// of the checkout, most recent first. A limit of zero means all of them.
// An empty repository, or a path with no history, yields no commits.
func (r *Repo) Log(ctx context.Context, path string, limit int) ([]*Commit, error) {
	if empty, err := r.Empty(ctx); err != nil || empty {
		return nil, err
	}
	args := []string{"log", logFormat}
	if limit > 0 {
		args = append(args, fmt.Sprintf("-%d", limit))
	}
	args = append(args, "--", path)
	out, err := r.git(ctx, args...)
	if err != nil {
		return nil, err
	}
	var commits []*Commit
	for _, record := range strings.Split(out, "\x1e") {
		record = strings.TrimSpace(record)
		if record == "" {
			continue
		}
		fields := strings.Split(record, "\x00")
		if len(fields) != 6 {
			return nil, fmt.Errorf("git log: unexpected output %q", record)
		}
		c := &Commit{
			Hash:    fields[0],
			Short:   fields[1],
			Author:  fields[2],
			Subject: fields[4],
			Body:    fields[5],
		}
		if c.Date, err = time.Parse(time.RFC3339, fields[3]); err != nil {
			return nil, fmt.Errorf("git log: %w", err)
		}
		commits = append(commits, c)
	}
	return commits, nil
}

// LastCommit returns the most recent commit touching path, or nil when
// the path has no history.
func (r *Repo) LastCommit(ctx context.Context, path string) (*Commit, error) {
	commits, err := r.Log(ctx, path, 1)
	if err != nil || len(commits) == 0 {
		return nil, err
	}
	return commits[0], nil
}

// Resolve returns the full revision named by rev, which may be
// abbreviated or symbolic.
func (r *Repo) Resolve(ctx context.Context, rev string) (string, error) {
	hash, err := r.git(ctx, "rev-parse", "--verify", "--quiet", rev+"^{commit}")
	if err != nil {
		return "", fmt.Errorf("%s: no such revision in %s", rev, r.url)
	}
	return hash, nil
}

// ErrNotInRevision reports that a path does not exist at a revision.
var ErrNotInRevision = errors.New("not present in revision")

// Show returns the contents of path as of rev, and whether it is
// executable there. It reports ErrNotInRevision when the revision has no
// such file.
func (r *Repo) Show(ctx context.Context, rev, path string) (data []byte, exec bool, err error) {
	entry, err := r.git(ctx, "ls-tree", rev, "--", path)
	if err != nil {
		return nil, false, err
	}
	if entry == "" {
		return nil, false, fmt.Errorf("%s at %s: %w", path, rev, ErrNotInRevision)
	}
	// "<mode> <type> <object>\t<path>"
	mode, rest, ok := strings.Cut(entry, " ")
	if !ok {
		return nil, false, fmt.Errorf("git ls-tree: unexpected output %q", entry)
	}
	kind, _, _ := strings.Cut(rest, " ")
	if kind != "blob" {
		return nil, false, fmt.Errorf("%s at %s: not a regular file", path, rev)
	}
	data, err = runRaw(ctx, r.dir, "show", rev+":"+path)
	if err != nil {
		return nil, false, err
	}
	return data, mode == "100755", nil
}

// git runs a git command in the checkout.
func (r *Repo) git(ctx context.Context, args ...string) (string, error) {
	return run(ctx, r.dir, args...)
}

// run runs a git command in dir, returning its trimmed standard output.
func run(ctx context.Context, dir string, args ...string) (string, error) {
	out, err := runRaw(ctx, dir, args...)
	return strings.TrimSpace(string(out)), err
}

// runRaw runs a git command in dir, returning its standard output
// unchanged. File contents have to come back this way: trimming them
// would corrupt them.
func runRaw(ctx context.Context, dir string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	// Keep git from stopping to ask for credentials; over is not
	// interactive.
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	if err := cmd.Run(); err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return nil, fmt.Errorf("git %s: %s: %w", args[0], msg, err)
		}
		return nil, fmt.Errorf("git %s: %w", args[0], err)
	}
	return stdout.Bytes(), nil
}
