package stats

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// allowedColumns is every column the stats DB may have. A new column
// must be added here deliberately — the point is that nothing resembling
// a visitor key ever appears.
var allowedColumns = map[string]bool{
	"product": true, "day": true, "uniques": true, "visits": true, "views": true,
	"downloads": true, "bounces": true, "kind": true, "page_key": true, "hits": true,
	"host": true, "dim": true, "value": true, "metric": true, "bucket": true, "visitors": true,
}

func TestSchemaHasNoVisitorKeyColumn(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "stats.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, table := range Tables {
		rs, err := db.SQL().Query(`SELECT name FROM pragma_table_info(?)`, table)
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for rs.Next() {
			var col string
			rs.Scan(&col)
			n++
			if !allowedColumns[col] {
				t.Errorf("%s.%s is not an allowed stats column", table, col)
			}
		}
		rs.Close()
		if n == 0 {
			t.Errorf("table %s missing", table)
		}
	}
}

func TestFileMode(t *testing.T) {
	p := filepath.Join(t.TempDir(), "sub", "stats.sqlite")
	db, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	fi, _ := os.Stat(p)
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, want 0600", fi.Mode().Perm())
	}
}

func TestBuckets(t *testing.T) {
	for n, want := range map[int]string{0: "0", 1: "1", 3: "3", 4: "4-5", 5: "4-5", 6: "6-10", 10: "6-10", 11: "11-20", 20: "11-20", 21: "21+", 500: "21+"} {
		if got := PagesBucket(n); got != want {
			t.Errorf("PagesBucket(%d) = %q, want %q", n, got, want)
		}
	}
	for n, want := range map[int]string{0: "0", 1: "1", 3: "3", 4: "4+", 99: "4+"} {
		if got := VisitsBucket(n); got != want {
			t.Errorf("VisitsBucket(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestWriteReadReplace(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "stats.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()

	first := &Day{Product: "cindernote", Date: "2026-10-01", Uniques: 9, Views: 9,
		Pages: []Page{{"view", "/old", 9, 9}}}
	want := &Day{Product: "cindernote", Date: "2026-10-01", Uniques: 1, Visits: 2, Views: 3, Downloads: 1, Bounces: 0,
		Pages:     []Page{{"download", "/files/:name", 1, 1}, {"view", "/", 2, 1}},
		Referrers: []Referrer{{"news.ycombinator.com", 1, 1}},
		Agents:    []Agent{{"browser", "firefox", 1}},
		Hist:      []Bucket{{"visits", "2", 1}},
	}
	other := &Day{Product: "persona", Date: "2026-10-01", Uniques: 5}
	for _, d := range []*Day{first, other, want} { // want replaces first
		if err := db.WriteDay(ctx, d); err != nil {
			t.Fatal(err)
		}
	}
	got, err := db.ReadDay(ctx, "cindernote", "2026-10-01")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ReadDay =\n %+v\nwant\n %+v", got, want)
	}
	if o, _ := db.ReadDay(ctx, "persona", "2026-10-01"); o == nil || o.Uniques != 5 {
		t.Error("replacing one product-day must not touch another")
	}
	if _, err := db.ReadDay(ctx, "cindernote", "2026-10-02"); err != ErrNotFound {
		t.Errorf("missing day: err = %v", err)
	}
}
