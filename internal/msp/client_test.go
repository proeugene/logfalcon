package msp

import (
	"bytes"
	"encoding/binary"
	"io"
	"testing"
	"time"
)

// mockSerial implements SerialPort backed by in-memory buffers.
type mockSerial struct {
	readBuf  *bytes.Buffer
	writeBuf *bytes.Buffer
}

func (m *mockSerial) Read(buf []byte) (int, error) {
	n, err := m.readBuf.Read(buf)
	if err == io.EOF {
		// Return 0 bytes without error so Receive can detect timeout
		// rather than treating EOF as a fatal read error.
		return 0, io.EOF
	}
	return n, err
}
func (m *mockSerial) Write(data []byte) (int, error) { return m.writeBuf.Write(data) }
func (m *mockSerial) Close() error                    { return nil }

// makeV1Response builds an MSP v1 response frame ($M>).
func makeV1Response(code byte, payload []byte) []byte {
	size := byte(len(payload))
	chk := CRC8Xor(append([]byte{size, code}, payload...))
	frame := []byte{'$', 'M', '>'}
	frame = append(frame, size, code)
	frame = append(frame, payload...)
	frame = append(frame, chk)
	return frame
}

func newTestClient(responseData []byte) (*Client, *mockSerial) {
	ms := &mockSerial{
		readBuf:  bytes.NewBuffer(responseData),
		writeBuf: &bytes.Buffer{},
	}
	return NewClient(ms, 100*time.Millisecond), ms
}

func TestGetAPIVersion(t *testing.T) {
	resp := makeV1Response(MSPAPIVersion, []byte{0x00, 0x02, 0x05})
	c, _ := newTestClient(resp)

	major, minor, err := c.GetAPIVersion()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if major != 2 || minor != 5 {
		t.Fatalf("expected (2, 5), got (%d, %d)", major, minor)
	}
}

func TestGetFCVariant(t *testing.T) {
	resp := makeV1Response(MSPFCVariant, []byte("BTFL"))
	c, _ := newTestClient(resp)

	variant, err := c.GetFCVariant()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if variant != "BTFL" {
		t.Fatalf("expected BTFL, got %s", variant)
	}
}

func TestGetUID(t *testing.T) {
	uid := []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c}
	resp := makeV1Response(MSPUID, uid)
	c, _ := newTestClient(resp)

	hexStr, err := c.GetUID()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := "0102030405060708090a0b0c"
	if hexStr != expected {
		t.Fatalf("expected %s, got %s", expected, hexStr)
	}
}

func TestGetDataflashSummary(t *testing.T) {
	// flags=0x03 (supported+ready), sectors=16, totalSize=4194304, usedSize=1048576
	payload := make([]byte, 13)
	payload[0] = 0x03
	binary.LittleEndian.PutUint32(payload[1:5], 16)
	binary.LittleEndian.PutUint32(payload[5:9], 4194304)
	binary.LittleEndian.PutUint32(payload[9:13], 1048576)

	resp := makeV1Response(MSPDataflashSummary, payload)
	c, _ := newTestClient(resp)

	ds, err := c.GetDataflashSummary()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ds.Flags != 0x03 {
		t.Fatalf("expected flags 0x03, got 0x%02x", ds.Flags)
	}
	if ds.Sectors != 16 {
		t.Fatalf("expected 16 sectors, got %d", ds.Sectors)
	}
	if ds.TotalSize != 4194304 {
		t.Fatalf("expected totalSize 4194304, got %d", ds.TotalSize)
	}
	if ds.UsedSize != 1048576 {
		t.Fatalf("expected usedSize 1048576, got %d", ds.UsedSize)
	}
	if !ds.Supported || !ds.Ready {
		t.Fatalf("expected supported=true, ready=true")
	}
}

func TestReceiveFlashReadBTFL(t *testing.T) {
	// Build BTFL-format payload: addr(4) + dataSize(2) + comprType(1) + data
	rawData := []byte("hello flash")
	payload := make([]byte, 4+2+1+len(rawData))
	binary.LittleEndian.PutUint32(payload[0:4], 0x1000)
	binary.LittleEndian.PutUint16(payload[4:6], uint16(len(rawData)))
	payload[6] = DataflashCompressionNone
	copy(payload[7:], rawData)

	resp := makeV1Response(MSPDataflashRead, payload)
	c, _ := newTestClient(resp)
	c.FCVariant = BTFLVariant

	addr, data, err := c.ReceiveFlashReadResponse()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if addr != 0x1000 {
		t.Fatalf("expected addr 0x1000, got 0x%x", addr)
	}
	if !bytes.Equal(data, rawData) {
		t.Fatalf("expected %q, got %q", rawData, data)
	}
}

func TestReceiveFlashReadINAV(t *testing.T) {
	// Build INAV-format payload: addr(4) + raw data (no length/compression header)
	rawData := []byte("inav data here")
	payload := make([]byte, 4+len(rawData))
	binary.LittleEndian.PutUint32(payload[0:4], 0x2000)
	copy(payload[4:], rawData)

	resp := makeV1Response(MSPDataflashRead, payload)
	c, _ := newTestClient(resp)
	c.FCVariant = INAVVariant

	addr, data, err := c.ReceiveFlashReadResponse()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if addr != 0x2000 {
		t.Fatalf("expected addr 0x2000, got 0x%x", addr)
	}
	if !bytes.Equal(data, rawData) {
		t.Fatalf("expected %q, got %q", rawData, data)
	}
}

func TestTimeout(t *testing.T) {
	// Empty read buffer → should time out.
	c, _ := newTestClient(nil)

	_, err := c.Receive(MSPAPIVersion)
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	if _, ok := err.(*TimeoutError); !ok {
		t.Fatalf("expected *TimeoutError, got %T: %v", err, err)
	}
}
