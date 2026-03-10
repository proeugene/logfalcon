package led

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// mockBackend records LED state for testing.
type mockBackend struct {
	mu              sync.Mutex
	on              bool
	setCount        int64 // atomic
	disableTrigger  bool
	restoreTrigger  bool
}

func (m *mockBackend) Set(on bool) {
	m.mu.Lock()
	m.on = on
	m.mu.Unlock()
	atomic.AddInt64(&m.setCount, 1)
}

func (m *mockBackend) DisableTrigger() {
	m.mu.Lock()
	m.disableTrigger = true
	m.mu.Unlock()
}

func (m *mockBackend) RestoreTrigger() {
	m.mu.Lock()
	m.restoreTrigger = true
	m.mu.Unlock()
}

func (m *mockBackend) isOn() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.on
}

func (m *mockBackend) getSetCount() int64 {
	return atomic.LoadInt64(&m.setCount)
}

func (m *mockBackend) didDisableTrigger() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.disableTrigger
}

func (m *mockBackend) didRestoreTrigger() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.restoreTrigger
}

func TestControllerStartStop(t *testing.T) {
	mb := &mockBackend{}
	c := NewWithBackend(mb)

	c.Start()
	// Give goroutine time to start.
	time.Sleep(50 * time.Millisecond)

	c.mu.Lock()
	running := c.running
	c.mu.Unlock()
	if !running {
		t.Fatal("expected controller to be running after Start()")
	}
	if !mb.didDisableTrigger() {
		t.Fatal("expected DisableTrigger to be called on Start()")
	}

	c.Stop()

	c.mu.Lock()
	running = c.running
	c.mu.Unlock()
	if running {
		t.Fatal("expected controller to not be running after Stop()")
	}
	if !mb.didRestoreTrigger() {
		t.Fatal("expected RestoreTrigger to be called on Stop()")
	}
	if mb.isOn() {
		t.Fatal("expected LED to be off after Stop()")
	}
}

func TestSetState(t *testing.T) {
	mb := &mockBackend{}
	c := NewWithBackend(mb)

	for _, st := range []State{Off, Booting, Ready, Busy, Done, Error} {
		c.SetState(st)
		if got := c.GetState(); got != st {
			t.Fatalf("GetState() = %d, want %d", got, st)
		}
	}
}

func TestReadyPattern(t *testing.T) {
	mb := &mockBackend{}
	c := NewWithBackend(mb)

	c.Start()
	defer c.Stop()

	c.SetState(Ready)
	// Allow pattern loop to process.
	time.Sleep(100 * time.Millisecond)

	if !mb.isOn() {
		t.Fatal("expected LED to be on in Ready state")
	}
}

func TestDonePattern(t *testing.T) {
	mb := &mockBackend{}
	c := NewWithBackend(mb)

	c.Start()
	defer c.Stop()

	c.SetState(Done)

	// Done pattern: 5×(50+50) + 3000+1 = 3501ms total.
	// WaitUntilIdle should return true within a generous timeout.
	if !c.WaitUntilIdle(6 * time.Second) {
		t.Fatal("expected WaitUntilIdle to return true for Done pattern")
	}

	// After Done completes, LED should be off.
	time.Sleep(50 * time.Millisecond)
	if mb.isOn() {
		t.Fatal("expected LED to be off after Done pattern completes")
	}
}

func TestInterruptPattern(t *testing.T) {
	mb := &mockBackend{}
	c := NewWithBackend(mb)

	c.Start()
	defer c.Stop()

	// Start a repeating pattern.
	c.SetState(Busy)
	time.Sleep(100 * time.Millisecond)

	// Interrupt with Done.
	c.SetState(Done)

	// Done should complete and signal idle.
	if !c.WaitUntilIdle(6 * time.Second) {
		t.Fatal("expected WaitUntilIdle to return true after interrupting Busy with Done")
	}

	if got := c.GetState(); got != Done {
		t.Fatalf("GetState() = %d, want Done(%d)", got, Done)
	}
}

func TestWaitUntilIdleTimeout(t *testing.T) {
	mb := &mockBackend{}
	c := NewWithBackend(mb)

	c.Start()
	defer c.Stop()

	// Booting is a repeating pattern; idle should never fire.
	c.SetState(Booting)

	if c.WaitUntilIdle(200 * time.Millisecond) {
		t.Fatal("expected WaitUntilIdle to timeout for a repeating pattern")
	}
}

func TestOffPattern(t *testing.T) {
	mb := &mockBackend{}
	c := NewWithBackend(mb)

	c.Start()
	defer c.Stop()

	// Default state is Off.
	time.Sleep(50 * time.Millisecond)

	if mb.isOn() {
		t.Fatal("expected LED to be off in Off state")
	}
}

func TestNewDefaultsSysfs(t *testing.T) {
	c := New("sysfs", 0)
	if _, ok := c.backend.(*sysfsBackend); !ok {
		t.Fatal("expected sysfs backend for 'sysfs' argument")
	}
}

func TestNewGPIOBackend(t *testing.T) {
	c := New("gpio", 17)
	if _, ok := c.backend.(*gpioBackend); !ok {
		t.Fatal("expected gpio backend for 'gpio' argument")
	}
}
