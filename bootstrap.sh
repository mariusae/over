#!/bin/sh
#
# Bring up a machine from a set of over layers.
#
#	curl -fsSL https://raw.githubusercontent.com/mariusae/over/master/bootstrap.sh |
#		sh -s -- mariusae/env::mac
#
# The argument is anything "over add" takes: a layer, a set of layers, or a
# repository. Everything else is arranged from there.
#
# The installer's whole job is to get one real binary onto the machine.
# After that, nothing is downloaded in compiled form: "over" arrives as a
# thunk -- a dozen lines of TOML naming a commit and a compiler -- and so
# does anything else the layers carry, because a thunk is text and a layer
# holds text. What runs here is therefore short enough to read, which is
# the only reason it is safe to pipe into a shell.
#
#	this script -> thunk         TLS, and a checksum pinned below
#	thunk       -> over          the Go module proxy, sum.golang.org, and
#	                             the module hash recorded in the thunk
#	over        -> everything    commits in your own repositories
#
# Getting over needs no credentials at all: the module proxy serves it over
# HTTPS, and its versions are immutable. Only your layers need a key, and
# only to push.
#
set -eu

# --- what this installer pins -------------------------------------------
#
# Every version here is exact. A bootstrap that floated would hand two
# machines different software and call it the same command.

THUNK_GIT=${THUNK_GIT:-https://github.com/mariusae/thunk.git}
THUNK_COMMIT=${THUNK_COMMIT:-0684594de88fb41b0ae378e360737b2b1179343b}

# A published release is the fast path: one checksum-verified tarball
# rather than a Rust toolchain and a cold compile. Empty means thunk has
# not published one yet, and the source path below is the only path.
THUNK_RELEASE=${THUNK_RELEASE:-}
THUNK_RELEASE_BASE=${THUNK_RELEASE_BASE:-https://github.com/mariusae/thunk/releases/download}

# over comes from the Go module proxy. Module versions are immutable,
# sum.golang.org is a transparency log, and the hash recorded here is
# checked again before every build -- so this needs no credentials and no
# trust in the network. The version is a pseudo-version because over has no
# tags yet; it names one commit exactly all the same.
OVER_PACKAGE=${OVER_PACKAGE:-github.com/mariusae/over/cmd/over}
OVER_MODULE=${OVER_MODULE:-github.com/mariusae/over}
OVER_VERSION=${OVER_VERSION:-v0.0.0-20260913025513-383072556336}
OVER_CKSUM=${OVER_CKSUM:-h1:fASsQWGZKkaXZgBeab6P+SfTVDnXZkePpAKrmgxnO2E=}
OVER_GO=${OVER_GO:-1.27.0}

# Setting OVER_GIT switches to building over from a repository instead,
# which is what a private fork needs: the module proxy cannot reach one.
# OVER_COMMIT must then name a full 40-character commit.
OVER_GIT=${OVER_GIT:-}
OVER_COMMIT=${OVER_COMMIT:-}

# Only used when thunk has to be built here.
RUST_VERSION=${RUST_VERSION:-1.98.1}
RUSTUP_VERSION=${RUSTUP_VERSION:-1.29.1}

BIN_DIR=${BIN_DIR:-$HOME/.local/bin}

# --- plumbing -----------------------------------------------------------

# Everything the installer says goes to stderr, so that stdout stays free
# for anything a caller wants to capture.
say()  { printf '%s\n' "$*" >&2; }
step() { printf '\n%s\n' "$*" >&2; }
die()  { printf 'bootstrap: %s\n' "$*" >&2; exit 1; }
have() { command -v "$1" >/dev/null 2>&1; }

usage() {
	cat >&2 <<'USAGE'
usage: bootstrap [-n] [--bin-dir dir] [--from-source] [<layer>...]

	<layer>         anything "over add" takes: mariusae/env::mac (a set),
	                mariusae/env:editors (one layer), mariusae/env (all of
	                them). Several may be given, lowest precedence first.
	                With none, over is installed and configured by hand.

	-n, --dry-run   print the plan, and the thunk that would be written,
	                without touching anything
	--bin-dir dir   where to put binaries (default ~/.local/bin)
	--from-source   build thunk here rather than fetching a release
USAGE
}

DRY_RUN=false
FROM_SOURCE=false
while [ $# -gt 0 ]; do
	case $1 in
	-h | --help) usage; exit 0 ;;
	-n | --dry-run) DRY_RUN=true ;;
	--from-source) FROM_SOURCE=true ;;
	--bin-dir)
		shift
		[ $# -gt 0 ] || die "--bin-dir wants a directory"
		BIN_DIR=$1
		;;
	--) shift; break ;;
	-*) die "unknown flag $1; try --help" ;;
	*) break ;;
	esac
	shift
