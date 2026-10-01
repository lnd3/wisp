package uptime

import (
	"context"
	"crypto/x509"
	"encoding/binary"
	"encoding/json"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func cfgWith(t *testing.T, checks string) *Config {
	t.Helper()
	c, err := Parse([]byte(`{"interval":"60s","timeout":"5s","alerts":[{"type":"webhook","url":"https://example.invalid/hook"}],"checks":` + checks + `}`))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// ---- config ----

func TestParseDefaultsAndValidation(t *testing.T) {
	c := cfgWith(t, `[{"name":"a","type":"http","url":"https://a.example/"},{"name":"d","type":"dns","host":"x.example","server":"192.0.2.53"}]`)
	if c.FailuresBeforeAlert != 2 || c.CertWarnDays != 14 || c.Resolver != "1.1.1.1:53" {
		t.Errorf("defaults = %+v", c)
	}
	if got := c.Checks[0].ExpectStatus; len(got) != 1 || got[0] != 200 {
		t.Errorf("default expect_status = %v", got)
	}
	if c.Checks[1].Server != "192.0.2.53:53" || c.Checks[1].RecordType != "A" {
		t.Errorf("dns defaults = %+v", c.Checks[1])
	}

	for name, js := range map[string]string{
		"no alerts":       `{"checks":[{"name":"a","type":"http","url":"https://a/"}]}`,
		"bad alert type":  `{"alerts":[{"type":"email","url":"https://x/"}],"checks":[{"name":"a","type":"http","url":"https://a/"}]}`,
		"no checks":       `{"alerts":[{"type":"ntfy","url":"https://ntfy.sh/t"}]}`,
		"dup names":       `{"alerts":[{"type":"ntfy","url":"https://ntfy.sh/t"}],"checks":[{"name":"a","type":"http","url":"https://a/"},{"name":"a","type":"http","url":"https://b/"}]}`,
		"bad url":         `{"alerts":[{"type":"ntfy","url":"https://ntfy.sh/t"}],"checks":[{"name":"a","type":"http","url":"a.example"}]}`,
		"bad type":        `{"alerts":[{"type":"ntfy","url":"https://ntfy.sh/t"}],"checks":[{"name":"a","type":"ping","url":"https://a/"}]}`,
		"dns no server":   `{"alerts":[{"type":"ntfy","url":"https://ntfy.sh/t"}],"checks":[{"name":"a","type":"dns","host":"x"}]}`,
		"dns bad expect":  `{"alerts":[{"type":"ntfy","url":"https://ntfy.sh/t"}],"checks":[{"name":"a","type":"dns","host":"x","server":"1.1.1.1","expect":["nope"]}]}`,
		"typo key":        `{"intervall":"60s","alerts":[{"type":"ntfy","url":"https://ntfy.sh/t"}],"checks":[{"name":"a","type":"http","url":"https://a/"}]}`,
		"timeout too big": `{"interval":"10s","timeout":"10s","alerts":[{"type":"ntfy","url":"https://ntfy.sh/t"}],"checks":[{"name":"a","type":"http","url":"https://a/"}]}`,
	} {
		if _, err := Parse([]byte(js)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

// ---- state machine ----

type recorder struct {
	mu   sync.Mutex
	sent []Alert
	fail int // fail this many sends first
}

func (r *recorder) String() string { return "recorder" }
func (r *recorder) Send(_ context.Context, a Alert) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.fail > 0 {
		r.fail--
		return io.ErrUnexpectedEOF
	}
	r.sent = append(r.sent, a)
	return nil
}
func (r *recorder) kinds() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var k []string
	for _, a := range r.sent {
		k = append(k, a.Kind)
	}
	return strings.Join(k, ",")
}

// scripted builds a monitor whose single check returns the given results
// in order, one per round.
func scripted(t *testing.T, results ...Result) (*Monitor, *recorder, *time.Time) {
	t.Helper()
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	m := New(cfgWith(t, `[{"name":"site","type":"http","url":"https://site.example/"}]`),
		Options{Log: log.New(io.Discard, "", 0), Now: func() time.Time { return now }})
	rec := &recorder{}
	m.alerters = []Alerter{rec}
	m.backoff = nil
	i := 0
	m.check = func(context.Context, CheckConfig) Result {
		r := results[i]
		i++
		return r
	}
	return m, rec, &now
}

var (
	ok   = Result{OK: true, Detail: "200"}
	fail = Result{Detail: "status 502 (want 200)"}
)

func TestAlertsOnlyOnStateChange(t *testing.T) {
	m, rec, now := scripted(t, ok, fail, ok, fail, fail, fail, fail, ok, ok)
	for range 9 {
		m.Round(context.Background())
		*now = now.Add(time.Minute)
	}
	// One blip (round 2) alerts nothing; two in a row goes down once
	// (round 5), more failures stay quiet, recovery alerts once.
	if got := rec.kinds(); got != "down,up" {
		t.Fatalf("alerts = %q, want down,up", got)
	}
	if !strings.Contains(rec.sent[0].Message, "failed 2 checks in a row: status 502") {
		t.Errorf("down message = %q", rec.sent[0].Message)
	}
	if !strings.Contains(rec.sent[1].Message, "recovered after 3m") {
		t.Errorf("up message = %q", rec.sent[1].Message)
	}
	states, _ := m.Snapshot()
	if states[0].Status != StatusUp || states[0].Failures != 0 {
		t.Errorf("final state = %+v", states[0])
	}
}

func TestDownFromStartAlerts(t *testing.T) {
	m, rec, _ := scripted(t, fail, fail)
	m.Round(context.Background())
	if states, _ := m.Snapshot(); states[0].Status != StatusUnknown {
		t.Error("one failure must not decide the state")
	}
	m.Round(context.Background())
	if rec.kinds() != "down" {
		t.Errorf("alerts = %q", rec.kinds())
	}
}

func TestCertWarningOncePerRenewal(t *testing.T) {
	base := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	soon := Result{OK: true, Detail: "200", CertExpiry: base.Add(10 * 24 * time.Hour)}
	renewed := Result{OK: true, Detail: "200", CertExpiry: base.Add(80 * 24 * time.Hour)}
	m, rec, _ := scripted(t, soon, soon, renewed, soon)
	for range 4 {
		m.Round(context.Background())
	}
	if rec.kinds() != "cert,cert" {
		t.Errorf("alerts = %q, want one warning, re-armed after renewal", rec.kinds())
	}
	if !strings.Contains(rec.sent[0].Message, "expires in 10 days") {
		t.Errorf("message = %q", rec.sent[0].Message)
	}
}

func TestDeliveryRetries(t *testing.T) {
	rec := &recorder{fail: 2}
	if err := deliver(context.Background(), rec, Alert{Kind: KindDown}, []time.Duration{0, 0}); err != nil || len(rec.sent) != 1 {
		t.Errorf("err=%v sent=%d; want success on the third attempt", err, len(rec.sent))
	}
	rec = &recorder{fail: 3}
	if err := deliver(context.Background(), rec, Alert{}, []time.Duration{0, 0}); err == nil {
		t.Error("must give up after the retries")
	}
}

// ---- channels ----

func TestNtfyAndWebhookPayloads(t *testing.T) {
	var got []*http.Request
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got, bodies = append(got, r.Clone(context.Background())), append(bodies, string(b))
	}))
	defer srv.Close()
	a := Alert{Check: "site", Kind: KindDown, Title: "site is DOWN", Message: "site failed", Time: time.Unix(0, 0).UTC()}

	if err := (&ntfy{url: srv.URL + "/topic", token: "tk", client: srv.Client()}).Send(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	if h := got[0].Header; h.Get("Title") != "site is DOWN" || h.Get("Priority") != "high" || h.Get("Authorization") != "Bearer tk" || bodies[0] != "site failed" {
		t.Errorf("ntfy request: headers=%v body=%q", h, bodies[0])
	}
	if err := (&webhook{url: srv.URL, client: srv.Client()}).Send(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	var decoded Alert
	if err := json.Unmarshal([]byte(bodies[1]), &decoded); err != nil || decoded.Check != "site" || decoded.Kind != KindDown {
		t.Errorf("webhook body = %q (%v)", bodies[1], err)
	}
}

// ---- real checks against local servers ----

func TestHTTPCheck(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/moved":
			http.Redirect(w, r, "/elsewhere", http.StatusMovedPermanently)
		case "/slow":
			time.Sleep(300 * time.Millisecond)
		case "/broken":
			w.WriteHeader(http.StatusBadGateway)
		}
	}))
	defer srv.Close()
	roots := x509.NewCertPool()
	roots.AddCert(srv.Certificate())
	cfg := cfgWith(t, `[{"name":"x","type":"http","url":"https://x.example/"}]`)
	cfg.Timeout.Duration = 150 * time.Millisecond
	p := newProber(cfg, roots)

	run := func(path string, expect ...int) Result {
		if len(expect) == 0 {
			expect = []int{200}
		}
		return p.run(context.Background(), CheckConfig{Type: "http", URL: srv.URL + path, ExpectStatus: expect})
	}
	if r := run("/"); !r.OK || r.Detail != "200" || r.CertExpiry.IsZero() {
		t.Errorf("/ = %+v", r)
	}
	if r := run("/broken"); r.OK || r.Detail != "status 502 (want 200)" {
		t.Errorf("/broken = %+v", r)
	}
	if r := run("/moved", 301); !r.OK {
		t.Errorf("a redirect must be reported, not followed: %+v", r)
	}
	if r := run("/slow"); r.OK || r.Detail != "timeout" {
		t.Errorf("/slow = %+v", r)
	}
	// Without the test CA the certificate must not validate.
	untrusted := newProber(cfg, nil)
	if r := untrusted.run(context.Background(), CheckConfig{Type: "http", URL: srv.URL + "/", ExpectStatus: []int{200}}); r.OK || !strings.HasPrefix(r.Detail, "tls: certificate not valid") {
		t.Errorf("untrusted cert = %+v", r)
	}
}

