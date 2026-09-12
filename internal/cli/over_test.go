package cli

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// A client is an over installation in a temporary directory: its own
// home, config, cache, and a set of bare repositories standing in for
// GitHub.
type client struct {
	t       *testing.T
	root    string
	home    string
	remotes string
}

// newClient sets up an over client whose repositories are local bare
// ones, reached through the $OVER_URL template.
func newClient(t *testing.T) *client {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	root := t.TempDir()
	c := &client{
		t:       t,
		root:    root,
		home:    filepath.Join(root, "home"),
		remotes: filepath.Join(root, "remotes"),
	}
	mkdir(t, c.home)
	mkdir(t, c.remotes)
	c.apply()

	// The CLI's -C flag chdirs the process; put it back afterwards.
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(cwd) })
	return c
}

// sub returns a second client sharing the same repositories, standing in
// for over running on another machine.
func (c *client) sub(t *testing.T) *client {
	t.Helper()
	other := &client{t: t, root: c.root, home: filepath.Join(c.root, "home2"), remotes: c.remotes}
	mkdir(t, other.home)
	return other
}

// apply points over's environment at this client. Each command applies
// it afresh, so that a test can drive two clients in turn.
func (c *client) apply() {
	c.t.Helper()
	c.t.Setenv("HOME", c.home)
	c.t.Setenv("OVER_HOME", filepath.Join(c.home, ".config", "over"))
	c.t.Setenv("OVER_CACHE", filepath.Join(c.home, ".cache", "over"))
	c.t.Setenv("OVER_URL", "file://"+c.remotes+"/%[2]s/%[3]s.git")
}

// over runs a command in the client's home directory.
func (c *client) over(args ...string) (code int, stdout, stderr string) {
	c.t.Helper()
	c.apply()
	var out, errOut bytes.Buffer
	args = append([]string{"-C", c.home}, args...)
	code = Run(context.Background(), &out, &errOut, args)
	return code, out.String(), errOut.String()
}

// mustOver runs a command and fails the test if it does not succeed.
func (c *client) mustOver(args ...string) string {
	c.t.Helper()
	code, stdout, stderr := c.over(args...)
	if code != exitOK {
		c.t.Fatalf("over %s: exit %d\n%s%s", strings.Join(args, " "), code, stdout, stderr)
	}
	return stdout
}

// path returns an absolute path under the client's home.
func (c *client) path(rel string) string { return filepath.Join(c.home, rel) }

// write creates a file under the client's home.
func (c *client) write(rel, content string) {
	c.t.Helper()
	writeFile(c.t, c.path(rel), content)
}

// read returns the contents of a file under the client's home.
func (c *client) read(rel string) string {
	c.t.Helper()
	data, err := os.ReadFile(c.path(rel))
	if err != nil {
		c.t.Fatalf("read %s: %v", rel, err)
	}
	return string(data)
}

// exists reports whether a file exists under the client's home.
func (c *client) exists(rel string) bool {
	_, err := os.Stat(c.path(rel))
	return err == nil
}

// A repo is a working clone of one of the bare repositories, used by
// tests to play the part of another machine editing a layer directly.
type repo struct {
	t    *testing.T
	bare string
	dir  string
}

// newBareRepo creates the bare repository owner/name with nothing in it:
// no commits, and a branch that does not exist yet. That is a repository
// as a hosting service hands it over, and over has to be able to make
// the first commit in one. The working clone is made on demand.
func (c *client) newBareRepo(owner, name string) *repo {
	c.t.Helper()
	bare := filepath.Join(c.remotes, owner, name+".git")
	mkdir(c.t, filepath.Dir(bare))
	git(c.t, "", "init", "--quiet", "--bare", bare)
	// The default branch of a bare repository created without global
	// configuration is master; the tests use main throughout.
	git(c.t, bare, "symbolic-ref", "HEAD", "refs/heads/main")
	return &repo{t: c.t, bare: bare, dir: filepath.Join(c.root, "work", owner, name)}
}

// newRepo creates the bare repository owner/name and a working clone of
// it, ready for the caller to write to and commit.
func (c *client) newRepo(owner, name string) *repo {
	c.t.Helper()
	r := c.newBareRepo(owner, name)
	r.clone()
	return r
}

// clone materializes the working clone if it is not there already.
func (r *repo) clone() {
	r.t.Helper()
	if _, err := os.Stat(filepath.Join(r.dir, ".git")); err == nil {
		return
	}
	mkdir(r.t, filepath.Dir(r.dir))
	git(r.t, "", "clone", "--quiet", r.bare, r.dir)
	git(r.t, r.dir, "symbolic-ref", "HEAD", "refs/heads/main")
}

// write creates a file in the working clone.
func (r *repo) write(rel, content string) {
	r.t.Helper()
	r.clone()
	writeFile(r.t, filepath.Join(r.dir, rel), content)
}

// remove deletes a file from the working clone.
func (r *repo) remove(rel string) {
	r.t.Helper()
	r.clone()
	if err := os.Remove(filepath.Join(r.dir, rel)); err != nil {
		r.t.Fatal(err)
	}
}

// read returns the contents of a file in the working clone.
func (r *repo) read(rel string) string {
	r.t.Helper()
	r.clone()
	data, err := os.ReadFile(filepath.Join(r.dir, rel))
	if err != nil {
		r.t.Fatalf("read %s: %v", rel, err)
	}
	return string(data)
}

// exists reports whether a file exists in the working clone.
func (r *repo) exists(rel string) bool {
	r.clone()
	_, err := os.Stat(filepath.Join(r.dir, rel))
	return err == nil
}

// commit publishes everything in the working clone.
func (r *repo) commit(message string) {
	r.t.Helper()
	r.clone()
	git(r.t, r.dir, "add", "--all", ".")
	git(r.t, r.dir, "-c", "user.name=test", "-c", "user.email=test@example.com",
		"commit", "--quiet", "--message", message)
	git(r.t, r.dir, "push", "--quiet", "origin", "HEAD:refs/heads/main")
}

// pull brings the working clone up to date with what over has pushed.
func (r *repo) pull() {
	r.t.Helper()
	r.clone()
	git(r.t, r.dir, "fetch", "--quiet", "origin")
	git(r.t, r.dir, "reset", "--quiet", "--hard", "origin/main")
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

func mkdir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	mkdir(t, filepath.Dir(path))
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// osRemove is os.Remove, wrapped so that the test files above read
// without importing os for a single call.
func osRemove(path string) error { return os.Remove(path) }

// readFile is os.ReadFile returning a string.
func readFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	return string(data), err
}

// symlink and readlink are os.Symlink and os.Readlink, wrapped so that
// the test files above read without importing os for a single call.
func symlink(target, path string) error { return os.Symlink(target, path) }

func readlink(path string) (string, error) { return os.Readlink(path) }
