package dashboard

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/lnd3/wisp/hook"
	"github.com/lnd3/wisp/internal/events"
	"github.com/lnd3/wisp/internal/staging"
	"github.com/lnd3/wisp/internal/stats"
)

const visitorKey = "SECRETKEYxxxxxxxxxxxxx"

func liveServer(t *testing.T, now time.Time, ev *events.Log, merge func(*staging.Store)) *httptest.Server {
	t.Helper()
	dir := t.TempDir()
	db, err := stats.Open(filepath.Join(dir, "stats.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	store, err := staging.Open(filepath.Join(dir, "staging"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	if merge != nil {
		merge(store)
	}
	mux := http.NewServeMux()
	(&Handler{
		Stats: db, Today: store, Events: ev,
		Registered: func() []string { return []string{"cinderapps", "offgridapp"} },
		Now:        func() time.Time { return now },
	}).Register(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func mergeBatch(t *testing.T, s *staging.Store, product, day string, v hook.Visitor) {
	t.Helper()
	if _, err := s.Merge(context.Background(), product, &hook.Batch{Product: product, Day: day, BatchID: product + day + v.K, Visitors: []hook.Visitor{v}}); err != nil {
		t.Fatal(err)
	}
}

func TestTodayTab(t *testing.T) {
	now := time.Date(2026, 10, 1, 14, 0, 0, 0, time.UTC)
	ev := events.New(func() time.Time { return now.Add(-10 * time.Minute) })
	ev.Delivered("cinderapps")
	srv := liveServer(t, now, ev, func(s *staging.Store) {
		ff := hook.Agent{Browser: "firefox", OS: "linux", Device: "desktop"}
		mergeBatch(t, s, "cinderapps", "2026-10-01", hook.Visitor{K: visitorKey, Visits: 1, Views: map[string]int{"/": 2, "/cindertunnel": 1}, Referrers: map[string]int{"lobste.rs": 1}, Agent: ff})
		mergeBatch(t, s, "cinderapps", "2026-10-01", hook.Visitor{K: "OTHERKEYyyyyyyyyyyyyyy", Visits: 1, Views: map[string]int{"/": 1}, Agent: ff})
		mergeBatch(t, s, "wisp", "2026-10-01", hook.Visitor{K: "WISPKEYzzzzzzzzzzzzzzz", Visits: 1, Views: map[string]int{"/": 1}, Agent: ff})
		mergeBatch(t, s, "cinderapps", "2026-09-30", hook.Visitor{K: "YESTERDAYwwwwwwwwwwwww", Visits: 1, Views: map[string]int{"/": 1}, Agent: ff})
	})

	_, _, body := get(t, srv, "/dashboard/?view=today&product=cinderapps")
	for _, want := range []string{"Last day so far", "2026-10-01 (UTC), so far", "Visitors today", "/cindertunnel", "lobste.rs",
		"2026-09-30 is still open", "Ingest status", "cinderapps", "offgridapp", "none since start", "10 min ago"} {
		if !strings.Contains(body, want) {
			t.Errorf("today tab lacks %q", want)
		}
	}
	// 2 visitors on cinderapps today (the wisp product and yesterday excluded).
	if !strings.Contains(body, `<div class="tile-value">2</div>`) {
		t.Error("cinderapps today should show 2 visitors")
	}
	// All products: summed across products, and labelled as such.
	_, _, all := get(t, srv, "/dashboard/?view=today")
	if !strings.Contains(all, `<div class="tile-value">3</div>`) || !strings.Contains(all, "summed across products") {
		t.Error("all-products today should sum to 3 and say it's a sum")
	}
	for _, page := range []string{body, all} {
		for _, k := range []string{visitorKey, "OTHERKEY", "WISPKEY", "YESTERDAY"} {
			if strings.Contains(page, k) {
				t.Fatalf("a visitor key (%s…) reached the dashboard", k)
			}
		}
		if regexp.MustCompile(`(?i)unique visitors`).MatchString(page) {
			t.Error(`today tab says "unique visitors"`)
		}
	}
	// A product registered but not yet reporting is selectable.
	if st, _, _ := get(t, srv, "/dashboard/?view=today&product=offgridapp"); st != 200 {
		t.Errorf("registered product without data: %d", st)
	}
}

func TestTodayEmpty(t *testing.T) {
	srv := liveServer(t, time.Date(2026, 10, 1, 14, 0, 0, 0, time.UTC), events.New(nil), nil)
	_, _, body := get(t, srv, "/dashboard/?view=today")
	if !strings.Contains(body, "Nothing received yet today") || !strings.Contains(body, "Ingest status") {
		t.Error("empty today tab should explain itself and still show ingest status")
	}
}

func TestIssuesTabAndBanner(t *testing.T) {
	now := time.Date(2026, 10, 1, 14, 0, 0, 0, time.UTC)
	ev := events.New(func() time.Time { return now })
	ev.Record(events.Warning, "ingest", "", "rejected: missing or unknown token (401)")
	ev.Record(events.Warning, "ingest", "", "rejected: missing or unknown token (401)")
	ev.Record(events.Error, "dayclose", "cinderapps", "2026-09-30 deleted UNAGGREGATED after repeated close failures: that day's stats are lost")
	srv := liveServer(t, now, ev, nil)

	_, _, issues := get(t, srv, "/dashboard/?view=issues")
	for _, want := range []string{"Issues (1 error, 1 warning)", "UNAGGREGATED", "unknown token (401)", `<td data-label="Count">2</td>`, "status-error", "Error", "Warning"} {
		if !strings.Contains(issues, want) {
			t.Errorf("issues tab lacks %q", want)
		}
	}
	if strings.Index(issues, "UNAGGREGATED") > strings.Index(issues, "unknown token") {
		t.Error("errors must be listed before warnings")
	}
	if strings.Contains(issues, `class="banner"`) {
		t.Error("the issues tab itself needs no banner")
	}
	for _, path := range []string{"/dashboard/", "/dashboard/?view=today"} {
		if _, _, b := get(t, srv, path); !strings.Contains(b, "1 error in the last 24 hours") {
			t.Errorf("%s: missing the error banner", path)
		}
	}

	quiet := liveServer(t, now, events.New(nil), nil)
	if _, _, b := get(t, quiet, "/dashboard/?view=issues"); !strings.Contains(b, "No issues since wisp started") {
		t.Error("empty issues tab")
	}
	if _, _, b := get(t, quiet, "/dashboard/"); strings.Contains(b, `class="banner"`) {
		t.Error("no banner without errors")
	}
}

func TestUnknownViewFallsBackToLive(t *testing.T) {
	srv := liveServer(t, time.Now(), events.New(nil), nil)
	if st, _, b := get(t, srv, "/dashboard/?view=bogus"); st != 200 || !strings.Contains(b, "Nothing received yet today") {
		t.Errorf("got %d", st)
	}
}
