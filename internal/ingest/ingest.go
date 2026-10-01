// Package ingest is wisp's server-to-server ingest API (plan/designs/D002
// §2): product backends' hooks POST batches of per-visitor daily counts,
// authenticated by their product token, and each batch is merged into
// that product-day's staging file.
//
// Nothing here logs request data. The only logged events are internal
// failures (staging I/O), described without any request content — no
// visitor keys, page keys, hosts or tokens.
package ingest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/lnd3/wisp/hook"
	"github.com/lnd3/wisp/internal/events"
	"github.com/lnd3/wisp/internal/registry"
	"github.com/lnd3/wisp/internal/staging"
)

// Limits on a batch — plan/designs/D002 §2. A well-behaved hook never
// exceeds them; they bound what a misbehaving or compromised product can
// make wisp store.
const (
	MaxBodyBytes        = 16 << 20
	MaxVisitorsPerBatch = 10_000
	MaxPagesPerVisitor  = 500 // distinct view + download page keys
	MaxRefsPerVisitor   = 100
	MaxCount            = 1_000_000 // any single count in one batch
	// MaxFutureSkew tolerates a product clock slightly ahead of wisp's
	// around midnight.
	MaxFutureSkew = 10 * time.Minute
)

var (
	visitorKeyPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{22}$`) // 16 bytes, base64url
	batchIDPattern    = regexp.MustCompile(`^[A-Za-z0-9-]{1,64}$`)
	agentPattern      = regexp.MustCompile(`^[a-z0-9]{1,32}$`)
	hostPattern       = regexp.MustCompile(`^[a-z0-9.-]{1,253}$`)
)

// Registry resolves a token to its product key.
type Registry interface {
	Lookup(token string) (product string, ok bool)
}

// Stager merges an authenticated, validated batch.
type Stager interface {
	Merge(ctx context.Context, product string, b *hook.Batch) (duplicate bool, err error)
}

// Handler serves POST /v1/ingest.
type Handler struct {
	// Registry returns the current registry (reloadable, so a func).
	Registry func() Registry
	Stager   Stager
	Now      func() time.Time // default time.Now
	Log      *log.Logger      // internal failures only; default discards
	// Events, if set, receives rejections and failures for the
	// dashboard's issue list, and accepted batches for its ingest
	// status. Messages are fixed strings; never request content.
	Events *events.Log
}

// Routes returns the API's mux: POST /v1/ingest and GET /healthz.
// Everything else is 404/405 from the mux itself.
func (h *Handler) Routes() *http.ServeMux {
	mux := http.NewServeMux()
	mux.Handle("POST /v1/ingest", h)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("ok\n"))
	})
	return mux
}

type response struct {
	Status   string `json:"status,omitempty"`
	Visitors int    `json:"visitors,omitempty"`
	Error    string `json:"error,omitempty"`
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Authenticate before reading the body, so an unauthenticated caller
	// can't make wisp parse 16 MiB.
	token, ok := bearerToken(r)
	var product string
	if ok {
		product, ok = h.Registry().Lookup(token)
	}
	if !ok {
		h.Events.Record(events.Warning, "ingest", "", "rejected: missing or unknown token (401)")
		writeJSON(w, http.StatusUnauthorized, response{Error: "unauthorized"})
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, MaxBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	var b hook.Batch
	if err := dec.Decode(&b); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			h.Events.Record(events.Warning, "ingest", product, "rejected: body too large (413)")
			writeJSON(w, http.StatusRequestEntityTooLarge, response{Error: "body too large"})
			return
		}
		h.Events.Record(events.Warning, "ingest", product, "rejected: invalid JSON (400)")
		writeJSON(w, http.StatusBadRequest, response{Error: "invalid JSON"})
		return
	}
	if dec.More() {
		h.Events.Record(events.Warning, "ingest", product, "rejected: invalid JSON (400)")
		writeJSON(w, http.StatusBadRequest, response{Error: "invalid JSON"})
		return
	}
	// Same uniform 401 as a bad token: a valid token for one product
	// can't be used to probe or write another product's key.
	if b.Product != product {
		// Logged under the token's real product: its operator needs to
		// know their token is being sent with another product's key.
		h.Events.Record(events.Warning, "ingest", product, "rejected: token used with another product key (401)")
		writeJSON(w, http.StatusUnauthorized, response{Error: "unauthorized"})
		return
	}

	now := time.Now
	if h.Now != nil {
		now = h.Now
	}
	if status, msg := validate(&b, now().UTC()); status != 0 {
		// msg is one of validate's fixed strings, never request content.
		h.Events.Record(events.Warning, "ingest", product, fmt.Sprintf("rejected: %s (%d)", msg, status))
		writeJSON(w, status, response{Error: msg})
		return
	}

	dup, err := h.Stager.Merge(r.Context(), product, &b)
	if errors.Is(err, staging.ErrClosed) {
		h.Events.Record(events.Warning, "ingest", product, "rejected: day closed (409)")
		// Validated just before the deadline, but the close sealed the day
		// first. Same answer as any closed day: the hook drops the batch.
		writeJSON(w, http.StatusConflict, response{Error: "day closed"})
		return
	}
	if err != nil {
		if h.Log != nil {
			// err comes from staging/SQLite (I/O, constraint names), never
			// from request content.
			h.Log.Printf("ingest: merge failed for product %s: %v", product, err)
		}
		h.Events.Record(events.Error, "ingest", product, "staging merge failed (500): see wisp's log")
		writeJSON(w, http.StatusInternalServerError, response{Error: "internal error"})
		return
	}
	h.Events.Delivered(product)
	if dup {
		writeJSON(w, http.StatusOK, response{Status: "duplicate"})
		return
	}
	writeJSON(w, http.StatusOK, response{Status: "ok", Visitors: len(b.Visitors)})
}

// validate checks a decoded batch. It returns 0 if the batch is
// acceptable, else an HTTP status and a message that never echoes
// request content.
func validate(b *hook.Batch, now time.Time) (int, string) {
	start, err := time.Parse(time.DateOnly, b.Day)
	if err != nil {
		return http.StatusBadRequest, "invalid day"
	}
	if !now.Before(start.Add(hook.DayCloseAfter)) {
		return http.StatusConflict, "day closed"
	}
	if start.After(now.Add(MaxFutureSkew)) {
		return http.StatusBadRequest, "day in the future"
	}
	if !batchIDPattern.MatchString(b.BatchID) {
		return http.StatusBadRequest, "invalid batch_id"
	}
	if len(b.Visitors) == 0 || len(b.Visitors) > MaxVisitorsPerBatch {
		return http.StatusBadRequest, "visitors: want 1–10000"
	}
	seen := make(map[string]bool, len(b.Visitors))
	for i := range b.Visitors {
		if msg := validateVisitor(&b.Visitors[i]); msg != "" {
			return http.StatusBadRequest, msg
		}
		if seen[b.Visitors[i].K] {
			return http.StatusBadRequest, "duplicate visitor key in batch"
		}
		seen[b.Visitors[i].K] = true
	}
	return 0, ""
}

func validateVisitor(v *hook.Visitor) string {
	if !visitorKeyPattern.MatchString(v.K) {
		return "invalid visitor key"
	}
	if v.Visits < 0 || v.Visits > MaxCount {
		return "invalid visits"
	}
	// Every hook increment comes from a hit, so a visitor with no view or
	// download would only inflate uniques.
	if len(v.Views) == 0 && len(v.Downloads) == 0 {
		return "visitor without views or downloads"
	}
	if len(v.Views)+len(v.Downloads) > MaxPagesPerVisitor {
		return "too many pages for one visitor"
	}
	for _, m := range []map[string]int{v.Views, v.Downloads} {
		for page, n := range m {
			if !hook.ValidPageKey(page) {
				return "invalid page key"
			}
			if n < 1 || n > MaxCount {
				return "invalid count"
			}
		}
	}
	if len(v.Referrers) > MaxRefsPerVisitor {
		return "too many referrers for one visitor"
	}
	for host, n := range v.Referrers {
		if !hostPattern.MatchString(host) {
			return "invalid referrer host"
		}
		if n < 1 || n > MaxCount {
			return "invalid count"
		}
	}
	a := v.Agent
	if !agentPattern.MatchString(a.Browser) || !agentPattern.MatchString(a.OS) || !agentPattern.MatchString(a.Device) {
		return "invalid agent"
	}
	return ""
}

func bearerToken(r *http.Request) (string, bool) {
	tok, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	return tok, ok && tok != ""
}

func writeJSON(w http.ResponseWriter, status int, v response) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// compile-time check: *registry.Registry satisfies Registry.
var _ Registry = (*registry.Registry)(nil)
