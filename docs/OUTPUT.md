# Output

`agentsurface` prints a readable summary by default and a JSON document with
`--json`. Both always end with what the run did not look at.

## Flags

| Flag | Effect |
|---|---|
| `--json` | Print the whole result as JSON on standard output instead of the summary. |
| `--no-baseline` | Do not read or write the local drift baseline. Drift is not reported. See [PRIVACY.md](../PRIVACY.md). |
| `--version` | Print `agentsurface <version>` and exit. |

Exit status is `0` for a completed run and `2` when the run could not start, for
example when the home directory cannot be determined. A finding is not an error,
so a machine with a hundred MCP servers still exits `0`. A detector that fails
does not fail the run either: it is reported in the errors section and the rest
of the inventory is still printed.

## Text output

Written for a person to read on a terminal. The exact shape:

```
agentsurface v0.1.0  darwin
4 items found across 3 categories

AI browser extensions (1)
  Example Assistant                Google Chrome, example.com, can reach: browser_tabs clipboard, not in catalogue
                                   ~/Library/Application Support/Google/Chrome/Default/Extensions/abcdefghijklmnop/2.4.1_0/manifest.json

Instruction files (1)
  AGENTS.md                        agents.md clients, not in catalogue
                                   ~/code/checkout-service/AGENTS.md

Model context protocol servers (2)
  filesystem                       Claude Desktop, can reach: shell filesystem
                                   ~/Library/Application Support/Claude/claude_desktop_config.json
  internal-deploy-tools            Cursor, can reach: shell network, not in catalogue
                                   ~/code/checkout-service/.cursor/mcp.json

6 notes about what these items declare are not shown. Run with -verbose for them.

Changed since the last run (1)
  internal-deploy-tools            ~/code/checkout-service/.cursor/mcp.json

Could not read (1)
  browsers: ~/Library/Application Support/Firefox/Profiles permission denied

What this did not look at
  browser extensions: classification: extensions are judged on what they declare, not on what their code does
  remote and cloud hosted servers: a server connected inside a vendor account leaves nothing on this disk
  running processes: this reads configuration files only

This tool inventories what is installed. It does not judge whether any of it is safe.
```

Reading it:

- **Header.** Tool version and the operating system the scan ran against, then a
  count of items and of categories.
- **Categories**, in alphabetical order of their heading, each with a count.
  Items inside a category are sorted by name. A category with nothing in it is
  not printed at all.
- **First line of an item:** its name, truncated to 32 characters, then the
  client it belongs to, the publisher if one is declared, what the declared
  configuration says it can reach, and `not in catalogue` if the shipped
  catalogue snapshot has no entry for it. Any of those parts can be absent.
- **Second line:** the exact path the item was found at, so you can go and read
  it yourself. This is the most useful line on the page.
- **`note:` lines:** plain observations, never verdicts. They are printed only
  under `-verbose`. The default prints one line saying how many it held back,
  because a laptop with real agent tooling on it produces hundreds of notes and
  a summary nobody reaches the end of hides its own counts.
- **Changed since the last run:** items whose declared definition differs from
  the previous run on this machine. Absent when there is no baseline or nothing
  changed.
- **Could not read:** files a detector could not open or parse. This section
  existing is normal, and it matters: a permission error means part of the
  machine was not inventoried.
- **What this did not look at:** the blind spots, printed on every run whether it
  went well or badly.

`not in catalogue` means the snapshot shipped inside the binary has no entry for
that item. It does not mean the item is unknown to the world, and it certainly
does not mean it is suspect. The snapshot is only as fresh as the release.

**Wrapping.** Prose is wrapped at 100 columns, and a wrapped line is indented to
line up under the text it continues, so a long note reads as one block instead
of leaving a word stranded at the left margin. The width is fixed rather than
read from the terminal, so that a run into a pager and a run into a file produce
the same bytes.

A path is never broken, because it is the thing you will copy, so the second
line of an item can be longer than 100 columns. Under "Could not read" the
scanner and the path stay together on one line for the same reason; the message
follows on that line when it fits and underneath it, lined up under the path,
when it does not.

The text format is meant for people. It is not a stable interface, and it will
change. If you are parsing it, use `--json` instead.

## JSON output

`--json` writes one JSON document to standard output, indented with two spaces,
with a trailing newline. It is the interface to build on.

```json
{
  "tool": "agentsurface",
  "version": "v0.1.0",
  "os": "darwin",
  "findings": [
    {
      "kind": "mcp_server",
      "name": "filesystem",
      "client": "Claude Desktop",
      "scope": "user",
      "source": "~/Library/Application Support/Claude/claude_desktop_config.json",
      "reach": [
        "shell",
        "filesystem"
      ],
      "catalogue": {
        "id": "mcp/filesystem",
        "name": "filesystem",
        "publisher": "Anthropic",
        "verified": true
      },
      "digest": "9f2c1a"
    },
    {
      "kind": "browser_extension",
      "name": "Example Assistant",
      "client": "Google Chrome",
      "scope": "user",
      "publisher": "example.com",
      "version": "2.4.1",
      "source": "~/Library/Application Support/Google/Chrome/Default/Extensions/abcdefghijklmnop/2.4.1_0/manifest.json",
      "reach": [
        "browser_tabs",
        "clipboard"
      ],
      "notes": [
        "matched on declared permissions, not on code"
      ]
    }
  ],
  "gaps": [
    {
      "area": "running processes",
      "reason": "this reads configuration files only"
    }
  ],
  "errors": [
    {
      "scanner": "browsers",
      "path": "~/Library/Application Support/Firefox/Profiles",
      "error": "permission denied"
    }
  ],
  "drift": [
    {
      "name": "internal-deploy-tools",
      "kind": "mcp_server",
      "source": "~/code/checkout-service/.cursor/mcp.json",
      "was": "1d90ff",
      "now": "3ab7d0"
    }
  ]
}
```

