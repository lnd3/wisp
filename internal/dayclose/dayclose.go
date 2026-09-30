// Package dayclose closes product-days (plan/designs/D002 §4): once a
// day is past its deadline (start + hook.DayCloseAfter), its staging file
// is sealed, summarized into the product statistics DB, and deleted —
// the moment every visitor key for that day stops existing at wisp.
package dayclose

import (
	"context"
	"log"
	"time"

	"github.com/lnd3/wisp/hook"
	"github.com/lnd3/wisp/internal/staging"
	"github.com/lnd3/wisp/internal/stats"
)

// DefaultMaxDelay bounds how long a failing close is retried before the
// staging file is deleted unaggregated. Losing a day's stats is
// recoverable; keys outliving their day is not (CLAUDE.md).
const DefaultMaxDelay = time.Hour

// Closer runs day closes. Run it at startup and then periodically.
type Closer struct {
	Staging  *staging.Store
	Stats    *stats.DB
	Log      *log.Logger   // required; product/day and errors only
	MaxDelay time.Duration // default DefaultMaxDelay
}

// Result counts one Run's outcomes.
type Result struct {
	Closed    int // summarized into stats, then deleted
	Discarded int // deleted unaggregated after MaxDelay of failures
	Retrying  int // failed, left for the next Run
}

// Run closes every staging day past its deadline at now.
//
// Per day, in order: Seal (no batch can merge any more), Summarize,
// WriteDay (replacing any earlier rows for that product-day, so a re-run
// after a crash is idempotent), then Delete. If summarizing or writing
// fails, the file is kept for the next Run — until MaxDelay past the
// deadline, after which it's deleted anyway.
func (c *Closer) Run(ctx context.Context, now time.Time) Result {
	maxDelay := c.MaxDelay
	if maxDelay <= 0 {
		maxDelay = DefaultMaxDelay
	}
	var res Result
	days, err := c.Staging.Days()
	if err != nil {
		c.Log.Printf("dayclose: list staging: %v", err)
		return res
	}
	for _, d := range days {
		deadline := d.Start().Add(hook.DayCloseAfter)
		if now.Before(deadline) {
			continue
		}
		c.Staging.Seal(d)

		sum, err := c.Staging.Summarize(ctx, d)
		if err == nil {
			err = c.Stats.WriteDay(ctx, sum)
		}
		if err != nil {
			if now.Before(deadline.Add(maxDelay)) {
				c.Log.Printf("dayclose: %s/%s failed, will retry: %v", d.Product, d.Date, err)
				res.Retrying++
				continue
			}
			c.Log.Printf("dayclose: %s/%s failed past the %s limit — deleting staging UNAGGREGATED: %v", d.Product, d.Date, maxDelay, err)
			if err := c.Staging.Delete(d); err != nil {
				c.Log.Printf("dayclose: delete %s/%s: %v", d.Product, d.Date, err)
				continue
			}
			res.Discarded++
			continue
		}

		if err := c.Staging.Delete(d); err != nil {
			// Stats are committed; the next Run re-summarizes the same file
			// and rewrites the same rows, then retries the delete.
			c.Log.Printf("dayclose: delete %s/%s: %v", d.Product, d.Date, err)
			res.Retrying++
			continue
		}
		c.Log.Printf("dayclose: closed %s/%s (%d visitors)", d.Product, d.Date, sum.Uniques)
		res.Closed++
	}
	return res
}
