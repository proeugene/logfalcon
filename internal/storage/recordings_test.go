package storage

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestRecordingHeadersMixedTimesAndBoundaries(t *testing.T) {
	times := []string{"2026-11-11T12:34:56.123+05:30", "0000-01-01T00:00:00.000+00:00", "bad", "", "2026-11-11T06:04:56-01:00"}
	var data []byte
	var want []RecordingHeader
	for _, stamp := range times {
		r := RecordingHeader{Offset: int64(len(data)), CraftName: "DARKSTAR20"}
		if stamp != "" && stamp != "bad" && !strings.HasPrefix(stamp, "0000") {
			r.StartTime = stamp
		}
		want = append(want, r)
		data = append(data, []byte(productHeader+"H Craft name:DARKSTAR20\nH Log start datetime:"+stamp+"\n")...)
		data = append(data, bytes.Repeat([]byte{0xff}, 2000)...)
	}
	for _, chunk := range []int{1, 17, 4096, len(data)} {
		var p headerParser
		for start := 0; start < len(data); start += chunk {
			p.Write(data[start:min(start+chunk, len(data))])
		}
		if !reflect.DeepEqual(p.recordings, want) {
			t.Fatalf("chunk %d: got %#v want %#v", chunk, p.recordings, want)
		}
	}
}

func TestRecordingHeadersBoundedAndOptional(t *testing.T) {
	var p headerParser
	p.Write([]byte(productHeader + "H Craft name:" + strings.Repeat("a", 10000) + "\n"))
	p.Write([]byte(strings.Repeat(productHeader+"H Craft name:Small\n", maxRecordingHeaders+50)))
	if len(p.recordings) != maxRecordingHeaders {
		t.Fatal(len(p.recordings))
	}
	if p.recordings[0].CraftName != "" {
		t.Fatal("accepted oversized header")
	}
	if len(p.buf) > maxHeaderLine+4096 {
		t.Fatal("unbounded parser buffer")
	}
	w, err := NewStreamWriter(t.TempDir() + "/copy.bbl")
	if err != nil {
		t.Fatal(err)
	}
	data := []byte("not a valid blackbox header\x00")
	if _, err = w.Write(data); err != nil {
		t.Fatal(err)
	}
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
	if ok, _, err := w.VerifyAgainstFile(); !ok || err != nil {
		t.Fatalf("optional parsing affected copy: %v", err)
	}
	if len(w.RecordingHeaders()) != 0 {
		t.Fatal("invented header metadata")
	}
}

func TestRecordingAfterInterruptedHeaderKeepsItsOwnMetadata(t *testing.T) {
	first := productHeader + "H Craft name:First\nH Firmware revision:partial\xff"
	stamp := "2026-11-10T12:23:34+01:00"
	data := []byte(first + productHeader + "H Craft name:Second\nH Log start datetime:" + stamp + "\n")
	want := []RecordingHeader{{CraftName: "First"}, {Offset: int64(len(first)), CraftName: "Second", StartTime: stamp}}
	for _, chunk := range []int{1, len(data)} {
		var p headerParser
		for start := 0; start < len(data); start += chunk {
			p.Write(data[start:min(start+chunk, len(data))])
		}
		if !reflect.DeepEqual(p.recordings, want) {
			t.Fatalf("chunk %d: got %#v, want %#v", chunk, p.recordings, want)
		}
	}
}

func TestRecordingStartRejectsUnsetAndMalformedValues(t *testing.T) {
	for _, value := range []string{"", "0000-01-01T00:00:00.000+00:00", "2026-02-30T12:00:00Z", "2026-11-11T12:00:00+24:00", "2026-11-11T12:00:00+00:60", "2026-11-11T1:00:00Z", "2026-11-11T12:00:00,123Z"} {
		if ValidRecordingStart(value) {
			t.Fatalf("accepted malformed time %q", value)
		}
	}
	for _, value := range []string{"2026-11-11T12:00:00Z", "2026-11-11T12:00:00.123-03:30"} {
		if !ValidRecordingStart(value) {
			t.Fatalf("rejected valid time %q", value)
		}
	}
}

func TestLastRetainedHeaderCompletesAcrossWrites(t *testing.T) {
	var p headerParser
	p.Write([]byte(strings.Repeat(productHeader+"H Craft name:Earlier\n", maxRecordingHeaders-1)))
	stamp := "2026-11-11T12:34:56.123-03:30"
	p.Write([]byte(productHeader + "H Craft name:Final\nH Log start datetime:" + stamp[:16]))
	if len(p.recordings) != maxRecordingHeaders || !p.active {
		t.Fatal("final header not active")
	}
	for _, b := range []byte(stamp[16:] + "\n") {
		p.Write([]byte{b})
	}
	p.Write([]byte("\x00" + strings.Repeat(productHeader+"H Craft name:Ignored\n", 100)))
	if len(p.recordings) != maxRecordingHeaders {
		t.Fatal("header cap exceeded")
	}
	last := p.recordings[maxRecordingHeaders-1]
	if last.CraftName != "Final" || last.StartTime != stamp {
		t.Fatalf("final header lost: %#v", last)
	}
	if p.active || len(p.buf) != 0 {
		t.Fatal("parser kept scanning after final header")
	}
}

