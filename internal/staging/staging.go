// Package staging is wisp's day-scoped, keyed store (plan/designs/D002
// §3): one SQLite file per (product, UTC day) holding per-visitor counts
// merged from the hooks' batches. It is the only place visitor keys ever
// exist at wisp. A day's file is deleted whole, never row by row, when
// the day closes, so no page or free-list remnant of a key survives in a
// long-lived database.
package staging

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/lnd3/wisp/hook"
	"github.com/lnd3/wisp/internal/registry"
	_ "modernc.org/sqlite"
)

const schema = `
CREATE TABLE IF NOT EXISTS staging_visitor (
	key     TEXT PRIMARY KEY,
	visits  INTEGER NOT NULL,
	browser TEXT NOT NULL,
	os      TEXT NOT NULL,
	device  TEXT NOT NULL
) WITHOUT ROWID;
CREATE TABLE IF NOT EXISTS staging_hit (
	key      TEXT NOT NULL,
	kind     TEXT NOT NULL CHECK (kind IN ('view', 'download')),
	page_key TEXT NOT NULL,
	count    INTEGER NOT NULL,
	PRIMARY KEY (key, kind, page_key)
) WITHOUT ROWID;
CREATE TABLE IF NOT EXISTS staging_ref (
	key   TEXT NOT NULL,
	host  TEXT NOT NULL,
	count INTEGER NOT NULL,
	PRIMARY KEY (key, host)
) WITHOUT ROWID;
CREATE TABLE IF NOT EXISTS applied_batches (
	batch_id TEXT PRIMARY KEY
) WITHOUT ROWID;
`

// Day identifies one staging file.
type Day struct {
	Product string
	Date    string // YYYY-MM-DD, UTC
}

// Start is the UTC instant the day begins.
func (d Day) Start() time.Time {
	t, _ := time.Parse(time.DateOnly, d.Date)
	return t
}

// Store manages the staging files under one directory. All operations
// are serialized by a single mutex: ingestion is a handful of products
// sending every few minutes, far below where finer locking would matter,
// and one lock makes "no merge races a delete" trivially true.
type Store struct {
	dir  string
	mu   sync.Mutex
	open map[Day]*sql.DB
}

// Open prepares dir (created 0700 if missing) as the staging root.
func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("staging: %w", err)
	}
	return &Store{dir: dir, open: make(map[Day]*sql.DB)}, nil
}

func (s *Store) path(d Day) string {
	return filepath.Join(s.dir, d.Product, d.Date+".sqlite")
}

// db returns the open handle for d, creating the file (0600) and schema
// on first use.
func (s *Store) db(d Day) (*sql.DB, error) {
	if db := s.open[d]; db != nil {
		return db, nil
	}
	// Both parts become path segments: refuse anything that isn't
	// exactly a product key and a date, whatever the caller checked.
	if !registry.ValidKey(d.Product) {
		return nil, fmt.Errorf("invalid product key")
	}
	if _, err := time.Parse(time.DateOnly, d.Date); err != nil {
		return nil, fmt.Errorf("invalid day")
	}
	p := s.path(d)
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return nil, err
	}
	// Create the file ourselves so its mode is 0600 before SQLite opens it.
	f, err := os.OpenFile(p, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	f.Close()
	// secure_delete overwrites deleted content with zeros; the rollback
	// journal (not WAL) keeps all keyed data in the one file plus a
	// transient -journal, both removed by Delete.
	dsn := "file:" + p + "?_pragma=secure_delete(1)&_pragma=journal_mode(DELETE)&_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, err
	}
	s.open[d] = db
	return db, nil
}

