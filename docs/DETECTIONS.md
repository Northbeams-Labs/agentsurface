# Detections

What each detector reads, and what it misses.

The second half of each entry is the part worth reading. A scanner that reports
nothing looks identical to a clean machine, so the honest way to describe a
detector is to say where it is blind. Every run prints its own blind spots in a
"What this did not look at" section, and the entries below are the standing
version of the same thing.

Two rules run through all four detectors:

- **They read. They never execute.** A detector will read a configuration file
  that says which command starts an MCP server. It will not start it, call it,
  or shell out to it to learn more. The compiled binary cannot start a process
  at all: `os/exec` is denied by the check in
  [`.github/workflows/no-network.yml`](../.github/workflows/no-network.yml).
- **Declared values only, and never secrets.** Configuration for agent tooling
  is where API keys live. Where a detector reads an environment or headers
  block, it keeps the names and drops the values.

There is no risk score anywhere in this tool, and there will not be one. A score
is a claim, and this project publishes observations.

---

## What it does not do at all

Before the detector list, the four things people reasonably expect and will not
find:

1. **It does not judge whether anything it finds is malicious.** It is an
   inventory. "Not in catalogue" means the snapshot in the binary has no entry,
   not that something is wrong.
2. **It does not detect prompt injection**, and does not attempt to. Where a
   detector reports that an instruction file contains a particular kind of
   wording, that is an observation about text, not a finding of an attack.
3. **It reads local files only.** Anything living purely in a vendor account, a
   cloud workspace or a hosted agent platform leaves nothing on this disk and is
   invisible to it. It makes no network calls, so it cannot go and ask.
4. **Catalogue matching is only as fresh as the binary.** The catalogue snapshot
   ships inside the release. A publisher who appeared after the release will not
   match.

---

## Detector: MCP servers

**Package:** `internal/scan/mcpservers`
**Kinds produced:** `mcp_server`

An MCP server is a program a client is configured to launch, or an endpoint it
is configured to call. There is no registry: every client keeps its own file, in
its own shape, in its own directory. So this detector carries a table of clients
and reads exactly those paths.

### Clients it reads

User scope:

| Client | Where |
|---|---|
| Claude Desktop | `Claude/claude_desktop_config.json` under Application Support (macOS), `%APPDATA%` (Windows), `~/.config` (Linux) |
| Claude Code | `~/.claude.json`, which holds both user-scope servers and a block per project directory |
| Cursor | `~/.cursor/mcp.json` |
| Windsurf | `~/.codeium/windsurf/mcp_config.json` |
| Zed | `~/.config/zed/settings.json` on macOS and Linux, `%APPDATA%/Zed/settings.json` on Windows |
| Cline (command line) | `~/.cline/mcp.json` |
| Cline (editor extension) | the host editor's global storage, for VS Code, VS Code Insiders, Cursor and Windsurf |
| Continue | `~/.continue/mcpServers/*.json` and `~/.continue/config.json` |
| Gemini CLI | `~/.gemini/settings.json` |
| VS Code and VS Code Insiders with GitHub Copilot | `mcp.json` in the default profile and in every named profile, plus the older placement inside `settings.json` |
| JetBrains AI Assistant | `mcp.xml` under the IDE options directory, with the embedded JSON pulled out |

Project scope, found by walking the directories you point the tool at:

| Client | File in the repository |
|---|---|
| Claude Code | `.mcp.json` |
| Cursor | `.cursor/mcp.json` |
| VS Code with GitHub Copilot | `.vscode/mcp.json` |
| Zed | `.zed/settings.json` |
| Continue | `.continue/mcpServers/*.json` |
| Gemini CLI | `.gemini/settings.json` |
| JetBrains Junie | `.junie/mcp/mcp.json` |

Project scope exists because it is the half people forget. Cloning a repository
and opening it in an agent client is enough to have a server declared on your
machine that you never installed.

### What it misses

- **Clients not in the table.** Named rather than summarised, so the list can be
  argued with: OpenAI Codex CLI (TOML configuration), Goose, LM Studio, Amazon Q
  Developer, Kiro, Warp, Cherry Studio, 5ire, BoltAI, and Visual Studio the
  Windows IDE. A server configured only in one of those is invisible.
- **Remote and cloud-hosted servers.** A connector attached inside a vendor
  account is stored on the vendor's side and leaves nothing on this disk.
- **Running processes.** This reads configuration. A server started by hand, by
  a wrapper script, or by another program is running and does not appear.
- **Continue's YAML configuration.** Continue also declares servers in YAML, and
  this tool ships no YAML parser because it takes no third-party dependencies.
  JSON dropped into `.continue/mcpServers` is read. Where YAML files are present,
  the run names them and their count in the gaps section rather than pretending
  they are not there.
