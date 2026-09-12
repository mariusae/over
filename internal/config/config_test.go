package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadMissing(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Layers) != 0 {
		t.Errorf("got %d layers, want none", len(cfg.Layers))
	}
}

func TestRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "over", "config.yaml")
	cfg := &Config{Layers: []Entry{
		{Layer: "mariusae/config:editors"},
		{Layer: "mariusae/ion:config", Root: "/Users/otheruser"},
	}}
	if err := cfg.Save(path); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Layers) != 2 || got.Layers[1].Root != "/Users/otheruser" {
		t.Errorf("round trip = %+v", got.Layers)
	}
	if i := got.Index("mariusae/ion:config"); i != 1 {
		t.Errorf("Index = %d, want 1", i)
	}
	if i := got.Index("nope"); i != -1 {
		t.Errorf("Index of an absent layer = %d, want -1", i)
	}
}

func TestLoadRepoDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	const text = `layers:
  editors:
    root: $HOME
  etc:
    root: /etc
sets:
  mac:
    - editors
    - defaults
`
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	r, err := LoadRepo(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := r.Root("etc"); got != "/etc" {
		t.Errorf("Root(etc) = %q", got)
	}
	// A layer with no key at all gets the default imputed.
	if got := r.Root("dotconfig"); got != "$HOME" {
		t.Errorf("Root(dotconfig) = %q, want $HOME", got)
	}
	if members, ok := r.Set("mac"); !ok || len(members) != 2 {
		t.Errorf("Set(mac) = %v, %v", members, ok)
	}
	if _, ok := r.Set("linux"); ok {
		t.Error("Set(linux) exists")
	}

	// LayerNames unions the declared layers with the directories on
	// disk.
	for _, name := range []string{"editors", "dotconfig", ".git"} {
		if err := os.Mkdir(filepath.Join(dir, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	names, err := r.LayerNames(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"dotconfig", "editors", "etc"}
	if len(names) != len(want) {
		t.Fatalf("LayerNames = %v, want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("LayerNames = %v, want %v", names, want)
		}
	}
}

func TestExpandRoot(t *testing.T) {
	t.Setenv("OVERTESTROOT", "/tmp")
	for _, test := range []struct {
		in      string
		want    string
		wantErr bool
	}{
		{in: "", want: os.Getenv("HOME")},
		{in: "$OVERTESTROOT", want: "/tmp"},
		{in: "$OVERTESTROOT/sub", want: "/tmp/sub"},
		{in: "$OVERTESTNOSUCHVAR", wantErr: true},
		{in: "relative/path", wantErr: true},
	} {
		got, err := ExpandRoot(test.in)
		if test.wantErr {
			if err == nil {
				t.Errorf("ExpandRoot(%q) = %q, want an error", test.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("ExpandRoot(%q): %v", test.in, err)
			continue
		}
		// The result is canonical, so compare canonically.
		if want, _ := ExpandRoot(test.want); got != want {
			t.Errorf("ExpandRoot(%q) = %q, want %q", test.in, got, want)
		}
	}
}

func TestTombstones(t *testing.T) {
	path := filepath.Join(t.TempDir(), "editors.tombstones.yaml")
	tombs, err := LoadTombstones(path)
	if err != nil {
		t.Fatal(err)
	}
	if tombs.Has(".zshrc") {
		t.Error("empty tombstones has an entry")
	}
	tombs.Add(".zshrc", time.Now())
	if err := tombs.Save(path); err != nil {
		t.Fatal(err)
	}
	again, err := LoadTombstones(path)
	if err != nil {
		t.Fatal(err)
	}
	if !again.Has(".zshrc") {
		t.Error("tombstone did not survive the round trip")
	}

	// A layer with nothing deleted carries no tombstone file.
	again.Remove(".zshrc")
	if err := again.Save(path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("empty tombstone file was not removed: %v", err)
	}
}
