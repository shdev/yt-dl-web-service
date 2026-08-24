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

// TestIndexPWAHead: die Seite muss sich als Home-Screen-App auf iOS
// installieren lassen (Apple-Meta-Tags, Manifest, Icon) und der Viewport
// muss den Fokus-Auto-Zoom unterbinden (maximum-scale=1).
func TestIndexPWAHead(t *testing.T) {
	h, _, _ := newServer(t, fakeProber{})
	body := do(t, h, "GET", "/", nil).Body.String()
	for _, want := range []string{
		"maximum-scale=1",
		"user-scalable=no",
		"viewport-fit=cover",
		`name="apple-mobile-web-app-capable" content="yes"`,
		`name="apple-mobile-web-app-status-bar-style"`,
		`rel="manifest" href="/manifest.webmanifest"`,
		`rel="apple-touch-icon" href="/static/icons/apple-touch-icon.png"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("Index-Head: %q fehlt", want)
		}
	}
}

func TestManifest(t *testing.T) {
	h, _, _ := newServer(t, fakeProber{})
	rec := do(t, h, "GET", "/manifest.webmanifest", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("Manifest-Route: %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/manifest+json" {
		t.Errorf("Content-Type: %q", ct)
	}
	var m struct {
		Display  string `json:"display"`
		StartURL string `json:"start_url"`
		Scope    string `json:"scope"`
		Icons    []struct {
			Src string `json:"src"`
		} `json:"icons"`
		// share_target: geteilte Links landen als GET-Query auf / —
		// Android-Teilen-Menü; die url/text-Übernahme macht app.js.
		ShareTarget struct {
			Action string `json:"action"`
			Method string `json:"method"`
			Params struct {
				URL  string `json:"url"`
				Text string `json:"text"`
			} `json:"params"`
		} `json:"share_target"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("Manifest kein gültiges JSON: %v", err)
	}
	if m.Display != "standalone" || m.StartURL != "/" || m.Scope != "/" {
		t.Errorf("display=%q start_url=%q scope=%q", m.Display, m.StartURL, m.Scope)
	}
	if m.ShareTarget.Action != "/" || m.ShareTarget.Method != "GET" ||
		m.ShareTarget.Params.URL != "url" || m.ShareTarget.Params.Text != "text" {
		t.Errorf("share_target unvollständig: %+v", m.ShareTarget)
	}
	if len(m.Icons) == 0 {
		t.Error("Manifest ohne Icons")
	}
	for _, ic := range m.Icons {
		if rec := do(t, h, "GET", ic.Src, nil); rec.Code != http.StatusOK {
			t.Errorf("Manifest-Icon %s: %d", ic.Src, rec.Code)
		}
	}
}

