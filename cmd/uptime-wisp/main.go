// Command uptime-wisp is wisp's lightweight external uptime prober: it
// checks every configured HTTP(S) endpoint and DNS answer once per
// interval, from outside the machines it watches, and alerts (ntfy or
// webhook) when a check goes down, comes back, or its TLS certificate
// nears expiry. See package uptime and deploy/uptime/README.md.
//
//	uptime-wisp [-config /etc/uptime-wisp/config.json]   run
//	uptime-wisp -once                                      one round, print results, no alerts (exit 1 if a check fails)
//	uptime-wisp -test-alert                                send a test alert to every channel
//
// Exit code 2 means the config is invalid.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/lnd3/wisp/uptime"
)

func main() {
	cfgPath := flag.String("config", "/etc/uptime-wisp/config.json", "config file")
	once := flag.Bool("once", false, "run one round, print the results, and exit (no alerts, no heartbeat)")
	testAlert := flag.Bool("test-alert", false, "send a test alert through every channel and exit")
	listen := flag.String("listen", "", "status page + /healthz address; overrides the config's \"listen\" (the container sets :8080)")
	flag.Parse()

	logger := log.New(os.Stderr, "", log.LstdFlags|log.LUTC)
	cfg, err := uptime.Load(*cfgPath)
	if err != nil {
		// Exit 2, not 1: -once uses 1 for "a check failed", and deploy.sh
		// must tell a bad config apart from a down site.
		logger.Print(err)
		os.Exit(2)
	}
	if *listen != "" {
		cfg.Listen = *listen
	}
	m := uptime.New(cfg, uptime.Options{Log: logger})

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	switch {
	case *testAlert:
		if err := m.SendTest(ctx); err != nil {
			logger.Fatalf("test alert failed: %v", err)
		}
		fmt.Println("test alert sent")
	case *once:
		os.Exit(printOnce(ctx, m))
	default:
		if cfg.Listen != "" {
			srv := &http.Server{Addr: cfg.Listen, Handler: m.Handler(), ReadHeaderTimeout: 5 * time.Second}
			go func() {
				if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
					logger.Fatalf("status page: %v", err)
				}
			}()
			defer srv.Close()
		}
		m.Run(ctx)
	}
}

// printOnce runs one round without alerting and prints a table; the exit
// code is 1 if anything failed.
func printOnce(ctx context.Context, m *uptime.Monitor) int {
	results := m.Probe(ctx)
	states, _ := m.Snapshot()
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "RESULT\tCHECK\tTARGET\tDETAIL\tLATENCY\tCERT EXPIRES")
	code := 0
	for i, r := range results {
		res := "ok"
		if !r.OK {
			res, code = "FAIL", 1
		}
		cert := "—"
		if !r.CertExpiry.IsZero() {
			cert = r.CertExpiry.UTC().Format("2006-01-02")
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", res, states[i].Name, states[i].Target, r.Detail, r.Latency.Round(time.Millisecond), cert)
	}
	tw.Flush()
	return code
}
