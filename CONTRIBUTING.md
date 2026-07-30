# Contributing

Thanks for looking. This page says what we take, what we do not, and how to get
a change through.

Set expectations first: **one person reviews pull requests here.** Review can
take days. If a pull request goes quiet, a comment on it is welcome and is not
rude.

## Sign your commits: DCO, not a CLA

This project uses the [Developer Certificate of Origin](https://developercertificate.org/)
version 1.1. **There is no contributor licence agreement.** You are not asked to
assign copyright, you are not asked to grant anyone rights beyond the Apache
License 2.0 that this repository is under, and there is no form to sign or
document to return.

You keep the copyright in what you write. Your contribution is licensed under
Apache-2.0, the same licence as the rest of the repository, and that is the
whole arrangement.

To sign off, add `-s` to your commit:

```
git commit -s -m "instructions: read AGENTS.md at project scope"
```

That appends one line:

```
Signed-off-by: Your Name <your.email@example.com>
```

which certifies the following:

> **Developer Certificate of Origin, Version 1.1**
>
> By making a contribution to this project, I certify that:
>
> (a) The contribution was created in whole or in part by me and I have the
> right to submit it under the open source license indicated in the file; or
>
> (b) The contribution is based upon previous work that, to the best of my
> knowledge, is covered under an appropriate open source license and I have the
> right under that license to submit that work with modifications, whether
> created in whole or in part by me, under the same open source license (unless
> I am permitted to submit under a different license), as indicated in the file;
> or
>
> (c) The contribution was provided directly to me by some other person who
> certified (a), (b) or (c) and I have not modified it.
>
> (d) I understand and agree that this project and the contribution are public
> and that a record of the contribution (including all personal information I
> submit with it, including my sign-off) is maintained indefinitely and may be
> redistributed consistent with this project or the open source license(s)
> involved.

Use a real name and a real email address. Anonymous and pseudonymous sign-offs
cannot certify anything, so we cannot take them.

Forgot to sign off? `git commit --amend -s` on the last commit, or
`git rebase --signoff main` on a branch, then force-push.

## Before you write code

For anything larger than a typo, **open an issue first**. That is not a
formality. This tool has a deliberately narrow scope (see below), and it is
better to find out in an issue than after a weekend of work.

Good first contributions, in rough order of how welcome they are:

1. A path we do not look at, on a platform we claim to support. Bring the path
   and a redacted example of what lives there.
2. A false negative with a fixture: something real that we should have found
   and did not.
3. A false positive: something reported that is not what we said it was.
4. Support for a client or platform we do not cover yet.
5. Documentation that is wrong, including any place where the tool is described
   as doing more than it does.

## What we will not take

These are settled decisions rather than open questions, so a pull request doing
any of them will be closed with a link to this section.

- **Anything that makes a network call.** No update checks, no catalogue
  lookups, no vulnerability feeds, no crash reporting, no model calls. The
  no-network CI job will fail the build before a human sees it. This is the
  point of the tool.
- **Telemetry or analytics**, including anonymised counts and opt-in schemes.
- **Any form of account, licence key, token or sign-up.**
- **A risk score, a grade, or a letter rating.** The tool reports what is
  installed. A score we cannot defend line by line would be a claim, and this
  project does not make claims it cannot show the working for.
- **Verdicts in output.** "Found", "not in catalogue" and "can reach: shell"
  are observations. "Dangerous", "suspicious" and "insecure" are not, and do
  not belong in a finding.
- **Executing anything found on the machine.** A detector may read a
  configuration file that starts a server. It must never start it, invoke it,
  or shell out to it in order to learn more.
- **A third-party dependency**, unless it is unavoidable. The module currently
  depends on the Go standard library only, and CI fails if a non-standard
  import appears. If you genuinely need one, open an issue before the pull
  request.
- **Marketing language anywhere**, including in the README, in help text and in
  release notes.

## Running the tests

You need Go 1.26 or newer, and nothing else.

```
make test     # go test -race ./...
make build    # builds ./bin/agentsurface
make vet      # go vet ./...
make fmt      # gofmt -w over the tree
make lint     # gofmt check plus go vet, no formatting changes
make verify   # the no-network check, exactly as CI runs it
make check    # lint, test and verify together
```

If you have no `make`, every target is a one-line command you can read out of
the `Makefile`.

`make verify` is the one to run before you push. It is the same check that
gates the release, so a failure there will fail CI too.

Tests must not read the developer's own home directory. Every scanner takes a
`model.Env`, and tests point `Env.HomeDir` at a fixture directory under
`testdata/`. A test that passes only on the machine that wrote it is not a
test.

## Adding a detector

A detector is a package under `internal/scan/` that implements one interface
from `internal/model`:

```go
type Scanner interface {
	Name() string
	Scan(env Env) ([]Finding, []Gap, []ScanError)
}
```

Read `internal/model/model.go` before you start. It is short, and it is the
vocabulary the whole tool shares.

1. **Create the package.** `internal/scan/<name>/<name>.go`, exporting a
   `New() model.Scanner`.
2. **Read only from `env`.** `env.HomeDir` for user scope, `env.Roots` for
   project scope, `env.OS` to decide which paths apply. Never call
   `os.UserHomeDir` inside a scanner, because that makes it untestable.
3. **Return findings, gaps and errors, and never abort the run.** A missing
   directory is normal and is not an error. An unreadable or unparseable file
   is a `ScanError` with the path, and the scan carries on. One detector must
   never take the run down with it.
4. **Always return at least one `Gap`.** State what you did not look at, in
   plain words: a platform you do not cover, a config format you skip, a
   location you cannot read without elevated privileges. Every run prints these,
   and a detector that reports no blind spots is a detector that has not thought
   about them.
5. **Fill in `Finding` honestly.** `Source` is the absolute path the item was
   found at, so a reader can go and look. `Reach` states capabilities declared
   by the configuration, as observed facts. Leave `Catalogue` nil when there is
   no match, because absent is reported as unknown and never as safe.
6. **Set `Digest`** if the item has a definition that should not change on its
   own. That is what drift detection compares between runs. Hash the parts that
   matter, not the whole file, so that unrelated edits do not produce noise.
7. **Add fixtures** under `testdata/` in your package, including at least one
   malformed file, and one file that looks like a match but is not.
8. **Register it** in the `scanners` slice in `cmd/agentsurface/main.go`.
9. **Document it** in `docs/DETECTIONS.md`, including the paths it reads and,
   in the same entry, what it misses.

## Pull requests

- Branch from `main`, one topic per pull request.
- `make check` passes locally.
- Commits are signed off.
- The pull request description says what changed and how you tested it. The
  template asks for both.
- CI runs on Linux, macOS and Windows. All three have to be green.
- New behaviour comes with a test. A bug fix comes with a test that failed
  before the fix.
- Do not edit `CHANGELOG.md` in a feature pull request. It is written at release
  time.

## Reporting security problems

Not here. `SECURITY.md` has the private channel.

## Code of conduct

`CODE_OF_CONDUCT.md` applies to every space this project uses.
