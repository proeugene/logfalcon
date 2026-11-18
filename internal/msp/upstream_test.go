package msp

import (
	"encoding/hex"
	"os"
	"strings"
	"testing"
)

// Fixed synthetic wire packets, not produced with LogFalcon's frame encoder.
// See testdata/README.md for upstream revision and the limit of this evidence.
func fixtureClient(t *testing.T, name string) *Client {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name + ".hex")
	if err != nil {
		t.Fatal(err)
	}
	wire, err := hex.DecodeString(strings.TrimSpace(string(b)))
	if err != nil {
		t.Fatal(err)
	}
	c, _ := newTestClient(wire)
	c.FCVariant = BTFLVariant
	return c
}
func TestPinnedBetaflightFixtures(t *testing.T) {
	if device, err := fixtureClient(t, "blackbox_spi").GetBlackboxConfig(); err != nil || device != BlackboxDeviceFlash {
		t.Fatalf("device=%d error=%v", device, err)
	}
	summary, err := fixtureClient(t, "summary_ready").GetDataflashSummary()
	if err != nil || !summary.Ready || !summary.Supported || summary.UsedSize != 65537 || summary.TotalSize != 4194304 {
		t.Fatalf("summary=%+v error=%v", summary, err)
	}
	address, data, err := fixtureClient(t, "flash_plain").ReceiveFlashReadResponse()
	if err != nil || address != 65536 || string(data) != "ABCD" {
		t.Fatalf("address=%d data=%q error=%v", address, data, err)
	}
	if _, _, err = fixtureClient(t, "flash_compressed").ReceiveFlashReadResponse(); err == nil {
		t.Fatal("compressed response accepted")
	}
	c, port := newTestClient(nil)
	if err = c.SendFlashReadRequest(0, 4, true); err == nil || port.writeBuf.Len() != 0 {
		t.Fatal("compression request reached serial port")
	}
}

// Independently authored field values for Betaflight 2026.6.2 e0b7bb01...;
// fixed response frames never use the production encoder.
func TestBetaflight2026Fixtures(t *testing.T) {
	if got, err := fixtureClient(t, "bf2026_version").GetFCVersion(); err != nil || got != "2026.6.2" {
		t.Fatalf("version=%q err=%v", got, err)
	}
	if major, minor, err := fixtureClient(t, "bf2026_api").GetAPIVersion(); err != nil || major != 1 || minor != 48 {
		t.Fatalf("api=%d.%d err=%v", major, minor, err)
	}
	if device, err := fixtureClient(t, "bf2026_blackbox").GetBlackboxConfig(); err != nil || device != BlackboxDeviceFlash {
		t.Fatalf("device=%d err=%v", device, err)
	}
	s, err := fixtureClient(t, "bf2026_summary").GetDataflashSummary()
	if err != nil || !s.Ready || !s.Supported || s.TotalSize != 8388608 || s.UsedSize != 65540 {
		t.Fatalf("summary=%+v err=%v", s, err)
	}
	address, data, err := fixtureClient(t, "bf2026_plain").ReceiveFlashReadResponse()
	if err != nil || address != 65536 || string(data) != "BF26" {
		t.Fatalf("address=%d data=%q err=%v", address, data, err)
	}
}
