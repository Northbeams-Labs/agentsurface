#!/usr/bin/env bash
#
# Authenticode-sign one freshly built Windows binary, in place.
#
#   .github/scripts/sign-windows.sh dist/agentsurface_windows_amd64_v1/agentsurface.exe
#
# GoReleaser calls this as a post-build hook for every target it builds. The
# script decides for itself whether there is anything to do, so the hook can be
# unconditional:
#
#   - not a .exe                 -> nothing to sign, exit 0
#   - no eSigner credentials     -> print a warning, exit 0
#
# The second case is what makes a fork, a pull request and `goreleaser
# --snapshot` work for people who have no access to our signing account. They
# get an unsigned Windows binary and a warning saying so, rather than a failed
# build. The release workflow turns that warning into a failure for a real tag,
# where an unsigned Windows archive must never ship silently.
#
# Signing is done by SSL.com's CodeSignTool against their cloud HSM (eSigner).
# The private key never exists on this machine, or on any machine of ours: the
# binary is uploaded, signed inside the HSM, and the signed copy comes back.
# The same account and the same tool sign the Sentinel installers.
#
# Environment, all four required to sign:
#
#   ESIGNER_USERNAME       SSL.com account
#   ESIGNER_PASSWORD
#   ESIGNER_CREDENTIAL_ID  which certificate in that account
#   ESIGNER_TOTP_SECRET    the TOTP seed, from which the tool derives the code
#
# Optional:
#
#   CODESIGNTOOL_URL       override the download location
#   CODESIGNTOOL_DIR       reuse an already extracted CodeSignTool
#   REQUIRE_WINDOWS_SIGNING=1  turn "no credentials" into an error

set -euo pipefail

binary="${1:?usage: sign-windows.sh <path to binary>}"

case "$binary" in
*.exe) ;;
*)
	exit 0
	;;
esac

if [ ! -f "$binary" ]; then
	echo "sign-windows: no such file: $binary" >&2
	exit 1
fi

missing=""
for var in ESIGNER_USERNAME ESIGNER_PASSWORD ESIGNER_CREDENTIAL_ID ESIGNER_TOTP_SECRET; do
	if [ -z "${!var:-}" ]; then
		missing="$missing $var"
	fi
done

if [ -n "$missing" ]; then
	if [ "${REQUIRE_WINDOWS_SIGNING:-0}" = "1" ]; then
		echo "::error::sign-windows: refusing to ship an unsigned Windows binary. Missing:$missing" >&2
		exit 1
	fi
	echo "::warning::sign-windows: no eSigner credentials, leaving $binary unsigned. Missing:$missing" >&2
	exit 0
fi

# `command -v java` is not enough on macOS, where /usr/bin/java exists as a stub
# that only tells you Java is missing. Run it and see.
if ! java -version >/dev/null 2>&1; then
	echo "sign-windows: CodeSignTool needs a working Java runtime on PATH" >&2
	exit 1
fi

# Resolved before anything changes directory, because the signing step runs
# from inside the CodeSignTool directory and CodeSignTool wants absolute paths.
abs_binary="$(cd "$(dirname "$binary")" && pwd)/$(basename "$binary")"

# CodeSignTool is downloaded once and reused for every binary in the run.
# Downloading it per architecture would double the round trips for no reason.
cst_dir="${CODESIGNTOOL_DIR:-${RUNNER_TEMP:-/tmp}/codesigntool}"
if [ ! -x "$cst_dir/CodeSignTool.sh" ]; then
	url="${CODESIGNTOOL_URL:-https://www.ssl.com/download/codesigntool-for-linux-and-macos/}"
	tmp="$(mktemp -d)"
	echo "sign-windows: downloading CodeSignTool from $url"
	curl -fsSL -o "$tmp/CodeSignTool.zip" "$url"

	# If SSL.com moves the download, curl happily fetches their HTML error page
	# and unzip fails several confusing steps later. Check the zip magic here,
	# where the message can name the actual problem.
	magic="$(head -c 4 "$tmp/CodeSignTool.zip" | od -An -t x1 | tr -d ' \n')"
	if [ "$magic" != "504b0304" ]; then
		echo "sign-windows: $url did not return a zip archive (magic=$magic). The download URL has probably moved." >&2
		head -c 500 "$tmp/CodeSignTool.zip" >&2
		exit 1
	fi

	rm -rf "$cst_dir"
	mkdir -p "$cst_dir"
	unzip -q "$tmp/CodeSignTool.zip" -d "$tmp/unpacked"
	# The archive has a single top level directory whose name carries the
	# version, so it cannot be hardcoded.
	inner="$(find "$tmp/unpacked" -maxdepth 2 -name CodeSignTool.sh -print -quit)"
	if [ -z "$inner" ]; then
		echo "sign-windows: CodeSignTool.sh is not in the downloaded archive" >&2
		exit 1
	fi
	cp -R "$(dirname "$inner")/." "$cst_dir/"
	rm -rf "$tmp"
	# A zip made on Windows carries no Unix permission bits, so the launcher
	# arrives without its executable bit.
	chmod +x "$cst_dir/CodeSignTool.sh"
fi

# CodeSignTool writes the signed file into a directory rather than over the
# input, so it signs into a scratch directory and the result is moved back over
# the original. GoReleaser archives whatever is at the original path.
out="$(mktemp -d)"
trap 'rm -rf "$out"' EXIT

echo "sign-windows: signing $binary"
(
	cd "$cst_dir"
	./CodeSignTool.sh sign \
		-username="$ESIGNER_USERNAME" \
		-password="$ESIGNER_PASSWORD" \
		-credential_id="$ESIGNER_CREDENTIAL_ID" \
		-totp_secret="$ESIGNER_TOTP_SECRET" \
		-input_file_path="$abs_binary" \
		-output_dir_path="$out"
)

signed="$out/$(basename "$binary")"
if [ ! -f "$signed" ]; then
	echo "sign-windows: CodeSignTool reported success but wrote no file to $out" >&2
	ls -la "$out" >&2
	exit 1
fi

# A signed binary is always larger than the one that went in, because the
# signature is appended. Equal size means the tool copied the input through
# without signing it, which has happened when credentials are accepted but the
# certificate is wrong.
before="$(wc -c <"$binary")"
after="$(wc -c <"$signed")"
if [ "$after" -le "$before" ]; then
	echo "sign-windows: signed file is not larger than the input ($before -> $after bytes), so nothing was signed" >&2
	exit 1
fi

mv "$signed" "$binary"
echo "sign-windows: signed $binary ($before -> $after bytes)"
