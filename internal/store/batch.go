package store

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"
)

// WriteResult is the per-message persistence outcome.
type WriteResult struct {
	Seq        int64  // authoritative seq (on dedup conflict: the ORIGINAL stored seq)
	Timestamp  int64  // authoritative timestamp ms (on conflict: the ORIGINAL stored one)
	Duplicated bool   // true when UNIQUE(cov_id, stanza_id) hit — replay old ACK, do NOT re-push
	Err        error
}

// ErrQueueFull is reported as WriteResult.Err when the batch queue is full.
var ErrQueueFull = errors.New("store: batch queue full")

const (
	// batchQueueCap is the memory-channel 削峰 capacity (Stage-1 contract: 10,000).
	batchQueueCap = 10000
	// batchMaxSize triggers a flush once a batch reaches 100 messages.
	batchMaxSize = 100
	// batchFlushPeriod is the periodic flush tick (50ms).
	batchFlushPeriod = 50 * time.Millisecond
)

// batchEntry carries a message plus its optional result waiter.
// A nil resultCh means fire-and-forget (EnqueueAsync): no result is delivered.
type batchEntry struct {
	msg      *Message
	resultCh chan WriteResult
}

// BatchWriter implements the Async Store pattern: memory channel (cap 10,000)削峰 + single worker bulk transaction (100条/50ms).
// This is the sole writer path protected by MaxOpenConns=1 and PRAGMA synchronous=FULL.
// Gate-0 establishes the safety primitives; Stage-1 completes the worker loop.
// Gate-0 primitives kept: drain-on-stop, WAL synchronous=FULL, single writer pool, INSERT OR IGNORE.
type BatchWriter struct {
	db      *gorm.DB
	queue   chan batchEntry
	logger  *zap.Logger
	done    chan struct{}
	stopped chan struct{}

	mu       sync.Mutex
	started  bool
	stopOnce sync.Once
}

// NewBatchWriter creates a batch writer with a 10,000-capacity channel.
func NewBatchWriter(db *gorm.DB, logger *zap.Logger) *BatchWriter {
	return &BatchWriter{
		db:      db,
		queue:   make(chan batchEntry, batchQueueCap),
		logger:  logger,
		done:    make(chan struct{}),
		stopped: make(chan struct{}),
	}
}

// EnqueueAsync enqueues a message for async persistence. It never blocks the caller on DB I/O.
// If the channel is full, it returns false (caller should apply backpressure).
func (w *BatchWriter) EnqueueAsync(msg *Message) bool {
	if msg == nil {
		return false
	}
	select {
	case w.queue <- batchEntry{msg: msg}:
		return true
	default:
		w.logger.Warn("store batch queue full, dropping message", zap.String("cov", msg.CovID), zap.String("stanza", msg.StanzaID))
		return false
	}
}

// EnqueueSync enqueues msg and returns a buffered (cap 1) channel that receives exactly one WriteResult.
// Never blocks the caller on DB I/O; if the queue is full it returns a channel pre-loaded with
// WriteResult{Err: ErrQueueFull} immediately.
func (w *BatchWriter) EnqueueSync(msg *Message) <-chan WriteResult {
	ch := make(chan WriteResult, 1)
	if msg == nil {
		ch <- WriteResult{Err: errors.New("store: nil message")}
		return ch
	}
	select {
	case w.queue <- batchEntry{msg: msg, resultCh: ch}:
		return ch
	default:
		ch <- WriteResult{Err: ErrQueueFull}
		return ch
	}
}

// Start launches the background batch worker. It batches up to 100 messages or 50ms, whichever comes first,
// and commits them in a single transaction with INSERT OR IGNORE to avoid batch rollback on single-row conflict.
// Start is idempotent: repeated calls do not launch extra workers.
func (w *BatchWriter) Start() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.started {
		return
	}
	w.started = true
	go w.loop()
}

// deliver sends exactly one WriteResult to a waiter (no-op for fire-and-forget entries).
// The channel is buffered (cap 1), so this never blocks the worker.
func deliver(e batchEntry, res WriteResult) {
	if e.resultCh != nil {
		e.resultCh <- res
	}
}

