package cli

import (
	"strings"
	"testing"
)

// newSetRepos sets up two repositories: one providing three layers and
// the sets that group them, and another providing one layer for a set to
// reach across to.
func newSetRepos(t *testing.T) (*client, *repo, *repo) {
	t.Helper()
	c := newClient(t)
	env := c.newRepo("mariusae", "env")
	env.write("editors/.emacs", "editors\n")
	env.write("shell/.zshrc", "shell\n")
	env.write("mac/.macrc", "mac layer\n")
	env.write("config.yaml", strings.Join([]string{
		"sets:",
		"    base:",
		"        - editors",
		"        - shell",
		"    mac:",
		"        - ::base",
		"        - mariusae/work:overrides",
		"",
	}, "\n"))
	env.commit("init")

	work := c.newRepo("mariusae", "work")
	work.write("overrides/.emacs", "overrides\n")
	work.commit("init")
	return c, env, work
}

// TestSetAndLayerSameName is the whole point of the doubled colon: the
// repository holds both a layer and a set called "mac", and which one is
// meant is said rather than guessed. Before the split the set won
// silently, and the directory could not be reached at all.
func TestSetAndLayerSameName(t *testing.T) {
	c, _, _ := newSetRepos(t)

	if out := c.mustOver("add", "mariusae/env:mac"); !strings.Contains(out, "added mariusae/env:mac") {
		t.Errorf("the layer was not added:\n%s", out)
	}
	if out := c.mustOver("status", "-a"); !strings.Contains(out, ".macrc") {
		t.Errorf("the layer's file is not there:\n%s", out)
	}

	c.mustOver("rm", "mariusae/env:mac")
	out := c.mustOver("add", "mariusae/env::mac")
	for _, want := range []string{"mariusae/env:editors", "mariusae/env:shell", "mariusae/work:overrides"} {
		if !strings.Contains(out, want) {
			t.Errorf("the set did not expand to %s:\n%s", want, out)
		}
	}
	if strings.Contains(out, "added mariusae/env:mac\n") {
		t.Errorf("the set took the layer of the same name with it:\n%s", out)
	}
}

// TestSetExpandsInOrder checks that a set's order is the precedence its
// members get, through a nested set and across repositories.
func TestSetExpandsInOrder(t *testing.T) {
	c, _, _ := newSetRepos(t)
	c.mustOver("add", "mariusae/env::mac")
	out := c.mustOver("status", "-a")
	var order []int
	for _, layer := range []string{"mariusae/env:editors", "mariusae/env:shell", "mariusae/work:overrides"} {
		i := strings.Index(c.read(".config/over/config.yaml"), layer)
		if i < 0 {
			t.Fatalf("%s is not configured:\n%s", layer, out)
		}
		order = append(order, i)
	}
	if !(order[0] < order[1] && order[1] < order[2]) {
		t.Errorf("members are out of order: %v\n%s", order, c.read(".config/over/config.yaml"))
	}
}

// TestSetSpellingSuggested checks that naming one kind when the other
// exists says how to spell what was probably meant.
func TestSetSpellingSuggested(t *testing.T) {
	c, _, _ := newSetRepos(t)

	code, _, stderr := c.over("add", "mariusae/env:base")
	if code != exitError || !strings.Contains(stderr, "mariusae/env::base") {
		t.Errorf("layer spelling of a set: exit %d, stderr %q", code, stderr)
	}
	code, _, stderr = c.over("add", "mariusae/env::editors")
	if code != exitError || !strings.Contains(stderr, "is a layer") {
		t.Errorf("set spelling of a layer: exit %d, stderr %q", code, stderr)
	}
	if !strings.Contains(stderr, "mariusae/env:editors") {
		t.Errorf("stderr does not spell the layer: %q", stderr)
	}
}

// TestSetDeduplicates checks that a layer two sets share is configured
// once, at its first position.
func TestSetDeduplicates(t *testing.T) {
	c, env, _ := newSetRepos(t)
	env.write("config.yaml", strings.Join([]string{
		"sets:",
		"    base:",
		"        - editors",
		"    both:",
		"        - ::base",
		"        - editors",
		"        - shell",
		"",
	}, "\n"))
	env.commit("one layer in two places")

	out := c.mustOver("add", "mariusae/env::both")
	if n := strings.Count(out, "added mariusae/env:editors"); n != 1 {
		t.Errorf("editors added %d times:\n%s", n, out)
	}
}