- **JetBrains placement.** JetBrains documents the settings dialog but not where
  it writes on disk, so the path used here is inferred. A configuration stored
  under another name is missed.
- **Managed and system-wide configuration.** Configuration deployed by an
  employer through device management is not read.
- **Claude Desktop on Linux.** Anthropic ships macOS and Windows builds only.
  The Linux path checked is the Electron default that community builds use, and
  it is unverified.
- **Depth.** Project scope goes three levels below a directory you name.
  Symlinks pointing out of a root are not followed, and `node_modules`, `vendor`,
  `dist`, `build` and similar directories are skipped.

---

## Detector: installed packages

**Package:** `internal/scan/packages`
**Kinds produced:** `extension`, `plugin`, `skill`, `connector`, `scheduled_task`

The things a client will load again on its next start without anyone installing
anything: desktop extensions, plugins and their marketplaces, skills, connectors
and scheduled agent tasks. This is the category the field mostly ignores, and on
a real machine it is where most of the items are.

### What it reads

- **Desktop extensions.** `Claude Extensions` under the Claude Desktop user data
  directory, with `manifest.json` per extension, plus
  `extensions-installations.json`. Extension settings files are named but not
  opened, because that is where user-entered API keys are stored.
- **Extension bundles on disk.** `.mcpb` and `.dxt` archives in `~/Downloads`
  and the client install directories, so an extension that was downloaded and
  installed is counted once and one that was only downloaded is still visible.
- **Gemini CLI extensions.** `~/.gemini/extensions/*/gemini-extension.json`.
- **Claude Code plugins.** `~/.claude/plugins`, including
  `installed_plugins.json`, `known_marketplaces.json`, `~/.claude/settings.json`,
  and each plugin's `.claude-plugin/plugin.json`. What a plugin brings with it
  (commands, agents, hooks, its own `.mcp.json`, skills) is counted.
- **Project plugins and marketplaces.** `.claude-plugin/plugin.json` and
  `.claude-plugin/marketplace.json` inside the directories you scan.
- **Skills.** `~/.claude/skills/*/SKILL.md` at user scope, `.claude/skills` in a
  project, and skills carried inside a plugin.
- **Connectors.** Entries in client configuration that declare a URL rather than
  a command to run. They are separated from local servers because they fail
  differently: a local server is code on this machine, a connector is a name
  pointing somewhere else, and whoever answers at that address can change
  without anything on this machine changing. Only the declared endpoint is read.
  Header and environment blocks hold tokens, so their names are counted and
  their values are never opened.
- **Scheduled agent tasks.** launchd agents and daemons on macOS, systemd user
  units and timers plus readable crontab files on Linux, and Task Scheduler XML
  under `System32\Tasks` plus the Startup folder on Windows.

Every file read is capped at 1 MiB. A multi-megabyte `manifest.json` is a reason
to stop reading, not to allocate.

### What it misses

- **Reach is what a manifest declares, not what code does.** A package can
  declare nothing and still shell out at runtime.
- **No full-disk search.** Extension bundles are read from the client install
  directories and `~/Downloads` only. A bundle saved anywhere else is not found.
- **Claude Desktop outside macOS and Windows.** On other systems the directory
  checked is the Electron convention and may not exist.
- **Account-side connectors.** A connector added through a Claude account rather
  than a configuration file is held server-side and is not on this machine.
- **Clients with no documented on-disk package format.** Cursor, Windsurf and
  the VS Code agent modes are not inventoried here.
- **Scheduled tasks started indirectly.** Jobs are matched against a fixed list
  of agent binaries. A job that starts an agent through a wrapper script or a
  shell one-liner is not counted.
- **Binary property lists** are not parsed, and per-user cron is not read on
  macOS.
- **Windows registry Run keys** are not read.

---

## Detector: AI browser extensions

**Package:** `internal/scan/browsers`
**Kinds produced:** `browser_extension`

Browser extensions are the emptiest cell in this field, and an AI extension sits
in a place worth knowing about: inside the browser, with whatever page access it
was granted.

### What it reads

Three kinds of file, and nothing else: extension manifests, the profile-level
add-on index Firefox keeps, and native messaging host manifests. Native
messaging hosts matter because they are how a browser extension talks to an
executable on the machine.

**Chromium family:** Google Chrome, Chrome Beta, Chrome Canary, Microsoft Edge,
Brave, Chromium, Vivaldi, Opera, Opera GX and Arc, per profile, plus their
per-user and machine-wide native messaging host directories.

**Firefox:** the profiles listed in `profiles.ini`, the add-on index inside each
profile, and the Mozilla native messaging host directories.

