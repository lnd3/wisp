package hook

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	firefoxUA = "Mozilla/5.0 (X11; Linux x86_64; rv:131.0) Gecko/20100101 Firefox/131.0"
	iphoneUA  = "Mozilla/5.0 (iPhone; CPU iPhone OS 17_6 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.6 Mobile/15E148 Safari/604.1"
	clientIP  = "203.0.113.7"
)

// ---- test helpers ----

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func newClock(s string) *fakeClock {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return &fakeClock{t: t}
}

func (c *fakeClock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

type received struct {
	header http.Header
	body   []byte
	batch  Batch
}

// ingest is a fake wisp ingest endpoint recording every request.
type ingest struct {
	*httptest.Server
	mu     sync.Mutex
	reqs   []received
	status func(n int) int // status for the n-th request (0-based)
}

func newIngest(t *testing.T) *ingest {
	in := &ingest{status: func(int) int { return http.StatusOK }}
	in.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var b Batch
		if err := json.Unmarshal(body, &b); err != nil {
			t.Errorf("ingest: bad JSON: %v", err)
		}
		in.mu.Lock()
		n := len(in.reqs)
		in.reqs = append(in.reqs, received{header: r.Header.Clone(), body: body, batch: b})
		st := in.status(n)
		in.mu.Unlock()
		w.WriteHeader(st)
	}))
	t.Cleanup(in.Close)
	return in
}

func (in *ingest) requests() []received {
	in.mu.Lock()
	defer in.mu.Unlock()
	return append([]received(nil), in.reqs...)
}

type errSink struct {
	mu   sync.Mutex
	errs []error
}

func (s *errSink) add(err error) { s.mu.Lock(); s.errs = append(s.errs, err); s.mu.Unlock() }
func (s *errSink) count(target error) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, e := range s.errs {
		if errors.Is(e, target) {
			n++
		}
	}
	return n
}

// testHook builds a hook with defaults applied but no dispatcher
// goroutine, so tests drive flush/sendDue directly against a fake clock.
func testHook(t *testing.T, in *ingest, clock *fakeClock, mod func(*Config)) (*Hook, *errSink) {
	t.Helper()
	sink := &errSink{}
	cfg := Config{
		Endpoint:   in.URL + "/v1/ingest",
		ProductKey: "cindernote",
		Token:      "test-token",
		Clock:      clock.Now,
		OnError:    sink.add,
	}
	if mod != nil {
		mod(&cfg)
	}
	cfg, err := withDefaults(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return newHook(cfg), sink
}

func req(ip, ua, referer string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "http://noteshare.example/notes/7HqSecretNoteID?k=secret", nil)
	r.RemoteAddr = ip + ":51234"
	r.Header.Set("User-Agent", ua)
	if referer != "" {
		r.Header.Set("Referer", referer)
	}
	return r
}

func onlyVisitor(t *testing.T, b Batch) Visitor {
	t.Helper()
	if len(b.Visitors) != 1 {
		t.Fatalf("want 1 visitor, got %d", len(b.Visitors))
	}
	return b.Visitors[0]
}

// ---- keying ----