// fakeDNS answers every A query with the given IPv4 addresses, or
// NXDOMAIN when none are given. Just enough of RFC 1035 for Go's resolver.
func fakeDNS(t *testing.T, answers ...string) string {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pc.Close() })
	go func() {
		buf := make([]byte, 512)
		for {
			n, from, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}
			q := buf[:n]
			end := 12
			for q[end] != 0 { // skip QNAME labels
				end += int(q[end]) + 1
			}
			question := q[12 : end+5]
			qtype := binary.BigEndian.Uint16(q[end+1:])
			resp := append([]byte{}, q[:2]...) // ID
			flags := uint16(0x8180)            // response, RD, RA
			if len(answers) == 0 {
				flags |= 3 // NXDOMAIN
			}
			resp = binary.BigEndian.AppendUint16(resp, flags)
			ans := 0
			if qtype == 1 {
				ans = len(answers)
			}
			resp = binary.BigEndian.AppendUint16(resp, 1)           // QDCOUNT
			resp = binary.BigEndian.AppendUint16(resp, uint16(ans)) // ANCOUNT
			resp = append(resp, 0, 0, 0, 0)                         // NS, AR
			resp = append(resp, question...)
			for i := 0; i < ans; i++ {
				resp = append(resp, 0xc0, 0x0c, 0, 1, 0, 1, 0, 0, 0, 60, 0, 4) // name ptr, A, IN, TTL 60, len 4
				resp = append(resp, net.ParseIP(answers[i]).To4()...)
			}
			pc.WriteTo(resp, from)
		}
	}()
	return pc.LocalAddr().String()
}

