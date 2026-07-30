#!/usr/bin/env bash
#
# no-network.sh checks that agentsurface cannot make network calls.
#
# The claim on the tin is that the binary makes no network calls of any kind.
# A promise in a README is worth nothing, so this script turns it into a check
# that fails the build. It runs identically in CI (.github/workflows/no-network.yml)
# and locally (`make verify`).
#
# It applies four rules.
#
#   1. Denied packages. The dependency graph of the command must not contain a
#      package that can open a socket, negotiate TLS, or start a process.
#
#   2. Allowed exceptions, and only these two. `net/url` and `net/netip` sit
#      under the `net/` prefix but are pure parsers: neither one imports `net`,
#      neither one can open anything. Verify that for yourself with
#      `go list -deps net/url`. They are allowed because a scanner reasonably
#      needs to parse a URL or an address that it read out of a config file.
#
#   3. No dependency outside the standard library and this module. This is what
#      catches a third-party HTTP client, which is a moving target that cannot
#      be enumerated reliably. `resty`, `fasthttp`, `grpc`, a cloud SDK and
#      whatever is written next all trip this rule without being named.
#      The named list below exists only so the error message is specific when
#      one of the common ones shows up.
#
#   4. The compiled artefact, not only the source. Every shipped platform is
#      built and its symbol table inspected with `go tool nm`, so a build-tag
#      guarded import on a platform CI does not run on is still caught.
#
# `os/exec` is denied along with the network packages, for a reason worth
# stating: without it, this check would be trivially defeated by shelling out
# to curl, which no import-graph rule would see. It is also a scanner policy in
# its own right, since agentsurface reads what it finds and never runs it.
#
# What this check does not prove: it is a build-time check, not a sandbox. A
# determined author could still reach the network through raw syscalls, through
# cgo, or through a file written to a location something else executes. Rules 1
# to 4 make an accidental network call impossible and a deliberate one obvious
# in review. They are not a substitute for reading the diff.

set -euo pipefail

MODULE="github.com/Northbeams-Labs/agentsurface"
PKG="./cmd/agentsurface"

# Platforms the check covers. macOS and Linux are shipped. Windows is built by
# CI and not released yet, and is checked here so that the source stays clean
# for the day it is.
PLATFORMS=(
	"darwin/amd64"
	"darwin/arm64"
	"linux/amd64"
	"linux/arm64"
	"windows/amd64"
	"windows/arm64"
)

# Rule 2. The only packages under the net/ prefix that may appear.
ALLOWED_NET_PACKAGES=(
	"net/url"
	"net/netip"
)

# Rule 1. Denied outright, by exact match.
DENIED_EXACT=(
	"net"
	"crypto/tls"
	"os/exec"
)

# Rule 3, naming clause. These are denied by the standard-library rule anyway.
# Listing the common ones only buys a clearer failure message.
DENIED_PREFIXES=(
	"golang.org/x/net"
	"google.golang.org/grpc"
	"github.com/go-resty/resty"
	"github.com/hashicorp/go-retryablehttp"
	"github.com/hashicorp/go-cleanhttp"
	"github.com/valyala/fasthttp"
	"github.com/imroc/req"
	"github.com/parnurzeal/gorequest"
	"github.com/gorilla/websocket"
	"github.com/coder/websocket"
	"nhooyr.io/websocket"
	"github.com/gojek/heimdall"
	"github.com/levigross/grequests"
	"github.com/aws/aws-sdk-go"
	"cloud.google.com/go"
	"github.com/Azure/azure-sdk-for-go"
)

fail=0
tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT

note() { printf '  %s\n' "$1"; }
bad() {
	printf 'FAIL  %s\n' "$1" >&2
	fail=1
}

is_allowed_net_package() {
	local p="$1"
	for a in "${ALLOWED_NET_PACKAGES[@]}"; do
		[ "$p" = "$a" ] && return 0
	done
	return 1
}

