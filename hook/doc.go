// Package hook is wisp's in-product analytics hook: products call it at
// the places where a real page view or download happens, and it forwards
// per-visitor daily counts to wisp's ingest API, server-to-server, on a
// timer — only when there is something to send.
//
// It exists because no visitor's browser is ever made to contact wisp, or
// any other third party, for analytics (wisp's plan/concepts/C001). Data
// is collected from requests the visitor already made to the product.
//
//	w, err := hook.Start(hook.Config{
//		Endpoint:   "https://wisp.mera.network/v1/ingest", // empty → no-op
//		ProductKey: "cindernote",
//		Token:      os.Getenv("WISP_TOKEN"),
//		ClientIP:   clientIP, // the product's own trusted-proxy logic
//	})
//	if err != nil { ... }
//	defer w.Close(ctx)
//
//	w.View(r, "/notes/:id")       // in the handler that served the page
//	w.Download(r, "/files/:name") // in the handler that served the file
//
// Privacy properties (wisp's plan/designs/D002 §1–1b):
//   - The raw client IP and User-Agent are used only inside View/Download
//     to derive a visitor key and a coarse browser/OS/device family, then
//     discarded. Neither is stored, logged, or sent.
//   - The key is HMAC-SHA256 under a random salt that lives only in this
//     process's memory for one UTC day and is discarded at midnight. wisp
//     never receives the salt, so it cannot reverse a key, and neither can
//     anyone else once the day is over.
//   - Page keys must be route templates ("/notes/:id"), never raw paths:
//     raw paths can carry secrets. Keys containing '?' or '#' are refused.
//   - Nothing is ever logged. Errors reach only Config.OnError, and never
//     contain request data.
//
// View and Download never block on the network and never fail visibly;
// wisp being down costs analytics data, never product latency or
// unbounded memory. All methods are safe on a nil *Hook.
//
// The package uses only the standard library.
package hook
