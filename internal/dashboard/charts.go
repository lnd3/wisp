package dashboard

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/lnd3/wisp/internal/stats"
)

// Chart geometry is computed here and rendered by the template. Line
// plots draw in a 1000×100 SVG user space stretched to the container
// (preserveAspectRatio="none", non-scaling strokes); everything that
// must not stretch — labels, the hover dot — is HTML positioned by
// percentage, so text stays legible at any width.

type tick struct {
	Pos   float64 // percent: from the top (y) or from the left (x)
	Label string
}

type point struct {
	X, Y     float64 // percent within the plot
	HitLeft  float64 // percent
	HitWidth float64 // percent
	Date     string  // e.g. "Tue 1 Oct"
	Value    string
	Zero     bool
}

type lineChart struct {
	Title, Subtitle string
	Line, Area      string // SVG path data in the 1000×100 space
	YTicks          []tick
	XTicks          []tick
	Points          []point
	End             point // direct label at the line's end
}

func newLineChart(title, subtitle string, dates []time.Time, values []int) lineChart {
	c := lineChart{Title: title, Subtitle: subtitle}
	n := len(values)
	if n == 0 {
		return c
	}
	max := 0
	for _, v := range values {
		max = int(math.Max(float64(max), float64(v)))
	}
	top, step := niceScale(max)
	for t := 0; t <= top; t += step {
		c.YTicks = append(c.YTicks, tick{Pos: 100 - 100*float64(t)/float64(top), Label: comma(t)})
	}

	xAt := func(i int) float64 {
		if n == 1 {
			return 50
		}
		return 100 * float64(i) / float64(n-1)
	}
	var line, area strings.Builder
	spacing := 100.0
	if n > 1 {
		spacing = 100 / float64(n-1)
	}
	for i, v := range values {
		x, y := xAt(i), 100-100*float64(v)/float64(top)
		cmd := "L"
		if i == 0 {
			cmd = "M"
			fmt.Fprintf(&area, "M%.2f,100 ", x*10)
		}
		fmt.Fprintf(&line, "%s%.2f,%.2f ", cmd, x*10, y)
		fmt.Fprintf(&area, "L%.2f,%.2f ", x*10, y)
		left := math.Max(0, x-spacing/2)
		right := math.Min(100, x+spacing/2)
		c.Points = append(c.Points, point{
			X: x, Y: y, HitLeft: left, HitWidth: right - left,
			Date: dates[i].Format("Mon 2 Jan"), Value: comma(v), Zero: v == 0,
		})
	}
	fmt.Fprintf(&area, "L%.2f,100 Z", xAt(n-1)*10)
	c.Line, c.Area = strings.TrimSpace(line.String()), area.String()
	c.End = c.Points[n-1]

	// About six evenly spaced date labels, always including both ends.
	every := int(math.Max(1, math.Ceil(float64(n-1)/5)))
	for i := 0; i < n; i += every {
		c.XTicks = append(c.XTicks, tick{Pos: xAt(i), Label: dates[i].Format("2 Jan")})
	}
	if last := n - 1; last%every != 0 {
		// The end label always shows; if the regular tick before it would
		// sit within half a step, drop that one instead of colliding.
		if last%every < (every+1)/2 {
			c.XTicks = c.XTicks[:len(c.XTicks)-1]
		}
		c.XTicks = append(c.XTicks, tick{Pos: xAt(last), Label: dates[last].Format("2 Jan")})
	}
	return c
}

// niceScale returns an axis top ≥ max and a round step (1, 2 or 5 × 10^k)
// giving about four intervals.
func niceScale(max int) (top, step int) {
	if max <= 0 {
		return 4, 1
	}
	raw := float64(max) / 4
	mag := math.Pow(10, math.Floor(math.Log10(raw)))
	for _, m := range []float64{1, 2, 5, 10} {
		if m*mag >= raw {
			step = int(math.Max(1, m*mag))
			break
		}
	}
	top = int(math.Ceil(float64(max)/float64(step))) * step
	return top, step
}

type barRow struct {
	Label       string
	Short       string // truncated for display; Label carries the full text
	Count       string
	VisitorDays string
	Pct         float64 // fill width, percent of the track
}

type barList struct {
	Title, Subtitle string
	CountLabel      string
	ShowVisitorDays bool
	UniqueUnit      string // "visitor-days" (over a range) or "visitors" (one day)
	Rows            []barRow
}

