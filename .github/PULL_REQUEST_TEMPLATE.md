<!--
Thanks for the patch. Everything below is short on purpose.
Security problems do not belong in a pull request. See SECURITY.md.
-->

## What this changes

<!-- One or two sentences. What is different after this is merged. -->

## Why

<!-- Link the issue if there is one: "Fixes #123". If there is no issue and
this is more than a typo, say why it did not need one. -->

## How it was tested

<!-- The commands you ran and what you saw. "make check passes" on its own is
not enough for a behaviour change: say what you pointed it at. -->

## Detector changes

<!-- Delete this section if you did not touch anything under internal/scan/. -->

- Paths read:
- Fixtures added under `testdata/`:
- Gaps this detector now reports (what it still does not look at):
- `docs/DETECTIONS.md` updated: yes / no

## Checklist

- [ ] Commits are signed off (`git commit -s`). See CONTRIBUTING.md. There is
      no contributor licence agreement.
- [ ] `make check` passes locally (lint, tests with race, no-network check).
- [ ] Tests read from `testdata/` fixtures, not from my own home directory.
- [ ] No new network capability. No telemetry, update check, catalogue lookup
      or model call.
- [ ] No new third-party dependency, or an issue exists agreeing to one.
- [ ] Nothing found on the machine is executed, started or invoked.
- [ ] Output states observations, not verdicts, and adds no score or grade.
- [ ] Documentation updated where behaviour changed, including anywhere the
      tool's limits are described.
- [ ] `CHANGELOG.md` untouched. It is written at release time.
