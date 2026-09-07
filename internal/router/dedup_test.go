package router

import (
	"fmt"
	"testing"
	"time"
)

// Sliding window keeps the last 100, evicts the oldest, refreshes on re-store.
func TestDedupWindowEvictionAndRefresh(t *testing.T) {
	d := NewDedupWindows()
	cov := "cov:u_a:u_b"

	for i := 1; i <= dedupWindowCap+1; i++ {
		d.Store(cov, fmt.Sprintf("s-%03d", i), int64(i), int64(1000+i))
	}

	if _, _, ok := d.Lookup(cov, "s-001"); ok {
		t.Fatal("oldest stanza should have been evicted after 101 stores")
	}
	for i := 2; i <= dedupWindowCap+1; i++ {
		seq, ts, ok := d.Lookup(cov, fmt.Sprintf("s-%03d", i))
		if !ok {
			t.Fatalf("stanza s-%03d should be retained", i)
		}
		if seq != int64(i) || ts != int64(1000+i) {
			t.Fatalf("stanza s-%03d = (%d,%d), want (%d,%d)", i, seq, ts, i, 1000+i)
		}
	}

	// Re-store refreshes values.
	d.Store(cov, "s-002", 999, 888)
	seq, ts, ok := d.Lookup(cov, "s-002")
	if !ok || seq != 999 || ts != 888 {
		t.Fatalf("re-store should refresh, got (%d,%d,%v)", seq, ts, ok)
	}

	// Windows are per-conversation isolated.
	if _, _, ok := d.Lookup("cov:other", "s-002"); ok {
		t.Fatal("lookup must not leak across conversations")
	}

	// Empty stanza is never keyable.
	d.Store(cov, "", 1, 1)
	if _, _, ok := d.Lookup(cov, ""); ok {
		t.Fatal("empty stanza must never hit")
	}
	if _, _, ok := d.Lookup("cov:missing", "s-x"); ok {
		t.Fatal("unknown conversation must miss")
	}
}

// FriendCache: LRU cap, TTL expiry, invalidate.
func TestFriendCacheLRUAndTTL(t *testing.T) {
	c := newFriendCache(2, time.Minute)
	c.add("k1", true)
	c.add("k2", false)

	if v, ok := c.get("k2"); !ok || v {
		t.Fatal("k2 should hit as denied")
	}
	if v, ok := c.get("k1"); !ok || !v {
		t.Fatal("k1 should hit as allowed")
	}
	// k1 is now most-recent; adding k3 must evict the LRU entry k2.
	c.add("k3", true)
	if _, ok := c.get("k2"); ok {
		t.Fatal("k2 should have been evicted by LRU cap")
	}
	if _, ok := c.get("k1"); !ok {
		t.Fatal("k1 was recently used and must survive")
	}
	if _, ok := c.get("k3"); !ok {
		t.Fatal("k3 must be present")
	}

	c.invalidate("k1")
	if _, ok := c.get("k1"); ok {
		t.Fatal("invalidated key must miss")
	}
}

func TestFriendCacheExpiry(t *testing.T) {
	c := newFriendCache(16, 30*time.Millisecond)
	c.add("k", true)
	if _, ok := c.get("k"); !ok {
		t.Fatal("fresh entry must hit")
	}
	time.Sleep(60 * time.Millisecond)
	if _, ok := c.get("k"); ok {
		t.Fatal("expired entry must miss")
	}
}

// Rate limiter: burst then throttle, refill over time.
func TestRateLimiterBurstAndRefill(t *testing.T) {
	l := newRateLimiter()
	now := time.Now()

	allowed := 0
	for i := 0; i < 25; i++ {
		if l.allowAt("u", now) {
			allowed++
		}
	}
	if allowed != 20 {
		t.Fatalf("burst should allow exactly 20, got %d", allowed)
	}

	// 1.5s of refill => ~15 tokens back.
	now = now.Add(1500 * time.Millisecond)
	allowed = 0
	for i := 0; i < 15; i++ {
		if l.allowAt("u", now) {
			allowed++
		}
	}
	if allowed != 15 {
		t.Fatalf("refill should allow 15 after 1.5s idle, got %d", allowed)
	}

	// Buckets are per-user isolated.
	if !l.allowAt("other-user", now) {
		t.Fatal("fresh user must have a full bucket")
	}
}
