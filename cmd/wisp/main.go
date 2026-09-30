// Command wisp runs wisp's server: the ingest API product hooks send to,
// and the staging sweep.
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

	"github.com/lnd3/wisp/hook"
	"github.com/lnd3/wisp/internal/ingest"
	"github.com/lnd3/wisp/internal/registry"
	"github.com/lnd3/wisp/internal/staging"
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
	data := fs.String("data", "/data", "data directory (staging files live under <data>/staging)")
	regPath := fs.String("registry", "/etc/wisp/products.json", "product registry file (reloaded on SIGHUP)")
	sweepEvery := fs.Duration("sweep", 10*time.Minute, "how often to delete staging files past their day's close deadline")
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

	// The sweep runs before serving so no expired keyed file ever sits
	// around after a restart.
	sweep(store, time.Now(), logger)
	go func() {
		t := time.NewTicker(*sweepEvery)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-t.C:
				sweep(store, now, logger)
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

// sweep deletes every staging file whose day is past its close deadline.
// This is the backstop behind CLAUDE.md's "visitor keys never outlive
// their day": it runs whether or not the day close (aggregation into the
// statistics DB) has happened.
func sweep(store *staging.Store, now time.Time, logger *log.Logger) {
	days, err := store.Days()
	if err != nil {
		logger.Printf("sweep: list staging: %v", err)
		return
	}
	for _, d := range days {
		if now.Before(d.Start().Add(hook.DayCloseAfter)) {
			continue
		}
		if err := store.Delete(d); err != nil {
			logger.Printf("sweep: delete %s/%s: %v", d.Product, d.Date, err)
			continue
		}
		logger.Printf("sweep: deleted expired staging %s/%s", d.Product, d.Date)
	}
}
