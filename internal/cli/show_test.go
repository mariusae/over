package cli

import (
	"strings"
	"testing"
)

func TestShow(t *testing.T) {
	c, _ := newLayer(t)
	c.mustOver("sync")

	out := c.mustOver("show", ".emacs")
	for _, want := range []string{
		"    path",
		c.path(".emacs"),
		"mariusae/config:editors (owner)",
		"editors/.emacs", // the file's place in the repository
		"init",           // the subject of the commit that last touched it
	} {
		if !strings.Contains(out, want) {
			t.Errorf("show output missing %q:\n%s", want, out)
		}
	}
	for _, want := range []string{"status unchanged", "owner mariusae/config:editors", "mode 644"} {
		if !strings.Contains(collapse(out), want) {
			t.Errorf("show output missing %q:\n%s", want, out)
		}
	}
}

// collapse squeezes runs of spaces within each line, so that assertions
// on show's output do not depend on the column widths the tabwriter
// happens to choose.
func collapse(s string) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = strings.Join(strings.Fields(line), " ")
	}
	return strings.Join(lines, "\n")
}

// TestShowReportsBothSidesOfAConflict checks that show is useful for the
// case it exists to explain.
func TestShowReportsBothSidesOfAConflict(t *testing.T) {
	c, r := newLayer(t)
	c.mustOver("sync")
	r.write("editors/.emacs", "remote\n")
	r.commit("remote edit")
	c.write(".emacs", "local\n")
	if code, _, _ := c.over("sync"); code != exitError {
		t.Fatalf("sync: exit %d, want a conflict", code)
	}

	out := c.mustOver("show", ".emacs")
	if !strings.Contains(collapse(out), "status conflict") {
		t.Errorf("show does not report the conflict:\n%s", out)
	}
	if !strings.Contains(out, "remote edit") {
		t.Errorf("show does not name the commit that changed the layer:\n%s", out)
	}
	// The local file, the layer's copy, and the recorded base are all
	// distinct, and all three are reported.
	for _, label := range []string{"local", "content", "synced"} {
		if !strings.Contains(out, label) {
			t.Errorf("show output missing the %q line:\n%s", label, out)
		}
	}
}

// TestShowShadowed checks that show names the layer a file comes from
// and the layers it is covering up.
func TestShowShadowed(t *testing.T) {
	c := newClient(t)
	base := c.newRepo("mariusae", "config")
	base.write("editors/.emacs", "base\n")
	base.commit("editors: initial")
	work := c.newRepo("mariusae", "work")
	work.write("overrides/.emacs", "override\n")
	work.commit("overrides: override emacs")

	c.mustOver("add", "mariusae/config:editors")
	c.mustOver("add", "mariusae/work:overrides")
	c.mustOver("sync")

	out := c.mustOver("show", ".emacs")
	owner := strings.Index(out, "mariusae/work:overrides (owner)")
	shadowed := strings.Index(out, "mariusae/config:editors (shadowed)")
	if owner < 0 || shadowed < 0 {
		t.Fatalf("show does not report both layers:\n%s", out)
	}
	if owner > shadowed {
		t.Errorf("the owning layer is not reported first:\n%s", out)
	}
	// Both layers' contents are shown, so it is plain what is covered.
	for _, want := range []string{"overrides/.emacs", "editors/.emacs", "editors: initial", "overrides: override emacs"} {
		if !strings.Contains(out, want) {
			t.Errorf("show output missing %q:\n%s", want, out)
		}
	}
}

// TestShowUnmanaged checks that a path over knows nothing about is
// reported, with the reason.
func TestShowUnmanaged(t *testing.T) {
	c, _ := newLayer(t)
	c.mustOver("sync")
	c.write(".vimrc", "set nocompatible\n")

	out := c.mustOver("show", ".vimrc")
	if !strings.Contains(out, "not managed") {
		t.Errorf("show = %q", out)
	}
	if !strings.Contains(out, "over track mariusae/config:editors .vimrc") {
		t.Errorf("show does not say how to track it:\n%s", out)
	}
	// The local file is still described.
	if !strings.Contains(out, "17 bytes") {
		t.Errorf("show does not describe the local file:\n%s", out)
	}
}

func TestShowExcluded(t *testing.T) {
	c, _ := newLayer(t)
	c.mustOver("sync")
	out := c.mustOver("show", ".config/over/config.yaml")
	if !strings.Contains(out, "not managed") {
		t.Errorf("show = %q", out)
	}
	if !strings.Contains(out, "excluded by $HOME/.config/over/...") {
		t.Errorf("show does not name the exclusion:\n%s", out)
	}
}

// TestShowOutsideEveryRoot checks the other reason a path goes
// unmanaged.
func TestShowOutsideEveryRoot(t *testing.T) {
	c, _ := newLayer(t)
	c.mustOver("sync")
	out := c.mustOver("show", "/")
	if !strings.Contains(out, "outside the root of every configured layer") {
		t.Errorf("show = %q", out)
	}
}

// TestShowTombstone checks that a deleted file reports its tombstone.
func TestShowTombstone(t *testing.T) {
	c, _ := newLayer(t)
	c.mustOver("sync")
	if err := removeFile(c.path(".zshrc")); err != nil {
		t.Fatal(err)
	}
	c.mustOver("sync")

	out := c.mustOver("show", ".zshrc")
	for _, want := range []string{"local (absent)", "content (deleted)", "tombstone"} {
		if !strings.Contains(collapse(out), want) {
			t.Errorf("show output missing %q:\n%s", want, out)
		}
	}
}

// TestShowPatterns checks that show takes the same path arguments as
// everything else, reporting on each file the pattern matches.
func TestShowPatterns(t *testing.T) {
	c := newClient(t)
	r := c.newRepo("mariusae", "config")
	r.write("editors/.emacs", "emacs\n")
	r.write("editors/.config/ion/config", "ion\n")
	r.write("editors/.config/nvim/init.lua", "nvim\n")
	r.commit("init")
	c.mustOver("add", "mariusae/config:editors")
	c.mustOver("sync")

	all := c.mustOver("show", "...")
	for _, want := range []string{".emacs", ".config/ion/config", ".config/nvim/init.lua"} {
		if !strings.Contains(all, c.path(want)) {
			t.Errorf("show ... missing %q:\n%s", want, all)
		}
	}

	under := c.mustOver("show", ".config/...")
	if strings.Contains(under, c.path(".emacs")) {
		t.Errorf("show .config/... reported a file outside .config:\n%s", under)
	}
	for _, want := range []string{".config/ion/config", ".config/nvim/init.lua"} {
		if !strings.Contains(under, c.path(want)) {
			t.Errorf("show .config/... missing %q:\n%s", want, under)
		}
	}
}

func TestShowRequiresAPath(t *testing.T) {
	c, _ := newLayer(t)
	code, _, stderr := c.over("show")
	if code != exitUsage {
		t.Errorf("exit %d, want %d", code, exitUsage)
	}
	if !strings.Contains(stderr, "expected at least one path") {
		t.Errorf("stderr = %q", stderr)
	}
}
