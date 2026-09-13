package cli

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/mariusae/over/internal/auth"
	"github.com/mariusae/over/internal/over"
)

func init() {
	Register(authCmd, hostCmd)
}

var (
	authFlagStatus bool
	authFlagRm     bool
	authFlagToken  bool
)

var authCmd = &Command{
	Name:  "auth",
	Usage: "auth [-status] [-rm] [-token] [host]",
	Short: "authorize over at a repository host",
	Long: `Auth gets over permission to reach repositories it may not
read anonymously, or may not write at all. The host defaults to
github.com.

Run on its own, it sends you to the host to authorize over and waits:

	$ over auth

	over needs your permission to reach the layers.

	    open  https://github.com/login/device
	    code  C1A2-B3D4

	waiting for you to authorize it (the code lasts 15m)...
	authorized github.com as mariusae

The code is deliberately not in the link. Opening a page cannot on its
own authorize anything; you have to type the code over showed you, which
is what makes it safe to paste a link into a terminal session you do not
entirely trust.

There is usually no need to run this at all. over asks when it finds it
needs to, and it asks at a moment when nothing is half done: before a
sync writes its first file, never partway through. Where there is no
terminal -- a cron job, a script -- over does not wait for somebody who
is not there, and fails saying to run this command. $OVER_AUTH=never
refuses even where there is a terminal.

	over auth -status          what over holds, for every host
	over auth -rm [host]       forget it
	over auth -token [host]    read a token from standard input

The -token flag is for a token you already have: a personal access token,
or one a build was given. It is also how a build of over with no client
id of its own reaches a private repository. over checks the token works
before keeping it.

A token is kept in the auth directory under over's home, readable by
nobody else, and over refuses to manage that directory as layer content
whatever the exclusions say -- a token published to a git repository is a
token to revoke. $OVER_TOKEN, $GITHUB_TOKEN, and $GH_TOKEN are used if
set, in that order, and are never written down.`,
	Flags: func(fs *flag.FlagSet) {
		fs.BoolVar(&authFlagStatus, "status", false, "report what over holds, for every host")
		fs.BoolVar(&authFlagRm, "rm", false, "forget the host's credential")
		fs.BoolVar(&authFlagToken, "token", false, "read a token from standard input instead of authorizing")
	},
	Run: runAuth,
}

func runAuth(ctx context.Context, env *Env, args []string) error {
	if len(args) > 1 {
		return Usagef("expected at most one host")
	}
	host := auth.DefaultHost
	if len(args) == 1 {
		host = args[0]
	}
	o, err := openOver(env)
	if err != nil {
		return err
	}
	switch {
	case authFlagStatus:
		if len(args) > 0 {
			return Usagef("-status reports every host, and takes no argument")
		}
		return authStatus(env, o)
	case authFlagRm:
		removed, err := o.Store().Remove(host)
		if err != nil {
			return err
		}
		if !removed {
			return fmt.Errorf("%s: no credential to forget", host)
		}
		env.Printf("forgot %s\n", host)
		return nil
	case authFlagToken:
		token, err := readToken(env)
		if err != nil {
			return err
		}
		t, err := o.SaveToken(ctx, host, token)
		if err != nil {
			return err
		}
		env.Printf("%s\n", describeToken(t))
		return nil
	}

	if err := o.Authorize(ctx, over.Need{Host: host, Write: true}); err != nil {
		return err
	}
	t, err := o.Store().Load(host)
	if err != nil {
		return err
	}
	if t == nil {
		// Authorize found a credential in the environment and had
		// nothing to do.
		env.Printf("%s is already reachable\n", host)
		return nil
	}
	env.Printf("%s\n", describeToken(t))
	return nil
}

