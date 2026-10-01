package stats

import (
	"context"
	"database/sql"
	"sort"
	"strconv"
	"strings"
)

// Read-side queries for the dashboard. A product of "" means every
// product: sums across products, which — like sums across days — are
// visitor-days, since each product keys visitors with its own salt.
//
// Field names say what they are: a per-day row has Uniques (distinct
// visitors that day), a multi-day sum has VisitorDays. Never present a
// VisitorDays value as "unique visitors".

// DayTotals is one day's totals (summed across products when the query
// covers all of them).
type DayTotals struct {
	Date      string
	Uniques   int
	Visits    int
	Views     int
	Downloads int
	Bounces   int
}

// RangeItem is one row of a ranked breakdown over a date range.
type RangeItem struct {
	Label       string // page key, referrer host, or agent value
	Count       int    // hits (pages), visits (referrers), or visitor-days (agents)
	VisitorDays int    // sum of the per-day distinct visitors for this item
}

// Products returns every product with at least one closed day.
func (s *DB) Products(ctx context.Context) ([]string, error) {
	var out []string
	err := query(ctx, s.db, `SELECT DISTINCT product FROM daily_totals ORDER BY product`, nil, func(rs *sql.Rows) error {
		var p string
		err := rs.Scan(&p)
		out = append(out, p)
		return err
	})
	return out, err
}

// LatestDay returns the most recent closed day ("" if none), for product
// or, if product is "", for any product.
func (s *DB) LatestDay(ctx context.Context, product string) (string, error) {
	var d *string
	err := s.db.QueryRowContext(ctx, `SELECT MAX(day) FROM daily_totals WHERE (? = '' OR product = ?)`, product, product).Scan(&d)
	if err != nil || d == nil {
		return "", err
	}
	return *d, nil
}

// DailyTotals returns one row per closed day in [from, to], ascending.
// Days with no row are simply absent; the caller fills gaps.
func (s *DB) DailyTotals(ctx context.Context, product, from, to string) ([]DayTotals, error) {
	var out []DayTotals
	err := query(ctx, s.db, `
		SELECT day, SUM(uniques), SUM(visits), SUM(views), SUM(downloads), SUM(bounces)
		FROM daily_totals
		WHERE (? = '' OR product = ?) AND day BETWEEN ? AND ?
		GROUP BY day ORDER BY day`,
		[]any{product, product, from, to}, func(rs *sql.Rows) error {
			var d DayTotals
			err := rs.Scan(&d.Date, &d.Uniques, &d.Visits, &d.Views, &d.Downloads, &d.Bounces)
			out = append(out, d)
			return err
		})
	return out, err
}

// TopPages ranks page keys of one kind ("view" or "download") by hits.
func (s *DB) TopPages(ctx context.Context, product, from, to, kind string, limit int) ([]RangeItem, error) {
	return s.ranked(ctx, `
		SELECT page_key, SUM(hits), SUM(uniques) FROM daily_page
		WHERE (? = '' OR product = ?) AND day BETWEEN ? AND ? AND kind = ?
		GROUP BY page_key ORDER BY SUM(hits) DESC, page_key LIMIT ?`,
		product, product, from, to, kind, limit)
}

// TopReferrers ranks referrer hosts by visits.
func (s *DB) TopReferrers(ctx context.Context, product, from, to string, limit int) ([]RangeItem, error) {
	return s.ranked(ctx, `
		SELECT host, SUM(visits), SUM(uniques) FROM daily_ref
		WHERE (? = '' OR product = ?) AND day BETWEEN ? AND ?
		GROUP BY host ORDER BY SUM(visits) DESC, host LIMIT ?`,
		product, product, from, to, limit)
}

// Agents ranks one agent dimension's values ("browser", "os", "device")
// by visitor-days.
func (s *DB) Agents(ctx context.Context, product, from, to, dim string, limit int) ([]RangeItem, error) {
	return s.ranked(ctx, `
		SELECT value, SUM(uniques), SUM(uniques) FROM daily_agent
		WHERE (? = '' OR product = ?) AND day BETWEEN ? AND ? AND dim = ?
		GROUP BY value ORDER BY SUM(uniques) DESC, value LIMIT ?`,
		product, product, from, to, dim, limit)
}

// Histogram sums one per-visitor histogram ("pages", "views", "visits")
// over the range, in bucket order (1, 2, 3, 4-5, …, 21+). Count is
// visitor-days.
func (s *DB) Histogram(ctx context.Context, product, from, to, metric string) ([]RangeItem, error) {
	items, err := s.ranked(ctx, `
		SELECT bucket, SUM(visitors), SUM(visitors) FROM daily_hist
		WHERE (? = '' OR product = ?) AND day BETWEEN ? AND ? AND metric = ?
		GROUP BY bucket LIMIT ?`,
		product, product, from, to, metric, 100)
	if err != nil {
		return nil, err
	}
	sort.Slice(items, func(i, j int) bool { return BucketOrder(items[i].Label) < BucketOrder(items[j].Label) })
	return items, nil
}

// BucketOrder sorts "0", "1", "4-5", "21+", "4+" by their lower bound.
func BucketOrder(b string) int {
	n, _ := strconv.Atoi(strings.TrimRight(strings.SplitN(b, "-", 2)[0], "+"))
	return n
}

func (s *DB) ranked(ctx context.Context, q string, args ...any) ([]RangeItem, error) {
	var out []RangeItem
	err := query(ctx, s.db, q, args, func(rs *sql.Rows) error {
		var it RangeItem
		err := rs.Scan(&it.Label, &it.Count, &it.VisitorDays)
		out = append(out, it)
		return err
	})
	return out, err
}
