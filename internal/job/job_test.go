package job_test

import (
	"encoding/json"
	"testing"

	"ytdlweb/internal/job"
)

func TestNewSetsFields(t *testing.T) {
	j := job.New("https://example.com/v", "Titel", "303+251", "1080p", "Meine Liste")
	if j.URL != "https://example.com/v" || j.Title != "Titel" {
		t.Fatalf("Felder nicht übernommen: %+v", j)
	}
	if j.Format != "303+251" || j.FormatLabel != "1080p" || j.PlaylistTitle != "Meine Liste" {
		t.Fatalf("Format-Felder nicht übernommen: %+v", j)
	}
	if j.State != job.StateQueued {
		t.Fatalf("neuer Job muss queued sein, war %s", j.State)
	}
	if j.ID == "" || j.CreatedAt.IsZero() {
		t.Fatalf("ID/CreatedAt fehlen: %+v", j)
	}
}

func TestNewUniqueIDs(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		id := job.New("u", "t", "f", "l", "").ID
		if seen[id] {
			t.Fatalf("doppelte ID: %s", id)
		}
		seen[id] = true
	}
}

func TestJobJSONRoundTrip(t *testing.T) {
	j := job.New("https://example.com/v", "Titel", "ba", "Nur Audio", "")
	j.Progress = job.Progress{Percent: 42.5, Speed: "1.25MiB/s", ETA: "00:35"}
	data, err := json.Marshal(j)
	if err != nil {
		t.Fatal(err)
	}
	var back job.Job
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}
	if back.ID != j.ID || back.Progress.Percent != 42.5 || back.State != job.StateQueued {
		t.Fatalf("Round-Trip verändert Daten: %+v", back)
	}
}
