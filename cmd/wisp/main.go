// Command wisp runs wisp's server: the ingest API product hooks send to,
// and the day close that turns each product-day's keyed staging into
// keyless rows in the product statistics DB.
//
//	wisp serve [-addr :8080] [-data /data] [-registry /etc/wisp/products.json]
//	wisp hash-token   < token   # prints the SHA-256 to put in the registry
//
// The registry is reloaded on SIGHUP. Nothing logged here ever contains
// request data.
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/lnd3/wisp/internal/dayclose"
	"github.com/lnd3/wisp/internal/ingest"
	"github.com/lnd3/wisp/internal/registry"
	"github.com/lnd3/wisp/internal/staging"
	"github.com/lnd3/wisp/internal/stats"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	switch os.Args[1] {
	case "serve":
		if err := serve(os.Args[2:]); err != nil {
			log.Fatal(err)
		}
	case "hash-token":
		if err := hashToken(); err != nil {
			log.Fatal(err)
		}
	default:
		usage()
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: wisp serve [flags] | wisp hash-token < token")
	os.Exit(2)
}

func hashToken() error {
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && line == "" {
		return fmt.Errorf("hash-token: read token from stdin: %w", err)
	}
	tok := strings.TrimSpace(line)
	if len(tok) < 32 {
		return errors.New("hash-token: token too short — generate one with: openssl rand -hex 32")
	}
	fmt.Println(registry.HashToken(tok))
	return nil
}

func serve(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	addr := fs.String("addr", ":8080", "listen address")
	data := fs.String("data", "/data", "data directory: <data>/staging (keyed, per product-day) and <data>/stats.sqlite (keyless)")
	regPath := fs.String("registry", "/etc/wisp/products.json", "product registry file (reloaded on SIGHUP)")
	closeEvery := fs.Duration("close-every", 10*time.Minute, "how often to close product-days past their deadline")
	fs.Parse(args)

	logger := log.New(os.Stderr, "wisp: ", log.LstdFlags|log.LUTC)

	reg, err := registry.Load(*regPath)
	if err != nil {
		return err
	}
	var current atomic.Pointer[registry.Registry]
	current.Store(reg)
	logger.Printf("registry: %d products", len(reg.Products()))

	store, err := staging.Open(filepath.Join(*data, "staging"))
	if err != nil {
		return err
	}
	defer store.Close()
	statsDB, err := stats.Open(filepath.Join(*data, "stats.sqlite"))
	if err != nil {
		return err
	}
	defer statsDB.Close()
	closer := &dayclose.Closer{Staging: store, Stats: statsDB, Log: logger}

	h := &ingest.Handler{
		Registry: func() ingest.Registry { return current.Load() },
		Stager:   store,
		Log:      logger,
	}
	srv := &http.Server{
		Addr:              *addr,
		Handler:           h.Routes(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
		// No ErrorLog override needed: net/http's own messages (TLS/handshake
		// noise, panics) can include the peer address, which here is always
		// wisp-caddy's, never a visitor's — only product servers call this.
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	hup := make(chan os.Signal, 1)
	signal.Notify(hup, syscall.SIGHUP)
	go func() {
		for range hup {
			r, err := registry.Load(*regPath)
			if err != nil {
				logger.Printf("registry reload failed, keeping the previous one: %v", err)
				continue
			}
			current.Store(r)
			logger.Printf("registry reloaded: %d products", len(r.Products()))
		}
	}()

	// Close before serving, so no expired keyed file sits around after a
	// restart, then keep closing on a timer.
	closer.Run(ctx, time.Now())
	go func() {
		t := time.NewTicker(*closeEvery)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-t.C:
				closer.Run(ctx, now)
			}
		}
	}()

	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	logger.Printf("listening on %s", *addr)

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	return srv.Shutdown(shutdown)
}
