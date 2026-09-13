package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"
)

// AppSlug is the GitHub App's name in a URL, which is what the page that
// installs it is addressed by. Like [ClientID] it is set at build time or
// with $OVER_APP_SLUG.
//
// It has to be configured, and is not merely a refinement. An
// installation reports the slug of the app it installs, so over can learn
// it from one -- but the case an installation prompt exists for is the
// one where there are none, and then there is nothing to learn it from.
// Nor can over ask: the endpoint that reports an app's own slug wants a
// JWT signed with the app's private key, which is the one credential a
// client must never hold.
//
// With no slug, over still detects the gap and says the app may not be
// installed. It just cannot offer the link.
var AppSlug = "overcfg"

// An Installation is one installation of the app: an account that has
// added it, and what it was given access to there.
//
// Installing is not authorizing. A user who authorizes over gets a token
// that identifies them, but a GitHub App reaches only the repositories it
// has been installed on -- and installing is a page the user visits,
// there being no API to do it for them. So the two can be out of step,
// and this is how over notices.
type Installation struct {
	ID int64 `json:"id"`

	// Account is who installed it.
	Account struct {
		Login string `json:"login"`
	} `json:"account"`

	// Selection is "all" when the installation covers every repository
	// of the account, or "selected" when it names them.
	Selection string `json:"repository_selection"`

	// Slug is the app's name in a URL, as the host reports it. It is
	// how over learns the slug when it was not built with one.
	Slug string `json:"app_slug"`
}

// Installations returns the installations of the app that the user can
// reach.
func (g *GitHub) Installations(ctx context.Context, t *Token) ([]Installation, error) {
	var body struct {
		Installations []Installation `json:"installations"`
	}
	if err := g.get(ctx, t, "/user/installations?per_page=100", &body); err != nil {
		return nil, err
	}
	return body.Installations, nil
}

// InstallationRepos returns the repositories an installation covers, as
// "owner/repo".
func (g *GitHub) InstallationRepos(ctx context.Context, t *Token, id int64) ([]string, error) {
	var out []string
	for page := 1; ; page++ {
		var body struct {
			TotalCount   int `json:"total_count"`
			Repositories []struct {
				FullName string `json:"full_name"`
			} `json:"repositories"`
		}
		path := fmt.Sprintf("/user/installations/%d/repositories?per_page=100&page=%d", id, page)
		if err := g.get(ctx, t, path, &body); err != nil {
			return nil, err
		}
		for _, r := range body.Repositories {
			out = append(out, r.FullName)
		}
		if len(body.Repositories) == 0 || len(out) >= body.TotalCount {
			break
		}
	}
	return out, nil
}

// A Reach is what the app can get at, as far as a token can tell.
type Reach struct {
	// Installations is how many installations of the app the user can
	// reach. None at all is the unambiguous case: the app was
	// authorized and never installed.
	Installations int

	// Repos are the repositories those installations cover, lowercased
	// for comparison. It is empty when every installation covers
	// everything, which [Reach.Covers] accounts for.
	Repos map[string]bool

	// All reports that some installation covers every repository of its
	// account, in which case over cannot enumerate what it reaches and
	// does not guess.
	All bool

	// Slug is the app's slug as the host reported it, if it did.
	Slug string
}

// Covers reports whether the app can reach a repository, named
// "owner/repo". An installation that covers everything makes this true
// for anything, which is the honest answer: over cannot tell from here,
// and saying no would send the user to reinstall something that is
// already there.
func (r *Reach) Covers(repo string) bool {
	if r.All {
		return true
	}
	return r.Repos[strings.ToLower(repo)]
}

// Reach reports what the app can get at with a token.
func (g *GitHub) Reach(ctx context.Context, t *Token) (*Reach, error) {
	installs, err := g.Installations(ctx, t)
	if err != nil {
		return nil, err
	}
	out := &Reach{Installations: len(installs), Repos: map[string]bool{}}
	for _, in := range installs {
		if out.Slug == "" {
			out.Slug = in.Slug
		}
		if in.Selection == "all" {
			out.All = true
			continue
		}
		repos, err := g.InstallationRepos(ctx, t, in.ID)
		if err != nil {
			return nil, err
		}
		for _, repo := range repos {
			out.Repos[strings.ToLower(repo)] = true
		}
	}
	return out, nil
}

// InstallURL returns the page that installs the app, or "" when the slug
// is not known. reported is the slug the host gave, which is used when
// this build was not given one.
func (g *GitHub) InstallURL(reported string) string {
	slug := envOr("OVER_APP_SLUG", AppSlug)
	if slug == "" {
		slug = reported
	}
	if slug == "" {
		return ""
	}
	return g.base() + "/apps/" + slug + "/installations/new"
}

// Install sends the user to install the app and waits until the named
// repositories are reachable. The token does not change: an installation
// is not a permission the token carries but a place it may be used, so
// the same token starts working when the page is submitted.
func (g *GitHub) Install(ctx context.Context, t *Token, repos []string, show func(Prompt) error) error {
	reach, err := g.Reach(ctx, t)
	if err != nil {
		return err
	}
	uri := g.InstallURL(reach.Slug)
	if uri == "" {
		return fmt.Errorf("this build of over does not know the app's name, so it cannot offer the page that installs it")
	}
	deadline := g.now().Add(10 * time.Minute)
	if err := show(Prompt{
		Kind:    PromptInstall,
		URI:     uri,
		Repos:   repos,
		Expires: deadline,
	}); err != nil {
		return err
	}
	for {
		if err := g.sleep(ctx, 3*time.Second); err != nil {
			return err
		}
		reach, err := g.Reach(ctx, t)
		if err != nil {
			return err
		}
		if missing := Missing(reach, repos); len(missing) == 0 {
			return nil
		}
		if g.now().After(deadline) {
			return fmt.Errorf("gave up waiting for over to be installed on %s",
				strings.Join(repos, ", "))
		}
	}
}

// Missing returns the repositories a reach does not cover, sorted.
func Missing(r *Reach, repos []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, repo := range repos {
		if r.Covers(repo) || seen[repo] {
			continue
		}
		seen[repo] = true
		out = append(out, repo)
	}
	sort.Strings(out)
	return out
}

// get reads a JSON document from the host's API.
func (g *GitHub) get(ctx context.Context, t *Token, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, g.api()+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+t.Access)
	resp, err := g.client().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s%s: %s", g.api(), path, resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
