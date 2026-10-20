package msp

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"time"
)

// SerialPort is the interface for serial I/O. Allows testing with mock ports.
type SerialPort interface {
	Read(buf []byte) (int, error)
	Write(data []byte) (int, error)
	Close() error
}

// Error is a general MSP error.
type Error struct {
	Message string
}

func (e *Error) Error() string { return e.Message }

// TimeoutError indicates a response was not received within the deadline.
type TimeoutError struct {
	Message string
}

func (e *TimeoutError) Error() string { return e.Message }

// DataflashSummary holds flash state from MSP_DATAFLASH_SUMMARY.
type DataflashSummary struct {
	Flags     byte
	Sectors   uint32
	TotalSize uint32
	UsedSize  uint32
	Supported bool
	Ready     bool
}

// Client is a synchronous MSP client wrapping a serial port.
type Client struct {
	port      SerialPort
	decoder   *FrameDecoder
	pending   map[uint16]*Frame
	timeout   time.Duration
	FCVariant string // "BTFL" or "INAV", set after detection
	deadline  time.Time
}

// NewClient creates a Client with the given serial port and response timeout.
func NewClient(port SerialPort, timeout time.Duration) *Client {
	return &Client{
		port:    port,
		decoder: NewFrameDecoder(),
		pending: make(map[uint16]*Frame),
		timeout: timeout,
	}
}

// Send encodes and writes an MSP v1 request frame to the serial port.
func (c *Client) Send(code byte, payload []byte) error {
	data := EncodeV1(code, payload)
	return c.writeFrame(data, uint16(code))
}

// SendV2 encodes and writes an MSP v2 request frame to the serial port.
// V2 frames use a 16-bit length field, enabling responses larger than 255 bytes.
func (c *Client) SendV2(code uint16, payload []byte) error {
	data := EncodeV2(code, payload)
	return c.writeFrame(data, code)
}

func (c *Client) writeFrame(data []byte, code uint16) error {
	n, err := c.port.Write(data)
	if err == nil && n != len(data) {
		err = io.ErrShortWrite
	}
	slog.Debug("MSP request", "code", code, "bytes", len(data), "written", n, "ok", err == nil)
	return err
}

// SetDeadline bounds subsequent receive operations as well as their per-request timeout.
func (c *Client) SetDeadline(deadline time.Time) { c.deadline = deadline }

// Receive blocks until a response frame with the given code arrives or the
// timeout expires. Decoded frames for other codes are buffered in pending.
func (c *Client) Receive(code uint16) (*Frame, error) {
	// Check pending buffer first.
	if f, ok := c.pending[code]; ok {
		delete(c.pending, code)
		return f, nil
	}

	deadline := time.Now().Add(c.timeout)
	if !c.deadline.IsZero() && c.deadline.Before(deadline) {
		deadline = c.deadline
	}
	buf := make([]byte, 4096)

	for time.Now().Before(deadline) {
		n, err := c.port.Read(buf)
		if err != nil && err != io.EOF {
			return nil, &Error{Message: fmt.Sprintf("read error: %v", err)}
		}
		if n > 0 {
			c.decoder.Feed(buf[:n])
		}

		// Drain decoded frames into the pending map.
		for _, f := range c.decoder.Frames {
			if f.Direction == MSPDirectionError && f.Code == code {
				c.decoder.Frames = c.decoder.Frames[:0]
				return nil, &Error{Message: fmt.Sprintf("FC rejected MSP code %d", code)}
			}
			if f.Direction == MSPDirectionFromFC {
				fc := f // copy
				c.pending[f.Code] = &fc
			}
		}
		c.decoder.Frames = c.decoder.Frames[:0]

		// Check if our target arrived.
		if f, ok := c.pending[code]; ok {
			delete(c.pending, code)
			slog.Debug("MSP response", "code", code, "payload_bytes", len(f.Payload))
			return f, nil
		}
		if n == 0 {
			time.Sleep(time.Millisecond)
		}
	}

	return nil, &TimeoutError{Message: fmt.Sprintf("timeout waiting for MSP code %d", code)}
}

// Request sends a command and waits for the matching response.
func (c *Client) Request(code byte, payload []byte) (*Frame, error) {
	c.FlushFrames(uint16(code))
	if err := c.Send(code, payload); err != nil {
		return nil, err
	}
	return c.Receive(uint16(code))
}

// FlushFrames removes any buffered frames for the given code.
func (c *Client) FlushFrames(code uint16) {
	delete(c.pending, code)
}

// Close closes the underlying serial port.
func (c *Client) Close() error {
	return c.port.Close()
}

// ---------- High-level commands ----------

// GetAPIVersion returns the MSP API major and minor version.
func (c *Client) GetAPIVersion() (major, minor int, err error) {
	f, err := c.Request(MSPAPIVersion, nil)
	if err != nil {
		return 0, 0, err
	}
	if len(f.Payload) < 3 {
		return 0, 0, &Error{Message: "MSP_API_VERSION payload too short"}
	}
	// payload: [protocolVersion, major, minor]
	return int(f.Payload[1]), int(f.Payload[2]), nil
}

