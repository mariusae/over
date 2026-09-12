package cli

import (
	"bytes"
	"context"
	"flag"
	"os"
	"strings"
	"testing"
)

// run runs the CLI with args, returning the exit code and the contents
// of the standard output and error streams.
func run(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code = Run(context.Background(), &out, &errOut, args)
	return code, out.String(), errOut.String()
}

func TestNoArgs(t *testing.T) {
	code, stdout, stderr := run(t)
	if code != exitUsage {
		t.Errorf("got exit %d, want %d", code, exitUsage)
	}
	if stdout != "" {
		t.Errorf("unexpected stdout: %q", stdout)
	}
	if !strings.Contains(stderr, "Usage:") {
		t.Errorf("stderr missing usage: %q", stderr)
	}
}

func TestUnknownCommand(t *testing.T) {
	code, _, stderr := run(t, "nosuchcommand")
	if code != exitUsage {
		t.Errorf("got exit %d, want %d", code, exitUsage)
	}
	if !strings.Contains(stderr, "unknown command") {
		t.Errorf("stderr = %q, want unknown command", stderr)
	}
}

func TestHelp(t *testing.T) {
	code, stdout, stderr := run(t, "help")
	if code != exitOK {
		t.Fatalf("got exit %d (%s), want 0", code, stderr)
	}
	for _, want := range []string{"Usage:", "help", "version", "-C dir"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("help output missing %q:\n%s", want, stdout)
		}
	}
}

func TestHelpCommand(t *testing.T) {
	code, stdout, stderr := run(t, "help", "version")
	if code != exitOK {
		t.Fatalf("got exit %d (%s), want 0", code, stderr)
	}
	if !strings.Contains(stdout, "over version") {
		t.Errorf("help version output = %q", stdout)
	}
}

func TestHelpUnknownCommand(t *testing.T) {
	code, _, stderr := run(t, "help", "nosuchcommand")
	if code != exitError {
		t.Errorf("got exit %d, want %d", code, exitError)
	}
	if !strings.Contains(stderr, "unknown command") {
		t.Errorf("stderr = %q", stderr)
	}
}

func TestVersion(t *testing.T) {
	code, stdout, stderr := run(t, "version")
	if code != exitOK {
		t.Fatalf("got exit %d (%s), want 0", code, stderr)
	}
	if !strings.HasPrefix(stdout, "over ") {
		t.Errorf("version output = %q", stdout)
	}
}

func TestUsageError(t *testing.T) {
	code, _, stderr := run(t, "version", "extra")
	if code != exitUsage {
		t.Errorf("got exit %d, want %d", code, exitUsage)
	}
	if !strings.Contains(stderr, "too many arguments") {
		t.Errorf("stderr = %q", stderr)
	}
	if !strings.Contains(stderr, "Usage:") {
		t.Errorf("stderr missing usage: %q", stderr)
	}
}

func TestGlobalFlagsBeforeCommand(t *testing.T) {
	// The -C flag changes the process's working directory; restore it
	// so that it does not leak into other tests.
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(cwd); err != nil {
			t.Fatal(err)
		}
	})

	dir := t.TempDir()
	code, stdout, stderr := run(t, "-v", "-C", dir, "version")
	if code != exitOK {
		t.Fatalf("got exit %d (%s), want 0", code, stderr)
	}
	if !strings.HasPrefix(stdout, "over ") {
		t.Errorf("version output = %q", stdout)
	}
}

// TestGlobalFlagsMatchHelp guards against the global flag set rendered by
// help drifting from the one Run actually parses.
func TestGlobalFlagsMatchHelp(t *testing.T) {
	var errOut bytes.Buffer
	if code := Run(context.Background(), &bytes.Buffer{}, &errOut, []string{"-nosuchflag"}); code != exitUsage {
		t.Errorf("got exit %d, want %d", code, exitUsage)
	}
	documented := map[string]bool{}
	globalFlags().VisitAll(func(f *flag.Flag) { documented[f.Name] = true })
	for _, name := range []string{"C", "v"} {
		if !documented[name] {
			t.Errorf("global flag -%s missing from help", name)
		}
	}
}

func TestCommandsAreWellFormed(t *testing.T) {
	for _, cmd := range sortedCommands() {
		switch {
		case cmd.Usage == "":
			t.Errorf("command %s: empty Usage", cmd.Name)
		case !strings.HasPrefix(cmd.Usage, cmd.Name):
			t.Errorf("command %s: Usage %q does not begin with the command name", cmd.Name, cmd.Usage)
		case cmd.Short == "":
			t.Errorf("command %s: empty Short", cmd.Name)
		case cmd.Run == nil:
			t.Errorf("command %s: nil Run", cmd.Name)
		}
	}
}
