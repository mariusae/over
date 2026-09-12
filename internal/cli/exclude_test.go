package cli

import (
	"strings"
	"testing"
)

// TestExcludeIsSeeded checks that a new configuration comes with over's
// own directories excluded, written against $HOME.
func TestExcludeIsSeeded(t *testing.T) {
	c, _ := newLayer(t)
	out := c.mustOver("exclude")
	for _, want := range []string{"$HOME/.config/over/...", "$HOME/.cache/over/..."} {
		if !strings.Contains(out, want) {
			t.Errorf("exclude output missing %q:\n%s", want, out)
		}
	}
	// The seed is written to the configuration, not just imputed, so
	// that it is there to be seen and edited.
	if got := c.read(".config/over/config.yaml"); !strings.Contains(got, "$HOME/.config/over/...") {
		t.Errorf("config.yaml does not record the exclusions:\n%s", got)
	}
}

// TestExcludeStopsManagement checks that an excluded path is skipped by
// every layer, on every sync.
func TestExcludeStopsManagement(t *testing.T) {
	c, r := newLayer(t)
	c.mustOver("sync")

	if out := c.mustOver("exclude", "$HOME/.zshrc"); !strings.Contains(out, "excluded $HOME/.zshrc") {
		t.Errorf("exclude = %q", out)
	}
	// The layer moves ahead, and over does not follow.
	r.write("editors/.zshrc", "remote\n")
	r.commit("remote edit")
	out := c.mustOver("sync")
	if strings.Contains(out, ".zshrc") {
		t.Errorf("sync acted on an excluded path:\n%s", out)
	}
	if got := c.read(".zshrc"); got != "set -o vi\n" {
		t.Errorf(".zshrc = %q, want it left alone", got)
	}

	// Removing the exclusion brings it back under management.
	if out := c.mustOver("exclude", "-rm", "$HOME/.zshrc"); !strings.Contains(out, "no longer excluded") {
		t.Errorf("exclude -rm = %q", out)
	}
	if out := c.mustOver("sync"); !strings.Contains(out, ".zshrc from mariusae/config:editors") {
		t.Errorf("sync after -rm = %q", out)
	}
	if got := c.read(".zshrc"); got != "remote\n" {
		t.Errorf(".zshrc = %q", got)
	}
}

// TestExcludeIsNotUntrack pins down the difference between the two.
// Untracking forgets a file once, and the next sync is free to pick it
// up again; excluding it is a standing rule that no sync looks past.
func TestExcludeIsNotUntrack(t *testing.T) {
	c, _ := newLayer(t)
	c.mustOver("sync")
	c.mustOver("untrack", ".emacs")
	c.write(".emacs", "local\n")

	// Forgotten, but still provided by the layer: the next sync meets a
	// file it has no record of and stops.
	code, out, _ := c.over("sync")
	if code != exitError || !strings.Contains(out, ".emacs conflicts") {
		t.Fatalf("sync after untrack: exit %d, out %q", code, out)
	}

	// Excluded, it is not over's business at all.
	c.mustOver("exclude", "$HOME/.emacs")
	out = c.mustOver("sync")
	if strings.Contains(out, ".emacs") {
		t.Errorf("sync acted on an excluded path:\n%s", out)
	}
	if got := c.read(".emacs"); got != "local\n" {
		t.Errorf(".emacs = %q, want it left alone", got)
	}
}

// TestExcludePatterns checks that exclusions take the same "..." form as
// the path arguments elsewhere.
func TestExcludePatterns(t *testing.T) {
	c := newClient(t)
	r := c.newRepo("mariusae", "config")
	r.write("editors/.config/ion/config", "ion\n")
	r.write("editors/.config/nvim/init.lua", "nvim\n")
	r.commit("init")
	c.mustOver("add", "mariusae/config:editors")
	c.mustOver("exclude", "$HOME/.config/nvim/...")

	c.mustOver("sync")
	if !c.exists(".config/ion/config") {
		t.Error("the unexcluded file was not synced")
	}
	if c.exists(".config/nvim/init.lua") {
		t.Error("the excluded file was synced")
	}
}

func TestExcludeErrors(t *testing.T) {
	c, _ := newLayer(t)
	code, _, stderr := c.over("exclude", "relative/path")
	if code != exitError || !strings.Contains(stderr, "not an absolute path") {
		t.Errorf("relative exclusion: exit %d, stderr %q", code, stderr)
	}
	code, _, stderr = c.over("exclude", "$OVER_NO_SUCH_VAR/x")
	if code != exitError || !strings.Contains(stderr, "undefined environment variable") {
		t.Errorf("undefined variable: exit %d, stderr %q", code, stderr)
	}
	code, _, stderr = c.over("exclude", "-rm", "/nowhere")
	if code != exitError || !strings.Contains(stderr, "not an exclusion") {
		t.Errorf("removing a non-exclusion: exit %d, stderr %q", code, stderr)
	}
	// Adding the same exclusion twice is not an error.
	c.mustOver("exclude", "$HOME/.zshrc")
	if out := c.mustOver("exclude", "$HOME/.zshrc"); !strings.Contains(out, "already excluded") {
		t.Errorf("second add = %q", out)
	}
}

// TestExcludeNone checks that an explicitly empty list is left alone
// rather than reseeded.
func TestExcludeNone(t *testing.T) {
	c, _ := newLayer(t)
	c.mustOver("exclude", "-rm", "$HOME/.config/over/...", "$HOME/.cache/over/...")
	if out := c.mustOver("exclude"); !strings.Contains(out, "no exclusions") {
		t.Errorf("exclude = %q", out)
	}
	// Reloading does not bring the seed back.
	if out := c.mustOver("exclude"); strings.Contains(out, ".config/over") {
		t.Errorf("the seed was restored: %q", out)
	}
}