func (w *BatchWriter) loop() {
	defer close(w.stopped)

	batch := make([]batchEntry, 0, batchMaxSize)
	ticker := time.NewTicker(batchFlushPeriod)
	defer ticker.Stop()

	flush := func() {
		if len(batch) == 0 {
			return
		}
		// Batch transaction with ON CONFLICT IGNORE to prevent single-message poisoning.
		tx := w.db.Begin()
		if tx.Error != nil {
			w.logger.Error("batch begin failed", zap.Error(tx.Error))
			beginErr := fmt.Errorf("store: batch begin: %w", tx.Error)
			for _, e := range batch {
				deliver(e, WriteResult{Err: beginErr})
			}
			// Gate-0 discipline: always clear batch even on failure to avoid
			// infinite retry loop and unbounded memory growth.
			batch = batch[:0]
			return
		}
		// Per-row outcomes are staged here and delivered ONLY after a successful
		// commit, so a commit failure never reports phantom success.
		pending := make([]WriteResult, len(batch))
		for i, e := range batch {
			if e.msg == nil {
				pending[i] = WriteResult{Err: errors.New("store: nil message in batch")}
				continue
			}
			m := e.msg
			// For SQLite we use INSERT OR IGNORE via Exec for simplicity.
			r := tx.Exec(
				`INSERT OR IGNORE INTO messages(cov_id, seq, stanza_id, chat_type, from_uid, to_uid, content_type, content, media_id, extra, status, timestamp, created_at)
				 VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				m.CovID, m.Seq, m.StanzaID, m.ChatType, m.FromUID, m.ToUID, m.ContentType, m.Content, m.MediaID, m.Extra, m.Status, m.Timestamp, m.CreatedAt,
			)
			if r.Error != nil {
				// Row Exec error: record Err for that row, continue with the rest (no batch poisoning).
				w.logger.Error("batch insert error", zap.Error(r.Error), zap.String("cov", m.CovID), zap.String("stanza", m.StanzaID))
				pending[i] = WriteResult{Err: fmt.Errorf("store: batch insert: %w", r.Error)}
				continue
			}
			if r.RowsAffected == 1 {
				pending[i] = WriteResult{Seq: m.Seq, Timestamp: m.Timestamp}
				continue
			}
			// RowsAffected == 0 → UNIQUE(cov_id, stanza_id) conflict → requery
			// inside the SAME tx for the ORIGINAL seq/timestamp (ACK replay).
			// Empty stanza_id messages never participate in dedup: the partial
			// unique index excludes them, so bypass the dedup re-query entirely
			// and report the message's own ACK (Duplicated=false).
			if strings.TrimSpace(m.StanzaID) == "" {
				pending[i] = WriteResult{Seq: m.Seq, Timestamp: m.Timestamp}
				continue
			}
			var row struct {
				Seq       int64
				Timestamp int64
			}
			q := tx.Raw(`SELECT seq, timestamp FROM messages WHERE cov_id=? AND stanza_id=?`, m.CovID, m.StanzaID).Scan(&row)
			if q.Error != nil {
				w.logger.Error("dedup requery error", zap.Error(q.Error), zap.String("cov", m.CovID), zap.String("stanza", m.StanzaID))
				pending[i] = WriteResult{Err: fmt.Errorf("store: dedup requery: %w", q.Error)}
				continue
			}
			if q.RowsAffected == 0 {
				pending[i] = WriteResult{Err: fmt.Errorf("store: dedup conflict but original row not found (cov=%s stanza=%s)", m.CovID, m.StanzaID)}
				continue
			}
			pending[i] = WriteResult{Seq: row.Seq, Timestamp: row.Timestamp, Duplicated: true}
		}
		if err := tx.Commit().Error; err != nil {
			w.logger.Error("batch commit failed", zap.Error(err))
			tx.Rollback()
			// Gate-0 discipline: clear the batch, never infinite-retry.
			commitErr := fmt.Errorf("store: batch commit: %w", err)
			for _, e := range batch {
				deliver(e, WriteResult{Err: commitErr})
			}
			batch = batch[:0]
			return
		}
		// Gate 0-3: ensure fsync after commit (WAL durability). The synchronous=FULL already fsyncs, but we also do file.Sync periodically.
		// For simplicity we rely on synchronous=FULL; explicit Sync is done in checkpoint path.
		for i, e := range batch {
			deliver(e, pending[i])
		}
		batch = batch[:0]
	}

	for {
		select {
		case e := <-w.queue:
			batch = append(batch, e)
			if len(batch) >= batchMaxSize {
				flush()
			} else if len(w.queue) == 0 {
				// Adaptive fast path: the queue has just drained to empty while
				// the batch is non-empty — flush now so a lone message's ACK
				// is not delayed a full 50ms tick.
				flush()
			}
		case <-ticker.C:
			flush()
		case <-w.done:
			// Gate-0 fix: drain the unconsumed queue completely before exiting,
			// otherwise queued messages are dropped without flushing on Stop().
			// Every drained message gets its result delivered by flush().
			for {
				select {
				case e := <-w.queue:
					batch = append(batch, e)
					if len(batch) >= batchMaxSize {
						flush()
					}
				default:
					flush()
					return
				}
			}
		}
	}
}

// Stop gracefully shuts down the writer: it drains the queue completely AND
// delivers a result for every drained message before returning.
// Stop is idempotent and safe to call when the worker was never started.
func (w *BatchWriter) Stop() {
	w.stopOnce.Do(func() { close(w.done) })
	w.mu.Lock()
	started := w.started
	w.mu.Unlock()
	if !started {
		return
	}
	<-w.stopped
}

// QueueLen returns current queue length (for metrics).
func (w *BatchWriter) QueueLen() int {
	return len(w.queue)
}

// GetMessageByStanza fetches the stored message for (covID, stanzaID).
// Used by tests/debug and by callers replaying the authoritative ACK on dedup.
func (w *BatchWriter) GetMessageByStanza(covID, stanzaID string) (*Message, error) {
	var m Message
	if err := w.db.Where("cov_id = ? AND stanza_id = ?", covID, stanzaID).First(&m).Error; err != nil {
		return nil, err
	}
	return &m, nil
}
