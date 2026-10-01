package site

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakeViewer struct{ keys []string }

func (f *fakeViewer) View(_ *http.Request, k string) { f.keys = append(f.keys, k) }

func TestIndexCountsOnlyServedGETs(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "index.html"), []byte("<h1>wisp</h1>"), 0o644)
	v := &fakeViewer{}
	mux := http.NewServeMux()
	(&Handler{Dir: dir, Viewer: v}).Register(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	for _, tc := range []struct {
		method, path string
		status       int
	}{
		{"GET", "/", 200},
		{"HEAD", "/", 200},
		{"GET", "/index.html", 404},
		{"GET", "/other", 404},
		{"POST", "/", 405},
	} {
		req, _ := http.NewRequest(tc.method, srv.URL+tc.path, nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != tc.status {
			t.Errorf("%s %s = %d, want %d", tc.method, tc.path, resp.StatusCode, tc.status)
		}
	}
	if strings.Join(v.keys, ",") != "/" {
		t.Errorf("views = %v, want exactly one GET / (no HEAD, no 404s)", v.keys)
	}
}

func TestMissingIndexIsNotCounted(t *testing.T) {
	v := &fakeViewer{}
	mux := http.NewServeMux()
	(&Handler{Dir: t.TempDir(), Viewer: v}).Register(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != 404 || len(v.keys) != 0 {
		t.Errorf("got %d, views %v", rec.Code, v.keys)
	}
}

func TestTrustedClientIP(t *testing.T) {
	trusted, err := ParsePrefixes("172.27.11.0/24, ")
	if err != nil {
		t.Fatal(err)
	}
	ip := TrustedClientIP(trusted)
	for _, tc := range []struct{ remote, header, want string }{
		{"172.27.11.3:5000", "203.0.113.7", "203.0.113.7"},   // from the proxy: trust the header
		{"172.27.11.3:5000", "", "172.27.11.3"},              // proxy without header: the peer
		{"198.51.100.9:5000", "203.0.113.7", "198.51.100.9"}, // not the proxy: ignore a spoofed header
		{"[::ffff:172.27.11.3]:5000", "203.0.113.7", "203.0.113.7"},
	} {
		r := httptest.NewRequest("GET", "/", nil)
		r.RemoteAddr = tc.remote
		if tc.header != "" {
			r.Header.Set("X-Real-IP", tc.header)
		}
		if got := ip(r); got != tc.want {
			t.Errorf("remote %s header %q: got %q, want %q", tc.remote, tc.header, got, tc.want)
		}
	}
	if got := TrustedClientIP(nil)(func() *http.Request {
		r := httptest.NewRequest("GET", "/", nil)
		r.RemoteAddr = "172.27.11.3:1"
		r.Header.Set("X-Real-IP", "203.0.113.7")
		return r
	}()); got != "172.27.11.3" {
		t.Errorf("no trusted prefixes must never trust the header; got %q", got)
	}
	if _, err := ParsePrefixes("not-a-cidr"); err == nil {
		t.Error("bad CIDR accepted")
	}
}
