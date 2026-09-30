package dayclose

import (
	"bytes"
	"context"
	"errors"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/lnd3/wisp/hook"
	"github.com/lnd3/wisp/internal/staging"
	"github.com/lnd3/wisp/internal/stats"
)

const keyA = "Q3xV9aaaaaaaaaaaaaaaaa"

var firefox = hook.Agent{Browser: "firefox", OS: "linux", Device: "desktop"}

type fixture struct {
	dir    string
	store  *staging.Store
	stats  *stats.DB
	closer *Closer
	logs   *bytes.Buffer
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	dir := t.TempDir()
	store, err := staging.Open(filepath.Join(dir, "staging"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	st, err := stats.Open(filepath.Join(dir, "stats.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	logs := &bytes.Buffer{}
	return &fixture{dir: dir, store: store, stats: st, logs: logs,
		closer: &Closer{Staging: store, Stats: st, Log: log.New(logs, "", 0)}}
}

func (f *fixture) merge(t *testing.T, product, day, id string, vs ...hook.Visitor) {
	t.Helper()
	if _, err := f.store.Merge(context.Background(), product, &hook.Batch{Product: product, Day: day, BatchID: id, Visitors: vs}); err != nil {
		t.Fatal(err)
	}
}

func at(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

// TestWorkedExample is plan/designs/D002's worked example, literally: a
// visitor on cindernote on 2026-10-01 arrives from Hacker News at 09:00,
// opens a note at 09:02, returns at 14:30. Two 5-minute flushes; the
// close at 2026-10-02T02:00Z must produce exactly the rows D002 lists,
// and the key must then exist nowhere.
func TestWorkedExample(t *testing.T) {
	f := newFixture(t)
	f.merge(t, "cindernote", "2026-10-01", "flush-0905", hook.Visitor{
		K: keyA, Visits: 1, Views: map[string]int{"/": 1, "/notes/:id": 1},
		Referrers: map[string]int{"news.ycombinator.com": 1}, Agent: firefox,
	})
	f.merge(t, "cindernote", "2026-10-01", "flush-1435", hook.Visitor{
		K: keyA, Visits: 1, Views: map[string]int{"/": 1}, Agent: firefox,
	})

	if r := f.closer.Run(context.Background(), at("2026-10-02T01:59:59Z")); r != (Result{}) {
		t.Fatalf("before the deadline nothing closes; got %+v", r)
	}
	if r := f.closer.Run(context.Background(), at("2026-10-02T02:00:00Z")); r.Closed != 1 {
		t.Fatalf("at the deadline: %+v; logs: %s", r, f.logs)
	}

	got, err := f.stats.ReadDay(context.Background(), "cindernote", "2026-10-01")
	if err != nil {
		t.Fatal(err)
	}
	want := &stats.Day{
		Product: "cindernote", Date: "2026-10-01",
		Uniques: 1, Views: 3, Visits: 2, Bounces: 0, Downloads: 0,
		Pages: []stats.Page{
			{Kind: "view", PageKey: "/", Hits: 2, Uniques: 1},
			{Kind: "view", PageKey: "/notes/:id", Hits: 1, Uniques: 1},
		},
		Referrers: []stats.Referrer{{Host: "news.ycombinator.com", Visits: 1, Uniques: 1}},
		Agents: []stats.Agent{
			{Dim: "browser", Value: "firefox", Uniques: 1},
			{Dim: "device", Value: "desktop", Uniques: 1},
			{Dim: "os", Value: "linux", Uniques: 1},
		},
		Hist: []stats.Bucket{
			{Metric: "pages", Bucket: "2", Visitors: 1},
			{Metric: "views", Bucket: "3", Visitors: 1},
			{Metric: "visits", Bucket: "2", Visitors: 1},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("stats =\n %+v\nwant\n %+v", got, want)
	}

	if days, _ := f.store.Days(); len(days) != 0 {
		t.Errorf("staging after close: %v", days)
	}
	assertKeyNowhere(t, f.dir, keyA)
}

// assertKeyNowhere greps every file under dir for key — the direct form
// of "visitor keys never outlive their day".
func assertKeyNowhere(t *testing.T, dir, key string) {
	t.Helper()
	filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		b, _ := os.ReadFile(p)
		if bytes.Contains(b, []byte(key)) {
			t.Errorf("visitor key found in %s after close", strings.TrimPrefix(p, dir))
		}
		return nil
	})
}

func TestAggregatesAcrossVisitors(t *testing.T) {
	f := newFixture(t)
	chrome := hook.Agent{Browser: "chrome", OS: "android", Device: "mobile"}
	f.merge(t, "cindernote", "2026-10-01", "b1",
		hook.Visitor{K: "AAAAAAAAAAAAAAAAAAAAAA", Visits: 1, Views: map[string]int{"/": 1}, Referrers: map[string]int{"lobste.rs": 1}, Agent: firefox},
		hook.Visitor{K: "BBBBBBBBBBBBBBBBBBBBBB", Visits: 2, Views: map[string]int{"/": 3, "/a": 1, "/b": 1, "/c": 1}, Downloads: map[string]int{"/files/:name": 2}, Referrers: map[string]int{"lobste.rs": 2}, Agent: chrome},
		hook.Visitor{K: "CCCCCCCCCCCCCCCCCCCCCC", Visits: 1, Downloads: map[string]int{"/files/:name": 1}, Agent: chrome},
	)
	f.closer.Run(context.Background(), at("2026-10-03T00:00:00Z"))
	d, err := f.stats.ReadDay(context.Background(), "cindernote", "2026-10-01")
	if err != nil {
		t.Fatal(err)
	}
	if d.Uniques != 3 || d.Visits != 4 || d.Views != 7 || d.Downloads != 3 || d.Bounces != 1 {
		t.Errorf("totals = %+v", d)
	}
	pages := map[string]stats.Page{}
	for _, p := range d.Pages {
		pages[p.Kind+" "+p.PageKey] = p
	}
	if p := pages["view /"]; p.Hits != 4 || p.Uniques != 2 {
		t.Errorf("view / = %+v", p)
	}
	if p := pages["download /files/:name"]; p.Hits != 3 || p.Uniques != 2 {
		t.Errorf("download = %+v", p)
	}
	if len(d.Referrers) != 1 || d.Referrers[0] != (stats.Referrer{Host: "lobste.rs", Visits: 3, Uniques: 2}) {
		t.Errorf("referrers = %+v", d.Referrers)
	}
	hist := map[string]int{}
	for _, b := range d.Hist {
		hist[b.Metric+" "+b.Bucket] = b.Visitors
	}
	// Download-only visitor C has no views: counted in visits, not in the
	// pages/views histograms.
	wantHist := map[string]int{"pages 1": 1, "pages 4-5": 1, "views 1": 1, "views 6-10": 1, "visits 1": 2, "visits 2": 1}
	if !reflect.DeepEqual(hist, wantHist) {
		t.Errorf("hist = %v, want %v", hist, wantHist)
	}
}

func TestSealedDayRefusesLateMerge(t *testing.T) {
	f := newFixture(t)
	v := hook.Visitor{K: keyA, Visits: 1, Views: map[string]int{"/": 1}, Agent: firefox}
	f.merge(t, "cindernote", "2026-10-01", "b1", v)
	f.closer.Run(context.Background(), at("2026-10-02T02:00:00Z"))

	_, err := f.store.Merge(context.Background(), "cindernote", &hook.Batch{Product: "cindernote", Day: "2026-10-01", BatchID: "late", Visitors: []hook.Visitor{v}})
	if !errors.Is(err, staging.ErrClosed) {
		t.Fatalf("late merge: err = %v, want ErrClosed", err)
	}
	if days, _ := f.store.Days(); len(days) != 0 {
		t.Error("a refused late merge must not recreate the staging file")
	}
	f.closer.Run(context.Background(), at("2026-10-02T02:10:00Z"))
	if d, _ := f.stats.ReadDay(context.Background(), "cindernote", "2026-10-01"); d == nil || d.Views != 1 {
		t.Errorf("the day's stats must be unchanged by the late batch: %+v", d)
	}
}

func TestOnlyDueDaysClose(t *testing.T) {
	f := newFixture(t)
	v := hook.Visitor{K: keyA, Visits: 1, Views: map[string]int{"/": 1}, Agent: firefox}
	f.merge(t, "cindernote", "2026-10-01", "a", v)
	f.merge(t, "persona", "2026-10-01", "b", v)
	f.merge(t, "cindernote", "2026-10-02", "c", v)
	if r := f.closer.Run(context.Background(), at("2026-10-02T12:00:00Z")); r.Closed != 2 {
		t.Fatalf("result = %+v", r)
	}
	days, _ := f.store.Days()
	if len(days) != 1 || days[0] != (staging.Day{Product: "cindernote", Date: "2026-10-02"}) {
		t.Errorf("remaining staging = %v", days)
	}
}

func TestCrashBetweenWriteAndDeleteIsIdempotent(t *testing.T) {
	f := newFixture(t)
	f.merge(t, "cindernote", "2026-10-01", "a", hook.Visitor{K: keyA, Visits: 1, Views: map[string]int{"/": 2}, Agent: firefox})
	day := staging.Day{Product: "cindernote", Date: "2026-10-01"}
	// Simulate a crash after the stats commit but before the unlink.
	sum, err := f.store.Summarize(context.Background(), day)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.stats.WriteDay(context.Background(), sum); err != nil {
		t.Fatal(err)
	}
	if r := f.closer.Run(context.Background(), at("2026-10-02T02:00:00Z")); r.Closed != 1 {
		t.Fatalf("the re-run close must succeed: %+v; logs: %s", r, f.logs)
	}
	d, _ := f.stats.ReadDay(context.Background(), "cindernote", "2026-10-01")
	if d == nil || d.Views != 2 || d.Uniques != 1 {
		t.Errorf("a re-run close must rewrite the same rows, not add to them: %+v", d)
	}
}

func TestFailingCloseRetriesThenDiscards(t *testing.T) {
	f := newFixture(t)
	f.merge(t, "cindernote", "2026-10-01", "a", hook.Visitor{K: keyA, Visits: 1, Views: map[string]int{"/": 1}, Agent: firefox})
	f.stats.Close() // every WriteDay now fails

	if r := f.closer.Run(context.Background(), at("2026-10-02T02:30:00Z")); r.Retrying != 1 {
		t.Fatalf("within MaxDelay: %+v", r)
	}
	if days, _ := f.store.Days(); len(days) != 1 {
		t.Fatal("a failed close must keep the staging file for a retry")
	}
	if r := f.closer.Run(context.Background(), at("2026-10-02T03:00:00Z")); r.Discarded != 1 {
		t.Fatalf("past MaxDelay: %+v", r)
	}
	if days, _ := f.store.Days(); len(days) != 0 {
		t.Error("past MaxDelay the staging file must be deleted even unaggregated")
	}
	if !strings.Contains(f.logs.String(), "UNAGGREGATED") {
		t.Error("discarding a day must be logged loudly")
	}
	if strings.Contains(f.logs.String(), keyA) {
		t.Error("logs must never contain a visitor key")
	}
}
