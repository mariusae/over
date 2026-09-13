package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeInstalls stands in for a host's installation endpoints. Its state
// can change between polls, which is what installing looks like from
// over's side.
type fakeInstalls struct {
	mu sync.Mutex

	// installs maps an installation id to the repositories it covers.
	// A nil slice with all set means it covers everything.
	installs map[int64][]string
	all      map[int64]bool

	calls int
}

func (f *fakeInstalls) serve(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/user/installations", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.calls++
		var list []map[string]any
		for id := range f.installs {
			sel := "selected"
			if f.all[id] {
				sel = "all"
			}
			list = append(list, map[string]any{
				"id":                   id,
				"repository_selection": sel,
				"app_slug":             "over",
				"account":              map[string]any{"login": "mariusae"},
			})
		}
		writeJSON(w, map[string]any{"total_count": len(list), "installations": list})
	})
	mux.HandleFunc("/user/installations/", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		var id int64
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		fmt.Sscanf(parts[2], "%d", &id)
		var repos []map[string]any
		for _, name := range f.installs[id] {
			repos = append(repos, map[string]any{"full_name": name})
		}
		writeJSON(w, map[string]any{"total_count": len(repos), "repositories": repos})
	})
	return httptest.NewServer(mux)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func (f *fakeInstalls) client(srv *httptest.Server) *GitHub {
	return &GitHub{
		Host:     "github.test",
		ClientID: "Iv1.test",
		BaseURL:  srv.URL,
		APIURL:   srv.URL,
		Sleep:    func(context.Context, time.Duration) error { return nil },
	}
}

func TestReach(t *testing.T) {
	f := &fakeInstalls{
		installs: map[int64][]string{7: {"mariusae/env", "mariusae/Config"}},
		all:      map[int64]bool{},
	}
	srv := f.serve(t)
	defer srv.Close()

	reach, err := f.client(srv).Reach(context.Background(), &Token{Access: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if reach.Installations != 1 {
		t.Errorf("Installations = %d", reach.Installations)
	}
	if reach.Slug != "over" {
		t.Errorf("Slug = %q", reach.Slug)
	}
	if !reach.Covers("mariusae/env") {
		t.Error("does not cover mariusae/env")
	}
	// GitHub is case-insensitive about repository names; the comparison
	// must be too, or over sends people to reinstall what is there.
	if !reach.Covers("mariusae/config") {
		t.Error("does not cover mariusae/config, differing only in case")
	}
	if reach.Covers("mariusae/secret") {
		t.Error("covers a repository it was not installed on")
	}
	if got := Missing(reach, []string{"mariusae/env", "mariusae/secret", "mariusae/secret"}); len(got) != 1 || got[0] != "mariusae/secret" {
		t.Errorf("Missing = %v, want just mariusae/secret, once", got)
	}
}

// TestReachAll checks that an installation covering everything is taken
// at its word rather than enumerated.
func TestReachAll(t *testing.T) {
	f := &fakeInstalls{installs: map[int64][]string{7: nil}, all: map[int64]bool{7: true}}
	srv := f.serve(t)
	defer srv.Close()
	reach, err := f.client(srv).Reach(context.Background(), &Token{Access: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if !reach.All || !reach.Covers("anyone/anything") {
		t.Errorf("an all-repositories installation does not cover everything: %+v", reach)
	}
	if got := Missing(reach, []string{"a/b"}); len(got) != 0 {
		t.Errorf("Missing = %v, want none", got)
	}
}

// TestReachNoInstallations is the unambiguous case: authorized and never
// installed, which is the state a new user is left in.
func TestReachNoInstallations(t *testing.T) {
	f := &fakeInstalls{installs: map[int64][]string{}, all: map[int64]bool{}}
	srv := f.serve(t)
	defer srv.Close()
	reach, err := f.client(srv).Reach(context.Background(), &Token{Access: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if reach.Installations != 0 || reach.Covers("mariusae/env") {
		t.Errorf("reach = %+v", reach)
	}
}

// TestInstallWaits checks that over waits for the page to be submitted
// and then goes on, without the token changing: an installation is not
// a permission the token carries but a place it may be used.
func TestInstallWaits(t *testing.T) {
	f := &fakeInstalls{installs: map[int64][]string{}, all: map[int64]bool{}}
	srv := f.serve(t)
	defer srv.Close()
	// The slug has to be configured: with nothing installed anywhere,
	// the host reports none either, and that is exactly the case an
	// installation prompt exists for.
	t.Setenv("OVER_APP_SLUG", "over")
	g := f.client(srv)

	var shown Prompt
	done := make(chan error, 1)
	go func() {
		done <- g.Install(context.Background(), &Token{Access: "x"}, []string{"mariusae/env"}, func(p Prompt) error {
			shown = p
			// The user goes and installs it.
			f.mu.Lock()
			f.installs[7] = []string{"mariusae/env"}
			f.mu.Unlock()
			return nil
		})
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Install did not return")
	}
	if shown.Kind != PromptInstall {
		t.Errorf("prompt kind = %v, want an install", shown.Kind)
	}
	if !strings.HasSuffix(shown.URI, "/apps/over/installations/new") {
		t.Errorf("prompt URI = %q", shown.URI)
	}
	if len(shown.Repos) != 1 || shown.Repos[0] != "mariusae/env" {
		t.Errorf("prompt repos = %v", shown.Repos)
	}
}

// TestInstallNeedsASlug checks that over says what is missing rather
// than offering a broken link.
func TestInstallNeedsASlug(t *testing.T) {
	f := &fakeInstalls{installs: map[int64][]string{}, all: map[int64]bool{}}
	srv := f.serve(t)
	defer srv.Close()
	// With no installations the host reports no slug either, and this
	// build was given none.
	err := f.client(srv).Install(context.Background(), &Token{Access: "x"},
		[]string{"mariusae/env"}, func(Prompt) error { return nil })
	if err == nil || !strings.Contains(err.Error(), "app's name") {
		t.Errorf("err = %v, want a complaint about the app's name", err)
	}
}

func TestInstallURL(t *testing.T) {
	g := &GitHub{Host: "github.test"}
	if got := g.InstallURL(""); got != "" {
		t.Errorf("InstallURL with no slug anywhere = %q, want empty", got)
	}
	if got, want := g.InstallURL("over"), "https://github.test/apps/over/installations/new"; got != want {
		t.Errorf("InstallURL = %q, want %q", got, want)
	}
	// A slug this build was given outranks the one the host reported.
	t.Setenv("OVER_APP_SLUG", "over-dev")
	if got, want := g.InstallURL("over"), "https://github.test/apps/over-dev/installations/new"; got != want {
		t.Errorf("InstallURL = %q, want %q", got, want)
	}
}

func TestClientIDFromEnv(t *testing.T) {
	g := &GitHub{Host: "github.test"}
	if g.clientID() != "" {
		t.Errorf("clientID = %q, want empty in a build with none", g.clientID())
	}
	t.Setenv("OVER_CLIENT_ID", "Iv1.fromenv")
	if got := g.clientID(); got != "Iv1.fromenv" {
		t.Errorf("clientID = %q", got)
	}
	// An explicit one outranks the environment.
	g.ClientID = "Iv1.explicit"
	if got := g.clientID(); got != "Iv1.explicit" {
		t.Errorf("clientID = %q", got)
	}
}
