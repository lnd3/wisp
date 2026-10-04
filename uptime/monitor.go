package uptime

import (
	"context"
	"crypto/x509"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Status of a check.
const (
	StatusUnknown = "unknown" // not yet checked
	StatusUp      = "up"
	StatusDown    = "down"
)

// State is one check's current state, as shown on the status page.
type State struct {
	Name, Type, Target string
	Status             string
	Since              time.Time // when Status last changed
	LastCheck          time.Time
	Detail             string
	Latency            time.Duration
	Failures           int       // consecutive failed checks
	CertExpiry         time.Time // zero unless HTTPS
	certWarned         bool
}

// Monitor runs every check once per interval and alerts on changes.
type Monitor struct {
	cfg      *Config
	prober   *prober
	alerters []Alerter
	hbClient *http.Client
	log      *log.Logger
	now      func() time.Time
	backoff  []time.Duration // alert delivery retries

	roots *x509.CertPool

	// check is the probe function; tests replace it (and clear
	// checkIsProber so a reload doesn't swap it back).
	check         func(ctx context.Context, c CheckConfig) Result
	checkIsProber bool

	// roundMu is held for a whole round, and by a reload while it swaps
	// the config: a round's results always match the check list it
	// started with.
	roundMu sync.Mutex

	mu         sync.Mutex // guards cfg, prober, check, alerters, states and the fields below
	states     []*State
	lastRound  time.Time
	loader     func() (*Config, error)
	lastReload *ReloadResult
	reloaded   chan struct{} // tells Run to reset its ticker and run a round now
	conn       ConnState
}

// ConnState is the prober's own connectivity, from the last round's
// anchors (Config.Connectivity).
type ConnState struct {
	Enabled      bool
	AnchorsUp    int
	AnchorsTotal int
	OfflineSince time.Time // zero while online
	Detail       string    // the anchors' failures while offline
}

// ReloadResult is the outcome of the last config reload, shown on the
// status page.
type ReloadResult struct {
	Time    time.Time
	OK      bool
	Message string
}

// Options for New. All optional.
type Options struct {
	Log   *log.Logger
	Now   func() time.Time
	Roots *x509.CertPool // TLS roots for HTTP checks (tests); nil = system roots
}

// New builds a monitor for cfg.
func New(cfg *Config, o Options) *Monitor {
	if o.Log == nil {
		o.Log = log.Default()
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	m := &Monitor{
		log:           o.Log,
		now:           o.Now,
		roots:         o.Roots,
		backoff:       []time.Duration{2 * time.Second, 10 * time.Second},
		checkIsProber: true,
		reloaded:      make(chan struct{}, 1),
	}
	m.applyConfig(cfg)
	return m
}

// applyConfig installs cfg: a new prober, alert channels and check
// list. A check keeps its state (status, since, failure count) when its
// name, type and target are unchanged, so a reload never fires
// spurious alerts; changed or new checks start unknown. The caller must
// hold roundMu (or be New).
func (m *Monitor) applyConfig(cfg *Config) (added, removed, changed int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	old := map[string]*State{}
	for _, s := range m.states {
		old[s.Name] = s
	}
	var states []*State
	for _, c := range cfg.Checks {
		if s, ok := old[c.Name]; ok && s.Type == c.Type && s.Target == c.Target() {
			states = append(states, s)
			delete(old, c.Name)
			continue
		} else if ok {
			changed++
			delete(old, c.Name)
		} else if m.cfg != nil {
			added++
		}
		states = append(states, &State{Name: c.Name, Type: c.Type, Target: c.Target(), Status: StatusUnknown})
	}
	removed = len(old)

	m.cfg = cfg
	m.states = states
	m.conn.Enabled = !cfg.Connectivity.Disabled
	m.prober = newProber(cfg, m.roots)
	if m.checkIsProber {
		m.check = m.prober.run
	}
	m.hbClient = &http.Client{Timeout: cfg.Timeout.Duration}
	alertClient := &http.Client{Timeout: 15 * time.Second}
	m.alerters = nil
	for _, a := range cfg.Alerts {
		m.alerters = append(m.alerters, newAlerter(a, alertClient))
	}
	return added, removed, changed
}

// SetLoader sets how Reload obtains a new config (typically: re-read and
// validate the config file exactly as at startup).
func (m *Monitor) SetLoader(f func() (*Config, error)) {
	m.mu.Lock()
	m.loader = f
	m.mu.Unlock()
}

// Reload re-reads the config through the loader and applies it between
// rounds. An invalid config is rejected and the running one kept.
func (m *Monitor) Reload() ReloadResult {
	m.mu.Lock()
	loader := m.loader
	m.mu.Unlock()
	res := ReloadResult{Time: m.now().UTC()}
	if loader == nil {
		res.Message = "reload is not available"
		return m.recordReload(res)
	}
	cfg, err := loader()
	if err != nil {
		res.Message = "config NOT reloaded, still running the previous one: " + err.Error()
		m.log.Printf("reload rejected: %v", err)
		return m.recordReload(res)
	}
	m.roundMu.Lock() // wait for a running round to finish
	added, removed, changed := m.applyConfig(cfg)
	m.roundMu.Unlock()
	res.OK = true
	res.Message = fmt.Sprintf("config reloaded: %d checks (%d added, %d removed, %d changed)", len(cfg.Checks), added, removed, changed)
	m.log.Print(res.Message)
	select {
	case m.reloaded <- struct{}{}:
	default:
	}
	return m.recordReload(res)
}

func (m *Monitor) recordReload(r ReloadResult) ReloadResult {
	m.mu.Lock()
	m.lastReload = &r
	m.mu.Unlock()
	return r
}

// config returns the current config.
func (m *Monitor) config() *Config {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cfg
}

// Run checks every interval until ctx ends. The first round starts
// immediately.
func (m *Monitor) Run(ctx context.Context) {
	cfg := m.config()
	m.log.Printf("uptime-wisp: %d checks every %s, alerting via %s", len(cfg.Checks), cfg.Interval.Duration, m.channels())
	t := time.NewTicker(cfg.Interval.Duration)
	defer t.Stop()
	for {
		m.Round(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-m.reloaded:
			// A reload: run a round now (so new checks show at once)
			// and restart the ticker at the possibly new interval.
			t.Reset(m.config().Interval.Duration)
		}
	}
}

// Probe runs every check and connectivity anchor concurrently and
// returns their results in config order, without touching state or
// alerting.
func (m *Monitor) Probe(ctx context.Context) (checks, anchors []Result) {
	m.mu.Lock()
	cks, anc, check := m.cfg.Checks, m.anchors(), m.check
	m.mu.Unlock()
	all := append(append([]CheckConfig{}, cks...), anc...)
	results := make([]Result, len(all))
	var wg sync.WaitGroup
	for i, c := range all {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i] = check(ctx, c)
		}()
	}
	wg.Wait()
	return results[:len(cks)], results[len(cks):]
}

