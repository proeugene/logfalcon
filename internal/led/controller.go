package led

import (
	"log"
	"os"
	"sync"
	"time"
)

// State represents the LED display mode.
type State int

const (
	Off     State = iota
	Booting       // 1s on / 1s off heartbeat
	Ready         // solid on
	Busy          // 150ms on / 150ms off fast blink
	Done          // 5× rapid flash then 3s solid then off
	Error         // SOS pattern
)

// Backend abstracts LED hardware control.
type Backend interface {
	Set(on bool)
	DisableTrigger()
	RestoreTrigger()
}

type step struct {
	onMs  int
	offMs int
}

type pattern struct {
	steps  []step
	repeat bool
}

var patterns = map[State]pattern{
	Off:     {steps: nil, repeat: false},
	Booting: {steps: []step{{1000, 1000}}, repeat: true},
	Ready:   {steps: nil, repeat: true},
	Busy:    {steps: []step{{150, 150}}, repeat: true},
	Done: {steps: []step{
		{50, 50}, {50, 50}, {50, 50}, {50, 50}, {50, 50},
		{3000, 1},
	}, repeat: false},
	Error: {steps: []step{
		// 3× short
		{150, 150}, {150, 150}, {150, 150},
		// 3× long
		{400, 150}, {400, 150}, {400, 150},
		// 3× short
		{150, 150}, {150, 150}, {150, 150},
		// pause
		{700, 700},
	}, repeat: true},
}

// Controller drives an LED through state-based blink patterns.
type Controller struct {
	mu      sync.Mutex
	state   State
	changed chan struct{} // signal state change
	idle    chan struct{} // signaled when non-repeating pattern completes
	running bool
	stop    chan struct{}
	done    chan struct{} // closed when goroutine exits
	backend Backend
}

// New creates a Controller with the specified backend.
// Supported backend names: "sysfs", "gpio".
// gpioPin is used only for the gpio backend.
func New(backend string, gpioPin int) *Controller {
	c := &Controller{
		state:   Off,
		changed: make(chan struct{}, 1),
		idle:    make(chan struct{}, 1),
		stop:    make(chan struct{}),
		done:    make(chan struct{}),
	}
	switch backend {
	case "gpio":
		c.backend = &gpioBackend{pin: gpioPin}
	default:
		c.backend = &sysfsBackend{}
	}
	return c
}

// NewWithBackend creates a Controller using a caller-supplied Backend (useful for testing).
func NewWithBackend(b Backend) *Controller {
	return &Controller{
		state:   Off,
		changed: make(chan struct{}, 1),
		idle:    make(chan struct{}, 1),
		stop:    make(chan struct{}),
		done:    make(chan struct{}),
		backend: b,
	}
}

// Start launches the background pattern goroutine.
func (c *Controller) Start() {
	c.mu.Lock()
	if c.running {
		c.mu.Unlock()
		return
	}
	c.running = true
	c.stop = make(chan struct{})
	c.done = make(chan struct{})
	c.mu.Unlock()

	c.backend.DisableTrigger()
	go c.run()
}

// Stop halts the background goroutine, turns the LED off, and restores the trigger.
func (c *Controller) Stop() {
	c.mu.Lock()
	if !c.running {
		c.mu.Unlock()
		return
	}
	c.running = false
	c.mu.Unlock()

	close(c.stop)
	<-c.done

	c.backend.Set(false)
	c.backend.RestoreTrigger()
}

// SetState changes the LED state and interrupts any running pattern.
func (c *Controller) SetState(s State) {
	c.mu.Lock()
	c.state = s
	// Non-blocking send to signal state change.
	select {
	case c.changed <- struct{}{}:
	default:
	}
	c.mu.Unlock()
}

// GetState returns the current state (thread-safe).
func (c *Controller) GetState() State {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.state
}

// WaitUntilIdle blocks until a non-repeating pattern completes or timeout elapses.
// Returns true if idle before timeout, false on timeout.
func (c *Controller) WaitUntilIdle(timeout time.Duration) bool {
	select {
	case <-c.idle:
		return true
	case <-time.After(timeout):
		return false
	}
}

// run is the background goroutine that executes patterns.
func (c *Controller) run() {
	defer close(c.done)

	for {
		c.mu.Lock()
		st := c.state
		c.mu.Unlock()

		pat := patterns[st]

		if len(pat.steps) == 0 {
			if pat.repeat {
				// Ready: solid on, wait for state change or stop.
				c.backend.Set(true)
			} else {
				// Off: LED off, wait for state change or stop.
				c.backend.Set(false)
			}
			if c.waitForChangeOrStop() {
				return
			}
			continue
		}

		interrupted := false
		for _, s := range pat.steps {
			c.backend.Set(true)
			if c.interruptibleSleep(time.Duration(s.onMs) * time.Millisecond) {
				return
			}
			if c.stateChanged() {
				interrupted = true
				break
			}

			c.backend.Set(false)
			if c.interruptibleSleep(time.Duration(s.offMs) * time.Millisecond) {
				return
			}
			if c.stateChanged() {
				interrupted = true
				break
			}
		}

		if interrupted {
			continue
		}

		if pat.repeat {
			continue
		}

		// Non-repeating pattern finished: signal idle, turn off, wait.
		c.backend.Set(false)
		select {
		case c.idle <- struct{}{}:
		default:
		}
		if c.waitForChangeOrStop() {
			return
		}
	}
}

// interruptibleSleep waits for duration, but returns early if stop or changed signals.
// Returns true if stop was signaled (goroutine should exit).
func (c *Controller) interruptibleSleep(d time.Duration) bool {
	select {
	case <-c.stop:
		return true
	case <-c.changed:
		// Put it back so the main loop sees it.
		select {
		case c.changed <- struct{}{}:
		default:
		}
		return false
	case <-time.After(d):
		return false
	}
}

// stateChanged checks if a state change is pending (non-blocking).
func (c *Controller) stateChanged() bool {
	select {
	case <-c.changed:
		return true
	default:
		return false
	}
}

// waitForChangeOrStop blocks until a state change or stop signal.
// Returns true if stop was signaled.
func (c *Controller) waitForChangeOrStop() bool {
	select {
	case <-c.stop:
		return true
	case <-c.changed:
		return false
	}
}

// --- sysfs backend ---

const (
	sysfsLEDBrightness = "/sys/class/leds/led0/brightness"
	sysfsLEDTrigger    = "/sys/class/leds/led0/trigger"
)

type sysfsBackend struct{}

func (b *sysfsBackend) Set(on bool) {
	val := "0"
	if on {
		val = "1"
	}
	_ = os.WriteFile(sysfsLEDBrightness, []byte(val), 0644)
}

func (b *sysfsBackend) DisableTrigger() {
	_ = os.WriteFile(sysfsLEDTrigger, []byte("none"), 0644)
}

func (b *sysfsBackend) RestoreTrigger() {
	_ = os.WriteFile(sysfsLEDTrigger, []byte("mmc0"), 0644)
}

// --- gpio backend (placeholder) ---

type gpioBackend struct {
	pin int
}

func (b *gpioBackend) Set(on bool) {
	log.Printf("gpio: pin %d set %v (placeholder)", b.pin, on)
}

func (b *gpioBackend) DisableTrigger() {
	log.Printf("gpio: disable trigger (placeholder)")
}

func (b *gpioBackend) RestoreTrigger() {
	log.Printf("gpio: restore trigger (placeholder)")
}
