// Package site serves wisp's own public landing page and counts its
// views with wisp's own hook — wisp as a product of itself, integrated
// exactly the way cinder, offgrid & co. are told to (see their plans'
// wisp-integration actions): a View call in the handler that served the
// page, the client IP read from the proxy's X-Real-IP only when the
// request really came from that proxy, nothing logged.
//
// Only the landing page is counted. The dashboard is operator traffic
// and the ingest API is machine traffic; neither goes through here.
package site

import (
	"net"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
)

// Viewer is the slice of *hook.Hook this package uses (a fake in tests).
type Viewer interface {
	View(r *http.Request, pageKey string)
}

// Handler serves <Dir>/index.html at exactly "/".
type Handler struct {
	Dir    string
	Viewer Viewer // may be nil: no counting
}

// Register adds GET / (exact) to mux. Anything else stays with the
// mux's other routes or 404s; this page has no other files.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /{$}", h.index)
}

func (h *Handler) index(w http.ResponseWriter, r *http.Request) {
	body, err := os.ReadFile(filepath.Join(h.Dir, "index.html"))
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	hd := w.Header()
	hd.Set("Content-Type", "text/html; charset=utf-8")
	hd.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
	hd.Set("Referrer-Policy", "no-referrer")
	hd.Set("X-Content-Type-Options", "nosniff")
	if _, err := w.Write(body); err != nil {
		return
	}
	// Count only a page actually served, and only real GETs (net/http
	// routes HEAD to GET handlers).
	if h.Viewer != nil && r.Method == http.MethodGet {
		h.Viewer.View(r, "/")
	}
}

// TrustedClientIP returns a hook.Config.ClientIP function: the
// X-Real-IP header when the request's direct peer is inside one of
// trusted (the proxy's own network), otherwise the direct peer itself.
// Without the peer check, any caller able to reach wisp directly could
// claim any client IP.
func TrustedClientIP(trusted []netip.Prefix) func(*http.Request) string {
	return func(r *http.Request) string {
		peer := r.RemoteAddr
		if h, _, err := net.SplitHostPort(peer); err == nil {
			peer = h
		}
		addr, err := netip.ParseAddr(peer)
		if err != nil {
			return peer
		}
		addr = addr.Unmap()
		for _, p := range trusted {
			if p.Contains(addr) {
				if real := strings.TrimSpace(r.Header.Get("X-Real-IP")); real != "" {
					return real
				}
				break
			}
		}
		return peer
	}
}

// ParsePrefixes parses a comma-separated CIDR list ("" → none).
func ParsePrefixes(list string) ([]netip.Prefix, error) {
	var out []netip.Prefix
	for _, s := range strings.Split(list, ",") {
		if s = strings.TrimSpace(s); s == "" {
			continue
		}
		p, err := netip.ParsePrefix(s)
		if err != nil {
			return nil, err
		}
		out = append(out, p.Masked())
	}
	return out, nil
}
