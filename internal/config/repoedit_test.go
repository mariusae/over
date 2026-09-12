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
