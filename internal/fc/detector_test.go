package fc

import (
	"errors"
	"testing"

	"github.com/proeugene/logfalcon/internal/msp"
)

// mockClient implements MSPClient for testing.
type mockClient struct {
	apiMajor, apiMinor int
	variant            string
	uid                string
	bbDevice           int
	apiErr             error
	varErr             error
	uidErr             error
	bbErr              error
	bbCalled           bool // tracks whether GetBlackboxConfig was invoked
}

func (m *mockClient) GetAPIVersion() (int, int, error) {
	return m.apiMajor, m.apiMinor, m.apiErr
}

func (m *mockClient) GetFCVariant() (string, error) {
	return m.variant, m.varErr
}

func (m *mockClient) GetUID() (string, error) {
	return m.uid, m.uidErr
}

func (m *mockClient) GetBlackboxConfig() (int, error) {
	m.bbCalled = true
	return m.bbDevice, m.bbErr
}

func TestDetectBTFL(t *testing.T) {
	c := &mockClient{
		apiMajor: 1, apiMinor: 46,
		variant:  msp.BTFLVariant,
		uid:      "abcdef123456",
		bbDevice: msp.BlackboxDeviceFlash,
	}

	info, err := Detect(c)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.APIMajor != 1 || info.APIMinor != 46 {
		t.Errorf("API version = %d.%d, want 1.46", info.APIMajor, info.APIMinor)
	}
	if info.Variant != msp.BTFLVariant {
		t.Errorf("Variant = %q, want %q", info.Variant, msp.BTFLVariant)
	}
	if info.UID != "abcdef123456" {
		t.Errorf("UID = %q, want %q", info.UID, "abcdef123456")
	}
	if info.BlackboxDevice != msp.BlackboxDeviceFlash {
		t.Errorf("BlackboxDevice = %d, want %d", info.BlackboxDevice, msp.BlackboxDeviceFlash)
	}
	if !c.bbCalled {
		t.Error("expected GetBlackboxConfig to be called for BTFL")
	}
}

func TestDetectINAV(t *testing.T) {
	c := &mockClient{
		apiMajor: 2, apiMinor: 5,
		variant:  msp.INAVVariant,
		uid:      "inavuid001",
		bbDevice: msp.BlackboxDeviceNone, // should be ignored
	}

	info, err := Detect(c)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.Variant != msp.INAVVariant {
		t.Errorf("Variant = %q, want %q", info.Variant, msp.INAVVariant)
	}
	if info.BlackboxDevice != msp.BlackboxDeviceFlash {
		t.Errorf("BlackboxDevice = %d, want %d (flash assumed for INAV)", info.BlackboxDevice, msp.BlackboxDeviceFlash)
	}
	if c.bbCalled {
		t.Error("GetBlackboxConfig should NOT be called for INAV")
	}
}

func TestDetectUnsupported(t *testing.T) {
	c := &mockClient{
		apiMajor: 1, apiMinor: 0,
		variant: "ARDU",
	}

	_, err := Detect(c)
	if err == nil {
		t.Fatal("expected error for unsupported variant")
	}
	var nse *NotSupportedError
	if !errors.As(err, &nse) {
		t.Fatalf("expected NotSupportedError, got %T: %v", err, err)
	}
	if nse.Variant != "ARDU" {
		t.Errorf("Variant = %q, want %q", nse.Variant, "ARDU")
	}
}

func TestDetectSDCard(t *testing.T) {
	c := &mockClient{
		apiMajor: 1, apiMinor: 46,
		variant:  msp.BTFLVariant,
		uid:      "uid",
		bbDevice: msp.BlackboxDeviceSDCard,
	}

	_, err := Detect(c)
	if err == nil {
		t.Fatal("expected error for SD card blackbox")
	}
	var sdErr *SDCardError
	if !errors.As(err, &sdErr) {
		t.Fatalf("expected SDCardError, got %T: %v", err, err)
	}
}

func TestDetectUIDError(t *testing.T) {
	c := &mockClient{
		apiMajor: 1, apiMinor: 46,
		variant:  msp.BTFLVariant,
		uidErr:   errors.New("timeout"),
		bbDevice: msp.BlackboxDeviceFlash,
	}

	info, err := Detect(c)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.UID != "unknown" {
		t.Errorf("UID = %q, want %q", info.UID, "unknown")
	}
}

func TestDetectAPIError(t *testing.T) {
	c := &mockClient{
		apiErr: errors.New("serial read timeout"),
	}

	_, err := Detect(c)
	if err == nil {
		t.Fatal("expected error for API version failure")
	}
	var de *DetectionError
	if !errors.As(err, &de) {
		t.Fatalf("expected DetectionError, got %T: %v", err, err)
	}
}
