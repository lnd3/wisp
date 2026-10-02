package hook

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Defaults and limits — wisp's plan/designs/D002, values marked
// "(proposed)" there.
const (
	DefaultInterval          = 5 * time.Minute
	DefaultMaxQueuedBatches  = 288     // a full day of 5-minute batches
	DefaultMaxVisitorsPerDay = 200_000 // bounds accumulator memory
	DefaultHTTPTimeout       = 10 * time.Second

	visitGap            = 30 * time.Minute // a gap longer than this starts a new visit
	maxPageKeyLen       = 256
	maxPagesPerVisitor  = 500
	maxVisitorsPerBatch = 10_000
	maxReferrerLen      = 253
	maxBackoff          = time.Hour
)

// DayCloseAfter is how long after a UTC day starts wisp keeps accepting
// batches for it: the day's 24h plus a 16-minute grace. A hook sends the
// finished day's last batch right at midnight and retries on its
// 5-minute ticks, so that's about three chances (≈00:00, 00:05, 00:15).
// A product built against an older, longer value still works: its late
// retries just get 409 and are dropped. Past it, wisp answers
// 409 and has deleted that day's keyed data. Shared by the hook and the
// ingest API as part of the wire contract.
const DayCloseAfter = 24*time.Hour + 16*time.Minute

var (
	// ErrInvalidPageKey: a page key wasn't a route template — empty, not
	// starting with '/', containing '?', '#' or control characters, or
	// longer than 256 bytes. The hit is dropped. The offending key is
	// deliberately not included: it may be a raw path carrying a secret.
	ErrInvalidPageKey = errors.New("wisp hook: invalid page key (must be a route template like \"/notes/:id\")")
	// ErrLikelyProxyAddress: most hits today came from loopback/private
	// addresses — the product is probably behind a proxy without
	// Config.ClientIP set, so every visitor collapses into a few keys.
	ErrLikelyProxyAddress = errors.New("wisp hook: most client addresses are private/loopback — set Config.ClientIP to the product's trusted-proxy client-IP logic")
	// ErrDayClosed: wisp refused a batch because its day is already
	// closed (409). The batch is dropped; the count stays a lower bound.
	ErrDayClosed = errors.New("wisp hook: day already closed at wisp, batch dropped")
	// ErrRejected: wisp refused a batch permanently (a 4xx other than
	// 408/409/429 — e.g. a bad token). The batch is dropped.
	ErrRejected = errors.New("wisp hook: batch rejected by wisp")
)

// Config configures a Hook. Only Endpoint, ProductKey and Token are
// required; an empty Endpoint yields a disabled, no-op Hook (for dev and
// local runs).
type Config struct {
	// Endpoint is wisp's ingest URL, e.g. "https://wisp.mera.network/v1/ingest".
	// Must be https, except for loopback hosts (local testing).
	Endpoint string
	// ProductKey is this product's public identifier at wisp.
	ProductKey string
	// Token is this product's secret ingest token. Keep it in the
	// product's deploy/.env, never in source.
	Token string

	// Interval between sends. Nothing is sent when nothing is pending.
	// Default 5 minutes.
	Interval time.Duration
	// ClientIP returns the visitor's IP for a request. Products behind a
	// proxy must supply their own trusted-proxy logic here. The default
	// uses r.RemoteAddr.
	ClientIP func(r *http.Request) string
	// OnError, if set, receives send failures and misuse warnings. It is
	// called without internal locks held. Errors never contain request
	// data (IPs, User-Agents, keys, raw paths).
	OnError func(error)

	// HTTPClient used for ingestion calls. Default: a client with a
	// 10-second timeout.
	HTTPClient *http.Client
	// MaxQueuedBatches caps batches kept for (re)sending; the oldest are
	// dropped first. Default 288.
	MaxQueuedBatches int
	// MaxVisitorsPerDay caps distinct visitors tracked per day; hits from
	// further new visitors are dropped. Default 200,000.
	MaxVisitorsPerDay int
	// Clock returns the current time. For tests; default time.Now.
	Clock func() time.Time
}

// Stats are cumulative counters since Start.
type Stats struct {
	Hits             uint64 // hits counted
	Bots             uint64 // hits dropped as bots
	Rejected         uint64 // hits dropped: invalid page key, unparsable client IP, or a cap reached
	PrivateClientIPs uint64 // counted hits whose client IP was loopback/private (see ErrLikelyProxyAddress)
	BatchesSent      uint64
	BatchesDropped   uint64 // refused by wisp, expired, or evicted by MaxQueuedBatches
	BatchesQueued    int    // currently waiting to be sent or retried
}