// TestSetCycle checks that a set reaching itself is an error rather than
// a hang, and says which sets were involved.
func TestSetCycle(t *testing.T) {
	c, env, _ := newSetRepos(t)
	env.write("config.yaml", strings.Join([]string{
		"sets:",
		"    a:",
		"        - ::b",
		"    b:",
		"        - ::a",
		"",
	}, "\n"))
	env.commit("a cycle")

	code, _, stderr := c.over("add", "mariusae/env::a")
	if code != exitError {
		t.Fatalf("exit %d, want %d", code, exitError)
	}
	for _, want := range []string{"refers to itself", "mariusae/env::a", "mariusae/env::b"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr missing %q: %q", want, stderr)
		}
	}
}

func TestSetEmpty(t *testing.T) {
	c, env, _ := newSetRepos(t)
	env.write("config.yaml", "sets:\n    nothing: []\n")
	env.commit("an empty set")
	code, _, stderr := c.over("add", "mariusae/env::nothing")
	if code != exitError || !strings.Contains(stderr, "set is empty") {
		t.Errorf("exit %d, stderr %q", code, stderr)
	}
}

func TestSetUnknown(t *testing.T) {
	c, _, _ := newSetRepos(t)
	code, _, stderr := c.over("add", "mariusae/env::nosuchset")
	if code != exitError || !strings.Contains(stderr, "no such set") {
		t.Errorf("exit %d, stderr %q", code, stderr)
	}
	if !strings.Contains(stderr, "base") || !strings.Contains(stderr, "mac") {
		t.Errorf("stderr does not list the sets there are: %q", stderr)
	}
}

func TestSetCommandShow(t *testing.T) {
	c, _, _ := newSetRepos(t)
	out := c.mustOver("set", "mariusae/env::mac")
	for _, want := range []string{"::base", "mariusae/work:overrides"} {
		if !strings.Contains(out, want) {
			t.Errorf("set output missing member %q:\n%s", want, out)
		}
	}
	// What it comes to is printed too, since a nested set does not say.
	for _, want := range []string{"mariusae/env:editors", "mariusae/env:shell"} {
		if !strings.Contains(out, want) {
			t.Errorf("set output missing layer %q:\n%s", want, out)
		}
	}
}

// TestSetCommandCreate checks that a set made on one machine is there for
// the next one, which is the reason it lives in the repository.
func TestSetCommandCreate(t *testing.T) {
	c, _, _ := newSetRepos(t)
	out := c.mustOver("set", "mariusae/env::minimal", "editors", "mariusae/work:overrides")
	for _, want := range []string{"editors added to mariusae/env::minimal", "mariusae/work:overrides added"} {
		if !strings.Contains(out, want) {
			t.Errorf("set output missing %q:\n%s", want, out)
		}
	}

	other := c.sub(t)
	got := other.mustOver("add", "mariusae/env::minimal")
	for _, want := range []string{"mariusae/env:editors", "mariusae/work:overrides"} {
		if !strings.Contains(got, want) {
			t.Errorf("the set did not reach the other machine (%q missing):\n%s", want, got)
		}
	}
}

func TestSetCommandAddIsIdempotent(t *testing.T) {
	c, _, _ := newSetRepos(t)
	if out := c.mustOver("set", "mariusae/env::base", "editors"); !strings.Contains(out, "already in") {
		t.Errorf("adding an existing member = %q", out)
	}
}

func TestSetCommandRemoveMember(t *testing.T) {
	c, _, _ := newSetRepos(t)
	out := c.mustOver("set", "-rm", "mariusae/env::base", "shell")
	if !strings.Contains(out, "shell dropped from mariusae/env::base") {
		t.Errorf("set -rm output = %q", out)
	}
	added := c.mustOver("add", "mariusae/env::base")
	if strings.Contains(added, ":shell") {
		t.Errorf("shell is still a member:\n%s", added)
	}
	code, _, stderr := c.over("set", "-rm", "mariusae/env::base", "shell")
	if code != exitError || !strings.Contains(stderr, "not a member") {
		t.Errorf("removing a non-member: exit %d, stderr %q", code, stderr)
	}
}

