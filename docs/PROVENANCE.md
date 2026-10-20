# Licence and provenance review

The repository declares MIT in [LICENSE](../LICENSE). On 20 October 2026 the
optional `internal/msp/huffman.go` table and decoder, plus their tests, were
removed at the maintainer's direction. Their header said the table was ported
from Betaflight Configurator, whose
[10.10.0 licence](https://github.com/betaflight/betaflight-configurator/blob/10.10.0/LICENSE)
is GPL-3.0. No replacement compression implementation is included. Plain MSP
transfers remain; setting `flash_read_compression=true` fails validation.

Removal from this checkout does not change old releases or Git history. Do not
assert that prior distributions' licensing obligations have been settled. Review
those artifacts before redistributing them. Also confirm the copyright holder:
LICENSE currently says “Eugene Prokopev”, whereas current commits identify
Eugene Prokudin. The notice has been preserved pending the owner's confirmation.

Protocol tests contain authored synthetic packets describing wire fields, not
copied firmware implementations; [fixture provenance](../internal/msp/testdata/README.md)
identifies the exact upstream source. `go.mod`/`go.sum` identify application
dependencies (TOML, serial and their transitive dependencies); their upstream
licences remain applicable. The Pi image additionally redistributes Raspberry
Pi OS packages with their own notices under `/usr/share/doc`. MIT is the
application's declared licence, not a blanket licence for the OS image.

Workflow actions, pi-gen and PiShrink are pinned to commits; Docker base images
are pinned to digests. APT package repositories and downloaded OS inputs can
still change. This is traceable build input selection, not a claim of fully
reproducible images or a legal clearance of every dependency.