func TestVisitorKey(t *testing.T) {
	s1, _ := newSalt()
	s2, _ := newSalt()
	ip, _ := normalizeIP(clientIP)

	k := visitorKey(&s1, ip, firefoxUA)
	if k != visitorKey(&s1, ip, firefoxUA) {
		t.Error("same salt and inputs must give the same key")
	}
	if len(k) != 22 {
		t.Errorf("key length = %d, want 22 (16 bytes base64url)", len(k))
	}
	if k == visitorKey(&s2, ip, firefoxUA) {
		t.Error("a different salt must give a different key")
	}
	if k == visitorKey(&s1, ip, iphoneUA) {
		t.Error("a different UA must give a different key")
	}

	a, _ := normalizeIP("2001:db8:1:2:aaaa::1")
	b, _ := normalizeIP("2001:db8:1:2:bbbb::9")
	c, _ := normalizeIP("2001:db8:1:3::1")
	if visitorKey(&s1, a, firefoxUA) != visitorKey(&s1, b, firefoxUA) {
		t.Error("IPv6 addresses in the same /64 must share a key")
	}
	if visitorKey(&s1, a, firefoxUA) == visitorKey(&s1, c, firefoxUA) {
		t.Error("IPv6 addresses in different /64s must not share a key")
	}
	m, _ := normalizeIP("::ffff:203.0.113.7")
	if visitorKey(&s1, m, firefoxUA) != k {
		t.Error("IPv4-mapped IPv6 must key like plain IPv4")
	}
	if _, ok := normalizeIP("not-an-ip"); ok {
		t.Error("unparsable IP must be rejected")
	}
}

// ---- accumulation ----

func TestViewsVisitsAndReferrers(t *testing.T) {
	in := newIngest(t)
	clock := newClock("2026-10-01T09:00:00Z")
	h, _ := testHook(t, in, clock, nil)

	h.View(req(clientIP, firefoxUA, "https://news.ycombinator.com/item?id=42"), "/")
	clock.Advance(2 * time.Minute)
	h.View(req(clientIP, firefoxUA, "https://noteshare.example/"), "/notes/:id") // same visit, internal referrer
	h.View(req(clientIP, firefoxUA, "https://lobste.rs/s/x"), "/notes/:id")      // same visit: external referrer mid-visit doesn't count
	clock.Advance(5*time.Hour + 30*time.Minute)
	h.View(req(clientIP, firefoxUA, ""), "/") // gap > 30 min: second visit
	h.Download(req(clientIP, firefoxUA, ""), "/files/:name")

	h.flush()
	h.sendDue(context.Background(), false)
	reqs := in.requests()
	if len(reqs) != 1 {
		t.Fatalf("want 1 request, got %d", len(reqs))
	}
	b := reqs[0].batch
	if b.Product != "cindernote" || b.Day != "2026-10-01" || b.BatchID == "" {
		t.Errorf("batch header = %+v", b)
	}
	v := onlyVisitor(t, b)
	if v.Visits != 2 {
		t.Errorf("visits = %d, want 2", v.Visits)
	}
	if v.Views["/"] != 2 || v.Views["/notes/:id"] != 2 || len(v.Views) != 2 {
		t.Errorf("views = %v", v.Views)
	}
	if v.Downloads["/files/:name"] != 1 {
		t.Errorf("downloads = %v", v.Downloads)
	}
	if len(v.Referrers) != 1 || v.Referrers["news.ycombinator.com"] != 1 {
		t.Errorf("referrers = %v (want only the external host, once)", v.Referrers)
	}
	if v.Agent != (Agent{Browser: "firefox", OS: "linux", Device: "desktop"}) {
		t.Errorf("agent = %+v", v.Agent)
	}
	if s := h.Stats(); s.Hits != 5 || s.BatchesSent != 1 || s.BatchesQueued != 0 {
		t.Errorf("stats = %+v", s)
	}
}

func TestDeltasAcrossBatches(t *testing.T) {
	in := newIngest(t)
	clock := newClock("2026-10-01T09:00:00Z")
	h, _ := testHook(t, in, clock, nil)

	h.View(req(clientIP, firefoxUA, ""), "/")
	h.flush()
	clock.Advance(5 * time.Minute)
	h.View(req(clientIP, firefoxUA, ""), "/")
	h.flush()
	h.sendDue(context.Background(), false)

	reqs := in.requests()
	if len(reqs) != 2 {
		t.Fatalf("want 2 requests, got %d", len(reqs))
	}
	a, b := onlyVisitor(t, reqs[0].batch), onlyVisitor(t, reqs[1].batch)
	if a.K != b.K {
		t.Error("same visitor, same day must keep its key across batches")
	}
	if a.Visits != 1 || b.Visits != 0 || a.Views["/"] != 1 || b.Views["/"] != 1 {
		t.Errorf("batches must carry increments only: %+v / %+v", a, b)
	}
	if reqs[0].batch.BatchID == reqs[1].batch.BatchID {
		t.Error("distinct batches must have distinct IDs")
	}
}

