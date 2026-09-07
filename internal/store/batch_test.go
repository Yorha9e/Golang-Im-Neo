package store

import (
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"
)

// openTestDB opens an isolated temp-file SQLite database (WAL needs a real file,
// NOT :memory:) ready for BatchWriter tests.
func openTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := NewDB(DBConfig{Path: filepath.Join(t.TempDir(), "test.db")})
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	// Windows locks open SQLite files: close the pool at test end (runs before
	// TempDir removal via LIFO cleanup ordering) so TempDir cleanup succeeds.
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sql.DB: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	return db
}

func newTestMessage(cov, stanza string, seq, ts int64) *Message {
	return &Message{
		CovID:       cov,
		Seq:         seq,
		StanzaID:    stanza,
		ChatType:    "chat",
		FromUID:     "u1",
		ToUID:       "u2",
		ContentType: 1,
		Content:     "hello " + stanza,
		Status:      1,
		Timestamp:   ts,
		CreatedAt:   time.Now(),
	}
}

// waitFor polls cond until true or timeout; fails the test on timeout.
func waitFor(t *testing.T, timeout time.Duration, cond func() bool, what string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for %s", what)
}

func countByCov(t *testing.T, db *gorm.DB, cov string) int64 {
	t.Helper()
	var n int64
	if err := db.Model(&Message{}).Where("cov_id = ?", cov).Count(&n).Error; err != nil {
		t.Fatalf("count messages: %v", err)
	}
	return n
}

func awaitResult(t *testing.T, ch <-chan WriteResult, what string) WriteResult {
	t.Helper()
	select {
	case res := <-ch:
		return res
	case <-time.After(5 * time.Second):
		t.Fatalf("timeout waiting for result: %s", what)
		return WriteResult{}
	}
}

// 1. flush by count: enqueue 100 → committed without waiting on the 50ms tick.
func TestBatchFlushByCount(t *testing.T) {
	db := openTestDB(t)
	w := NewBatchWriter(db, zap.NewNop())
	w.Start()
	defer w.Stop()

	cov := "cov-count"
	for i := 1; i <= 100; i++ {
		if !w.EnqueueAsync(newTestMessage(cov, fmt.Sprintf("s-%03d", i), int64(i), 1700000000000+int64(i))) {
			t.Fatalf("enqueue %d rejected", i)
		}
	}
	waitFor(t, 5*time.Second, func() bool { return countByCov(t, db, cov) == 100 },
		"100 messages committed by count flush")
}

// 2. flush by timer: a single message becomes visible well within ~2s.
func TestBatchFlushByTimer(t *testing.T) {
	db := openTestDB(t)
	w := NewBatchWriter(db, zap.NewNop())
	w.Start()
	defer w.Stop()

	cov := "cov-timer"
	if !w.EnqueueAsync(newTestMessage(cov, "s-timer", 1, 1700000000000)) {
		t.Fatal("enqueue rejected")
	}
	waitFor(t, 2*time.Second, func() bool {
		_, err := w.GetMessageByStanza(cov, "s-timer")
		return err == nil
	}, "single message visible by timer flush")
}

// 3. queue-drain fast path: a lone EnqueueSync resolves well under 50ms.
func TestBatchDrainFastPath(t *testing.T) {
	db := openTestDB(t)
	w := NewBatchWriter(db, zap.NewNop())
	w.Start()
	defer w.Stop()

	// Warm-up so WAL creation / first-commit costs don't pollute the timing.
	warm := awaitResult(t, w.EnqueueSync(newTestMessage("cov-fast", "s-warm", 1, 1700000000000)), "warm-up")
	if warm.Err != nil {
		t.Fatalf("warm-up failed: %v", warm.Err)
	}

	start := time.Now()
	res := awaitResult(t, w.EnqueueSync(newTestMessage("cov-fast", "s-fast", 2, 1700000000001)), "fast-path")
	elapsed := time.Since(start)
	if res.Err != nil {
		t.Fatalf("fast-path result err: %v", res.Err)
	}
	if res.Duplicated {
		t.Fatal("fast-path result unexpectedly duplicated")
	}
	t.Logf("lone EnqueueSync resolved in %v", elapsed)
	if elapsed >= 40*time.Millisecond {
		t.Fatalf("fast-path too slow: %v >= 40ms (lone ACK must not wait the 50ms tick)", elapsed)
	}
}

// 4. EnqueueSync success → same seq/ts, Duplicated=false; row present.
func TestBatchEnqueueSyncSuccess(t *testing.T) {
	db := openTestDB(t)
	w := NewBatchWriter(db, zap.NewNop())
	w.Start()
	defer w.Stop()

	cov, stanza, seq, ts := "cov-sync", "s-sync", int64(7), int64(1700000012345)
	res := awaitResult(t, w.EnqueueSync(newTestMessage(cov, stanza, seq, ts)), "sync write")
	if res.Err != nil {
		t.Fatalf("sync write err: %v", res.Err)
	}
	if res.Duplicated {
		t.Fatal("fresh write reported Duplicated=true")
	}
	if res.Seq != seq || res.Timestamp != ts {
		t.Fatalf("result mismatch: got seq=%d ts=%d, want seq=%d ts=%d", res.Seq, res.Timestamp, seq, ts)
	}
	got, err := w.GetMessageByStanza(cov, stanza)
	if err != nil {
		t.Fatalf("row not present: %v", err)
	}
	if got.Seq != seq || got.Timestamp != ts || got.Content != "hello "+stanza {
		t.Fatalf("stored row mismatch: %+v", got)
	}
}

