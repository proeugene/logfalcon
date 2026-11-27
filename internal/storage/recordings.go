package storage

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"strings"
	"time"
)

// RecordingHeader is optional evidence from a BBL header, not a decoded flight.
// Offset is the byte position of its Product header within the original dump.
type RecordingHeader struct {
	Offset    int64       `json:"offset"`
	CraftName string      `json:"craft_name,omitempty"`
	StartTime string      `json:"start_time,omitempty"`
	Gyro      *GyroFields `json:"gyro,omitempty"`
}

// GyroFields describes axes declared in the main frame header. Bits 0, 1 and 2
// are X, Y and Z. A nil *GyroFields means the field list could not be read.
type GyroFields struct {
	Filtered   uint8 `json:"filtered"`
	Unfiltered uint8 `json:"unfiltered"`
}

// UnmarshalJSON requires explicit zero masks to report absent axes. A partially
// written object must never turn a missing key into a known zero.
func (g *GyroFields) UnmarshalJSON(data []byte) error {
	var masks map[string]*uint8
	if err := json.Unmarshal(data, &masks); err != nil {
		return err
	}
	filtered, unfiltered := masks["filtered"], masks["unfiltered"]
	if len(masks) != 2 || filtered == nil || unfiltered == nil || *filtered > 7 || *unfiltered > 7 {
		return errors.New("gyro fields require both three-axis masks")
	}
	*g = GyroFields{Filtered: *filtered, Unfiltered: *unfiltered}
	return nil
}

const productHeader = "H Product:Blackbox flight data recorder by Nicholas Sherlock\n"
const maxRecordingHeaders = 256
const maxHeaderBytes = 64 * 1024
const maxHeaderLine = 8192

var recordingTime = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(\.[0-9]+)?(Z|[+-]([01][0-9]|2[0-3]):[0-5][0-9])$`)
var fieldName = regexp.MustCompile(`^[A-Za-z_][A-Za-z_0-9]*(\[(0|[1-9][0-9]*)\])?$`)

const mainFieldName = "H Field I name"

func readGyroFields(list string) *GyroFields {
	// Betaflight's I-frame schema starts with these unconditional fields.
	// A clipped front or an arbitrary list cannot attest to gyro absence.
	if list != "loopIteration,time" && !strings.HasPrefix(list, "loopIteration,time,") {
		return nil
	}
	var axes GyroFields
	seen := make(map[string]bool)
	for name := range strings.SplitSeq(list, ",") {
		if seen[name] || !fieldName.MatchString(name) {
			return nil
		}
		seen[name] = true
		base, index, _ := strings.Cut(name, "[")
		if base != "gyroADC" && base != "gyroUnfilt" {
			continue
		}
		if len(index) != 2 || index[0] < '0' || index[0] > '2' {
			return nil
		}
		axis := uint8(1) << (index[0] - '0')
		if base == "gyroADC" {
			axes.Filtered |= axis
		} else {
			axes.Unfiltered |= axis
		}
	}
	return &axes
}

// ValidRecordingStart excludes unset controller clocks and malformed RFC3339.
func ValidRecordingStart(value string) bool {
	if len(value) > 64 || !recordingTime.MatchString(value) {
		return false
	}
	stamp, err := time.Parse(time.RFC3339Nano, value)
	return err == nil && stamp.Year() > 0
}

// ReadHeaderPrefix recovers a craft name for legacy downloads without scanning
// the dump or modifying an older session's manifest.
func ReadHeaderPrefix(r io.Reader) []RecordingHeader {
	var p headerParser
	b, _ := io.ReadAll(io.LimitReader(r, maxHeaderBytes))
	p.Write(b)
	return p.recordings
}

// headerParser scans the existing write stream with bounded memory and work per
// byte. Header bodies and lines are bounded; arbitrary binary data is never kept.
type headerParser struct {
	buf           []byte
	offset        int64
	active        bool
	headerBytes   int
	recordings    []RecordingHeader
	seenFieldList bool
}

func (p *headerParser) Write(data []byte) {
	for len(data) > 0 && (len(p.recordings) < maxRecordingHeaders || p.active) {
		n := min(len(data), 4096)
		p.buf = append(p.buf, data[:n]...)
		data = data[n:]
		p.consume()
	}
}

func (p *headerParser) drop(n int) { p.buf = p.buf[n:]; p.offset += int64(n) }

func (p *headerParser) consume() {
	for {
		if !p.active {
			if len(p.recordings) >= maxRecordingHeaders {
				p.buf = nil
				return
			}
			i := bytes.Index(p.buf, []byte(productHeader))
			if i < 0 {
				if n := len(p.buf) - len(productHeader) + 1; n > 0 {
					p.drop(n)
				}
				return
			}
			p.drop(i)
			p.recordings = append(p.recordings, RecordingHeader{Offset: p.offset})
			p.drop(len(productHeader))
			p.active = true
			p.headerBytes = len(productHeader)
			p.seenFieldList = false
		}
		if len(p.buf) < 2 {
			return
		}
		if !bytes.HasPrefix(p.buf, []byte("H ")) {
			p.active = false
			continue
		}
		r := &p.recordings[len(p.recordings)-1]
		// Even a second, interrupted field declaration makes the first one ambiguous.
		if p.seenFieldList && bytes.HasPrefix(p.buf, []byte(mainFieldName)) {
			r.Gyro = nil
		}
		end := bytes.IndexByte(p.buf, '\n')
		if end < 0 {
			if len(p.buf) > maxHeaderLine || p.headerBytes+len(p.buf) > maxHeaderBytes {
				r.Gyro = nil // The rest of this header cannot be checked.
				p.active = false
				continue
			}
			return
		}
		if end > maxHeaderLine || p.headerBytes+end+1 > maxHeaderBytes {
			r.Gyro = nil
			p.active = false
			continue
		}
		// An interrupted header line can run directly into the next recording.
		// Its complete Product marker ends at this newline in either case.
		if bytes.HasSuffix(p.buf[:end+1], []byte(productHeader)) {
			p.drop(end + 1 - len(productHeader))
			p.active = false
			continue
		}
		line := strings.TrimSuffix(string(p.buf[:end]), "\r")
		if strings.HasPrefix(line, mainFieldName) {
			if list, ok := strings.CutPrefix(line, mainFieldName+":"); ok && !p.seenFieldList {
				r.Gyro = readGyroFields(list)
			}
			p.seenFieldList = true
		}
		if value, ok := strings.CutPrefix(line, "H Craft name:"); ok {
			value = strings.TrimSpace(value)
			if len(value) <= 80 && !strings.ContainsAny(value, "\x00\r\n") {
				r.CraftName = value
			}
		}
		if value, ok := strings.CutPrefix(line, "H Log start datetime:"); ok {
			r.StartTime = ""
			if ValidRecordingStart(value) {
				// Retain the controller's original timezone offset and precision.
				r.StartTime = value
			}
		}
		p.headerBytes += end + 1
		p.drop(end + 1)
	}
}
