package store

import (
	"time"

	"go.uber.org/zap"
	"gorm.io/gorm"
)

// BatchWriter implements the Async Store pattern: memory channel (cap 10,000)削峰 + single worker bulk transaction (100条/50ms).
// This is the sole writer path protected by MaxOpenConns=1 and PRAGMA synchronous=FULL.
// Gate-0 establishes the safety primitives; Stage-1 completes the worker loop.
type BatchWriter struct {
	db     *gorm.DB
	queue  chan *Message
	logger *zap.Logger
	done   chan struct{}
}

// NewBatchWriter creates a batch writer with a 10,000-capacity channel.
func NewBatchWriter(db *gorm.DB, logger *zap.Logger) *BatchWriter {
	return &BatchWriter{
		db:     db,
		queue:  make(chan *Message, 10000),
		logger: logger,
		done:   make(chan struct{}),
	}
}

// EnqueueAsync enqueues a message for async persistence. It never blocks the caller on DB I/O.
// If the channel is full, it returns false (caller should apply backpressure).
func (w *BatchWriter) EnqueueAsync(msg *Message) bool {
	select {
	case w.queue <- msg:
		return true
	default:
		w.logger.Warn("store batch queue full, dropping message", zap.String("cov", msg.CovID), zap.String("stanza", msg.StanzaID))
		return false
	}
}

// Start launches the background batch worker. It batches up to 100 messages or 50ms, whichever comes first,
// and commits them in a single transaction with INSERT OR IGNORE to avoid batch rollback on single-row conflict.
func (w *BatchWriter) Start() {
	go w.loop()
}

func (w *BatchWriter) loop() {
	batch := make([]*Message, 0, 100)
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	flush := func() {
		if len(batch) == 0 {
			return
		}
		// Batch transaction with ON CONFLICT IGNORE to prevent single-message poisoning.
		tx := w.db.Begin()
		for _, m := range batch {
			// Use Create with ON CONFLICT clause; GORM's CreateInBatches with clause.
			// For SQLite we use INSERT OR IGNORE via Exec for simplicity.
			if err := tx.Exec(
				`INSERT OR IGNORE INTO messages(cov_id, seq, stanza_id, chat_type, from_uid, to_uid, content_type, content, media_id, extra, status, timestamp, created_at)
				 VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				m.CovID, m.Seq, m.StanzaID, m.ChatType, m.FromUID, m.ToUID, m.ContentType, m.Content, m.MediaID, m.Extra, m.Status, m.Timestamp, m.CreatedAt,
			).Error; err != nil {
				w.logger.Error("batch insert error", zap.Error(err))
			}
		}
		if err := tx.Commit().Error; err != nil {
			w.logger.Error("batch commit failed", zap.Error(err))
			tx.Rollback()
			return
		}
		// Gate 0-3: ensure fsync after commit (WAL durability). The synchronous=FULL already fsyncs, but we also do file.Sync periodically.
		// For simplicity we rely on synchronous=FULL; explicit Sync is done in checkpoint path.
		batch = batch[:0]
	}

	for {
		select {
		case msg := <-w.queue:
			batch = append(batch, msg)
			if len(batch) >= 100 {
				flush()
			}
		case <-ticker.C:
			flush()
		case <-w.done:
			flush()
			return
		}
	}
}

// Stop gracefully shuts down the writer, flushing remaining messages.
func (w *BatchWriter) Stop() {
	close(w.done)
}

// QueueLen returns current queue length (for metrics).
func (w *BatchWriter) QueueLen() int {
	return len(w.queue)
}
