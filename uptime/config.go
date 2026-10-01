// Package uptime is wisp's lightweight external uptime prober (P001's
// health-monitoring item): it actively checks products' public HTTP(S)
// endpoints and DNS answers from a vantage point outside the machines it
// watches, and alerts on state changes through a third-party channel.
//
// Standard library only. Every HTTP check resolves names itself through
// a configured external resolver — never /etc/hosts, a local cache or
// Docker's embedded DNS — so "the domain resolves" is part of what's
// checked.
package uptime

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"time"
)

// Defaults for zero-valued Config fields.
const (
	DefaultInterval            = 60 * time.Second
	DefaultTimeout             = 10 * time.Second
	DefaultFailuresBeforeAlert = 2
	DefaultCertWarnDays        = 14
	DefaultResolver            = "1.1.1.1:53"
)

// Duration is a time.Duration that unmarshals from "60s"-style strings.
type Duration struct{ time.Duration }

func (d *Duration) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return fmt.Errorf("duration must be a string like \"60s\"")
	}
	v, err := time.ParseDuration(s)
	if err != nil {
		return err
	}
	d.Duration = v
	return nil
}

func (d Duration) MarshalJSON() ([]byte, error) { return json.Marshal(d.String()) }

// Config is the whole prober configuration (one JSON file).
type Config struct {
	Interval            Duration      `json:"interval"`              // between rounds; default 60s
	Timeout             Duration      `json:"timeout"`               // per check; default 10s
	FailuresBeforeAlert int           `json:"failures_before_alert"` // consecutive failures before "down"; default 2
	CertWarnDays        int           `json:"cert_warn_days"`        // warn when a TLS cert expires sooner; default 14
	Resolver            string        `json:"resolver"`              // DNS server (host:port) for HTTP checks; default 1.1.1.1:53
	Listen              string        `json:"listen"`                // status page + /healthz, e.g. ":8080"; "" disables
	Heartbeat           *Heartbeat    `json:"heartbeat"`
	Alerts              []AlertConfig `json:"alerts"`
	Checks              []CheckConfig `json:"checks"`
}

// Heartbeat, if set, is pinged (GET) after every completed round — for a
// third-party dead-man's-switch service (e.g. healthchecks.io) that
// alerts when the pings stop, i.e. when this prober or its host dies.
type Heartbeat struct {
	URL string `json:"url"`
}

// AlertConfig is one notification channel.
type AlertConfig struct {
	Type  string `json:"type"`  // "ntfy" | "webhook"
	URL   string `json:"url"`   // ntfy: the full topic URL; webhook: the endpoint
	Token string `json:"token"` // optional: sent as "Authorization: Bearer <token>"
}

// CheckConfig is one thing to probe.
type CheckConfig struct {
	Name string `json:"name"`
	Type string `json:"type"` // "http" | "dns"

	// http: GET URL; up when the first response's status is in
	// ExpectStatus (default [200]). Redirects are not followed — a 301 is
	// a 301 — so list it if that's what "up" looks like.
	URL          string `json:"url,omitempty"`
	ExpectStatus []int  `json:"expect_status,omitempty"`

	// dns: ask Server (host[:port], default port 53) for Host's A records
	// (or AAAA with RecordType "AAAA"); up when the answer set equals
	// Expect, or is non-empty when Expect is empty.
	Host       string   `json:"host,omitempty"`
	Server     string   `json:"server,omitempty"`
	RecordType string   `json:"record_type,omitempty"`
	Expect     []string `json:"expect,omitempty"`
}

// Target is a one-line description for logs and the status page.
func (c CheckConfig) Target() string {
	if c.Type == "dns" {
		return fmt.Sprintf("%s %s @%s", c.RecordType, c.Host, c.Server)
	}
	return c.URL
}

// Load reads and validates a config file.
func Load(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(b)
}

