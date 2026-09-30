package hook

// Batch is one ingestion call's body — wisp's plan/designs/D002 §2.
// Counts are increments since the previous batch for the same day; wisp
// merges them by visitor key and dedups on BatchID, so a retried batch
// is harmless.
type Batch struct {
	Product  string    `json:"product"`
	Day      string    `json:"day"` // UTC, YYYY-MM-DD
	BatchID  string    `json:"batch_id"`
	Visitors []Visitor `json:"visitors"`
}

// Visitor is one day-scoped visitor's increments within a Batch.
type Visitor struct {
	K         string         `json:"k"`
	Visits    int            `json:"visits"`
	Views     map[string]int `json:"views,omitempty"`
	Downloads map[string]int `json:"downloads,omitempty"`
	Referrers map[string]int `json:"referrers,omitempty"`
	Agent     Agent          `json:"agent"`
}

func (v *Visitor) empty() bool {
	return v.Visits == 0 && len(v.Views) == 0 && len(v.Downloads) == 0 && len(v.Referrers) == 0
}

func bump(m *map[string]int, k string) {
	if *m == nil {
		*m = make(map[string]int)
	}
	(*m)[k]++
}