func TestDNSCheck(t *testing.T) {
	cfg := cfgWith(t, `[{"name":"x","type":"http","url":"https://x.example/"}]`)
	p := newProber(cfg, nil)
	good := fakeDNS(t, "158.174.211.245")
	nx := fakeDNS(t)

	r := p.run(context.Background(), CheckConfig{Type: "dns", Host: "wisp.mera.network", Server: good, RecordType: "A", Expect: []string{"158.174.211.245"}})
	if !r.OK || r.Detail != "158.174.211.245" {
		t.Errorf("matching answer = %+v", r)
	}
	r = p.run(context.Background(), CheckConfig{Type: "dns", Host: "wisp.mera.network", Server: good, RecordType: "A", Expect: []string{"192.0.2.1"}})
	if r.OK || r.Detail != "got 158.174.211.245 (want 192.0.2.1)" {
		t.Errorf("wrong answer = %+v", r)
	}
	r = p.run(context.Background(), CheckConfig{Type: "dns", Host: "ns1.mera.network", Server: nx, RecordType: "A"})
	if r.OK || r.Detail != "dns: no such host (ns1.mera.network)" {
		t.Errorf("NXDOMAIN = %+v", r)
	}
}

// ---- status page ----

func TestStatusAndHealth(t *testing.T) {
	m, _, now := scripted(t, ok)
	srv := httptest.NewServer(m.Handler())
	defer srv.Close()
	get := func(path string) (int, string) {
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(b)
	}
	if st, _ := get("/healthz"); st != 503 {
		t.Errorf("healthz before any round = %d", st)
	}
	m.Round(context.Background())
	if st, _ := get("/healthz"); st != 200 {
		t.Errorf("healthz after a round = %d", st)
	}
	if st, body := get("/"); st != 200 || !strings.Contains(body, "site") || !strings.Contains(body, `class="s up"`) {
		t.Errorf("status page = %d %q", st, body)
	}
	*now = now.Add(4 * time.Minute) // > 3 intervals without a round
	if st, _ := get("/healthz"); st != 503 {
		t.Errorf("healthz when stalled = %d", st)
	}
}
