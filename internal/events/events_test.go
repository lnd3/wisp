package events

import (
	"fmt"
	"testing"
	"time"
)

func TestDedupOrderAndCounts(t *testing.T) {
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	l := New(func() time.Time { return now })
	l.Record(Warning, "ingest", "", "unknown token")
	now = now.Add(time.Minute)
	l.Record(Error, "dayclose", "cinderapps", "close failed")
	now = now.Add(time.Minute)
	l.Record(Warning, "ingest", "", "unknown token")

	es := l.Entries()
	if len(es) != 2 {
		t.Fatalf("entries = %d, want 2 (deduplicated)", len(es))
	}
	if es[0].Level != Error {
		t.Error("errors must sort first")
	}
	if w := es[1]; w.Count != 2 || !w.Last.After(w.First) {
		t.Errorf("warning entry = %+v, want count 2 with first < last", w)
	}
	if e, w := l.Counts(now.Add(-90 * time.Second)); e != 1 || w != 1 {
		t.Errorf("Counts = %d, %d", e, w)
	}
	if e, w := l.Counts(now.Add(time.Second)); e != 0 || w != 0 {
		t.Errorf("Counts after everything = %d, %d", e, w)
	}
}

func TestBoundedAndNilSafe(t *testing.T) {
	l := New(nil)
	for i := 0; i < MaxEntries+5; i++ {
		l.Record(Warning, "ingest", fmt.Sprint(i), "x")
	}
	if n := len(l.Entries()); n != MaxEntries {
		t.Errorf("entries = %d, want capped at %d", n, MaxEntries)
	}
	var nl *Log
	nl.Record(Error, "x", "", "y")
	nl.Delivered("p")
	if nl.Entries() != nil || nl.Deliveries() != nil || !nl.Started().IsZero() {
		t.Error("nil log must be a no-op")
	}
}

func TestDeliveries(t *testing.T) {
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	l := New(func() time.Time { return now })
	l.Delivered("cinderapps")
	now = now.Add(5 * time.Minute)
	l.Delivered("cinderapps")
	d := l.Deliveries()["cinderapps"]
	if d.Batches != 2 || !d.Last.Equal(now) {
		t.Errorf("delivery = %+v", d)
	}
}
