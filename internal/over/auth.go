package over

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/mariusae/over/internal/auth"
	"github.com/mariusae/over/internal/config"
	"github.com/mariusae/over/internal/gitrepo"
	"github.com/mariusae/over/internal/pathspec"
	"github.com/mariusae/over/internal/spec"
)

// Authorization
//
// over reaches its repositories over HTTPS, which needs no credential to
// read a public one and a bearer token for anything else. Where the
// token comes from is the interesting part, and there are two moments it
// can be wanted:
//
// Reading. A fetch that is refused is a fetch that has changed nothing,
// so over can authorize and try again on the spot. Nothing is half done
// at that point, which is what makes it safe.
//
// Writing. A push happens after local files have been read and commits
// have been made, which is a bad moment to stop and wait for a person.
// But over plans before it applies, and the plan already says which
// layers are to be written -- so over asks for those credentials at the
// barrier between the two, before anything has happened at all.
//
// Both go through [Over.Authorize]. The difference is only when it is
// called, and that is the whole of the design: a credential is obtained
// at a moment over chose, never from inside a subprocess partway through
// the work.

// timeNow is time.Now, replaceable by tests that need a token to have
// expired without waiting for it.
var timeNow = time.Now

// A Need is a credential a command is going to want.
type Need struct {
	// Host is the repository host, e.g. "github.com".
	Host string

	// Write reports that over means to push, not only fetch. Reading a
	// public repository needs nothing; writing one always needs a
	// credential, whoever owns it.
	//
	// It also settles how sure over is of an installation gap. At a
	// write barrier over has already read the layer, so the repository
	// certainly exists and certainly can be reached -- if the app
	// cannot get at it, installing is the answer and over says so
	// plainly. A failed read is ambiguous, and over does not send
	// somebody to an installation page over a typo.
	Write bool

	// Repos are the repositories over means to reach, as "owner/repo".
	// A GitHub App reaches only what it has been installed on, which
	// is a separate act from authorizing it, so knowing the names is
	// what lets over tell the two apart.
	Repos []string
}

// RepoName returns the "owner/repo" a specification names.
func RepoName(s spec.Spec) string { return s.Owner + "/" + s.Repo }

// ErrNoPrompt is returned when a credential is needed and over has no
// way to ask for one, which is what happens in a script, a cron job, or
// anywhere else with nobody watching.
var ErrNoPrompt = errors.New("no terminal to authorize on")

// Store returns the token store. Its directory is over's own and is
// never layer content; see [Over.Vetoes].
func (o *Over) Store() *auth.Store { return o.store }

// WriteNeeds returns the credentials a plan will want in order to
// publish what it has decided to publish. Reads are not included:
// they are cheap to retry, so they are authorized lazily.
func WriteNeeds(changes []Change) []Need {
	var (
		needs []Need
		at    = map[string]int{}
		seen  = map[string]bool{}
	)
	for i := range changes {
		c := &changes[i]
		if c.Status != Push && c.Status != PushDelete {
			continue
		}
		host, repo := c.Layer.Spec.Host, RepoName(c.Layer.Spec)
		j, ok := at[host]
		if !ok {
			j = len(needs)
			at[host] = j
			needs = append(needs, Need{Host: host, Write: true})
		}
		if key := host + "/" + repo; !seen[key] {
			seen[key] = true
			needs[j].Repos = append(needs[j].Repos, repo)
		}
	}
	return needs
}

// Authorize makes sure over holds a usable credential for each need,
// obtaining one where it does not. A need already covered costs nothing,
// so calling it before every barrier is cheap.
//
// Authorizing invalidates the repository cache, because the checkouts
// were opened without a credential helper and have to be reopened with
// one.
func (o *Over) Authorize(ctx context.Context, needs ...Need) error {
	var got bool
	for _, need := range needs {
		if !o.wantsToken(need.Host) {
			continue
		}
		had, err := o.token(ctx, need.Host)
		if err != nil {
			return err
		}
		if had == nil {
			if err := o.obtain(ctx, need.Host); err != nil {
				return err
			}
			got = true
		}
		// A token says who you are; an installation says where over
		// may act. Having the one is no guarantee of the other.
		if err := o.ensureReach(ctx, need.Host, need.Repos, need.Write); err != nil {
			return err
		}
	}
	if got {
		o.repos = map[string]*gitrepo.Repo{}
	}
	return nil
}

