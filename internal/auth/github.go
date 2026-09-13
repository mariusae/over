package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// DefaultHost is the host over authenticates to when none is named.
const DefaultHost = "github.com"

// ClientID is the GitHub App over authenticates as, overridable at build
// time with -ldflags "-X .../internal/auth.ClientID=..." and at run time
// with $OVER_CLIENT_ID.
//
// It is empty in a build that has not been given one, and over says so
// rather than sending the user to a broken authorization page. A token
// pasted into "over auth -token" needs no app at all, which is how a
// build with no client id still reaches a private repository.
var ClientID = ""

// Scopes is what over asks for. A GitHub App's permissions are settled
// when the app is installed, not when it is authorized, so this is empty
// for one; it is the OAuth App fallback that needs to ask, and "repo" is
// the narrowest scope that can write a private repository.
var Scopes = []string{}

// ErrNoClientID is returned when over has no app to authorize as.
var ErrNoClientID = errors.New("this build of over has no GitHub client id; " +
	"paste a token with 'over auth -token' instead")

// ErrDenied is returned when the user declines the authorization.
var ErrDenied = errors.New("authorization was declined")

// A PromptKind is which of the two things the user has to go and do.
// They are separate acts on GitHub and can be out of step: authorizing
// says who you are, installing says where over may act.
type PromptKind int

const (
	// PromptAuthorize asks the user to authorize over at the host.
	PromptAuthorize PromptKind = iota

	// PromptInstall asks them to install the app on the repositories
	// over needs, which is a page rather than an API and so cannot be
	// done for them.
	PromptInstall
)

// A Prompt is what the user has to do next. The caller shows it however
// it likes; this package waits for the host to say it was done.
type Prompt struct {
	// Kind is what is being asked for.
	Kind PromptKind

	// URI is the page the user opens.
	URI string

	// Code is what they type into it, for an authorization. The device
	// flow deliberately does not put the code in the URI, so that
	// clicking a link is not on its own enough to authorize anything.
	// An installation has no code.
	Code string

	// Repos are the repositories an installation is wanted for, as
	// "owner/repo".
	Repos []string

	// Expires is when the code, or over's patience, runs out.
	Expires time.Time
}

// A GitHub authorizes over against github.com, or a GitHub Enterprise
// host, using the OAuth device flow.
//
// The device flow is the one that works everywhere: it needs no loopback
// listener and no browser on this machine, so it behaves the same over
// SSH, in a container, and on the machine in front of you.
type GitHub struct {
	// Host is the GitHub host, e.g. "github.com".
	Host string

	// ClientID is the app to authorize as. Defaults to [ClientID].
	ClientID string

	// Scopes is what to request. Defaults to [Scopes].
	Scopes []string

	// HTTP is the client to use. Defaults to http.DefaultClient.
	HTTP *http.Client

	// Now returns the current time. Defaults to time.Now.
	Now func() time.Time

	// Sleep waits, and returns an error if the wait was cut short.
	// Defaults to waiting on ctx. Tests replace it to poll without
	// taking the host's advice on how long to wait.
	Sleep func(ctx context.Context, d time.Duration) error

	// BaseURL overrides the host's web endpoints, for tests.
	BaseURL string

	// APIURL overrides the host's API endpoint, for tests.
	APIURL string
}

func (g *GitHub) host() string {
	if g.Host == "" {
		return DefaultHost
	}
	return g.Host
}

func (g *GitHub) clientID() string {
	if g.ClientID != "" {
		return g.ClientID
	}
	return envOr("OVER_CLIENT_ID", ClientID)
}

// envOr returns an environment variable, or def where it is unset. The
// app over authenticates as is settled at build time, and overridable at
// run time so that a private deployment needs no fork.
func envOr(name, def string) string {
	if v := strings.TrimSpace(os.Getenv(name)); v != "" {
		return v
	}
	return def
}

func (g *GitHub) now() time.Time {
	if g.Now != nil {
		return g.Now()
	}
	return time.Now()
}

func (g *GitHub) client() *http.Client {
	if g.HTTP != nil {
		return g.HTTP
	}
	return http.DefaultClient
}

