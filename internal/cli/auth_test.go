package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// overIn runs a command with something on standard input, for the
// commands that read one.
func (c *client) overIn(stdin string, args ...string) (code int, stdout, stderr string) {
	c.t.Helper()
	c.apply()
	var out, errOut bytes.Buffer
	args = append([]string{"-C", c.home}, args...)
	code = RunWith(context.Background(), strings.NewReader(stdin), &out, &errOut, args)
	return code, out.String(), errOut.String()
}

// https makes over reach its hosts over HTTPS, as it does by default.
// The harness otherwise points $OVER_URL at local bare repositories,
// where no credential is any use and so none is ever asked for.
func (c *client) https() {
	c.t.Helper()
	c.url = ""
}

// writeToken puts a credential in the store by hand, standing in for one
// the user authorized.
func (c *client) writeToken(host, token string) {
	c.t.Helper()
	dir := filepath.Join(c.home, ".config", "over", "auth")
	mkdir(c.t, dir)
	data, err := json.Marshal(map[string]string{
		"host": host, "access_token": token, "login": "mariusae", "source": "token",
	})
	if err != nil {
		c.t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, host+".json"), data, 0o600); err != nil {
		c.t.Fatal(err)
	}
}

// TestCredentialHelper checks the protocol over speaks to git. This is
// the whole of over's side of handing a token over.
func TestCredentialHelper(t *testing.T) {
	c := newClient(t)
	c.writeToken("github.com", "gho_secret")

	code, stdout, stderr := c.overIn("protocol=https\nhost=github.com\n\n", "credential", "get")
	if code != exitOK {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	for _, want := range []string{"username=x-access-token", "password=gho_secret"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("credential get missing %q:\n%s", want, stdout)
		}
	}

	// git addresses a host with a port; the token belongs to the host.
	_, stdout, _ = c.overIn("protocol=https\nhost=github.com:443\n\n", "credential", "get")
	if !strings.Contains(stdout, "password=gho_secret") {
		t.Errorf("a host with a port was not matched:\n%s", stdout)
	}

	// A host over has nothing for gets no answer, rather than a wrong
	// one: git goes on to whatever is next.
	_, stdout, _ = c.overIn("protocol=https\nhost=gitlab.com\n\n", "credential", "get")
	if strings.TrimSpace(stdout) != "" {
		t.Errorf("answered for a host it has no token for:\n%s", stdout)
	}
}

// TestCredentialStoreAndEraseAreIgnored checks that git cannot tell over
// anything about its own tokens.
func TestCredentialStoreAndEraseAreIgnored(t *testing.T) {
	c := newClient(t)
	c.writeToken("github.com", "gho_secret")
	for _, op := range []string{"store", "erase"} {
		code, stdout, stderr := c.overIn("protocol=https\nhost=github.com\npassword=other\n\n", "credential", op)
		if code != exitOK {
			t.Errorf("%s: exit %d: %s", op, code, stderr)
		}
		if strings.TrimSpace(stdout) != "" {
			t.Errorf("%s said %q", op, stdout)
		}
	}
	// The token is untouched.
	_, stdout, _ := c.overIn("protocol=https\nhost=github.com\n\n", "credential", "get")
	if !strings.Contains(stdout, "password=gho_secret") {
		t.Errorf("the stored token changed:\n%s", stdout)
	}
}

func TestCredentialUsage(t *testing.T) {
	c := newClient(t)
	if code, _, _ := c.overIn("", "credential"); code != exitUsage {
		t.Errorf("no operation: exit %d, want %d", code, exitUsage)
	}
	if code, _, _ := c.overIn("host=github.com\n\n", "credential", "frobnicate"); code != exitUsage {
		t.Errorf("unknown operation: exit %d, want %d", code, exitUsage)
	}
}

// TestCredentialIsHidden checks that the helper stays out of the command
// list, being for git rather than for people, while remaining
// documented.
func TestCredentialIsHidden(t *testing.T) {
	code, stdout, stderr := run(t, "help")
	if code != exitOK {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	if strings.Contains(stdout, "credential ") {
		t.Errorf("credential is in the command list:\n%s", stdout)
	}
	code, stdout, _ = run(t, "help", "credential")
	if code != exitOK || !strings.Contains(stdout, "credential protocol") {
		t.Errorf("help credential = %q", stdout)
	}
}

func TestAuthStatus(t *testing.T) {
	c := newClient(t)
	if out := c.mustOver("auth", "-status"); !strings.Contains(out, "no credentials") {
		t.Errorf("empty status = %q", out)
	}
	c.writeToken("github.com", "gho_secret")
	out := c.mustOver("auth", "-status")
	for _, want := range []string{"github.com", "mariusae", "does not expire"} {
		if !strings.Contains(out, want) {
			t.Errorf("status missing %q:\n%s", want, out)
		}
	}
	// The token itself is never printed.
	if strings.Contains(out, "gho_secret") {
		t.Errorf("status printed the token:\n%s", out)
	}
}

func TestAuthRm(t *testing.T) {
	c := newClient(t)
	c.writeToken("github.com", "gho_secret")
	if out := c.mustOver("auth", "-rm"); !strings.Contains(out, "forgot github.com") {
		t.Errorf("auth -rm = %q", out)
	}
	if out := c.mustOver("auth", "-status"); !strings.Contains(out, "no credentials") {
		t.Errorf("the token survived: %q", out)
	}
	code, _, stderr := c.over("auth", "-rm")
	if code != exitError || !strings.Contains(stderr, "no credential to forget") {
		t.Errorf("second auth -rm: exit %d, stderr %q", code, stderr)
	}
}

// TestAuthWithoutTerminal is the promise that over will not block on
// somebody who is not there. The test process has no terminal, which is
// exactly the case that matters.
func TestAuthWithoutTerminal(t *testing.T) {
	c := newClient(t)
	c.https()
	code, _, stderr := c.over("auth")
	if code != exitError {
		t.Fatalf("exit %d, want %d", code, exitError)
	}
	for _, want := range []string{"no terminal", "over auth"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr missing %q: %q", want, stderr)
		}
	}
}

