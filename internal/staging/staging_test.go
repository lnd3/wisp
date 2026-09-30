package staging

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/lnd3/wisp/hook"
)

const (
	keyA = "AAAAAAAAAAAAAAAAAAAAAA"
	keyB = "BBBBBBBBBBBBBBBBBBBBBB"
)

var firefox = hook.Agent{Browser: "firefox", OS: "linux", Device: "desktop"}

func batch(id, day string, vs ...hook.Visitor) *hook.Batch {
	return &hook.Batch{Product: "cindernote", Day: day, BatchID: id, Visitors: vs}
}

type row struct {
	visits  int
	browser string
}

func visitors(t *testing.T, s *Store, d Day) map[string]row {
	t.Helper()
	out := map[string]row{}
	err := s.Query(d, func(db *sql.DB) error {
		rs, err := db.Query(`SELECT key, visits, browser FROM staging_visitor`)
		if err != nil {
			return err
		}
		defer rs.Close()
		for rs.Next() {
			var k string
			var r row
			if err := rs.Scan(&k, &r.visits, &r.browser); err != nil {
				return err
			}
			out[k] = r
		}
		return rs.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func count(t *testing.T, s *Store, d Day, q string, args ...any) int {
	t.Helper()
	var n int
	if err := s.Query(d, func(db *sql.DB) error { return db.QueryRow(q, args...).Scan(&n) }); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestMergeSumsAndDedups(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	day := Day{Product: "cindernote", Date: "2026-10-01"}

	b1 := batch("b1", day.Date,
		hook.Visitor{K: keyA, Visits: 1, Views: map[string]int{"/": 1, "/notes/:id": 1}, Referrers: map[string]int{"news.ycombinator.com": 1}, Agent: firefox},
		hook.Visitor{K: keyB, Visits: 1, Downloads: map[string]int{"/files/:name": 1}, Agent: firefox})
	b2 := batch("b2", day.Date,
		hook.Visitor{K: keyA, Visits: 1, Views: map[string]int{"/": 2}, Referrers: map[string]int{"news.ycombinator.com": 1},
			Agent: hook.Agent{Browser: "chrome", OS: "linux", Device: "desktop"}})

	for _, b := range []*hook.Batch{b1, b2} {
		if dup, err := s.Merge(ctx, "cindernote", b); err != nil || dup {
			t.Fatalf("Merge(%s) = %v, %v", b.BatchID, dup, err)
		}
	}
	if dup, err := s.Merge(ctx, "cindernote", b2); err != nil || !dup {
		t.Fatalf("re-Merge(b2) = %v, %v; want duplicate", dup, err)
	}

	vs := visitors(t, s, day)
	if len(vs) != 2 || vs[keyA].visits != 2 || vs[keyB].visits != 1 {
		t.Errorf("visitors = %v", vs)
	}
	if vs[keyA].browser != "firefox" {
		t.Errorf("agent must keep its first-seen value, got %q", vs[keyA].browser)
	}
	if n := count(t, s, day, `SELECT count FROM staging_hit WHERE key=? AND kind='view' AND page_key='/'`, keyA); n != 3 {
		t.Errorf("view count = %d, want 3 (1+2, duplicate ignored)", n)
	}
	if n := count(t, s, day, `SELECT count FROM staging_ref WHERE key=?`, keyA); n != 2 {
		t.Errorf("referrer count = %d, want 2", n)
	}
	if n := count(t, s, day, `SELECT count FROM staging_hit WHERE key=? AND kind='download'`, keyB); n != 1 {
		t.Errorf("download count = %d", n)
	}
	if n := count(t, s, day, `PRAGMA secure_delete`); n != 1 {
		t.Errorf("secure_delete = %d, want 1", n)
	}
}

func TestFilesPerProductDayAndDelete(t *testing.T) {
	dir := t.TempDir()
	s, _ := Open(dir)
	defer s.Close()
	ctx := context.Background()
	v := hook.Visitor{K: keyA, Visits: 1, Views: map[string]int{"/": 1}, Agent: firefox}
	s.Merge(ctx, "cindernote", batch("1", "2026-10-01", v))
	s.Merge(ctx, "cindernote", batch("2", "2026-10-02", v))
	s.Merge(ctx, "persona", batch("3", "2026-10-01", v))

	days, err := s.Days()
	if err != nil {
		t.Fatal(err)
	}
	want := []Day{{"cindernote", "2026-10-01"}, {"persona", "2026-10-01"}, {"cindernote", "2026-10-02"}}
	if len(days) != len(want) {
		t.Fatalf("Days() = %v", days)
	}
	for i := range want {
		if days[i] != want[i] {
			t.Errorf("Days()[%d] = %v, want %v", i, days[i], want[i])
		}
	}

	p := filepath.Join(dir, "cindernote", "2026-10-01.sqlite")
	fi, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("staging file mode = %v, want 0600", fi.Mode().Perm())
	}

	if err := s.Delete(want[0]); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{p, p + "-journal", p + "-wal", p + "-shm"} {
		if _, err := os.Stat(f); !os.IsNotExist(err) {
			t.Errorf("%s still exists after Delete", filepath.Base(f))
		}
	}
	if err := s.Delete(want[0]); err != nil {
		t.Errorf("deleting a missing day: %v", err)
	}
	days, _ = s.Days()
	if len(days) != 2 {
		t.Errorf("after Delete, Days() = %v", days)
	}
}

func TestRejectsUnsafePathParts(t *testing.T) {
	s, _ := Open(t.TempDir())
	defer s.Close()
	v := hook.Visitor{K: keyA, Visits: 1, Views: map[string]int{"/": 1}, Agent: firefox}
	if _, err := s.Merge(context.Background(), "../escape", batch("1", "2026-10-01", v)); err == nil {
		t.Error("product with path characters must be refused")
	}
	if _, err := s.Merge(context.Background(), "cindernote", batch("1", "2026-10-01/../../x", v)); err == nil {
		t.Error("day that isn't a date must be refused")
	}
}
