package router

import (
	"container/list"
	"sync"
)

// dedupWindowCap is the per-conversation sliding window size (漏洞6: last 100
// stanza_id -> {seq, timestamp}; oldest evicted). Cap keeps memory bounded
// while covering realistic client-retry horizons.
const dedupWindowCap = 100

// dedupEntry is one remembered stanza mapping.
type dedupEntry struct {
	stanza string
	seq    int64
	ts     int64
}

// convWindow is the ordered last-N memory of one conversation.
// Front of ll = most recently stored; back = eviction candidate.
type convWindow struct {
	mu    sync.Mutex
	ll    *list.List // []*dedupEntry, front = newest
	items map[string]*list.Element
}

// DedupWindows is the per-conversation sliding dedup store (漏洞6 铁律载体):
// a replayed stanza_id must replay its ACK WITHOUT re-persist and WITHOUT
// re-push. Safe for concurrent use.
type DedupWindows struct {
	mu   sync.RWMutex
	covs map[string]*convWindow
}

// NewDedupWindows creates an empty dedup store.
func NewDedupWindows() *DedupWindows {
	return &DedupWindows{covs: make(map[string]*convWindow)}
}

func (d *DedupWindows) windowFor(covID string) *convWindow {
	d.mu.RLock()
	w, ok := d.covs[covID]
	d.mu.RUnlock()
	if ok {
		return w
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if w, ok := d.covs[covID]; ok {
		return w
	}
	w = &convWindow{ll: list.New(), items: make(map[string]*list.Element)}
	d.covs[covID] = w
	return w
}

// Lookup returns the remembered (seq, ts) for (covID, stanza), ok=false on miss.
func (d *DedupWindows) Lookup(covID, stanza string) (seq, ts int64, ok bool) {
	if stanza == "" {
		return 0, 0, false
	}
	d.mu.RLock()
	w, found := d.covs[covID]
	d.mu.RUnlock()
	if !found {
		return 0, 0, false
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if el, hit := w.items[stanza]; hit {
		e := el.Value.(*dedupEntry)
		return e.seq, e.ts, true
	}
	return 0, 0, false
}

// Store remembers (stanza -> seq, ts) for covID, evicting the oldest entry
// when the window exceeds dedupWindowCap. Re-storing an existing stanza
// refreshes its values and recency. Empty stanzas are not keyable and are ignored.
func (d *DedupWindows) Store(covID, stanza string, seq, ts int64) {
	if stanza == "" {
		return
	}
	w := d.windowFor(covID)
	w.mu.Lock()
	defer w.mu.Unlock()
	if el, hit := w.items[stanza]; hit {
		e := el.Value.(*dedupEntry)
		e.seq, e.ts = seq, ts
		w.ll.MoveToFront(el)
		return
	}
	el := w.ll.PushFront(&dedupEntry{stanza: stanza, seq: seq, ts: ts})
	w.items[stanza] = el
	for w.ll.Len() > dedupWindowCap {
		back := w.ll.Back()
		if back == nil {
			break
		}
		w.ll.Remove(back)
		delete(w.items, back.Value.(*dedupEntry).stanza)
	}
}
