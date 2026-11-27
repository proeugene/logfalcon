package sync

import (
	"io"
	"sync"
	"testing"
	"time"

	goSerial "go.bug.st/serial"
)

type blockedReadinessPort struct {
	goSerial.Port
	closed chan struct{}
	once   sync.Once
}

func (p *blockedReadinessPort) SetReadTimeout(time.Duration) error { return nil }
func (p *blockedReadinessPort) Write([]byte) (int, error) {
	<-p.closed
	return 0, io.ErrClosedPipe
}
func (p *blockedReadinessPort) Close() error {
	p.once.Do(func() { close(p.closed) })
	return nil
}

func TestReadinessDeadlineClosesBlockedWrite(t *testing.T) {
	o, _ := transferFixture(t, 1, "")
	port := &blockedReadinessPort{closed: make(chan struct{})}
	o.OpenPort = func(string, *goSerial.Mode) (goSerial.Port, error) { return port, nil }
	finished := make(chan error, 1)
	go func() {
		_, _, err := o.openReadyPort("fake", time.Now().Add(50*time.Millisecond))
		finished <- err
	}()
	select {
	case err := <-finished:
		if err == nil {
			t.Fatal("blocked readiness write reported ready")
		}
	case <-time.After(time.Second):
		_ = port.Close()
		if err := <-finished; err == nil {
			t.Error("closed readiness port reported ready")
		}
		t.Fatal("readiness deadline did not interrupt blocked serial write")
	}
}

func TestReadinessDeadlineDoesNotReadOrEraseFlash(t *testing.T) {
	o, peer := transferFixture(t, 4096, "")
	o.Config.SerialTimeout = .01
	peer.apiSilentFor = 100
	port, reason, err := o.openReadyPort("fake", time.Now().Add(40*time.Millisecond))
	if err == nil || port != nil || reason != "controller_not_ready" || peer.requests != 0 || peer.eraseCount != 0 {
		t.Fatalf("port=%v reason=%s err=%v requests=%d erases=%d", port, reason, err, peer.requests, peer.eraseCount)
	}
}
