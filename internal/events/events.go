// Package events keeps wisp's operator-facing issue log and per-product
// ingest status in memory, for the dashboard: what went wrong recently
// (rejected batches, failed day closes, registry problems), and when each
// product last delivered data.
//
// Callers must never pass request data — no IPs, tokens, visitor keys,
// page keys or hosts. Messages are fixed descriptions; the product key
// (a public identifier) is the only variable part besides the day.
//
// In memory only: the log starts empty at every restart (Started says
// when). It is a monitoring aid, not an audit trail.
package events

import (
	"sort"
	"sync"
	"time"
)

// Levels, most severe first.
const (
	Error   = "error"
	Warning = "warning"
)

// MaxEntries bounds memory: past it, the least recently seen entry is
// evicted.
const MaxEntries = 200

// Entry is one distinct issue, deduplicated by (level, source, product,
// message), with how often and when it occurred.
type Entry struct {
	Level   string
	Source  string // "ingest", "dayclose", "registry", "self-report", "dashboard"
	Product string // "" when unknown or not product-specific
	Message string
	Count   int
	First   time.Time
	Last    time.Time
}

// Delivery is one product's ingest status since Started.
type Delivery struct {
	Product string
	Last    time.Time
	Batches int
}

// Log is safe for concurrent use. A nil *Log discards everything.
type Log struct {
	mu        sync.Mutex
	now       func() time.Time
	started   time.Time
	entries   map[[4]string]*Entry
	delivered map[string]*Delivery
}

// New returns an empty log; now defaults to time.Now.
func New(now func() time.Time) *Log {
	if now == nil {
		now = time.Now
	}
	return &Log{now: now, started: now().UTC(), entries: map[[4]string]*Entry{}, delivered: map[string]*Delivery{}}
}

// Record notes one occurrence of an issue.
func (l *Log) Record(level, source, product, message string) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now().UTC()
	k := [4]string{level, source, product, message}
	if e := l.entries[k]; e != nil {
		e.Count++
		e.Last = now
		return
	}
	if len(l.entries) >= MaxEntries {
		var oldest [4]string
		var at time.Time
		for key, e := range l.entries {
			if at.IsZero() || e.Last.Before(at) {
				oldest, at = key, e.Last
			}
		}
		delete(l.entries, oldest)
	}
	l.entries[k] = &Entry{Level: level, Source: source, Product: product, Message: message, Count: 1, First: now, Last: now}
}

// Delivered notes an accepted batch from product.
func (l *Log) Delivered(product string) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	d := l.delivered[product]
	if d == nil {
		d = &Delivery{Product: product}
		l.delivered[product] = d
	}
	d.Batches++
	d.Last = l.now().UTC()
}

// Started is when this log (and so the process) started.
func (l *Log) Started() time.Time {
	if l == nil {
		return time.Time{}
	}
	return l.started
}

// Entries returns the issues, errors first, then most recently seen.
func (l *Log) Entries() []Entry {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]Entry, 0, len(l.entries))
	for _, e := range l.entries {
		out = append(out, *e)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Level != out[j].Level {
			return out[i].Level == Error
		}
		return out[i].Last.After(out[j].Last)
	})
	return out
}

// Deliveries returns ingest status for every product that delivered.
func (l *Log) Deliveries() map[string]Delivery {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make(map[string]Delivery, len(l.delivered))
	for k, d := range l.delivered {
		out[k] = *d
	}
	return out
}

// Counts returns how many distinct error and warning entries were seen
// at or after since.
func (l *Log) Counts(since time.Time) (errors, warnings int) {
	for _, e := range l.Entries() {
		if e.Last.Before(since) {
			continue
		}
		if e.Level == Error {
			errors++
		} else {
			warnings++
		}
	}
	return errors, warnings
}
