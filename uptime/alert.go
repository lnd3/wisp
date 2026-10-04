package uptime

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Alert kinds.
const (
	KindDown   = "down"
	KindUp     = "up"
	KindCert   = "cert"
	KindTest   = "test"
	KindProber = "prober" // the prober's own connectivity
)

// Alert is one notification.
type Alert struct {
	Check   string    `json:"check"`
	Kind    string    `json:"kind"`
	Title   string    `json:"title"`
	Message string    `json:"message"`
	Time    time.Time `json:"time"`
}

// Alerter delivers alerts to one channel.
type Alerter interface {
	Send(ctx context.Context, a Alert) error
	String() string
}

func newAlerter(c AlertConfig, client *http.Client) Alerter {
	if c.Type == "ntfy" {
		return &ntfy{url: c.URL, token: c.Token, client: client}
	}
	return &webhook{url: c.URL, token: c.Token, client: client}
}

// ntfy posts to an ntfy topic URL (ntfy.sh or self-hosted): plain-text
// body, title/priority/tags as headers.
type ntfy struct {
	url, token string
	client     *http.Client
}

func (n *ntfy) String() string { return "ntfy" }

func (n *ntfy) Send(ctx context.Context, a Alert) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.url, bytes.NewBufferString(a.Message))
	if err != nil {
		return err
	}
	req.Header.Set("Title", a.Title)
	switch a.Kind {
	case KindDown:
		req.Header.Set("Priority", "high")
		req.Header.Set("Tags", "rotating_light")
	case KindUp:
		req.Header.Set("Tags", "white_check_mark")
	case KindCert, KindProber:
		req.Header.Set("Tags", "warning")
	default:
		req.Header.Set("Tags", "information_source")
	}
	return post(n.client, req, n.token)
}

// webhook posts the Alert as JSON.
type webhook struct {
	url, token string
	client     *http.Client
}

func (w *webhook) String() string { return "webhook" }

func (w *webhook) Send(ctx context.Context, a Alert) error {
	b, _ := json.Marshal(a)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.url, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	return post(w.client, req, w.token)
}

func post(client *http.Client, req *http.Request, token string) error {
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return nil
}

// deliver sends a to one channel, retrying with backoff.
func deliver(ctx context.Context, al Alerter, a Alert, backoff []time.Duration) error {
	var err error
	for i := 0; ; i++ {
		if err = al.Send(ctx, a); err == nil {
			return nil
		}
		if i >= len(backoff) {
			return err
		}
		select {
		case <-ctx.Done():
			return err
		case <-time.After(backoff[i]):
		}
	}
}
