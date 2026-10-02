// Command health-wisp reports one machine's disk, memory and load as
// JSON for uptime-wisp's "host" check. See package health and
// deploy/health/README.md.
//
//	health-wisp -host bh2 -disk root=/fs/root      serve (token hash in $HEALTH_TOKEN_SHA256)
//	health-wisp -once -disk root=/                  print one sample and exit
//	health-wisp -hash-token < token                 print the token's SHA-256
//	health-wisp -probe http://127.0.0.1:8080/healthz   exit 0 if it answers 200 (Docker's health check)
package main

import (
	"bufio"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/lnd3/wisp/health"
)

type disks []health.Disk

func (d *disks) String() string { return fmt.Sprint(*d) }
func (d *disks) Set(v string) error {
	name, path, ok := strings.Cut(v, "=")
	if !ok || name == "" || path == "" {
		return errors.New("want name=path, e.g. root=/fs/root")
	}
	*d = append(*d, health.Disk{Name: name, Path: path})
	return nil
}

func main() {
	var ds disks
	flag.Var(&ds, "disk", "a filesystem to report, as name=path (any path on it); repeatable")
	host := flag.String("host", "", "machine name in the report (default: the hostname)")
	listen := flag.String("listen", ":8080", "address to serve on")
	interval := flag.Duration("interval", 30*time.Second, "time between samples")
	proc := flag.String("proc", "/proc", "procfs root")
	once := flag.Bool("once", false, "print one sample as JSON and exit")
	hashToken := flag.Bool("hash-token", false, "read a token on stdin, print its SHA-256 for HEALTH_TOKEN_SHA256, and exit")
	probe := flag.String("probe", "", "GET this URL and exit 0 if it answers 200 (for Docker's health check; the image has no shell)")
	flag.Parse()

	logger := log.New(os.Stderr, "", log.LstdFlags|log.LUTC)
	switch {
	case *hashToken:
		os.Exit(printHash())
	case *probe != "":
		c := http.Client{Timeout: 5 * time.Second}
		resp, err := c.Get(*probe)
		if err != nil || resp.StatusCode != http.StatusOK {
			os.Exit(1)
		}
		return
	}
	if *host == "" {
		*host, _ = os.Hostname()
	}
	s := &health.Sampler{Host: *host, Disks: ds, Proc: *proc}
	if *once {
		r, err := s.Sample(time.Now())
		json.NewEncoder(os.Stdout).Encode(r)
		if err != nil {
			logger.Fatal(err)
		}
		return
	}

	if len(ds) == 0 {
		logger.Print("no -disk given: the report will have no disks")
	}
	if *interval < time.Second {
		logger.Print("-interval must be at least 1s")
		os.Exit(2)
	}
	// Never serve the report without a token: it describes a shared host.
	tok, err := hex.DecodeString(os.Getenv("HEALTH_TOKEN_SHA256"))
	if err != nil || len(tok) != 32 {
		logger.Print("HEALTH_TOKEN_SHA256 must be 64 hex chars (health-wisp -hash-token)")
		os.Exit(2)
	}
	srv := &health.Server{Sampler: s, Interval: *interval, TokenSHA256: tok, Logf: logger.Printf}
	stop := make(chan struct{})
	go srv.Run(stop)

	hs := &http.Server{Addr: *listen, Handler: srv.Handler(), ReadHeaderTimeout: 5 * time.Second, ErrorLog: log.New(discard{}, "", 0)}
	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
		<-sig
		close(stop)
		hs.Close()
	}()
	logger.Printf("health-wisp: %s, %d disk(s), every %s, on %s", *host, len(ds), *interval, *listen)
	if err := hs.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Fatal(err)
	}
}

// discard drops net/http's own error log: its TLS-handshake and
// malformed-request lines name the client's address.
type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }

func printHash() int {
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	tok := strings.TrimRight(line, "\r\n")
	if err != nil && tok == "" {
		fmt.Fprintln(os.Stderr, "hash-token: pipe the token on stdin")
		return 2
	}
	if len(tok) < 24 {
		fmt.Fprintln(os.Stderr, "hash-token: use at least 24 characters (e.g. openssl rand -base64 32)")
		return 2
	}
	fmt.Println(health.HashToken(tok))
	return 0
}