// Hook accumulates hits and sends them to wisp. Create one with Start;
// all methods are safe for concurrent use and on a nil *Hook.
type Hook struct {
	cfg      Config
	disabled bool

	mu     sync.Mutex
	closed bool
	day    *dayState
	queue  []*queued
	stats  Stats
	warned map[error]string // error → UTC date it was last reported

	sendMu sync.Mutex // serializes sendDue between the dispatcher and Close
	kick   chan struct{}
	stop   chan struct{}
	done   chan struct{}
	ctx    context.Context // cancelled by Close, aborting an in-flight dispatcher send
	cancel context.CancelFunc
}

type dayState struct {
	date     string
	start    time.Time
	salt     salt
	visitors map[string]*visitorState
	pending  map[string]*Visitor

	hits, privateHits int
}

type visitorState struct {
	lastSeen time.Time
	agent    Agent
	pages    map[string]struct{} // distinct "kind page" pairs, for the per-visitor cap
}

type queued struct {
	day      string
	id       string
	body     []byte
	deadline time.Time
	attempts int
	next     time.Time
}

const (
	kindView     = "view"
	kindDownload = "download"
)

// Start validates cfg, applies defaults and starts the dispatcher. With
// an empty Endpoint it returns a disabled Hook whose methods do nothing.
func Start(cfg Config) (*Hook, error) {
	if cfg.Endpoint == "" {
		return &Hook{disabled: true}, nil
	}
	cfg, err := withDefaults(cfg)
	if err != nil {
		return nil, err
	}
	h := newHook(cfg)
	go h.run()
	return h, nil
}

// withDefaults validates cfg and fills in defaults.
func withDefaults(cfg Config) (Config, error) {
	u, err := url.Parse(cfg.Endpoint)
	if err != nil || u.Host == "" {
		return cfg, fmt.Errorf("wisp hook: invalid Endpoint")
	}
	switch {
	case u.Scheme == "https":
	case u.Scheme == "http" && isLoopbackHost(u.Hostname()):
	default:
		return cfg, fmt.Errorf("wisp hook: Endpoint must be https (http is allowed only for loopback hosts)")
	}
	if cfg.ProductKey == "" {
		return cfg, fmt.Errorf("wisp hook: ProductKey is required")
	}
	if cfg.Token == "" {
		return cfg, fmt.Errorf("wisp hook: Token is required")
	}
	if cfg.Interval <= 0 {
		cfg.Interval = DefaultInterval
	}
	if cfg.ClientIP == nil {
		cfg.ClientIP = remoteAddrIP
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: DefaultHTTPTimeout}
	}
	if cfg.MaxQueuedBatches <= 0 {
		cfg.MaxQueuedBatches = DefaultMaxQueuedBatches
	}
	if cfg.MaxVisitorsPerDay <= 0 {
		cfg.MaxVisitorsPerDay = DefaultMaxVisitorsPerDay
	}
	if cfg.Clock == nil {
		cfg.Clock = time.Now
	}
	return cfg, nil
}

func newHook(cfg Config) *Hook {
	ctx, cancel := context.WithCancel(context.Background())
	return &Hook{
		cfg:    cfg,
		warned: make(map[error]string),
		kick:   make(chan struct{}, 1),
		stop:   make(chan struct{}),
		done:   make(chan struct{}),
		ctx:    ctx,
		cancel: cancel,
	}
}

// View records a page view for r under the route template pageKey
// (e.g. "/notes/:id" — never the raw path). Call it where the product
// has actually served a page. It never blocks on the network.
func (h *Hook) View(r *http.Request, pageKey string) { h.observe(r, kindView, pageKey) }

// Download records a file download for r under the route template
// pageKey (e.g. "/files/:name"). It never blocks on the network.
func (h *Hook) Download(r *http.Request, pageKey string) { h.observe(r, kindDownload, pageKey) }