### Top level

| Field | Type | Always present | Meaning |
|---|---|---|---|
| `tool` | string | yes | Always `agentsurface`. |
| `version` | string | yes | The build's version. `dev` for a build that was not stamped by the release process. |
| `os` | string | yes | The `GOOS` value of the machine the scan ran on: `darwin`, `linux` or `windows`. |
| `findings` | array | yes | Everything found. `[]` when nothing was found, never `null`. |
| `gaps` | array | yes | What the run did not look at. `[]` when a run somehow records none, never `null`. |
| `errors` | array | no | Detector failures. Omitted when there were none. |
| `drift` | array | no | Items whose definition changed since the last run. Omitted when there is no baseline or nothing changed. |

### `findings[]`

| Field | Type | Always present | Meaning |
|---|---|---|---|
| `kind` | string | yes | What class of thing this is. Values below. |
| `name` | string | yes | The item's own name, as it appears in its configuration. |
| `client` | string | no | The application that loads it, for example `Claude Desktop`. |
| `scope` | string | yes | `user`, `project` or `system`. |
| `publisher` | string | no | Who published it, as declared in its own manifest. Declared, not verified. |
| `version` | string | no | As declared. |
| `source` | string | yes | Absolute path of the file the item was found in or declared by. |
| `command` | string | no | The command line the configuration says will be run. Never executed. |
| `reach` | array of string | no | Capabilities the configuration declares. Values below. |
| `catalogue` | object | no | A match in the snapshot shipped inside the binary. Absent means no match. |
| `digest` | string | no | A stable hash of the parts of the item that should not change on their own. This is what drift compares. |
| `notes` | array of string | no | Factual observations about the item. |

`catalogue` is `{ "id", "name", "publisher", "verified" }`, with `publisher`
omitted when unknown. **`verified` describes the catalogue entry, not the item on
your disk.** It means the snapshot records this identity as a known published
one. It does not mean the copy installed on your machine is unmodified.

### `kind` values

| Value | Heading in text output |
|---|---|
| `mcp_server` | Model context protocol servers |
| `extension` | Desktop extensions |
| `plugin` | Plugins |
| `skill` | Skills |
| `connector` | Connectors |
| `instruction_file` | Instruction files |
| `browser_extension` | AI browser extensions |
| `scheduled_task` | Scheduled agent tasks |

### `scope` values

| Value | Meaning |
|---|---|
| `user` | Applies to your whole user account. |
| `project` | Declared by a directory on disk, usually a repository. Worth its own value because a repository can carry agent configuration that you never installed. |
| `system` | Applies machine-wide. |

### `reach` values

What the item's declared configuration says it can touch. These are read from
manifests and configuration files. They are observations about a declaration,
**not** an analysis of what the code does.

| Value | Meaning |
|---|---|
| `shell` | Can run commands. |
| `applescript` | Can drive other applications on macOS. |
| `filesystem` | Can read or write files. |
| `network` | Can make network requests. |
| `browser_tabs` | Can read or act on browser tabs. |
| `clipboard` | Can read or write the clipboard. |
| `credentials` | Declares access to credentials or a credential store. |
| `unknown` | The format gave no usable signal. |

An empty or absent `reach` means nothing was declared. It does not mean the item
can do nothing.

### `gaps[]`

`{ "area": string, "reason": string }`. Both always present. This array is not a
footnote: it is how you tell "nothing is installed" apart from "nothing was
looked at". Read it before you conclude anything from a small `findings` array.

### `errors[]`

`{ "scanner": string, "path": string (optional), "error": string }`. A detector
that failed. The run continued.

### `drift[]`

`{ "name", "kind", "source", "was", "now", "first_seen_changed" (optional) }`.
`was` and `now` are digests from the previous and current run. Drift means the
declared definition of something already on your machine changed. It does not
say who changed it or why.

## Stability

Ahead of a `1.0.0` release, the JSON shape can change in a minor version, and
changes are recorded in `CHANGELOG.md`.

If you are consuming this:

- Treat unknown fields as additions and ignore them.
- Treat unknown `kind` and `reach` values as additions and do not fail on them.
  New detectors add new values.
- Do not treat an absent optional field as an empty value with meaning. Absent
  `catalogue` means no match was found, not that the item is unknown to
  everyone.
- Do not parse the text output.

SARIF output is a reasonable future addition and is deliberately not in this
version.
