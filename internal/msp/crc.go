package msp

// crc8Table is the precomputed CRC8-DVB-S2 lookup table.
// Polynomial: 0xD5.
var crc8Table [256]byte

func init() {
	for i := 0; i < 256; i++ {
		crc := byte(i)
		for bit := 0; bit < 8; bit++ {
			if crc&0x80 != 0 {
				crc = (crc << 1) ^ 0xD5
			} else {
				crc = crc << 1
			}
		}
		crc8Table[i] = crc
	}
}

// CRC8Xor computes the XOR checksum used in MSP v1 frames.
func CRC8Xor(data []byte) byte {
	var crc byte
	for _, b := range data {
		crc ^= b
	}
	return crc
}

// CRC8DVBS2 computes the CRC8-DVB-S2 checksum used in MSP v2 frames.
func CRC8DVBS2(data []byte, initial byte) byte {
	crc := initial
	for _, b := range data {
		crc = crc8Table[crc^b]
	}
	return crc
}
