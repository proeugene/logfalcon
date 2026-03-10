package msp

import "testing"

func TestCRC8Xor(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want byte
	}{
		{"empty", nil, 0x00},
		{"single byte 0x47", []byte{0x47}, 0x47},
		{"two bytes 0x00 0x01", []byte{0x00, 0x01}, 0x01},
		{"all same", []byte{0xAA, 0xAA}, 0x00},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := CRC8Xor(tc.data); got != tc.want {
				t.Errorf("CRC8Xor(%v) = 0x%02x, want 0x%02x", tc.data, got, tc.want)
			}
		})
	}
}

func TestCRC8DVBS2(t *testing.T) {
	tests := []struct {
		name    string
		data    []byte
		initial byte
		want    byte
	}{
		{"empty initial=0", nil, 0x00, 0x00},
		{"empty initial=0xFF", nil, 0xFF, 0xFF},
		{"five bytes", []byte{0x00, 0x47, 0x00, 0x00, 0x00}, 0x00, crc8DVBS2Reference([]byte{0x00, 0x47, 0x00, 0x00, 0x00}, 0x00)},
		{"chained", []byte{0x01, 0x02}, CRC8DVBS2([]byte{0x03, 0x04}, 0x00), crc8DVBS2Reference([]byte{0x03, 0x04, 0x01, 0x02}, 0x00)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := CRC8DVBS2(tc.data, tc.initial); got != tc.want {
				t.Errorf("CRC8DVBS2(%v, 0x%02x) = 0x%02x, want 0x%02x", tc.data, tc.initial, got, tc.want)
			}
		})
	}
}

// crc8DVBS2Reference is a bit-by-bit reference implementation used for validation.
func crc8DVBS2Reference(data []byte, initial byte) byte {
	crc := initial
	for _, b := range data {
		crc ^= b
		for i := 0; i < 8; i++ {
			if crc&0x80 != 0 {
				crc = (crc << 1) ^ 0xD5
			} else {
				crc = crc << 1
			}
		}
	}
	return crc
}

func TestCRC8DVBS2AgainstReference(t *testing.T) {
	vectors := [][]byte{
		{0x00},
		{0x47},
		{0x00, 0x47, 0x00, 0x00, 0x00},
		{0xFF, 0xFE, 0xFD},
		{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08},
	}
	for _, data := range vectors {
		want := crc8DVBS2Reference(data, 0)
		got := CRC8DVBS2(data, 0)
		if got != want {
			t.Errorf("CRC8DVBS2(%v, 0) = 0x%02x, reference = 0x%02x", data, got, want)
		}
	}
}
