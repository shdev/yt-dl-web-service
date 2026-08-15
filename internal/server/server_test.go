package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"ytdlweb/internal/job"
	"ytdlweb/internal/queue"
	"ytdlweb/internal/server"
	"ytdlweb/internal/store"
	"ytdlweb/internal/ytdlp"
)

type nopRunner struct{}

func (nopRunner) Run(context.Context, job.Job, func(job.Progress)) error { return nil }

type fakeProber struct {
	res *ytdlp.ProbeResult
	err error
}

func (f fakeProber) Probe(context.Context, string) (*ytdlp.ProbeResult, error) {
	return f.res, f.err
}

// newServer baut den Handler mit nicht gestarteter Queue — Jobs bleiben queued.
func newServer(t *testing.T, p server.Prober) (http.Handler, *store.Store) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "jobs.json"))
	if err != nil {
		t.Fatal(err)
	}
	q := queue.New(st, nopRunner{}, 1)
	return server.New(st, q, p), st
}

func do(t *testing.T, h http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func videoProbe() *ytdlp.ProbeResult {
	return &ytdlp.ProbeResult{
		Type: "video",
		Video: &ytdlp.Video{
			ID: "abc", Title: "Test Video",
			Formats: []ytdlp.Format{{ID: "303", VCodec: "vp9", ACodec: "none"}},
		},
	}
}

func TestHealthz(t *testing.T) {
	h, _ := newServer(t, fakeProber{})
	rec := do(t, h, "GET", "/healthz", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("healthz: %d", rec.Code)
	}
}

func TestIndexAndStatic(t *testing.T) {
	h, _ := newServer(t, fakeProber{})
	rec := do(t, h, "GET", "/", nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "yt-dl Web") {
		t.Fatalf("Index fehlt: %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Beste ≤1080p") || !strings.Contains(body, `value="720p"`) {
		t.Fatalf("Profile aus ytdlp.Profiles fehlen im gerenderten Index: %s", body)
	}
	rec = do(t, h, "GET", "/static/app.js", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("Static-Route: %d", rec.Code)
	}
}

func TestProbeReturnsResult(t *testing.T) {
	h, _ := newServer(t, fakeProber{res: videoProbe()})
	rec := do(t, h, "POST", "/api/probe", map[string]string{"url": "https://example.com/v"})
	if rec.Code != http.StatusOK {
		t.Fatalf("Code %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"format_id":"303"`) {
		t.Fatalf("Formatliste fehlt: %s", rec.Body.String())
	}
}

func TestProbeValidation(t *testing.T) {
	h, _ := newServer(t, fakeProber{res: videoProbe()})
	if rec := do(t, h, "POST", "/api/probe", map[string]string{"url": "  "}); rec.Code != http.StatusBadRequest {
		t.Fatalf("leere URL: %d", rec.Code)
	}
}

func TestProbeErrorBecomes502(t *testing.T) {
	h, _ := newServer(t, fakeProber{err: errors.New("yt-dlp: Unsupported URL")})
	rec := do(t, h, "POST", "/api/probe", map[string]string{"url": "https://example.invalid"})
	if rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), "Unsupported URL") {
		t.Fatalf("Code %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCreateVideoJob(t *testing.T) {
	h, st := newServer(t, fakeProber{})
	rec := do(t, h, "POST", "/api/jobs", map[string]any{
		"type": "video", "url": "https://example.com/v", "title": "Test",
		"format_video": "303", "format_audio": "251", "format_label": "1080p · vp9",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("Code %d: %s", rec.Code, rec.Body.String())
	}
	jobs := st.List()
	if len(jobs) != 1 || jobs[0].Format != "303+251" || jobs[0].FormatLabel != "1080p · vp9" {
		t.Fatalf("Job falsch angelegt: %+v", jobs)
	}
}

func TestCreateVideoJobDuplicate(t *testing.T) {
	h, _ := newServer(t, fakeProber{})
	body := map[string]any{"type": "video", "url": "https://example.com/v", "format_video": "303"}
	if rec := do(t, h, "POST", "/api/jobs", body); rec.Code != http.StatusCreated {
		t.Fatalf("erster: %d", rec.Code)
	}
	if rec := do(t, h, "POST", "/api/jobs", body); rec.Code != http.StatusConflict {
		t.Fatalf("Duplikat muss 409 liefern, war %d", rec.Code)
	}
}

func TestCreatePlaylistJobs(t *testing.T) {
	h, st := newServer(t, fakeProber{})
	rec := do(t, h, "POST", "/api/jobs", map[string]any{
		"type": "playlist", "profile": "720p", "playlist_title": "Liste",
		"entries": []map[string]string{
			{"url": "https://example.com/1", "title": "Eins"},
			{"url": "https://example.com/2", "title": "Zwei"},
		},
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("Code %d: %s", rec.Code, rec.Body.String())
	}
	jobs := st.List()
	if len(jobs) != 2 {
		t.Fatalf("2 Jobs erwartet: %+v", jobs)
	}
	for _, j := range jobs {
		if j.Format != "bv*[height<=720]+ba/b[height<=720]" || j.PlaylistTitle != "Liste" {
			t.Fatalf("Playlist-Job falsch: %+v", j)
		}
	}
}

func TestCreatePlaylistValidation(t *testing.T) {
	h, _ := newServer(t, fakeProber{})
	rec := do(t, h, "POST", "/api/jobs", map[string]any{
		"type": "playlist", "profile": "gibtsnicht",
		"entries": []map[string]string{{"url": "https://example.com/1"}},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unbekanntes Profil: %d", rec.Code)
	}
	rec = do(t, h, "POST", "/api/jobs", map[string]any{"type": "playlist", "profile": "best"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("leere Einträge: %d", rec.Code)
	}
}

func TestListJobs(t *testing.T) {
	h, st := newServer(t, fakeProber{})
	_ = st.Add(job.New("https://example.com/v", "Test", "ba", "l", ""))
	rec := do(t, h, "GET", "/api/jobs", nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"state":"queued"`) {
		t.Fatalf("Code %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCancelQueuedJob(t *testing.T) {
	h, st := newServer(t, fakeProber{})
	j := job.New("https://example.com/v", "Test", "ba", "l", "")
	_ = st.Add(j)
	if rec := do(t, h, "POST", "/api/jobs/"+j.ID+"/cancel", nil); rec.Code != http.StatusNoContent {
		t.Fatalf("Cancel: %d", rec.Code)
	}
	got, _ := st.Get(j.ID)
	if got.State != job.StateCanceled {
		t.Fatalf("Zustand %s", got.State)
	}
	// done-Job kann nicht abgebrochen werden
	if rec := do(t, h, "POST", "/api/jobs/"+j.ID+"/cancel", nil); rec.Code != http.StatusConflict {
		t.Fatalf("Cancel auf canceled muss 409 sein: %d", rec.Code)
	}
}

func TestRetryJob(t *testing.T) {
	h, st := newServer(t, fakeProber{})
	j := job.New("https://example.com/v", "Test", "ba", "l", "")
	_ = st.Add(j)
	_ = st.Update(j.ID, func(x *job.Job) {
		x.State = job.StateError
		x.Error = "kaputt"
		x.Progress = job.Progress{Percent: 33}
	})
	if rec := do(t, h, "POST", "/api/jobs/"+j.ID+"/retry", nil); rec.Code != http.StatusNoContent {
		t.Fatalf("Retry: %d", rec.Code)
	}
	got, _ := st.Get(j.ID)
	if got.State != job.StateQueued || got.Error != "" || got.Progress.Percent != 0 {
		t.Fatalf("Retry muss zurücksetzen: %+v", got)
	}
	// queued-Job kann nicht erneut versucht werden
	if rec := do(t, h, "POST", "/api/jobs/"+j.ID+"/retry", nil); rec.Code != http.StatusConflict {
		t.Fatalf("Retry auf queued muss 409 sein: %d", rec.Code)
	}
}

func TestDeleteJob(t *testing.T) {
	h, st := newServer(t, fakeProber{})
	j := job.New("https://example.com/v", "Test", "ba", "l", "")
	_ = st.Add(j)
	_ = st.Update(j.ID, func(x *job.Job) { x.State = job.StateRunning })
	if rec := do(t, h, "DELETE", "/api/jobs/"+j.ID, nil); rec.Code != http.StatusConflict {
		t.Fatalf("laufender Job darf nicht gelöscht werden: %d", rec.Code)
	}
	_ = st.Update(j.ID, func(x *job.Job) { x.State = job.StateDone })
	if rec := do(t, h, "DELETE", "/api/jobs/"+j.ID, nil); rec.Code != http.StatusNoContent {
		t.Fatalf("Delete: %d", rec.Code)
	}
	if _, ok := st.Get(j.ID); ok {
		t.Fatal("Job muss entfernt sein")
	}
}

func TestUnknownJobID(t *testing.T) {
	h, _ := newServer(t, fakeProber{})
	for _, c := range []struct{ method, path string }{
		{"POST", "/api/jobs/nix/cancel"},
		{"POST", "/api/jobs/nix/retry"},
		{"DELETE", "/api/jobs/nix"},
	} {
		if rec := do(t, h, c.method, c.path, nil); rec.Code != http.StatusNotFound {
			t.Fatalf("%s %s: %d", c.method, c.path, rec.Code)
		}
	}
}
