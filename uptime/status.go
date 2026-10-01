package uptime

import (
	"html/template"
	"net/http"
	"time"
)

var statusPage = template.Must(template.New("status").Funcs(template.FuncMap{
	"ago": func(t time.Time) string {
		if t.IsZero() {
			return "—"
		}
		return roundDur(time.Since(t)) + " ago"
	},
	"ms": func(d time.Duration) string {
		if d == 0 {
			return "—"
		}
		return d.Round(time.Millisecond).String()
	},
	"days": func(t time.Time) string {
		if t.IsZero() {
			return "—"
		}
		return roundDur(time.Until(t))
	},
}).Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<meta http-equiv="refresh" content="30"><title>uptime-wisp</title>
<style>
:root{color-scheme:light dark;--fg:#0b0b0b;--muted:#6e6e73;--line:#e1e0d9;--bg:#fcfcfb;--up:#0ca30c;--down:#d03b3b;--unk:#898781}
@media (prefers-color-scheme:dark){:root{--fg:#fff;--muted:#a1a09a;--line:#2c2c2a;--bg:#1a1a19}}
body{margin:0;background:var(--bg);color:var(--fg);font:14px/1.45 system-ui,sans-serif}
main{max-width:960px;margin:0 auto;padding:20px 16px}h1{font-size:18px;margin:0 0 4px}p{color:var(--muted);margin:0 0 16px}
table{border-collapse:collapse;width:100%}th,td{text-align:left;padding:6px 8px;border-top:1px solid var(--line);vertical-align:top}
th{color:var(--muted);font-weight:500;border-top:0}td.n{font-variant-numeric:tabular-nums;white-space:nowrap}
.s{font-weight:600;white-space:nowrap}.s::before{content:"";display:inline-block;width:9px;height:9px;border-radius:50%;margin-right:6px;background:var(--unk)}
.up::before{background:var(--up)}.down::before{background:var(--down)}.t{color:var(--muted);word-break:break-all}
</style></head><body><main>
<h1>uptime-wisp</h1><p>{{len .States}} checks every {{.Interval}} · last round {{ago .Last}} · refreshes every 30s</p>
<table><thead><tr><th>Status</th><th>Check</th><th>Detail</th><th>Latency</th><th>Since</th><th>Cert expires in</th></tr></thead><tbody>
{{range .States}}<tr><td class="s {{.Status}}">{{.Status}}</td><td>{{.Name}}<div class="t">{{.Target}}</div></td><td>{{.Detail}}</td><td class="n">{{ms .Latency}}</td><td class="n">{{ago .Since}}</td><td class="n">{{days .CertExpiry}}</td></tr>
{{end}}</tbody></table></main></body></html>`))

// Handler serves the status page at / and a health check at /healthz:
// 200 while rounds are completing, 503 once none has finished for three
// intervals (or before the first).
func (m *Monitor) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		states, last := m.Snapshot()
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'")
		statusPage.Execute(w, struct {
			States   []State
			Last     time.Time
			Interval time.Duration
		}{states, last, m.cfg.Interval.Duration})
	})
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		_, last := m.Snapshot()
		if last.IsZero() || m.now().Sub(last) > 3*m.cfg.Interval.Duration {
			http.Error(w, "no recent round", http.StatusServiceUnavailable)
			return
		}
		w.Write([]byte("ok\n"))
	})
	return mux
}
