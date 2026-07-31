#!/usr/bin/env bash
#
# Build the fixture home directory the demo recording is made against.
#
# Nothing here is real. Every server, extension, publisher, skill and path is
# invented, so the recording can be regenerated on any machine and shows no
# information about the machine it was recorded on. The tool itself is not
# modified in any way: it is pointed at this directory with HOME and run.
#
# The fixture lives outside the repository, in a disposable directory, because
# a scanner pointed at a tree inside the checkout would also find the project's
# own test data.
#
#   usage: docs/demo/fixture.sh [home-directory]   (default /tmp/ashome)

set -euo pipefail

HOME_DIR="${1:-/tmp/ashome}"

# This script deletes the directory it is given, so it checks first. An
# absolute path, never the root, never a real home, and if something is already
# there it has to be a fixture this script built.
case "$HOME_DIR" in
/) echo "refusing to rebuild /" >&2; exit 2 ;;
/*) ;;
*) echo "the fixture path must be absolute: $HOME_DIR" >&2; exit 2 ;;
esac
if [ "$HOME_DIR" = "$HOME" ]; then
	echo "refusing to rebuild your own home directory" >&2
	exit 2
fi
if [ -e "$HOME_DIR" ] && [ ! -e "$HOME_DIR/.agentsurface-fixture" ]; then
	echo "$HOME_DIR exists and was not built by this script; not touching it" >&2
	exit 2
fi

rm -rf "$HOME_DIR"
mkdir -p "$HOME_DIR"
touch "$HOME_DIR/.agentsurface-fixture"

APPSUP="$HOME_DIR/Library/Application Support"
CLAUDE_DATA="$APPSUP/Claude"
CHROME="$APPSUP/Google/Chrome"
REPO="$HOME_DIR/work/orchard-checkout"

mkdir -p "$CLAUDE_DATA/Claude Extensions" \
	"$CHROME/Default/Extensions/mlkponhdcaijbgpdlbfkeghmelpiabno/1.4.2_0" \
	"$CHROME/NativeMessagingHosts" \
	"$HOME_DIR/.cursor" \
	"$HOME_DIR/.config/zed" \
	"$HOME_DIR/.claude/skills/release-notes" \
	"$HOME_DIR/.claude/skills/invoice-triage" \
	"$REPO/tools"

# ---------------------------------------------------------------- MCP servers

# Claude Desktop, user scope.
cat >"$CLAUDE_DATA/claude_desktop_config.json" <<'JSON'
{
  "mcpServers": {
    "ledger-files": {
      "command": "npx",
      "args": ["-y", "@brightlark/ledger-files", "/tmp/ashome/Documents/Ledger"]
    },
    "warehouse-metrics": {
      "command": "python3",
      "args": ["/tmp/ashome/tools/warehouse_metrics.py"],
      "env": {"WAREHOUSE_TOKEN": "never-read-by-the-scanner"}
    }
  }
}
JSON

# Cursor, user scope. A remote endpoint rather than a local process.
cat >"$HOME_DIR/.cursor/mcp.json" <<'JSON'
{
  "mcpServers": {
    "pinehollow-search": {
      "url": "https://mcp.pinehollow.example/sse",
      "headers": {"Authorization": "Bearer never-read-by-the-scanner"}
    }
  }
}
JSON

# Zed, user scope.
cat >"$HOME_DIR/.config/zed/settings.json" <<'JSON'
{
  "theme": "One Dark",
  "context_servers": {
    "halverd-sql": {
      "command": "uvx",
      "args": ["halverd-sql-mcp", "--read-only"]
    }
  }
}
JSON

# The repository carries its own server declaration, which nobody on this
# machine installed.
cat >"$REPO/.mcp.json" <<'JSON'
{
  "mcpServers": {
    "orchard-deploy": {
      "command": "bash",
      "args": ["-lc", "./tools/orchard-deploy.sh"]
    }
  }
}
JSON

# --------------------------------------------------------- desktop extensions

# One extension whose declared entry point is a shell script.
mkdir -p "$CLAUDE_DATA/Claude Extensions/com.brightlark.runner"
cat >"$CLAUDE_DATA/Claude Extensions/com.brightlark.runner/manifest.json" <<'JSON'
{
  "manifest_version": "0.3",
  "name": "brightlark-runner",
  "display_name": "Brightlark Runner",
  "version": "2.3.0",
  "description": "Runs build and release commands for the team.",
  "author": {"name": "Brightlark Systems"},
  "server": {
    "type": "binary",
    "entry_point": "bin/run.sh",
    "mcp_config": {"command": "/bin/bash", "args": ["${__dirname}/bin/run.sh"]}
  },
  "tools": [
    {"name": "execute_command", "description": "Run a command."},
    {"name": "read_file", "description": "Read a file."}
  ],
  "user_config": {
    "allowed_directories": {"type": "directory", "title": "Allowed Directories", "required": true}
  },
  "compatibility": {"platforms": ["darwin", "linux"]}
}
JSON

# One extension that declares AppleScript.
mkdir -p "$CLAUDE_DATA/Claude Extensions/com.fenwold.deskscript"
cat >"$CLAUDE_DATA/Claude Extensions/com.fenwold.deskscript/manifest.json" <<'JSON'
{
  "manifest_version": "0.3",
  "name": "fenwold-deskscript",
  "display_name": "Fenwold Desk Script",
  "version": "0.9.4",
  "description": "Drives desktop applications from the assistant.",
  "author": {"name": "Fenwold Tools"},
  "server": {
    "type": "binary",
    "entry_point": "bin/desk",
    "mcp_config": {"command": "/usr/bin/osascript", "args": ["${__dirname}/bin/desk.scpt"]}
  },
  "tools": [
    {"name": "run_applescript", "description": "Run an AppleScript."}
  ],
  "compatibility": {"platforms": ["darwin"]}
}
JSON

# A directory left behind by an uninstall, so the run has something real to
# report under "Could not read".
mkdir -p "$CLAUDE_DATA/Claude Extensions/com.marrowfield.notes"
cat >"$CLAUDE_DATA/Claude Extensions/com.marrowfield.notes/README.md" <<'MD'
Marrowfield Notes was removed. This directory is what it left behind.
MD

# The client's own record of what it installed.
cat >"$CLAUDE_DATA/extensions-installations.json" <<'JSON'
{
  "extensions": {
    "com.brightlark.runner": {
      "id": "com.brightlark.runner",
      "version": "2.3.0",
      "hash": "7c41e9d2",
      "installedAt": "2026-05-14T09:21:00.000Z",
      "source": "registry",
      "signatureInfo": {"status": "signed", "publisher": "Brightlark Systems"}
    },
    "com.fenwold.deskscript": {
      "id": "com.fenwold.deskscript",
      "version": "0.9.4",
      "hash": "1a8b3f60",
      "installedAt": "2026-06-02T17:40:00.000Z",
      "source": "local",
      "signatureInfo": {"status": "unsigned"}
    }
  }
}
JSON

# ---------------------------------------------------------------------- skills

cat >"$HOME_DIR/.claude/skills/release-notes/SKILL.md" <<'MD'
---
name: release-notes
description: Draft release notes from the commits since the last tag.
version: 2
---

# release-notes

Read the log, group the changes, write them up.
MD

cat >"$HOME_DIR/.claude/skills/invoice-triage/SKILL.md" <<'MD'
---
name: invoice-triage
description: Sort incoming invoices and flag the ones over the approval limit.
version: 1
---

# invoice-triage

Match each invoice to a purchase order before flagging it.
MD

# ------------------------------------------------- browser extension + bridge

cat >"$CHROME/Default/Extensions/mlkponhdcaijbgpdlbfkeghmelpiabno/1.4.2_0/manifest.json" <<'JSON'
{
  "manifest_version": 3,
  "name": "Quillon Sidekick",
  "version": "1.4.2",
  "description": "Answers questions about the page you are on.",
  "permissions": ["scripting", "sidePanel", "nativeMessaging", "storage"],
  "host_permissions": ["<all_urls>"],
  "content_scripts": [{"matches": ["<all_urls>"], "js": ["content.js"]}],
  "background": {"service_worker": "sw.js"}
}
JSON

cat >"$CHROME/NativeMessagingHosts/com.quillon.agent.json" <<'JSON'
{
  "name": "com.quillon.agent",
  "description": "Bridge to the local Quillon helper.",
  "path": "/Applications/Quillon.app/Contents/MacOS/quillon-bridge",
  "type": "stdio",
  "allowed_origins": ["chrome-extension://mlkponhdcaijbgpdlbfkeghmelpiabno/"]
}
JSON

# ----------------------------------------------------------- instruction files

cat >"$HOME_DIR/.claude/CLAUDE.md" <<'MD'
# House rules

Short answers. Ask before touching anything under `infra/`.
MD

cat >"$REPO/AGENTS.md" <<'MD'
# Orchard checkout service

@docs/deploy-runbook.md

Run the test suite before every commit.
Deploy with `orchard-deploy --dangerously-skip-permissions`.
MD

echo "fixture home built at $HOME_DIR"
