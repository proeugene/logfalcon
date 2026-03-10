package msp

// Frame represents a decoded MSP frame.
type Frame struct {
	Version   int    // 1 or 2
	Direction byte   // '<', '>', or '!'
	Code      uint16
	Payload   []byte
}

// EncodeV1 encodes an MSP v1 request frame ($M<).
// Checksum: XOR over [size, code, payload...]
func EncodeV1(code byte, payload []byte) []byte {
	size := len(payload)
	buf := make([]byte, 0, MSPV1Overhead+size)
	buf = append(buf, '$', 'M', '<')
	buf = append(buf, byte(size), code)
	buf = append(buf, payload...)
	crc := CRC8Xor(buf[3:]) // XOR over size, code, payload
	buf = append(buf, crc)
	return buf
}

// EncodeV2 encodes an MSP v2 request frame ($X<).
// CRC: CRC8-DVB-S2 over [flag(0), code_lo, code_hi, len_lo, len_hi, payload...]
func EncodeV2(code uint16, payload []byte) []byte {
	size := len(payload)
	buf := make([]byte, 0, MSPV2Overhead+size)
	buf = append(buf, '$', 'X', '<')
	buf = append(buf, 0) // flag
	buf = append(buf, byte(code), byte(code>>8))
	buf = append(buf, byte(size), byte(size>>8))
	buf = append(buf, payload...)
	crc := CRC8DVBS2(buf[3:], 0) // CRC over flag..payload
	buf = append(buf, crc)
	return buf
}

// Decoder states.
const (
	stateIdle = iota + 1
	stateProtoV1M
	stateDirection
	stateV1Len
	stateV1Code
	stateV1Payload
	stateV1Checksum
	stateV2Flag
	stateV2CodeLo
	stateV2CodeHi
	stateV2LenLo
	stateV2LenHi
	stateV2Payload
	stateV2Checksum
)

// FrameDecoder is a streaming MSP frame decoder implementing a 14-state machine.
type FrameDecoder struct {
	Frames     []Frame
	state      int
	version    int
	direction  byte
	code       uint16
	size       int
	payload    []byte
	payloadIdx int
	checksum   byte   // running XOR for v1
	v2Header   []byte // accumulated V2 header bytes for batch CRC
}

// NewFrameDecoder returns a FrameDecoder in the idle state.
func NewFrameDecoder() *FrameDecoder {
	return &FrameDecoder{state: stateIdle}
}

// Feed processes incoming bytes, appending complete frames to d.Frames.
func (d *FrameDecoder) Feed(data []byte) {
	for _, b := range data {
		d.process(b)
	}
}

func (d *FrameDecoder) reset() {
	d.state = stateIdle
	d.version = 0
	d.direction = 0
	d.code = 0
	d.size = 0
	d.payload = nil
	d.payloadIdx = 0
	d.checksum = 0
	d.v2Header = nil
}

func (d *FrameDecoder) process(b byte) {
	switch d.state {
	case stateIdle:
		if b == '$' {
			d.state = stateProtoV1M
		}

	case stateProtoV1M:
		switch b {
		case 'M':
			d.version = 1
			d.state = stateDirection
		case 'X':
			d.version = 2
			d.state = stateDirection
		default:
			d.reset()
		}

	case stateDirection:
		if b == '<' || b == '>' || b == '!' {
			d.direction = b
			if d.version == 1 {
				d.state = stateV1Len
			} else {
				d.state = stateV2Flag
			}
		} else {
			d.reset()
		}

	// --- V1 path ---
	case stateV1Len:
		d.size = int(b)
		d.checksum = b
		d.state = stateV1Code

	case stateV1Code:
		d.code = uint16(b)
		d.checksum ^= b
		if d.size == 0 {
			d.state = stateV1Checksum
		} else {
			d.payload = make([]byte, d.size)
			d.payloadIdx = 0
			d.state = stateV1Payload
		}

	case stateV1Payload:
		d.payload[d.payloadIdx] = b
		d.payloadIdx++
		d.checksum ^= b
		if d.payloadIdx == d.size {
			d.state = stateV1Checksum
		}

	case stateV1Checksum:
		if b == d.checksum {
			pl := make([]byte, len(d.payload))
			copy(pl, d.payload)
			d.Frames = append(d.Frames, Frame{
				Version:   1,
				Direction: d.direction,
				Code:      d.code,
				Payload:   pl,
			})
		}
		d.reset()

	// --- V2 path ---
	case stateV2Flag:
		d.v2Header = []byte{b}
		d.state = stateV2CodeLo

	case stateV2CodeLo:
		d.code = uint16(b)
		d.v2Header = append(d.v2Header, b)
		d.state = stateV2CodeHi

	case stateV2CodeHi:
		d.code |= uint16(b) << 8
		d.v2Header = append(d.v2Header, b)
		d.state = stateV2LenLo

	case stateV2LenLo:
		d.size = int(b)
		d.v2Header = append(d.v2Header, b)
		d.state = stateV2LenHi

	case stateV2LenHi:
		d.size |= int(b) << 8
		d.v2Header = append(d.v2Header, b)
		if d.size == 0 {
			d.state = stateV2Checksum
		} else {
			d.payload = make([]byte, d.size)
			d.payloadIdx = 0
			d.state = stateV2Payload
		}

	case stateV2Payload:
		d.payload[d.payloadIdx] = b
		d.payloadIdx++
		if d.payloadIdx == d.size {
			d.state = stateV2Checksum
		}

	case stateV2Checksum:
		expected := CRC8DVBS2(d.payload, CRC8DVBS2(d.v2Header, 0))
		if b == expected {
			pl := make([]byte, len(d.payload))
			copy(pl, d.payload)
			d.Frames = append(d.Frames, Frame{
				Version:   2,
				Direction: d.direction,
				Code:      d.code,
				Payload:   pl,
			})
		}
		d.reset()
	}
}