done

# The PATH as the caller's shell has it. main puts BIN_DIR on its own PATH
# so that the binaries it installs can be run, which would otherwise make
# the hint at the end think the caller needs no advice.
CALLER_PATH=$PATH

STAGE=$(mktemp -d "${TMPDIR:-/tmp}/over-bootstrap.XXXXXX")
trap 'rm -rf "$STAGE"' EXIT HUP INT TERM

download() {
	if have curl; then
		curl -fsSL --proto '=https' --tlsv1.2 -o "$2" "$1"
	elif have wget; then
		wget -q --https-only -O "$2" "$1"
	else
		die "need curl or wget"
	fi
}

# sha256 prints the hash of a file, with whichever of the two commands
# this system calls it.
sha256() {
	if have sha256sum; then
		sha256sum "$1" | cut -d' ' -f1
	elif have shasum; then
		shasum -a 256 "$1" | cut -d' ' -f1
	else
		die "need sha256sum or shasum to verify what was downloaded"
	fi
}

verify() {
	got=$(sha256 "$1")
	[ "$got" = "$2" ] || die "$1: sha256 is $got, expected $2"
}

# triple is the Rust target triple for this machine, which names both
# thunk's release assets and rustup's.
triple() {
	os=$(uname -s)
	arch=$(uname -m)
	case $os in
	Darwin) os=apple-darwin ;;
	Linux) os=unknown-linux-gnu ;;
	*) die "$os is not supported yet" ;;
	esac
	case $arch in
	arm64 | aarch64) arch=aarch64 ;;
	x86_64 | amd64) arch=x86_64 ;;
	*) die "$arch is not supported yet" ;;
	esac
	printf '%s-%s\n' "$arch" "$os"
}

# cache_dir is where thunk keeps its own cache. A toolchain fetched to
# build thunk lands there rather than in a scratch directory, so that it
# is still around for thunk's first build and not downloaded twice.
cache_dir() {
	if [ -n "${THUNK_CACHE:-}" ]; then
		printf '%s\n' "$THUNK_CACHE"
	elif [ "$(uname -s)" = Darwin ]; then
		printf '%s\n' "$HOME/Library/Caches/thunk"
	else
		printf '%s\n' "${XDG_CACHE_HOME:-$HOME/.cache}/thunk"
	fi
}

# --- thunk --------------------------------------------------------------

install_thunk() {
	if have thunk; then
		say "thunk: already here, at $(command -v thunk)"
		return
	fi
	if [ "$FROM_SOURCE" = false ] && fetch_thunk; then
		return
	fi
	build_thunk
}

# fetch_thunk installs the pinned release, and reports failure so that the
# caller can fall back to building. A missing release is an ordinary state
# of affairs, not an error: thunk has not published one yet.
fetch_thunk() {
	if [ -z "$THUNK_RELEASE" ]; then
		say "thunk: no release is pinned, so it will be built here"
		return 1
	fi
	t=$(triple)
	url=$THUNK_RELEASE_BASE/$THUNK_RELEASE/thunk-$THUNK_RELEASE-$t.tar.gz
	step "thunk: fetching $THUNK_RELEASE for $t"
	if ! download "$url" "$STAGE/thunk.tar.gz"; then
		say "thunk: no prebuilt $THUNK_RELEASE for $t; building it here instead"
		return 1
	fi
	download "$url.sha256" "$STAGE/thunk.sha256" ||
		die "fetched the tarball but not its checksum; refusing to run it unverified"
	verify "$STAGE/thunk.tar.gz" "$(cut -d' ' -f1 <"$STAGE/thunk.sha256")"
	tar xzf "$STAGE/thunk.tar.gz" -C "$STAGE"
	[ -f "$STAGE/thunk" ] || die "the tarball does not contain a thunk binary"
	mkdir -p "$BIN_DIR"
	install -m 755 "$STAGE/thunk" "$BIN_DIR/thunk"
	say "thunk: installed $BIN_DIR/thunk"
}

