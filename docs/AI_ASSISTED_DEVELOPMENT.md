# AI-assisted development: retrospective

This describes the local candidate prepared on 20 October 2026, based on commit
`91847b19b5ba4427d13b490de121622f9c61f768` plus uncommitted changes. It is a
retrospective account of the recorded work, not evidence of an independent human
code review, completed field test or a released version.

Eugene set the product goal: dependable laptop-free collection and an easy-to-
evaluate open-source repository. Recorded decisions include keeping debug on,
using UK defaults, retaining non-destructive evaluation, and removing optional
compression after the GPL-origin table was identified. The coding assistant
investigated paths, proposed implementations, edited code/docs and ran checks.
Those implementation choices remain reviewable; authorship of a requirement is
not equivalent to human approval of every resulting line.

Three examples show the checks and their limits:

- A crafted session symlink allowed a download of an `outside-secret` fixture.
  The regression failed before the fix. Downloads now use rooted file handles;
  [containment tests](../internal/web/containment_test.go) cover directory-prefix
  collisions, directory symlinks and final-file symlinks. Suffix-range testing
  also exposed the wrong returned bytes; standard `http.ServeContent` replaces
  custom range handling.
- Earlier tests used an MSP peer built with LogFalcon's own encoder. Such tests
  exercise orchestration but can repeat the same protocol mistake. Fixed
  [synthetic fixtures](../internal/msp/testdata/README.md) now reference an exact
  Betaflight firmware revision. They are still not real captures or controller
  validation. That gap must be closed with a real dump and Blackbox Explorer.
- Settings updates previously wrote files sequentially, and boot application
  recorded success before restart confirmation. [Failure-injection tests](../internal/settings/transaction_test.go)
  now cover the second write, restart, applied-hash write, concurrent access and
  pending recovery. These test process/filesystem behaviour, not Pi power cuts.

Go race tests, vet, lint, fuzzing, vulnerability scanning and cross-compilation
provide separate evidence; actual results and skipped checks belong in
[DEVELOPMENT_TESTING.md](../DEVELOPMENT_TESTING.md). No hardware measurements,
review history, adoption or time savings are inferred from generated code.

The next decision belongs to the maintainer: review this candidate, build/flash
the exact image and record physical acceptance. Then ask an independent pilot to
try the documented workflow and report where it fails or becomes confusing.
Do not substitute another documentation-polish cycle for that evidence.
