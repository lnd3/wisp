// Package stats is wisp's product statistics DB (plan/designs/D002 §5):
// the durable, keyless result of each product-day's close. No table has
// a visitor-key column; each row describes one product-day and is
// written once, at close.
//
// What's additive over a window of days: views, downloads, visits,
// bounces, per-page hits, per-referrer visits and histogram visitors.
// What isn't: uniques. A sum of daily uniques is visitor-days, never
// "unique visitors" — the same person on five days is five different
// keys, by design.
package stats

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

const schema = `
CREATE TABLE IF NOT EXISTS daily_totals (
	product   TEXT NOT NULL,
	day       TEXT NOT NULL,
	uniques   INTEGER NOT NULL,
	visits    INTEGER NOT NULL,
	views     INTEGER NOT NULL,
	downloads INTEGER NOT NULL,
	bounces   INTEGER NOT NULL,
	PRIMARY KEY (product, day)
) WITHOUT ROWID;
CREATE TABLE IF NOT EXISTS daily_page (
	product  TEXT NOT NULL,
	day      TEXT NOT NULL,
	kind     TEXT NOT NULL CHECK (kind IN ('view', 'download')),
	page_key TEXT NOT NULL,
	hits     INTEGER NOT NULL,
	uniques  INTEGER NOT NULL,
	PRIMARY KEY (product, day, kind, page_key)
) WITHOUT ROWID;
CREATE TABLE IF NOT EXISTS daily_ref (
	product TEXT NOT NULL,
	day     TEXT NOT NULL,
	host    TEXT NOT NULL,
	visits  INTEGER NOT NULL,
	uniques INTEGER NOT NULL,
	PRIMARY KEY (product, day, host)
) WITHOUT ROWID;
CREATE TABLE IF NOT EXISTS daily_agent (
	product TEXT NOT NULL,
	day     TEXT NOT NULL,
	dim     TEXT NOT NULL CHECK (dim IN ('browser', 'os', 'device')),
	value   TEXT NOT NULL,
	uniques INTEGER NOT NULL,
	PRIMARY KEY (product, day, dim, value)
) WITHOUT ROWID;
CREATE TABLE IF NOT EXISTS daily_hist (
	product  TEXT NOT NULL,
	day      TEXT NOT NULL,
	metric   TEXT NOT NULL CHECK (metric IN ('pages', 'views', 'visits')),
	bucket   TEXT NOT NULL,
	visitors INTEGER NOT NULL,
	PRIMARY KEY (product, day, metric, bucket)
) WITHOUT ROWID;
`

// Tables lists every stats table, for tests and for WriteDay's replace.
var Tables = []string{"daily_totals", "daily_page", "daily_ref", "daily_agent", "daily_hist"}

// ErrNotFound: no closed day for that product and date.
var ErrNotFound = errors.New("stats: day not found")

// Page is one page key's hits (views or downloads) and distinct visitors.
type Page struct {
	Kind    string // "view" | "download"
	PageKey string
	Hits    int
	Uniques int
}

// Referrer is one external referrer host's visits and distinct visitors.
type Referrer struct {
	Host    string
	Visits  int
	Uniques int
}

// Agent is the number of distinct visitors with one browser, OS or
// device value.
type Agent struct {
	Dim     string // "browser" | "os" | "device"
	Value   string
	Uniques int
}

// Bucket is one histogram bar: how many visitors fell into a bucket of
// a per-visitor metric.
type Bucket struct {
	Metric   string // "pages" | "views" | "visits"
	Bucket   string
	Visitors int
}

// Day is one closed product-day. Slices are in a deterministic order
// (see staging.Summarize).
type Day struct {
	Product   string
	Date      string // YYYY-MM-DD, UTC
	Uniques   int
	Visits    int
	Views     int
	Downloads int
	Bounces   int // visitors with exactly one view in total
	Pages     []Page
	Referrers []Referrer
	Agents    []Agent
	Hist      []Bucket
}

// PagesBucket and ViewsBucket bucket a visitor's distinct pages or total
// views: 1, 2, 3, 4-5, 6-10, 11-20, 21+.
func PagesBucket(n int) string { return countBucket(n) }

// ViewsBucket — see PagesBucket.
func ViewsBucket(n int) string { return countBucket(n) }

func countBucket(n int) string {
	switch {
	case n <= 0:
		return "0"
	case n <= 3:
		return fmt.Sprint(n)
	case n <= 5:
		return "4-5"
	case n <= 10:
		return "6-10"
	case n <= 20:
		return "11-20"
	default:
		return "21+"
	}
}

// VisitsBucket buckets a visitor's visits: 1, 2, 3, 4+.
func VisitsBucket(n int) string {
	switch {
	case n <= 0:
		return "0"
	case n <= 3:
		return fmt.Sprint(n)
	default:
		return "4+"
	}
}

// DB is the product statistics database.
type DB struct {
	db *sql.DB
}

// Open opens (creating 0600 if needed) the stats DB at path.
func Open(path string) (*DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("stats: %w", err)
	}
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, fmt.Errorf("stats: %w", err)
	}
	f.Close()
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, fmt.Errorf("stats: %w", err)
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("stats: schema: %w", err)
	}
	return &DB{db: db}, nil
}