// Stats returns a snapshot of the hook's counters.
func (h *Hook) Stats() Stats {
	if h == nil || h.disabled {
		return Stats{}
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	s := h.stats
	s.BatchesQueued = len(h.queue)
	return s
}

func (h *Hook) observe(r *http.Request, kind, pageKey string) {
	if h == nil || h.disabled || r == nil {
		return
	}
	// Read what's needed from the request before taking the lock. The raw
	// IP and User-Agent never leave this function.
	ua := r.UserAgent()
	ipRaw := h.cfg.ClientIP(r)
	ref := externalReferrer(r)
	now := h.cfg.Clock().UTC()

	var report []error
	h.mu.Lock()
	report = h.observeLocked(now, kind, pageKey, ua, ipRaw, ref)
	h.mu.Unlock()
	for _, err := range report {
		h.report(err)
	}
}

func (h *Hook) observeLocked(now time.Time, kind, pageKey, ua, ipRaw, ref string) []error {
	if h.closed {
		return nil
	}
	if isBot(ua) {
		h.stats.Bots++
		return nil
	}
	date := now.Format(time.DateOnly)
	if !ValidPageKey(pageKey) {
		h.stats.Rejected++
		return h.warnOnceLocked(ErrInvalidPageKey, date)
	}
	ipnorm, ok := normalizeIP(ipRaw)
	if !ok {
		h.stats.Rejected++
		return nil
	}

	retired := h.retireLocked(now)
	d, err := h.currentDayLocked(now)
	if err != nil {
		h.stats.Rejected++
		return []error{err}
	}
	if retired {
		// The finished day's last batch is queued; send it now rather than
		// waiting for the next tick.
		select {
		case h.kick <- struct{}{}:
		default:
		}
	}

	key := visitorKey(&d.salt, ipnorm, ua)
	st := d.visitors[key]
	if st == nil {
		if len(d.visitors) >= h.cfg.MaxVisitorsPerDay {
			h.stats.Rejected++
			return nil
		}
		st = &visitorState{agent: parseAgent(ua), pages: make(map[string]struct{})}
		d.visitors[key] = st
	}
	page := kind + " " + pageKey
	if _, seen := st.pages[page]; !seen {
		if len(st.pages) >= maxPagesPerVisitor {
			h.stats.Rejected++
			return nil
		}
		st.pages[page] = struct{}{}
	}

	p := d.pending[key]
	if p == nil {
		p = &Visitor{K: key, Agent: st.agent}
		d.pending[key] = p
	}
	if st.lastSeen.IsZero() || now.Sub(st.lastSeen) > visitGap {
		p.Visits++
		if ref != "" {
			bump(&p.Referrers, ref)
		}
	}
	st.lastSeen = now
	if kind == kindView {
		bump(&p.Views, pageKey)
	} else {
		bump(&p.Downloads, pageKey)
	}

	h.stats.Hits++
	d.hits++
	if isPrivate(ipRaw) {
		h.stats.PrivateClientIPs++
		d.privateHits++
		if d.privateHits >= 10 && d.privateHits*2 >= d.hits {
			return h.warnOnceLocked(ErrLikelyProxyAddress, date)
		}
	}
	return nil
}

// retireLocked ends the current day if now falls on a later (or
// different) UTC date: its pending increments are queued as the day's
// final batch, and its salt and accumulator are discarded. It reports
// whether a day was retired.
func (h *Hook) retireLocked(now time.Time) bool {
	d := h.day
	if d == nil || d.date == now.Format(time.DateOnly) {
		return false
	}
	h.enqueueLocked(d)
	d.salt.wipe()
	d.visitors = nil
	d.pending = nil
	h.day = nil
	return true
}

func (h *Hook) currentDayLocked(now time.Time) (*dayState, error) {
	if h.day != nil {
		return h.day, nil
	}
	s, err := newSalt()
	if err != nil {
		return nil, fmt.Errorf("wisp hook: generating daily salt: %w", err)
	}
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	h.day = &dayState{
		date:     now.Format(time.DateOnly),
		start:    start,
		salt:     s,
		visitors: make(map[string]*visitorState),
		pending:  make(map[string]*Visitor),
	}
	return h.day, nil
}

// warnOnceLocked returns err the first time it's seen on a given UTC
// date, so a misuse repeated on every request reports once a day, not
// once per hit.
func (h *Hook) warnOnceLocked(err error, date string) []error {
	if h.warned[err] == date {
		return nil
	}
	h.warned[err] = date
	return []error{err}
}

func (h *Hook) report(err error) {
	if err != nil && h.cfg.OnError != nil {
		h.cfg.OnError(err)
	}
}

// ValidPageKey reports whether k is an acceptable page key: a route
// template, "/"-rooted, at most 256 bytes, with no query string,
// fragment, whitespace or control characters. The ingest API applies the
// same rule.
func ValidPageKey(k string) bool {
	if k == "" || len(k) > maxPageKeyLen || k[0] != '/' {
		return false
	}
	for i := 0; i < len(k); i++ {
		c := k[i]
		if c == '?' || c == '#' || c <= ' ' || c == 0x7F {
			return false
		}
	}
	return true
}

// externalReferrer returns the lowercase host of r's Referer if it's an
// http(s) URL on a different host than the request — only the host,
// never the path or query, which can identify.
func externalReferrer(r *http.Request) string {
	ref := r.Referer()
	if ref == "" {
		return ""
	}
	u, err := url.Parse(ref)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return ""
	}
	host := strings.ToLower(u.Hostname())
	if host == "" || len(host) > maxReferrerLen {
		return ""
	}
	if host == strings.ToLower(hostOnly(r.Host)) {
		return ""
	}
	return host
}

func hostOnly(hostport string) string {
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		return h
	}
	return hostport
}

func remoteAddrIP(r *http.Request) string {
	return hostOnly(r.RemoteAddr)
}

func isLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
