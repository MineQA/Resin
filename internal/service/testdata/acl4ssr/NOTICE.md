# NOTICE — Test Fixture Assets

The files in this directory are test fixture/golden assets derived from
**ACL4SSR/ACL4SSR** (https://github.com/ACL4SSR/ACL4SSR), used solely for
deterministic regression testing of the ACL4SSR `[custom]` INI converter.

## Source

| Field | Value |
|---|---|
| Upstream repository | [ACL4SSR/ACL4SSR](https://github.com/ACL4SSR/ACL4SSR) |
| Source file | `Clash/config/ACL4SSR_Online_Full.ini` |
| Commit SHA | `2bd8c32ca469c6e548a21627bbe284ec8d7c92ba` |
| Snapshot date | 2026-07-17 |
| License | CC-BY-SA-4.0 |

## Attribution and License

ACL4SSR is (c) its contributors, licensed under
[Creative Commons Attribution-ShareAlike 4.0 International (CC-BY-SA-4.0)](https://creativecommons.org/licenses/by-sa/4.0/).

- `ACL4SSR_Online_Full.ini` is a verbatim copy of the upstream file at the
  specified commit. It is included as a frozen test fixture.
- `ACL4SSR_Online_Full.golden.yaml` is a deterministic conversion output
  derived from the above INI file. It is included as a golden reference for
  regression testing.

These three files are the only assets in this directory and are **test material
only**. They are not part of the Resin application source or runtime.

The license notice above applies exclusively to these fixture/golden assets.
All other Resin source code remains under Resin's own license terms, which are
not modified or superseded by the inclusion of these derived test assets.
