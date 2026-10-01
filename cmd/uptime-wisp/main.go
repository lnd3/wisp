// Command uptime-wisp is wisp's lightweight external uptime prober: it
// checks every configured HTTP(S) endpoint and DNS answer once per
// interval, from outside the machines it watches, and alerts (ntfy or
// webhook) when a check goes down, comes back, or its TLS certificate
// nears expiry. See package uptime and deploy/uptime/README.md.
//
//	uptime-wisp [-config /etc/uptime-wisp/config.json]   run (SIGHUP or the page's button reloads the config)
//	uptime-wisp -once                                      one round, print results, no alerts (exit 1 if a check fails)
//	uptime-wisp -test-alert                                send a test alert to every channel
//	uptime-wisp -hash-password < password                 print auth.password_sha256
//	uptime-wisp -check-config [-listen :8080]              validate as the service would run it
//
// Exit code 2 means the config is invalid.
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/lnd3/wisp/uptime"
)

func main() {
	cfgPath := flag.String("config", "/etc/uptime-wisp/config.json", "config file")
	once := flag.Bool("once", false, "run one round, print the results, and exit (no alerts, no heartbeat)")
	testAlert := flag.Bool("test-alert", false, "send a test alert through every channel and exit")
	checkConfig := flag.Bool("check-config", false, "validate the config exactly as the service would run it (with -listen), then exit: 0 ok, 2 invalid")
	hashPassword := flag.Bool("hash-password", false, "read a password on stdin, print its SHA-256 for auth.password_sha256, and exit")
	listen := flag.String("listen", "", "status page + /healthz address; overrides the config's \"listen\" (the container sets :8080)")
	flag.Parse()

	logger := log.New(os.Stderr, "", log.LstdFlags|log.LUTC)
	if *hashPassword {
		os.Exit(printHash())
	}
	// load reads and validates the config exactly as the service runs it.
	// Used at startup and for every reload, so a reload can never accept
	// what a restart would reject.
	load := func() (*uptime.Config, error) {
		cfg, err := uptime.Load(*cfgPath)
		if err != nil {
			return nil, err
		}
		if *listen != "" {
			cfg.Listen = *listen
		}
		// Never serve an open status page by accident.
		if cfg.Listen != "" && cfg.Auth == nil && !*once && !*testAlert {
			return nil, fmt.Errorf("config: the status page is enabled but \"auth\" is missing — add {\"user\":…, \"password_sha256\":…} (see -hash-password)")
		}
		return cfg, nil
	}
	cfg, err := load()
	if err != nil {
		// Exit 2, not 1: -once uses 1 for "a check failed", and deploy.sh
		// must tell a bad config apart from a down site.
		logger.Print(err)
		os.Exit(2)
	}
	if *checkConfig {
		fmt.Printf("config ok: %d checks, %d alert channel(s), status page %s\n", len(cfg.Checks), len(cfg.Alerts), orOff(cfg.Listen))
		return
	}
	m := uptime.New(cfg, uptime.Options{Log: logger})
	m.SetLoader(func() (*uptime.Config, error) {
		c, err := load()
		if err == nil && c.Listen != cfg.Listen {
			logger.Printf("reload: \"listen\" changed to %q; the status page keeps %q until a restart", c.Listen, cfg.Listen)
		}
		return c, err
	})

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
		// SIGHUP reloads config.json, the same as the page's button.
		hup := make(chan os.Signal, 1)
		signal.Notify(hup, syscall.SIGHUP)
		go func() {
			for range hup {
				m.Reload()
			}
		}()
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

// printHash reads one line (the password) from stdin and prints its
// SHA-256 for the config's auth.password_sha256.
func printHash() int {
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	pw := strings.TrimRight(line, "\r\n")
	if err != nil && pw == "" {
		fmt.Fprintln(os.Stderr, "hash-password: pipe the password on stdin")
		return 2
	}
	if len(pw) < 16 {
		fmt.Fprintln(os.Stderr, "hash-password: use at least 16 characters (e.g. openssl rand -base64 24)")
		return 2
	}
	fmt.Println(uptime.HashPassword(pw))
	return 0
}

func orOff(listen string) string {
	if listen == "" {
		return "off"
	}
	return "on " + listen + " (auth required)"
}
