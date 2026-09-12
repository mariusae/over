package content

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIsBinary(t *testing.T) {
	for _, test := range []struct {
		name   string
		data   string
		binary bool
	}{
		{"empty", "", false},
		{"shell script", "#!/bin/sh\necho hello\n", false},
		{"utf-8", "café ☕\n", false},
		{"crlf", "one\r\ntwo\r\n", false},
		{"elf header", "\x7fELF\x02\x01\x01\x00", true},
		{"nul anywhere in the head", strings.Repeat("a", 100) + "\x00", true},
		// A NUL past the window is not looked for; git draws the line
		// in the same place.
		{"nul past the window", strings.Repeat("a", sniff+10) + "\x00", false},
	} {
		if got := IsBinary([]byte(test.data)); got != test.binary {
			t.Errorf("%s: IsBinary = %v, want %v", test.name, got, test.binary)
		}
	}
}

func TestIsBinaryFile(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}
	if binary, err := IsBinaryFile(write("script", "#!/bin/sh\n")); err != nil || binary {
		t.Errorf("script: %v, %v", binary, err)
	}
	if binary, err := IsBinaryFile(write("program", "\x7fELF\x00\x00")); err != nil || !binary {
		t.Errorf("program: %v, %v", binary, err)
	}
	// A file shorter than the window is read to its end, not padded.
	if binary, err := IsBinaryFile(write("short", "hi")); err != nil || binary {
		t.Errorf("short: %v, %v", binary, err)
	}
	if binary, err := IsBinaryFile(write("empty", "")); err != nil || binary {
		t.Errorf("empty: %v, %v", binary, err)
	}
	if _, err := IsBinaryFile(filepath.Join(dir, "absent")); err == nil {
		t.Error("IsBinaryFile of a missing file succeeded")
	}
}
