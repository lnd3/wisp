package health

import (
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fakeProc(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	write := func(name, s string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("meminfo", "MemTotal:        4000000 kB\nMemFree:          100000 kB\nMemAvailable:    3000000 kB\n")
	write("loadavg", "3.10 2.40 1.37 2/345 6789\n")
	write("stat", "cpu  1 2 3\ncpu0 1 2 3\ncpu1 1 2 3\nintr 5\n")
	return dir
}

func TestSample(t *testing.T) {
	s := &Sampler{Host: "h", Proc: fakeProc(t), Disks: []Disk{{"root", t.TempDir()}}}
	r, err := s.Sample(time.Unix(1000, 500))
	if err != nil {
		t.Fatal(err)
	}
	if r.MemPct != 25 {
		t.Errorf("mem = %d, want 25", r.MemPct)
	}
	// 1.37 / 2 CPUs = 0.685 → nearest 0.05 = 0.70
	if r.LoadPerCPU != 0.7 {
		t.Errorf("load = %v, want 0.7", r.LoadPerCPU)
	}
	if p, ok := r.DiskPct["root"]; !ok || p < 0 || p > 100 {
		t.Errorf("disk = %v", r.DiskPct)
	}
	if r.Host != "h" || !r.Sampled.Equal(time.Unix(1000, 0)) {
		t.Errorf("host/sampled = %q %v", r.Host, r.Sampled)
	}
}

func TestSampleBadDiskKeepsTheRest(t *testing.T) {
	s := &Sampler{Proc: fakeProc(t), Disks: []Disk{{"gone", "/does/not/exist"}}}
	r, err := s.Sample(time.Now())
	if err == nil || !strings.Contains(err.Error(), "disk gone") {
		t.Fatalf("err = %v", err)
	}
	if r.MemPct != 25 {
		t.Errorf("mem not reported alongside the failed disk")
	}
}

func newServer(t *testing.T, now *time.Time) (*Server, http.Handler) {
	b, _ := hex.DecodeString(HashToken("the-token"))
	s := &Server{
		Sampler:     &Sampler{Host: "h", Proc: fakeProc(t)},
		Interval:    30 * time.Second,
		TokenSHA256: b,
		Now:         func() time.Time { return *now },
	}
	return s, s.Handler()
}

func get(h http.Handler, path, auth string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("GET", path, nil)
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

func TestServerAuthAndStaleness(t *testing.T) {
	now := time.Unix(10000, 0)
	s, h := newServer(t, &now)

	if w := get(h, "/", "Bearer the-token"); w.Code != 503 {
		t.Errorf("before first sample: %d, want 503", w.Code)
	}
	s.Refresh()
	for _, auth := range []string{"", "Bearer wrong", "the-token", "Basic dGhlLXRva2Vu"} {
		if w := get(h, "/", auth); w.Code != 401 {
			t.Errorf("auth %q: %d, want 401", auth, w.Code)
		}
	}
	w := get(h, "/", "Bearer the-token")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"mem_pct":25`) {
		t.Errorf("authorized: %d %s", w.Code, w.Body)
	}
	if w := get(h, "/healthz", ""); w.Code != 200 {
		t.Errorf("healthz: %d", w.Code)
	}
	now = now.Add(91 * time.Second) // more than 3 intervals without a sample
	if w := get(h, "/", "Bearer the-token"); w.Code != 503 {
		t.Errorf("stale: %d, want 503", w.Code)
	}
	if w := get(h, "/healthz", ""); w.Code != 503 {
		t.Errorf("stale healthz: %d, want 503", w.Code)
	}
}

func TestServerRateLimit(t *testing.T) {
	now := time.Unix(10000, 0)
	s, h := newServer(t, &now)
	s.Refresh()
	ok := 0
	for range 50 {
		if get(h, "/healthz", "").Code == 200 {
			ok++
		}
	}
	if ok != 20 {
		t.Errorf("burst allowed %d, want 20", ok)
	}
	if w := get(h, "/", "Bearer the-token"); w.Code != 429 {
		t.Errorf("over the limit: %d, want 429 (before auth)", w.Code)
	}
	now = now.Add(time.Second)
	if get(h, "/healthz", "").Code != 200 || get(h, "/healthz", "").Code != 200 || get(h, "/healthz", "").Code != 429 {
		t.Error("want 2 requests per second after the burst")
	}
}
