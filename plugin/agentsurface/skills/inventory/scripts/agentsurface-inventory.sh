#!/usr/bin/env bash
#
# Run agentsurface and print its inventory for a person to read.
#
# This script is the whole of what the plugin executes. It finds the binary,
# runs it, and by default hides the per-item `note:` lines so that a terminal
# session gets an overview rather than several hundred lines. It changes
# nothing else about the output: the categories, the item lines, the source
# paths, the "what this did not look at" section and the closing line all come
# through exactly as the tool printed them.
#
# What it deliberately does not do, because the tool it wraps does not do it
# either: no network call, no upload, no telemetry, no account, no prompt for
# anything. There is nothing in here that talks to a socket.
#
#   agentsurface-inventory.sh              overview, notes hidden
#   agentsurface-inventory.sh --full       every line the tool printed
#   agentsurface-inventory.sh --json       the tool's JSON, unfiltered
#
# Any other argument is passed straight through to agentsurface, so
# --no-baseline works here too.

set -u -o pipefail

REPO_URL="https://github.com/Northbeams-Labs/agentsurface"

# Where to look, in order: an explicit override, then PATH, then the places a
# Homebrew or `go install` install actually lands. The last part matters
# because a Claude Code session does not always inherit the login shell's PATH.
find_binary() {
	if [ -n "${AGENTSURFACE_BIN:-}" ]; then
		printf '%s\n' "$AGENTSURFACE_BIN"
		return 0
	fi

	if command -v agentsurface >/dev/null 2>&1; then
		command -v agentsurface
		return 0
	fi

	local candidate
	for candidate in \
		"${GOBIN:-}/agentsurface" \
		"${GOPATH:-}/bin/agentsurface" \
		"$HOME/go/bin/agentsurface" \
		"/opt/homebrew/bin/agentsurface" \
		"/usr/local/bin/agentsurface" \
		"/home/linuxbrew/.linuxbrew/bin/agentsurface"; do
		case "$candidate" in
		/agentsurface | /bin/agentsurface) continue ;; # empty GOBIN or GOPATH
		esac
		if [ -x "$candidate" ]; then
			printf '%s\n' "$candidate"
			return 0
		fi
	done

	return 1
}

not_installed() {
	cat >&2 <<EOF
agentsurface is not installed on this machine, so there is nothing to run yet.

Install it with either of these, then run the command again:

  go install github.com/Northbeams-Labs/agentsurface/cmd/agentsurface@latest
  brew install Northbeams-Labs/tap/agentsurface

Source, releases and checksums: $REPO_URL

If it is already installed somewhere unusual, point AGENTSURFACE_BIN at the
binary instead, for example:

  AGENTSURFACE_BIN=/opt/tools/agentsurface agentsurface-inventory.sh
EOF
	exit 127
}

mode=compact
args=()
for arg in "$@"; do
	case "$arg" in
	--full)
		mode=full
		;;
	--json)
		mode=raw
		args+=("$arg")
		;;
	*)
		args+=("$arg")
		;;
	esac
done

bin="$(find_binary)" || not_installed

if [ ! -e "$bin" ]; then
	printf 'AGENTSURFACE_BIN points at %s, and there is nothing there.\n' "$bin" >&2
	printf 'Unset it to search PATH instead, or point it at the real binary.\n' >&2
	exit 127
fi

if [ ! -x "$bin" ]; then
	printf 'agentsurface was found at %s but is not executable.\n' "$bin" >&2
	printf 'Fix the permissions on it, or set AGENTSURFACE_BIN to a copy that is.\n' >&2
	exit 126
fi

if [ "$mode" = "compact" ]; then
	"$bin" ${args+"${args[@]}"} | awk '
		/^[[:space:]]+note: / { hidden++; next }
		{ print }
		END {
			if (hidden > 0) {
				printf "\n(%d per-item detail notes were hidden. Re-run with --full for every line.)\n", hidden
			}
		}
	'
	exit "${PIPESTATUS[0]}"
fi

exec "$bin" ${args+"${args[@]}"}
