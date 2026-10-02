package dashboard

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/lnd3/wisp/internal/stats"
)

func server(t *testing.T, days ...*stats.Day) *httptest.Server {
	t.Helper()
	db, err := stats.Open(filepath.Join(t.TempDir(), "stats.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	for _, d := range days {
		if err := db.WriteDay(context.Background(), d); err != nil {
			t.Fatal(err)
		}
	}
	mux := http.NewServeMux()
	(&Handler{Stats: db}).Register(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func get(t *testing.T, srv *httptest.Server, path string) (int, http.Header, string) {
	t.Helper()
	resp, err := http.Get(srv.URL + path)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, resp.Header, string(b)
}

func day(product, date string, uniques int, pages ...stats.Page) *stats.Day {
	return &stats.Day{Product: product, Date: date, Uniques: uniques, Visits: uniques, Views: 2 * uniques, Bounces: 1,
		Pages: pages, Agents: []stats.Agent{{Dim: "browser", Value: "firefox", Uniques: uniques}},
		Hist: []stats.Bucket{{Metric: "views", Bucket: "2", Visitors: uniques}}}
}

func TestEmpty(t *testing.T) {
	st, h, body := get(t, server(t), "/dashboard/?view=history&days=30")
	if st != 200 || !strings.Contains(body, "No closed days yet") {
		t.Fatalf("got %d: %s", st, body)
	}
	if !strings.Contains(h.Get("Content-Security-Policy"), "default-src 'none'") {
		t.Errorf("CSP = %q", h.Get("Content-Security-Policy"))
	}
}

func TestRendersWithHonestWording(t *testing.T) {
	srv := server(t,
		day("cindernote", "2026-09-29", 10, stats.Page{Kind: "view", PageKey: "/notes/:id", Hits: 20, Uniques: 10}),
		day("cindernote", "2026-10-01", 30),
		day("persona", "2026-10-01", 5))
	st, _, body := get(t, srv, "/dashboard/?product=cindernote&days=7")
	if st != 200 {
		t.Fatalf("status %d", st)
	}
	for _, want := range []string{"Visitor-days", "visitor-days", "/notes/:id", "2026-09-25 to 2026-10-01", "cindernote"} {
		if !strings.Contains(body, want) {
			t.Errorf("page lacks %q", want)
		}
	}
	// Multi-day figures are never called unique visitors.
	if regexp.MustCompile(`(?i)unique visitors`).MatchString(body) {
		t.Error(`page says "unique visitors"`)
	}
	// 7 days in the table, the missing ones as zero rows.
	if n := strings.Count(body, `<tr><th scope="row">`); n != 7 {
		t.Errorf("daily table rows = %d, want 7", n)
	}
	// Average of 10 + 30 over 7 days.
	if !strings.Contains(body, `<div class="tile-value">5</div>`) {
		t.Error("visitors-per-day tile should average over every day in range (40/7 → 5)")
	}
}

func TestEscapesProductData(t *testing.T) {
	evil := `/<script>alert(1)</script>`
	srv := server(t, day("cindernote", "2026-10-01", 3, stats.Page{Kind: "view", PageKey: evil, Hits: 3, Uniques: 3}))
	_, _, body := get(t, srv, "/dashboard/?view=history&days=30")
	if strings.Contains(body, "<script>alert") {
		t.Fatal("a page key reached the HTML unescaped")
	}
	if !strings.Contains(body, "&lt;script&gt;alert(1)&lt;/script&gt;") {
		t.Error("expected the escaped page key")
	}
}

func TestParams(t *testing.T) {
	srv := server(t, day("cindernote", "2026-10-01", 3))
	if st, _, _ := get(t, srv, "/dashboard/?product=nobody"); st != 404 {
		t.Errorf("unknown product: %d", st)
	}
	if _, _, body := get(t, srv, "/dashboard/?days=13"); !strings.Contains(body, "2026-09-02 to 2026-10-01") {
		t.Error("an unsupported range must fall back to 30 days")
	}
	if st, _, _ := get(t, srv, "/dashboard/?product=cindernote&days=90"); st != 200 {
		t.Errorf("90 days: %d", st)
	}
	resp, err := (&http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}).Get(srv.URL + "/dashboard")
	if err != nil || resp.StatusCode != 301 || resp.Header.Get("Location") != "/dashboard/" {
		t.Errorf("/dashboard redirect: %v %v", resp.StatusCode, err)
	}
}

// Everything the dashboard serves is same-origin: no CDN, font, or any
// other third-party URL (the spirit of C001, applied to the operator).
func TestNoThirdPartyReferences(t *testing.T) {
	srv := server(t, day("cindernote", "2026-10-01", 3))
	external := regexp.MustCompile(`(?i)(https?:)?//[a-z0-9.-]+\.[a-z]{2,}`)
	for _, path := range []string{"/dashboard/", "/dashboard/static/dashboard.css", "/dashboard/static/dashboard.js"} {
		st, h, body := get(t, srv, path)
		if st != 200 {
			t.Errorf("%s: %d", path, st)
		}
		if m := external.FindString(body); m != "" {
			t.Errorf("%s references %q", path, m)
		}
		if h.Get("Content-Security-Policy") == "" {
			t.Errorf("%s: no CSP", path)
		}
	}
}

func TestLineChartTicks(t *testing.T) {
	start, _ := time.Parse(time.DateOnly, "2026-07-02")
	for _, n := range []int{1, 7, 30, 90} {
		dates := make([]time.Time, n)
		vals := make([]int, n)
		for i := range dates {
			dates[i] = start.AddDate(0, 0, i)
			vals[i] = i * 3
		}
		c := newLineChart("t", "", dates, vals)
		if len(c.Points) != n || c.End.Value != comma((n-1)*3) {
			t.Errorf("n=%d: %d points, end %q", n, len(c.Points), c.End.Value)
		}
		last := c.XTicks[len(c.XTicks)-1]
		if last.Label != dates[n-1].Format("2 Jan") {
			t.Errorf("n=%d: last x tick %q, want the final day", n, last.Label)
		}
		for i := 1; i < len(c.XTicks); i++ {
			if gap := c.XTicks[i].Pos - c.XTicks[i-1].Pos; gap < 9.9 {
				t.Errorf("n=%d: x ticks %q and %q only %.1f%% apart", n, c.XTicks[i-1].Label, c.XTicks[i].Label, gap)
			}
		}
		if top := c.YTicks[0]; top.Pos != 100 || top.Label != "0" {
			t.Errorf("n=%d: first y tick = %+v, want 0 at the baseline", n, top)
		}
	}
}

func TestFormatting(t *testing.T) {
	for n, want := range map[int]string{0: "0", 999: "999", 1284: "1,284", 1234567: "1,234,567"} {
		if got := comma(n); got != want {
			t.Errorf("comma(%d) = %q", n, got)
		}
	}
	for n, want := range map[int]string{1284: "1,284", 9999: "9,999", 12900: "12.9K", 18000: "18K", 4200000: "4.2M"} {
		if got := compact(n); got != want {
			t.Errorf("compact(%d) = %q, want %q", n, got, want)
		}
	}
	for max, wantTop := range map[int]int{0: 4, 3: 3, 671: 800, 1570: 2000, 3300: 4000} {
		if top, _ := niceScale(max); top != wantTop {
			t.Errorf("niceScale(%d) top = %d, want %d", max, top, wantTop)
		}
	}
}

func TestHistogramExplains(t *testing.T) {
	items := []stats.RangeItem{{Label: "1", Count: 58}, {Label: "2", Count: 30}, {Label: "4-5", Count: 12}}
	day := histogram("pages", items, false)
	if day.Title != "How many different pages did people open?" || day.Axis != "different pages opened" || day.MultiDay != "" {
		t.Errorf("single-day copy = %+v", day)
	}
	if day.Takeaway != "Most common: 1 page, 58% of visitors." {
		t.Errorf("takeaway = %q", day.Takeaway)
	}
	if c := day.Columns[2]; c.Tip != "4-5 pages" || c.Share != "12%" {
		t.Errorf("tooltip = %q / %q", c.Tip, c.Share)
	}
	multi := histogram("visits", []stats.RangeItem{{Label: "1", Count: 3}, {Label: "2", Count: 9}}, true)
	if multi.Unit != "visitor-days" || !strings.Contains(multi.MultiDay, "visitor-days") || multi.Takeaway != "Most common: 2 visits, 75% of visitor-days." {
		t.Errorf("multi-day = %+v", multi)
	}
	if empty := histogram("views", nil, true); empty.Takeaway != "" {
		t.Errorf("no data must give no takeaway, got %q", empty.Takeaway)
	}
}

func TestViewTabs(t *testing.T) {
	srv := server(t, day("cindernote", "2026-10-01", 30, stats.Page{Kind: "view", PageKey: "/", Hits: 40, Uniques: 30}),
		day("cindernote", "2026-09-30", 10))
	_, _, body := get(t, srv, "/dashboard/")
	// One row of views, in this order; the old History/range rows are gone.
	order := []string{">Issues", ">Last day so far<", ">Last day<", ">Last 3 days<", ">Last 7 days<", ">Last 30 days<", ">Last 90 days<"}
	at := -1
	for _, label := range order {
		i := strings.Index(body, label)
		if i < 0 || i < at {
			t.Fatalf("tab %q missing or out of order", label)
		}
		at = i
	}
	if strings.Contains(body, ">History<") || strings.Contains(body, `aria-label="Date range"`) {
		t.Error("the History tab and range row should be gone")
	}
	// The product list sits below the tabs.
	if strings.Index(body, `aria-label="Product"`) < strings.Index(body, ">Last 90 days<") {
		t.Error("product list must come after the view tabs")
	}
	// The default view is the live one.
	if !strings.Contains(body, `aria-current="page">Last day so far<`) {
		t.Error("default view should be Last day so far")
	}
}

func TestLastDayView(t *testing.T) {
	srv := server(t, day("cindernote", "2026-10-01", 30, stats.Page{Kind: "view", PageKey: "/", Hits: 40, Uniques: 30}),
		day("cindernote", "2026-09-30", 10))
	_, _, body := get(t, srv, "/dashboard/?view=history&days=1")
	if !strings.Contains(body, "2026-10-01 (UTC), the last closed day") {
		t.Error("last day view should name the single closed day")
	}
	if strings.Contains(body, `class="line-chart"`) {
		t.Error("a single day has no trend lines")
	}
	if !strings.Contains(body, `<div class="tile-label">Visitors</div>`) || strings.Contains(strings.ToLower(body), "visitor-days") {
		t.Error("one day's distinct visitors are 'Visitors', not visitor-days")
	}
	if strings.Contains(body, "visitor-days ·") {
		t.Error("bar tooltips for one day should say visitors")
	}
	// Old-style links keep working: a bare ?days=N means that range.
	if _, _, old := get(t, srv, "/dashboard/?days=7"); !strings.Contains(old, `aria-current="page">Last 7 days<`) {
		t.Error("?days=7 should open Last 7 days")
	}
}