// Parse validates config JSON and fills defaults.
func Parse(b []byte) (*Config, error) {
	var c Config
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields() // a typo'd key fails loudly instead of being ignored
	if err := dec.Decode(&c); err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}

	if c.Interval.Duration == 0 {
		c.Interval.Duration = DefaultInterval
	}
	if c.Timeout.Duration == 0 {
		c.Timeout.Duration = DefaultTimeout
	}
	if c.FailuresBeforeAlert == 0 {
		c.FailuresBeforeAlert = DefaultFailuresBeforeAlert
	}
	if c.CertWarnDays == 0 {
		c.CertWarnDays = DefaultCertWarnDays
	}
	if c.Resolver == "" {
		c.Resolver = DefaultResolver
	}

	var errs []error
	if c.Interval.Duration < 5*time.Second {
		errs = append(errs, errors.New("interval must be at least 5s"))
	}
	if c.Timeout.Duration <= 0 || c.Timeout.Duration >= c.Interval.Duration {
		errs = append(errs, errors.New("timeout must be positive and shorter than interval"))
	}
	if c.FailuresBeforeAlert < 1 {
		errs = append(errs, errors.New("failures_before_alert must be at least 1"))
	}
	if _, _, err := net.SplitHostPort(c.Resolver); err != nil {
		errs = append(errs, fmt.Errorf("resolver must be host:port: %v", err))
	}
	if c.Heartbeat != nil && !httpURL(c.Heartbeat.URL) {
		errs = append(errs, errors.New("heartbeat.url must be an http(s) URL"))
	}
	if len(c.Alerts) == 0 {
		errs = append(errs, errors.New("at least one alert channel is required — an uptime monitor nobody hears is pointless"))
	}
	for i, a := range c.Alerts {
		if a.Type != "ntfy" && a.Type != "webhook" {
			errs = append(errs, fmt.Errorf("alerts[%d]: type must be \"ntfy\" or \"webhook\"", i))
		}
		if !httpURL(a.URL) {
			errs = append(errs, fmt.Errorf("alerts[%d]: url must be an http(s) URL", i))
		}
	}
	if len(c.Checks) == 0 {
		errs = append(errs, errors.New("no checks configured"))
	}
	seen := map[string]bool{}
	for i := range c.Checks {
		ck := &c.Checks[i]
		if ck.Name == "" {
			errs = append(errs, fmt.Errorf("checks[%d]: name is required", i))
		} else if seen[ck.Name] {
			errs = append(errs, fmt.Errorf("checks[%d]: duplicate name %q", i, ck.Name))
		}
		seen[ck.Name] = true
		switch ck.Type {
		case "http":
			if !httpURL(ck.URL) {
				errs = append(errs, fmt.Errorf("check %q: url must be an http(s) URL", ck.Name))
			}
			if len(ck.ExpectStatus) == 0 {
				ck.ExpectStatus = []int{200}
			}
		case "dns":
			if ck.Host == "" || ck.Server == "" {
				errs = append(errs, fmt.Errorf("check %q: dns needs host and server", ck.Name))
			}
			if _, _, err := net.SplitHostPort(ck.Server); err != nil {
				ck.Server = net.JoinHostPort(ck.Server, "53")
			}
			if ck.RecordType == "" {
				ck.RecordType = "A"
			}
			if ck.RecordType != "A" && ck.RecordType != "AAAA" {
				errs = append(errs, fmt.Errorf("check %q: record_type must be A or AAAA", ck.Name))
			}
			for _, e := range ck.Expect {
				if net.ParseIP(e) == nil {
					errs = append(errs, fmt.Errorf("check %q: expect %q is not an IP address", ck.Name, e))
				}
			}
		default:
			errs = append(errs, fmt.Errorf("check %q: type must be \"http\" or \"dns\"", ck.Name))
		}
	}
	if err := errors.Join(errs...); err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	return &c, nil
}

func httpURL(s string) bool {
	u, err := url.Parse(s)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}