func newBarList(title, subtitle, countLabel string, items []stats.RangeItem, showVisitorDays bool) barList {
	b := barList{Title: title, Subtitle: subtitle, CountLabel: countLabel, ShowVisitorDays: showVisitorDays, UniqueUnit: "visitor-days"}
	max := 0
	for _, it := range items {
		max = int(math.Max(float64(max), float64(it.Count)))
	}
	for _, it := range items {
		pct := 0.0
		if max > 0 && it.Count > 0 {
			// The longest bar stops short of the track's end so its value
			// label always fits at the tip; a non-zero count stays visible.
			pct = math.Max(78*float64(it.Count)/float64(max), 0.5)
		}
		b.Rows = append(b.Rows, barRow{
			Label: it.Label, Short: truncate(it.Label, 48),
			Count: comma(it.Count), VisitorDays: comma(it.VisitorDays), Pct: pct,
		})
	}
	return b
}

type column struct {
	Label string
	Value string
	Pct   float64 // height, percent of the plot
	Tip   string  // tooltip label, e.g. "4-5 pages"
	Share string  // tooltip share, e.g. "14%"
}

type columnChart struct {
	Title    string // a plain question, e.g. "How many different pages did people open?"
	Subtitle string // what one bar counts
	MultiDay string // extra note when summed over days (visitor-days); "" for a single day
	Axis     string // x-axis caption, e.g. "different pages opened"
	Takeaway string // e.g. "Most common: 1 page, 58% of visitors."
	Unit     string // "visitors" or "visitor-days"
	Columns  []column
}

// histogramCopy is the wording for each per-visitor histogram, kept in
// one place so the History and Today tabs explain them identically.
var histogramCopy = map[string]struct{ title, what, axis, one, many string }{
	"pages": {
		"How many different pages did people open?",
		"Each bar counts visitors by how many different pages they opened in a day.",
		"different pages opened", "page", "pages",
	},
	"views": {
		"How many page views did each visitor make?",
		"Counts every page view, including repeat views of the same page.",
		"page views", "view", "views",
	},
	"visits": {
		"Did people come back later the same day?",
		"A visit ends after 30 min idle, so 2 or more means they came back later that day.",
		"visits that day", "visit", "visits",
	},
}

// histogram builds one of the per-visitor histograms. multiDay marks a
// range of days, where each visitor counts once per day (visitor-days).
func histogram(metric string, items []stats.RangeItem, multiDay bool) columnChart {
	cp := histogramCopy[metric]
	c := newColumnChart(cp.title, items)
	c.Subtitle, c.Axis, c.Unit = cp.what, cp.axis, "visitors"
	if multiDay {
		c.Unit = "visitor-days"
		c.MultiDay = "Over several days, a visitor counts once per day they came (visitor-days), so the same person can appear in more than one bar."
	}
	total, best := 0, -1
	for i, it := range items {
		total += it.Count
		if best < 0 || it.Count > items[best].Count {
			best = i
		}
	}
	noun := func(bucket string) string {
		if bucket == "1" {
			return cp.one
		}
		return cp.many
	}
	for i := range c.Columns {
		c.Columns[i].Tip = c.Columns[i].Label + " " + noun(c.Columns[i].Label)
		if total > 0 {
			c.Columns[i].Share = fmt.Sprintf("%.0f%%", 100*float64(items[i].Count)/float64(total))
		}
	}
	if best >= 0 && total > 0 {
		c.Takeaway = fmt.Sprintf("Most common: %s %s, %.0f%% of %s.",
			items[best].Label, noun(items[best].Label), 100*float64(items[best].Count)/float64(total), c.Unit)
	}
	return c
}

func newColumnChart(title string, items []stats.RangeItem) columnChart {
	c := columnChart{Title: title, Unit: "visitor-days"}
	max := 0
	for _, it := range items {
		max = int(math.Max(float64(max), float64(it.Count)))
	}
	for _, it := range items {
		pct := 0.0
		if max > 0 && it.Count > 0 {
			pct = math.Max(100*float64(it.Count)/float64(max), 1)
		}
		c.Columns = append(c.Columns, column{Label: it.Label, Value: comma(it.Count), Pct: pct})
	}
	return c
}

// comma formats 1234567 as "1,234,567".
func comma(n int) string {
	s := strconv.Itoa(n)
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	var b strings.Builder
	for i, r := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(r)
	}
	if neg {
		return "-" + b.String()
	}
	return b.String()
}

// compact formats stat-tile values: 1,284 / 12.9K / 4.2M.
func compact(n int) string {
	switch {
	case n < 10_000:
		return comma(n)
	case n < 1_000_000:
		return strings.Replace(fmt.Sprintf("%.1fK", float64(n)/1e3), ".0K", "K", 1)
	default:
		return strings.Replace(fmt.Sprintf("%.1fM", float64(n)/1e6), ".0M", "M", 1)
	}
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}
