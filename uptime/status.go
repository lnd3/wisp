package uptime

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"html/template"
	"net/http"
	"net/url"
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
.bar{display:flex;flex-wrap:wrap;gap:8px 16px;align-items:center;margin:0 0 12px}.bar p{margin:0}
button{font:inherit;padding:4px 12px;border-radius:6px;border:1px solid var(--line);background:transparent;color:var(--fg);cursor:pointer}
.rl{margin:0 0 12px;padding:6px 10px;border-radius:6px;border-left:3px solid var(--up)}.rl.err{border-left-color:var(--down)}
</style></head><body><main>
<h1>uptime-wisp</h1>
<div class="bar"><p>{{len .States}} checks every {{.Interval}} · last round {{ago .Last}} · refreshes every 30s</p>
<form method="post" action="/reload"><button type="submit" title="Re-read config.json and apply it; an invalid file is rejected and the running config kept">Reload config</button></form></div>
{{with .Conn}}{{if .Enabled}}{{if .OfflineSince.IsZero}}<p>connectivity ok: {{.AnchorsUp}}/{{.AnchorsTotal}} anchors answering</p>{{else}}<p class="rl err">uptime-wisp itself is OFFLINE since {{.OfflineSince.Format "15:04:05 UTC"}} (every connectivity anchor failed), so checks are paused, not judged: {{.Detail}}</p>{{end}}{{end}}{{end}}
{{with .Reload}}<p class="rl{{if not .OK}} err{{end}}">{{.Time.Format "15:04:05 UTC"}}: {{.Message}}</p>{{end}}
<table><thead><tr><th>Status</th><th>Check</th><th>Detail</th><th>Latency</th><th>Since</th><th>Cert expires in</th></tr></thead><tbody>
{{range .States}}<tr><td class="s {{.Status}}">{{.Status}}</td><td>{{.Name}}<div class="t">{{.Target}}</div></td><td>{{.Detail}}</td><td class="n">{{ms .Latency}}</td><td class="n">{{ago .Since}}</td><td class="n">{{days .CertExpiry}}</td></tr>
{{end}}</tbody></table></main></body></html>`))

// Handler serves the status page at / (behind Basic Auth when
// cfg.Auth is set) and a health check at /healthz (always open: it says
// only "ok", and Docker's health check calls it): 200 while rounds are
// completing, 503 once none has finished for three intervals (or before
// the first).
func (m *Monitor) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", m.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		states, last := m.Snapshot()
		m.mu.Lock()
		reload := m.lastReload
		m.mu.Unlock()
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; form-action 'self'")
		w.Header().Set("Cache-Control", "no-store")
		statusPage.Execute(w, struct {
			States   []State
			Last     time.Time
			Interval time.Duration
			Reload   *ReloadResult
			Conn     ConnState
		}{states, last, m.config().Interval.Duration, reload, m.Conn()})
	}))
	// Reload re-reads config.json. Behind the login, and refused for
	// cross-site requests: a browser would otherwise attach the cached
	// Basic Auth credentials to a POST another site makes.
	mux.HandleFunc("POST /reload", m.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		if crossSite(r) {
			http.Error(w, "cross-site request refused", http.StatusForbidden)
			return
		}
		m.Reload()
		http.Redirect(w, r, "/", http.StatusSeeOther)
	}))
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		_, last := m.Snapshot()
		if last.IsZero() || m.now().Sub(last) > 3*m.config().Interval.Duration {
			http.Error(w, "no recent round", http.StatusServiceUnavailable)
			return
		}
		w.Write([]byte("ok\n"))
	})
	return mux
}

// requireAuth wraps h in HTTP Basic Auth when the current config has
// auth. Read per request, so a reload can change the credentials. Both
// the user and the password hash are compared in constant time.
func (m *Monitor) requireAuth(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		a := m.config().Auth
		if a == nil {
			h(w, r)
			return
		}
		wantUser := sha256.Sum256([]byte(a.User))
		wantPass, _ := hex.DecodeString(a.PasswordSHA256)
		user, pass, ok := r.BasicAuth()
		gotUser := sha256.Sum256([]byte(user))
		gotPass := sha256.Sum256([]byte(pass))
		userOK := subtle.ConstantTimeCompare(gotUser[:], wantUser[:])
		passOK := subtle.ConstantTimeCompare(gotPass[:], wantPass)
		if !ok || userOK&passOK != 1 {
			w.Header().Set("WWW-Authenticate", `Basic realm="uptime-wisp", charset="UTF-8"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		h(w, r)
	}
}

// crossSite reports a request made by another site's page: the browser's
// Sec-Fetch-Site says so, or an Origin header names a different host.
func crossSite(r *http.Request) bool {
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" && site != "none" {
		return true
	}
	if origin := r.Header.Get("Origin"); origin != "" {
		u, err := url.Parse(origin)
		if err != nil || u.Host != r.Host {
			return true
		}
	}
	return false
}
