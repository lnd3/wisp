package dashboard

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/lnd3/wisp/internal/events"
	"github.com/lnd3/wisp/internal/staging"
	"github.com/lnd3/wisp/internal/stats"
)

// TodaySource is the slice of *staging.Store the "today so far" tab uses.
// Summarize is the same keyless aggregation the day close runs: visitor
// keys are only grouped by inside it, never returned — so the dashboard
// never sees one, before or after a day closes.
type TodaySource interface {
	Days() ([]staging.Day, error)
	Summarize(ctx context.Context, d staging.Day) (*stats.Day, error)
}

type ingestRow struct {
	Product string
	Last    string
	Batches int
	Stale   bool
}

type issueRow struct {
	Level   string // "error" | "warning"
	Label   string // "Error" | "Warning"
	Source  string
	Product string
	Message string
	Count   int
	First   string
	Last    string
}

type todayView struct {
	Date        string
	HasData     bool
	OpenEarlier []string // earlier days still open (before their close)
	Tiles       []tile
	TopPages    barList
	Downloads   barList
	Referrers   barList
	Browsers    barList
	OSes        barList
	Devices     barList
	PagesHist   columnChart
	ViewsHist   columnChart
	VisitsHist  columnChart
	Ingest      []ingestRow
	Started     string
}

func (h *Handler) buildToday(ctx context.Context, v *view, product string, registered []string, now time.Time) error {
	t := &todayView{Date: now.Format(time.DateOnly)}
	v.Today = t
	if h.Events != nil {
		t.Started = h.Events.Started().Format("2006-01-02 15:04 UTC")
	}
	t.Ingest = ingestRows(h.Events, registered, now)
	if h.Today == nil {
		return nil
	}
	days, err := h.Today.Days()
	if err != nil {
		return err
	}
	var parts []*stats.Day
	seenEarlier := map[string]bool{}
	for _, d := range days {
		if product != "" && d.Product != product {
			continue
		}
		if d.Date < t.Date {
			if !seenEarlier[d.Date] {
				seenEarlier[d.Date] = true
				t.OpenEarlier = append(t.OpenEarlier, d.Date)
			}
			continue
		}
		if d.Date != t.Date {
			continue
		}
		s, err := h.Today.Summarize(ctx, d)
		if err != nil {
			return err
		}
		parts = append(parts, s)
	}
	if len(parts) == 0 {
		return nil
	}
	t.HasData = true
	sum := mergeDays(parts)

	visitorsLabel, visitorsNote := "Visitors today", "distinct, so far; a deliberate lower bound"
	if len(parts) > 1 {
		visitorsLabel, visitorsNote = "Visitors today", "summed across products — not unique people"
	}
	bounce := "–"
	if sum.Uniques > 0 {
		bounce = fmt.Sprintf("%.0f%%", 100*float64(sum.Bounces)/float64(sum.Uniques))
	}
	t.Tiles = []tile{
		{visitorsLabel, compact(sum.Uniques), visitorsNote},
		{"Views", compact(sum.Views), ""},
		{"Visits", compact(sum.Visits), "a new visit after 30 min idle"},
		{"Downloads", compact(sum.Downloads), ""},
		{"Single-view visitors", bounce, "share of today's visitors with one view"},
	}

	pages := func(kind string) []stats.RangeItem {
		var out []stats.RangeItem
		for _, p := range sum.Pages {
			if p.Kind == kind {
				out = append(out, stats.RangeItem{Label: p.PageKey, Count: p.Hits, VisitorDays: p.Uniques})
			}
		}
		return topItems(out)
	}
	var refs []stats.RangeItem
	for _, r := range sum.Referrers {
		refs = append(refs, stats.RangeItem{Label: r.Host, Count: r.Visits, VisitorDays: r.Uniques})
	}
	agents := func(dim string) []stats.RangeItem {
		var out []stats.RangeItem
		for _, a := range sum.Agents {
			if a.Dim == dim {
				out = append(out, stats.RangeItem{Label: a.Value, Count: a.Uniques, VisitorDays: a.Uniques})
			}
		}
		return topItems(out)[:min(agentN, len(out))]
	}
	t.TopPages = withUnit(newBarList("Top pages", "Views, and visitors per page", "views", pages("view"), true), "visitors")
	t.Downloads = withUnit(newBarList("Downloads", "Downloads, and visitors per file", "downloads", pages("download"), true), "visitors")
	t.Referrers = withUnit(newBarList("Referrers", "Visits arriving from another site (host only)", "visits", topItems(refs), true), "visitors")
	t.Browsers = newBarList("Browsers", "Visitors", "visitors", agents("browser"), false)
	t.OSes = newBarList("Operating systems", "Visitors", "visitors", agents("os"), false)
	t.Devices = newBarList("Devices", "Visitors", "visitors", agents("device"), false)
	hist := func(metric, title string) columnChart {
		var out []stats.RangeItem
		for _, b := range sum.Hist {
			if b.Metric == metric {
				out = append(out, stats.RangeItem{Label: b.Bucket, Count: b.Visitors})
			}
		}
		sort.Slice(out, func(i, j int) bool { return stats.BucketOrder(out[i].Label) < stats.BucketOrder(out[j].Label) })
		c := newColumnChart(title, out)
		c.Subtitle, c.Unit = "Visitors in each bucket", "visitors"
		return c
	}
	t.PagesHist = hist("pages", "Distinct pages per visitor")
	t.ViewsHist = hist("views", "Views per visitor")
	t.VisitsHist = hist("visits", "Visits per visitor")
	return nil
}