func TestDroppedHits(t *testing.T) {
	in := newIngest(t)
	clock := newClock("2026-10-01T09:00:00Z")
	h, sink := testHook(t, in, clock, nil)

	h.View(req(clientIP, "", ""), "/")
	h.View(req(clientIP, "curl/8.5.0", ""), "/")
	h.View(req(clientIP, "Mozilla/5.0 (compatible; Googlebot/2.1)", ""), "/")
	for _, k := range []string{"", "notes", "/notes/7Hq?k=secret", "/a#b", "/a b", "/" + strings.Repeat("x", 256)} {
		h.View(req(clientIP, firefoxUA, ""), k)
	}
	h.View(req("garbage", firefoxUA, ""), "/")

	s := h.Stats()
	if s.Bots != 3 || s.Rejected != 7 || s.Hits != 0 {
		t.Errorf("stats = %+v, want 3 bots, 7 rejected, 0 hits", s)
	}
	if n := sink.count(ErrInvalidPageKey); n != 1 {
		t.Errorf("ErrInvalidPageKey reported %d times, want once per day", n)
	}
	h.flush()
	h.sendDue(context.Background(), false)
	if n := len(in.requests()); n != 0 {
		t.Errorf("dropped hits must not produce a batch; got %d requests", n)
	}
}

func TestPagesPerVisitorCap(t *testing.T) {
	in := newIngest(t)
	h, _ := testHook(t, in, newClock("2026-10-01T09:00:00Z"), nil)
	for i := 0; i <= maxPagesPerVisitor; i++ {
		h.View(req(clientIP, firefoxUA, ""), fmt.Sprintf("/p/%d", i))
	}
	if s := h.Stats(); s.Hits != maxPagesPerVisitor || s.Rejected != 1 {
		t.Errorf("stats = %+v", s)
	}
}

func TestVisitorsPerDayCap(t *testing.T) {
	in := newIngest(t)
	h, _ := testHook(t, in, newClock("2026-10-01T09:00:00Z"), func(c *Config) { c.MaxVisitorsPerDay = 2 })
	for i := 1; i <= 3; i++ {
		h.View(req(fmt.Sprintf("198.51.100.%d", i), firefoxUA, ""), "/")
	}
	h.View(req("198.51.100.1", firefoxUA, ""), "/") // existing visitor still counts
	if s := h.Stats(); s.Hits != 3 || s.Rejected != 1 {
		t.Errorf("stats = %+v", s)
	}
}

func TestBatchSplit(t *testing.T) {
	in := newIngest(t)
	h, _ := testHook(t, in, newClock("2026-10-01T09:00:00Z"), nil)
	for i := 0; i <= maxVisitorsPerBatch; i++ {
		h.View(req(fmt.Sprintf("10.%d.%d.%d", i>>16&255, i>>8&255, i&255), firefoxUA, ""), "/")
	}
	h.flush()
	h.sendDue(context.Background(), false)
	reqs := in.requests()
	if len(reqs) != 2 || len(reqs[0].batch.Visitors) != maxVisitorsPerBatch || len(reqs[1].batch.Visitors) != 1 {
		t.Fatalf("want batches of %d and 1, got %d requests", maxVisitorsPerBatch, len(reqs))
	}
}

// ---- privacy ----

