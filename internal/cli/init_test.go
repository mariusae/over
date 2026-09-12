package cli

import (
	"strings"
	"testing"
)

// TestInitEmptyRepository is the case over has to handle to be usable at
// all: a repository just created and never pushed to, with no commits
// and no branch. Init makes the first commit in it, and the layer is
// then ready to take files.
func TestInitEmptyRepository(t *testing.T) {
	c := newClient(t)
	r := c.newBareRepo("mariusae", "dotfiles")

	out := c.mustOver("init", "mariusae/dotfiles:editors")
	for _, want := range []string{
		"created mariusae/dotfiles:editors (root $HOME)",
		"added mariusae/dotfiles:editors",
		"over track mariusae/dotfiles:editors",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("init output missing %q:\n%s", want, out)
		}
	}

	// The declaration is in the repository, not just locally.
	r.pull()
	if got := r.read("config.yaml"); !strings.Contains(got, "editors") || !strings.Contains(got, "$HOME") {
		t.Errorf("config.yaml = %q", got)
	}

	// And the layer works: track a file, sync, and it is published.
	c.write(".zshrc", "set -o vi\n")
	c.mustOver("track", "mariusae/dotfiles:editors", ".zshrc")
	if out := c.mustOver("sync"); !strings.Contains(out, ".zshrc to mariusae/dotfiles:editors") {
		t.Errorf("sync = %q", out)
	}
	r.pull()
	if got := r.read("editors/.zshrc"); got != "set -o vi\n" {
		t.Errorf("layer copy = %q", got)
	}
}

// TestInitRoot checks that -root sets the root the layer declares for
// itself, which is what every machine adding it will use.
func TestInitRoot(t *testing.T) {
	c := newClient(t)
	r := c.newBareRepo("mariusae", "dotfiles")
	etc := c.path("etc")
	mkdir(t, etc)
	t.Setenv("OVERTESTETC", etc)

	c.mustOver("init", "-root", "$OVERTESTETC", "mariusae/dotfiles:etc")
	r.pull()
	if got := r.read("config.yaml"); !strings.Contains(got, "$OVERTESTETC") {
		t.Errorf("config.yaml = %q, want the root as written, not expanded", got)
	}

	c.write("etc/hosts", "hosts\n")
	c.mustOver("track", "mariusae/dotfiles:etc", "etc/hosts")
	c.mustOver("sync")
	r.pull()
	if got := r.read("etc/hosts"); got != "hosts\n" {
		t.Errorf("layer copy = %q", got)
	}
}

// TestInitPreservesTheFile checks that init edits config.yaml in place:
// a layer repository's configuration is written by hand too, and over
// does not get to discard what it does not understand.
func TestInitPreservesTheFile(t *testing.T) {
	c := newClient(t)
	r := c.newRepo("mariusae", "dotfiles")
	r.write("config.yaml", `# Layers provided by this repository.
layers:
    editors:
        root: $HOME
sets:
    mac:
        - editors
# A key over does not know about.
future: yes
`)
	r.write("editors/.emacs", "emacs\n")
	r.commit("init")

	c.mustOver("init", "-root", "/etc", "mariusae/dotfiles:etc")
	r.pull()
	got := r.read("config.yaml")
	for _, want := range []string{
		"# Layers provided by this repository.",
		"# A key over does not know about.",
		"future: yes",
		"mac:",
		"etc:",
		"root: /etc",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("config.yaml lost %q:\n%s", want, got)
		}
	}
}

// TestInitMultipleLayers checks that layers sharing a repository are
// created together.
func TestInitMultipleLayers(t *testing.T) {
	c := newClient(t)
	r := c.newBareRepo("mariusae", "dotfiles")

	c.mustOver("init", "mariusae/dotfiles:editors", "mariusae/dotfiles:shell")
	r.pull()
	got := r.read("config.yaml")
	if !strings.Contains(got, "editors") || !strings.Contains(got, "shell") {
		t.Errorf("config.yaml = %q", got)
	}
	// One commit for both.
	if n := len(strings.Split(strings.TrimSpace(gitLog(t, r)), "\n")); n != 1 {
		t.Errorf("got %d commits, want 1:\n%s", n, gitLog(t, r))
	}
	if out := c.mustOver("status"); !strings.Contains(out, ":editors") || !strings.Contains(out, ":shell") {
		t.Errorf("both layers were not added:\n%s", out)
	}
}

func TestInitNoAdd(t *testing.T) {
	c := newClient(t)
	c.newBareRepo("mariusae", "dotfiles")

	out := c.mustOver("init", "-no-add", "mariusae/dotfiles:editors")
	if strings.Contains(out, "added") {
		t.Errorf("init -no-add added the layer: %q", out)
	}
	// It exists in the repository, so adding it afterwards works.
	if out := c.mustOver("add", "mariusae/dotfiles:editors"); !strings.Contains(out, "added") {
		t.Errorf("add after init -no-add = %q", out)
	}
}

func TestInitErrors(t *testing.T) {
	c := newClient(t)
	r := c.newRepo("mariusae", "dotfiles")
	r.write("config.yaml", "layers:\n    editors:\n        root: $HOME\nsets:\n    mac:\n        - editors\n")
	r.write("editors/.emacs", "emacs\n")
	r.commit("init")

	code, _, stderr := c.over("init", "mariusae/dotfiles:editors")
	if code != exitError || !strings.Contains(stderr, "already exists") {
		t.Errorf("existing layer: exit %d, stderr %q", code, stderr)
	}
	code, _, stderr = c.over("init", "mariusae/dotfiles:mac")
	if code != exitError || !strings.Contains(stderr, "is a set") {
		t.Errorf("set name: exit %d, stderr %q", code, stderr)
	}
	code, _, stderr = c.over("init", "mariusae/dotfiles")
	if code != exitError || !strings.Contains(stderr, "name the layer to create") {
		t.Errorf("no layer name: exit %d, stderr %q", code, stderr)
	}
	code, _, stderr = c.over("init", "-root", "relative", "mariusae/dotfiles:new")
	if code != exitError || !strings.Contains(stderr, "not an absolute path") {
		t.Errorf("relative root: exit %d, stderr %q", code, stderr)
	}
	if code, _, _ := c.over("init"); code != exitUsage {
		t.Errorf("no arguments: exit %d, want %d", code, exitUsage)
	}
}

// TestAddSuggestsInit checks that the way to make a missing layer is
// pointed at from where you notice it is missing.
func TestAddSuggestsInit(t *testing.T) {
	c := newClient(t)
	c.newBareRepo("mariusae", "dotfiles")

	code, _, stderr := c.over("add", "mariusae/dotfiles:editors")
	if code != exitError || !strings.Contains(stderr, "over init mariusae/dotfiles:editors") {
		t.Errorf("exit %d, stderr %q", code, stderr)
	}
}

// gitLog returns the one-line log of a repository's working clone.
func gitLog(t *testing.T, r *repo) string {
	t.Helper()
	return git(t, r.dir, "log", "--oneline")
}
