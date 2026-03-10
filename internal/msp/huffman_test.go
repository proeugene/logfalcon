package msp

import (
"bytes"
"testing"
)

func TestHuffmanTreeSize(t *testing.T) {
if got := len(defaultTree); got != 257 {
t.Errorf("tree size = %d, want 257", got)
}
}

func TestHuffmanLenIndex(t *testing.T) {
for codeLen := 1; codeLen <= maxCodeLen; codeLen++ {
idx := huffmanLenIndex[codeLen]
if idx == -1 {
continue
}
if defaultTree[idx].CodeLen != codeLen {
t.Errorf("huffmanLenIndex[%d] points to entry with CodeLen=%d", codeLen, defaultTree[idx].CodeLen)
}
if idx > 0 && defaultTree[idx-1].CodeLen >= codeLen {
t.Errorf("huffmanLenIndex[%d]=%d but previous entry also has CodeLen=%d", codeLen, idx, defaultTree[idx-1].CodeLen)
}
}
}

func TestHuffmanDecodeEmpty(t *testing.T) {
out, err := HuffmanDecode(nil, 0)
if err != nil {
t.Fatalf("unexpected error: %v", err)
}
if len(out) != 0 {
t.Errorf("expected empty output, got %v", out)
}
}

func TestHuffmanDecodeKnownVector(t *testing.T) {
// Encode [0x00, 0x00, 0x00] by hand:
//   0x00 -> codeLen=2, code=0x03 = 0b11
// Three copies: 11 11 11 -> 6 bits -> padded to 1 byte: 0b11111100 = 0xFC
input := []byte{0xFC}
want := []byte{0x00, 0x00, 0x00}
got, err := HuffmanDecode(input, 3)
if err != nil {
t.Fatalf("unexpected error: %v", err)
}
if !bytes.Equal(got, want) {
t.Errorf("HuffmanDecode = %v, want %v", got, want)
}
}

func TestHuffmanDecodeMixed(t *testing.T) {
// Encode [0x00, 0x01]:
//   0x00 -> codeLen=2, code=0b11
//   0x01 -> codeLen=3, code=0b101
// Bits: 11 101 -> 11101_000 = 0b11101000 = 0xE8
input := []byte{0xE8}
want := []byte{0x00, 0x01}
got, err := HuffmanDecode(input, 2)
if err != nil {
t.Fatalf("unexpected error: %v", err)
}
if !bytes.Equal(got, want) {
t.Errorf("HuffmanDecode = %v, want %v", got, want)
}
}

func TestHuffmanDecodeEOF(t *testing.T) {
// Encode [0x00] then EOF:
//   0x00 -> codeLen=2, code=0b11
//   EOF  -> codeLen=12, code=0b000000000000
// Bits: 11 000000000000 -> 14 bits -> pad to 2 bytes
//   11000000 00000000 = 0xC0 0x00
input := []byte{0xC0, 0x00}
// Ask for more than 1 byte; decoder should stop at EOF after 1 byte.
got, err := HuffmanDecode(input, 100)
if err != nil {
t.Fatalf("unexpected error: %v", err)
}
want := []byte{0x00}
if !bytes.Equal(got, want) {
t.Errorf("HuffmanDecode with EOF = %v, want %v", got, want)
}
}

func TestHuffmanRoundTrip(t *testing.T) {
for _, e := range defaultTree {
val, ok := huffmanLookup[huffmanKey{e.CodeLen, e.Code}]
if !ok {
t.Errorf("lookup missing for CodeLen=%d Code=0x%x", e.CodeLen, e.Code)
continue
}
if val != e.Value {
t.Errorf("lookup[%d,0x%x] = 0x%x, want 0x%x", e.CodeLen, e.Code, val, e.Value)
}
}
}