// TestSetCommandRemoveSet checks that removing a set leaves the layers it
// grouped alone: a set is a name for a group, not the group.
func TestSetCommandRemoveSet(t *testing.T) {
	c, _, _ := newSetRepos(t)
	if out := c.mustOver("set", "-rm", "mariusae/env::mac"); !strings.Contains(out, "removed mariusae/env::mac") {
		t.Errorf("set -rm output = %q", out)
	}
	// The layer of that name is all that is left, so that is what the
	// error points at rather than the list of sets.
	code, _, stderr := c.over("add", "mariusae/env::mac")
	if code != exitError || !strings.Contains(stderr, "is a layer in mariusae/env") {
		t.Errorf("the set survived: exit %d, stderr %q", code, stderr)
	}
	// The layer of the same name, and the set's members, are untouched.
	c.mustOver("add", "mariusae/env:mac")
	c.mustOver("add", "mariusae/env::base")
}

// TestSetCommandRejectsBadMember checks that a member that cannot resolve
// is refused before it is pushed to every machine that names the set.
func TestSetCommandRejectsBadMember(t *testing.T) {
	c, _, _ := newSetRepos(t)
	code, _, stderr := c.over("set", "mariusae/env::new", "nosuchlayer")
	if code != exitError || !strings.Contains(stderr, "no such layer") {
		t.Errorf("unknown layer member: exit %d, stderr %q", code, stderr)
	}
	code, _, stderr = c.over("set", "mariusae/env::new", "::nosuchset")
	if code != exitError || !strings.Contains(stderr, "no such set") {
		t.Errorf("unknown set member: exit %d, stderr %q", code, stderr)
	}
	code, _, stderr = c.over("set", "mariusae/env::new", "::new")
	if code != exitError || !strings.Contains(stderr, "member of itself") {
		t.Errorf("self-membership: exit %d, stderr %q", code, stderr)
	}
	// None of that created the set.
	if code, _, _ := c.over("set", "mariusae/env::new"); code != exitError {
		t.Error("a refused member created the set anyway")
	}
}

func TestSetCommandUsage(t *testing.T) {
	c, _, _ := newSetRepos(t)
	code, _, stderr := c.over("set", "mariusae/env:mac", "editors")
	if code != exitUsage || !strings.Contains(stderr, "mariusae/env::mac") {
		t.Errorf("layer spelling: exit %d, stderr %q", code, stderr)
	}
	if code, _, _ := c.over("set"); code != exitUsage {
		t.Errorf("no arguments: exit %d, want %d", code, exitUsage)
	}
}

// TestRmSet checks that a machine comes apart the way it went together:
// the argument that added an arrangement removes it.
func TestRmSet(t *testing.T) {
	c, _, _ := newSetRepos(t)
	c.mustOver("add", "mariusae/env::mac")
	c.mustOver("add", "mariusae/env:mac")

	out := c.mustOver("rm", "mariusae/env::mac")
	for _, want := range []string{"mariusae/env:editors", "mariusae/env:shell", "mariusae/work:overrides"} {
		if !strings.Contains(out, want) {
			t.Errorf("rm did not remove %s:\n%s", want, out)
		}
	}
	// The layer of the same name was not in the set, so it is still there.
	if cfg := c.read(".config/over/config.yaml"); !strings.Contains(cfg, "mariusae/env:mac") {
		t.Errorf("rm of the set took the layer of the same name:\n%s", cfg)
	}
	// A second rm has nothing left to take.
	code, _, stderr := c.over("rm", "mariusae/env::mac")
	if code != exitError || !strings.Contains(stderr, "none of its layers are configured") {
		t.Errorf("second rm: exit %d, stderr %q", code, stderr)
	}
}

// TestRmPartialSet checks that a set still removes the members that are
// there when the others have already gone.
func TestRmPartialSet(t *testing.T) {
	c, _, _ := newSetRepos(t)
	c.mustOver("add", "mariusae/env::base")
	c.mustOver("rm", "mariusae/env:shell")
	if out := c.mustOver("rm", "mariusae/env::base"); !strings.Contains(out, "mariusae/env:editors") {
		t.Errorf("rm of a partly-removed set = %q", out)
	}
}

// TestRmLayerNeedsNoNetwork checks that a single layer is matched against
// the configuration literally, so that one whose repository has gone away
// can still be removed.
func TestRmLayerNeedsNoNetwork(t *testing.T) {
	c, _, _ := newSetRepos(t)
	c.mustOver("add", "mariusae/env:editors")
	c.rmRemote("mariusae", "env")
	if out := c.mustOver("rm", "mariusae/env:editors"); !strings.Contains(out, "removed") {
		t.Errorf("rm of a layer whose repository is gone = %q", out)
	}
}
