package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"
)

func init() {
	Register(credentialCmd)
}

var credentialCmd = &Command{
	Name:   "credential",
	Usage:  "credential <get|store|erase>",
	Short:  "answer git's requests for a credential",
	Hidden: true,
	Long: `Credential is git's credential helper, speaking git's
credential protocol on standard input and output. over points git at
itself with -c credential.helper when it holds a token for the host, so
this is not a command to run by hand.

It is deliberately the dumbest part of over's authorization. It looks up
a token and answers, or answers nothing; it never obtains one. Anything
interactive would be happening inside a subprocess of git, halfway
through whatever over was doing, with its output stream spoken for by
the protocol. Obtaining a credential is over's own business, done at a
moment over chose -- see "over help auth".

"store" and "erase" are accepted and ignored: over keeps its own tokens,
and git has nothing to tell it about them.`,
	Run: runCredential,
}

func runCredential(ctx context.Context, env *Env, args []string) error {
	if len(args) != 1 {
		return Usagef("expected one of get, store, or erase")
	}
	req, err := readCredentialRequest(env.Stdin)
	if err != nil {
		return err
	}
	switch args[0] {
	case "get":
	case "store", "erase":
		// over is the authority on its own tokens.
		return nil
	default:
		return Usagef("%s: expected one of get, store, or erase", args[0])
	}

	host := req["host"]
	if host == "" {
		return nil
	}
	// git sends host:port; a token belongs to the host.
	if i := strings.LastIndex(host, ":"); i >= 0 && !strings.Contains(host[i+1:], "]") {
		host = host[:i]
	}
	o, err := openOver(env)
	if err != nil {
		return err
	}
	username, password, ok, err := o.Credential(ctx, host)
	if err != nil || !ok {
		// Nothing to say. git goes on to whatever is next, and fails
		// in its own words if there is nothing.
		return err
	}
	env.Printf("username=%s\n", username)
	env.Printf("password=%s\n", password)
	return nil
}

// readCredentialRequest reads git's key=value request, which ends at a
// blank line or at end of input.
func readCredentialRequest(r io.Reader) (map[string]string, error) {
	if r == nil {
		return map[string]string{}, nil
	}
	out := map[string]string{}
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			break
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return nil, fmt.Errorf("malformed credential request line %q", line)
		}
		out[key] = value
	}
	return out, sc.Err()
}