func TestNothingIdentifyingLeavesTheProcess(t *testing.T) {
	in := newIngest(t)
	h, _ := testHook(t, in, newClock("2026-10-01T09:00:00Z"), nil)
	h.View(req(clientIP, firefoxUA, "https://search.example/?q=alice%40example.com"), "/notes/:id")
	h.flush()
	h.sendDue(context.Background(), false)

	r := in.requests()[0]
	body := string(r.body)
	for _, forbidden := range []string{clientIP, firefoxUA, "Gecko", "rv:131", "7HqSecretNoteID", "secret", "alice", "q="} {
		if strings.Contains(body, forbidden) {
			t.Errorf("request body contains %q: %s", forbidden, body)
		}
	}
	if got := r.header.Get("Authorization"); got != "Bearer test-token" {
		t.Errorf("Authorization = %q", got)
	}
	if got := r.header.Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q", got)
	}
}

func TestClientIPOverride(t *testing.T) {
	in := newIngest(t)
	h, sink := testHook(t, in, newClock("2026-10-01T09:00:00Z"), func(c *Config) {
		c.ClientIP = func(r *http.Request) string { return r.Header.Get("X-Forwarded-For") }
	})
	for _, xff := range []string{"198.51.100.1", "198.51.100.2"} {
		r := req("172.27.1.5", firefoxUA, "") // the proxy's address
		r.Header.Set("X-Forwarded-For", xff)
		h.View(r, "/")
	}
	h.flush()
	h.sendDue(context.Background(), false)
	if n := len(in.requests()[0].batch.Visitors); n != 2 {
		t.Errorf("visitors = %d, want 2 (keyed by forwarded client IP)", n)
	}
	if h.Stats().PrivateClientIPs != 0 || sink.count(ErrLikelyProxyAddress) != 0 {
		t.Error("forwarded public IPs must not count as private")
	}
}

func TestLikelyProxyWarning(t *testing.T) {
	in := newIngest(t)
	h, sink := testHook(t, in, newClock("2026-10-01T09:00:00Z"), nil)
	for i := 0; i < 20; i++ {
		h.View(req("172.27.1.5", fmt.Sprintf("%s v%d", firefoxUA, i), ""), "/")
	}
	if n := sink.count(ErrLikelyProxyAddress); n != 1 {
		t.Errorf("ErrLikelyProxyAddress reported %d times, want once", n)
	}
	if s := h.Stats(); s.PrivateClientIPs != 20 {
		t.Errorf("PrivateClientIPs = %d", s.PrivateClientIPs)
	}
}

// ---- day rollover ----

func TestMidnightRolloverOnHit(t *testing.T) {
	in := newIngest(t)
	clock := newClock("2026-10-01T23:59:00Z")
	h, _ := testHook(t, in, clock, nil)

	h.View(req(clientIP, firefoxUA, ""), "/")
	oldDay := h.day
	clock.Advance(2 * time.Minute)
	h.View(req(clientIP, firefoxUA, ""), "/")

	select {
	case <-h.kick:
	default:
		t.Error("a hit that retires a day must kick the dispatcher")
	}
	if oldDay.salt != (salt{}) || oldDay.visitors != nil {
		t.Error("the retired day's salt must be wiped and its accumulator dropped")
	}
	h.sendDue(context.Background(), false) // what the kick triggers
	reqs := in.requests()
	if len(reqs) != 1 || reqs[0].batch.Day != "2026-10-01" {
		t.Fatalf("want the finished day's batch sent at once, got %d requests", len(reqs))
	}
	h.flush()
	h.sendDue(context.Background(), false)
	reqs = in.requests()
	if len(reqs) != 2 || reqs[1].batch.Day != "2026-10-02" {
		t.Fatalf("want a second batch for the new day, got %d requests", len(reqs))
	}
	a, b := onlyVisitor(t, reqs[0].batch), onlyVisitor(t, reqs[1].batch)
	if a.K == b.K {
		t.Error("the same visitor must get an unrelated key on a new day")
	}
	if b.Visits != 1 {
		t.Errorf("a new day starts a new visit; visits = %d", b.Visits)
	}
}

