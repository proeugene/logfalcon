# Release checklist

Use this checklist for each release. Record hardware results against the exact
image in the [physical acceptance record](DEVELOPMENT_TESTING.md). The
[roadmap](https://proeugene.github.io/logfalcon/roadmap.html) summarises the next field tests.

1. Review the diff and confirm [provenance](docs/PROVENANCE.md), including any
   obligations for older distributions.
2. Run the checks in [CONTRIBUTING.md](CONTRIBUTING.md). Require successful Linux
   installer/transaction tests, race tests, vet, lint, fuzz and actionable
   vulnerability scans. No blanket suppression of scanner failures.
3. Build from one recorded commit on Linux. Inspect that exact image; record its
   SHA-256. Cross-compilation is separate evidence. The pinned pi-gen/PiShrink
   commits do not make changing OS/APT inputs reproducible.
   Image budgets are separate: at most **3 GiB raw** after shrinking and
   **600 MiB compressed** for download. The pinned builder reserves 512 MiB
   for boot, so 600 MiB cannot be a viable raw-image budget. Neither budget
   proves bootability; inspect contents and retain physical acceptance.
4. Flash the candidate to a spare card and complete physical acceptance with
   erase and cleanup disabled. Record the real Pi/FC/firmware, log sizes,
   outcomes, measured timings, interrupted copies and reboot/settings recovery.
   Open a downloaded real dump in Blackbox Explorer. Check SD diagnostic retrieval.
5. Obtain one independent pilot's attempt at the documented workflow. Record
   failures/confusion before broadening compatibility claims or publishing.
6. Choose the version and prerelease status, review notes and run the release
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
acceptance record.

Review branch protection and required checks, Dependabot alerts and security
updates, private vulnerability reporting, secret scanning and push protection
in the repository settings.

Offline updates additionally require all three signed .lfu files, trusted public
keys/key list, and the updater service/path from the same release commit.
Provision signing and test the rotation/direct-upgrade route as described in
[OFFLINE_UPDATES.md](docs/OFFLINE_UPDATES.md). Missing signing configuration is a
release failure. Check the exact uploaded package on the spare card, including
failed health/rollback and reboot recovery. Do not configure secrets or publish
from an untrusted PR.
