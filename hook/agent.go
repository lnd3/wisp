package hook

import "strings"

// Agent is the coarse browser/OS/device family derived from a
// User-Agent — never the User-Agent itself.
type Agent struct {
	Browser string `json:"browser"`
	OS      string `json:"os"`
	Device  string `json:"device"`
}

// botMarkers are lowercase User-Agent substrings that mark automated
// clients. Deliberately broad: a real visitor misclassified as a bot is
// an under-count, which the lower-bound design tolerates; a bot counted
// as a visitor is noise it doesn't.
var botMarkers = []string{
	"bot", "crawl", "spider", "slurp", "scrape", "headless", "lighthouse",
	"preview", "monitor", "uptime", "pingdom", "facebookexternalhit",
	"curl/", "wget/", "httpie/", "python-", "python/", "go-http-client",
	"java/", "okhttp", "libwww", "node-fetch", "axios/", "undici",
	"http_request", "httpclient", "feed", "rss", "wisp-hook",
}

func isBot(ua string) bool {
	if strings.TrimSpace(ua) == "" {
		return true
	}
	l := strings.ToLower(ua)
	for _, m := range botMarkers {
		if strings.Contains(l, m) {
			return true
		}
	}
	return false
}

// parseAgent maps a User-Agent to coarse families. Order matters: many
// browsers include other browsers' tokens ("Edg/" UAs also say "Chrome/"
// and "Safari/"), so the most specific token is checked first.
func parseAgent(ua string) Agent {
	l := strings.ToLower(ua)
	a := Agent{Browser: "other", OS: "other", Device: "desktop"}

	switch {
	case strings.Contains(l, "edg/"), strings.Contains(l, "edga/"), strings.Contains(l, "edgios/"):
		a.Browser = "edge"
	case strings.Contains(l, "opr/"), strings.Contains(l, "opera"):
		a.Browser = "opera"
	case strings.Contains(l, "samsungbrowser/"):
		a.Browser = "samsung"
	case strings.Contains(l, "firefox/"), strings.Contains(l, "fxios/"):
		a.Browser = "firefox"
	case strings.Contains(l, "chrome/"), strings.Contains(l, "crios/"), strings.Contains(l, "chromium/"):
		a.Browser = "chrome"
	case strings.Contains(l, "safari/"):
		a.Browser = "safari"
	}

	switch {
	case strings.Contains(l, "android"):
		a.OS = "android"
	case strings.Contains(l, "iphone"), strings.Contains(l, "ipad"), strings.Contains(l, "ipod"):
		a.OS = "ios"
	case strings.Contains(l, "windows"):
		a.OS = "windows"
	case strings.Contains(l, "mac os x"), strings.Contains(l, "macintosh"):
		a.OS = "macos"
	case strings.Contains(l, "cros"):
		a.OS = "chromeos"
	case strings.Contains(l, "linux"), strings.Contains(l, "x11"):
		a.OS = "linux"
	}

	switch {
	case strings.Contains(l, "ipad"), strings.Contains(l, "tablet"),
		strings.Contains(l, "android") && !strings.Contains(l, "mobile"):
		a.Device = "tablet"
	case strings.Contains(l, "mobi"), strings.Contains(l, "iphone"), strings.Contains(l, "ipod"):
		a.Device = "mobile"
	}
	return a
}
