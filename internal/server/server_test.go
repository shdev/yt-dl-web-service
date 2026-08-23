package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ytdlweb/internal/job"
	"ytdlweb/internal/queue"
	"ytdlweb/internal/server"
	"ytdlweb/internal/settings"
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

type fakeYtdlp struct {
	version string
	verr    error
	updated string
	uerr    error
}

func (f fakeYtdlp) Version(context.Context) (string, error) { return f.version, f.verr }
func (f fakeYtdlp) Update(context.Context) (string, error)  { return f.updated, f.uerr }

// newServer baut den Handler mit nicht gestarteter Queue — Jobs bleiben queued.
func newServer(t *testing.T, p server.Prober) (http.Handler, *store.Store, *settings.Store) {
	t.Helper()
	return newServerYtdlp(t, p, fakeYtdlp{version: "2026.08.19"})
}

func newServerYtdlp(t *testing.T, p server.Prober, y server.Ytdlp) (http.Handler, *store.Store, *settings.Store) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "jobs.json"))
	if err != nil {
		t.Fatal(err)
	}
	set, err := settings.Open(filepath.Join(t.TempDir(), "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	q := queue.New(st, nopRunner{}, 1)
	return server.New(st, q, p, set, y), st, set
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

// doRaw schickt einen rohen (ggf. ungültigen) Body — für Decode-Fehler-Tests.
func doRaw(t *testing.T, h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
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

// multiLangVideoProbe liefert ein Einzelvideo mit zwei Tonspuren (de, en) —
// für den audio_languages-Test der Probe-Antwort.
func multiLangVideoProbe() *ytdlp.ProbeResult {
	return &ytdlp.ProbeResult{
		Type: "video",
		Video: &ytdlp.Video{
			ID: "abc", Title: "Test Video",
			Formats: []ytdlp.Format{
				{ID: "137", VCodec: "vp9", ACodec: "none"},
				{ID: "140-0", ACodec: "mp4a", VCodec: "none", Language: "de"},
				{ID: "140-1", ACodec: "mp4a", VCodec: "none", Language: "en"},
			},
		},
	}
}

func TestHealthz(t *testing.T) {
	h, _, _ := newServer(t, fakeProber{})
	rec := do(t, h, "GET", "/healthz", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("healthz: %d", rec.Code)
	}
}

func TestIndexAndStatic(t *testing.T) {
	h, _, _ := newServer(t, fakeProber{})
	rec := do(t, h, "GET", "/", nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "yt-dl Web") {
		t.Fatalf("Index fehlt: %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Beste ≤1080p") || !strings.Contains(body, `value="720p"`) {
		t.Fatalf("Profile aus ytdlp.Profiles fehlen im gerenderten Index: %s", body)
	}
	if !strings.Contains(body, `id="settings-btn"`) || !strings.Contains(body, `id="video-profile"`) ||
		!strings.Contains(body, `value="1080p-mp4"`) {
		t.Fatalf("Settings-UI fehlt im gerenderten Index: %s", body)
	}
	if !strings.Contains(body, `id="ytdlp-version"`) || !strings.Contains(body, `id="ytdlp-update-btn"`) {
		t.Fatalf("yt-dlp-Update-UI fehlt im gerenderten Index: %s", body)
	}
	rec = do(t, h, "GET", "/static/app.js", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("Static-Route: %d", rec.Code)
	}
}

func TestYtdlpVersion(t *testing.T) {
	h, _, _ := newServerYtdlp(t, fakeProber{}, fakeYtdlp{version: "2026.08.19"})
	rec := do(t, h, "GET", "/api/ytdlp", nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"version":"2026.08.19"`) {
		t.Fatalf("Code %d: %s", rec.Code, rec.Body.String())
	}
}

func TestYtdlpVersionErrorBecomes502(t *testing.T) {
	h, _, _ := newServerYtdlp(t, fakeProber{}, fakeYtdlp{verr: errors.New("exec kaputt")})
	rec := do(t, h, "GET", "/api/ytdlp", nil)
	if rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), "exec kaputt") {
		t.Fatalf("Code %d: %s", rec.Code, rec.Body.String())
	}
}

func TestYtdlpUpdate(t *testing.T) {
	h, _, _ := newServerYtdlp(t, fakeProber{}, fakeYtdlp{updated: "2026.08.19"})
	rec := do(t, h, "POST", "/api/ytdlp/update", nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"version":"2026.08.19"`) {
		t.Fatalf("Code %d: %s", rec.Code, rec.Body.String())
	}
}

func TestYtdlpUpdateRunningBecomes409(t *testing.T) {
	h, _, _ := newServerYtdlp(t, fakeProber{}, fakeYtdlp{uerr: ytdlp.ErrUpdateRunning})
	rec := do(t, h, "POST", "/api/ytdlp/update", nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("Code %d: %s", rec.Code, rec.Body.String())
	}
}

// ctxCheckYtdlp meldet Erfolg nur, wenn der übergebene Context noch lebt —
// so lässt sich prüfen, dass das Update vom Request-Context entkoppelt ist.
type ctxCheckYtdlp struct{}

func (ctxCheckYtdlp) Version(ctx context.Context) (string, error) { return "", ctx.Err() }
func (ctxCheckYtdlp) Update(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return "2026.08.19", nil
}

// Ein Client-Abbruch (Tab zu, Reload, Proxy-Timeout) darf ein laufendes
// Update nicht mehr killen (Review-Finding).
func TestYtdlpUpdateSurvivesClientDisconnect(t *testing.T) {
	h, _, _ := newServerYtdlp(t, fakeProber{}, ctxCheckYtdlp{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Request-Context ist bereits abgebrochen
	req := httptest.NewRequest("POST", "/api/ytdlp/update", nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"version":"2026.08.19"`) {
		t.Fatalf("Update muss Client-Abbruch überleben, Code %d: %s", rec.Code, rec.Body.String())
	}
}

func TestYtdlpUpdateErrorBecomes502(t *testing.T) {
	h, _, _ := newServerYtdlp(t, fakeProber{}, fakeYtdlp{uerr: errors.New("yt-dlp -U: exit 1")})
	rec := do(t, h, "POST", "/api/ytdlp/update", nil)
	if rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), "exit 1") {
		t.Fatalf("Code %d: %s", rec.Code, rec.Body.String())
	}
}

func TestProbeReturnsResult(t *testing.T) {
	h, _, _ := newServer(t, fakeProber{res: videoProbe()})
	rec := do(t, h, "POST", "/api/probe", map[string]string{"url": "https://example.com/v"})
	if rec.Code != http.StatusOK {
		t.Fatalf("Code %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"format_id":"303"`) {
		t.Fatalf("Formatliste fehlt: %s", rec.Body.String())
	}
}

func TestProbeValidation(t *testing.T) {
	h, _, _ := newServer(t, fakeProber{res: videoProbe()})
	if rec := do(t, h, "POST", "/api/probe", map[string]string{"url": "  "}); rec.Code != http.StatusBadRequest {
		t.Fatalf("leere URL: %d", rec.Code)
	}
}

func TestProbeErrorBecomes502(t *testing.T) {
	h, _, _ := newServer(t, fakeProber{err: errors.New("yt-dlp: Unsupported URL")})
	rec := do(t, h, "POST", "/api/probe", map[string]string{"url": "https://example.invalid"})
	if rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), "Unsupported URL") {
		t.Fatalf("Code %d: %s", rec.Code, rec.Body.String())
	}
}

// TestProbeReturnsAudioLanguages prüft, dass die Probe-Antwort bei einem
// mehrsprachigen Einzelvideo audio_languages mit Selected-Flags liefert
// (RankAudio(video.Formats) — Task 2).
func TestProbeReturnsAudioLanguages(t *testing.T) {
	h, _, _ := newServer(t, fakeProber{res: multiLangVideoProbe()})
	rec := do(t, h, "POST", "/api/probe", map[string]string{"url": "https://example.com/v"})
	if rec.Code != http.StatusOK {
		t.Fatalf("Code %d: %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Video struct {
			AudioLanguages []ytdlp.AudioTrack `json:"audio_languages"`
		} `json:"video"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("Antwort nicht dekodierbar: %v: %s", err, rec.Body.String())
	}
	if len(out.Video.AudioLanguages) != 2 {
		t.Fatalf("2 Audiospuren erwartet: %+v", out.Video.AudioLanguages)
	}
	selected := 0
	for _, tr := range out.Video.AudioLanguages {
		if tr.Selected {
			selected++
		}
	}
	if selected == 0 {
		t.Fatalf("mindestens eine Spur muss selected sein: %+v", out.Video.AudioLanguages)
	}
}

// TestProbeSingleLanguageOmitsAudioLanguages: bei einem einsprachigen Video
// fehlt audio_languages (oder hat höchstens 1 Eintrag) — RankAudio liefert
// bei fehlenden Sprachinfos nil.
func TestProbeSingleLanguageOmitsAudioLanguages(t *testing.T) {
	h, _, _ := newServer(t, fakeProber{res: videoProbe()})
	rec := do(t, h, "POST", "/api/probe", map[string]string{"url": "https://example.com/v"})
	if rec.Code != http.StatusOK {
		t.Fatalf("Code %d: %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "audio_languages") {
		t.Fatalf("audio_languages darf bei fehlenden Sprachinfos nicht auftauchen: %s", rec.Body.String())
	}
}

func TestCreateVideoJob(t *testing.T) {
	h, st, _ := newServer(t, fakeProber{})
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

func TestCreateVideoJobWithProfile(t *testing.T) {
	h, st, _ := newServer(t, fakeProber{})
	rec := do(t, h, "POST", "/api/jobs", map[string]any{
		"type": "video", "url": "https://example.com/v", "title": "Test",
		"profile": "1080p-mp4",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("Code %d: %s", rec.Code, rec.Body.String())
	}
	jobs := st.List()
	wantExpr := "bv*[height<=1080][ext=mp4]+ba[ext=m4a]/b[ext=mp4][height<=1080]/b[height<=1080]"
	if len(jobs) != 1 || jobs[0].Format != wantExpr || jobs[0].FormatLabel != "Beste ≤1080p (MP4/M4A)" {
		t.Fatalf("Job falsch angelegt: %+v", jobs)
	}
}

func TestCreateVideoJobUnknownProfile(t *testing.T) {
	h, _, _ := newServer(t, fakeProber{})
	rec := do(t, h, "POST", "/api/jobs", map[string]any{
		"type": "video", "url": "https://example.com/v", "profile": "gibtsnicht",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unbekanntes Profil: %d", rec.Code)
	}
}

func TestCreateVideoJobDuplicate(t *testing.T) {
	h, _, _ := newServer(t, fakeProber{})
	body := map[string]any{"type": "video", "url": "https://example.com/v", "format_video": "303"}
	if rec := do(t, h, "POST", "/api/jobs", body); rec.Code != http.StatusCreated {
		t.Fatalf("erster: %d", rec.Code)
	}
	if rec := do(t, h, "POST", "/api/jobs", body); rec.Code != http.StatusConflict {
		t.Fatalf("Duplikat muss 409 liefern, war %d", rec.Code)
	}
}

func TestCreatePlaylistJobs(t *testing.T) {
	h, st, _ := newServer(t, fakeProber{})
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
	h, _, _ := newServer(t, fakeProber{})
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
	h, st, _ := newServer(t, fakeProber{})
	_ = st.Add(job.New("https://example.com/v", "Test", "ba", "l", ""))
	rec := do(t, h, "GET", "/api/jobs", nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"state":"queued"`) {
		t.Fatalf("Code %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCancelQueuedJob(t *testing.T) {
	h, st, _ := newServer(t, fakeProber{})
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
	h, st, _ := newServer(t, fakeProber{})
	j := job.New("https://example.com/v", "Test", "ba", "l", "")
	_ = st.Add(j)
	finished := time.Now().UTC()
	_ = st.Update(j.ID, func(x *job.Job) {
		x.State = job.StateError
		x.Error = "kaputt"
		x.Progress = job.Progress{Percent: 33}
		x.FinishedAt = &finished
		x.Filename = "video.mp4"
	})
	if rec := do(t, h, "POST", "/api/jobs/"+j.ID+"/retry", nil); rec.Code != http.StatusNoContent {
		t.Fatalf("Retry: %d", rec.Code)
	}
	got, _ := st.Get(j.ID)
	if got.State != job.StateQueued || got.Error != "" || got.Progress.Percent != 0 {
		t.Fatalf("Retry muss zurücksetzen: %+v", got)
	}
	if got.FinishedAt != nil {
		t.Fatalf("Retry muss FinishedAt auf nil zurücksetzen: %+v", got)
	}
	if got.Filename != "" {
		t.Fatalf("Retry muss Filename leeren: %+v", got)
	}
	// queued-Job kann nicht erneut versucht werden
	if rec := do(t, h, "POST", "/api/jobs/"+j.ID+"/retry", nil); rec.Code != http.StatusConflict {
		t.Fatalf("Retry auf queued muss 409 sein: %d", rec.Code)
	}
}

func TestDeleteJob(t *testing.T) {
	h, st, _ := newServer(t, fakeProber{})
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
	h, _, _ := newServer(t, fakeProber{})
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

func TestGetSettingsDefaults(t *testing.T) {
	h, _, _ := newServer(t, fakeProber{})
	rec := do(t, h, "GET", "/api/settings", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("Code %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"default_profile":"best"`) {
		t.Fatalf("Default-Profil fehlt: %s", rec.Body.String())
	}
}

func TestPutSettingsValidThenGet(t *testing.T) {
	h, _, _ := newServer(t, fakeProber{})
	rec := do(t, h, "PUT", "/api/settings", map[string]string{"default_profile": "1080p-mp4"})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("PUT Code %d: %s", rec.Code, rec.Body.String())
	}
	rec = do(t, h, "GET", "/api/settings", nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"default_profile":"1080p-mp4"`) {
		t.Fatalf("GET nach PUT: %d %s", rec.Code, rec.Body.String())
	}
}

func TestPutSettingsUnknownProfile(t *testing.T) {
	h, _, _ := newServer(t, fakeProber{})
	rec := do(t, h, "PUT", "/api/settings", map[string]string{"default_profile": "gibtsnicht"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unbekanntes Profil: %d", rec.Code)
	}
}

func TestPutSettingsBadBody(t *testing.T) {
	h, _, _ := newServer(t, fakeProber{})
	rec := doRaw(t, h, "PUT", "/api/settings", "{kaputt")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("kaputter Body: %d", rec.Code)
	}
}

// TestGetSettingsNormalizesUnknownProfile ist ein Regressionstest: eine bereits
// auf Disk liegende settings.json mit unbekanntem Profil-Key (z.B. weil ein
// Profil zwischenzeitlich entfernt wurde) darf GET /api/settings nicht mit
// dem ungültigen Wert beantworten — normalisiert wird auf "best".
func TestGetSettingsNormalizesUnknownProfile(t *testing.T) {
	dir := t.TempDir()
	settingsPath := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(settingsPath, []byte(`{"default_profile":"gibtsnicht"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(filepath.Join(dir, "jobs.json"))
	if err != nil {
		t.Fatal(err)
	}
	set, err := settings.Open(settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	h := server.New(st, queue.New(st, nopRunner{}, 1), fakeProber{}, set, fakeYtdlp{})
	rec := do(t, h, "GET", "/api/settings", nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"default_profile":"best"`) {
		t.Fatalf("Code %d: %s", rec.Code, rec.Body.String())
	}
}

// TestCreateVideoJobWithProfileDuplicate stellt sicher, dass der Duplikat-Check
// auch für den Profil-Pfad greift (nicht nur für manuell gewählte format_ids).
func TestCreateVideoJobWithProfileDuplicate(t *testing.T) {
	h, _, _ := newServer(t, fakeProber{})
	body := map[string]any{"type": "video", "url": "https://example.com/v", "profile": "1080p-mp4"}
	if rec := do(t, h, "POST", "/api/jobs", body); rec.Code != http.StatusCreated {
		t.Fatalf("erster: %d", rec.Code)
	}
	if rec := do(t, h, "POST", "/api/jobs", body); rec.Code != http.StatusConflict {
		t.Fatalf("Profil-Duplikat muss 409 liefern, war %d", rec.Code)
	}
}

// TestCreateVideoJobProfileWithAudioFormatIDs prüft den Profil-Modus-Ausdruck
// aus dem Brief: VideoExpr + "+" + join(ids, "+") + "/" + Expr.
func TestCreateVideoJobProfileWithAudioFormatIDs(t *testing.T) {
	h, st, _ := newServer(t, fakeProber{})
	rec := do(t, h, "POST", "/api/jobs", map[string]any{
		"type": "video", "url": "https://example.com/v", "profile": "1080p",
		"audio_format_ids": []string{"140-0", "140-1"},
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("Code %d: %s", rec.Code, rec.Body.String())
	}
	jobs := st.List()
	wantExpr := "bv*[height<=1080]+140-0+140-1/bv*[height<=1080]+ba/b[height<=1080]"
	if len(jobs) != 1 || jobs[0].Format != wantExpr {
		t.Fatalf("Format-Ausdruck falsch: %+v (want %q)", jobs, wantExpr)
	}
	if len(jobs[0].AudioFormatIDs) != 2 || jobs[0].AudioFormatIDs[0] != "140-0" || jobs[0].AudioFormatIDs[1] != "140-1" {
		t.Fatalf("AudioFormatIDs falsch: %+v", jobs[0])
	}
	if !jobs[0].MultiAudio {
		t.Fatalf("MultiAudio muss bei >1 IDs gesetzt sein: %+v", jobs[0])
	}
}

// TestCreateVideoJobProfileAudioOnlyIgnoresAudioFormatIDs deckt den
// Sonderfall aus dem Self-Review-Katalog ab: Profil "audio" hat einen leeren
// VideoExpr — audio_format_ids darf keinen kaputten Ausdruck "+id" erzeugen,
// sondern muss auf den bisherigen Profilausdruck zurückfallen.
func TestCreateVideoJobProfileAudioOnlyIgnoresAudioFormatIDs(t *testing.T) {
	h, st, _ := newServer(t, fakeProber{})
	rec := do(t, h, "POST", "/api/jobs", map[string]any{
		"type": "video", "url": "https://example.com/v", "profile": "audio",
		"audio_format_ids": []string{"140-0", "140-1"},
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("Code %d: %s", rec.Code, rec.Body.String())
	}
	jobs := st.List()
	if len(jobs) != 1 || jobs[0].Format != "ba" {
		t.Fatalf("Format-Ausdruck muss auf 'ba' zurückfallen (kein '+id'-Präfix): %+v", jobs)
	}
	if strings.HasPrefix(jobs[0].Format, "+") {
		t.Fatalf("kaputter Ausdruck mit führendem '+': %q", jobs[0].Format)
	}
}

// TestCreateVideoJobManualWithAudioFormatIDs prüft den manuellen Modus:
// BuildFormatMulti(format_video, ids, audio_only).
func TestCreateVideoJobManualWithAudioFormatIDs(t *testing.T) {
	h, st, _ := newServer(t, fakeProber{})
	rec := do(t, h, "POST", "/api/jobs", map[string]any{
		"type": "video", "url": "https://example.com/v", "format_video": "303",
		"audio_format_ids": []string{"140-0", "140-1"},
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("Code %d: %s", rec.Code, rec.Body.String())
	}
	jobs := st.List()
	if len(jobs) != 1 || jobs[0].Format != "303+140-0+140-1" {
		t.Fatalf("Format-Ausdruck falsch: %+v", jobs)
	}
	if !jobs[0].MultiAudio {
		t.Fatalf("MultiAudio muss bei >1 IDs gesetzt sein: %+v", jobs[0])
	}
}

// TestCreateVideoJobSingleAudioFormatIDNoMultiAudio: nur eine ID → MultiAudio
// bleibt false (Merge-Flags nur bei tatsächlich mehreren Tonspuren nötig).
func TestCreateVideoJobSingleAudioFormatIDNoMultiAudio(t *testing.T) {
	h, st, _ := newServer(t, fakeProber{})
	rec := do(t, h, "POST", "/api/jobs", map[string]any{
		"type": "video", "url": "https://example.com/v", "format_video": "303",
		"audio_format_ids": []string{"140-0"},
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("Code %d: %s", rec.Code, rec.Body.String())
	}
	jobs := st.List()
	if len(jobs) != 1 || jobs[0].Format != "303+140-0" {
		t.Fatalf("Format-Ausdruck falsch: %+v", jobs)
	}
	if jobs[0].MultiAudio {
		t.Fatalf("MultiAudio darf bei nur 1 ID nicht gesetzt sein: %+v", jobs[0])
	}
}