func TestIdleMidnightRetiresDay(t *testing.T) {
	in := newIngest(t)
	clock := newClock("2026-10-01T23:00:00Z")
	h, _ := testHook(t, in, clock, nil)
	h.View(req(clientIP, firefoxUA, ""), "/")
	clock.Advance(time.Hour + time.Second)
	h.flush() // what the dispatcher's midnight timer does, with no hit to trigger it
	if h.day != nil {
		t.Error("an idle rollover must drop the day (and its salt) without creating a new one")
	}
	h.sendDue(context.Background(), false)
	if reqs := in.requests(); len(reqs) != 1 || reqs[0].batch.Day != "2026-10-01" {
		t.Fatalf("want the finished day's batch, got %d requests", len(reqs))
	}
}

// ---- sending ----

func TestIdleIntervalSendsNothing(t *testing.T) {
	in := newIngest(t)
	h, _ := testHook(t, in, newClock("2026-10-01T09:00:00Z"), nil)
	for i := 0; i < 3; i++ {
		h.flush()
		h.sendDue(context.Background(), false)
	}
	if n := len(in.requests()); n != 0 {
		t.Errorf("idle intervals made %d requests, want 0", n)
	}
}

func TestRetryKeepsBatchIDAndBacksOff(t *testing.T) {
	in := newIngest(t)
	in.status = func(n int) int {
		if n == 0 {
			return http.StatusServiceUnavailable
		}
		return http.StatusOK
	}
	clock := newClock("2026-10-01T09:00:00Z")
	h, sink := testHook(t, in, clock, func(c *Config) { c.Interval = time.Minute })

	h.View(req(clientIP, firefoxUA, ""), "/")
	h.flush()
	h.sendDue(context.Background(), false)
	if s := h.Stats(); s.BatchesQueued != 1 || s.BatchesSent != 0 {
		t.Fatalf("after a 503: stats = %+v", s)
	}
	if len(sink.errs) != 1 {
		t.Errorf("want one retry error reported, got %v", sink.errs)
	}

	h.sendDue(context.Background(), false) // backoff not elapsed
	if n := len(in.requests()); n != 1 {
		t.Fatalf("retried before backoff elapsed: %d requests", n)
	}

	clock.Advance(time.Minute)
	h.sendDue(context.Background(), false)
	reqs := in.requests()
	if len(reqs) != 2 {
		t.Fatalf("want a retry, got %d requests", len(reqs))
	}
	if reqs[0].batch.BatchID != reqs[1].batch.BatchID || string(reqs[0].body) != string(reqs[1].body) {
		t.Error("a retry must resend the same batch under the same batch_id")
	}
	if s := h.Stats(); s.BatchesQueued != 0 || s.BatchesSent != 1 {
		t.Errorf("after retry: stats = %+v", s)
	}
}

func TestPermanentRefusalsDrop(t *testing.T) {
	for _, tc := range []struct {
		status int
		want   error
	}{
		{http.StatusConflict, ErrDayClosed},
		{http.StatusUnauthorized, ErrRejected},
		{http.StatusBadRequest, ErrRejected},
	} {
		t.Run(http.StatusText(tc.status), func(t *testing.T) {
			in := newIngest(t)
			in.status = func(int) int { return tc.status }
			h, sink := testHook(t, in, newClock("2026-10-01T09:00:00Z"), nil)
			h.View(req(clientIP, firefoxUA, ""), "/")
			h.flush()
			h.sendDue(context.Background(), false)
			if s := h.Stats(); s.BatchesQueued != 0 || s.BatchesDropped != 1 {
				t.Errorf("stats = %+v", s)
			}
			if sink.count(tc.want) != 1 {
				t.Errorf("want %v reported, got %v", tc.want, sink.errs)
			}
		})
	}
}

