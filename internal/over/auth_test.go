package over

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mariusae/over/internal/auth"
	"github.com/mariusae/over/internal/config"
	"github.com/mariusae/over/internal/gitrepo"
	"github.com/mariusae/over/internal/pathspec"
	"github.com/mariusae/over/internal/spec"
)

// newTestOver returns a client in a temporary directory.
func newTestOver(t *testing.T) *Over {
	t.Helper()
	dir := t.TempDir()
	o, err := Open(Options{
		Home:       filepath.Join(dir, "home"),
		Cache:      filepath.Join(dir, "cache"),
		Executable: "/usr/local/bin/over",
	})
	if err != nil {
		t.Fatal(err)
	}
	return o
}

func mustSpec(t *testing.T, s string) spec.Spec {
	t.Helper()
	sp, err := spec.Parse(s)
	if err != nil {
		t.Fatal(err)
	}
	return sp
}

// TestWriteNeeds checks that the plan is what says which credentials a
// sync will want, which is what lets over ask before it starts.
func TestWriteNeeds(t *testing.T) {
	env := &Layer{Spec: mustSpec(t, "mariusae/env:bin")}
	work := &Layer{Spec: mustSpec(t, "git.example.com/m/c:etc")}
	changes := []Change{
		{Layer: env, Status: Pull},       // a read wants nothing up front
		{Layer: env, Status: Push},       //
		{Layer: env, Status: PushDelete}, // the same host again, counted once
		{Layer: work, Status: Push},
		{Layer: work, Status: Conflict}, // not going anywhere
	}
	needs := WriteNeeds(changes)
	if len(needs) != 2 {
		t.Fatalf("WriteNeeds = %+v, want one per host written", needs)
	}
	if needs[0].Host != "github.com" || !needs[0].Write {
		t.Errorf("needs[0] = %+v", needs[0])
	}
	if needs[1].Host != "git.example.com" {
		t.Errorf("needs[1] = %+v", needs[1])
	}

	// Nothing to publish, nothing to ask for.
	if got := WriteNeeds([]Change{{Layer: env, Status: Pull}}); len(got) != 0 {
		t.Errorf("a read-only plan wants %+v", got)
	}
}

// TestWantsToken checks that over only looks for a credential where one
// would be any use.
func TestWantsToken(t *testing.T) {
	o := newTestOver(t)
	if !o.wantsToken("github.com") {
		t.Error("the HTTPS default does not want a token")
	}
	if err := o.SetTransport("github.com", config.TransportSSH); err != nil {
		t.Fatal(err)
	}
	if o.wantsToken("github.com") {
		t.Error("SSH wants a token; the key answers for it")
	}
	// $OVER_URL can point over at something that is not a host at all.
	o.url = func(spec.Spec) string { return "file:///tmp/x.git" }
	if o.wantsToken("github.com") {
		t.Error("a file URL wants a token")
	}
}

func TestURLFollowsTransport(t *testing.T) {
	o := newTestOver(t)
	s := mustSpec(t, "mariusae/env:bin")
	if got, want := o.URL(s), "https://github.com/mariusae/env.git"; got != want {
		t.Errorf("URL = %q, want %q", got, want)
	}
	if err := o.SetTransport("github.com", config.TransportSSH); err != nil {
		t.Fatal(err)
	}
	if got, want := o.URL(s), "git@github.com:mariusae/env.git"; got != want {
		t.Errorf("URL after switching to ssh = %q, want %q", got, want)
	}
	if err := o.SetTransport("github.com", "carrier-pigeon"); err == nil {
		t.Error("an unknown transport was accepted")
	}
}

// TestGitConfigOnlyWithToken is the reason a machine whose own git
// already works goes on working: with no token of its own, over adds
// nothing to git's configuration and whatever answered before answers
// still.
func TestGitConfigOnlyWithToken(t *testing.T) {
	o := newTestOver(t)
	s := mustSpec(t, "mariusae/env:bin")

	cfg, err := o.gitConfig(s)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Helper != "" {
		t.Errorf("a helper was installed with no token: %q", cfg.Helper)
	}

	if err := o.store.Save(&auth.Token{Host: "github.com", Access: "gho_x"}); err != nil {
		t.Fatal(err)
	}
	cfg, err = o.gitConfig(s)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(cfg.Helper, "credential") || !strings.HasPrefix(cfg.Helper, "!") {
		t.Errorf("helper = %q, want a shell command running over credential", cfg.Helper)
	}
	// The helper must be able to clear whatever the user's git installs,
	// so that a stale credential elsewhere cannot keep winning.
	args := cfg.Args()
	if len(args) < 4 || args[1] != "credential.helper=" {
		t.Errorf("args = %q, want the helper list cleared first", args)
	}
}

