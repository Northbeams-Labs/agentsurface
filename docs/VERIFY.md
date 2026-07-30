# Verifying a release

This page is for someone who does not trust us, which is the correct posture
towards a tool that reads your home directory.

Four things can be checked, in increasing order of effort and of what they
prove:

1. [The bytes are the bytes we published](#1-checksums) (checksums).
2. [We published them](#2-signature) (cosign signature).
3. [They came out of a specific workflow, from a specific commit](#3-build-provenance)
   (build provenance attestation).
4. [That commit really does produce those bytes](#4-reproduce-the-build), and
   [there is no network code in them](#5-check-for-network-code) (reproducible
   build, and reading the binary's own symbol table).

An honest note before the commands: almost nobody runs these. That is fine. Part
of the value of this page is that a sceptic can see the steps exist and would
work.

Replace `VERSION` with the release tag throughout, for example `v0.1.0`.

---

## 1. Checksums

Every release has a `checksums.txt` listing the SHA-256 of each archive.

```sh
# Download the archive and the checksums file
gh release download VERSION \
  --repo Northbeams-Labs/agentsurface \
  --pattern 'agentsurface_*' \
  --pattern 'checksums.txt'

# Verify. Prints "OK" for the file you downloaded and complains about the rest,
# which is expected if you only downloaded one archive.
sha256sum --ignore-missing --check checksums.txt
```

On macOS without GNU coreutils:

```sh
shasum -a 256 agentsurface_VERSION_macos_universal.tar.gz
grep agentsurface_VERSION_macos_universal.tar.gz checksums.txt
```

**What this proves:** the file you have matches the file listed. **What it does
not prove:** that we wrote the list. For that, step 2.

---

## 2. Signature

`checksums.txt` is signed with [cosign](https://github.com/sigstore/cosign) in
keyless mode. There is no private key anywhere in this project, so there is no
private key to be stolen. The signature is bound to the release workflow's OIDC
identity and is recorded in the public Sigstore transparency log.

```sh
gh release download VERSION \
  --repo Northbeams-Labs/agentsurface \
  --pattern 'checksums.txt*'

cosign verify-blob \
  --certificate checksums.txt.pem \
  --signature checksums.txt.sig \
  --certificate-identity-regexp '^https://github\.com/Northbeams-Labs/agentsurface/\.github/workflows/release\.yml@refs/tags/' \
  --certificate-oidc-issuer 'https://token.actions.githubusercontent.com' \
  checksums.txt
```

The two `--certificate-*` flags are the whole point. Without them cosign will
confirm that *somebody* signed the file. With them it confirms that the
signature came from `release.yml` in this repository, running on a tag.

**What this proves:** the checksum list was produced by that workflow in that
repository. Combined with step 1, the archive you hold is the one that workflow
produced.

---

## 3. Build provenance

Every artefact carries a GitHub build provenance attestation, which records the
workflow, the repository, the commit SHA and the event that triggered the build.
It is SLSA Build Level 2.

```sh
gh attestation verify agentsurface_VERSION_macos_universal.tar.gz \
  --repo Northbeams-Labs/agentsurface
```

To see what it actually says, rather than just that it passed:

```sh
gh attestation verify agentsurface_VERSION_macos_universal.tar.gz \
  --repo Northbeams-Labs/agentsurface \
  --format json
```

Read `buildDefinition.externalParameters` for the workflow and the ref, and
`.resolvedDependencies` for the commit SHA. Then go and read that commit.

**What this proves:** which source commit and which workflow produced the
artefact. **What it does not prove:** that the commit does what it says. Nothing
automated can prove that. Step 4 lets you check the commit matches the binary,
and then the source is yours to read.

---

## 4. Reproduce the build

This is the step almost no project offers, and it is the strongest one. Build
the binary yourself from the tagged source and confirm you get the same bytes.

Three things make a Go build reproducible, and the release uses all three:

| Ingredient | Value | Where it is set |
|---|---|---|
| `-trimpath` | removes build-machine paths from the binary | `.goreleaser.yaml`, `builds.flags` |
| `CGO_ENABLED=0` | no host C toolchain in the result | `.goreleaser.yaml`, `builds.env` |
| A pinned Go version | see below | `.github/workflows/release.yml`, the `Set up Go` step |

The Go version used to build releases is **1.26.5**. It is pinned to an exact
patch version on purpose: a different compiler produces different bytes. If a
release was built with a different version, the release workflow log records it,
in the step named "Record the toolchain".

```sh
# 1. Get the exact source
git clone https://github.com/Northbeams-Labs/agentsurface
cd agentsurface
git checkout VERSION

# 2. Confirm your Go version matches the one above
go version

# 3. Build with the same flags the release uses.
#    Set GOOS and GOARCH to the platform you are checking.
CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 \
  go build -trimpath -ldflags "-X main.version=VERSION" \
  -o agentsurface-local ./cmd/agentsurface

# 4. Compare against the released binary, extracted from its archive
tar xzf agentsurface_VERSION_linux_arm64.tar.gz
sha256sum agentsurface-local agentsurface
```

Two things to expect:

- **The macOS artefact is a universal binary**, built by joining the `amd64` and
  `arm64` builds. A single-architecture local build will not hash the same as
  the universal file. Compare against a Linux artefact, or extract the matching
  slice with `lipo -extract arm64` before hashing.
- **`-ldflags` must match exactly**, including the version string, because it is
  compiled into the binary.

If your hash differs on a Linux artefact with a matching Go version and matching
flags, that is worth an issue. It is either a bug in this recipe or something
worse, and either way we want to know.

---

## 5. Check for network code

`agentsurface` is published on the basis that it makes no network calls. You do
not have to take that on trust, and you do not have to read the source to check
it.

The release binaries are **not stripped**, deliberately. `-s -w` would save a
couple of megabytes and would make this check impossible. So the symbol table is
in there, and you can read it:

```sh
go tool nm $(which agentsurface) | grep -E ' (net|net/http|crypto/tls|os/exec)\.' || \
  echo "no network or process-starting symbols"
```

An empty result is the expected one. There is nothing to filter out or explain
away.

You can also check the source side of the same claim, on your own machine,
against any checkout:

```sh
git clone https://github.com/Northbeams-Labs/agentsurface
cd agentsurface
make verify
```

`make verify` runs
[`.github/scripts/no-network.sh`](../.github/scripts/no-network.sh), which is
the same script CI runs. It:

- resolves the full dependency graph of the command for six operating system and
  architecture combinations, so an import hidden behind a build tag for a
  platform you are not on is still caught;
- fails on `net`, on anything under `net/` other than the two pure parsers
  `net/url` and `net/netip`, on `crypto/tls`, and on `os/exec`;
- fails on **any** dependency outside the Go standard library and this module,
  which is what catches a third-party HTTP client without having to know its
  name;
- builds each platform and greps the compiled binary's symbol table, so the
  check covers the artefact and not only the source.

The reasoning behind each rule, including why `os/exec` is on the list and why
`net/url` is not, is written at the top of that script.

**What this does not prove.** It is a build-time check, not a sandbox. A
determined author could still reach the network through raw syscalls or through
cgo. The check makes an accidental network call impossible and a deliberate one
obvious to anyone reading the diff. It is not a substitute for reading the code,
and it is not claimed to be.

The strongest check available to you is the one nothing here can replace: watch
the process. `agentsurface` is a short-lived command-line program, so run it
under whatever your operating system gives you.

---

## 6. SBOM

Each archive has a software bill of materials next to it, produced by
[syft](https://github.com/anchore/syft).

```sh
gh release download VERSION \
  --repo Northbeams-Labs/agentsurface \
  --pattern '*.sbom.json'
```

For a module with no third-party dependencies this is a short document, and that
is the point: the SBOM is how you confirm the dependency claim rather than
taking our word for it.

---

## Signing that is deliberately not here

**Windows.** There is no Windows release artefact, so there is no Authenticode
signature to check. The reason is in the comment at the top of `.goreleaser.yaml`:
an unsigned Windows download makes SmartScreen warn about it, and a security
tool that makes the operating system warn about it is a worse trust story than
one that is honest about not shipping a Windows build yet. Signing costs money,
so it is a spending decision rather than a build task. CI compiles the Windows
binary on every push so that the code does not rot in the meantime.

**macOS notarisation.** Not yet in place. Until it is, macOS may quarantine a
downloaded binary and refuse to run it until you approve it in System Settings
under Privacy and Security. Installing through a package manager avoids the
quarantine flag. This is a gap, and it is stated here rather than left to be
discovered.

## If something does not verify

That is a security report. Follow [SECURITY.md](../SECURITY.md) and use the
private channel. A signature or checksum that does not verify is in scope.