// TestAuthNeverRefuses checks the override for a script that would
// rather have the error than the wait.
func TestAuthNeverRefuses(t *testing.T) {
	c := newClient(t)
	c.https()
	t.Setenv("OVER_AUTH", "never")
	code, _, stderr := c.over("auth")
	if code != exitError || !strings.Contains(stderr, "no terminal") {
		t.Errorf("exit %d, stderr %q", code, stderr)
	}
}

// TestAuthFromEnvironment checks that a token in the environment is
// enough, which is what makes over work in a build with no store.
func TestAuthFromEnvironment(t *testing.T) {
	c := newClient(t)
	c.https()
	t.Setenv("GITHUB_TOKEN", "ghp_fromenv")
	_, stdout, _ := c.overIn("protocol=https\nhost=github.com\n\n", "credential", "get")
	if !strings.Contains(stdout, "password=ghp_fromenv") {
		t.Errorf("the environment token was not used:\n%s", stdout)
	}
	if out := c.mustOver("auth"); !strings.Contains(out, "already reachable") {
		t.Errorf("auth with a token in the environment = %q", out)
	}
}

func TestHost(t *testing.T) {
	c, _ := newMultiLayer(t)
	c.mustOver("add", "mariusae/config:shell")

	// HTTPS is the default, and it is the configured layers' hosts the
	// answer bears on.
	out := c.mustOver("host")
	if !strings.Contains(out, "github.com") || !strings.Contains(out, "https") {
		t.Errorf("over host = %q", out)
	}
	if out := c.mustOver("host", "github.com", "ssh"); !strings.Contains(out, "ssh") {
		t.Errorf("over host github.com ssh = %q", out)
	}
	if out := c.mustOver("host", "github.com"); !strings.Contains(out, "ssh") {
		t.Errorf("the transport did not stick: %q", out)
	}
	if !strings.Contains(c.read(".config/over/config.yaml"), "transport: ssh") {
		t.Errorf("the transport was not written down:\n%s", c.read(".config/over/config.yaml"))
	}
	code, _, stderr := c.over("host", "github.com", "carrier-pigeon")
	if code != exitError || !strings.Contains(stderr, "is not a transport") {
		t.Errorf("unknown transport: exit %d, stderr %q", code, stderr)
	}
}

// TestTokenStoreIsNeverLayerContent is the one rule the configuration
// cannot switch off. The layer claims everything under $HOME and the
// exclusions are emptied, and over still refuses to publish its own
// credentials -- a token committed to a layer is a token to revoke, and
// it would go to every machine that adds it.
func TestTokenStoreIsNeverLayerContent(t *testing.T) {
	c := newClient(t)
	r := c.newRepo("mariusae", "env")
	r.write("all/.keep", "keep\n")
	r.write("config.yaml", "layers:\n    all:\n        root: $HOME\n        track:\n            - .config/...\n")
	r.commit("a layer that claims everything")

	c.mustOver("add", "mariusae/env:all")
	c.writeToken("github.com", "gho_secret")

	// Empty the exclusions, which over otherwise seeds with its own
	// directories, and which it respects as written.
	cfg := c.read(".config/over/config.yaml")
	c.write(".config/over/config.yaml", strings.Replace(cfg,
		"exclude:", "exclude: []\nunused:", 1))

	c.mustOver("sync")
	out := c.mustOver("status", "-a")
	if strings.Contains(out, "auth/github.com.json") {
		t.Errorf("the token store is layer content:\n%s", out)
	}
	if r.exists("all/.config/over/auth/github.com.json") {
		t.Error("the token was published to the layer")
	}
	// The store is still where over left it.
	if _, err := os.Stat(filepath.Join(c.home, ".config", "over", "auth", "github.com.json")); err != nil {
		t.Errorf("the token went missing: %v", err)
	}
}

// TestPromptableIgnoresStdin is the regression test for the way over is
// actually installed:
//
//	curl -fsSL .../bootstrap.sh | sh -s -- mariusae/env::mac
//
// There stdin is the pipe carrying the script, while a person sits
// watching the output. Authorizing reads nothing, so only the output
// stream has any bearing on whether anybody can see the prompt.
func TestPromptableIgnoresStdin(t *testing.T) {
	pty, err := os.OpenFile("/dev/ptmx", os.O_RDWR, 0)
	if err != nil {
		t.Skipf("no pty available: %v", err)
	}
	defer pty.Close()

	// A terminal to write to is enough, whatever stdin is.
	if !promptable(pty) {
		t.Error("over would not prompt on a terminal")
	}

	// A pipe is not somewhere a person can read a link.
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	if promptable(w) {
		t.Error("over would prompt into a pipe")
	}

	// /dev/null is a character device, and is what a cron job gets.
	nul, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer nul.Close()
	if promptable(nul) {
		t.Error("over would prompt into /dev/null")
	}
}

func TestPromptableOverrides(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()

	t.Setenv("OVER_AUTH", "always")
	if !promptable(w) {
		t.Error("OVER_AUTH=always did not insist")
	}
	t.Setenv("OVER_AUTH", "never")
	if promptable(w) {
		t.Error("OVER_AUTH=never did not refuse")
	}
	if pty, err := os.OpenFile("/dev/ptmx", os.O_RDWR, 0); err == nil {
		defer pty.Close()
		if promptable(pty) {
			t.Error("OVER_AUTH=never did not refuse a terminal")
		}
	}
}