// ensureReach checks that the app can actually get at the repositories,
// and sends the user to install it where it cannot.
//
// This only applies to a credential over obtained from the app itself. A
// pasted token or one from the environment is not app-scoped, so there is
// no installation to be missing, and asking the host about one would
// invite a wrong answer.
//
// Diagnosis must never be what breaks an operation: a host that will not
// answer questions about its installations gets the benefit of the
// doubt, and whatever git had to say stands as the error.
func (o *Over) ensureReach(ctx context.Context, host string, repos []string, sure bool) error {
	if len(repos) == 0 {
		return nil
	}
	t, err := o.token(ctx, host)
	if err != nil || t == nil || t.Source != auth.FromDevice {
		return nil
	}
	g := o.github(host)
	reach, err := g.Reach(ctx, t)
	if err != nil {
		return nil
	}
	missing := auth.Missing(reach, repos)
	if len(missing) == 0 {
		return nil
	}
	// Prompt when over is sure enough to be worth somebody's time: at a
	// write barrier, or when the app has not been installed anywhere at
	// all, which leaves nothing else it could be.
	if o.prompt != nil && (sure || reach.Installations == 0) {
		return g.Install(ctx, t, missing, o.prompt)
	}
	return notInstalled(g, reach, missing, host)
}

// notInstalled explains an installation gap over will not act on by
// itself, naming both things it could be.
func notInstalled(g *auth.GitHub, reach *auth.Reach, missing []string, host string) error {
	where := strings.Join(missing, ", ")
	if uri := g.InstallURL(reach.Slug); uri != "" {
		return fmt.Errorf("over is authorized at %s but not installed on %s;\n"+
			"  install it at %s\n"+
			"  (or check the name -- a repository you cannot see looks the same from here)",
			host, where, uri)
	}
	return fmt.Errorf("over is authorized at %s but may not be installed on %s, "+
		"and this build does not know the app's name to offer the page that installs it", host, where)
}

// wantsToken reports whether a token is any use for a host, which it is
// not when over reaches it some way that does not carry one. A layer
// repository on the local file system, or reached over SSH, answers for
// itself.
func (o *Over) wantsToken(host string) bool {
	u := o.URL(spec.Spec{Host: host, Owner: "o", Repo: "r"})
	return strings.HasPrefix(u, "https://") || strings.HasPrefix(u, "http://")
}

// token returns the credential over holds for a host, renewing it if it
// has expired and can be renewed, and nil if there is none to be had
// without asking the user.
func (o *Over) token(ctx context.Context, host string) (*auth.Token, error) {
	if t := envToken(host); t != nil {
		return t, nil
	}
	t, err := o.store.Load(host)
	if err != nil || t == nil {
		return nil, err
	}
	now := timeNow()
	if t.Usable(now) {
		return t, nil
	}
	if !t.Renewable(now) {
		return nil, nil
	}
	fresh, err := o.github(host).Renew(ctx, t)
	if err != nil {
		// A refusal to renew is not fatal: the user can authorize
		// again, which is what the caller will ask for next.
		return nil, nil
	}
	if err := o.store.Save(fresh); err != nil {
		return nil, err
	}
	return fresh, nil
}

// obtain sends the user to the host to authorize over, and stores what
// they granted.
func (o *Over) obtain(ctx context.Context, host string) error {
	if o.prompt == nil {
		return fmt.Errorf("%s: %w; run 'over auth %s' where you can see it, "+
			"or give over a token with 'over auth -token %s'", host, ErrNoPrompt, host, host)
	}
	t, err := o.github(host).Authorize(ctx, o.prompt)
	if err != nil {
		return fmt.Errorf("authorizing over at %s: %w", host, err)
	}
	return o.store.Save(t)
}

// SaveToken records a token the user supplied, in place of authorizing.
// It is how a build with no client id, or a script with a token of its
// own, reaches a private repository.
func (o *Over) SaveToken(ctx context.Context, host, token string) (*auth.Token, error) {
	t := &auth.Token{
		Host:     host,
		Access:   strings.TrimSpace(token),
		Source:   auth.FromPaste,
		Obtained: timeNow(),
	}
	if t.Access == "" {
		return nil, fmt.Errorf("empty token")
	}
	// Ask the host who it belongs to, which is both a check that it
	// works and something to show in "over auth -status".
	if login, err := o.github(host).Login(ctx, t); err == nil {
		t.Login = login
	} else {
		return nil, fmt.Errorf("%s did not accept the token: %w", host, err)
	}
	if err := o.store.Save(t); err != nil {
		return nil, err
	}
	o.repos = map[string]*gitrepo.Repo{}
	return t, nil
}