// Merge applies a batch to the (product, b.Day) staging file in one
// transaction: counts are summed per visitor key, a visitor's agent keeps
// its first-seen value, and a batch ID already applied makes the whole
// call a no-op (duplicate = true). The caller has authenticated product
// and validated b.
func (s *Store) Merge(ctx context.Context, product string, b *hook.Batch) (duplicate bool, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	db, err := s.db(Day{Product: product, Date: b.Day})
	if err != nil {
		return false, fmt.Errorf("staging: open: %w", err)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("staging: begin: %w", err)
	}
	defer tx.Rollback()

	res, err := tx.ExecContext(ctx, `INSERT INTO applied_batches (batch_id) VALUES (?) ON CONFLICT DO NOTHING`, b.BatchID)
	if err != nil {
		return false, fmt.Errorf("staging: dedup: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return true, nil
	}

	visitor, err := tx.PrepareContext(ctx, `INSERT INTO staging_visitor (key, visits, browser, os, device) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (key) DO UPDATE SET visits = visits + excluded.visits`)
	if err != nil {
		return false, err
	}
	hit, err := tx.PrepareContext(ctx, `INSERT INTO staging_hit (key, kind, page_key, count) VALUES (?, ?, ?, ?)
		ON CONFLICT (key, kind, page_key) DO UPDATE SET count = count + excluded.count`)
	if err != nil {
		return false, err
	}
	ref, err := tx.PrepareContext(ctx, `INSERT INTO staging_ref (key, host, count) VALUES (?, ?, ?)
		ON CONFLICT (key, host) DO UPDATE SET count = count + excluded.count`)
	if err != nil {
		return false, err
	}

	for i := range b.Visitors {
		v := &b.Visitors[i]
		if _, err := visitor.ExecContext(ctx, v.K, v.Visits, v.Agent.Browser, v.Agent.OS, v.Agent.Device); err != nil {
			return false, fmt.Errorf("staging: visitor: %w", err)
		}
		for page, n := range v.Views {
			if _, err := hit.ExecContext(ctx, v.K, "view", page, n); err != nil {
				return false, fmt.Errorf("staging: view: %w", err)
			}
		}
		for page, n := range v.Downloads {
			if _, err := hit.ExecContext(ctx, v.K, "download", page, n); err != nil {
				return false, fmt.Errorf("staging: download: %w", err)
			}
		}
		for host, n := range v.Referrers {
			if _, err := ref.ExecContext(ctx, v.K, host, n); err != nil {
				return false, fmt.Errorf("staging: referrer: %w", err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("staging: commit: %w", err)
	}
	return false, nil
}

// Days lists the staging files present, sorted by date then product.
func (s *Store) Days() ([]Day, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	products, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, err
	}
	var days []Day
	for _, p := range products {
		if !p.IsDir() {
			continue
		}
		files, err := os.ReadDir(filepath.Join(s.dir, p.Name()))
		if err != nil {
			return nil, err
		}
		for _, f := range files {
			date, ok := strings.CutSuffix(f.Name(), ".sqlite")
			if !ok {
				continue
			}
			if _, err := time.Parse(time.DateOnly, date); err != nil {
				continue
			}
			days = append(days, Day{Product: p.Name(), Date: date})
		}
	}
	sort.Slice(days, func(i, j int) bool {
		if days[i].Date != days[j].Date {
			return days[i].Date < days[j].Date
		}
		return days[i].Product < days[j].Product
	})
	return days, nil
}

// Delete closes d's handle and removes its file and any SQLite side
// files. Deleting a day that doesn't exist is not an error.
func (s *Store) Delete(d Day) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if db := s.open[d]; db != nil {
		db.Close()
		delete(s.open, d)
	}
	p := s.path(d)
	var errs []error
	for _, f := range []string{p, p + "-journal", p + "-wal", p + "-shm"} {
		if err := os.Remove(f); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// Query runs fn against d's open staging database, for the day close
// and tests. The store lock is held for the duration.
func (s *Store) Query(d Day, fn func(*sql.DB) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := os.Stat(s.path(d)); err != nil {
		return err
	}
	db, err := s.db(d)
	if err != nil {
		return err
	}
	return fn(db)
}

// Close closes every open handle. Files are kept.
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var errs []error
	for d, db := range s.open {
		errs = append(errs, db.Close())
		delete(s.open, d)
	}
	return errors.Join(errs...)
}
