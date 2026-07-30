# Security policy

## Reporting a vulnerability

Report privately through GitHub, using the **Security** tab of this repository
and then **Report a vulnerability**. That opens a private advisory draft that
only the maintainers and you can read.

Direct link: <https://github.com/Northbeams-Labs/agentsurface/security/advisories/new>

If private reporting is unavailable to you for any reason, open a normal issue
titled `security contact request` containing no details of the problem, and a
maintainer will open a private channel.

Please do not report a vulnerability in a public issue, a pull request, a
discussion thread or a social post before it has been handled.

## What we commit to

**An initial response within 14 days of the report.** That is the whole
commitment. It means a human has read the report and replied to you with an
assessment of whether we consider it in scope.

We do not commit to a fix within any period, to a severity rating within any
period, or to a coordinated release date. If a fix is going to take longer than
you would like, we will say so rather than go quiet.

There is no bug bounty and no payment for reports.

## What is in scope

- A way to make `agentsurface` execute code, write outside its documented
  output paths, or escalate privileges, triggered by the contents of a
  configuration file, manifest, extension or directory that it reads.
- Any network call made by the released binary. The tool is built to make none,
  and CI fails the build if the dependency graph acquires the ability. If you
  observe a network call from a released binary, that is a security report and
  we will treat it as one.
- Disclosure of file contents, secrets or environment values in normal terminal
  output, in `--json` output, or in the local baseline file.
- Writing the baseline file with permissions that let another local user read
  or modify it.
- Path traversal, symlink following or archive handling that makes the scanner
  read or write somewhere it was not asked to.
- A crash triggered by a hostile local file where the crash is more than an
  error message, for example a panic that leaves a partially written file
  behind.
- A supply chain problem in our release process: an unpinned action, an
  incorrect checksum, a signature or provenance attestation that does not
  verify.

## What is out of scope

- Findings that `agentsurface` did not report. Missed detections are ordinary
  bugs. Please open a public issue using the "Missed detection" template so
  that others can see it.
- The behaviour of the agent machinery that `agentsurface` inventories. If an
  MCP server or browser extension on your machine is malicious, that is a
  problem with that software, not with this tool. We will not act as an
  intermediary for reporting it.
- Vulnerabilities in a fork, a repackaged build, or any binary that is not an
  official release artefact.
- Reports produced solely by an automated scanner with no demonstrated impact.
- Denial of service against a local command-line tool by a user who already
  controls the machine it runs on.
- Social engineering, physical access, and anything requiring an attacker who
  is already root on the machine.

## What happens after you report

1. **Within 14 days:** a maintainer acknowledges the report and tells you
   whether it is in scope.
2. **Triage:** we try to reproduce it, and we will ask you for detail if we
   cannot. If we cannot reproduce it and you cannot help us, we will close the
   report and say why.
3. **Fix:** developed in a private fork of this repository where the change is
   not visible until release.
4. **Release:** a patched release is published with the fix. Release notes state
   that a security fix is included.
5. **Disclosure:** we publish a GitHub Security Advisory describing the problem,
   the affected versions and the fixed version. We request a CVE through GitHub
   where the problem warrants one.
6. **Credit:** you are named in the advisory unless you ask not to be. Tell us
   how you want to be named.

If we decide something is not a vulnerability, we say so and explain the
reasoning. You are free to disagree publicly.

## Supported versions

Fixes are made on the latest released version. Older versions are not patched.
The version you are running is printed by `agentsurface --version`.

## Verifying what you downloaded

Every release carries checksums, a cosign signature over the checksums file,
and a GitHub build provenance attestation. `docs/VERIFY.md` has the commands.
