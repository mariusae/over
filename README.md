# over

`over` manages overlays: sets of files kept in sync with a remote
source. An overlay — a *layer* — is a directory of a git repository,
materialized under a root directory on the local machine.

	$ over add mariusae/config:editors
	added mariusae/config:editors
	$ over sync
	.emacs from mariusae/config:editors
	.zshrc from mariusae/config:editors
	2 updated

`mariusae/config:editors` is the `editors` directory of the
`mariusae/config` repository on GitHub. Its contents land under `$HOME`,
so `editors/.emacs` in the repository becomes `~/.emacs` on disk.

Sync goes both ways. A file that has changed locally is committed back
to its layer:

	$ over sync
	.emacs from mariusae/config:editors
	.zshrc to mariusae/config:editors
	1 updated, 1 written, 13 unchanged

`over fetch` updates over's copy of the layers without writing anything,
so that the status you read is the one you are about to act on:

	$ over fetch
	mariusae/config  5fce44b..bef94f8
	1 repository updated; run 'over status' to see what a sync would do
	$ over status
	.emacs from mariusae/config:editors

## Conservatism

over never silently overwrites a file it did not put there. It records
the contents of every file at the moment it was last synced, and
compares three versions: the layer's, the local one, and that record.
When both sides have moved, the file is in conflict and over does
nothing to it.

	$ over sync
	.emacs conflicts
	1 conflict, 1 updated, 2 written, 13 unchanged

The same applies to a file over has never synced at all: if the layer
provides `.emacs` and you already have one with different contents, that
is a conflict on the very first sync.

Look at the difference, then resolve it in one direction or the other:

	$ over diff
	--- .emacs (local)
	+++ .emacs (mariusae/config:editors)
	@@ -1 +1 @@
	-(setq visible-bell t)
	+(setq inhibit-startup-message nil)

	$ over reset .emacs    # take the layer's copy, discarding the local one
	$ over ack .emacs      # keep the local copy; the next sync writes it to the layer

`reset` does not require a conflict: it always just overwrites, which
makes it the way to undo a local edit as well.

## Starting a layer

`over init` creates a layer: it declares it in the repository's
config.yaml, commits, and pushes. Nothing else is needed to bring one
into existence, so this is the whole of it:

	$ over init mariusae/dotfiles:editors
	created mariusae/dotfiles:editors (root $HOME)
	added mariusae/dotfiles:editors
	track files into it with 'over track mariusae/dotfiles:editors <path>...'
	$ over track mariusae/dotfiles:editors .vimrc .emacs
	$ over sync

The repository has to exist, but it may be empty — a repository just
created and never pushed to has no commits and no branch at all, and
init makes the first commit in it. Creating the repository itself is not
over's business; `gh repo create` does that half.

`-root` sets the root the layer declares for itself, which is where
every machine that adds it will materialize it:

	over init -root /etc mariusae/dotfiles:etc

That is the layer's own root, shared by everyone; `over root` sets a
local override instead. Init edits config.yaml in place, so comments and
anything else in the file are kept.

## What over knows

`over show` reports everything over has on a path: the local file, what
it recorded at the last sync, and every layer with something to say
about it, highest precedence first.

	$ over show .emacs
	.emacs
	    path       /Users/marius/.emacs
	    local      5b75d6976c1f  mode 644  11 bytes  2026-09-12 12:37:48
	    status     conflict
	    owner      mariusae/work:overrides
	    layer      mariusae/work:overrides (owner)
	      root     /Users/marius
	      file     overrides/.emacs
	      content  7722336c16cb  mode 644
	      synced   7722336c16cb  mode 644  2026-09-12 12:36:59
	      commit   bef94f8  2026-09-10 09:12:03  Marius Eriksen
	               overrides: override .emacs
	    layer      mariusae/config:editors (shadowed)
	      root     /Users/marius
	      file     editors/.emacs
	      content  73e03d310574  mode 644
	      synced   (never)
	      commit   5fce44b  2026-09-01 08:00:11  Marius Eriksen
	               editors: initial

The three hashes are the three versions over compares: `local` is what
is on disk, `content` is what the layer holds, and `synced` is what was
there when the two last agreed. A path no layer provides is reported
too, with the reason — excluded, outside every root, or simply never
tracked.

