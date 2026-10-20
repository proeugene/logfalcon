# Protocol fixture provenance

Synthetic MSP v1 response packets authored from the documented field order in
Betaflight 4.5.2, commit `024f8e13d4e642eb6a380308685b9ea3aa3ef1a2`:
[msp.c](https://github.com/betaflight/betaflight/blob/024f8e13d4e642eb6a380308685b9ea3aa3ef1a2/src/main/msp/msp.c).
Relevant sections are `MSP_BLACKBOX_CONFIG`, `serializeDataflashSummaryReply`
and the uncompressed branch of `serializeDataflashReadReply`.

The blackbox packet selects SPI flash. The summary has both ready/supported bits,
16 sectors, 4 MiB total and 65,537 bytes used. The read returns four literal bytes
at offset 65,536; the negative fixture changes compression type to 1.
Frames include manually calculated XOR checksums and are stored as fixed hex;
tests never construct them with LogFalcon's encoder.

These are protocol-layout checks, not captures or firmware execution. They do
not prove USB timing, firmware compatibility or valid blackbox contents. No
upstream implementation or compression table is copied here. Obtain a real
controller capture and Blackbox Explorer result during physical acceptance.
