package session

import (
	"fmt"
	"sync"
	"sync/atomic"

	"gorm.io/gorm"

	"golang-im-neo-system/internal/store"
)

// Allocator implements the in-memory conversation sequence allocator with persistent watermark (Sync-DB-First).
// Gate-0-5: "先落库（写入会话水线水位 +1000），再内存发号" — ensures power-loss never rewinds.
//
// Design (VULNERABILITIES_AND_FIXES.md Gate 1, REQUIREMENTS 3):
// - Each conversation (cov_id) has an independent monotonic seq space.
// - In-memory atomic counter for <1µs allocation.
// - Persistent watermark stored in conversations.current_watermark, stepped by 1000.
// - On Allocate, if next seq exceeds current watermark, the watermark is advanced by 1000 in DB FIRST, then memory is updated.
// - On restart, allocator reloads watermark from DB and starts from watermark+1, never rewinding.
//
// The allocator is safe for concurrent use across goroutines.
type Allocator struct {
	mu   sync.RWMutex
	seqs map[string]*covSeq // cov_id -> seq state
	db   *gorm.DB
}

type covSeq struct {
	current   atomic.Int64 // last allocated seq
	watermark atomic.Int64 // persisted watermark (covers up to watermark)
}

// NewAllocator creates an allocator bound to the given GORM DB handle.
// It does NOT preload all conversations; entries are lazily loaded/created on first Allocate
// and bulk-loaded via LoadAll if needed at startup.
func NewAllocator(db *gorm.DB) *Allocator {
	return &Allocator{
		seqs: make(map[string]*covSeq),
		db:   db,
	}
}