Show takes path arguments like everything else, so `over show
.config/...` reports on a whole tree. It reads from over's cache and
does not fetch.

## Layers

Layers are ordered, and the last layer providing a file owns it — the
usual overlay rule, applied per file. A file provided by an earlier
layer but overridden by a later one is reported as shadowed, and over
leaves the earlier layer's copy alone.

	$ over add mariusae/work:overrides   # added last, so it wins
	$ over add -before mariusae/work:overrides mariusae/config:misc

Each layer has a *root*, the directory its files are materialized under.
The root is expanded for environment variables at sync time, so a layer
rooted at `$HOME` follows whoever runs over.

## Tracking and deleting

Sync discovers new files in a layer by itself. A new *local* file is
over's business only once it has been tracked. Which layer it goes in
decides where it is published and who else receives it, so over does not
guess — the layer is named:

	$ over track mariusae/config:editors .config/ion/config
	.config/ion/config tracked in mariusae/config:editors
	$ over sync
	.config/ion/config to mariusae/config:editors
	1 written

Deleting a tracked file deletes it from the layer, recorded as a
tombstone so that other machines delete their copies too:

	$ rm .zshrc
	$ over sync
	.zshrc deleted in mariusae/config:editors

Tombstones live beside the layer in `<layer>.tombstones.yaml`, and are
removed when the file comes back.

## History

Every commit over makes records where the change came from and why:

	editors: .vimrc

	write editors/.vimrc (ack)

	Over-Host: mariusmac
	Over-User: marius
	Over-Time: 2026-09-12T15:32:58Z
	Over-Version: devel (10ed6a9)

`over log` reads that back:

	$ over log .vimrc
	.vimrc  (mariusae/config:editors, editors/.vimrc)
	    3fc5f69f1fb5  2026-09-12 12:58:39  marius@airy     write (restore)  editors: .vimrc
	    cb45948a48a0  2026-09-12 12:58:02  marius@airy     write (ack)      editors: .vimrc
	    49aac3139b7a  2026-09-12 12:58:01  Other Person                     editors: remote edit
	    8e7d5557153e  2026-09-12 12:58:00  Marius Eriksen                   editors: initial

	restore an earlier version with 'over restore <revision> .vimrc'

The fourth column is what over was doing. An ordinary edit says only
`write`; `ack`, `track`, and `restore` name the deliberate act that put
the local file ahead, which is the explanation for an overwrite that
would otherwise look wrong. Commits made by hand carry none of this, and
log falls back to their git author.

Any of those versions can be put back:

	$ over restore cb45948a48a0 .vimrc
	.vimrc restored from cb45948a48a0 in mariusae/config:editors
	$ over sync
	.vimrc to mariusae/config:editors

Restoring makes an old version current again rather than rewinding
quietly: the restored contents become the local change, and the next
sync publishes them. The layer's history keeps both, and the commit that
publishes the restore says that is what it was. To undo a local edit
without reaching into the history, use `over reset`.

## Exclusions

Some paths should never be overlay content, whatever a layer's root.
They are listed in the client configuration as path patterns:

	$ over exclude
	$HOME/.config/over/...  /Users/marius/.config/over/...
	$HOME/.cache/over/...   /Users/marius/.cache/over/...
	$ over exclude '$HOME/.local/state/...'
	excluded $HOME/.local/state/...

An exclusion is a **standing rule**, not a one-time edit: over skips
these paths on every sync, in every layer, whether or not it has state
for them. That is what separates it from `over untrack`, which forgets
one file once and leaves the next sync free to pick it up again — if the
layer still provides the file, it comes straight back as a pull or a
conflict.

A new configuration is seeded with over's own home and cache. They hold
the state sync writes as it runs, so a layer carrying them would dirty
its own input on every sync and never settle. They are ordinary entries
and can be removed; doing so is a way to make over unstable, not a way
to manage more files. An empty `exclude:` list is respected as written
and is not reseeded.

Excluding a path over already tracks does not take the file out of its
layer. over simply stops looking at it; use `over untrack` to forget it
as well.

## Path arguments

Every command that takes paths takes them the same way. A path names
files by where they live on disk, relative to the working directory
unless it is absolute. The element `...` matches any number of path
elements:

	$ over reset .config/...    # everything tracked under .config
	$ over status ...           # everything over tracks, wherever it is rooted