// GetFCVariant returns the 4-character FC variant string (e.g. "BTFL").
func (c *Client) GetFCVariant() (string, error) {
	f, err := c.Request(MSPFCVariant, nil)
	if err != nil {
		return "", err
	}
	return string(f.Payload), nil
}

// GetFCVersion returns the firmware version string (e.g. "4.5.0").
// The payload is three bytes: [major, minor, patch].
func (c *Client) GetFCVersion() (string, error) {
	f, err := c.Request(MSPFCVersion, nil)
	if err != nil {
		return "", err
	}
	if len(f.Payload) < 3 {
		return "", fmt.Errorf("MSP_FC_VERSION payload too short (%d bytes)", len(f.Payload))
	}
	return fmt.Sprintf("%d.%d.%d", f.Payload[0], f.Payload[1], f.Payload[2]), nil
}

// GetUID returns the 12-byte board UID as a hex string.
func (c *Client) GetUID() (string, error) {
	f, err := c.Request(MSPUID, nil)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(f.Payload), nil
}

// GetBlackboxConfig returns the blackbox device type.
func (c *Client) GetBlackboxConfig() (deviceType int, err error) {
	f, err := c.Request(MSPBlackboxConfig, nil)
	if err != nil {
		return 0, err
	}
	if len(f.Payload) < 2 {
		return 0, &Error{Message: "MSP_BLACKBOX_CONFIG payload too short"}
	}
	if f.Payload[0] != 1 {
		return 0, &Error{Message: "FC blackbox support unavailable"}
	}
	if f.Payload[1] > BlackboxDeviceSerial {
		return 0, &Error{Message: "unknown blackbox storage device"}
	}
	return int(f.Payload[1]), nil
}

// GetDataflashSummary queries and parses the dataflash summary.
func (c *Client) GetDataflashSummary() (*DataflashSummary, error) {
	f, err := c.Request(MSPDataflashSummary, nil)
	if err != nil {
		return nil, err
	}
	if len(f.Payload) < 13 {
		return nil, &Error{Message: "MSP_DATAFLASH_SUMMARY payload too short"}
	}
	ds := &DataflashSummary{
		Flags:     f.Payload[0],
		Sectors:   binary.LittleEndian.Uint32(f.Payload[1:5]),
		TotalSize: binary.LittleEndian.Uint32(f.Payload[5:9]),
		UsedSize:  binary.LittleEndian.Uint32(f.Payload[9:13]),
	}
	ds.Supported = ds.Flags&DataflashFlagSupported != 0
	ds.Ready = ds.Flags&DataflashFlagReady != 0
	return ds, nil
}

// SendFlashReadRequest sends MSP_DATAFLASH_READ with the given parameters.
// Uses MSP v2 framing for larger response payloads (~4 KB vs 255 B with v1).
// Compression is deliberately unsupported; requests always use plain transfers.
func (c *Client) SendFlashReadRequest(address uint32, size uint16, compression bool) error {
	if compression {
		return &Error{Message: "flash compression is unsupported; set flash_read_compression=false"}
	}
	payload := make([]byte, 7)
	binary.LittleEndian.PutUint32(payload[0:4], address)
	binary.LittleEndian.PutUint16(payload[4:6], size)
	payload[6] = DataflashCompressionNone
	return c.SendV2(MSPDataflashRead, payload)
}

// ReceiveFlashReadResponse reads and parses the next MSP_DATAFLASH_READ response.
// Parsing is variant-aware: Betaflight includes length/compression headers,
// while iNav sends raw data after the address.
func (c *Client) ReceiveFlashReadResponse() (address uint32, data []byte, err error) {
	f, err := c.Receive(MSPDataflashRead)
	if err != nil {
		return 0, nil, err
	}
	p := f.Payload
	if len(p) < 4 {
		return 0, nil, &Error{Message: "MSP_DATAFLASH_READ payload too short"}
	}
	address = binary.LittleEndian.Uint32(p[0:4])

	if c.FCVariant == BTFLVariant {
		// Betaflight: addr(4) + dataSize(2) + compressionType(1) + data[dataSize]
		if len(p) < 7 {
			return address, nil, &Error{Message: "BTFL flash read payload too short"}
		}
		dataSize := int(binary.LittleEndian.Uint16(p[4:6]))
		compressionType := p[6]
		raw := p[7:]
		if len(raw) != dataSize {
			return address, nil, &Error{Message: "BTFL flash read length mismatch"}
		}
		raw = raw[:dataSize]

		if compressionType != DataflashCompressionNone {
			return address, nil, &Error{Message: "compressed flash responses are unsupported"}
		}
		data = raw
	} else {
		// iNav: addr(4) + raw data (no length/compression header)
		data = p[4:]
	}

	return address, data, nil
}

// ReadFlashChunk sends a flash read request and returns the parsed response.
func (c *Client) ReadFlashChunk(address uint32, size uint16, compression bool) (uint32, []byte, error) {
	if err := c.SendFlashReadRequest(address, size, compression); err != nil {
		return 0, nil, err
	}
	return c.ReceiveFlashReadResponse()
}

// EraseFlash sends MSP_DATAFLASH_ERASE (fire-and-forget, no response expected).
func (c *Client) EraseFlash() error {
	return c.Send(MSPDataflashErase, nil)
}
