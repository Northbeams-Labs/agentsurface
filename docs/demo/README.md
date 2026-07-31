# The demo recording

![agentsurface listing the agent machinery on a fixture machine](agentsurface.gif)

`agentsurface.gif` is one real run of the binary, 29 seconds, paged through with
`less` because the output is four screens long. It shows the command, the
summary line, the inventory, and the "What this did not look at" section that
prints on every run. `agentsurface.png` is a still of the first page, for
directories that want one.

Nothing is edited. Every character on screen was printed by the binary.

## The machine it reads is invented

The recording must not show the machine it was recorded on, so `fixture.sh`
builds a home directory full of agent machinery that does not exist: MCP servers
declared by Claude Desktop, Cursor and Zed plus one carried by a repository, two
desktop extensions, two skills, a browser extension and the native messaging
host that names it, and two instruction files. Every name, publisher, extension
id and path in it is made up. The fixture is written to `/tmp/ashome`, outside
the checkout, and rebuilt from scratch on every recording.

The tape then points `HOME` at it. That is the whole trick: the tool is not
modified, patched or wrapped, it is simply run against a different machine.

Two paths cannot be moved that way. Native messaging hosts are also registered
machine-wide, at absolute paths under `/Library`, and on a real Mac those hold
real software. So the recorded shell runs inside
[`machine-browsers.sb`](machine-browsers.sb), a sandbox profile that denies
exactly those directories. The run reports them under "Could not read", which is
the honest outcome and visible in the recording: two directory names belonging
to the recording machine appear there, and nothing else from it does.

## Regenerating

Needs Go and [VHS](https://github.com/charmbracelet/vhs) (`brew install vhs`,
which brings `ttyd` and `ffmpeg` with it).

```sh
docs/demo/record.sh
```

That builds the binary, rebuilds the fixture, and re-records both files. Do it
whenever the output changes; a recording nobody regenerates is a screenshot of
an old version.

| | |
|---|---|
| [`agentsurface.tape`](agentsurface.tape) | The recording script: window size, what is typed, when it pages |
| [`fixture.sh`](fixture.sh) | Builds the invented machine |
| [`machine-browsers.sb`](machine-browsers.sb) | Denies the machine-wide browser directories for the length of the recording |
| [`check.sh`](check.sh) | Runs the same command the tape runs and asserts the output is worth showing and safe to show |
| [`record.sh`](record.sh) | Runs all of them in order |

`check.sh` runs before every recording and can be run on its own. It fails if
the run stops printing any of the things the demo exists to show, and it fails
if a single absolute path from the recording machine reaches the output. The one
allowance is written out as the whole line it is allowed to be, so a finding
read out of one of those directories would still fail it.

If the output grows, the page count in the tape is the thing to adjust: it pages
three times, and a longer inventory needs another `PageDown` and another
`Sleep`.
