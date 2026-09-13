#!/bin/sh
#
# Re-pin bootstrap.sh at the current version of what it installs.
#
#	./repin.sh            the newest over, and the newest thunk
#	./repin.sh v0.2.0     a particular over
#	./repin.sh -n         say what would change, change nothing
#
# The pins are asked for rather than typed. "thunk create" resolves a
# coordinate against the module proxy and writes the manifest it would
# build from, so the version and the module hash come from the thing that
# will do the building -- not from a person copying two hashes across a
# terminal, which is the way a pin comes to name something nobody
# intended.
#
set -eu

cd "$(dirname "$0")"

BOOTSTRAP=bootstrap.sh
PACKAGE=${OVER_PACKAGE:-github.com/mariusae/over/cmd/over}
THUNK_REPO=${THUNK_REPO:-https://github.com/mariusae/thunk.git}
THUNK_BRANCH=${THUNK_BRANCH:-main}

DRY_RUN=false
VERSION=
while [ $# -gt 0 ]; do
	case $1 in
	-h | --help) sed -n '2,12p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
	-n | --dry-run) DRY_RUN=true ;;
	-*) echo "repin: unknown flag $1" >&2; exit 2 ;;
	*) VERSION=$1 ;;
	esac
	shift
done

die() { printf 'repin: %s\n' "$*" >&2; exit 1; }

[ -f "$BOOTSTRAP" ] || die "$BOOTSTRAP is not here"
command -v thunk >/dev/null 2>&1 ||
	die "thunk is not on the path, and it is what resolves the version.
  Install it with $BOOTSTRAP, or pass the pins by hand."

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT HUP INT TERM

# --- over, as thunk resolves it -----------------------------------------

coordinate=$PACKAGE${VERSION:+@$VERSION}
printf 'resolving %s\n' "$coordinate" >&2
thunk create "$coordinate" "$tmp/over" >&2

# field reads one key out of the generated manifest.
field() {
	sed -n "s/^$1 = \"\\(.*\\)\"\$/\\1/p" "$tmp/over" | head -1
}

over_version=$(field version)
over_cksum=$(field cksum)
over_go=$(field toolchain_version)
over_module=$(field module)
over_package=$(field package)

[ -n "$over_version" ] || die "thunk wrote no version; is $PACKAGE a Go module path?"
[ -n "$over_cksum" ] || die "thunk wrote no module hash"

# --- thunk, at the tip of its branch ------------------------------------

thunk_commit=
if [ -z "$VERSION" ]; then
	printf 'resolving %s %s\n' "$THUNK_REPO" "$THUNK_BRANCH" >&2
	thunk_commit=$(git ls-remote "$THUNK_REPO" "refs/heads/$THUNK_BRANCH" | cut -f1)
	[ -n "$thunk_commit" ] ||
		die "$THUNK_REPO has no $THUNK_BRANCH branch"
fi

# --- rewrite --------------------------------------------------------------

# set_pin replaces one "NAME=${NAME:-value}" line, and fails rather than
# silently doing nothing if the line is not there to replace.
set_pin() {
	name=$1 value=$2
	grep -q "^$name=\${$name:-" "$tmp/work" ||
		die "$BOOTSTRAP has no $name to re-pin"
	awk -v name="$name" -v value="$value" '
		index($0, name "=${" name ":-") == 1 { print name "=${" name ":-" value "}"; next }
		{ print }
	' "$tmp/work" > "$tmp/work.new"
	mv "$tmp/work.new" "$tmp/work"
}

cp "$BOOTSTRAP" "$tmp/work"
set_pin OVER_PACKAGE "$over_package"
set_pin OVER_MODULE "$over_module"
set_pin OVER_VERSION "$over_version"
set_pin OVER_CKSUM "$over_cksum"
set_pin OVER_GO "$over_go"
[ -z "$thunk_commit" ] || set_pin THUNK_COMMIT "$thunk_commit"

if cmp -s "$BOOTSTRAP" "$tmp/work"; then
	printf '%s is already current\n' "$BOOTSTRAP" >&2
	exit 0
fi

diff -u "$BOOTSTRAP" "$tmp/work" | sed -n '3,$p' || true

if [ "$DRY_RUN" = true ]; then
	printf '\nnothing was changed (-n)\n' >&2
	exit 0
fi

# Rename rather than write in place: the file may be the one a shell is
# reading, and a half-written script is worse than an old one.
cp "$tmp/work" "$tmp/final"
mv "$tmp/final" "$BOOTSTRAP"
chmod 755 "$BOOTSTRAP"
printf '\nre-pinned %s\n' "$BOOTSTRAP" >&2

# The manifest thunk just wrote is the one bootstrap.sh now writes, so
# checking them against each other is free and catches a bad rewrite.
sh "$BOOTSTRAP" -n >/dev/null 2>&1 ||
	die "$BOOTSTRAP does not run after re-pinning; look at it before committing"