func TestGyroFieldsFromRecordingHeader(t *testing.T) {
	const main = "H Field I name:"
	for _, tc := range []struct {
		name, body string
		want       *GyroFields
	}{
		{"all axes", main + "loopIteration,time,gyroADC[0],gyroADC[1],gyroADC[2],gyroUnfilt[0],gyroUnfilt[1],gyroUnfilt[2]\n", &GyroFields{7, 7}},
		{"partial axes", main + "loopIteration,time,gyroADC[2],gyroADC[0],gyroUnfilt[1]\r\n", &GyroFields{5, 2}},
		{"no gyro fields", main + "loopIteration,time,gyroADCExtra[0],eRPM[0]\n", &GyroFields{}},
		{"header only", main + "loopIteration,time,gyroUnfilt[0]\n", &GyroFields{Unfiltered: 1}},
		{"wrong frame", "H Field P name:gyroADC[1]\n", nil},
		{"missing", "H Craft name:DARKSTAR20\n", nil},
		{"truncated", main + "loopIteration,time,gyroADC[1]", nil},
		{"empty", main + "\n", nil},
		{"missing required fields", main + "gyroADC[0]\n", nil},
		{"wrong field order", main + "time,loopIteration,gyroADC[0]\n", nil},
		{"trailing comma", main + "loopIteration,time,\n", nil},
		{"malformed", main + "loopIteration,time,gyroADC[0]garbage\n", nil},
		{"invalid gyro axis", main + "loopIteration,time,gyroADC[3]\n", nil},
		{"duplicate field", main + "loopIteration,time,gyroADC[0],gyroADC[0]\n", nil},
		{"conflicting lists", main + "loopIteration,time,gyroADC[1]\n" + main + "loopIteration,time\n", nil},
		{"interrupted second list", main + "loopIteration,time,gyroADC[1]\n" + main + "gyroADC[2]", nil},
		{"hidden conflict beyond line cap", main + "loopIteration,time\nH large:" + strings.Repeat("x", maxHeaderLine) + "\n" + main + "loopIteration,time,gyroADC[0]\n", nil},
		{"hidden conflict beyond header cap", main + "loopIteration,time\n" + strings.Repeat("H large:"+strings.Repeat("x", 4000)+"\n", maxHeaderBytes/4000+1) + main + "loopIteration,time,gyroADC[0]\n", nil},
		{"oversize", main + strings.Repeat("a", maxHeaderLine) + "\n", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := []byte(productHeader + tc.body)
			for _, chunk := range []int{1, 17, len(data)} {
				var p headerParser
				for start := 0; start < len(data); start += chunk {
					p.Write(data[start:min(start+chunk, len(data))])
				}
				if len(p.recordings) != 1 || !reflect.DeepEqual(p.recordings[0].Gyro, tc.want) {
					t.Fatalf("chunk %d: got %#v; want %+v", chunk, p.recordings, tc.want)
				}
			}
		})
	}
}

func TestGyroFieldsAfterInterruptedAdjacentHeader(t *testing.T) {
	first := productHeader + "H Field I name:loopIteration,time,gyroADC[0]\nH Field I name:interrupted"
	data := []byte(first + productHeader + "H Field I name:loopIteration,time,gyroADC[2]\n")
	var p headerParser
	for _, b := range data {
		p.Write([]byte{b})
	}
	if len(p.recordings) != 2 || p.recordings[0].Gyro != nil {
		t.Fatalf("interrupted header: %+v", p.recordings)
	}
	if next := p.recordings[1]; next.Offset != int64(len(first)) || !reflect.DeepEqual(next.Gyro, &GyroFields{Filtered: 4}) {
		t.Fatalf("next header: %+v", next)
	}
}

func TestStoredGyroFieldsRequireBothMasks(t *testing.T) {
	for _, encoded := range []string{
		`{}`, `{"offset":0,"gyro":null}`,
	} {
		var h RecordingHeader
		if err := json.Unmarshal([]byte(encoded), &h); err != nil || h.Gyro != nil {
			t.Fatalf("legacy unknown fields %s: %+v, %v", encoded, h, err)
		}
	}
	for _, fields := range []string{
		`{}`, `{"filtered":7}`, `{"unfiltered":0}`,
		`{"filtered":null,"unfiltered":7}`, `{"filtered":0,"unfiltered":8}`,
		`{"filtered":"7","unfiltered":0}`,
	} {
		var h RecordingHeader
		if err := json.Unmarshal([]byte(`{"gyro":`+fields+`}`), &h); err == nil {
			t.Errorf("accepted ambiguous stored masks %s", fields)
		}
	}
	for _, want := range []GyroFields{{0, 0}, {7, 0}, {2, 5}} {
		data, err := json.Marshal(RecordingHeader{Gyro: &want})
		if err != nil {
			t.Fatal(err)
		}
		var h RecordingHeader
		if err := json.Unmarshal(data, &h); err != nil || h.Gyro == nil || *h.Gyro != want {
			t.Fatalf("round-trip of %s: %+v, %v", data, h, err)
		}
	}
}
