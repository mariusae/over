package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// fakeGitHub stands in for a host's device-flow endpoints.
type fakeGitHub struct {
	// pending is how many times the token endpoint says the user has
	// not finished yet before it hands one over.
	pending int

	// err is the error the token endpoint reports instead, if any.
	err string

	// scope is what the granted token says it may do.
	scope string

	// expiresIn, when set, makes the granted token expire.
	expiresIn int

	polls int
	form  map[string]string
}

func (f *fakeGitHub) serve(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/login/device/code", func(w http.ResponseWriter, r *http.Request) {
		f.record(r)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"device_code":"dev-1","user_code":"C1A2-B3D4",`+
			`"verification_uri":"https://example.invalid/login/device",`+
			`"expires_in":900,"interval":1}`)
	})
	mux.HandleFunc("/login/oauth/access_token", func(w http.ResponseWriter, r *http.Request) {
		f.record(r)
		w.Header().Set("Content-Type", "application/json")
		if f.err != "" {
			fmt.Fprintf(w, `{"error":%q}`, f.err)
			return
		}
		if f.polls++; f.polls <= f.pending {
			fmt.Fprint(w, `{"error":"authorization_pending"}`)
			return
		}
		fmt.Fprintf(w, `{"access_token":"gho_granted","refresh_token":"ghr_1",`+
			`"expires_in":%d,"refresh_token_expires_in":15897600,"scope":%q}`,
			f.expiresIn, f.scope)
	})
	mux.HandleFunc("/user", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer gho_granted" && got != "Bearer pasted" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"login":"mariusae"}`)
	})
	return httptest.NewServer(mux)
}

func (f *fakeGitHub) record(r *http.Request) {
	if err := r.ParseForm(); err != nil {
		return
	}
	if f.form == nil {
		f.form = map[string]string{}
	}
	for k, v := range r.PostForm {
		f.form[k] = v[0]
	}
}

// client returns a GitHub pointed at the fake, polling without waiting.
func (f *fakeGitHub) client(srv *httptest.Server) *GitHub {
	return &GitHub{
		Host:     "github.test",
		ClientID: "Iv1.test",
		BaseURL:  srv.URL,
		APIURL:   srv.URL,
		Sleep:    func(context.Context, time.Duration) error { return nil },
	}
}