// readToken reads a token from standard input, from a terminal without
// echoing it back if it can help it. It cannot -- turning off echo needs
// a terminal call over has no dependency for -- so it says what it is
// about to do instead of quietly putting a secret on the screen.
func readToken(env *Env) (string, error) {
	if isTerminal(os.Stdin) {
		fmt.Fprintf(env.Stderr, "paste the token and press return (it will be visible): ")
	}
	sc := bufio.NewScanner(env.Stdin)
	if !sc.Scan() {
		if err := sc.Err(); err != nil {
			return "", err
		}
		return "", fmt.Errorf("no token on standard input")
	}
	if isTerminal(os.Stdin) {
		fmt.Fprintln(env.Stderr)
	}
	return strings.TrimSpace(sc.Text()), nil
}

// authStatus reports every credential over holds.
func authStatus(env *Env, o *over.Over) error {
	hosts, err := o.Store().Hosts()
	if err != nil {
		return err
	}
	if len(hosts) == 0 {
		env.Printf("no credentials; over reads public repositories without one\n")
		return nil
	}
	tw := tabwriter.NewWriter(env.Stdout, 0, 4, 2, ' ', 0)
	defer tw.Flush()
	for _, host := range hosts {
		t, err := o.Store().Load(host)
		if err != nil {
			return err
		}
		if t == nil {
			continue
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\n", host, tokenWho(t), tokenWhen(t))
	}
	return nil
}

// describeToken is the one-line report of a credential just obtained.
func describeToken(t *auth.Token) string {
	return fmt.Sprintf("authorized %s%s", t.Host, tokenSuffix(t))
}

func tokenSuffix(t *auth.Token) string {
	if t.Login == "" {
		return ""
	}
	return " as " + t.Login
}

func tokenWho(t *auth.Token) string {
	if t.Login != "" {
		return t.Login
	}
	return "(unknown account)"
}

// tokenWhen says how long a credential has left, since that is the thing
// about it that changes.
func tokenWhen(t *auth.Token) string {
	now := time.Now()
	switch {
	case t.Expiry.IsZero():
		return "does not expire"
	case t.Expired(now) && t.Renewable(now):
		return "expired, renews itself"
	case t.Expired(now):
		return "expired; run 'over auth' again"
	default:
		return "expires in " + time.Until(t.Expiry).Round(time.Minute).String()
	}
}

var hostCmd = &Command{
	Name:  "host",
	Usage: "host [<host> [https|ssh]]",
	Short: "show or set how over reaches a host",
	Long: `Host prints how over reaches its repository hosts, or
changes one when given a transport.

	$ over host
	github.com  https
	$ over host github.com ssh
	github.com  ssh

HTTPS is the default. Reading a public repository over it needs no
credential at all, and the credential anything else needs is one over can
go and get: see "over help auth". That is what makes a fresh machine
usable before it has a key on it.

SSH is for the machine that has a key already and would rather use it.
over asks for nothing in that case, because the key answers for it.

The setting is per host and local to this machine -- which transport
suits a machine is a property of the machine, not of the layers. $OVER_URL
outranks it for every host at once.`,
	Run: runHost,
}

func runHost(ctx context.Context, env *Env, args []string) error {
	o, err := openOver(env)
	if err != nil {
		return err
	}
	switch len(args) {
	case 0:
		return listHosts(env, o)
	case 1:
		env.Printf("%s\t%s\n", args[0], o.Config.Transport(args[0]))
		return nil
	case 2:
		if err := o.SetTransport(args[0], args[1]); err != nil {
			return err
		}
		env.Printf("%s\t%s\n", args[0], o.Config.Transport(args[0]))
		return nil
	default:
		return Usagef("expected a host and an optional transport")
	}
}

// listHosts prints the transport of every host the configuration
// mentions, and of every host a configured layer lives on, since those
// are the ones the answer bears on.
func listHosts(env *Env, o *over.Over) error {
	hosts, err := o.Hosts()
	if err != nil {
		return err
	}
	if len(hosts) == 0 {
		env.Printf("no layers configured, and no host configured\n")
		return nil
	}
	tw := tabwriter.NewWriter(env.Stdout, 0, 4, 2, ' ', 0)
	defer tw.Flush()
	for _, host := range hosts {
		fmt.Fprintf(tw, "%s\t%s\n", host, o.Config.Transport(host))
	}
	return nil
}
