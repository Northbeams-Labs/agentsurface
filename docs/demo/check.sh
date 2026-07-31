#!/usr/bin/env bash
#
# Check what the recording will show, before recording it.
#
# The recording is only safe to publish if the run behind it says nothing about
# the machine it ran on, and only worth publishing if it still shows the things
# the demo is meant to show. This runs the same command the tape runs and
# asserts both.
#
#   docs/demo/check.sh
#
# Run it after any change to the fixture, and after any change to the tool's
# output.

set -euo pipefail

cd "$(dirname "$0")/../.."

ROOT="$PWD"
BIN="$ROOT/bin/agentsurface"
FIXTURE="/tmp/ashome"
OUT="$(mktemp -t agentsurface-demo-check)"
trap 'rm -f "$OUT"' EXIT

if [ ! -x "$BIN" ]; then
	echo "build it first: make build" >&2
	exit 1
fi

docs/demo/fixture.sh "$FIXTURE" >/dev/null

run() {
	if command -v sandbox-exec >/dev/null 2>&1; then
		sandbox-exec -f "$ROOT/docs/demo/machine-browsers.sb" "$BIN" "$@"
	else
		"$BIN" "$@"
	fi
}

(cd "$FIXTURE/work/orchard-checkout" && HOME="$FIXTURE" run --no-baseline) >"$OUT"

fail=0
note() {
	echo "FAIL: $1" >&2
	fail=1
}

# What the demo has to show. Each of these is a category or a detail the
# recording is there to demonstrate; losing one silently would leave a demo
# that no longer demonstrates anything.
for want in \
	"items found across" \
	"AI browser extensions" \
	"Quillon Sidekick" \
	"com.quillon.agent" \
	"Desktop extensions" \
	"Brightlark Runner" \
	"Fenwold Desk Script" \
	"applescript" \
	"Instruction files" \
	"permission-bypassing flag" \
	"Model context protocol servers" \
	"Claude Desktop" \
	"Cursor" \
	"Zed" \
	"Skills" \
	"not in catalogue" \
	"Could not read" \
	"What this did not look at"; do
	grep -qF "$want" "$OUT" || note "the run no longer prints \"$want\""
done

# What it must never show. The fixture lives under /tmp, so any other absolute
# path is something that leaked from the machine this ran on. The two
# machine-wide browser directories are denied by machine-browsers.sb and are
# reported as unreadable by name; that is the one known exception.
if grep -q "$HOME/" "$OUT"; then
	note "the run printed a path inside the real home directory"
fi
if grep -q "/Users/" "$OUT"; then
	note "the run printed a path under /Users"
fi
# The exception is written as the whole line it is allowed to be, not as a
# path fragment: a fragment would also excuse a finding read out of one of
# those directories, which is exactly what must never appear.
leaked="$(grep -E '(^|[[:space:]])/(Library|System|Applications|etc|opt|var|private)' "$OUT" |
	grep -vE '^  browsers: /[^:]*NativeMessagingHosts$' |
	grep -vE '^ +open /[^:]*NativeMessagingHosts: operation not permitted$' || true)"
if [ -n "$leaked" ]; then
	note "the run printed an absolute path outside the fixture:"
	echo "$leaked" >&2
fi

if [ "$fail" -ne 0 ]; then
	exit 1
fi

echo "demo run is clean: $(grep -c '' "$OUT") lines, nothing from this machine except the two denied directories"
