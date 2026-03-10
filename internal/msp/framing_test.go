package msp

import (
	"bytes"
	"testing"
)

// toResponse rewrites a request frame's direction byte from '<' to '>'.
func toResponse(frame []byte) []byte {
	out := make([]byte, len(frame))
	copy(out, frame)
	out[2] = '>'
	return out
}

func TestEncodeV1Empty(t *testing.T) {
	got := EncodeV1(0x01, nil)
	want := []byte{'$', 'M', '<', 0x00, 0x01, 0x01}
	if !bytes.Equal(got, want) {
		t.Fatalf("EncodeV1 empty: got %v, want %v", got, want)
	}
}

func TestEncodeV1WithPayload(t *testing.T) {
	payload := []byte{0x00, 0x01, 0x02}
	got := EncodeV1(0x01, payload)
	// size=3, code=1, payload=0,1,2, checksum = 3^1^0^1^2 = 1
	want := []byte{'$', 'M', '<', 0x03, 0x01, 0x00, 0x01, 0x02, 0x01}
	if !bytes.Equal(got, want) {
		t.Fatalf("EncodeV1 with payload: got %v, want %v", got, want)
	}
}

func TestEncodeV2Empty(t *testing.T) {
	got := EncodeV2(0x01, nil)
	if len(got) != MSPV2Overhead {
		t.Fatalf("EncodeV2 empty: expected length %d, got %d", MSPV2Overhead, len(got))
	}
	if string(got[:3]) != "$X<" {
		t.Fatalf("EncodeV2 empty: bad preamble %q", got[:3])
	}
	// Verify CRC: CRC8-DVB-S2 over [flag=0, code_lo=1, code_hi=0, len_lo=0, len_hi=0]
	header := got[3 : len(got)-1]
	expectedCRC := CRC8DVBS2(header, 0)
	if got[len(got)-1] != expectedCRC {
		t.Fatalf("EncodeV2 empty: bad CRC got 0x%02x, want 0x%02x", got[len(got)-1], expectedCRC)
	}
}

func TestEncodeV2WithPayload(t *testing.T) {
	payload := []byte{0x10, 0x20, 0x30}
	got := EncodeV2(0x0047, payload)
	if string(got[:3]) != "$X<" {
		t.Fatalf("EncodeV2 with payload: bad preamble %q", got[:3])
	}
	// code=0x0047 → lo=0x47, hi=0x00; len=3 → lo=3, hi=0
	if got[4] != 0x47 || got[5] != 0x00 {
		t.Fatalf("EncodeV2: bad code bytes got [0x%02x, 0x%02x]", got[4], got[5])
	}
	if got[6] != 0x03 || got[7] != 0x00 {
		t.Fatalf("EncodeV2: bad len bytes got [0x%02x, 0x%02x]", got[6], got[7])
	}
	header := got[3 : len(got)-1]
	expectedCRC := CRC8DVBS2(header, 0)
	if got[len(got)-1] != expectedCRC {
		t.Fatalf("EncodeV2 with payload: bad CRC got 0x%02x, want 0x%02x", got[len(got)-1], expectedCRC)
	}
}

func TestDecodeV1SingleFrame(t *testing.T) {
	frame := toResponse(EncodeV1(MSPAPIVersion, []byte{0x01, 0x02}))
	dec := NewFrameDecoder()
	dec.Feed(frame)
	if len(dec.Frames) != 1 {
		t.Fatalf("expected 1 frame, got %d", len(dec.Frames))
	}
	f := dec.Frames[0]
	if f.Version != 1 || f.Direction != '>' || f.Code != MSPAPIVersion {
		t.Fatalf("unexpected frame: %+v", f)
	}
	if !bytes.Equal(f.Payload, []byte{0x01, 0x02}) {
		t.Fatalf("unexpected payload: %v", f.Payload)
	}
}