// LoadAll preloads all conversation watermarks from the database into memory.
// Call this at server startup to warm the allocator; it is optional (lazy load also works).
func (a *Allocator) LoadAll() error {
	var convs []store.Conversation
	if err := a.db.Find(&convs).Error; err != nil {
		return fmt.Errorf("load conversations: %w", err)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, c := range convs {
		cs := &covSeq{}
		cs.current.Store(c.CurrentWatermark)
		cs.watermark.Store(c.CurrentWatermark)
		// current is set to watermark (last used watermark). Next Allocate will bump to watermark+1
		// but if watermark is 0 (new conv), first seq will be 1.
		// For existing convs, we need to set current to the last allocated seq.
		// The watermark is the high-water mark reserved; the actual last seq is <= watermark.
		// On startup we set current = watermark - free slack? To avoid gaps, we set current to watermark's base.
		// Actually we track "current" as last allocated; watermark is ceiling. So init current = current watermark's last allocated?
		// Simpler: init both to watermark, but first allocate will check and may not trigger persist.
		// However we want to resume from watermark, so we set current to watermark's last persisted boundary.
		// The real last seq is stored in conversation.LastMsgID or messages MAX(seq) but we use watermark as ceiling.
		// For correctness, we query MAX(seq) for each cov and set current to max(watermark, maxSeq).
		a.seqs[c.CovID] = cs
	}
	// Correct current to actual max(seq) per conversation to avoid reusing unallocated tail after crash.
	// This is safe because Sync-DB-First guarantees watermark >= max(seq) + slack.
	type row struct {
		CovID string
		MaxSeq int64
	}
	var rows []row
	if err := a.db.Model(&store.Message{}).Select("cov_id, MAX(seq) as max_seq").Group("cov_id").Scan(&rows).Error; err != nil {
		return fmt.Errorf("load max seq: %w", err)
	}
	for _, r := range rows {
		if cs, ok := a.seqs[r.CovID]; ok {
			if r.MaxSeq > cs.current.Load() {
				cs.current.Store(r.MaxSeq)
			}
			if r.MaxSeq > cs.watermark.Load() {
				cs.watermark.Store(r.MaxSeq)
			}
		} else {
			cs := &covSeq{}
			cs.current.Store(r.MaxSeq)
			cs.watermark.Store(r.MaxSeq)
			a.seqs[r.CovID] = cs
		}
	}
	return nil
}

// buildCovID constructs the canonical cov_id for a private conversation using colon-separated dictionary order.
// Gate 0-1: cov:<min_uid>:<max_uid> with ':' separator ensures lexicographic correctness.
func buildCovID(uid1, uid2 string) string {
	if uid1 < uid2 {
		return fmt.Sprintf("cov:%s:%s", uid1, uid2)
	}
	return fmt.Sprintf("cov:%s:%s", uid2, uid1)
}

// EnsureConversation ensures the conversation row exists for the given covID.
// If it does not exist, it is created with watermark 0.
func (a *Allocator) EnsureConversation(covID, chatType string) error {
	var cnt int64
	if err := a.db.Model(&store.Conversation{}).Where("cov_id = ?", covID).Count(&cnt).Error; err != nil {
		return err
	}
	if cnt > 0 {
		return nil
	}
	conv := store.Conversation{
		CovID:            covID,
		ChatType:         chatType,
		CurrentWatermark: 0,
	}
	// Use FirstOrCreate to handle race between concurrent Ensure calls.
	return a.db.Where("cov_id = ?", covID).FirstOrCreate(&conv).Error
}

// Allocate returns the next monotonic seq for the given covID using Sync-DB-First.
// Steps:
//  1. Lazily load or create covSeq state.
//  2. Peek next = current+1
//  3. If next > watermark, persist watermark+1000 FIRST (UPDATE ... SET current_watermark = watermark+1000), then update in-memory watermark.
//  4. Increment current and return.
func (a *Allocator) Allocate(covID string) (int64, error) {
	cs := a.getOrCreate(covID)

	for {
		cur := cs.current.Load()
		wm := cs.watermark.Load()
		next := cur + 1

		// Need to extend watermark?
		if next > wm {
			newWM := wm + 1000
			if newWM == 1000 && wm == 0 {
				newWM = 1000
			}
			// Sync-DB-First: persist watermark before exposing seq.
			// Use atomic DB update to handle concurrent extenders: UPDATE ... WHERE current_watermark = wm
			result := a.db.Model(&store.Conversation{}).
				Where("cov_id = ? AND current_watermark = ?", covID, wm).
				Update("current_watermark", newWM)
			if result.Error != nil {
				return 0, fmt.Errorf("persist watermark: %w", result.Error)
			}
			if result.RowsAffected == 0 {
				// Another goroutine extended first, or row does not exist — reload watermark.
				var conv store.Conversation
				if err := a.db.Where("cov_id = ?", covID).First(&conv).Error; err != nil {
					if err == gorm.ErrRecordNotFound {
						// Create missing conversation and retry.
						if err := a.EnsureConversation(covID, "chat"); err != nil {
							return 0, err
						}
						continue
					}
					return 0, err
				}
				cs.watermark.Store(conv.CurrentWatermark)
				continue // retry allocation with new watermark
			}
			// Persist succeeded — update in-memory watermark.
			cs.watermark.Store(newWM)
			// Now allocate the seq.
			if cs.current.CompareAndSwap(cur, next) {
				return next, nil
			}
			continue
		}

		// Watermark suffices — try to CAS current.
		if cs.current.CompareAndSwap(cur, next) {
			return next, nil
		}
		// CAS failed due to concurrent allocate, retry.
	}
}

func (a *Allocator) getOrCreate(covID string) *covSeq {
	a.mu.RLock()
	cs, ok := a.seqs[covID]
	a.mu.RUnlock()
	if ok {
		return cs
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if cs, ok := a.seqs[covID]; ok {
		return cs
	}
	// Try to load from DB.
	var conv store.Conversation
	if err := a.db.Where("cov_id = ?", covID).First(&conv).Error; err == nil {
		cs = &covSeq{}
		cs.watermark.Store(conv.CurrentWatermark)
		// current should be at least max(seq) but we haven't loaded messages max yet.
		// Query max(seq) for this cov to set current correctly.
		// Gate-0 fix: current must never rewind below watermark.
		// Start at max(maxSeq, watermark) to avoid sequence duplication after restart.
		var maxSeq *int64
		a.db.Model(&store.Message{}).Where("cov_id = ?", covID).Select("MAX(seq)").Scan(&maxSeq)
		if maxSeq != nil && *maxSeq > conv.CurrentWatermark {
			cs.current.Store(*maxSeq)
			cs.watermark.Store(*maxSeq)
		} else {
			// Never rewind: clamp current to watermark even when maxSeq is smaller or absent.
			cs.current.Store(conv.CurrentWatermark)
		}
		a.seqs[covID] = cs
		return cs
	}
	// Not in DB — create entry and new seq state.
	// We create the row with watermark 0; first Allocate will extend to 1000.
	_ = a.EnsureConversation(covID, "chat")
	cs = &covSeq{}
	cs.current.Store(0)
	cs.watermark.Store(0)
	a.seqs[covID] = cs
	return cs
}

// Current returns the last allocated seq for the given covID (0 if unknown).
func (a *Allocator) Current(covID string) int64 {
	a.mu.RLock()
	cs, ok := a.seqs[covID]
	a.mu.RUnlock()
	if !ok {
		return 0
	}
	return cs.current.Load()
}

// Watermark returns the persisted watermark for the given covID (0 if unknown).
func (a *Allocator) Watermark(covID string) int64 {
	a.mu.RLock()
	cs, ok := a.seqs[covID]
	a.mu.RUnlock()
	if !ok {
		return 0
	}
	return cs.watermark.Load()
}

// BuildPrivateCovID is the exported helper for constructing cov:uid1:uid2 with colon order.
func BuildPrivateCovID(uid1, uid2 string) string {
	return buildCovID(uid1, uid2)
}