// 5. dedup recovery: same (cov, stanza) replays the ORIGINAL seq/timestamp.
func TestBatchDedupRecovery(t *testing.T) {
	db := openTestDB(t)
	w := NewBatchWriter(db, zap.NewNop())
	w.Start()
	defer w.Stop()

	cov, stanza := "cov-dedup", "S"
	origSeq, origTs := int64(5), int64(1700000050000)

	first := awaitResult(t, w.EnqueueSync(newTestMessage(cov, stanza, origSeq, origTs)), "first write")
	if first.Err != nil || first.Duplicated || first.Seq != origSeq || first.Timestamp != origTs {
		t.Fatalf("first write unexpected: %+v", first)
	}

	// Retry of the same stanza with a DIFFERENT seq must replay the original ACK.
	retryMsg := newTestMessage(cov, stanza, 9, 1700000099999)
	retryMsg.Content = "retry payload must not create a second row"
	retry := awaitResult(t, w.EnqueueSync(retryMsg), "dedup retry")
	if retry.Err != nil {
		t.Fatalf("dedup retry err: %v", retry.Err)
	}
	if !retry.Duplicated {
		t.Fatal("dedup retry reported Duplicated=false, want true")
	}
	if retry.Seq != origSeq || retry.Timestamp != origTs {
		t.Fatalf("dedup replay mismatch: got seq=%d ts=%d, want seq=%d ts=%d",
			retry.Seq, retry.Timestamp, origSeq, origTs)
	}
	if n := countByCov(t, db, cov); n != 1 {
		t.Fatalf("expected exactly 1 row after dedup, got %d", n)
	}
	stored, err := w.GetMessageByStanza(cov, stanza)
	if err != nil {
		t.Fatalf("GetMessageByStanza: %v", err)
	}
	if stored.Seq != origSeq || stored.Timestamp != origTs {
		t.Fatalf("stored row changed by retry: %+v", stored)
	}
}

// 6. Stop() drains a partially filled queue and delivers every result.
func TestBatchStopDrainsQueue(t *testing.T) {
	db := openTestDB(t)
	w := NewBatchWriter(db, zap.NewNop())
	w.Start()

	const n = 50
	cov := "cov-drain"
	chans := make([]<-chan WriteResult, 0, n)
	for i := 1; i <= n; i++ {
		chans = append(chans, w.EnqueueSync(newTestMessage(cov, fmt.Sprintf("s-%03d", i), int64(i), 1700000000000+int64(i))))
	}
	// Drain-on-stop must deliver a result for every queued message.
	w.Stop()

	for i, ch := range chans {
		select {
		case res := <-ch:
			if res.Err != nil {
				t.Fatalf("message %d result err: %v", i+1, res.Err)
			}
			if res.Duplicated {
				t.Fatalf("message %d unexpectedly duplicated", i+1)
			}
			if res.Seq != int64(i+1) {
				t.Fatalf("message %d seq=%d, want %d", i+1, res.Seq, i+1)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("message %d result never delivered after Stop", i+1)
		}
	}
	if got := countByCov(t, db, cov); got != n {
		t.Fatalf("expected %d rows after Stop drain, got %d", n, got)
	}
}

// 7. full queue (10,000 pending, worker not started) → ErrQueueFull immediately.
func TestBatchQueueFull(t *testing.T) {
	db := openTestDB(t)
	w := NewBatchWriter(db, zap.NewNop())
	// NOTE: worker deliberately NOT started so the queue stays full.

	cov := "cov-full"
	for i := 1; i <= 10000; i++ {
		if !w.EnqueueAsync(newTestMessage(cov, fmt.Sprintf("s-%05d", i), int64(i), 1700000000000+int64(i))) {
			t.Fatalf("enqueue %d rejected before queue filled", i)
		}
	}
	if got := w.QueueLen(); got != 10000 {
		t.Fatalf("queue len=%d, want 10000", got)
	}
	// One more async must be rejected.
	if w.EnqueueAsync(newTestMessage(cov, "s-overflow", 10001, 1700000000000)) {
		t.Fatal("EnqueueAsync accepted message on a full queue")
	}

	start := time.Now()
	ch := w.EnqueueSync(newTestMessage(cov, "s-sync-overflow", 10002, 1700000000000))
	select {
	case res := <-ch:
		if !errors.Is(res.Err, ErrQueueFull) {
			t.Fatalf("expected ErrQueueFull, got %+v", res)
		}
		if time.Since(start) >= time.Second {
			t.Fatal("ErrQueueFull was not returned immediately")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("EnqueueSync on full queue blocked instead of returning ErrQueueFull")
	}
}

// 8. GetMessageByStanza roundtrip (hit + miss).
func TestGetMessageByStanza(t *testing.T) {
	db := openTestDB(t)
	w := NewBatchWriter(db, zap.NewNop())
	w.Start()
	defer w.Stop()

	cov, stanza, seq, ts := "cov-get", "s-get", int64(3), int64(1700000030000)
	res := awaitResult(t, w.EnqueueSync(newTestMessage(cov, stanza, seq, ts)), "roundtrip write")
	if res.Err != nil {
		t.Fatalf("write err: %v", res.Err)
	}
	got, err := w.GetMessageByStanza(cov, stanza)
	if err != nil {
		t.Fatalf("GetMessageByStanza: %v", err)
	}
	if got.CovID != cov || got.StanzaID != stanza || got.Seq != seq || got.Timestamp != ts {
		t.Fatalf("roundtrip mismatch: %+v", got)
	}
	if _, err := w.GetMessageByStanza("cov-missing", "s-missing"); err == nil {
		t.Fatal("expected error for missing (cov, stanza), got nil")
	}
}
