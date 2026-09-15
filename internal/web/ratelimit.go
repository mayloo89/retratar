package web

import (
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"
)

// Rate limiting for the write endpoints that a script can abuse cheaply.
//
// POST /login is the reason this exists: it mints a magic link and hands it to
// the SMTP sender, so an unthrottled caller is an email flood aimed at someone
// else's inbox and a bill on our relay. POST /handle and POST /mood are
// session-gated, but each is still an unbounded database write a logged-in
// script could hammer, so they get the same treatment.
//
// This is a hand-rolled per-IP token bucket, not golang.org/x/time/rate.
// Decision 0003 ("use the standard library, add dependencies reluctantly")
// names rate limiting specifically as something this project writes by hand,
// and the whole mechanism is well under a hundred lines with no state the
// standard library does not already provide. A dependency would carry its own
// weight for no gain here.

const (
	// writeRateBurst is how many requests one client may make back-to-back
	// before its bucket is empty.
	writeRateBurst = 5

	// writeRateRefill is how long one token takes to return, i.e. the
	// steady-state allowance once a burst is spent — one every 20s, three a
	// minute. A person signing in needs one; a script wanting thousands is
	// what this stops.
	writeRateRefill = 20 * time.Second

	// limiterIdleTTL is how long a bucket may sit untouched before it is
	// eligible for eviction. A bucket idle this long has refilled to full, so
	// dropping it and recreating it later is indistinguishable from keeping
	// it.
	limiterIdleTTL = 10 * time.Minute

	// limiterMaxBuckets is the hard ceiling on the per-IP map. It is only
	// reached under an attack far larger than this site's organic traffic;
	// see [rateLimiter.allow] for what happens then.
	limiterMaxBuckets = 1 << 16

	// limiterReapSample is how many buckets [rateLimiter.allow] inspects for
	// expiry on each call. Map iteration order is randomised, so repeated
	// calls sweep the whole map over time without ever holding the lock for a
	// full scan.
	limiterReapSample = 8
)

// tokenBucket is one client's allowance. tokens is a float so that a partial
// refill between two requests is not lost to truncation.
type tokenBucket struct {
	tokens float64
	last   time.Time
}

// rateLimiter is a set of per-key token buckets guarded by one mutex. Keys are
// client IPs; see [clientIP].
type rateLimiter struct {
	refillPerSec float64
	burst        float64
	idleTTL      time.Duration
	maxBuckets   int
	now          func() time.Time

	mu      sync.Mutex
	buckets map[string]*tokenBucket
}

// newRateLimiter builds a limiter that lets a key make burst requests at once
// and then one more every refill interval.
func newRateLimiter(burst float64, refill, idleTTL time.Duration) *rateLimiter {
	return &rateLimiter{
		refillPerSec: 1 / refill.Seconds(),
		burst:        burst,
		idleTTL:      idleTTL,
		maxBuckets:   limiterMaxBuckets,
		now:          time.Now,
		buckets:      make(map[string]*tokenBucket),
	}
}

// allow reports whether the client identified by key may make one more request
// now, spending a token if so.
func (l *rateLimiter) allow(key string) bool {
	now := l.now()

	l.mu.Lock()
	defer l.mu.Unlock()

	l.reap(now)

	b := l.buckets[key]
	if b == nil {
		if len(l.buckets) >= l.maxBuckets {
			// Every bucket in the map is still within its idle TTL: that many
			// distinct active clients at once is an attack, not this site's
			// traffic. Fail closed — a new key is refused until reap frees a
			// slot. Clients that already hold a bucket are unaffected, and the
			// attacker gains nothing by being denied. Allowing here instead
			// would turn "fill the map" into a way past the limiter.
			return false
		}
		l.buckets[key] = &tokenBucket{tokens: l.burst - 1, last: now}
		return true
	}

	b.tokens = min(l.burst, b.tokens+now.Sub(b.last).Seconds()*l.refillPerSec)
	b.last = now

	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// reap deletes up to limiterReapSample expired buckets. It runs inside allow,
// under the lock, but only ever visits a fixed number of entries, so the lock
// is never held for a map-wide scan. A timer-driven full sweep would be that
// scan, and under load its duration is something the attacker controls.
// Sampling keeps eviction O(1) per request, and because its rate rises with
// the request rate it clears expired buckets fastest exactly when new ones
// arrive fastest.
func (l *rateLimiter) reap(now time.Time) {
	n := 0
	for key, b := range l.buckets {
		if now.Sub(b.last) > l.idleTTL {
			delete(l.buckets, key)
		}
		if n++; n >= limiterReapSample {
			return
		}
	}
}

// rateLimit rejects a request with 429 when its client IP is over budget. The
// response body says nothing about the limit: its rate, burst and the caller's
// remaining tokens all stay server-side.
func (l *rateLimiter) rateLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !l.allow(clientIP(r)) {
			http.Error(w, "rate limited", http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// clientIP returns the address a request should be rate-limited by.
//
// In production the app listens on 127.0.0.1 only (ADDR in deploy/retratar.env)
// and its one caller is the Pi's nginx, connecting over loopback. nginx accepts
// connections only from Cloudflare's edge (deploy/nginx/cloudflare-ips.conf.inc),
// and Cloudflare sets CF-Connecting-IP to the real visitor address, replacing
// anything the visitor put there. nginx forwards that header unchanged
// (deploy/nginx/*.conf).
//
// So CF-Connecting-IP is trustworthy, but only one hop back. If the immediate
// peer (r.RemoteAddr) is not loopback, the request did not come through nginx
// and every forwarding header on it is attacker-controlled; the header is then
// ignored and the peer address is the key. Trusting a forwarding header from an
// untrusted peer would let anyone hand themselves a fresh bucket per request by
// varying it — protection that only looks like protection.
//
// IPv6 keys are truncated to a /64, the smallest block routinely assigned to a
// single subscriber, so an attacker holding a /64 (2^64 addresses) still maps
// to one bucket rather than 2^64 of them.
func clientIP(r *http.Request) string {
	peer := r.RemoteAddr
	if host, _, err := net.SplitHostPort(peer); err == nil {
		peer = host
	}

	peerAddr, err := netip.ParseAddr(peer)
	if err != nil {
		// Not an address we can parse (a test harness value, say). Use it
		// verbatim so it still partitions callers rather than merging them.
		return peer
	}

	addr := peerAddr
	if peerAddr.IsLoopback() {
		if cf := strings.TrimSpace(r.Header.Get("CF-Connecting-IP")); cf != "" {
			if forwarded, err := netip.ParseAddr(cf); err == nil {
				addr = forwarded
			}
		}
	}
	return rateLimitKey(addr)
}

// rateLimitKey collapses an address to the block it shares a bucket with: the
// whole address for IPv4, the /64 prefix for IPv6.
func rateLimitKey(addr netip.Addr) string {
	addr = addr.Unmap()
	if addr.Is6() {
		if prefix, err := addr.Prefix(64); err == nil {
			return prefix.String()
		}
	}
	return addr.String()
}