// Close closes the database.
func (s *DB) Close() error { return s.db.Close() }

// SQL exposes the handle for read queries (dashboard, tests).
func (s *DB) SQL() *sql.DB { return s.db }

// WriteDay stores d, replacing any rows already stored for the same
// product-day, in one transaction. Replacing (not adding) is what makes
// a re-run close idempotent: a crash between writing stats and deleting
// the staging file just recomputes the same rows.
func (s *DB) WriteDay(ctx context.Context, d *Day) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("stats: begin: %w", err)
	}
	defer tx.Rollback()
	for _, t := range Tables {
		if _, err := tx.ExecContext(ctx, `DELETE FROM `+t+` WHERE product = ? AND day = ?`, d.Product, d.Date); err != nil {
			return fmt.Errorf("stats: clear %s: %w", t, err)
		}
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO daily_totals (product, day, uniques, visits, views, downloads, bounces) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		d.Product, d.Date, d.Uniques, d.Visits, d.Views, d.Downloads, d.Bounces); err != nil {
		return fmt.Errorf("stats: totals: %w", err)
	}
	for _, p := range d.Pages {
		if _, err := tx.ExecContext(ctx, `INSERT INTO daily_page (product, day, kind, page_key, hits, uniques) VALUES (?, ?, ?, ?, ?, ?)`,
			d.Product, d.Date, p.Kind, p.PageKey, p.Hits, p.Uniques); err != nil {
			return fmt.Errorf("stats: page: %w", err)
		}
	}
	for _, r := range d.Referrers {
		if _, err := tx.ExecContext(ctx, `INSERT INTO daily_ref (product, day, host, visits, uniques) VALUES (?, ?, ?, ?, ?)`,
			d.Product, d.Date, r.Host, r.Visits, r.Uniques); err != nil {
			return fmt.Errorf("stats: referrer: %w", err)
		}
	}
	for _, a := range d.Agents {
		if _, err := tx.ExecContext(ctx, `INSERT INTO daily_agent (product, day, dim, value, uniques) VALUES (?, ?, ?, ?, ?)`,
			d.Product, d.Date, a.Dim, a.Value, a.Uniques); err != nil {
			return fmt.Errorf("stats: agent: %w", err)
		}
	}
	for _, b := range d.Hist {
		if _, err := tx.ExecContext(ctx, `INSERT INTO daily_hist (product, day, metric, bucket, visitors) VALUES (?, ?, ?, ?, ?)`,
			d.Product, d.Date, b.Metric, b.Bucket, b.Visitors); err != nil {
			return fmt.Errorf("stats: histogram: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("stats: commit: %w", err)
	}
	return nil
}

// ReadDay loads one closed product-day, with slices in the same order
// Summarize produces.
func (s *DB) ReadDay(ctx context.Context, product, date string) (*Day, error) {
	d := &Day{Product: product, Date: date}
	err := s.db.QueryRowContext(ctx,
		`SELECT uniques, visits, views, downloads, bounces FROM daily_totals WHERE product = ? AND day = ?`, product, date).
		Scan(&d.Uniques, &d.Visits, &d.Views, &d.Downloads, &d.Bounces)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("stats: %w", err)
	}
	if err := query(ctx, s.db, `SELECT kind, page_key, hits, uniques FROM daily_page WHERE product = ? AND day = ? ORDER BY kind, page_key`,
		[]any{product, date}, func(rs *sql.Rows) error {
			var p Page
			err := rs.Scan(&p.Kind, &p.PageKey, &p.Hits, &p.Uniques)
			d.Pages = append(d.Pages, p)
			return err
		}); err != nil {
		return nil, err
	}
	if err := query(ctx, s.db, `SELECT host, visits, uniques FROM daily_ref WHERE product = ? AND day = ? ORDER BY host`,
		[]any{product, date}, func(rs *sql.Rows) error {
			var r Referrer
			err := rs.Scan(&r.Host, &r.Visits, &r.Uniques)
			d.Referrers = append(d.Referrers, r)
			return err
		}); err != nil {
		return nil, err
	}
	if err := query(ctx, s.db, `SELECT dim, value, uniques FROM daily_agent WHERE product = ? AND day = ? ORDER BY dim, value`,
		[]any{product, date}, func(rs *sql.Rows) error {
			var a Agent
			err := rs.Scan(&a.Dim, &a.Value, &a.Uniques)
			d.Agents = append(d.Agents, a)
			return err
		}); err != nil {
		return nil, err
	}
	if err := query(ctx, s.db, `SELECT metric, bucket, visitors FROM daily_hist WHERE product = ? AND day = ? ORDER BY metric, bucket`,
		[]any{product, date}, func(rs *sql.Rows) error {
			var b Bucket
			err := rs.Scan(&b.Metric, &b.Bucket, &b.Visitors)
			d.Hist = append(d.Hist, b)
			return err
		}); err != nil {
		return nil, err
	}
	return d, nil
}

func query(ctx context.Context, db *sql.DB, q string, args []any, row func(*sql.Rows) error) error {
	rs, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return fmt.Errorf("stats: %w", err)
	}
	defer rs.Close()
	for rs.Next() {
		if err := row(rs); err != nil {
			return fmt.Errorf("stats: %w", err)
		}
	}
	return rs.Err()
}