func TestShellQuote(t *testing.T) {
	for _, test := range []struct{ in, want string }{
		{"/usr/bin/over", `'/usr/bin/over'`},
		{"/Applications/My Tools/over", `'/Applications/My Tools/over'`},
		{"/tmp/o'ver", `'/tmp/o'\''ver'`},
	} {
		if got := shellQuote(test.in); got != test.want {
			t.Errorf("shellQuote(%q) = %q, want %q", test.in, got, test.want)
		}
	}
}

// TestCredentialFromEnv checks that a token in the environment is used
// and never written down, which is what makes over work in a build.
func TestCredentialFromEnv(t *testing.T) {
	o := newTestOver(t)
	t.Setenv("GITHUB_TOKEN", "ghp_fromenv")
	user, pass, ok, err := o.Credential(context.Background(), "github.com")
	if err != nil || !ok {
		t.Fatalf("Credential = %v, %v", ok, err)
	}
	if user != "x-access-token" || pass != "ghp_fromenv" {
		t.Errorf("Credential = %q, %q", user, pass)
	}
	if hosts, err := o.store.Hosts(); err != nil || len(hosts) != 0 {
		t.Errorf("an environment token was stored: %v, %v", hosts, err)
	}
	// An explicit token outranks a stored one.
	if err := o.store.Save(&auth.Token{Host: "github.com", Access: "stored"}); err != nil {
		t.Fatal(err)
	}
	if _, pass, _, _ := o.Credential(context.Background(), "github.com"); pass != "ghp_fromenv" {
		t.Errorf("password = %q, want the environment's", pass)
	}
}

// TestAuthorizeWithoutPrompt checks that over does not hang on somebody
// who is not there, and says what to run instead.
func TestAuthorizeWithoutPrompt(t *testing.T) {
	o := newTestOver(t)
	err := o.Authorize(context.Background(), Need{Host: "github.com", Write: true})
	if !errors.Is(err, ErrNoPrompt) {
		t.Fatalf("err = %v, want ErrNoPrompt", err)
	}
	if !strings.Contains(err.Error(), "over auth") {
		t.Errorf("err = %v, want the command to run", err)
	}
}

// TestAuthorizeIsCheapWhenHeld checks that a credential already held
// costs nothing, so that asking at every barrier is free.
func TestAuthorizeIsCheapWhenHeld(t *testing.T) {
	o := newTestOver(t)
	if err := o.store.Save(&auth.Token{Host: "github.com", Access: "gho_x"}); err != nil {
		t.Fatal(err)
	}
	// No prompt is installed, so any attempt to obtain one would fail.
	if err := o.Authorize(context.Background(), Need{Host: "github.com", Write: true}); err != nil {
		t.Errorf("Authorize with a token in hand: %v", err)
	}
}

// TestAuthorizeSkipsSSH checks that a host reached with a key is not
// asked about.
func TestAuthorizeSkipsSSH(t *testing.T) {
	o := newTestOver(t)
	if err := o.SetTransport("github.com", config.TransportSSH); err != nil {
		t.Fatal(err)
	}
	if err := o.Authorize(context.Background(), Need{Host: "github.com", Write: true}); err != nil {
		t.Errorf("Authorize for an SSH host: %v", err)
	}
}

// TestExplainNotFound is the GitHub wrinkle: a private repository you
// cannot see is reported as missing, so over must not say the name is
// wrong.
func TestExplainNotFound(t *testing.T) {
	o := newTestOver(t)
	s := mustSpec(t, "mariusae/secret:bin")
	notFound := &gitrepo.RemoteError{
		Kind: gitrepo.ErrNotFound,
		Op:   "clone",
		Msg:  "remote: Repository not found.",
	}
	err := o.explain(s, notFound)
	if !strings.Contains(err.Error(), "may be private") {
		t.Errorf("err = %v, want the private possibility named", err)
	}
	if !strings.Contains(err.Error(), "over auth github.com") {
		t.Errorf("err = %v, want the command to run", err)
	}

	// With a credential in hand, missing means missing.
	if err := o.store.Save(&auth.Token{Host: "github.com", Access: "gho_x"}); err != nil {
		t.Fatal(err)
	}
	if err := o.explain(s, notFound); strings.Contains(err.Error(), "may be private") {
		t.Errorf("err = %v, want no hedging once over can see", err)
	}

	// Other failures pass through untouched.
	other := errors.New("disk on fire")
	if got := o.explain(s, other); got != other {
		t.Errorf("explain rewrote an unrelated error: %v", got)
	}
	if got := o.explain(s, nil); got != nil {
		t.Errorf("explain invented an error: %v", got)
	}
}

