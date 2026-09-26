package transport

import (
	"fmt"
	"testing"
	"time"
)

func TestAuthFailureLimiterUsesFixedWindow(t *testing.T) {
	limiter := newAuthFailureLimiter()
	now := time.Date(2026, 9, 26, 8, 0, 0, 0, time.UTC)

	for attempt := 1; attempt <= authFailureLimit; attempt++ {
		if !limiter.tryConsume("192.0.2.1", now) {
			t.Fatalf("attempt %d unexpectedly consumed no budget", attempt)
		}
	}
	if limiter.tryConsume("192.0.2.1", now.Add(authFailureWindow-time.Second)) {
		t.Fatal("failure inside the window unexpectedly consumed budget")
	}
	if !limiter.tryConsume("192.0.2.1", now.Add(authFailureWindow)) {
		t.Fatal("new window did not reset the failure budget")
	}
	limiter.clear("192.0.2.1")
	if !limiter.tryConsume("192.0.2.1", now.Add(2*authFailureWindow)) {
		t.Fatal("successful authentication did not clear failure history")
	}
}

func TestAuthFailureLimiterBoundsSourceBookkeeping(t *testing.T) {
	limiter := newAuthFailureLimiter()
	now := time.Date(2026, 9, 26, 8, 0, 0, 0, time.UTC)
	for index := 0; index < maxAuthFailureSources; index++ {
		source := fmt.Sprintf("192.0.2.%d", index+1)
		if !limiter.tryConsume(source, now) {
			t.Fatalf("source %d unexpectedly consumed no budget", index+1)
		}
	}
	if !limiter.tryConsume("198.51.100.1", now) {
		t.Fatal("bounded eviction unexpectedly denied a new source")
	}
	limiter.mu.Lock()
	size := len(limiter.entries)
	limiter.mu.Unlock()
	if size != maxAuthFailureSources {
		t.Fatalf("tracked sources = %d, want %d", size, maxAuthFailureSources)
	}
}

func TestRequestSourceUsesHostOnly(t *testing.T) {
	for _, test := range []struct {
		remoteAddr string
		want       string
	}{
		{remoteAddr: "192.0.2.1:12345", want: "192.0.2.1"},
		{remoteAddr: "[2001:db8::1]:12345", want: "2001:db8::1"},
		{remoteAddr: "unix", want: "unix"},
		{remoteAddr: "", want: "unknown"},
	} {
		if got := requestSource(test.remoteAddr); got != test.want {
			t.Fatalf("requestSource(%q) = %q, want %q", test.remoteAddr, got, test.want)
		}
	}
}
