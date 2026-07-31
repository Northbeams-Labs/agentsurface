#!/usr/bin/env bash
#
# Regenerate docs/demo/agentsurface.gif and docs/demo/agentsurface.png.
#
# Needs Go, and vhs (brew install vhs, which brings ttyd and ffmpeg).
#
#   docs/demo/record.sh
#
# It builds the binary, rebuilds the fixture home from scratch, then records
# the tape. Nothing is edited afterwards: what the binary printed is what the
# recording shows.

set -euo pipefail

cd "$(dirname "$0")/../.."

for tool in go vhs; do
	if ! command -v "$tool" >/dev/null 2>&1; then
		echo "$tool is not on PATH" >&2
		exit 1
	fi
done

VERSION="$(git describe --tags --abbrev=0 2>/dev/null || echo dev)"
make build VERSION="$VERSION"

docs/demo/fixture.sh /tmp/ashome

# Look at what the run will print before spending 25 seconds recording it: that
# it still shows what the demo is for, and that it says nothing about this
# machine.
docs/demo/check.sh

# The tape puts the recorded shell inside a sandbox profile before it runs
# anything; see the comment there and in docs/demo/machine-browsers.sb.
vhs docs/demo/agentsurface.tape

ls -lh docs/demo/agentsurface.gif docs/demo/agentsurface.png