build_thunk() {
	have git || die "need git to build thunk"
	ensure_cargo
	step "thunk: building from $THUNK_GIT at ${THUNK_COMMIT%"${THUNK_COMMIT#????????????}"}"
	say "      (a few minutes, once)"
	cargo install --quiet --locked \
		--git "$THUNK_GIT" --rev "$THUNK_COMMIT" \
		--root "$STAGE/out" thunk >&2
	mkdir -p "$BIN_DIR"
	install -m 755 "$STAGE/out/bin/thunk" "$BIN_DIR/thunk"
	say "thunk: installed $BIN_DIR/thunk"
}

# ensure_cargo puts a cargo on PATH, installing a pinned Rust toolchain
# under thunk's cache if there is none. It goes through rustup rather than
# unpacking a compiler by hand, for the same reason thunk does: rustup
# already verifies what it downloads, and reimplementing that is how you
# get it wrong.
ensure_cargo() {
	if have cargo; then
		say "cargo: using $(cargo --version 2>/dev/null || echo 'the installed cargo')"
		return
	fi
	t=$(triple)
	root=$(cache_dir)/bootstrap
	export RUSTUP_HOME=$root/rustup CARGO_HOME=$root/cargo
	PATH=$CARGO_HOME/bin:$PATH
	export PATH
	if have cargo; then
		say "cargo: reusing the toolchain in $root"
		return
	fi
	step "cargo: installing Rust $RUST_VERSION under $root"
	say "      (nothing is written outside it, and your PATH is not touched)"
	base=https://static.rust-lang.org/rustup/archive/$RUSTUP_VERSION/$t
	download "$base/rustup-init" "$STAGE/rustup-init"
	download "$base/rustup-init.sha256" "$STAGE/rustup-init.sha256"
	verify "$STAGE/rustup-init" "$(cut -d' ' -f1 <"$STAGE/rustup-init.sha256")"
	chmod 755 "$STAGE/rustup-init"
	"$STAGE/rustup-init" -y --no-modify-path --profile minimal \
		--default-toolchain "$RUST_VERSION" >&2
	have cargo || die "rustup ran but left no cargo on the path"
}

# --- over ---------------------------------------------------------------

# write_over_thunk writes the thunk for over. This is the heart of the
# installer, and it is a heredoc: a thunk is configuration, so bootstrapping
# one needs no tool, no clone, and no resolution step that could pick a
# different version than the one this script names.
write_over_thunk() {
	if [ -n "$OVER_GIT" ]; then
		case $OVER_COMMIT in
		????????????????????????????????????????) ;;
		*) die "OVER_GIT needs OVER_COMMIT set to a full 40-character commit" ;;
		esac
		cat >"$1" <<THUNK
#!/usr/bin/env thunk
thunk = 1
name = "over"

[source]
git = "$OVER_GIT"
commit = "$OVER_COMMIT"

[build]
toolchain = "go"
toolchain_version = "$OVER_GO"
target = "./cmd/over"
THUNK
	else
		# A Go registry source needs no target: the package path is the
		# whole coordinate.
		cat >"$1" <<THUNK
#!/usr/bin/env thunk
thunk = 1
name = "over"

[source]
registry = "go"
package = "$OVER_PACKAGE"
module = "$OVER_MODULE"
version = "$OVER_VERSION"
cksum = "$OVER_CKSUM"

[build]
toolchain = "go"
toolchain_version = "$OVER_GO"
THUNK
	fi
	chmod 755 "$1"
}

