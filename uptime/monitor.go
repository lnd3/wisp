package uptime

import (
	"context"
	"crypto/x509"
	"fmt"
	"log"
	"net/http"
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

	// check is the probe function; tests replace it.
	check func(ctx context.Context, c CheckConfig) Result

	mu        sync.Mutex
	states    []*State
	lastRound time.Time
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
		cfg:      cfg,
		prober:   newProber(cfg, o.Roots),
		hbClient: &http.Client{Timeout: cfg.Timeout.Duration},
		log:      o.Log,
		now:      o.Now,
		backoff:  []time.Duration{2 * time.Second, 10 * time.Second},
	}
	m.check = m.prober.run
	alertClient := &http.Client{Timeout: 15 * time.Second}
	for _, a := range cfg.Alerts {
		m.alerters = append(m.alerters, newAlerter(a, alertClient))
	}
	for _, c := range cfg.Checks {
		m.states = append(m.states, &State{Name: c.Name, Type: c.Type, Target: c.Target(), Status: StatusUnknown})
	}
	return m
}

// Run checks every interval until ctx ends. The first round starts
// immediately.
func (m *Monitor) Run(ctx context.Context) {
	m.log.Printf("uptime-wisp: %d checks every %s, alerting via %s", len(m.cfg.Checks), m.cfg.Interval.Duration, m.channels())
	t := time.NewTicker(m.cfg.Interval.Duration)
	defer t.Stop()
	for {
		m.Round(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// Probe runs every check concurrently and returns the results in
// config order, without touching state or alerting.
func (m *Monitor) Probe(ctx context.Context) []Result {
	results := make([]Result, len(m.cfg.Checks))
	var wg sync.WaitGroup
	for i, c := range m.cfg.Checks {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i] = m.check(ctx, c)
		}()
	}
	wg.Wait()
	return results
}

// Round runs every check, applies the results, sends any alerts, and
// pings the heartbeat.
func (m *Monitor) Round(ctx context.Context) {
	results := m.Probe(ctx)
	if ctx.Err() != nil {
		return
	}
	alerts := m.apply(results)
	for _, a := range alerts {
		m.send(ctx, a)
	}
	m.heartbeat(ctx)
}

// apply folds one round of results into the states and returns the
// alerts that state changes call for.
func (m *Monitor) apply(results []Result) []Alert {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now().UTC()
	m.lastRound = now
	var alerts []Alert
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
	for _, al := range m.alerters {
		if err := deliver(ctx, al, a, m.backoff); err != nil {
			m.log.Printf("alert via %s failed: %v", al, err)
		}
	}
}

// SendTest sends a test alert through every channel and reports the
// first failure, for `uptime-wisp -test-alert`.
func (m *Monitor) SendTest(ctx context.Context) error {
	a := Alert{Kind: KindTest, Time: m.now().UTC(), Title: "uptime-wisp test alert",
		Message: fmt.Sprintf("Test from uptime-wisp: %d checks configured. If you can read this, alerts work.", len(m.cfg.Checks))}
	for _, al := range m.alerters {
		if err := deliver(ctx, al, a, nil); err != nil {
			return fmt.Errorf("%s: %w", al, err)
		}
	}
	return nil
}

func (m *Monitor) heartbeat(ctx context.Context) {
	if m.cfg.Heartbeat == nil {
		return
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, m.cfg.Heartbeat.URL, nil)
	if err != nil {
		return
	}
	resp, err := m.hbClient.Do(req)
	if err != nil {
		m.log.Printf("heartbeat failed: %v", err)
		return
	}
	resp.Body.Close()
	if resp.StatusCode > 299 {
		m.log.Printf("heartbeat failed: HTTP %d", resp.StatusCode)
	}
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
