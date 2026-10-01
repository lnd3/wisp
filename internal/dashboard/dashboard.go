// Package dashboard is wisp's read-only operator dashboard over the
// product statistics DB (plan/projects/P001 Phase 3).
//
// It is server-rendered HTML with inline SVG and a few lines of embedded
// JS/CSS: no third-party requests (plan/concepts/C001 applies to the
// operator too), no cookies, no storage. Selection lives in the URL.
// Access control is Caddy's basic_auth in front of /dashboard/ (see
// deploy/wisp-caddy/Caddyfile); wisp itself is only reachable from Caddy.
//
// Wording is part of the privacy design: a per-day figure is "visitors",
// a figure summed over days is "visitor-days" — never "unique visitors",
// because the same person on two days is two different keys, by design.
package dashboard

import (
	"context"
	"embed"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"time"

	"github.com/lnd3/wisp/internal/events"
	"github.com/lnd3/wisp/internal/stats"
)

//go:embed templates/*.html
var templateFS embed.FS

//go:embed static
var staticFS embed.FS

// Ranges are the offered date ranges, in days, ending at the latest
// closed day.
var Ranges = []int{7, 30, 90}

const (
	defaultRange = 30
	topN         = 10
	agentN       = 6
)

// Handler serves the dashboard.
type Handler struct {
	Stats *stats.DB
	Log   *log.Logger // internal failures only
	// Optional, for the "Today so far" and "Issues" tabs:
	Today      TodaySource     // today's open staging, summarized keylessly
	Events     *events.Log     // issue log + per-product ingest status
	Registered func() []string // registered product keys
	Now        func() time.Time
}

// Views are the dashboard's tabs.
var Views = []string{"history", "today", "issues"}

var tmpl = template.Must(template.New("page.html").Funcs(template.FuncMap{
	"pct": func(f float64) string { return strconv.FormatFloat(f, 'f', 2, 64) },
}).ParseFS(templateFS, "templates/page.html"))