// Credential returns the credential git should use for a URL, and
// whether there is one. It is what the credential helper answers with.
func (o *Over) Credential(ctx context.Context, host string) (username, password string, ok bool, err error) {
	t, err := o.token(ctx, host)
	if err != nil || t == nil {
		return "", "", false, err
	}
	// GitHub takes the token as the password, with a username that is
	// only required to be present. x-access-token is the convention.
	return "x-access-token", t.Access, true, nil
}

// github returns the client that authorizes over at a host. It is a
// variable so that tests can answer for a host without one existing.
var github = func(host string) *auth.GitHub {
	return &auth.GitHub{Host: host, Now: timeNow}
}

func (o *Over) github(host string) *auth.GitHub { return github(host) }

// gitConfig returns how git should be invoked for a repository.
//
// A credential helper is installed only when over actually holds a token
// for the host. With none, git's own configuration is left entirely
// alone, so a machine whose keychain or helper already answers for the
// host goes on working and is never asked to authorize anything. Once
// over does hold a token, it answers alone: mixing the two would let a
// stale credential elsewhere keep winning the race.
func (o *Over) gitConfig(s spec.Spec) (gitrepo.Config, error) {
	if !o.wantsToken(s.Host) {
		return gitrepo.Config{}, nil
	}
	t, err := o.token(context.Background(), s.Host)
	if err != nil || t == nil {
		return gitrepo.Config{}, err
	}
	exe := o.exe
	if exe == "" {
		if exe, err = os.Executable(); err != nil {
			return gitrepo.Config{}, err
		}
	}
	return gitrepo.Config{Helper: "!" + shellQuote(exe) + " credential"}, nil
}

// withAuth runs f, and if it failed for want of a credential, authorizes
// and runs it once more. This is the lazy half of the design, for reads:
// a fetch that was refused has changed nothing, so there is nothing to
// undo before asking.
//
// There are three ways this ends badly, and they want different things
// said. over could not ask -- say so, and what to run. over asked and
// the second attempt failed anyway -- report that. Or over held a
// credential all along and the host would not take it, which is the one
// nothing here can fix: a revoked token, or one for the wrong account.
func (o *Over) withAuth(ctx context.Context, host string, write bool, repos []string, f func() error) error {
	err := f()
	if err == nil || !o.wantsToken(host) {
		return err
	}
	refused := errors.Is(err, gitrepo.ErrAuth)
	if !refused && !errors.Is(err, gitrepo.ErrNotFound) {
		return err
	}
	had, _ := o.token(ctx, host)
	if had == nil && !refused {
		// A repository reported missing, and no credential to find out
		// whether that is the truth. explain says as much; there is
		// nothing here to authorize against.
		return err
	}
	need := Need{Host: host, Write: write, Repos: repos}
	if aerr := o.Authorize(ctx, need); aerr != nil {
		// The reason over could not authorize leads: it is the part
		// with something to do about it.
		return fmt.Errorf("%w (%w)", aerr, err)
	}
	retry := f()
	if retry == nil || had == nil || !errors.Is(retry, gitrepo.ErrAuth) {
		return retry
	}
	return fmt.Errorf("%s would not accept the credential over holds%s (%w)",
		host, remedy(had, host), retry)
}

// remedy says what to do about a credential a host will not take, which
// depends on where over got it: one from the environment is not over's
// to replace, and telling the user to forget it would not work.
func remedy(t *auth.Token, host string) string {
	if t.Source == auth.FromEnv {
		return fmt.Sprintf(", which comes from %s; unset it to let over authorize instead", t.Scope)
	}
	return fmt.Sprintf("; run 'over auth %s' to authorize again, or 'over auth -rm %s' to forget it", host, host)
}

// explain adds what over knows about a remote failure that git cannot
// know: that a repository reported missing may be one the caller is not
// allowed to see, and that over has no credential to find out.
func (o *Over) explain(s spec.Spec, err error) error {
	if err == nil {
		return nil
	}
	if !errors.Is(err, gitrepo.ErrNotFound) {
		return err
	}
	t, _ := o.token(context.Background(), s.Repository().Host)
	if t != nil || !o.wantsToken(s.Repository().Host) {
		return err
	}
	return fmt.Errorf("%w\n  %s may be private, and over has no credential for %s;\n"+
		"  run 'over auth %s' to authorize, or check the name",
		err, s.Repository(), s.Repository().Host, s.Repository().Host)
}

