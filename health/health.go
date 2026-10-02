// Package health is health-wisp: a tiny per-machine reporter of disk,
// memory and load, polled by uptime-wisp's "host" check. One container
// per machine; it never makes an outbound request and never decides
// what "healthy" means — thresholds live in uptime-wisp's config.
//
// What it reports is deliberately coarse, so the endpoint is useless as
// a side channel: totals only (no process or container names), whole
// percentages, and load as the kernel's 15-minute average, which a
// prober can't watch its own requests move. Every request is served
// from a sample taken by a background loop, so requests cost nothing
// and one global rate limit is enough — no per-client state, and no
// client address is ever looked at or logged.
//
// Standard library only.
package health

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Report is the JSON health-wisp serves.
type Report struct {
	Host       string         `json:"host"`
	DiskPct    map[string]int `json:"disk_pct"`     // disk name → used %, rounded up like df's Use%
	MemPct     int            `json:"mem_pct"`      // (MemTotal − MemAvailable) / MemTotal, whole %
	LoadPerCPU float64        `json:"load_per_cpu"` // 15-minute load average ÷ CPUs, to 0.05
	Sampled    time.Time      `json:"sampled"`
}

// Disk is one filesystem to report: any path on it works (statfs
// reports the filesystem the path lives on), so a container needs only
// a read-only bind of one small directory, never the host's root.
type Disk struct {
	Name, Path string
}

// Sampler reads the machine's numbers.
type Sampler struct {
	Host  string
	Disks []Disk
	Proc  string // procfs root; "/proc" in production (host-wide values, even in a container)
}

// Sample takes one reading.
func (s *Sampler) Sample(now time.Time) (Report, error) {
	r := Report{Host: s.Host, DiskPct: map[string]int{}, Sampled: now.UTC().Truncate(time.Second)}
	var errs []error
	for _, d := range s.Disks {
		pct, err := diskPct(d.Path)
		if err != nil {
			errs = append(errs, fmt.Errorf("disk %s: %w", d.Name, err))
			continue
		}
		r.DiskPct[d.Name] = pct
	}
	mem, err := memPct(filepath.Join(s.Proc, "meminfo"))
	if err != nil {
		errs = append(errs, err)
	}
	r.MemPct = mem
	load, err := load15(filepath.Join(s.Proc, "loadavg"))
	if err != nil {
		errs = append(errs, err)
	}
	cpus := cpuCount(filepath.Join(s.Proc, "stat"))
	r.LoadPerCPU = math.Round(load/float64(cpus)*20) / 20
	return r, errors.Join(errs...)
}

// diskPct is df's Use%: used / (used + available to unprivileged
// users), rounded up — so a disk is never reported as less full than
// df says.
func diskPct(path string) (int, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, err
	}
	used := st.Blocks - st.Bfree
	total := used + st.Bavail
	if total == 0 {
		return 0, errors.New("empty filesystem")
	}
	return int((used*100 + total - 1) / total), nil
}

func memPct(path string) (int, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	var total, avail float64
	sc := bufio.NewScanner(bytes.NewReader(b))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) < 2 {
			continue
		}
		v, _ := strconv.ParseFloat(f[1], 64)
		switch f[0] {
		case "MemTotal:":
			total = v
		case "MemAvailable:":
			avail = v
		}
	}
	if total == 0 {
		return 0, errors.New("meminfo: no MemTotal")
	}
	return int(math.Round((total - avail) / total * 100)), nil
}

func load15(path string) (float64, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	f := strings.Fields(string(b))
	if len(f) < 3 {
		return 0, errors.New("loadavg: malformed")
	}
	return strconv.ParseFloat(f[2], 64)
}

// cpuCount counts the host's CPUs from /proc/stat (the load average is
// host-wide, so divide by the host's CPUs, not the container's
// affinity); falls back to runtime.NumCPU.
func cpuCount(path string) int {
	b, err := os.ReadFile(path)
	n := 0
	if err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			if strings.HasPrefix(line, "cpu") && len(line) > 3 && line[3] >= '0' && line[3] <= '9' {
				n++
			}
		}
	}
	if n == 0 {
		n = runtime.NumCPU()
	}
	return n
}

// Server samples on an interval and serves the latest report.
type Server struct {
	Sampler     *Sampler
	Interval    time.Duration
	TokenSHA256 []byte // required: a request must carry "Authorization: Bearer <token>"
	Now         func() time.Time
	Logf        func(format string, args ...any)

	mu      sync.Mutex
	body    []byte // the latest report, marshalled
	sampled time.Time
	lastErr string
	limit   *bucket
}

// HashToken returns the hex SHA-256 stored as HEALTH_TOKEN_SHA256.
func HashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

// Refresh takes a sample now. A partial failure (one disk unreadable)
// still publishes the rest; the error is logged once per change.
func (s *Server) Refresh() {
	now := s.now()
	r, err := s.Sampler.Sample(now)
	body, _ := json.Marshal(r)
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.body, s.sampled = body, now
	if msg != s.lastErr && s.Logf != nil {
		if msg != "" {
			s.Logf("health-wisp: sample: %s", msg)
		} else {
			s.Logf("health-wisp: sample ok again")
		}
	}
	s.lastErr = msg
}

// Run samples every Interval until stop is closed.
func (s *Server) Run(stop <-chan struct{}) {
	s.Refresh()
	t := time.NewTicker(s.Interval)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			s.Refresh()
		}
	}
}

func (s *Server) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// fresh returns the latest report, or nil if there's none or the
// sampler has stalled (three intervals without a sample).
func (s *Server) fresh() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.body == nil || s.now().Sub(s.sampled) > 3*s.Interval {
		return nil
	}
	return s.body
}

// Handler serves GET / (the report, token required) and GET /healthz
// (open; says only "ok", for Docker's health check). Both go through
// one global rate limit, before the token is even looked at, so
// guessing tokens is as slow as everything else.
func (s *Server) Handler() http.Handler {
	s.limit = newBucket(2, 20, s.now) // 2 req/s sustained, bursts of 20
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		if !s.authorized(r) {
			w.Header().Set("WWW-Authenticate", `Bearer realm="health-wisp"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		body := s.fresh()
		if body == nil {
			http.Error(w, "no recent sample", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.Write(body)
	})
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		if s.fresh() == nil {
			http.Error(w, "no recent sample", http.StatusServiceUnavailable)
			return
		}
		w.Write([]byte("ok\n"))
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.limit.take() {
			w.Header().Set("Retry-After", "1")
			http.Error(w, "rate limited", http.StatusTooManyRequests)
			return
		}
		mux.ServeHTTP(w, r)
	})
}

func (s *Server) authorized(r *http.Request) bool {
	tok, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || len(s.TokenSHA256) != sha256.Size {
		return false
	}
	got := sha256.Sum256([]byte(tok))
	return subtle.ConstantTimeCompare(got[:], s.TokenSHA256) == 1
}

// bucket is a global token bucket: rate tokens per second, up to burst.
type bucket struct {
	mu          sync.Mutex
	rate, burst float64
	tokens      float64
	last        time.Time
	now         func() time.Time
}

func newBucket(rate, burst float64, now func() time.Time) *bucket {
	return &bucket{rate: rate, burst: burst, tokens: burst, last: now(), now: now}
}

func (b *bucket) take() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	t := b.now()
	b.tokens = math.Min(b.burst, b.tokens+t.Sub(b.last).Seconds()*b.rate)
	b.last = t
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}
