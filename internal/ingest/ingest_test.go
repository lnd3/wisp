package ingest

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/lnd3/wisp/hook"
	"github.com/lnd3/wisp/internal/registry"
	"github.com/lnd3/wisp/internal/staging"
)

const (
	tokenCinder  = "c1c1c1c1c1c1c1c1c1c1c1c1c1c1c1c1c1c1c1c1c1c1c1c1c1c1c1c1c1c1c1c1"
	tokenPersona = "p2p2p2p2p2p2p2p2p2p2p2p2p2p2p2p2p2p2p2p2p2p2p2p2p2p2p2p2p2p2p2p2"
	keyA         = "AAAAAAAAAAAAAAAAAAAAAA"
)

type fixture struct {
	srv   *httptest.Server
	store *staging.Store
	logs  *bytes.Buffer
}

func newFixture(t *testing.T, now string) *fixture {
	t.Helper()
	reg, err := registry.Parse([]byte(`{"products":[
		{"key":"cindernote","token_sha256":["` + registry.HashToken(tokenCinder) + `"]},
		{"key":"persona","token_sha256":["` + registry.HashToken(tokenPersona) + `"]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	store, err := staging.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	logs := &bytes.Buffer{}
	h := &Handler{
		Registry: func() Registry { return reg },
		Stager:   store,
		Log:      log.New(logs, "", 0),
	}
	if now != "" {
		ts, _ := time.Parse(time.RFC3339, now)
		h.Now = func() time.Time { return ts }
	}
	srv := httptest.NewServer(h.Routes())
	t.Cleanup(srv.Close)
	return &fixture{srv: srv, store: store, logs: logs}
}

func (f *fixture) post(t *testing.T, token string, body any) (int, response) {
	t.Helper()
	var raw []byte
	switch b := body.(type) {
	case string:
		raw = []byte(b)
	default:
		raw, _ = json.Marshal(b)
	}
	req, _ := http.NewRequest(http.MethodPost, f.srv.URL+"/v1/ingest", bytes.NewReader(raw))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var r response
	json.NewDecoder(resp.Body).Decode(&r)
	return resp.StatusCode, r
}

func goodVisitor() hook.Visitor {
	return hook.Visitor{
		K: keyA, Visits: 1,
		Views:     map[string]int{"/": 2, "/notes/:id": 1},
		Referrers: map[string]int{"news.ycombinator.com": 1},
		Agent:     hook.Agent{Browser: "firefox", OS: "linux", Device: "desktop"},
	}
}

func goodBatch() hook.Batch {
	return hook.Batch{Product: "cindernote", Day: "2026-10-01", BatchID: "0192f1c4a1b2", Visitors: []hook.Visitor{goodVisitor()}}
}

func (f *fixture) visits(t *testing.T, product, day string) int {
	t.Helper()
	var n int
	err := f.store.Query(staging.Day{Product: product, Date: day}, func(db *sql.DB) error {
		return db.QueryRow(`SELECT COALESCE(SUM(visits), 0) FROM staging_visitor`).Scan(&n)
	})
	if err != nil {
		return -1
	}
	return n
}

func TestAcceptAndDedup(t *testing.T) {
	f := newFixture(t, "2026-10-01T12:00:00Z")
	b := goodBatch()
	if st, r := f.post(t, tokenCinder, b); st != 200 || r.Status != "ok" || r.Visitors != 1 {
		t.Fatalf("got %d %+v", st, r)
	}
	if st, r := f.post(t, tokenCinder, b); st != 200 || r.Status != "duplicate" {
		t.Fatalf("retry: got %d %+v", st, r)
	}
	if n := f.visits(t, "cindernote", "2026-10-01"); n != 1 {
		t.Errorf("visits = %d, want 1 (retry must not double-count)", n)
	}
}

func TestAuthentication(t *testing.T) {
	f := newFixture(t, "2026-10-01T12:00:00Z")
	b := goodBatch()
	for name, token := range map[string]string{"no token": "", "wrong token": "nope", "hash as token": registry.HashToken(tokenCinder)} {
		if st, r := f.post(t, token, b); st != 401 || r.Error != "unauthorized" {
			t.Errorf("%s: got %d %+v", name, st, r)
		}
	}
	// persona's valid token can't write cindernote's data — same 401.
	if st, _ := f.post(t, tokenPersona, b); st != 401 {
		t.Errorf("cross-product token: got %d, want 401", st)
	}
	if n := f.visits(t, "cindernote", "2026-10-01"); n != -1 {
		t.Error("rejected requests must not create staging data")
	}
	// Unauthenticated requests are refused before the body is read.
	if st, _ := f.post(t, "", "{not json"); st != 401 {
		t.Errorf("unauthenticated bad body: got %d, want 401", st)
	}
}

func TestDayWindow(t *testing.T) {
	f := newFixture(t, "2026-10-02T01:59:00Z") // 2026-10-01's close is 02:00Z
	b := goodBatch()
	if st, _ := f.post(t, tokenCinder, b); st != 200 {
		t.Errorf("within grace: got %d", st)
	}
	b.Day, b.BatchID = "2026-09-30", "x2"
	if st, r := f.post(t, tokenCinder, b); st != 409 || r.Error != "day closed" {
		t.Errorf("closed day: got %d %+v", st, r)
	}
	b.Day, b.BatchID = "2026-10-03", "x3"
	if st, _ := f.post(t, tokenCinder, b); st != 400 {
		t.Errorf("future day: got %d", st)
	}
	f2 := newFixture(t, "2026-10-01T23:55:00Z")
	b.Day, b.BatchID = "2026-10-02", "x4"
	if st, _ := f2.post(t, tokenCinder, b); st != 200 {
		t.Errorf("tomorrow within clock skew: got %d", st)
	}
}

func TestValidation(t *testing.T) {
	f := newFixture(t, "2026-10-01T12:00:00Z")
	many := map[string]int{}
	for i := 0; i <= MaxPagesPerVisitor; i++ {
		many[fmt.Sprintf("/p/%d", i)] = 1
	}
	cases := map[string]func(b *hook.Batch){
		"bad day":           func(b *hook.Batch) { b.Day = "01/10/2026" },
		"bad batch id":      func(b *hook.Batch) { b.BatchID = "has space" },
		"no visitors":       func(b *hook.Batch) { b.Visitors = nil },
		"bad visitor key":   func(b *hook.Batch) { b.Visitors[0].K = "203.0.113.7" },
		"raw path":          func(b *hook.Batch) { b.Visitors[0].Views = map[string]int{"/notes/abc?k=secret": 1} },
		"zero count":        func(b *hook.Batch) { b.Visitors[0].Views = map[string]int{"/": 0} },
		"no hits":           func(b *hook.Batch) { b.Visitors[0].Views = nil },
		"negative visits":   func(b *hook.Batch) { b.Visitors[0].Visits = -1 },
		"referrer with url": func(b *hook.Batch) { b.Visitors[0].Referrers = map[string]int{"evil.example/path?q=x": 1} },
		"agent is full UA":  func(b *hook.Batch) { b.Visitors[0].Agent.Browser = "Mozilla/5.0 (X11)" },
		"too many pages":    func(b *hook.Batch) { b.Visitors[0].Views = many },
		"duplicate key":     func(b *hook.Batch) { b.Visitors = append(b.Visitors, b.Visitors[0]) },
	}
	for name, mutate := range cases {
		b := goodBatch()
		mutate(&b)
		if st, r := f.post(t, tokenCinder, b); st != 400 {
			t.Errorf("%s: got %d %+v, want 400", name, st, r)
		}
	}
	for name, body := range map[string]string{
		"not json":      "{",
		"unknown field": `{"product":"cindernote","day":"2026-10-01","batch_id":"x","visitors":[],"ip":"203.0.113.7"}`,
		"two objects":   `{"product":"cindernote"} {}`,
	} {
		if st, _ := f.post(t, tokenCinder, body); st != 400 {
			t.Errorf("%s: got %d, want 400", name, st)
		}
	}
	if n := f.visits(t, "cindernote", "2026-10-01"); n != -1 {
		t.Error("invalid batches must not create staging data")
	}
	if f.logs.Len() != 0 {
		t.Errorf("client errors must not be logged; got %q", f.logs.String())
	}
}

func TestBodyTooLarge(t *testing.T) {
	f := newFixture(t, "2026-10-01T12:00:00Z")
	big := `{"product":"cindernote","day":"2026-10-01","batch_id":"x","visitors":[{"k":"` + strings.Repeat("A", MaxBodyBytes) + `"}]}`
	if st, _ := f.post(t, tokenCinder, big); st != 413 {
		t.Errorf("got %d, want 413", st)
	}
}

func TestRoutes(t *testing.T) {
	f := newFixture(t, "")
	for _, tc := range []struct {
		method, path string
		want         int
	}{
		{"GET", "/v1/ingest", 405},
		{"GET", "/healthz", 200},
		{"GET", "/", 404},
		{"POST", "/v1/other", 404},
	} {
		req, _ := http.NewRequest(tc.method, f.srv.URL+tc.path, nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != tc.want {
			t.Errorf("%s %s = %d, want %d", tc.method, tc.path, resp.StatusCode, tc.want)
		}
	}
}

// TestHookEndToEnd runs the real hook against the real handler: what a
// product records is what lands in staging, under today's UTC date.
func TestHookEndToEnd(t *testing.T) {
	f := newFixture(t, "")
	w, err := hook.Start(hook.Config{
		Endpoint: f.srv.URL + "/v1/ingest", ProductKey: "cindernote", Token: tokenCinder,
		Interval: time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "http://noteshare.example/notes/7Hq", nil)
	r.RemoteAddr = "203.0.113.7:4000"
	r.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64; rv:131.0) Gecko/20100101 Firefox/131.0")
	r.Header.Set("Referer", "https://news.ycombinator.com/item?id=1")
	w.View(r, "/notes/:id")
	w.View(r, "/notes/:id")
	if err := w.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if s := w.Stats(); s.BatchesSent != 1 {
		t.Fatalf("hook stats = %+v", s)
	}

	day := staging.Day{Product: "cindernote", Date: time.Now().UTC().Format(time.DateOnly)}
	var visitors, views, refs int
	err = f.store.Query(day, func(db *sql.DB) error {
		if err := db.QueryRow(`SELECT COUNT(*) FROM staging_visitor`).Scan(&visitors); err != nil {
			return err
		}
		if err := db.QueryRow(`SELECT count FROM staging_hit WHERE page_key='/notes/:id'`).Scan(&views); err != nil {
			return err
		}
		return db.QueryRow(`SELECT count FROM staging_ref WHERE host='news.ycombinator.com'`).Scan(&refs)
	})
	if err != nil {
		t.Fatal(err)
	}
	if visitors != 1 || views != 2 || refs != 1 {
		t.Errorf("staging: visitors=%d views=%d refs=%d, want 1/2/1", visitors, views, refs)
	}
}

// A batch that passes the deadline check but reaches a day the close has
// already sealed gets the same 409 as any closed day.
func TestSealedDayIs409(t *testing.T) {
	f := newFixture(t, "2026-10-02T01:59:00Z")
	f.store.Seal(staging.Day{Product: "cindernote", Date: "2026-10-01"})
	if st, r := f.post(t, tokenCinder, goodBatch()); st != 409 || r.Error != "day closed" {
		t.Errorf("got %d %+v, want 409 day closed", st, r)
	}
	if f.logs.Len() != 0 {
		t.Errorf("a sealed day is not an internal error; logged %q", f.logs.String())
	}
}
