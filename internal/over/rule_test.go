package over

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/mariusae/over/internal/pathspec"
)

func TestParseRule(t *testing.T) {
	root := pathspec.Resolve(t.TempDir())
	for _, rule := range []string{".apex/...", ".vimrc", ".config/.../init.lua"} {
		if _, err := ParseRule(root, rule); err != nil {
			t.Errorf("ParseRule(%q): %v", rule, err)
		}
	}
	// A rule has to mean the same thing on every machine that adds the
	// layer, so it is relative to the root and stays under it.
	for _, rule := range []string{"", "...", "/etc/hosts", "../outside/..."} {
		if _, err := ParseRule(root, rule); err == nil {
			t.Errorf("ParseRule(%q) succeeded, want an error", rule)
		}
	}
}

func TestClaims(t *testing.T) {
	root := pathspec.Resolve(t.TempDir())
	at := func(rel string) string { return filepath.Join(root, rel) }
	mustParse := func(rules ...string) []pathspec.Pattern {
		t.Helper()
		pats, err := ParseRules(root, rules)
		if err != nil {
			t.Fatal(err)
		}
		return pats
	}
	excl, err := pathspec.ParseAll(root, []string{at(".apex/private/...")})
	if err != nil {
		t.Fatal(err)
	}
	l := &Layer{
		Root:    root,
		Track:   mustParse(".apex/...", ".vimrc"),
		Ignore:  mustParse(".apex/cache/..."),
		Exclude: excl,
	}
	for _, test := range []struct {
		path  string
		claim bool
	}{
		{at(".apex/attach"), true},
		{at(".apex/deep/down/here"), true},
		{at(".vimrc"), true},
		{at(".zshrc"), false},            // no rule matches
		{at(".apex/cache/blob"), false},  // an ignore rule carves it out
		{at(".apex/private/key"), false}, // the client's exclusions outrank the layer
	} {
		if got := l.Claims(test.path); got != test.claim {
			t.Errorf("Claims(%q) = %v, want %v", test.path, got, test.claim)
		}
	}
}

// TestWalkRoots checks that overlapping rules are walked once, from the
// outermost anchor.
func TestWalkRoots(t *testing.T) {
	root := pathspec.Resolve(t.TempDir())
	pats, err := ParseRules(root, []string{".config/ion/...", ".config/...", ".local/share/..."})
	if err != nil {
		t.Fatal(err)
	}
	roots := walkRoots(pats)
	want := []string{filepath.Join(root, ".config"), filepath.Join(root, ".local/share")}
	if len(roots) != len(want) {
		t.Fatalf("walkRoots = %v, want %v", roots, want)
	}
	for i := range want {
		if roots[i] != want[i] {
			t.Fatalf("walkRoots = %v, want %v", roots, want)
		}
	}
}

// TestRelativeRule checks that a path argument is stored as a rule
// relative to the layer's root, wildcards intact.
func TestRelativeRule(t *testing.T) {
	root := pathspec.Resolve(t.TempDir())
	l := &Layer{Root: root}
	pat, err := pathspec.Parse(root, ".apex/...")
	if err != nil {
		t.Fatal(err)
	}
	rule, err := RelativeRule(l, pat)
	if err != nil {
		t.Fatal(err)
	}
	if rule != ".apex/..." {
		t.Errorf("RelativeRule = %q", rule)
	}
	outside, err := pathspec.Parse(filepath.Dir(root), "elsewhere/...")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := RelativeRule(l, outside); err == nil || !strings.Contains(err.Error(), "outside") {
		t.Errorf("RelativeRule of a path outside the root = %v", err)
	}
}