An extension is reported when at least one signal says it is AI-aware, and the
finding records **which signal fired**, so you can disagree with the classifier
rather than trust it. Signals include a match against the shipped identifier
list, declared permissions, the manifest description, and the presence of a
native messaging host.

### What it never opens

History, cookies, local storage, saved passwords, profile display names, and any
other browsing data. Not read, not hashed, not printed. This is a deliberate
limit and it costs real capability: see the next section.

### What it misses

- **The shipped identifier list goes stale.** A new assistant, or one renamed to
  something ordinary, is only caught if its permissions, description or native
  messaging host give it away.
- **Classification is on declarations, not code.** An extension that reaches a
  model over a permission it already holds for another reason will not be
  reported.
- **Enabled or disabled is unknown.** That state lives in the browser's own
  databases, which are deliberately not opened, so every extension present on
  disk is reported whether or not it is currently switched on.
- **Safari** extensions live inside signed application bundles and are not read.
  Tor Browser, Zen, LibreWolf, Waterfox and other forks are not enumerated.
- **Native messaging hosts on Windows** are registered in the registry rather
  than in a directory. This build reads files only, so none are enumerated
  there.
- **Sandboxed installs on Linux.** Flatpak and Snap keep profiles under
  `~/.var/app` and `~/snap` rather than `~/.config`, and those are not scanned.
- **Managed policy.** Force-installed extensions are read where they landed on
  disk. The policy files and registry keys that installed them are not read.

---

## Detector: instruction files

**Package:** `internal/scan/instructions`
**Kinds produced:** `instruction_file`

The files that steer an agent before it does anything: memory, rules and
instruction files. They matter for the same reason project-scope MCP
configuration matters. A repository someone cloned can carry instructions the
user never wrote and never read.

### What it reads

User scope:

| Client | File |
|---|---|
| Claude Code | `~/.claude/CLAUDE.md`, and per-project memory under `~/.claude/projects/*/memory/MEMORY.md` |
| Codex CLI | `~/.codex/AGENTS.md` |
| Windsurf | `~/.codeium/windsurf/memories/global_rules.md` |
| Cline | `~/Documents/Cline/Rules/*.md` |
| GitHub Copilot | `*.instructions.md` in the VS Code and VS Code Insiders user profile, on macOS and Linux |

Project scope, matched anywhere in a scanned tree: `CLAUDE.md`,
`CLAUDE.local.md`, `.claude/CLAUDE.md`, `AGENTS.md`, `.cursorrules`,
`.cursor/rules/*.mdc` and `.md`, `.windsurfrules`, `.windsurf/rules/*.md`,
`.clinerules` as a file or a directory, `.rules` for Zed,
`.github/copilot-instructions.md`, and `.github/instructions/*.instructions.md`.

Each finding records a digest of the file, so a later run reports an instruction
file that changed under you. Notes on a finding are observations about the text,
never a verdict about it.

### What it misses

- **Generated and third-party trees are skipped:** `node_modules`, `.git`,
  `vendor`, `dist`, `build`, `out`, `target`, virtual environments, caches and
  similar. Instruction files inside them are not read.
- **Symbolic links are never followed**, so a file reachable only through a link
  is not read.
- **Only the home directory and the project roots you scan are read.**
  System-wide and enterprise-managed instruction files elsewhere on disk are
  not.
- **Cursor and Zed keep user-level rules in application state** rather than in a
  file on disk, so those rules are not read.
- **Clients not covered:** Gemini CLI, Continue, Roo Code, Junie and Aider.
- **Claude Code subagents, skills and slash commands** also carry instructions
  and are not counted by this detector.
- **Large files are hashed in full but only their first 1 MiB is inspected** for
  imports and wording. Files containing null bytes are skipped as binary and
  reported as skipped.
- **The wording check reports the lines it matched and nothing else.** It cannot
  see an instruction phrased in a way it does not match, and a match is an
  observation rather than a judgement. It is not prompt-injection detection.
- **Bounded walk.** Ten levels below a root, and at most 25,000 directories per
  root. When that budget runs out the run says so rather than pretending the
  tree was covered.

---

## When a detector cannot read something

A path that does not exist is the ordinary case on a machine that does not run
that client. It is silent and is not an error.

Permission denied, or a file that will not parse, is reported in the errors
section with the path, and the run carries on. One detector failing never costs
you the rest of the inventory. Those entries are worth reading: a permission
error means part of your machine was not inventoried.

## Something missing?

Open a
[missed detection issue](https://github.com/Northbeams-Labs/agentsurface/issues/new?template=missed_detection.yml)
with the path and a redacted copy of the file. Take the secrets out first. That
is the most useful thing anyone sends this project.