// Register adds the dashboard's routes to mux.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /dashboard", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/dashboard/", http.StatusMovedPermanently)
	})
	mux.HandleFunc("GET /dashboard/{$}", h.page)
	static, _ := fs.Sub(staticFS, "static")
	files := http.StripPrefix("/dashboard/static/", http.FileServer(http.FS(static)))
	mux.Handle("GET /dashboard/static/", securityHeaders(files))
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		// Everything is same-origin; style-src-attr allows the bar/position
		// percentages html/template writes into style attributes.
		h.Set("Content-Security-Policy", "default-src 'none'; style-src 'self'; style-src-attr 'unsafe-inline'; script-src 'self'; img-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

func (h *Handler) page(w http.ResponseWriter, r *http.Request) {
	securityHeaders(http.HandlerFunc(h.render)).ServeHTTP(w, r)
}

func (h *Handler) render(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	now := time.Now().UTC()
	if h.Now != nil {
		now = h.Now().UTC()
	}
	products, err := h.Stats.Products(ctx)
	if err != nil {
		h.fail(w, err)
		return
	}
	// The selector offers every product with closed days, open staging
	// today, or a registration — so a newly wired product shows up on
	// "Today so far" before its first day closes.
	var registered []string
	if h.Registered != nil {
		registered = h.Registered()
	}
	all := append([]string(nil), products...)
	all = append(all, registered...)
	if h.Today != nil {
		if days, err := h.Today.Days(); err == nil {
			for _, d := range days {
				all = append(all, d.Product)
			}
		}
	}
	slices.Sort(all)
	all = slices.Compact(all)

	product := r.URL.Query().Get("product")
	if product != "" && !slices.Contains(all, product) {
		http.Error(w, "unknown product", http.StatusNotFound)
		return
	}
	days := defaultRange
	if d, err := strconv.Atoi(r.URL.Query().Get("days")); err == nil && slices.Contains(Ranges, d) {
		days = d
	}
	viewName := r.URL.Query().Get("view")
	if !slices.Contains(Views, viewName) {
		viewName = "history"
	}

	v, err := h.build(ctx, all, product, days, viewName)
	if err != nil {
		h.fail(w, err)
		return
	}
	switch viewName {
	case "today":
		err = h.buildToday(ctx, v, product, registered, now)
	case "issues":
		h.buildIssues(v)
	}
	if err != nil {
		h.fail(w, err)
		return
	}
	v.Errors, v.Warnings = h.Events.Counts(now.Add(-24 * time.Hour))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.Execute(w, v); err != nil && h.Log != nil {
		h.Log.Printf("dashboard: render: %v", err)
	}
}

func (h *Handler) fail(w http.ResponseWriter, err error) {
	if h.Log != nil {
		h.Log.Printf("dashboard: %v", err)
	}
	h.Events.Record(events.Error, "dashboard", "", "a dashboard page failed to render (see wisp's log)")
	http.Error(w, "internal error", http.StatusInternalServerError)
}

// ---- view model ----

type link struct {
	Label    string
	URL      string
	Selected bool
}

type tile struct {
	Label string
	Value string
	Note  string
}

type view struct {
	View         string // "history" | "today" | "issues"
	Tabs         []link
	Errors       int // distinct error entries seen in the last 24h
	Warnings     int
	Today        *todayView
	Issues       []issueRow
	IssuesSince  string
	Products     []link
	Ranges       []link
	ProductLabel string
	HasData      bool
	From, To     string
	Days         int
	Tiles        []tile
	Visitors     lineChart
	Views        lineChart
	Daily        []stats.DayTotals // table view, most recent first
	TopPages     barList
	Downloads    barList
	Referrers    barList
	Browsers     barList
	OSes         barList
	Devices      barList
	PagesHist    columnChart
	ViewsHist    columnChart
	VisitsHist   columnChart
}

func (h *Handler) build(ctx context.Context, products []string, product string, days int, viewName string) (*view, error) {
	v := &view{Days: days, ProductLabel: "All products", View: viewName}
	if product != "" {
		v.ProductLabel = product
	}
	hrefView := func(p string, d int, vn string) string {
		q := url.Values{"days": {strconv.Itoa(d)}}
		if p != "" {
			q.Set("product", p)
		}
		if vn != "history" {
			q.Set("view", vn)
		}
		return "/dashboard/?" + q.Encode()
	}
	href := func(p string, d int) string { return hrefView(p, d, viewName) }
	for _, t := range []struct{ name, label string }{{"history", "History"}, {"today", "Today so far"}, {"issues", "Issues"}} {
		v.Tabs = append(v.Tabs, link{t.label, hrefView(product, days, t.name), t.name == viewName})
	}
	if viewName != "history" {
		// Only the history tab reads closed days; skip its queries.
		v.Products = append(v.Products, link{"All products", href("", days), product == ""})
		for _, p := range products {
			v.Products = append(v.Products, link{p, href(p, days), p == product})
		}
		return v, nil
	}
	v.Products = append(v.Products, link{"All products", href("", days), product == ""})
	for _, p := range products {
		v.Products = append(v.Products, link{p, href(p, days), p == product})
	}
	for _, d := range Ranges {
		v.Ranges = append(v.Ranges, link{fmt.Sprintf("Last %d days", d), href(product, d), d == days})
	}

	latest, err := h.Stats.LatestDay(ctx, product)
	if err != nil || latest == "" {
		return v, err
	}
	to, _ := time.Parse(time.DateOnly, latest)
	from := to.AddDate(0, 0, -(days - 1))
	v.HasData, v.From, v.To = true, from.Format(time.DateOnly), latest

	rows, err := h.Stats.DailyTotals(ctx, product, v.From, v.To)
	if err != nil {
		return nil, err
	}
	byDate := make(map[string]stats.DayTotals, len(rows))
	for _, r := range rows {
		byDate[r.Date] = r
	}
	var dates []time.Time
	var visitors, views []int
	var sum stats.DayTotals
	for d := from; !d.After(to); d = d.AddDate(0, 0, 1) {
		r := byDate[d.Format(time.DateOnly)]
		r.Date = d.Format(time.DateOnly)
		dates = append(dates, d)
		visitors = append(visitors, r.Uniques)
		views = append(views, r.Views)
		sum.Uniques += r.Uniques
		sum.Visits += r.Visits
		sum.Views += r.Views
		sum.Downloads += r.Downloads
		sum.Bounces += r.Bounces
		v.Daily = append([]stats.DayTotals{r}, v.Daily...)
	}

	bounceRate := "–"
	if sum.Uniques > 0 {
		bounceRate = fmt.Sprintf("%.0f%%", 100*float64(sum.Bounces)/float64(sum.Uniques))
	}
	v.Tiles = []tile{
		{"Visitors per day", compact(sum.Uniques / len(dates)), "average; a deliberate lower bound"},
		{"Visitor-days", compact(sum.Uniques), "daily visitors summed — not unique people"},
		{"Views", compact(sum.Views), ""},
		{"Visits", compact(sum.Visits), "a new visit after 30 min idle"},
		{"Downloads", compact(sum.Downloads), ""},
		{"Single-view visitors", bounceRate, "share of visitor-days with one view"},
	}
	v.Visitors = newLineChart("Visitors per day", "Distinct visitors each day — a deliberate lower bound", dates, visitors)
	v.Views = newLineChart("Views per day", "", dates, views)

	type ranked struct {
		dst                  *barList
		title, sub, countLbl string
		load                 func() ([]stats.RangeItem, error)
	}
	for _, q := range []ranked{
		{&v.TopPages, "Top pages", "Views, and visitor-days per page", "views", func() ([]stats.RangeItem, error) {
			return h.Stats.TopPages(ctx, product, v.From, v.To, "view", topN)
		}},
		{&v.Downloads, "Downloads", "Downloads, and visitor-days per file", "downloads", func() ([]stats.RangeItem, error) {
			return h.Stats.TopPages(ctx, product, v.From, v.To, "download", topN)
		}},
		{&v.Referrers, "Referrers", "Visits arriving from another site (host only)", "visits", func() ([]stats.RangeItem, error) {
			return h.Stats.TopReferrers(ctx, product, v.From, v.To, topN)
		}},
		{&v.Browsers, "Browsers", "Visitor-days", "visitor-days", func() ([]stats.RangeItem, error) {
			return h.Stats.Agents(ctx, product, v.From, v.To, "browser", agentN)
		}},
		{&v.OSes, "Operating systems", "Visitor-days", "visitor-days", func() ([]stats.RangeItem, error) {
			return h.Stats.Agents(ctx, product, v.From, v.To, "os", agentN)
		}},
		{&v.Devices, "Devices", "Visitor-days", "visitor-days", func() ([]stats.RangeItem, error) {
			return h.Stats.Agents(ctx, product, v.From, v.To, "device", agentN)
		}},
	} {
		items, err := q.load()
		if err != nil {
			return nil, err
		}
		*q.dst = newBarList(q.title, q.sub, q.countLbl, items, q.countLbl != "visitor-days")
	}

	for _, q := range []struct {
		dst    *columnChart
		metric string
		title  string
	}{
		{&v.PagesHist, "pages", ""},
		{&v.ViewsHist, "views", ""},
		{&v.VisitsHist, "visits", ""},
	} {
		items, err := h.Stats.Histogram(ctx, product, v.From, v.To, q.metric)
		if err != nil {
			return nil, err
		}
		*q.dst = histogram(q.metric, items, days > 1)
	}
	return v, nil
}
