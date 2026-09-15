package web

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// fakeClock is a hand-advanced time source so the refill and eviction tests do
// not have to sleep.
type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time      { return c.t }
func (c *fakeClock) add(d time.Duration) { c.t = c.t.Add(d) }

func newTestLimiter(clock *fakeClock) *rateLimiter {
	l := newRateLimiter(writeRateBurst, writeRateRefill, limiterIdleTTL)
	l.now = clock.now
	return l
}

// TestRateLimiter_BlocksAfterBurst is the core promise: burst requests go
// through, the next one does not.
func TestRateLimiter_BlocksAfterBurst(t *testing.T) {
	l := newTestLimiter(&fakeClock{t: time.Unix(1_700_000_000, 0)})

	for i := range writeRateBurst {
		if !l.allow("k") {
			t.Fatalf("request %d/%d denied, want allowed", i+1, writeRateBurst)
		}
	}
	if l.allow("k") {
		t.Fatalf("request %d denied nothing, want denied after a full burst", writeRateBurst+1)
	}
}

// TestRateLimiter_RefillsOverTime checks that a blocked key recovers exactly
// one token per refill interval and no faster.
func TestRateLimiter_RefillsOverTime(t *testing.T) {
	clock := &fakeClock{t: time.Unix(1_700_000_000, 0)}
	l := newTestLimiter(clock)

	for range writeRateBurst {
		l.allow("k")
	}
	if l.allow("k") {
		t.Fatal("bucket not empty after a full burst")
	}

	// Not quite one interval: still empty.
	clock.add(writeRateRefill - time.Second)
	if l.allow("k") {
		t.Fatal("token granted before a full refill interval elapsed")
	}

	// Crossing the interval: exactly one token back, and only one.
	clock.add(2 * time.Second)
	if !l.allow("k") {
		t.Fatal("no token after a full refill interval")
	}
	if l.allow("k") {
		t.Fatal("second token granted; refill is faster than one per interval")
	}
}

// TestRateLimiter_PerKey proves buckets are independent: one key spending its
// whole burst does not touch another's.
func TestRateLimiter_PerKey(t *testing.T) {
	l := newTestLimiter(&fakeClock{t: time.Unix(1_700_000_000, 0)})

	for range writeRateBurst {
		l.allow("a")
	}
	if l.allow("a") {
		t.Fatal("key a not blocked after its burst")
	}
	if !l.allow("b") {
		t.Fatal("key b blocked by key a's spending; buckets are not per-key")
	}
}

// TestRateLimiter_EvictsIdleBuckets checks that buckets untouched for longer
// than the idle TTL are dropped, so the map does not grow without bound.
func TestRateLimiter_EvictsIdleBuckets(t *testing.T) {
	clock := &fakeClock{t: time.Unix(1_700_000_000, 0)}
	l := newTestLimiter(clock)

	for i := range 100 {
		l.allow(string(rune('A'+i%26)) + string(rune('0'+i/26)))
	}
	if got := len(l.buckets); got == 0 {
		t.Fatal("no buckets recorded")
	}

	// Let every existing bucket age past the TTL, then make enough fresh
	// calls that reap's sampling covers the whole map.
	clock.add(limiterIdleTTL + time.Minute)
	for range 200 {
		l.allow("sweeper")
	}

	if got := len(l.buckets); got > 2 {
		t.Fatalf("map holds %d buckets after every entry idled out, want the sweeper's plus at most a slack of 1", got)
	}
}

// TestRateLimiter_FailsClosedWhenFull checks that once the hard ceiling is hit
// with live buckets, a new key is denied rather than silently allowed — the
// safe direction, since allowing would make "fill the map" a bypass.
func TestRateLimiter_FailsClosedWhenFull(t *testing.T) {
	l := newTestLimiter(&fakeClock{t: time.Unix(1_700_000_000, 0)})
	l.maxBuckets = 4

	for i := range l.maxBuckets {
		if !l.allow(string(rune('a' + i))) {
			t.Fatalf("bucket %d denied while filling to capacity", i)
		}
	}
	if l.allow("overflow") {
		t.Fatal("new key allowed past the hard ceiling, want denied")
	}
	// A key that already has a bucket still works.
	if !l.allow("a") {
		t.Fatal("existing key denied when the map was full; only new keys should be")
	}
}

// TestClientIP_TrustsForwardHeaderOnlyFromLoopback is the spoofing guard. A
// forwarding header is honoured when the peer is the loopback proxy and
// ignored otherwise.
func TestClientIP_TrustsForwardHeaderOnlyFromLoopback(t *testing.T) {
	tests := []struct {
		name       string
		remoteAddr string
		cfHeader   string
		want       string
	}{
		{"loopback peer, header honoured", "127.0.0.1:40000", "203.0.113.9", "203.0.113.9"},
		{"loopback peer, ipv6 header to /64", "127.0.0.1:40000", "2001:db8:abcd:1234::1", "2001:db8:abcd:1234::/64"},
		{"loopback peer, no header, falls back to peer", "127.0.0.1:40000", "", "127.0.0.1"},
		{"untrusted peer, header ignored", "198.51.100.7:1234", "203.0.113.9", "198.51.100.7"},
		{"untrusted peer, no header", "198.51.100.7:1234", "", "198.51.100.7"},
		{"loopback peer, junk header, falls back to peer", "127.0.0.1:40000", "not-an-ip", "127.0.0.1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/login", nil)
			r.RemoteAddr = tt.remoteAddr
			if tt.cfHeader != "" {
				r.Header.Set("CF-Connecting-IP", tt.cfHeader)
			}
			if got := clientIP(r); got != tt.want {
				t.Errorf("clientIP() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestRateLimit_SpoofedHeaderFromUntrustedPeerCannotPartition ties the two
// halves together: a caller that is not the loopback proxy cannot escape its
// bucket by rotating CF-Connecting-IP, because the header is not trusted from
// it.
func TestRateLimit_SpoofedHeaderFromUntrustedPeerCannotPartition(t *testing.T) {
	l := newTestLimiter(&fakeClock{t: time.Unix(1_700_000_000, 0)})
	h := l.rateLimit(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	var lastCode int
	for i := range writeRateBurst + 3 {
		r := httptest.NewRequest(http.MethodPost, "/login", nil)
		r.RemoteAddr = "198.51.100.7:1234"
		r.Header.Set("CF-Connecting-IP", "203.0.113."+string(rune('0'+i))) // rotates every request
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		lastCode = rec.Code
	}
	if lastCode != http.StatusTooManyRequests {
		t.Fatalf("last status = %d, want 429: a rotating spoofed header partitioned the bucket", lastCode)
	}
}