// TestVetoCoversTheStore checks the one rule the configuration cannot
// switch off: over will not carry its own credentials into a layer.
func TestVetoCoversTheStore(t *testing.T) {
	o := newTestOver(t)
	vetoes := o.Vetoes()
	if len(vetoes) != 1 {
		t.Fatalf("Vetoes = %+v", vetoes)
	}
	// Patterns are matched against resolved paths, as the sync builds
	// them from a layer's root.
	inside := pathspec.Resolve(filepath.Join(o.Store().Dir(), "github.com.json"))
	if !vetoes[0].Match(inside) {
		t.Errorf("the veto does not cover %s", inside)
	}
	outside := pathspec.Resolve(filepath.Join(o.Home(), "config.yaml"))
	if vetoes[0].Match(outside) {
		t.Errorf("the veto covers %s, which is not the store", outside)
	}
}

// TestRenewOnUse checks that an expired credential is exchanged without
// troubling the user.
func TestRenewOnUse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/login/oauth/access_token":
			fmt.Fprint(w, `{"access_token":"gho_fresh","refresh_token":"ghr_2","expires_in":28800}`)
		case "/user":
			fmt.Fprint(w, `{"login":"mariusae"}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	old := github
	github = func(host string) *auth.GitHub {
		return &auth.GitHub{Host: host, ClientID: "Iv1.test", BaseURL: srv.URL, APIURL: srv.URL, Now: timeNow}
	}
	defer func() { github = old }()

	o := newTestOver(t)
	if err := o.store.Save(&auth.Token{
		Host:          "github.com",
		Access:        "stale",
		Refresh:       "ghr_1",
		Expiry:        time.Now().Add(-time.Hour),
		RefreshExpiry: time.Now().Add(30 * 24 * time.Hour),
		Source:        auth.FromDevice,
	}); err != nil {
		t.Fatal(err)
	}
	_, pass, ok, err := o.Credential(context.Background(), "github.com")
	if err != nil || !ok {
		t.Fatalf("Credential = %v, %v", ok, err)
	}
	if pass != "gho_fresh" {
		t.Errorf("password = %q, want the renewed token", pass)
	}
	// The renewal is kept, so the next command does not repeat it.
	stored, err := o.store.Load("github.com")
	if err != nil {
		t.Fatal(err)
	}
	if stored.Access != "gho_fresh" {
		t.Errorf("stored token = %q", stored.Access)
	}
}

// TestSaveTokenChecksIt checks that a pasted token is tried before it is
// kept, so that a typo is reported now and not at the next sync.
func TestSaveTokenChecksIt(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer good" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"login":"mariusae"}`)
	}))
	defer srv.Close()
	old := github
	github = func(host string) *auth.GitHub {
		return &auth.GitHub{Host: host, BaseURL: srv.URL, APIURL: srv.URL, Now: timeNow}
	}
	defer func() { github = old }()

	o := newTestOver(t)
	if _, err := o.SaveToken(context.Background(), "github.com", "bad"); err == nil {
		t.Error("a token the host refused was kept")
	}
	if hosts, _ := o.store.Hosts(); len(hosts) != 0 {
		t.Errorf("a refused token was stored: %v", hosts)
	}
	tok, err := o.SaveToken(context.Background(), "github.com", "  good\n")
	if err != nil {
		t.Fatal(err)
	}
	if tok.Access != "good" {
		t.Errorf("token = %q, want it trimmed", tok.Access)
	}
	if tok.Login != "mariusae" || tok.Source != auth.FromPaste {
		t.Errorf("token = %+v", tok)
	}
	if _, err := o.SaveToken(context.Background(), "github.com", "   "); err == nil {
		t.Error("an empty token was accepted")
	}
}
