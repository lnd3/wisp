package main

import (
	"context"
	"io"
	"log"
	"testing"
	"time"

	"github.com/lnd3/wisp/hook"
	"github.com/lnd3/wisp/internal/staging"
)

func TestSweepDeletesOnlyExpiredDays(t *testing.T) {
	store, err := staging.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	v := hook.Visitor{K: "AAAAAAAAAAAAAAAAAAAAAA", Visits: 1, Views: map[string]int{"/": 1},
		Agent: hook.Agent{Browser: "firefox", OS: "linux", Device: "desktop"}}
	for i, day := range []string{"2026-09-30", "2026-10-01", "2026-10-02"} {
		b := &hook.Batch{Product: "cindernote", Day: day, BatchID: string(rune('a' + i)), Visitors: []hook.Visitor{v}}
		if _, err := store.Merge(context.Background(), "cindernote", b); err != nil {
			t.Fatal(err)
		}
	}
	// 2026-10-01 closes at 2026-10-02T02:00Z; 2026-09-30 closed a day earlier.
	now, _ := time.Parse(time.RFC3339, "2026-10-02T02:00:00Z")
	sweep(store, now, log.New(io.Discard, "", 0))

	days, _ := store.Days()
	if len(days) != 1 || days[0].Date != "2026-10-02" {
		t.Errorf("after sweep: %v, want only 2026-10-02", days)
	}
}
