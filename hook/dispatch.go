package hook

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"sync/atomic"
	"time"
)

// run is the dispatcher: on every Interval tick it queues pending
// increments (if any) and sends what's due; just after 00:00 UTC it
// retires the finished day even if no hit arrived to do so; a kick from
// the hot path (a day retired mid-request) sends immediately.
func (h *Hook) run() {
	defer close(h.done)
	tick := time.NewTicker(h.cfg.Interval)
	defer tick.Stop()
	midnight := time.NewTimer(untilNextUTCDay(h.cfg.Clock()))
	defer midnight.Stop()

	for {
		select {
		case <-h.stop:
			return
		case <-tick.C:
			h.flush()
		case <-midnight.C:
			midnight.Reset(untilNextUTCDay(h.cfg.Clock()))
			h.flush()
		case <-h.kick:
		}
		h.sendDue(h.ctx, false)
	}
}

// flush retires the day if it has ended, then queues the current day's
// pending increments as a batch — if there are any. An idle interval
// queues nothing and so sends nothing.
func (h *Hook) flush() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.retireLocked(h.cfg.Clock().UTC())
	if h.day != nil {
		h.enqueueLocked(h.day)
	}
}

// enqueueLocked turns d's pending increments into one or more batches
// (at most maxVisitorsPerBatch visitors each), queues them, and resets
// pending. Visitor records are encoded once here; retries resend the
// same bytes under the same batch_id, which wisp dedups on.
func (h *Hook) enqueueLocked(d *dayState) {
	if len(d.pending) == 0 {
		return
	}
	visitors := make([]Visitor, 0, len(d.pending))
	for _, v := range d.pending {
		if !v.empty() {
			visitors = append(visitors, *v)
		}
	}
	d.pending = make(map[string]*Visitor)
	// Deterministic order: easier to test, and batch contents don't
	// reveal hit order.
	sort.Slice(visitors, func(i, j int) bool { return visitors[i].K < visitors[j].K })

	for len(visitors) > 0 {
		n := min(len(visitors), maxVisitorsPerBatch)
		b := Batch{Product: h.cfg.ProductKey, Day: d.date, BatchID: newBatchID(), Visitors: visitors[:n]}
		visitors = visitors[n:]
		body, err := json.Marshal(b)
		if err != nil { // not reachable with these types; drop rather than panic
			h.stats.BatchesDropped++
			continue
		}
		h.queue = append(h.queue, &queued{
			day:      b.Day,
			id:       b.BatchID,
			body:     body,
			deadline: d.start.Add(closeDeadline),
		})
	}
	for len(h.queue) > h.cfg.MaxQueuedBatches {
		h.queue = h.queue[1:]
		h.stats.BatchesDropped++
	}
}

// sendDue posts every queued batch whose retry time has come (all of
// them when force is set), in queue order. Batches past their day's
// close deadline are dropped unsent.
func (h *Hook) sendDue(ctx context.Context, force bool) {
	h.sendMu.Lock()
	defer h.sendMu.Unlock()

	now := h.cfg.Clock()
	var due []*queued
	h.mu.Lock()
	kept := h.queue[:0]
	for _, q := range h.queue {
		if !now.Before(q.deadline) {
			h.stats.BatchesDropped++
			continue
		}
		kept = append(kept, q)
		if force || !now.Before(q.next) {
			due = append(due, q)
		}
	}
	h.queue = kept
	h.mu.Unlock()

	for _, q := range due {
		if ctx.Err() != nil {
			return
		}
		status, err := h.post(ctx, q.body)
		h.mu.Lock()
		report := h.settleLocked(q, status, err, now)
		h.mu.Unlock()
		h.report(report)
	}
}

// settleLocked applies a send outcome to q: success and permanent
// refusals remove it, transient failures schedule a retry with backoff.
func (h *Hook) settleLocked(q *queued, status int, err error, now time.Time) error {
	switch {
	case err == nil && status >= 200 && status < 300:
		h.removeLocked(q)
		h.stats.BatchesSent++
		return nil
	case err == nil && status == http.StatusConflict:
		h.removeLocked(q)
		h.stats.BatchesDropped++
		return fmt.Errorf("%w (day %s)", ErrDayClosed, q.day)
	case err == nil && status >= 400 && status < 500 &&
		status != http.StatusRequestTimeout && status != http.StatusTooManyRequests:
		h.removeLocked(q)
		h.stats.BatchesDropped++
		return fmt.Errorf("%w: HTTP %d", ErrRejected, status)
	}
	q.attempts++
	backoff := h.cfg.Interval << min(q.attempts-1, 16)
	q.next = now.Add(min(backoff, maxBackoff))
	if err != nil {
		return fmt.Errorf("wisp hook: send failed, will retry: %w", err)
	}
	return fmt.Errorf("wisp hook: ingest returned HTTP %d, will retry", status)
}

func (h *Hook) removeLocked(q *queued) {
	for i, x := range h.queue {
		if x == q {
			h.queue = append(h.queue[:i], h.queue[i+1:]...)
			return
		}
	}
}

func (h *Hook) post(ctx context.Context, body []byte) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.cfg.Endpoint, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+h.cfg.Token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "wisp-hook/1")
	resp, err := h.cfg.HTTPClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	return resp.StatusCode, nil
}

// Close stops the dispatcher, queues whatever is pending, and makes one
// final attempt to send every queued batch within ctx. Hits after Close
// are ignored. The day's salt and accumulator are discarded. It returns
// an error if batches were left unsent (they are dropped).
func (h *Hook) Close(ctx context.Context) error {
	if h == nil || h.disabled {
		return nil
	}
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return nil
	}
	h.closed = true
	h.mu.Unlock()

	close(h.stop)
	h.cancel() // abort an in-flight dispatcher send; it's retried below
	<-h.done

	h.flush()
	h.sendDue(ctx, true)

	h.mu.Lock()
	defer h.mu.Unlock()
	if h.day != nil {
		h.day.salt.wipe()
		h.day = nil
	}
	unsent := len(h.queue)
	h.stats.BatchesDropped += uint64(unsent)
	h.queue = nil
	if unsent > 0 {
		return fmt.Errorf("wisp hook: %d batches unsent at close", unsent)
	}
	return nil
}

func untilNextUTCDay(now time.Time) time.Duration {
	now = now.UTC()
	next := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, time.UTC)
	return next.Sub(now) + time.Second
}

var batchSeq atomic.Uint64

// newBatchID returns a random 128-bit hex ID. If the system RNG fails,
// it falls back to time plus a process counter — still unique per
// process, which is all dedup needs.
func newBatchID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err == nil {
		return hex.EncodeToString(b[:])
	}
	return fmt.Sprintf("t%x-%x", time.Now().UnixNano(), batchSeq.Add(1))
}