// Anchors returns the connectivity anchors (none when the gate is off).
func (m *Monitor) Anchors() []CheckConfig {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.anchors()
}

func (m *Monitor) anchors() []CheckConfig {
	if m.cfg.Connectivity.Disabled {
		return nil
	}
	return m.cfg.Connectivity.Anchors
}

// Round runs every check, applies the results, sends any alerts, and
// pings the heartbeat. A reload waits for it to finish.
func (m *Monitor) Round(ctx context.Context) {
	m.roundMu.Lock()
	defer m.roundMu.Unlock()
	results, anchors := m.Probe(ctx)
	if ctx.Err() != nil {
		return
	}
	alerts := group(m.apply(results, anchors))
	for _, a := range alerts {
		m.send(ctx, a)
	}
	m.heartbeat(ctx)
}

// apply folds one round of results into the states and returns the
// alerts that state changes call for. When every anchor failed, the
// prober itself is offline: the round judges no check (failure counts
// and states stay as they were) and alerts nothing.
func (m *Monitor) apply(results, anchors []Result) []Alert {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now().UTC()
	m.lastRound = now
	var alerts []Alert

	if len(anchors) > 0 {
		up := 0
		var failed []string
		for i, r := range anchors {
			if r.OK {
				up++
			} else {
				failed = append(failed, m.cfg.Connectivity.Anchors[i].Name+": "+r.Detail)
			}
		}
		m.conn.AnchorsUp, m.conn.AnchorsTotal = up, len(anchors)
		if up == 0 {
			if m.conn.OfflineSince.IsZero() {
				m.conn.OfflineSince = now
				m.log.Printf("connectivity lost: all %d anchors failed (%s); not judging checks until one answers", len(anchors), strings.Join(failed, "; "))
			}
			m.conn.Detail = strings.Join(failed, "; ")
			return nil
		}
		if since := m.conn.OfflineSince; !since.IsZero() {
			off := now.Sub(since)
			m.log.Printf("connectivity back after %s", roundDur(off))
			if off >= m.cfg.Connectivity.ReportAfter.Duration {
				alerts = append(alerts, Alert{Kind: KindProber, Time: now,
					Title: "uptime-wisp was offline for " + roundDur(off),
					Message: fmt.Sprintf("uptime-wisp lost its own connectivity from %s to %s (every connectivity anchor failed), so no check was judged in that time. Its own uplink, not the targets.",
						since.Format("15:04 UTC"), now.Format("15:04 UTC"))})
			}
			m.conn.OfflineSince, m.conn.Detail = time.Time{}, ""
		}
	}

	for i, r := range results {
		s := m.states[i]
		s.LastCheck, s.Detail, s.Latency, s.CertExpiry = now, r.Detail, r.Latency, r.CertExpiry

		if r.OK {
			s.Failures = 0
			if s.Status == StatusDown {
				alerts = append(alerts, Alert{Check: s.Name, Kind: KindUp, Time: now,
					Title:   s.Name + " is back up",
					Message: fmt.Sprintf("%s (%s) recovered after %s: %s", s.Name, s.Target, roundDur(now.Sub(s.Since)), r.Detail)})
			}
			if s.Status != StatusUp {
				s.Status, s.Since = StatusUp, now
			}
		} else {
			s.Failures++
			if s.Failures >= m.cfg.FailuresBeforeAlert && s.Status != StatusDown {
				if s.Status == StatusUnknown {
					s.Since = now
				}
				alerts = append(alerts, Alert{Check: s.Name, Kind: KindDown, Time: now,
					Title:   s.Name + " is DOWN",
					Message: fmt.Sprintf("%s (%s) failed %d checks in a row: %s", s.Name, s.Target, s.Failures, r.Detail)})
				s.Status, s.Since = StatusDown, now
			}
		}

		// Certificate expiry: warn once when it enters the window; re-arm
		// once a renewed certificate leaves it.
		if !r.CertExpiry.IsZero() {
			left := r.CertExpiry.Sub(now)
			if left < time.Duration(m.cfg.CertWarnDays)*24*time.Hour {
				if !s.certWarned {
					s.certWarned = true
					alerts = append(alerts, Alert{Check: s.Name, Kind: KindCert, Time: now,
						Title:   s.Name + ": TLS certificate expires soon",
						Message: fmt.Sprintf("%s (%s): certificate expires in %s (%s) — check the ACME renewal", s.Name, s.Target, roundDur(left), r.CertExpiry.UTC().Format("2006-01-02 15:04 UTC"))})
				}
			} else {
				s.certWarned = false
			}
		}
	}
	return alerts
}