func TestExpiredBatchDroppedUnsent(t *testing.T) {
	in := newIngest(t)
	in.status = func(int) int { return http.StatusBadGateway }
	clock := newClock("2026-10-01T09:00:00Z")
	h, _ := testHook(t, in, clock, nil)
	h.View(req(clientIP, firefoxUA, ""), "/")
	h.flush()
	h.sendDue(context.Background(), false)

	clock.Advance(15*time.Hour + 16*time.Minute) // 2026-10-02T00:16Z = day start + 24h16m
	h.sendDue(context.Background(), true)
	if n := len(in.requests()); n != 1 {
		t.Errorf("a batch past its close deadline must not be sent; %d requests", n)
	}
	if s := h.Stats(); s.BatchesQueued != 0 || s.BatchesDropped != 1 {
		t.Errorf("stats = %+v", s)
	}
}

func TestQueueCapDropsOldest(t *testing.T) {
	in := newIngest(t)
	in.status = func(int) int { return http.StatusServiceUnavailable }
	clock := newClock("2026-10-01T09:00:00Z")
	h, _ := testHook(t, in, clock, func(c *Config) { c.MaxQueuedBatches = 2 })
	var ids []string
	for i := 0; i < 3; i++ {
		h.View(req(clientIP, firefoxUA, ""), "/")
		h.flush()
		h.mu.Lock()
		ids = append(ids, h.queue[len(h.queue)-1].id)
		h.mu.Unlock()
	}
	h.mu.Lock()
	got := []string{h.queue[0].id, h.queue[1].id}
	h.mu.Unlock()
	if got[0] != ids[1] || got[1] != ids[2] {
		t.Error("the oldest batch must be evicted first")
	}
	if s := h.Stats(); s.BatchesQueued != 2 || s.BatchesDropped != 1 {
		t.Errorf("stats = %+v", s)
	}
}

// ---- lifecycle ----

