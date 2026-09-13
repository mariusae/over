package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAddLayerNewFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := AddLayer(path, "editors", "$HOME"); err != nil {
		t.Fatal(err)
	}
	r, err := LoadRepo(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := r.Root("editors"); got != "$HOME" {
		t.Errorf("Root(editors) = %q", got)
	}
}

// TestAddLayerPreserves is the reason the edit is made on the parsed
// document rather than by re-marshalling a struct: the file is written
// by hand too.
func TestAddLayerPreserves(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	const original = `# Layers provided by this repository.
layers:
    editors:
        root: $HOME
sets:
    mac:
        - editors
        - shell
# Something a later version of over might add.
future: yes
`
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := AddLayer(path, "etc", "/etc"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	for _, want := range []string{
		"# Layers provided by this repository.",
		"# Something a later version of over might add.",
		"future: yes",
		"root: $HOME",
		"root: /etc",
		"- shell",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("AddLayer lost %q:\n%s", want, got)
		}
	}

	r, err := LoadRepo(path)
	if err != nil {
		t.Fatal(err)
	}
	if r.Root("etc") != "/etc" || r.Root("editors") != "$HOME" {
		t.Errorf("roots = %q, %q", r.Root("etc"), r.Root("editors"))
	}
	if members, ok := r.Set("mac"); !ok || len(members) != 2 {
		t.Errorf("Set(mac) = %v, %v", members, ok)
	}
}

func TestAddLayerErrors(t *testing.T) {
	dir := t.TempDir()

	path := filepath.Join(dir, "duplicate.yaml")
	if err := AddLayer(path, "editors", "$HOME"); err != nil {
		t.Fatal(err)
	}
	if err := AddLayer(path, "editors", "$HOME"); err == nil {
		t.Error("AddLayer of an existing layer succeeded")
	}

	notMapping := filepath.Join(dir, "list.yaml")
	if err := os.WriteFile(notMapping, []byte("- one\n- two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := AddLayer(notMapping, "editors", "$HOME"); err == nil {
		t.Error("AddLayer of a non-mapping document succeeded")
	}

	badLayers := filepath.Join(dir, "badlayers.yaml")
	if err := os.WriteFile(badLayers, []byte("layers: nope\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := AddLayer(badLayers, "editors", "$HOME"); err == nil {
		t.Error("AddLayer with a scalar layers key succeeded")
	}
}

// TestAddLayerEmptyFile checks a file that exists but holds nothing,
// which is what an editor leaves behind.
func TestAddLayerEmptyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("\n\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := AddLayer(path, "editors", "$HOME"); err != nil {
		t.Fatal(err)
	}
	r, err := LoadRepo(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := r.Root("editors"); got != "$HOME" {
		t.Errorf("Root(editors) = %q", got)
	}
}

func TestSetMembers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	added, err := AddSetMembers(path, "mac", []string{"editors", "shell", "mariusae/work:overrides"})
	if err != nil {
		t.Fatal(err)
	}
	if len(added) != 3 {
		t.Errorf("added = %v, want three members", added)
	}
	// A member already there is skipped rather than repeated.
	added, err = AddSetMembers(path, "mac", []string{"editors", "::base"})
	if err != nil {
		t.Fatal(err)
	}
	if len(added) != 1 || added[0] != "::base" {
		t.Errorf("added = %v, want just ::base", added)
	}
	r, err := LoadRepo(path)
	if err != nil {
		t.Fatal(err)
	}
	members, ok := r.Set("mac")
	if !ok {
		t.Fatal("set mac is not there")
	}
	want := []string{"editors", "shell", "mariusae/work:overrides", "::base"}
	if strings.Join(members, ",") != strings.Join(want, ",") {
		t.Errorf("members = %v, want %v", members, want)
	}
	if names := r.SetNames(); len(names) != 1 || names[0] != "mac" {
		t.Errorf("SetNames() = %v", names)
	}

	removed, err := RemoveSetMembers(path, "mac", []string{"shell", "nosuchmember"})
	if err != nil {
		t.Fatal(err)
	}
	if len(removed) != 1 || removed[0] != "shell" {
		t.Errorf("removed = %v, want just shell", removed)
	}
	if r, err = LoadRepo(path); err != nil {
		t.Fatal(err)
	}
	if members, _ := r.Set("mac"); len(members) != 3 {
		t.Errorf("members after removal = %v", members)
	}
}

func TestRemoveSet(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if _, err := AddSetMembers(path, "mac", []string{"editors"}); err != nil {
		t.Fatal(err)
	}
	if _, err := AddSetMembers(path, "linux", []string{"shell"}); err != nil {
		t.Fatal(err)
	}
	removed, err := RemoveSet(path, "mac")
	if err != nil {
		t.Fatal(err)
	}
	if !removed {
		t.Error("RemoveSet reported nothing removed")
	}
	r, err := LoadRepo(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := r.Set("mac"); ok {
		t.Error("set mac survived")
	}
	if _, ok := r.Set("linux"); !ok {
		t.Error("RemoveSet took the other set too")
	}
	if removed, err := RemoveSet(path, "mac"); err != nil || removed {
		t.Errorf("second RemoveSet = %v, %v", removed, err)
	}
}

// TestSetMembersPreserves checks that editing a set leaves the rest of a
// hand-written file alone, as editing a layer does.
func TestSetMembersPreserves(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	const original = `# What this repository provides.
layers:
    editors:
        root: $HOME
sets:
    # Everything a Mac wants.
    mac:
        - editors
future: yes
`
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := AddSetMembers(path, "mac", []string{"mariusae/work::linux"}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	for _, want := range []string{
		"# What this repository provides.",
		"# Everything a Mac wants.",
		"future: yes",
		"root: $HOME",
		"- editors",
		"- mariusae/work::linux",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("AddSetMembers lost %q:\n%s", want, got)
		}
	}
}