// envToken returns a token from the environment, which is how a script
// or a build hands over one without a store. $GITHUB_TOKEN is the name
// every other tool uses, so over reads it too.
func envToken(host string) *auth.Token {
	if host != auth.DefaultHost {
		return nil
	}
	for _, name := range []string{"OVER_TOKEN", "GITHUB_TOKEN", "GH_TOKEN"} {
		if v := strings.TrimSpace(os.Getenv(name)); v != "" {
			return &auth.Token{Host: host, Access: v, Source: auth.FromEnv, Scope: "$" + name}
		}
	}
	return nil
}

// HostOf returns the host a git URL names, for the credential helper,
// which git addresses by URL rather than by repository.
func HostOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return ""
	}
	return u.Hostname()
}

// shellQuote quotes a path for the shell git runs a "!" helper with.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// UpdateRepo opens a repository, brings it up to date, and authorizes
// over at its host if that is what the fetch turns out to need.
//
// Every read in over goes through here, which is what makes the lazy
// half of the design a single place rather than a habit: a caller cannot
// forget to handle a refused fetch, because it does not see one.
func (o *Over) UpdateRepo(ctx context.Context, s spec.Spec) (*gitrepo.Repo, error) {
	var repo *gitrepo.Repo
	err := o.withAuth(ctx, s.Host, false, []string{RepoName(s)}, func() error {
		var err error
		if repo, err = o.Repo(ctx, s); err != nil {
			return err
		}
		return repo.Update(ctx)
	})
	if err != nil {
		return nil, o.explain(s, err)
	}
	return repo, nil
}

// PushRepo publishes a repository's commits, authorizing if it must.
//
// A push reached here has commits behind it, so authorizing at this
// point is the fallback and not the plan: [Over.Authorize] with the
// plan's [WriteNeeds] is meant to have settled it before any of the work
// was done. This catches the token that expired in between, and the push
// to a layer no plan predicted.
func (o *Over) PushRepo(ctx context.Context, s spec.Spec, repo *gitrepo.Repo) error {
	return o.explain(s, o.withAuth(ctx, s.Host, true, []string{RepoName(s)}, func() error {
		return repo.Push(ctx)
	}))
}

// SetTransport records how over should reach a host.
func (o *Over) SetTransport(host, transport string) error {
	if host == "" {
		return fmt.Errorf("empty host")
	}
	if err := config.CheckTransport(transport); err != nil {
		return err
	}
	if o.Config.Hosts == nil {
		o.Config.Hosts = map[string]config.Host{}
	}
	h := o.Config.Hosts[host]
	h.Transport = transport
	o.Config.Hosts[host] = h
	// The checkouts were opened against the old transport, and the
	// origin each one points at has to follow.
	o.repos = map[string]*gitrepo.Repo{}
	return o.SaveConfig()
}

// Hosts returns the hosts the answer bears on: every one a configured
// layer lives on, and every one the configuration mentions. Sorted.
func (o *Over) Hosts() ([]string, error) {
	seen := map[string]bool{}
	for host := range o.Config.Hosts {
		seen[host] = true
	}
	for _, entry := range o.Config.Layers {
		s, err := spec.Parse(entry.Layer)
		if err != nil {
			return nil, err
		}
		seen[s.Host] = true
	}
	hosts := make([]string, 0, len(seen))
	for host := range seen {
		hosts = append(hosts, host)
	}
	sort.Strings(hosts)
	return hosts, nil
}

// RepoNames returns the repositories the configured layers live in on a
// host, as "owner/repo". It is what "over auth" checks the app can reach:
// authorizing and installing are separate acts, and the useful thing to
// report is whether the layers this machine actually has are covered.
func (o *Over) RepoNames(host string) ([]string, error) {
	var out []string
	seen := map[string]bool{}
	for _, entry := range o.Config.Layers {
		s, err := spec.Parse(entry.Layer)
		if err != nil {
			return nil, err
		}
		if s.Host != host {
			continue
		}
		name := RepoName(s)
		if seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	sort.Strings(out)
	return out, nil
}

// Vetoes are the paths over refuses to manage whatever the configuration
// says. The exclusion list is the user's to edit -- an empty one is
// respected as written -- but the token store is not an opinion. A
// credential committed to a layer is a credential to revoke, and it
// would be published to every machine that adds the layer.
func (o *Over) Vetoes() []pathspec.Pattern {
	pat, err := pathspec.Parse("/", filepath.Join(o.store.Dir(), "..."))
	if err != nil {
		return nil
	}
	return []pathspec.Pattern{pat}
}
