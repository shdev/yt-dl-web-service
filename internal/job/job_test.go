package job_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

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

// TestJobJSONOmitsEmptyNewFields belegt, dass die neuen Felder bei einem
// frisch erzeugten Job (queued, unbefüllt) nicht im JSON auftauchen.
func TestJobJSONOmitsEmptyNewFields(t *testing.T) {
	j := job.New("https://example.com/v", "Titel", "ba", "Nur Audio", "")
	data, err := json.Marshal(j)
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	for _, key := range []string{`"finished_at"`, `"filename"`, `"audio_format_ids"`, `"multi_audio"`} {
		if strings.Contains(s, key) {
			t.Fatalf("leeres Feld %s darf nicht im JSON stehen: %s", key, s)
		}
	}
}

// TestJobJSONIncludesSetNewFields belegt, dass die neuen Felder bei
// gesetzten Werten korrekt serialisiert werden (Namen/Typen laut Brief).
func TestJobJSONIncludesSetNewFields(t *testing.T) {
	j := job.New("https://example.com/v", "Titel", "ba", "Nur Audio", "")
	now := time.Now().UTC()
	j.FinishedAt = &now
	j.Filename = "video.mp4"
	j.AudioFormatIDs = []string{"140-0", "140-7"}
	j.MultiAudio = true

	data, err := json.Marshal(j)
	if err != nil {
		t.Fatal(err)
	}
	var back struct {
		FinishedAt     *time.Time `json:"finished_at"`
		Filename       string     `json:"filename"`
		AudioFormatIDs []string   `json:"audio_format_ids"`
		MultiAudio     bool       `json:"multi_audio"`
	}
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}
	if back.FinishedAt == nil || !back.FinishedAt.Equal(now) {
		t.Fatalf("finished_at nicht korrekt serialisiert: %+v", back.FinishedAt)
	}
	if back.Filename != "video.mp4" {
		t.Fatalf("filename nicht korrekt serialisiert: %+v", back.Filename)
	}
	if len(back.AudioFormatIDs) != 2 || back.AudioFormatIDs[0] != "140-0" {
		t.Fatalf("audio_format_ids nicht korrekt serialisiert: %+v", back.AudioFormatIDs)
	}
	if !back.MultiAudio {
		t.Fatalf("multi_audio nicht korrekt serialisiert: %+v", back.MultiAudio)
	}
}
