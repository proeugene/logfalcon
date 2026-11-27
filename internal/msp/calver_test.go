package msp

import "testing"

func TestCalendarVersionReplies(t *testing.T) {
	for _, tt := range []struct {
		payload []byte
		want    string
	}{
		{[]byte{4, 5, 2}, "4.5.2"},
		{[]byte{26, 6, 2}, "2026.6.2"},
		{[]byte{26, 6, 2, 8, '2', '0', '2', '6', '.', '6', '.', '2'}, "2026.6.2"},
	} {
		got, err := parseFCVersion(tt.payload)
		if err != nil || got != tt.want {
			t.Fatalf("%v: got=%q err=%v", tt.payload, got, err)
		}
	}
	for _, b := range [][]byte{nil, {26, 6}, {26, 6, 2, 0}, {26, 6, 2, 8, 'a'}, {26, 6, 2, 1, 0}, {26, 6, 2, 65}} {
		if _, err := parseFCVersion(b); err == nil {
			t.Fatalf("malformed reply accepted: %v", b)
		}
	}
}