# check_layer_access looks at whether the host the layers live on will
# answer, before over gets as far as asking git and git gets as far as
# asking for a password nobody can type. over reaches its repositories over
# SSH by default, and a freshly imaged machine has no key yet.
#
# This only warns. A public layer repository clones fine over HTTPS with no
# credentials at all, which is exactly what a bootstrap does, so a missing
# key is not necessarily a problem -- and quietly rewriting how repositories
# are reached would be worse than letting sync say what went wrong.
check_layer_access() {
	case ${OVER_URL:-} in
	'') host=github.com ;; # over's default, which is SSH
	https://* | http://* | file://*) return 0 ;;
	*) host=${OVER_URL#*@}; host=${host%%[:/]*} ;;
	esac
	if ! have ssh; then
		say "ssh: not installed, so only layers reachable another way will sync"
		return 0
	fi
	rc=0
	ssh -T -o BatchMode=yes -o StrictHostKeyChecking=accept-new \
		"git@$host" >/dev/null 2>&1 || rc=$?
	if [ "$rc" = 255 ]; then
		say "ssh: $host will not authenticate, so a private layer cannot be reached."
		say "     Add a key there, or for public layers reach them over HTTPS:"
		say "       OVER_URL='https://%[1]s/%[2]s/%[3]s.git'"
		say "     Syncing anyway, since that may be all you need."
		return 0
	fi
	say "ssh: $host answers"
}

# --- the plan -----------------------------------------------------------

plan() {
	say "over bootstrap"
	say
	say "  thunk        $THUNK_GIT"
	say "               at ${THUNK_COMMIT%"${THUNK_COMMIT#????????????}"}${THUNK_RELEASE:+, or release $THUNK_RELEASE}"
	if [ -n "$OVER_GIT" ]; then
		say "  over         $OVER_GIT"
		say "               at ${OVER_COMMIT%"${OVER_COMMIT#????????????}"}, built with go $OVER_GO"
	else
		say "  over         go $OVER_PACKAGE"
		say "               $OVER_VERSION, built with go $OVER_GO"
	fi
	say "  binaries     $BIN_DIR"
	if [ $# -eq 0 ]; then
		say "  layers       none named; over will be installed and left to you"
	else
		say "  layers       $*"
	fi
}

main() {
	plan "$@"
	if [ "$DRY_RUN" = true ]; then
		step "the thunk that would be written to $BIN_DIR/over:"
		write_over_thunk "$STAGE/over"
		sed 's/^/  /' "$STAGE/over" >&2
		step "nothing was changed (-n)"
		return 0
	fi

	install_thunk
	PATH=$BIN_DIR:$PATH
	export PATH

	# over runs from the scratch directory for the duration of the
	# bootstrap. The over you keep should come from a layer -- so that
	# one commit moves the whole fleet -- and writing this pin into
	# place first would only put it in conflict with the layer's.
	write_over_thunk "$STAGE/over"
	step "over: building it, from the version this script names"
	"$STAGE/over" version >&2

	if [ $# -eq 0 ]; then
		settle_over
		step "next: make a layer, then bring it down"
		say "  over init mariusae/env:editors"
		say "  over track mariusae/env:editors .zshrc"
		say "  over sync"
		path_hint
		return 0
	fi

	check_layer_access

	step "over: configuring $*"
	"$STAGE/over" add "$@" >&2

	# The first sync on a fresh machine has nothing to publish, so it is
	# a pull in everything but name. over has no flag to say so yet.
	step "over: syncing"
	"$STAGE/over" sync >&2

	settle_over
	path_hint
}

# settle_over decides which over the machine keeps. If a layer provided
# one, that is the one: its pin is the fleet's, and the bootstrap's copy
# was only ever a way to perform the first sync. Otherwise the scratch
# copy is installed, and said to be unmanaged, because an over that no
# layer carries is one you will have to upgrade by hand.
settle_over() {
	if [ -x "$BIN_DIR/over" ] && [ "$BIN_DIR/over" != "$STAGE/over" ]; then
		step "over: $BIN_DIR/over came from a layer, so the bootstrap's copy is dropped"
		say "      that layer pins over for every machine that adds it;"
		say "      re-pin it with 'thunk create' and commit, and they all follow"
		return
	fi
	mkdir -p "$BIN_DIR"
	install -m 755 "$STAGE/over" "$BIN_DIR/over"
	step "over: installed $BIN_DIR/over, which no layer manages"
	say "      a thunk is text, so a layer can carry it:"
	say "        over track <layer> $BIN_DIR/over"
	say "      after that, upgrading over everywhere is a commit"
}

path_hint() {
	case ":${CALLER_PATH}:" in
	*":$BIN_DIR:"*) return 0 ;;
	esac
	step "$BIN_DIR is not on your PATH"
	say "      export PATH=\"$BIN_DIR:\$PATH\""
	say "      a layer carrying your shell configuration does this for you"
}

main "$@"