## Commands

	over init [-root dir] [-no-add] <layer>...
	over add [-before layer] [-root dir] <layer>...
	over rm <layer>...
	over root <layer> [dir]
	over fetch
	over sync [-n] [path...]
	over status [path...]
	over show <path>...
	over diff [path...]
	over log [-n count] <path>...
	over restore <revision> <path>...
	over reset <path>...
	over ack <path>...
	over track <layer> <path>...
	over untrack <path>...
	over exclude [-rm] [path...]

Run `over help <command>` for the details of any of them.

## Configuration

### The client

over keeps its configuration in `$XDG_CONFIG_HOME/over/config.yaml`,
defaulting to `~/.config/over/config.yaml`. It is written by `over add`,
`over rm`, `over root`, and `over exclude`; there is no need to edit it
by hand.

	layers:
	    - layer: mariusae/config:editors
	    - layer: mariusae/ion:config
	      root: /Users/otheruser
	exclude:
	    - $HOME/.config/over/...
	    - $HOME/.cache/over/...

Beside it, `state/<host>/<owner>/<repo>/<layer>.yaml` records what over
saw at the last sync: the revision, the time, and the hash of every
file. The state is what makes conflict detection possible, and it is
the only thing over consults to decide whether a file is its business.

Repository checkouts are cached under `$XDG_CACHE_HOME/over`. They are
disposable: delete one and the next command clones it again.

### The repository

A layer repository provides one directory per layer. Its top-level
`config.yaml` is optional, and configures the layers it provides:

	layers:
	    editors:
	        root: $HOME
	    etc:
	        root: /etc
	sets:
	    mac:
	        - editors
	        - defaults
	        - dotconfig
	    linux:
	        - emacs

A layer need not appear under `layers:` at all; the default root,
`$HOME`, is imputed. A *set* names a group of layers and may be used
wherever a layer name is expected:

	$ over add mariusae/config:mac    # adds editors, defaults, dotconfig
	$ over add mariusae/config        # adds every layer the repository provides

## Environment

	OVER_HOME     over's configuration directory
	OVER_CACHE    the repository cache directory
	OVER_URL      a format string for repository URLs, taking the host,
	              owner, and repository as %[1]s, %[2]s, and %[3]s;
	              the default is https://%[1]s/%[2]s/%[3]s.git

## Building

	go build ./cmd/over     # writes ./over
	go install ./cmd/over   # writes $GOBIN/over

## Testing

	go test ./...
	go vet ./...
	gofmt -l .              # prints files that need formatting; silence is good

The end-to-end tests drive the real `git` command against bare
repositories in a temporary directory, so they need git on the path but
no network.

## Layout

	cmd/over             the over command; a thin main that calls internal/cli
	internal/cli         command registration, flag parsing, and dispatch
	internal/over        the engine: layer resolution, planning, and applying
	internal/config      the client and repository configuration files
	internal/state       per-layer sync state, the base for comparison
	internal/gitrepo     the git command line, wrapped
	internal/spec        layer specifications
	internal/pathspec    path arguments
	internal/diff        unified diffs

## Adding a command

Each command lives in its own file under `internal/cli` and registers
itself from an `init` function:

```go
func init() {
	Register(exampleCmd)
}

var exampleFlagN bool

var exampleCmd = &Command{
	Name:  "example",
	Usage: "example [-n] <arg>",
	Short: "do the example thing",
	Long:  `Example does the example thing to arg.`,
	Flags: func(fs *flag.FlagSet) {
		fs.BoolVar(&exampleFlagN, "n", false, "print what would be done, but do nothing")
	},
	Run: func(ctx context.Context, env *Env, args []string) error {
		if len(args) != 1 {
			return Usagef("expected exactly one argument")
		}
		env.Printf("example %s\n", args[0])
		return nil
	},
}
```

`Run` returns an error to fail with exit status 1, or a `*UsageError`
(from `Usagef`) to print the command's usage and exit with status 2.
Output goes to `env.Stdout` and `env.Stderr` rather than to the process
streams, so commands remain testable; `env.Logf` writes only under the
global `-v` flag.

A command that works on layers and files starts from `load`, which
resolves the configured layers, plans the changes, and filters them by
the command's path arguments.