// mergeDays sums closed-day-shaped summaries (one per product). Uniques
// summed across products are visitor sums, not unique people — each
// product keys visitors with its own salt.
func mergeDays(parts []*stats.Day) *stats.Day {
	if len(parts) == 1 {
		return parts[0]
	}
	out := &stats.Day{}
	pages := map[[2]string]*stats.Page{}
	refs := map[string]*stats.Referrer{}
	agents := map[[2]string]*stats.Agent{}
	hist := map[[2]string]*stats.Bucket{}
	for _, d := range parts {
		out.Uniques += d.Uniques
		out.Visits += d.Visits
		out.Views += d.Views
		out.Downloads += d.Downloads
		out.Bounces += d.Bounces
		for _, p := range d.Pages {
			k := [2]string{p.Kind, p.PageKey}
			if pages[k] == nil {
				pages[k] = &stats.Page{Kind: p.Kind, PageKey: p.PageKey}
			}
			pages[k].Hits += p.Hits
			pages[k].Uniques += p.Uniques
		}
		for _, r := range d.Referrers {
			if refs[r.Host] == nil {
				refs[r.Host] = &stats.Referrer{Host: r.Host}
			}
			refs[r.Host].Visits += r.Visits
			refs[r.Host].Uniques += r.Uniques
		}
		for _, a := range d.Agents {
			k := [2]string{a.Dim, a.Value}
			if agents[k] == nil {
				agents[k] = &stats.Agent{Dim: a.Dim, Value: a.Value}
			}
			agents[k].Uniques += a.Uniques
		}
		for _, b := range d.Hist {
			k := [2]string{b.Metric, b.Bucket}
			if hist[k] == nil {
				hist[k] = &stats.Bucket{Metric: b.Metric, Bucket: b.Bucket}
			}
			hist[k].Visitors += b.Visitors
		}
	}
	for _, p := range pages {
		out.Pages = append(out.Pages, *p)
	}
	for _, r := range refs {
		out.Referrers = append(out.Referrers, *r)
	}
	for _, a := range agents {
		out.Agents = append(out.Agents, *a)
	}
	for _, b := range hist {
		out.Hist = append(out.Hist, *b)
	}
	return out
}

// topItems sorts by count descending (then label) and keeps topN.
func topItems(items []stats.RangeItem) []stats.RangeItem {
	sort.Slice(items, func(i, j int) bool {
		if items[i].Count != items[j].Count {
			return items[i].Count > items[j].Count
		}
		return items[i].Label < items[j].Label
	})
	return items[:min(topN, len(items))]
}

func withUnit(b barList, unit string) barList {
	b.UniqueUnit = unit
	return b
}

// ingestRows lists every registered product, plus any that delivered
// without being registered now (e.g. removed since), with its last
// accepted batch since wisp started.
func ingestRows(l *events.Log, registered []string, now time.Time) []ingestRow {
	delivered := l.Deliveries()
	seen := map[string]bool{}
	var rows []ingestRow
	add := func(p string) {
		if seen[p] {
			return
		}
		seen[p] = true
		r := ingestRow{Product: p, Last: "none since start"}
		if d, ok := delivered[p]; ok {
			r.Last = d.Last.Format("15:04 UTC") + " (" + ago(now.Sub(d.Last)) + ")"
			r.Batches = d.Batches
		}
		rows = append(rows, r)
	}
	for _, p := range registered {
		add(p)
	}
	var extra []string
	for p := range delivered {
		extra = append(extra, p)
	}
	sort.Strings(extra)
	for _, p := range extra {
		add(p)
	}
	return rows
}

func ago(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%d min ago", int(d.Minutes()))
	default:
		return fmt.Sprintf("%.1f h ago", d.Hours())
	}
}

func (h *Handler) buildIssues(v *view) {
	for _, e := range h.Events.Entries() {
		label := "Warning"
		if e.Level == events.Error {
			label = "Error"
		}
		product := e.Product
		if product == "" {
			product = "—"
		}
		v.Issues = append(v.Issues, issueRow{
			Level: e.Level, Label: label, Source: e.Source, Product: product, Message: e.Message,
			Count: e.Count, First: e.First.Format("Jan 2 15:04"), Last: e.Last.Format("Jan 2 15:04"),
		})
	}
	if h.Events != nil {
		v.IssuesSince = h.Events.Started().Format("2006-01-02 15:04 UTC")
	}
}
