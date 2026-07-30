# Support

## Where to go

| You want to | Go to |
|---|---|
| Report something broken | [Open a bug report](https://github.com/Northbeams-Labs/agentsurface/issues/new?template=bug_report.yml) |
| Tell us we missed something on your machine | [Open a missed detection report](https://github.com/Northbeams-Labs/agentsurface/issues/new?template=missed_detection.yml) |
| Ask us to cover a client or platform | [Open a coverage request](https://github.com/Northbeams-Labs/agentsurface/issues/new?template=client_support.yml) |
| Report a vulnerability | [SECURITY.md](SECURITY.md). Not a public issue. |
| Understand what a detector looks at | [docs/DETECTIONS.md](docs/DETECTIONS.md) |
| Understand the output | [docs/OUTPUT.md](docs/OUTPUT.md) |
| Check a release is what it claims to be | [docs/VERIFY.md](docs/VERIFY.md) |
| Know what the tool stores | [PRIVACY.md](PRIVACY.md) |
| Submit a change | [CONTRIBUTING.md](CONTRIBUTING.md) |

## What support means here

This is a free tool maintained by one person alongside other work. Issues are
read. Replies are not immediate, and there is no service level attached to
anything except the 14-day initial response to a vulnerability report in
`SECURITY.md`.

If an issue has gone quiet for a couple of weeks, a comment on it is a
reasonable thing to do.

## Things that make an issue answerable

- The output of `agentsurface --version`.
- Your operating system and version.
- The exact command you ran.
- What you expected and what happened.
- For a missed detection: the path of the thing we should have found, and a
  redacted copy of the file. Take the secrets out first. We do not need them
  and do not want them.

## Things this project cannot help with

- Whether a particular MCP server, extension or plugin on your machine is safe.
  `agentsurface` inventories what is installed. It does not judge it, and we
  cannot judge it for you from an issue thread.
- Removing or configuring the software it finds. That belongs to whoever
  publishes that software.
- Anything about a fork or a repackaged build that is not an official release.

## Commercial products

Northbeams sells a separate commercial product. This repository is not a
support channel for it, and issues about it will be closed. Details of who
makes this tool are at <https://labs.northbeams.com>.
