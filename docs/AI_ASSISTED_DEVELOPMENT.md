# AI-assisted development: retrospective

This note covers the 20 October 2026 changes based on commit
`91847b19b5ba4427d13b490de121622f9c61f768`. Eugene set the product goal:
collect a flight log at the field without a laptop. He chose to keep erasure off
by default, keep diagnostics on and remove the compression table whose source
was GPL-licensed. A coding assistant investigated faults, edited the Go code
and documentation, and ran automated checks.

## What the checks caught

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

The [test record](../DEVELOPMENT_TESTING.md) lists the checks run, skipped checks and field observations.
