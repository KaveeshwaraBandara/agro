package llm

import (
	"testing"
	"time"
)

// fakeClock advances only when sleep is called, so throttle tests are instant.
type fakeClock struct {
	t     time.Time
	slept []time.Duration
}

func (f *fakeClock) now() time.Time { return f.t }
func (f *fakeClock) sleep(d time.Duration) {
	f.slept = append(f.slept, d)
	f.t = f.t.Add(d)
}

// Two back-to-back requests must be spaced at least MinInterval apart.
func TestThrottleSpacesConsecutiveRequests(t *testing.T) {
	clk := &fakeClock{t: time.Unix(0, 0)}
	c := &Client{
		MinInterval: 13 * time.Second,
		now:         clk.now,
		sleep:       clk.sleep,
	}

	c.throttle() // first request: nothing prior, no wait
	first := c.lastReq
	c.throttle() // second request immediately after: must wait the interval
	second := c.lastReq

	if spacing := second.Sub(first); spacing < c.MinInterval {
		t.Fatalf("requests spaced %s apart, want >= %s", spacing, c.MinInterval)
	}
	if len(clk.slept) != 1 {
		t.Fatalf("expected exactly one throttle sleep, got %d: %v", len(clk.slept), clk.slept)
	}
	if clk.slept[0] < 13*time.Second {
		t.Fatalf("expected a sleep of >= 13s, got %s", clk.slept[0])
	}
}

// With MinInterval == 0 (default), throttle is a no-op — no sleeping.
func TestThrottleDisabledByDefault(t *testing.T) {
	clk := &fakeClock{t: time.Unix(0, 0)}
	c := &Client{now: clk.now, sleep: clk.sleep} // MinInterval defaults to 0

	c.throttle()
	c.throttle()

	if len(clk.slept) != 0 {
		t.Fatalf("throttle must be a no-op when MinInterval==0, but slept %v", clk.slept)
	}
}

// If the interval has already elapsed, the next request does not wait.
func TestThrottleNoWaitWhenIntervalElapsed(t *testing.T) {
	clk := &fakeClock{t: time.Unix(0, 0)}
	c := &Client{MinInterval: 13 * time.Second, now: clk.now, sleep: clk.sleep}

	c.throttle()                  // records lastReq at T0
	clk.t = clk.t.Add(20 * time.Second) // 20s passes externally (> interval)
	c.throttle()                  // should not sleep

	if len(clk.slept) != 0 {
		t.Fatalf("expected no sleep when interval already elapsed, got %v", clk.slept)
	}
}

func TestParseInterval(t *testing.T) {
	if d, err := parseInterval(""); err != nil || d != 0 {
		t.Fatalf(`parseInterval("") = %v, %v; want 0, nil`, d, err)
	}
	if d, err := parseInterval("13s"); err != nil || d != 13*time.Second {
		t.Fatalf(`parseInterval("13s") = %v, %v; want 13s, nil`, d, err)
	}
	if _, err := parseInterval("not-a-duration"); err == nil {
		t.Fatal("expected an error for an invalid duration")
	}
	if _, err := parseInterval("-5s"); err == nil {
		t.Fatal("expected an error for a negative duration")
	}
}