// TestNoStoreCacheControl: nichts darf clientseitig gecacht werden — jede
// Antwort (UI, Assets, API) trägt Cache-Control: no-store.
func TestNoStoreCacheControl(t *testing.T) {
	h, _, _ := newServer(t, fakeProber{})
	for _, path := range []string{"/", "/static/app.js", "/static/app.css", "/manifest.webmanifest", "/api/jobs"} {
		rec := do(t, h, "GET", path, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: %d", path, rec.Code)
		}
		if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
			t.Errorf("%s: Cache-Control %q, erwartet no-store", path, cc)
		}
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

// TestProbeReturnsPlaylist ist ein Regressionstest: probeResponse() darf den
// Playlist-Zweig (kein Video, also keine audio_languages-Anreicherung) nicht
// verändern.
func TestProbeReturnsPlaylist(t *testing.T) {
	res := &ytdlp.ProbeResult{
		Type: "playlist",
		Playlist: &ytdlp.Playlist{
			Title:   "Liste",
			Entries: []ytdlp.PlaylistEntry{{URL: "https://example.com/1", Title: "Eins"}},
		},
	}
	h, _, _ := newServer(t, fakeProber{res: res})
	rec := do(t, h, "POST", "/api/probe", map[string]string{"url": "https://example.com/liste"})
	if rec.Code != http.StatusOK {
		t.Fatalf("Code %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"type":"playlist"`) || !strings.Contains(body, `"title":"Liste"`) ||
		!strings.Contains(body, `"url":"https://example.com/1"`) {
		t.Fatalf("Playlist-Antwort unvollständig: %s", body)
	}
	if strings.Contains(body, `"video"`) || strings.Contains(body, "audio_languages") {
		t.Fatalf("Playlist-Antwort darf kein video/audio_languages enthalten: %s", body)
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

// TestCreateVideoJobProfileFormatLabelOverride: im Profil-Modus mit
// audio_format_ids muss das clientseitig angereicherte Sprachen-Label
// (format_label, z. B. "Beste Qualität · de + en (Original)") übernommen
// werden — sonst geht es verloren, weil ohne diesen Fix immer profile.Label
// gilt (Controller-Ruling Task 8).
func TestCreateVideoJobProfileFormatLabelOverride(t *testing.T) {
	h, st, _ := newServer(t, fakeProber{})
	rec := do(t, h, "POST", "/api/jobs", map[string]any{
		"type": "video", "url": "https://example.com/v", "title": "Test",
		"profile": "best", "audio_format_ids": []string{"140-0", "140-7"},
		"format_label": "Beste Qualität · de + en (Original)",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("Code %d: %s", rec.Code, rec.Body.String())
	}
	jobs := st.List()
	if len(jobs) != 1 || jobs[0].FormatLabel != "Beste Qualität · de + en (Original)" {
		t.Fatalf("clientseitiges Label muss im Profil-Modus mit IDs übernommen werden: %+v", jobs)
	}
}

// TestCreateVideoJobProfileWithoutIDsKeepsProfileLabel: ohne
// audio_format_ids bleibt profile.Label maßgeblich, selbst wenn ein
// format_label mitgeschickt wird (Controller-Ruling Task 8) — die
// Übernahme gilt nur, wenn tatsächlich Audiospuren gewählt wurden.
func TestCreateVideoJobProfileWithoutIDsKeepsProfileLabel(t *testing.T) {
	h, st, _ := newServer(t, fakeProber{})
	rec := do(t, h, "POST", "/api/jobs", map[string]any{
		"type": "video", "url": "https://example.com/v", "title": "Test",
		"profile": "best", "format_label": "Sollte ignoriert werden",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("Code %d: %s", rec.Code, rec.Body.String())
	}
	jobs := st.List()
	if len(jobs) != 1 || jobs[0].FormatLabel != "Beste Qualität" {
		t.Fatalf("ohne IDs muss profile.Label gelten: %+v", jobs)
	}
}

// TestCreateVideoJobAudioProfileFormatLabelOverride: die Label-Übernahme
// gilt auch im Profil "audio", dessen ids-Handling die IDs auf die
// bevorzugte erste Spur reduziert (server.go createVideoJob) — das darf
// die Label-Übernahme nicht unterlaufen.
func TestCreateVideoJobAudioProfileFormatLabelOverride(t *testing.T) {
	h, st, _ := newServer(t, fakeProber{})
	rec := do(t, h, "POST", "/api/jobs", map[string]any{
		"type": "video", "url": "https://example.com/v", "title": "Test",
		"profile": "audio", "audio_format_ids": []string{"140-0"},
		"format_label": "Nur Audio · de",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("Code %d: %s", rec.Code, rec.Body.String())
	}
	jobs := st.List()
	if len(jobs) != 1 || jobs[0].FormatLabel != "Nur Audio · de" {
		t.Fatalf("Label-Übernahme muss auch im Profil audio gelten: %+v", jobs)
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

// TestCreatePlaylistJobs prüft die Sprach-Fallback-Kette (Task 8, Step 1):
// bevorzugt deutsche Synchro mit Original-Zweitspur, dann irgendeine
// deutschsprachige Spur, sonst der bisherige Profil-Ausdruck — plus
// MultiAudio, weil die Kette bis zu zwei Audiospuren kombinieren kann.
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
	wantFormat := "bv*[height<=720]+ba[language^=de]+ba[format_note*=original][language!^=de]/" +
		"bv*[height<=720]+ba[language^=de]/bv*[height<=720]+ba/b[height<=720]"
	for _, j := range jobs {
		if j.Format != wantFormat || j.PlaylistTitle != "Liste" {
			t.Fatalf("Playlist-Job falsch: %+v", j)
		}
		if !j.MultiAudio {
			t.Fatalf("Playlist-Job muss MultiAudio setzen: %+v", j)
		}
	}
}

// TestCreatePlaylistJobs1080pMp4Chain prüft die Sprach-Fallback-Kette für ein
// Profil, dessen eigener Expr bereits ein Audio-Fallback-Glied enthält
// ("+ba[ext=m4a]") — die Kette darf dieses Glied nicht verdrängen, sondern
// hängt sich nur vor den unveränderten Profil-Ausdruck (Final-Review-Fund 1).
func TestCreatePlaylistJobs1080pMp4Chain(t *testing.T) {
	h, st, _ := newServer(t, fakeProber{})
	rec := do(t, h, "POST", "/api/jobs", map[string]any{
		"type": "playlist", "profile": "1080p-mp4", "playlist_title": "Liste",
		"entries": []map[string]string{{"url": "https://example.com/1", "title": "Eins"}},
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("Code %d: %s", rec.Code, rec.Body.String())
	}
	jobs := st.List()
	wantFormat := "bv*[height<=1080][ext=mp4]+ba[language^=de]+ba[format_note*=original][language!^=de]/" +
		"bv*[height<=1080][ext=mp4]+ba[language^=de]/" +
		"bv*[height<=1080][ext=mp4]+ba[ext=m4a]/b[ext=mp4][height<=1080]/b[height<=1080]"
	if len(jobs) != 1 || jobs[0].Format != wantFormat {
		t.Fatalf("Playlist-Job (1080p-mp4) falsch: %+v", jobs)
	}
	if !jobs[0].MultiAudio {
		t.Fatalf("Playlist-Job muss MultiAudio setzen: %+v", jobs[0])
	}
}

// TestCreatePlaylistJobsDedupe: derselbe Playlist-Eintrag mit demselben
// Profil zweimal eingereicht — der zweite Request darf keinen neuen Job
// anlegen, sondern muss ihn als "skipped" zählen, weil der deterministische
// Kettenausdruck (playlistFormat) beim ersten Job bereits denselben Format-
// String erzeugt hat (Final-Review-Fund 1, Regressionstest).
func TestCreatePlaylistJobsDedupe(t *testing.T) {
	h, st, _ := newServer(t, fakeProber{})
	body := map[string]any{
		"type": "playlist", "profile": "720p", "playlist_title": "Liste",
		"entries": []map[string]string{{"url": "https://example.com/1", "title": "Eins"}},
	}
	rec := do(t, h, "POST", "/api/jobs", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("erster Request: Code %d: %s", rec.Code, rec.Body.String())
	}
	var first struct {
		IDs     []string `json:"ids"`
		Skipped int      `json:"skipped"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &first); err != nil {
		t.Fatal(err)
	}
	if len(first.IDs) != 1 || first.Skipped != 0 {
		t.Fatalf("erster Request unerwartet: %+v", first)
	}

	rec = do(t, h, "POST", "/api/jobs", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("zweiter Request: Code %d: %s", rec.Code, rec.Body.String())
	}
	var second struct {
		IDs     []string `json:"ids"`
		Skipped int      `json:"skipped"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &second); err != nil {
		t.Fatal(err)
	}
	if len(second.IDs) != 0 || second.Skipped != 1 {
		t.Fatalf("zweiter Request muss den Eintrag als Duplikat überspringen: %+v", second)
	}
	if len(st.List()) != 1 {
		t.Fatalf("es darf nur ein Job angelegt worden sein: %+v", st.List())
	}
}

// TestCreatePlaylistJobsAudioProfileUnchanged: beim Profil "audio" (kein
// VideoExpr zum Kombinieren) bleibt der bisherige Expr unverändert und
// MultiAudio false — die Sprach-Fallback-Kette gilt nur für Profile mit
// Videoteil (Task 8, Ausnahme aus dem Brief).
func TestCreatePlaylistJobsAudioProfileUnchanged(t *testing.T) {
	h, st, _ := newServer(t, fakeProber{})
	rec := do(t, h, "POST", "/api/jobs", map[string]any{
		"type": "playlist", "profile": "audio", "playlist_title": "Liste",
		"entries": []map[string]string{{"url": "https://example.com/1", "title": "Eins"}},
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("Code %d: %s", rec.Code, rec.Body.String())
	}
	jobs := st.List()
	if len(jobs) != 1 || jobs[0].Format != "ba" {
		t.Fatalf("Profil audio muss unverändert bleiben: %+v", jobs)
	}
	if jobs[0].MultiAudio {
		t.Fatalf("Profil audio darf MultiAudio nicht setzen: %+v", jobs[0])
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

// --- Theme-Umschalter --------------------------------------------------------

func TestGetSettingsDefaultTheme(t *testing.T) {
	h, _, _ := newServer(t, fakeProber{})
	rec := do(t, h, "GET", "/api/settings", nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"theme":"auto"`) {
		t.Fatalf("Theme-Default fehlt: %d %s", rec.Code, rec.Body.String())
	}
}

func TestPutSettingsTheme(t *testing.T) {
	h, _, _ := newServer(t, fakeProber{})
	rec := do(t, h, "PUT", "/api/settings",
		map[string]string{"default_profile": "best", "theme": "dark"})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("PUT Code %d: %s", rec.Code, rec.Body.String())
	}
	rec = do(t, h, "GET", "/api/settings", nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"theme":"dark"`) {
		t.Fatalf("GET nach PUT: %d %s", rec.Code, rec.Body.String())
	}
}

// TestPutSettingsEmptyThemeNormalizesToAuto: ein PUT ohne theme-Feld (alte
// Clients bzw. reine Profil-Änderung) darf das Theme nicht auf einen
// ungültigen Leerwert setzen — es wird als "auto" gespeichert.
func TestPutSettingsEmptyThemeNormalizesToAuto(t *testing.T) {
	h, _, _ := newServer(t, fakeProber{})
	rec := do(t, h, "PUT", "/api/settings", map[string]string{"default_profile": "best"})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("PUT Code %d: %s", rec.Code, rec.Body.String())
	}
	rec = do(t, h, "GET", "/api/settings", nil)
	if !strings.Contains(rec.Body.String(), `"theme":"auto"`) {
		t.Fatalf("Theme nicht normalisiert: %s", rec.Body.String())
	}
}

func TestPutSettingsUnknownTheme(t *testing.T) {
	h, _, _ := newServer(t, fakeProber{})
	rec := do(t, h, "PUT", "/api/settings",
		map[string]string{"default_profile": "best", "theme": "neon"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unbekanntes Theme: %d", rec.Code)
	}
}

// TestIndexRendersTheme: das gespeicherte Theme landet serverseitig als
// data-theme am <html>-Element (kein Theme-Flackern beim Laden) und steuert
// die theme-color-Metas; die Settings-Card enthält den Umschalter.
func TestIndexRendersTheme(t *testing.T) {
	h, _, _ := newServer(t, fakeProber{})

	body := do(t, h, "GET", "/", nil).Body.String()
	if !strings.Contains(body, `<html lang="de" data-theme="auto">`) {
		t.Errorf("data-theme=auto fehlt im Index")
	}
	// Auto: zwei media-gebundene theme-color-Metas.
	if !strings.Contains(body, `name="theme-color" media="(prefers-color-scheme: dark)"`) ||
		!strings.Contains(body, `name="theme-color" media="(prefers-color-scheme: light)"`) {
		t.Errorf("media-gebundene theme-color-Metas fehlen bei auto")
	}
	for _, id := range []string{`id="theme-auto"`, `id="theme-light"`, `id="theme-dark"`} {
		if !strings.Contains(body, id) {
			t.Errorf("Theme-Umschalter: %s fehlt", id)
		}
	}

	if rec := do(t, h, "PUT", "/api/settings",
		map[string]string{"default_profile": "best", "theme": "dark"}); rec.Code != http.StatusNoContent {
		t.Fatalf("PUT: %d", rec.Code)
	}
	body = do(t, h, "GET", "/", nil).Body.String()
	if !strings.Contains(body, `<html lang="de" data-theme="dark">`) {
		t.Errorf("data-theme=dark fehlt nach PUT")
	}
	// checked muss serverseitig am gespeicherten Theme hängen — der Client
	// zeigt sonst nach fehlgeschlagenem Settings-Fetch "System" an.
	if !strings.Contains(body, `id="theme-dark" value="dark" checked`) ||
		strings.Contains(body, `id="theme-auto" value="auto" checked`) {
		t.Errorf("checked folgt nicht dem gespeicherten Theme")
	}
	// Erzwungenes Theme: eine feste theme-color statt der media-Metas.
	if !strings.Contains(body, `<meta name="theme-color" content="#0f1116">`) ||
		strings.Contains(body, `media="(prefers-color-scheme: light)"`) {
		t.Errorf("feste theme-color bei dark fehlt bzw. media-Metas noch da")
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

// TestCreateVideoJobProfileAudioOnlyUsesFirstAudioFormatID deckt den
// Controller-Ruling-Fix ab (Fix-Runde 1): Profil "audio" hat einen leeren
// VideoExpr — mehrere audio_format_ids ergäben dort keinen Sinn (kein
// Videoteil zum Kombinieren), deshalb wird auf die erste ID reduziert
// (bevorzugte Sprache per RankAudio-Konvention). Format-Ausdruck UND
// persistierter Job-Zustand (AudioFormatIDs, MultiAudio) müssen dieselbe,
// reduzierte Liste widerspiegeln — vorher liefen sie auseinander
// (Format="ba" ignorierte die IDs komplett, aber MultiAudio=true und beide
// IDs wurden trotzdem gespeichert).
func TestCreateVideoJobProfileAudioOnlyUsesFirstAudioFormatID(t *testing.T) {
	h, st, _ := newServer(t, fakeProber{})
	rec := do(t, h, "POST", "/api/jobs", map[string]any{
		"type": "video", "url": "https://example.com/v", "profile": "audio",
		"audio_format_ids": []string{"140-0", "140-1"},
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("Code %d: %s", rec.Code, rec.Body.String())
	}
	jobs := st.List()
	if len(jobs) != 1 || jobs[0].Format != "140-0" {
		t.Fatalf("Format-Ausdruck muss die erste ID sein (kein 'ba'-Fallback, kein '+id'-Präfix): %+v", jobs)
	}
	if len(jobs[0].AudioFormatIDs) != 1 || jobs[0].AudioFormatIDs[0] != "140-0" {
		t.Fatalf("AudioFormatIDs muss auf die erste ID reduziert sein: %+v", jobs[0])
	}
	if jobs[0].MultiAudio {
		t.Fatalf("MultiAudio darf im Audio-only-Kontext nicht gesetzt sein: %+v", jobs[0])
	}
}

// TestCreateVideoJobProfileAudioWithoutIDsStaysOnBa: ohne audio_format_ids
// bleibt das Profil "audio" unverändert bei "ba" — der Fix darf das
// bestehende Verhalten ohne IDs nicht anfassen.
func TestCreateVideoJobProfileAudioWithoutIDsStaysOnBa(t *testing.T) {
	h, st, _ := newServer(t, fakeProber{})
	rec := do(t, h, "POST", "/api/jobs", map[string]any{
		"type": "video", "url": "https://example.com/v", "profile": "audio",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("Code %d: %s", rec.Code, rec.Body.String())
	}
	jobs := st.List()
	if len(jobs) != 1 || jobs[0].Format != "ba" {
		t.Fatalf("ohne IDs muss Profil 'audio' bei 'ba' bleiben: %+v", jobs)
	}
	if jobs[0].MultiAudio || len(jobs[0].AudioFormatIDs) != 0 {
		t.Fatalf("ohne IDs dürfen AudioFormatIDs/MultiAudio nicht gesetzt sein: %+v", jobs[0])
	}
}

// TestCreateVideoJobManualAudioOnlyUsesFirstAudioFormatID deckt denselben
// Controller-Ruling-Fix im manuellen Modus ab: audio_only=true + mehrere IDs
// — BuildFormatMulti liefert intern bereits nur die erste Spur, aber vor dem
// Fix wurden trotzdem beide IDs persistiert und MultiAudio gesetzt, obwohl
// nur eine Spur tatsächlich geladen wird.
func TestCreateVideoJobManualAudioOnlyUsesFirstAudioFormatID(t *testing.T) {
	h, st, _ := newServer(t, fakeProber{})
	rec := do(t, h, "POST", "/api/jobs", map[string]any{
		"type": "video", "url": "https://example.com/v", "audio_only": true,
		"audio_format_ids": []string{"140-0", "140-1"},
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("Code %d: %s", rec.Code, rec.Body.String())
	}
	jobs := st.List()
	if len(jobs) != 1 || jobs[0].Format != "140-0" {
		t.Fatalf("Format-Ausdruck muss die erste ID sein: %+v", jobs)
	}
	if len(jobs[0].AudioFormatIDs) != 1 || jobs[0].AudioFormatIDs[0] != "140-0" {
		t.Fatalf("AudioFormatIDs muss auf die erste ID reduziert sein: %+v", jobs[0])
	}
	if jobs[0].MultiAudio {
		t.Fatalf("MultiAudio darf im Audio-only-Kontext nicht gesetzt sein: %+v", jobs[0])
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