func (g *GitHub) sleep(ctx context.Context, d time.Duration) error {
	if g.Sleep != nil {
		return g.Sleep(ctx, d)
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// base returns the root of the host's web endpoints. github.com serves
// these from the bare domain; Enterprise serves them from the host
// itself, which is the same shape.
func (g *GitHub) base() string {
	if g.BaseURL != "" {
		return strings.TrimSuffix(g.BaseURL, "/")
	}
	return "https://" + g.host()
}

// api returns the root of the host's API, which github.com serves from a
// separate domain and Enterprise from /api/v3.
func (g *GitHub) api() string {
	if g.APIURL != "" {
		return strings.TrimSuffix(g.APIURL, "/")
	}
	if g.host() == DefaultHost {
		return "https://api.github.com"
	}
	return "https://" + g.host() + "/api/v3"
}

// deviceCode is the host's answer to a request to start authorizing.
type deviceCode struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`
}

// tokenReply is the host's answer to a request for a token, whether by
// polling a device code or by redeeming a refresh token. An error is
// reported in the body with HTTP 200, so both have to be read.
type tokenReply struct {
	AccessToken           string `json:"access_token"`
	RefreshToken          string `json:"refresh_token"`
	ExpiresIn             int    `json:"expires_in"`
	RefreshTokenExpiresIn int    `json:"refresh_token_expires_in"`
	Scope                 string `json:"scope"`
	Error                 string `json:"error"`
	ErrorDescription      string `json:"error_description"`
}

// Authorize sends the user to the host to authorize over, and returns
// the token they granted. show is called once, with what they have to
// do; Authorize then polls until they have done it, the code expires, or
// ctx is cancelled.
func (g *GitHub) Authorize(ctx context.Context, show func(Prompt) error) (*Token, error) {
	if g.clientID() == "" {
		return nil, ErrNoClientID
	}
	form := url.Values{"client_id": {g.clientID()}}
	if scopes := g.Scopes; len(scopes) > 0 {
		form.Set("scope", strings.Join(scopes, " "))
	} else if len(Scopes) > 0 {
		form.Set("scope", strings.Join(Scopes, " "))
	}
	var code deviceCode
	if err := g.post(ctx, g.base()+"/login/device/code", form, &code); err != nil {
		// A host that will not start a device flow at all is nearly
		// always one of two things, and neither is guessable from
		// "404". Say both.
		var se *statusError
		if errors.As(err, &se) && (se.Code == http.StatusNotFound || se.Code == http.StatusUnauthorized) {
			return nil, fmt.Errorf("%w\n  check the client id (%s), "+
				"and that the app has device flow enabled in its settings",
				err, g.clientID())
		}
		return nil, err
	}
	if code.DeviceCode == "" || code.UserCode == "" {
		return nil, fmt.Errorf("%s: did not return a device code", g.host())
	}
	interval := time.Duration(code.Interval) * time.Second
	if interval <= 0 {
		interval = 5 * time.Second
	}
	expires := g.now().Add(time.Duration(code.ExpiresIn) * time.Second)
	if code.ExpiresIn == 0 {
		expires = g.now().Add(15 * time.Minute)
	}
	if err := show(Prompt{
		Kind:    PromptAuthorize,
		URI:     code.VerificationURI,
		Code:    code.UserCode,
		Expires: expires,
	}); err != nil {
		return nil, err
	}

	poll := url.Values{
		"client_id":   {g.clientID()},
		"device_code": {code.DeviceCode},
		"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
	}
	for {
		if err := g.sleep(ctx, interval); err != nil {
			return nil, err
		}
		var reply tokenReply
		if err := g.post(ctx, g.base()+"/login/oauth/access_token", poll, &reply); err != nil {
			return nil, err
		}
		switch reply.Error {
		case "":
			return g.token(ctx, &reply, FromDevice), nil
		case "authorization_pending":
			// The user has not finished yet, which is the normal case.
		case "slow_down":
			interval += 5 * time.Second
		case "expired_token":
			return nil, fmt.Errorf("the code expired before it was entered; run 'over auth' again")
		case "access_denied":
			return nil, ErrDenied
		default:
			return nil, fmt.Errorf("%s: %s", reply.Error, firstNonEmpty(reply.ErrorDescription, "authorization failed"))
		}
		if g.now().After(expires) {
			return nil, fmt.Errorf("the code expired before it was entered; run 'over auth' again")
		}
	}
}

// Renew exchanges a refresh token for a fresh access token. A token that
// cannot be renewed is reported as such, so that the caller can tell
// "ask the user again" from "the network is down".
func (g *GitHub) Renew(ctx context.Context, t *Token) (*Token, error) {
	if !t.Renewable(g.now()) {
		return nil, fmt.Errorf("the credential for %s cannot be renewed; run 'over auth %s'", t.Host, t.Host)
	}
	if g.clientID() == "" {
		return nil, ErrNoClientID
	}
	form := url.Values{
		"client_id":     {g.clientID()},
		"grant_type":    {"refresh_token"},
		"refresh_token": {t.Refresh},
	}
	var reply tokenReply
	if err := g.post(ctx, g.base()+"/login/oauth/access_token", form, &reply); err != nil {
		return nil, err
	}
	if reply.Error != "" {
		return nil, fmt.Errorf("renewing the credential for %s: %s; run 'over auth %s'",
			t.Host, firstNonEmpty(reply.ErrorDescription, reply.Error), t.Host)
	}
	return g.token(ctx, &reply, t.Source), nil
}

// token turns a reply into a stored token, asking the host who it
// belongs to. The login is a convenience for "over auth -status" and its
// absence is not an error: a token that works is a token that works.
func (g *GitHub) token(ctx context.Context, reply *tokenReply, source Source) *Token {
	now := g.now()
	t := &Token{
		Host:     g.host(),
		Access:   reply.AccessToken,
		Refresh:  reply.RefreshToken,
		Scope:    reply.Scope,
		Source:   source,
		Obtained: now,
	}
	if reply.ExpiresIn > 0 {
		t.Expiry = now.Add(time.Duration(reply.ExpiresIn) * time.Second)
	}
	if reply.RefreshTokenExpiresIn > 0 {
		t.RefreshExpiry = now.Add(time.Duration(reply.RefreshTokenExpiresIn) * time.Second)
	}
	if login, err := g.Login(ctx, t); err == nil {
		t.Login = login
	}
	return t
}

// Login asks the host which account a token belongs to.
func (g *GitHub) Login(ctx context.Context, t *Token) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, g.api()+"/user", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+t.Access)
	resp, err := g.client().Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%s/user: %s", g.api(), resp.Status)
	}
	var body struct {
		Login string `json:"login"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", err
	}
	return body.Login, nil
}

// post sends a form and decodes a JSON reply.
func (g *GitHub) post(ctx context.Context, endpoint string, form url.Values, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := g.client().Do(req)
	if err != nil {
		return fmt.Errorf("%s: %w", endpoint, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return &statusError{URL: endpoint, Status: resp.Status, Code: resp.StatusCode}
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("%s: %w", endpoint, err)
	}
	return nil
}

// A statusError is a reply that was not a success, kept typed so that
// the caller can tell a misconfiguration from a host having a bad day.
type statusError struct {
	URL    string
	Status string
	Code   int
}

func (e *statusError) Error() string { return e.URL + ": " + e.Status }

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}
