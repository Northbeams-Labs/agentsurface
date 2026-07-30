# Privacy

`agentsurface` collects nothing and sends nothing.

- **No network calls.** The binary has no network code in it. Not for
  telemetry, not for version checks, not for catalogue lookups, not for crash
  reports. See "How this is enforced" below.
- **No account, no token, no sign-up, no licence key.**
- **No upload.** Nothing found on your machine leaves your machine.
- **No analytics of any kind**, including anonymised or aggregated counts.
- **Nothing is written outside your home directory** except what you redirect
  with your shell.

There is no opt-out, because there is nothing to opt out of.

## The one file it writes

`agentsurface` stores a local baseline so that a later run can tell you when a
tool definition changed under you. That file is:

```
$HOME/.config/agentsurface/baseline.json
```

The same path is used on macOS, Linux and Windows. The directory is created
with mode `0700` and the file with mode `0600`, so on a Unix system only your
user account can read it.

**What is in it:** a JSON object mapping a key to a hash. The key is the item's
kind, the path the item was found at, and the item's name, joined together. The
hash is a digest of the parts of the item that should not change on their own.

**What is not in it:** the contents of any file it read, environment variables,
secrets, tokens, or anything about you or your machine beyond the paths above.

Because the keys contain local file paths, treat the file the way you would
treat any local index of your home directory. It is not designed to be shared.

**To turn it off:**

```
agentsurface --no-baseline
```

With that flag the file is neither read nor written. Drift detection is then
unavailable, because there is nothing to compare against.

**To delete it:**

```
rm ~/.config/agentsurface/baseline.json
```

Deleting it is always safe. The next run recreates it, and reports no drift
because it has nothing to compare against.

## What the tool reads

It reads local configuration belonging to AI agent tooling: client
configuration files, extension and plugin directories, skill and connector
definitions, instruction files, and browser extension manifests and profile
metadata. `docs/DETECTIONS.md` lists the paths per detector, and every run
prints a "What this did not look at" section stating its own blind spots.

It reads. It does not execute anything it finds, and it does not start any
configured server in order to inspect it.

## How this is enforced

The claim "no network calls" is checked in CI on every push, and the release
fails if the check fails.

- `.github/workflows/no-network.yml` runs `go list -deps` over the command
  package for every operating system and architecture we ship, and fails if the
  dependency graph contains `net`, `net/http`, `crypto/tls` or any other
  package on the denylist in that workflow.
- The same job fails if the dependency graph contains any package outside the
  Go standard library and this module, so a third-party HTTP client cannot
  arrive without tripping it.
- The same job inspects the symbols in the compiled binary with `go tool nm`,
  so the check looks at the artefact and not only at the source.
- `make verify` runs the identical check on your own machine.

You do not have to take our word for any of this. Building from source and
running `make verify` reproduces the check, and `docs/VERIFY.md` shows how to
confirm the release binary matches the source it claims to come from.

## Contact

Questions about this page: open an issue. Suspected privacy problem in a
released binary: follow `SECURITY.md`, because that is a security report.