func (m *Monitor) send(ctx context.Context, a Alert) {
	m.log.Printf("alert [%s] %s", a.Kind, a.Message)
	m.mu.Lock()
	alerters := m.alerters
	m.mu.Unlock()
	for _, al := range alerters {
		if err := deliver(ctx, al, a, m.backoff); err != nil {
			m.log.Printf("alert via %s failed: %v", al, err)
		}
	}
}

// SendTest sends a test alert through every channel and reports the
// first failure, for `uptime-wisp -test-alert`.
func (m *Monitor) SendTest(ctx context.Context) error {
	cfg := m.config()
	a := Alert{Kind: KindTest, Time: m.now().UTC(), Title: "uptime-wisp test alert",
		Message: fmt.Sprintf("Test from uptime-wisp: %d checks configured. If you can read this, alerts work.", len(cfg.Checks))}
	for _, al := range m.alerters {
		if err := deliver(ctx, al, a, nil); err != nil {
			return fmt.Errorf("%s: %w", al, err)
		}
	}
	return nil
}

func (m *Monitor) heartbeat(ctx context.Context) {
	m.mu.Lock()
	hb, client := m.cfg.Heartbeat, m.hbClient
	m.mu.Unlock()
	if hb == nil {
		return
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, hb.URL, nil)
	if err != nil {
		return
	}
	resp, err := client.Do(req)
	if err != nil {
		m.log.Printf("heartbeat failed: %v", err)
		return
	}
	resp.Body.Close()
	if resp.StatusCode > 299 {
		m.log.Printf("heartbeat failed: HTTP %d", resp.StatusCode)
	}
}

// Conn returns the prober's own connectivity state.
func (m *Monitor) Conn() ConnState {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.conn
}

// groupFrom is how many same-kind state changes in one round get folded
// into a single alert: a shared cause (a server, its network) shouldn't
// arrive as a wall of separate notifications.
const groupFrom = 3

// group folds a round's down (and up) alerts into one alert per kind
// when there are at least groupFrom of them. Others pass unchanged.
func group(alerts []Alert) []Alert {
	byKind := map[string][]Alert{}
	for _, a := range alerts {
		byKind[a.Kind] = append(byKind[a.Kind], a)
	}
	var out []Alert
	done := map[string]bool{}
	for _, a := range alerts {
		same := byKind[a.Kind]
		if (a.Kind != KindDown && a.Kind != KindUp) || len(same) < groupFrom {
			out = append(out, a)
			continue
		}
		if done[a.Kind] {
			continue
		}
		done[a.Kind] = true
		names := make([]string, len(same))
		lines := make([]string, len(same))
		for i, x := range same {
			names[i], lines[i] = x.Check, x.Message
		}
		title := fmt.Sprintf("%d checks DOWN", len(same))
		if a.Kind == KindUp {
			title = fmt.Sprintf("%d checks back up", len(same))
		}
		out = append(out, Alert{Check: strings.Join(names, ", "), Kind: a.Kind, Time: a.Time,
			Title: title, Message: strings.Join(lines, "\n")})
	}
	return out
}

// Snapshot returns copies of the current states and the time of the
// last completed round.
func (m *Monitor) Snapshot() ([]State, time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]State, len(m.states))
	for i, s := range m.states {
		out[i] = *s
	}
	return out, m.lastRound
}

func (m *Monitor) channels() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := ""
	for i, a := range m.alerters {
		if i > 0 {
			s += ", "
		}
		s += a.String()
	}
	return s
}

func roundDur(d time.Duration) string {
	switch {
	case d < time.Minute:
		return d.Round(time.Second).String()
	case d < 48*time.Hour:
		return d.Round(time.Minute).String()
	default:
		return fmt.Sprintf("%d days", int(d.Hours()/24))
	}
}
