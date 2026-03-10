package msp

// MSP preambles.
const (
	MSPPreambleV1 = "$M"
	MSPPreambleV2 = "$X"
)

// MSP direction bytes.
const (
	MSPDirectionToFC   byte = '<'
	MSPDirectionFromFC byte = '>'
	MSPDirectionError  byte = '!'
)

// Protocol overhead sizes.
const (
	MSPV1Overhead = 6
	MSPV2Overhead = 9
)

// MSP command codes.
const (
	MSPAPIVersion       = 1
	MSPFCVariant        = 2
	MSPFCVersion        = 3
	MSPBoardInfo        = 4
	MSPBuildInfo        = 5
	MSPUID              = 160
	MSPBlackboxConfig   = 80
	MSPDataflashSummary = 70
	MSPDataflashRead    = 71
	MSPDataflashErase   = 72
)

// Blackbox device types.
const (
	BlackboxDeviceNone   = 0
	BlackboxDeviceFlash  = 1
	BlackboxDeviceSDCard = 2
	BlackboxDeviceSerial = 3
)

// Dataflash flags.
const (
	DataflashFlagSupported = 0x01
	DataflashFlagReady     = 0x02
)

// Compression types.
const (
	DataflashCompressionNone    = 0
	DataflashCompressionHuffman = 1
)

// FC variant identifiers.
const (
	BTFLVariant = "BTFL"
	INAVVariant = "INAV"
)

// SupportedVariants lists recognized flight-controller variants.
var SupportedVariants = map[string]bool{
	"BTFL": true,
	"INAV": true,
}
