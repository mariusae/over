package cli

import (
	"path/filepath"
	"strings"
	"testing"
)

// newMultiLayer sets up a repository providing three layers and the set
// that groups two of them.
func newMultiLayer(t *testing.T) (*client, *repo) {
	t.Helper()
	c := newClient(t)
	r := c.newRepo("mariusae", "config")
	r.write("editors/.emacs", "editors\n")
	r.write("shell/.zshrc", "shell\n")
	r.write("etc/hosts", "hosts\n")
	r.write("config.yaml", "layers:\n  etc:\n    root: $OVERTESTETC\nsets:\n  mac:\n    - editors\n    - shell\n")
	r.commit("init")
	return c, r
}

func TestAddSet(t *testing.T) {
	c, _ := newMultiLayer(t)
	out := c.mustOver("add", "mariusae/config::mac")
	for _, want := range []string{"added mariusae/config:editors", "added mariusae/config:shell"} {
		if !strings.Contains(out, want) {
			t.Errorf("add output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, ":etc") {
		t.Errorf("the set's non-members were added too:\n%s", out)
	}
}

func TestAddWholeRepository(t *testing.T) {
	c, _ := newMultiLayer(t)
	t.Setenv("OVERTESTETC", filepath.Join(c.root, "etc"))
	out := c.mustOver("add", "mariusae/config")
	for _, want := range []string{":editors", ":etc", ":shell"} {
		if !strings.Contains(out, want) {
			t.Errorf("add output missing %q:\n%s", want, out)
		}
	}
}

func TestAddUnknownLayer(t *testing.T) {
	c, _ := newMultiLayer(t)
	code, _, stderr := c.over("add", "mariusae/config:nosuchlayer")
	if code != exitError {
		t.Errorf("exit %d, want %d", code, exitError)
	}
	if !strings.Contains(stderr, "no such layer") {
		t.Errorf("stderr = %q", stderr)
	}
}

func TestAddBefore(t *testing.T) {
	c, _ := newMultiLayer(t)
	c.mustOver("add", "mariusae/config:shell")
	c.mustOver("add", "-before", "mariusae/config:shell", "mariusae/config:editors")
	out := c.mustOver("status")
	editors := strings.Index(out, "mariusae/config:editors")
	shell := strings.Index(out, "mariusae/config:shell")
	if editors < 0 || shell < 0 || editors > shell {
		t.Errorf("editors was not inserted before shell:\n%s", out)
	}
}

func TestAddIsIdempotent(t *testing.T) {
	c, _ := newMultiLayer(t)
	c.mustOver("add", "mariusae/config:shell")
	if out := c.mustOver("add", "mariusae/config:shell"); !strings.Contains(out, "already configured") {
		t.Errorf("second add = %q", out)
	}
}

func TestRm(t *testing.T) {
	c, _ := newMultiLayer(t)
	c.mustOver("add", "mariusae/config:shell")
	c.mustOver("sync")
	if out := c.mustOver("rm", "mariusae/config:shell"); !strings.Contains(out, "removed mariusae/config:shell") {
		t.Errorf("rm = %q", out)
	}
	if !c.exists(".zshrc") {
		t.Error("rm removed the local file")
	}
	code, _, stderr := c.over("status")
	if code != exitError || !strings.Contains(stderr, "no layers configured") {
		t.Errorf("status after rm: exit %d, stderr %q", code, stderr)
	}
}

// TestRootFromRepository checks that a layer is materialized under the
// root its repository declares, with environment variables expanded.
func TestRootFromRepository(t *testing.T) {
	c, _ := newMultiLayer(t)
	etc := filepath.Join(c.root, "etc")
	mkdir(t, etc)
	t.Setenv("OVERTESTETC", etc)

	c.mustOver("add", "mariusae/config:etc")
	c.mustOver("sync")
	if c.exists("hosts") {
		t.Error("the layer was materialized under $HOME, not its own root")
	}
	data, err := readFile(filepath.Join(etc, "hosts"))
	if err != nil || data != "hosts\n" {
		t.Errorf("%s/hosts = %q, %v", etc, data, err)
	}
}

// TestRootOverride checks the -root flag and the root command.
func TestRootOverride(t *testing.T) {
	c, _ := newMultiLayer(t)
	alt := filepath.Join(c.root, "alt")
	mkdir(t, alt)

	c.mustOver("add", "-root", alt, "mariusae/config:editors")
	if out := c.mustOver("root", "mariusae/config:editors"); strings.TrimSpace(out) != alt {
		t.Errorf("root = %q, want %q", out, alt)
	}
	c.mustOver("sync")
	if data, err := readFile(filepath.Join(alt, ".emacs")); err != nil || data != "editors\n" {
		t.Errorf("%s/.emacs = %q, %v", alt, data, err)
	}

	// Clearing the override restores the layer's default root.
	c.mustOver("root", "mariusae/config:editors", "")
	if out := c.mustOver("root", "mariusae/config:editors"); !strings.Contains(out, "(default)") {
		t.Errorf("root after clearing = %q", out)
	}
}

// TestLastLayerWins checks the precedence rule: when two layers provide
// the same file, the later one owns it and the earlier one is shadowed.
func TestLastLayerWins(t *testing.T) {
	c := newClient(t)
	base := c.newRepo("mariusae", "config")
	base.write("editors/.emacs", "base\n")
	base.commit("init")
	work := c.newRepo("mariusae", "work")
	work.write("overrides/.emacs", "override\n")
	work.commit("init")

	c.mustOver("add", "mariusae/config:editors")
	c.mustOver("sync")
	if got := c.read(".emacs"); got != "base\n" {
		t.Fatalf(".emacs = %q", got)
	}

	c.mustOver("add", "mariusae/work:overrides")
	// The local file is the base layer's, which the new layer has never
	// synced, so over stops rather than overwrite it.
	if code, out, _ := c.over("sync"); code != exitError || !strings.Contains(out, "conflicts") {
		t.Fatalf("sync: exit %d, out %q", code, out)
	}
	c.mustOver("reset", ".emacs")
	if got := c.read(".emacs"); got != "override\n" {
		t.Errorf("after reset, .emacs = %q, want the last layer's copy", got)
	}

	out := c.mustOver("status")
	if !strings.Contains(out, ".emacs shadowed in mariusae/config:editors") {
		t.Errorf("status does not report the shadowed file:\n%s", out)
	}
	// The shadowed layer is not written to: its copy stands unchanged.
	c.mustOver("sync")
	base.pull()
	if got := base.read("editors/.emacs"); got != "base\n" {
		t.Errorf("the shadowed layer was written to: %q", got)
	}
}

// TestPathArguments checks that commands restrict themselves to the
// files named, including recursively with "...".
func TestPathArguments(t *testing.T) {
	c := newClient(t)
	r := c.newRepo("mariusae", "config")
	r.write("editors/.emacs", "emacs\n")
	r.write("editors/.config/ion/config", "ion\n")
	r.write("editors/.config/nvim/init.lua", "nvim\n")
	r.commit("init")
	c.mustOver("add", "mariusae/config:editors")
	c.mustOver("sync")

	c.write(".emacs", "edited\n")
	c.write(".config/ion/config", "edited\n")
	c.write(".config/nvim/init.lua", "edited\n")

	c.mustOver("reset", ".config/...")
	if got := c.read(".config/ion/config"); got != "ion\n" {
		t.Errorf(".config/ion/config = %q, want it reset", got)
	}
	if got := c.read(".config/nvim/init.lua"); got != "nvim\n" {
		t.Errorf(".config/nvim/init.lua = %q, want it reset", got)
	}
	if got := c.read(".emacs"); got != "edited\n" {
		t.Errorf(".emacs = %q, want it left alone", got)
	}

	code, _, stderr := c.over("reset", ".nosuchfile")
	if code != exitError || !strings.Contains(stderr, "no tracked files match") {
		t.Errorf("reset of an untracked path: exit %d, stderr %q", code, stderr)
	}
}

// TestStatusDoesNotFetch checks that status reports from over's cache,
// leaving the network to sync.
func TestStatusDoesNotFetch(t *testing.T) {
	c, r := newLayer(t)
	c.mustOver("sync")
	r.write("editors/.emacs", "remote\n")
	r.commit("remote edit")

	if out := c.mustOver("status"); !strings.Contains(out, "2 unchanged") {
		t.Errorf("status saw the unfetched change:\n%s", out)
	}
	if out := c.mustOver("sync", "-n"); !strings.Contains(out, ".emacs from") {
		t.Errorf("sync -n did not fetch:\n%s", out)
	}
	// -n changed nothing, so the same work is still pending.
	if got := c.read(".emacs"); got != "(setq inhibit-startup-message t)\n" {
		t.Errorf("sync -n wrote the file: %q", got)
	}
	if out := c.mustOver("sync"); !strings.Contains(out, "1 updated") {
		t.Errorf("sync = %q", out)
	}
}

// TestStatusAll checks the full inventory: every file over is looking
// after, not only the ones a sync would act on.
func TestStatusAll(t *testing.T) {
	c := newClient(t)
	r := c.newRepo("mariusae", "config")
	r.write("editors/.emacs", "emacs\n")
	r.write("editors/.zshrc", "zsh\n")
	r.write("editors/.config/ion/config", "ion\n")
	r.commit("init")
	c.mustOver("add", "mariusae/config:editors")
	c.mustOver("sync")
	c.write(".zshrc", "edited\n")

	// Without -a, only the file with something to do.
	plain := c.mustOver("status")
	if !strings.Contains(plain, ".zshrc to mariusae/config:editors") {
		t.Errorf("status = %q", plain)
	}
	for _, quiet := range []string{".emacs", ".config/ion/config"} {
		if strings.Contains(plain, quiet) {
			t.Errorf("status named %q, which nothing is happening to:\n%s", quiet, plain)
		}
	}

	// With it, everything.
	all := c.mustOver("status", "-a")
	for _, want := range []string{
		".emacs unchanged in mariusae/config:editors",
		".config/ion/config unchanged in mariusae/config:editors",
		".zshrc to mariusae/config:editors",
	} {
		if !strings.Contains(all, want) {
			t.Errorf("status -a missing %q:\n%s", want, all)
		}
	}
	// The summary is the same either way; -a changes what is listed,
	// not what is counted.
	if !strings.Contains(plain, "1 written, 2 unchanged") || !strings.Contains(all, "1 written, 2 unchanged") {
		t.Errorf("summaries differ:\n%s\n%s", plain, all)
	}
}

// TestStatusAllListsEachFileOnce checks that a file held by two layers is
// listed under the one that owns it, rather than once per layer.
func TestStatusAllListsEachFileOnce(t *testing.T) {
	c := newClient(t)
	base := c.newRepo("mariusae", "config")
	base.write("editors/.emacs", "same\n")
	base.commit("init")
	work := c.newRepo("mariusae", "work")
	work.write("overrides/.emacs", "same\n")
	work.commit("init")
	c.mustOver("add", "mariusae/config:editors")
	c.mustOver("add", "mariusae/work:overrides")
	c.mustOver("sync")

	out := c.mustOver("status", "-a")
	if n := strings.Count(out, ".emacs unchanged"); n != 1 {
		t.Errorf(".emacs listed %d times, want once:\n%s", n, out)
	}
	if !strings.Contains(out, ".emacs unchanged in mariusae/work:overrides") {
		t.Errorf("the owning layer does not speak for the file:\n%s", out)
	}
}

// TestStatusAllTakesPaths checks that -a is still bounded by the path
// arguments.
func TestStatusAllTakesPaths(t *testing.T) {
	c := newClient(t)
	r := c.newRepo("mariusae", "config")
	r.write("editors/.emacs", "emacs\n")
	r.write("editors/.config/ion/config", "ion\n")
	r.commit("init")
	c.mustOver("add", "mariusae/config:editors")
	c.mustOver("sync")

	out := c.mustOver("status", "-a", ".config/...")
	if !strings.Contains(out, ".config/ion/config unchanged") {
		t.Errorf("status -a .config/... = %q", out)
	}
	if strings.Contains(out, ".emacs unchanged") {
		t.Errorf("status -a ignored its path argument:\n%s", out)
	}
}

// TestStatusAllOmitsWhatIsNotOurs checks that a tombstoned path with an
// untracked local file is not passed off as tracked.
func TestStatusAllOmitsWhatIsNotOurs(t *testing.T) {
	c, r := newLayer(t)
	c.mustOver("sync")
	if err := removeFile(c.path(".zshrc")); err != nil {
		t.Fatal(err)
	}
	c.mustOver("sync") // tombstones it in the layer
	r.pull()
	if !strings.Contains(r.read("editors.tombstones.yaml"), ".zshrc") {
		t.Fatal("the file was not tombstoned")
	}
	// A local file reappears, but over has no claim on it.
	c.mustOver("untrack", ".zshrc")
	c.write(".zshrc", "mine now\n")

	out := c.mustOver("status", "-a")
	if strings.Contains(out, ".zshrc") {
		t.Errorf("status -a claimed a file that is not over's:\n%s", out)
	}
}
