package uptime

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"regexp"
	"slices"
	"strings"
	"time"
)

// Result is one check's outcome.
type Result struct {
	OK         bool
	Detail     string        // human-readable: "200", "status 502 (want 200)", "dns: no such host", …
	Latency    time.Duration // time to the response
	CertExpiry time.Time     // leaf certificate NotAfter for HTTPS; zero otherwise
}

// prober runs checks. Its HTTP client and resolvers never use the host's
// own name resolution: every lookup goes to Config.Resolver (or, for DNS
// checks, the check's own Server).
type prober struct {
	timeout  time.Duration
	resolver *net.Resolver // the external resolver used for every lookup
	client   *http.Client
}

func newProber(cfg *Config, roots *x509.CertPool) *prober {
	res := resolverAt(cfg.Resolver, nil)
	dialer := &net.Dialer{Timeout: cfg.Timeout.Duration, Resolver: res}
	tr := &http.Transport{
		DialContext:           dialer.DialContext,
		DisableKeepAlives:     true, // a fresh connection — and so a fresh lookup — every check
		ForceAttemptHTTP2:     true,
		TLSClientConfig:       &tls.Config{RootCAs: roots}, // nil = the system roots
		Proxy:                 nil,                         // never via a proxy from the environment
		TLSHandshakeTimeout:   cfg.Timeout.Duration,
		ResponseHeaderTimeout: cfg.Timeout.Duration,
	}
	return &prober{
		timeout:  cfg.Timeout.Duration,
		resolver: res,
		client: &http.Client{
			Transport: tr,
			// Report the first response as-is: a redirect is a status like
			// any other, listed in expect_status if it's what "up" means.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}
}

// resolverAt returns a resolver that sends every query to server
// (host:port). If server is a hostname, it is itself resolved through
// via (nil = the system resolver, used only to find a configured DNS
// server by name).
func resolverAt(server string, via *net.Resolver) *net.Resolver {
	return &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			d := net.Dialer{Resolver: via}
			return d.DialContext(ctx, network, server)
		},
	}
}

func (p *prober) run(ctx context.Context, c CheckConfig) Result {
	ctx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()
	if c.Type == "dns" {
		return p.dns(ctx, c)
	}
	return p.http(ctx, c)
}

func (p *prober) http(ctx context.Context, c CheckConfig) Result {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.URL, nil)
	if err != nil {
		return Result{Detail: err.Error()}
	}
	req.Header.Set("User-Agent", "uptime-wisp/1 (+https://wisp.mera.network)")
	start := time.Now()
	resp, err := p.client.Do(req)
	lat := time.Since(start)
	if err != nil {
		return Result{Detail: describe(err), Latency: lat}
	}
	resp.Body.Close()
	r := Result{Latency: lat, OK: slices.Contains(c.ExpectStatus, resp.StatusCode)}
	if resp.TLS != nil && len(resp.TLS.PeerCertificates) > 0 {
		r.CertExpiry = resp.TLS.PeerCertificates[0].NotAfter
	}
	if r.OK {
		r.Detail = fmt.Sprint(resp.StatusCode)
	} else {
		r.Detail = fmt.Sprintf("status %d (want %s)", resp.StatusCode, joinInts(c.ExpectStatus))
	}
	return r
}

func (p *prober) dns(ctx context.Context, c CheckConfig) Result {
	// The nameserver's own name is found through the external resolver,
	// never the host's.
	res := resolverAt(c.Server, p.resolver)
	network := "ip4"
	if c.RecordType == "AAAA" {
		network = "ip6"
	}
	start := time.Now()
	addrs, err := res.LookupNetIP(ctx, network, c.Host)
	lat := time.Since(start)
	if err != nil {
		return Result{Detail: describe(err), Latency: lat}
	}
	got := make([]string, 0, len(addrs))
	for _, a := range addrs {
		got = append(got, a.Unmap().String())
	}
	slices.Sort(got)
	got = slices.Compact(got)
	if len(c.Expect) == 0 {
		return Result{OK: len(got) > 0, Detail: strings.Join(got, ", "), Latency: lat}
	}
	want := make([]string, 0, len(c.Expect))
	for _, e := range c.Expect {
		if a, err := netip.ParseAddr(e); err == nil {
			want = append(want, a.Unmap().String())
		}
	}
	slices.Sort(want)
	if slices.Equal(got, want) {
		return Result{OK: true, Detail: strings.Join(got, ", "), Latency: lat}
	}
	return Result{Detail: fmt.Sprintf("got %s (want %s)", orNone(got), strings.Join(want, ", ")), Latency: lat}
}

var resolvConfServer = regexp.MustCompile(` on [^ ]+:\d+`)

// describe turns transport errors into short, stable messages: the same
// failure should read the same way every round.
func describe(err error) string {
	var dnsErr *net.DNSError
	var certErr *tls.CertificateVerificationError
	var opErr *net.OpError
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.As(err, &dnsErr):
		if dnsErr.IsNotFound {
			return "dns: no such host (" + dnsErr.Name + ")"
		}
		if dnsErr.IsTimeout {
			return "dns: timeout (" + dnsErr.Name + ")"
		}
		// Go names the resolv.conf server ("on 192.168.1.1:53") even when
		// a custom Dial sent the query elsewhere; drop it, it's misleading.
		return "dns: " + resolvConfServer.ReplaceAllString(dnsErr.Err, "")
	case errors.As(err, &certErr):
		return "tls: certificate not valid: " + certErr.Err.Error()
	case errors.As(err, &opErr) && opErr.Op == "dial":
		return "connect: " + opErr.Err.Error()
	}
	return err.Error()
}

func joinInts(v []int) string {
	s := make([]string, len(v))
	for i, n := range v {
		s[i] = fmt.Sprint(n)
	}
	return strings.Join(s, "/")
}

func orNone(v []string) string {
	if len(v) == 0 {
		return "no records"
	}
	return strings.Join(v, ", ")
}