func TestAuthorize(t *testing.T) {
	f := &fakeGitHub{pending: 2, scope: "repo", expiresIn: 28800}
	srv := f.serve(t)
	defer srv.Close()

	var shown Prompt
	tok, err := f.client(srv).Authorize(context.Background(), func(p Prompt) error {
		shown = p
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if shown.Code != "C1A2-B3D4" {
		t.Errorf("prompt code = %q", shown.Code)
	}
	if shown.URI != "https://example.invalid/login/device" {
		t.Errorf("prompt URI = %q", shown.URI)
	}
	// The code must not be in the link: opening a page should not be
	// enough on its own to authorize anything.
	if strings.Contains(shown.URI, shown.Code) {
		t.Errorf("the user code is in the verification URI: %q", shown.URI)
	}
	if tok.Access != "gho_granted" || tok.Refresh != "ghr_1" {
		t.Errorf("token = %+v", tok)
	}
	if tok.Login != "mariusae" {
		t.Errorf("login = %q, want mariusae", tok.Login)
	}
	if tok.Expiry.IsZero() || tok.RefreshExpiry.IsZero() {
		t.Errorf("expiries not set: %+v", tok)
	}
	if tok.Source != FromDevice {
		t.Errorf("source = %q", tok.Source)
	}
	if f.polls != 3 {
		t.Errorf("polled %d times, want 3 (two pending, then granted)", f.polls)
	}
	if f.form["grant_type"] != "urn:ietf:params:oauth:grant-type:device_code" {
		t.Errorf("grant_type = %q", f.form["grant_type"])
	}
}

func TestAuthorizeDenied(t *testing.T) {
	f := &fakeGitHub{err: "access_denied"}
	srv := f.serve(t)
	defer srv.Close()
	_, err := f.client(srv).Authorize(context.Background(), func(Prompt) error { return nil })
	if !errors.Is(err, ErrDenied) {
		t.Errorf("err = %v, want ErrDenied", err)
	}
}

func TestAuthorizeExpired(t *testing.T) {
	f := &fakeGitHub{err: "expired_token"}
	srv := f.serve(t)
	defer srv.Close()
	_, err := f.client(srv).Authorize(context.Background(), func(Prompt) error { return nil })
	if err == nil || !strings.Contains(err.Error(), "expired") {
		t.Errorf("err = %v, want an expiry message", err)
	}
}

// TestAuthorizeShowFailureStops checks that a prompt that cannot be
// shown stops the flow rather than polling for something nobody was
// told to do.
func TestAuthorizeShowFailureStops(t *testing.T) {
	f := &fakeGitHub{}
	srv := f.serve(t)
	defer srv.Close()
	boom := errors.New("no terminal")
	_, err := f.client(srv).Authorize(context.Background(), func(Prompt) error { return boom })
	if !errors.Is(err, boom) {
		t.Errorf("err = %v, want the prompt's error", err)
	}
	if f.polls != 0 {
		t.Errorf("polled %d times after failing to show the prompt", f.polls)
	}
}

func TestAuthorizeNeedsClientID(t *testing.T) {
	defer withClientID("")()
	g := &GitHub{Host: "github.test"}
	if _, err := g.Authorize(context.Background(), func(Prompt) error { return nil }); !errors.Is(err, ErrNoClientID) {
		t.Errorf("err = %v, want ErrNoClientID", err)
	}
}

func TestRenew(t *testing.T) {
	f := &fakeGitHub{scope: "repo", expiresIn: 28800}
	srv := f.serve(t)
	defer srv.Close()
	old := &Token{
		Host:          "github.test",
		Access:        "stale",
		Refresh:       "ghr_1",
		Expiry:        time.Now().Add(-time.Hour),
		RefreshExpiry: time.Now().Add(30 * 24 * time.Hour),
		Source:        FromDevice,
	}
	fresh, err := f.client(srv).Renew(context.Background(), old)
	if err != nil {
		t.Fatal(err)
	}
	if fresh.Access != "gho_granted" {
		t.Errorf("renewed access token = %q", fresh.Access)
	}
	if fresh.Source != FromDevice {
		t.Errorf("renewing lost the source: %q", fresh.Source)
	}
	if f.form["grant_type"] != "refresh_token" {
		t.Errorf("grant_type = %q", f.form["grant_type"])
	}
}

// TestRenewUnrenewable checks that over says to authorize again rather
// than pretending it can fix an unrenewable credential.
func TestRenewUnrenewable(t *testing.T) {
	f := &fakeGitHub{}
	srv := f.serve(t)
	defer srv.Close()
	_, err := f.client(srv).Renew(context.Background(), &Token{Host: "github.test", Access: "x"})
	if err == nil || !strings.Contains(err.Error(), "over auth") {
		t.Errorf("err = %v, want a pointer to 'over auth'", err)
	}
}

// TestAPIURL checks the endpoint shapes: github.com serves its API from
// another domain, and Enterprise from a path on the host itself.
func TestAPIURL(t *testing.T) {
	for _, test := range []struct{ host, base, api string }{
		{"", "https://github.com", "https://api.github.com"},
		{"github.com", "https://github.com", "https://api.github.com"},
		{"github.example.com", "https://github.example.com", "https://github.example.com/api/v3"},
	} {
		g := &GitHub{Host: test.host}
		if got := g.base(); got != test.base {
			t.Errorf("%q: base = %q, want %q", test.host, got, test.base)
		}
		if got := g.api(); got != test.api {
			t.Errorf("%q: api = %q, want %q", test.host, got, test.api)
		}
	}
}

// withClientID replaces the built-in client id for one test, and returns
// the function that puts it back.
func withClientID(id string) func() {
	old := ClientID
	ClientID = id
	return func() { ClientID = old }
}

// withAppSlug does the same for the app's slug.
func withAppSlug(slug string) func() {
	old := AppSlug
	AppSlug = slug
	return func() { AppSlug = old }
}
