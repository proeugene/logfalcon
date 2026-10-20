# Candidate and release gates

The current checkout is a local candidate, not a published or hardware-validated
release. Use [DEVELOPMENT_TESTING.md](DEVELOPMENT_TESTING.md) as the physical
acceptance procedure; do not duplicate or shorten those tests into a smoke-only
sign-off.

1. Review the diff and confirm [provenance](docs/PROVENANCE.md), including the
   copyright-holder discrepancy and any obligations for older distributions.
2. Run the checks in [CONTRIBUTING.md](CONTRIBUTING.md). Require successful Linux
   installer/transaction tests, race tests, vet, lint, fuzz and actionable
   vulnerability scans. No blanket suppression of scanner failures.
3. Build from one recorded commit on Linux. Inspect that exact image; record its
   SHA-256. Cross-compilation is separate evidence. The pinned pi-gen/PiShrink
   commits do not make changing OS/APT inputs reproducible.
4. Flash the candidate to a spare card and complete physical acceptance with
   erase and cleanup disabled. Record the real Pi/FC/firmware, log sizes,
   outcomes, measured timings, interrupted copies and reboot/settings recovery.
   Open a downloaded real dump in Blackbox Explorer. Check SD diagnostic retrieval.
5. Obtain one independent pilot's attempt at the documented workflow. Record
   failures/confusion before broadening compatibility claims or publishing.
6. Select the intended experimental version, review notes and run the release
   workflow. It builds binaries/image from the same commit, packages integration
   assets, a source archive, changelog and full build identity, then checksums all
   payloads. Releases are created as drafts for final maintainer review.

Required binary assets: `logfalcon-arm6`, `logfalcon-arm64`, `logfalcon-amd64`.
Also required: `logfalcon-*.img.xz`, canonical system units/scripts/polkit rule,
`logfalcon-default.toml`, `logfalcon-boot-config.txt`, `logfalcon-build.txt`,
`logfalcon-source.tar.gz`, source commit record, changelog and `checksums.txt`.
The installer verifies each consumed payload and compares the full binary
version/commit against `logfalcon-build.txt`. Older incomplete releases fail
before system changes. Do not fall back to mutable files from `main`.

Before publishing, verify downloaded assets against `checksums.txt`, check the
installed `--version`, and retain physical results for that exact image. A
rebuild with different package inputs requires a new artifact checksum and
acceptance record. Do not imply that the earlier local candidate's checks apply
to a different image.

Maintainer settings still needed (read-only inspection, 20 October 2026): main
branch protection/required checks or rulesets; Dependabot alerts/security updates;
private vulnerability reporting. Secret scanning and push protection were enabled
and no open secret alerts were returned. Settings were inspected, not changed.
