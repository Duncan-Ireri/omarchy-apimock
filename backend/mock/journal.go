package mock

import (
	"sync"
	"time"
)

// JournalEntry is one served request, as shown in the plugin's live log.
type JournalEntry struct {
	Seq        int64     `json:"seq"`
	Time       time.Time `json:"ts"`
	Method     string    `json:"method"`
	URL        string    `json:"url"`
	Status     int       `json:"status"`
	Matched    string    `json:"matched"` // stub label, or "" when unmatched
	RemoteAddr string    `json:"remoteAddr"`
	DurationMs int64     `json:"durationMs"`
}

// Journal keeps the most recent requests (newest last) plus lifetime counters.
// Safe for concurrent use.
type Journal struct {
	mu        sync.Mutex
	buf       []JournalEntry
	cap       int
	seq       int64
	total     int64
	unmatched int64
}

func NewJournal(capacity int) *Journal {
	if capacity < 1 {
		capacity = 200
	}
	return &Journal{buf: make([]JournalEntry, 0, capacity), cap: capacity}
}

func (j *Journal) Record(e JournalEntry) JournalEntry {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.seq++
	j.total++
	e.Seq = j.seq
	if e.Matched == "" {
		j.unmatched++
	}
	j.buf = append(j.buf, e)
	if len(j.buf) > j.cap {
		j.buf = append(j.buf[:0], j.buf[len(j.buf)-j.cap:]...)
	}
	return e
}

// Recent returns up to limit entries, newest first (limit <= 0 means all).
func (j *Journal) Recent(limit int) []JournalEntry {
	j.mu.Lock()
	defer j.mu.Unlock()
	n := len(j.buf)
	if limit <= 0 || limit > n {
		limit = n
	}
	out := make([]JournalEntry, 0, limit)
	for i := 0; i < limit; i++ {
		out = append(out, j.buf[n-1-i])
	}
	return out
}

func (j *Journal) Clear() {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.buf = j.buf[:0]
}

func (j *Journal) Counts() (total, unmatched int64) {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.total, j.unmatched
}