check_import_graph() {
	local goos="$1" goarch="$2" label="$3"
	local deps
	if ! deps="$(GOOS="$goos" GOARCH="$goarch" CGO_ENABLED=0 go list -deps -f '{{.ImportPath}} {{.Standard}}' "$PKG" 2>&1)"; then
		bad "$label: go list failed"
		printf '%s\n' "$deps" >&2
		return
	fi

	local path std
	while read -r path std; do
		[ -z "$path" ] && continue

		# Rule 1: denied by exact match.
		for d in "${DENIED_EXACT[@]}"; do
			if [ "$path" = "$d" ]; then
				bad "$label: dependency graph contains denied package '$path'"
			fi
		done

		# Rule 1 continued: anything under net/ that is not on the short
		# allowlist, and anything under crypto/tls or net/http.
		case "$path" in
		net/*)
			if ! is_allowed_net_package "$path"; then
				bad "$label: dependency graph contains network package '$path'"
			fi
			;;
		crypto/tls/* | os/exec/*)
			bad "$label: dependency graph contains denied package '$path'"
			;;
		esac

		# Rule 3, naming clause.
		for d in "${DENIED_PREFIXES[@]}"; do
			case "$path" in
			"$d" | "$d"/*)
				bad "$label: dependency graph contains HTTP client package '$path'"
				;;
			esac
		done

		# Rule 3: nothing outside the standard library and this module.
		if [ "$std" != "true" ]; then
			case "$path" in
			"$MODULE" | "$MODULE"/*) ;;
			*)
				bad "$label: dependency graph contains third-party package '$path'. This module depends on the standard library only. If a dependency is genuinely needed, it has to be agreed in an issue first and this script updated deliberately."
				;;
			esac
		fi
	done <<<"$deps"
}

check_binary_symbols() {
	local goos="$1" goarch="$2" label="$3"
	local out="$tmpdir/agentsurface-$goos-$goarch"
	[ "$goos" = "windows" ] && out="$out.exe"

	if ! GOOS="$goos" GOARCH="$goarch" CGO_ENABLED=0 go build -trimpath -o "$out" "$PKG" 2>"$tmpdir/build.err"; then
		bad "$label: build failed"
		cat "$tmpdir/build.err" >&2
		return
	fi

	local syms
	if ! syms="$(go tool nm "$out" 2>/dev/null)"; then
		bad "$label: could not read the symbol table of the built binary"
		return
	fi

	# Symbols are printed as "<addr> <type> <name>"; names are qualified with
	# their package, for example "net/http.(*Client).Do".
	local hits
	hits="$(printf '%s\n' "$syms" |
		awk '{print $NF}' |
		grep -E '^(net|net/http|net/http/[a-z0-9]+|crypto/tls|net/rpc|net/smtp|net/textproto|os/exec)\.' || true)"

	if [ -n "$hits" ]; then
		bad "$label: the compiled binary contains symbols from a denied package"
		printf '%s\n' "$hits" | head -20 >&2
	fi
}

printf 'no-network check\n'
printf 'module %s, command %s\n\n' "$MODULE" "$PKG"

for platform in "${PLATFORMS[@]}"; do
	goos="${platform%%/*}"
	goarch="${platform##*/}"
	label="$goos/$goarch"
	note "checking $label"
	check_import_graph "$goos" "$goarch" "$label"
	check_binary_symbols "$goos" "$goarch" "$label"
done

printf '\n'

if [ "$fail" -ne 0 ]; then
	cat >&2 <<'EOF'
The no-network check failed.

agentsurface is published on the basis that it makes no network calls, has no
account and uploads nothing. That claim is in README.md, PRIVACY.md and
SECURITY.md, and this check is what makes it checkable rather than asserted.

Do not add an exception to make this pass. If the change genuinely needs
something on the denied list, that is a decision to take in an issue first,
because it changes what the tool is.
EOF
	exit 1
fi

printf 'PASS  no denied package appears in the dependency graph or in any built binary\n'
printf 'PASS  no dependency outside the Go standard library and %s\n' "$MODULE"
printf 'checked %d platforms\n' "${#PLATFORMS[@]}"
