# agentsurface, as a Claude Code plugin

A thin wrapper around the [`agentsurface`](https://github.com/Northbeams-Labs/agentsurface)
command-line tool. It adds one command to Claude Code:

```
/agentsurface:inventory
```

It runs the inventory on your own machine and presents what was found: model
context protocol servers across every client the tool knows, desktop
extensions, plugins, skills, connectors, scheduled agent tasks, instruction
files, and AI-aware browser extensions. For each item it shows where it came
from, who published it according to its own manifest, and what its declared
configuration says it can reach.

It answers one question: **what agent machinery is actually on this machine,
and who put it there.**

## What it does not do

The plugin inherits the tool's refusals. They are the point of the tool, not a
footnote.

- **No network call of any kind.** Not from the tool, not from this plugin.
  The binary's inability to make one is
  [checked in CI on every push](../../.github/workflows/no-network.yml) and a
  release cannot ship if the check fails. The plugin adds a shell script that
  runs the binary and filters its output, and nothing else.
- **No telemetry, no phone home, no version check, no catalogue lookup.**
- **No account, no token, no sign-up, no licence key.** The plugin asks you for
  nothing.
- **No upload.** What the inventory finds stays on your machine. The skill
  carries a standing instruction not to send it anywhere.
- **No risk score, no grade, no letter rating.** It inventories. It does not
  judge whether anything it finds is safe. `not in catalogue` means the
  catalogue snapshot inside the binary has no entry for that item; it does not
  mean anything is wrong with it.
- **It does not act on what it finds.** It will not remove an MCP server,
  disable an extension or edit a config file.
- **It bundles no MCP server** and starts no process other than the
  `agentsurface` binary you installed yourself.

And the honest limits of the inventory itself, which the tool prints at the end
of every run under **What this did not look at**:

1. It reads local files only. A connector attached inside a vendor account, or
   an agent running in a cloud workspace, leaves nothing on this disk and is
   invisible to it. It makes no network call, so it cannot go and ask.
2. It only finds paths somebody has told it about. Clients change, and a path
   this build does not know is a silent gap.
3. Catalogue matching is only as fresh as the installed binary.
4. It reports what a manifest declares, not what code does at runtime.

## Install

The plugin needs the `agentsurface` binary. Install it first, either way:

```sh
go install github.com/Northbeams-Labs/agentsurface/cmd/agentsurface@latest
```

```sh
brew install Northbeams-Labs/tap/agentsurface
```

If the binary is missing, the command does not fail silently: it prints both of
those lines and stops.

Then add the marketplace and install the plugin:

```
/plugin marketplace add Northbeams-Labs/agentsurface
/plugin install agentsurface@northbeams-labs
```

## Use

| Command | What it does |
|---|---|
| `/agentsurface:inventory` | The overview: every item, grouped by category, with its source path. |
| `/agentsurface:inventory --full` | The same, plus the per-item detail notes the overview hides. |
| `/agentsurface:inventory --json` | The tool's JSON document, unfiltered. |
| `/agentsurface:inventory --no-baseline` | Do not read or write the local drift baseline. |

The overview hides the tool's per-item `note:` lines, which on a busy machine
run to several hundred, and says how many it hid. Nothing else about the output
is changed: the categories, item lines, source paths, the **What this did not
look at** section and the closing line all come through as the tool printed
them.

## The one file it writes

`agentsurface` records a local baseline of hashes at
`~/.config/agentsurface/baseline.json` so that a later run can tell you what
changed. Nothing else is written, and nothing is sent anywhere. Pass
`--no-baseline` to switch it off. See
[PRIVACY.md](../../PRIVACY.md).

## What is in here

```
plugin/agentsurface/
├── .claude-plugin/plugin.json                     the manifest
├── skills/inventory/SKILL.md                      the command
├── skills/inventory/scripts/agentsurface-inventory.sh   the only thing that executes
├── LICENSE
├── NOTICE
└── README.md
```

The script is short and worth reading before you run it. That is the habit this
whole project is asking for.

## Requirements

- Claude Code v2.1.129 or later, for the `${CLAUDE_SKILL_DIR}` substitution in
  the skill's `allowed-tools` rule. On an older version the command still
  works; Claude Code just asks you to approve the script each time.
- `bash`, to run the wrapper script. On Windows that means Git Bash or WSL,
  which is what Claude Code's shell tool uses there.

## Licence

Apache-2.0, the same as the tool. See [LICENSE](LICENSE) and [NOTICE](NOTICE).
