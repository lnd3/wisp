package stats

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"
)

func seeded(t *testing.T) *DB {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "stats.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	ctx := context.Background()
	for _, d := range []*Day{
		{Product: "cindernote", Date: "2026-10-01", Uniques: 10, Visits: 12, Views: 30, Downloads: 2, Bounces: 4,
			Pages:     []Page{{"view", "/", 20, 8}, {"view", "/about", 10, 5}, {"download", "/f", 2, 2}},
			Referrers: []Referrer{{"lobste.rs", 3, 3}},
			Agents:    []Agent{{"browser", "firefox", 6}, {"browser", "chrome", 4}},
			Hist:      []Bucket{{"views", "1", 4}, {"views", "21+", 1}, {"views", "4-5", 5}}},
		{Product: "cindernote", Date: "2026-10-02", Uniques: 5, Visits: 5, Views: 9, Bounces: 1,
			Pages:  []Page{{"view", "/about", 9, 5}},
			Agents: []Agent{{"browser", "chrome", 5}},
			Hist:   []Bucket{{"views", "1", 1}, {"views", "11-20", 4}}},
		{Product: "persona", Date: "2026-10-02", Uniques: 7, Visits: 7, Views: 7, Bounces: 7,
			Pages: []Page{{"view", "/", 7, 7}}},
	} {
		if err := db.WriteDay(ctx, d); err != nil {
			t.Fatal(err)
		}
	}
	return db
}

func TestReadQueries(t *testing.T) {
	db := seeded(t)
	ctx := context.Background()

	if p, _ := db.Products(ctx); !reflect.DeepEqual(p, []string{"cindernote", "persona"}) {
		t.Errorf("Products = %v", p)
	}
	if d, _ := db.LatestDay(ctx, "cindernote"); d != "2026-10-02" {
		t.Errorf("LatestDay = %q", d)
	}
	if d, _ := db.LatestDay(ctx, "nobody"); d != "" {
		t.Errorf("LatestDay(unknown) = %q", d)
	}

	all, _ := db.DailyTotals(ctx, "", "2026-10-01", "2026-10-02")
	if len(all) != 2 || all[1].Uniques != 12 || all[1].Views != 16 {
		t.Errorf("all-products daily totals must sum products per day: %+v", all)
	}

	pages, _ := db.TopPages(ctx, "cindernote", "2026-10-01", "2026-10-02", "view", 10)
	want := []RangeItem{{"/", 20, 8}, {"/about", 19, 10}} // ranked by hits
	if !reflect.DeepEqual(pages, want) {
		t.Errorf("TopPages = %+v, want %+v", pages, want)
	}
	if dl, _ := db.TopPages(ctx, "cindernote", "2026-10-01", "2026-10-02", "download", 10); len(dl) != 1 || dl[0].Label != "/f" {
		t.Errorf("downloads = %+v", dl)
	}

	ag, _ := db.Agents(ctx, "cindernote", "2026-10-01", "2026-10-02", "browser", 10)
	if !reflect.DeepEqual(ag, []RangeItem{{"chrome", 9, 9}, {"firefox", 6, 6}}) {
		t.Errorf("Agents = %+v", ag)
	}

	h, _ := db.Histogram(ctx, "cindernote", "2026-10-01", "2026-10-02", "views")
	var order []string
	for _, b := range h {
		order = append(order, b.Label)
	}
	if !reflect.DeepEqual(order, []string{"1", "4-5", "11-20", "21+"}) {
		t.Errorf("histogram order = %v (want numeric bucket order)", order)
	}
	if h[0].Count != 5 {
		t.Errorf("bucket 1 summed = %d, want 5", h[0].Count)
	}
}