func TestDecodeV1MultipleFrames(t *testing.T) {
	f1 := toResponse(EncodeV1(MSPAPIVersion, []byte{0xAA}))
	f2 := toResponse(EncodeV1(MSPFCVariant, []byte{0xBB, 0xCC}))
	dec := NewFrameDecoder()
	dec.Feed(append(f1, f2...))
	if len(dec.Frames) != 2 {
		t.Fatalf("expected 2 frames, got %d", len(dec.Frames))
	}
	if dec.Frames[0].Code != MSPAPIVersion {
		t.Fatalf("frame 0: expected code %d, got %d", MSPAPIVersion, dec.Frames[0].Code)
	}
	if dec.Frames[1].Code != MSPFCVariant {
		t.Fatalf("frame 1: expected code %d, got %d", MSPFCVariant, dec.Frames[1].Code)
	}
}

func TestDecodeV2SingleFrame(t *testing.T) {
	frame := toResponse(EncodeV2(0x0047, []byte{0x10, 0x20}))
	dec := NewFrameDecoder()
	dec.Feed(frame)
	if len(dec.Frames) != 1 {
		t.Fatalf("expected 1 frame, got %d", len(dec.Frames))
	}
	f := dec.Frames[0]
	if f.Version != 2 || f.Direction != '>' || f.Code != 0x0047 {
		t.Fatalf("unexpected frame: %+v", f)
	}
	if !bytes.Equal(f.Payload, []byte{0x10, 0x20}) {
		t.Fatalf("unexpected payload: %v", f.Payload)
	}
}

func TestDecodeV1BadChecksum(t *testing.T) {
	frame := toResponse(EncodeV1(MSPAPIVersion, []byte{0x01}))
	frame[len(frame)-1] ^= 0xFF // corrupt checksum
	dec := NewFrameDecoder()
	dec.Feed(frame)
	if len(dec.Frames) != 0 {
		t.Fatalf("expected 0 frames for bad checksum, got %d", len(dec.Frames))
	}
}

func TestDecodeV2BadChecksum(t *testing.T) {
	frame := toResponse(EncodeV2(0x0047, []byte{0x10}))
	frame[len(frame)-1] ^= 0xFF // corrupt CRC
	dec := NewFrameDecoder()
	dec.Feed(frame)
	if len(dec.Frames) != 0 {
		t.Fatalf("expected 0 frames for bad CRC, got %d", len(dec.Frames))
	}
}

func TestDecodeGarbagePrefix(t *testing.T) {
	garbage := []byte{0xFF, 0x00, 0xAB, 0xCD, '$', 'Q', 0x99}
	valid := toResponse(EncodeV1(MSPFCVersion, []byte{0x04, 0x05, 0x06}))
	dec := NewFrameDecoder()
	dec.Feed(append(garbage, valid...))
	if len(dec.Frames) != 1 {
		t.Fatalf("expected 1 frame after garbage, got %d", len(dec.Frames))
	}
	if dec.Frames[0].Code != MSPFCVersion {
		t.Fatalf("expected code %d, got %d", MSPFCVersion, dec.Frames[0].Code)
	}
}

func TestDecodeV1ZeroLengthPayload(t *testing.T) {
	frame := toResponse(EncodeV1(MSPBoardInfo, nil))
	dec := NewFrameDecoder()
	dec.Feed(frame)
	if len(dec.Frames) != 1 {
		t.Fatalf("expected 1 frame, got %d", len(dec.Frames))
	}
	if len(dec.Frames[0].Payload) != 0 {
		t.Fatalf("expected empty payload, got %v", dec.Frames[0].Payload)
	}
}

func TestDecodeFragmented(t *testing.T) {
	frame := toResponse(EncodeV1(MSPAPIVersion, []byte{0x01, 0x02, 0x03}))
	dec := NewFrameDecoder()
	for _, b := range frame {
		dec.Feed([]byte{b})
	}
	if len(dec.Frames) != 1 {
		t.Fatalf("expected 1 frame from fragmented feed, got %d", len(dec.Frames))
	}
	if !bytes.Equal(dec.Frames[0].Payload, []byte{0x01, 0x02, 0x03}) {
		t.Fatalf("unexpected payload: %v", dec.Frames[0].Payload)
	}
}