func TestDispatcherSendsOnlyWhenPending(t *testing.T) {
	in := newIngest(t)
	h, err := Start(Config{
		Endpoint: in.URL + "/v1/ingest", ProductKey: "cindernote", Token: "t",
		Interval: 10 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close(context.Background())

	time.Sleep(60 * time.Millisecond)
	if n := len(in.requests()); n != 0 {
		t.Fatalf("idle dispatcher made %d requests", n)
	}
	h.View(req(clientIP, firefoxUA, ""), "/")
	waitFor(t, func() bool { return len(in.requests()) == 1 })
	time.Sleep(60 * time.Millisecond)
	if n := len(in.requests()); n != 1 {
		t.Errorf("after sending, idle intervals made more requests: %d", n)
	}
}

func TestCloseFlushesAndStops(t *testing.T) {
	in := newIngest(t)
	h, err := Start(Config{Endpoint: in.URL, ProductKey: "cindernote", Token: "t", Interval: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	h.View(req(clientIP, firefoxUA, ""), "/")
	if err := h.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if n := len(in.requests()); n != 1 {
		t.Fatalf("Close must flush pending data; %d requests", n)
	}
	h.View(req(clientIP, firefoxUA, ""), "/")
	if h.Stats().Hits != 1 {
		t.Error("hits after Close must be ignored")
	}
	if err := h.Close(context.Background()); err != nil {
		t.Errorf("second Close: %v", err)
	}
}

func TestCloseReportsUnsent(t *testing.T) {
	in := newIngest(t)
	in.status = func(int) int { return http.StatusServiceUnavailable }
	h, err := Start(Config{Endpoint: in.URL, ProductKey: "cindernote", Token: "t", Interval: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	h.View(req(clientIP, firefoxUA, ""), "/")
	if err := h.Close(context.Background()); err == nil {
		t.Error("Close must report batches it couldn't send")
	}
}

func TestDisabledAndNilHooks(t *testing.T) {
	h, err := Start(Config{})
	if err != nil {
		t.Fatal(err)
	}
	var nilHook *Hook
	for _, x := range []*Hook{h, nilHook} {
		x.View(req(clientIP, firefoxUA, ""), "/")
		x.Download(req(clientIP, firefoxUA, ""), "/f")
		if x.Stats() != (Stats{}) {
			t.Error("a disabled/nil hook must count nothing")
		}
		if err := x.Close(context.Background()); err != nil {
			t.Error(err)
		}
	}
}

func TestStartValidation(t *testing.T) {
	for _, tc := range []struct {
		name string
		cfg  Config
		ok   bool
	}{
		{"https", Config{Endpoint: "https://wisp.mera.network/v1/ingest", ProductKey: "p", Token: "t"}, true},
		{"loopback http", Config{Endpoint: "http://127.0.0.1:9/v1/ingest", ProductKey: "p", Token: "t"}, true},
		{"localhost http", Config{Endpoint: "http://localhost:9/v1/ingest", ProductKey: "p", Token: "t"}, true},
		{"plain http", Config{Endpoint: "http://wisp.mera.network/v1/ingest", ProductKey: "p", Token: "t"}, false},
		{"no product", Config{Endpoint: "https://wisp.mera.network", Token: "t"}, false},
		{"no token", Config{Endpoint: "https://wisp.mera.network", ProductKey: "p"}, false},
		{"bad url", Config{Endpoint: "://", ProductKey: "p", Token: "t"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, err := Start(tc.cfg)
			if (err == nil) != tc.ok {
				t.Fatalf("err = %v, want ok=%v", err, tc.ok)
			}
			if h != nil {
				h.Close(context.Background())
			}
		})
	}
}

// ---- parsing ----

func TestExternalReferrer(t *testing.T) {
	for ref, want := range map[string]string{
		"":                                       "",
		"https://news.ycombinator.com/item?id=1": "news.ycombinator.com",
		"https://Search.Example:8443/?q=x":       "search.example",
		"https://noteshare.example/other":        "", // same host as the request
		"android-app://com.example":              "",
		"not a url %%":                           "",
	} {
		if got := externalReferrer(req(clientIP, firefoxUA, ref)); got != want {
			t.Errorf("externalReferrer(%q) = %q, want %q", ref, got, want)
		}
	}
}

func TestAgentAndBots(t *testing.T) {
	for ua, want := range map[string]Agent{
		firefoxUA: {"firefox", "linux", "desktop"},
		iphoneUA:  {"safari", "ios", "mobile"},
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/129.0.0.0 Safari/537.36 Edg/129.0.0.0": {"edge", "windows", "desktop"},
		"Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/129.0.0.0 Mobile Safari/537.36":         {"chrome", "android", "mobile"},
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 14_6) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.6 Safari/605.1.15":            {"safari", "macos", "desktop"},
	} {
		if got := parseAgent(ua); got != want {
			t.Errorf("parseAgent(%q) = %+v, want %+v", ua, got, want)
		}
		if isBot(ua) {
			t.Errorf("isBot(%q) = true", ua)
		}
	}
	for _, ua := range []string{"", "  ", "curl/8.5.0", "Go-http-client/1.1", "python-requests/2.32", "Mozilla/5.0 (compatible; bingbot/2.0)", "wisp-hook/1"} {
		if !isBot(ua) {
			t.Errorf("isBot(%q) = false", ua)
		}
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for condition")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestNetworkErrorRetries(t *testing.T) {
	in := newIngest(t)
	h, sink := testHook(t, in, newClock("2026-10-01T09:00:00Z"), nil)
	in.Close() // nothing listening: every send fails at the network level
	h.View(req(clientIP, firefoxUA, ""), "/")
	h.flush()
	h.sendDue(context.Background(), false)
	if s := h.Stats(); s.BatchesQueued != 1 || s.BatchesDropped != 0 {
		t.Errorf("a network failure must keep the batch for retry: %+v", s)
	}
	if len(sink.errs) != 1 || strings.Contains(sink.errs[0].Error(), clientIP) {
		t.Errorf("want one retry error without request data, got %v", sink.errs)
	}
}
