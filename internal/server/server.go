// Package server stellt die HTTP-API und die eingebettete UI bereit (Spec §6).
package server

import (
	"context"
	"encoding/json"
	"errors"
	"html/template"
	"log"
	"net/http"
	"strings"
	"time"

	"ytdlweb/internal/intake"
	"ytdlweb/internal/job"
	"ytdlweb/internal/queue"
	"ytdlweb/internal/settings"
	"ytdlweb/internal/store"
	"ytdlweb/internal/ytdlp"
	"ytdlweb/web"
)

type Prober interface {
	Probe(ctx context.Context, url string) (*ytdlp.ProbeResult, error)
}

// Ytdlp liefert Version und Selfupdate der yt-dlp-Installation (ytdlp.Manager).
type Ytdlp interface {
	Version(ctx context.Context) (string, error)
	Update(ctx context.Context) (string, error)
}

type Server struct {
	store     *store.Store
	queue     *queue.Queue
	prober    Prober
	settings  *settings.Store
	ytdlp     Ytdlp
	indexTmpl *template.Template
}

func New(st *store.Store, q *queue.Queue, p Prober, set *settings.Store, y Ytdlp) http.Handler {
	tmpl := template.Must(template.ParseFS(web.FS, "templates/index.html"))
	s := &Server{store: st, queue: q, prober: p, settings: set, ytdlp: y, indexTmpl: tmpl}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.handleIndex)
	mux.Handle("GET /static/", http.FileServerFS(web.FS))
	mux.HandleFunc("GET /manifest.webmanifest", s.handleManifest)
	mux.HandleFunc("GET /healthz", s.handleHealthz)
	mux.HandleFunc("POST /api/probe", s.handleProbe)
	mux.HandleFunc("GET /api/jobs", s.handleListJobs)
	mux.HandleFunc("POST /api/jobs", s.handleCreateJobs)
	mux.HandleFunc("POST /api/jobs/{id}/cancel", s.handleCancel)
	mux.HandleFunc("POST /api/jobs/{id}/retry", s.handleRetry)
	mux.HandleFunc("DELETE /api/jobs/{id}", s.handleDelete)
	mux.HandleFunc("GET /api/settings", s.handleGetSettings)
	mux.HandleFunc("PUT /api/settings", s.handlePutSettings)
	mux.HandleFunc("GET /api/ytdlp", s.handleYtdlpVersion)
	mux.HandleFunc("POST /api/ytdlp/update", s.handleYtdlpUpdate)
	return noStore(mux)
}

// noStore verbietet jedes clientseitige Caching — UI, Assets und API werden
// bei jedem Aufruf frisch geladen (bewusst gibt es auch keinen Service
// Worker). Gilt global: der Server liefert ausschließlich UI und API aus.
func noStore(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		h.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

// themeColors: muss zu den --bg-Tokens in web/src/input.css passen — die
// Werte färben Browser-Chrome/iOS-Statusbar bei erzwungenem Theme.
var themeColors = map[string]string{"dark": "#0f1116", "light": "#f6f7f9"}

// normalizeTheme klemmt unbekannte/leere Werte (Legacy-Dateien, alte
// Clients) auf "auto".
func normalizeTheme(theme string) string {
	if theme == "light" || theme == "dark" {
		return theme
	}
	return "auto"
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	theme := normalizeTheme(s.settings.Get().Theme)
	err := s.indexTmpl.Execute(w, map[string]any{
		"Profiles": ytdlp.Profiles,
		"Theme":    theme,
		// Leer bei "auto": dann rendern media-gebundene Metas beide Farben.
		"ThemeColor": themeColors[theme],
	})
	if err != nil {
		log.Printf("index-template: %v", err)
	}
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	_, _ = w.Write([]byte("ok"))
}

// handleManifest liefert das Web-App-Manifest mit korrektem MIME-Type aus —
// der FileServer würde .webmanifest nur als text/plain sniffen. Die Route
// liegt an der Root, damit iOS/Chrome den Scope "/" akzeptieren.
func (s *Server) handleManifest(w http.ResponseWriter, r *http.Request) {
	data, err := web.FS.ReadFile("static/manifest.webmanifest")
	if err != nil {
		http.Error(w, "manifest fehlt", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/manifest+json")
	_, _ = w.Write(data)
}

func (s *Server) handleProbe(w http.ResponseWriter, r *http.Request) {
	var req struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.URL) == "" {
		writeError(w, http.StatusBadRequest, "url fehlt")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	res, err := s.prober.Probe(ctx, strings.TrimSpace(req.URL))
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, probeResponse(res))
}

// probeVideoResponse bettet ytdlp.Video ein und ergänzt audio_languages —
// nur bei Einzelvideos relevant, deshalb kein Feld auf ytdlp.Video selbst.
type probeVideoResponse struct {
	ytdlp.Video
	AudioLanguages []ytdlp.AudioTrack `json:"audio_languages,omitempty"`
}

type probeResultResponse struct {
	Type     string              `json:"type"`
	Video    *probeVideoResponse `json:"video,omitempty"`
	Playlist *ytdlp.Playlist     `json:"playlist,omitempty"`
}

// probeResponse reichert die Probe-Antwort bei Einzelvideos um
// audio_languages an (RankAudio über die gemeldeten Formate, Task 2).
func probeResponse(res *ytdlp.ProbeResult) probeResultResponse {
	out := probeResultResponse{Type: res.Type, Playlist: res.Playlist}
	if res.Video != nil {
		out.Video = &probeVideoResponse{
			Video:          *res.Video,
			AudioLanguages: ytdlp.RankAudio(res.Video.Formats),
		}
	}
	return out
}

func (s *Server) handleListJobs(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"jobs": s.store.List()})
}

type entryPayload struct {
	URL   string `json:"url"`
	Title string `json:"title"`
}

type createJobsRequest struct {
	Type           string         `json:"type"`
	URL            string         `json:"url"`
	Title          string         `json:"title"`
	FormatVideo    string         `json:"format_video"`
	FormatAudio    string         `json:"format_audio"`
	AudioOnly      bool           `json:"audio_only"`
	FormatLabel    string         `json:"format_label"`
	Profile        string         `json:"profile"`
	PlaylistTitle  string         `json:"playlist_title"`
	Entries        []entryPayload `json:"entries"`
	AudioFormatIDs []string       `json:"audio_format_ids"`
	Replace        string         `json:"replace"`
}

// createOutcome ist das Ergebnis eines create*-Zweigs; handleCreateJobs
// schreibt die Antwort erst, nachdem ein ersetzter Job entfernt wurde.
type createOutcome struct {
	status int            // 201 bei Erfolg, sonst Fehlercode
	errMsg string         // Meldung bei status != 201
	ids    []string       // angelegte Job-IDs
	body   map[string]any // JSON-Antwort bei 201
}

func failOutcome(status int, msg string) createOutcome {
	return createOutcome{status: status, errMsg: msg}
}

// replaceable liefert den Job zu id, wenn er ersetzt werden darf
// (State error oder canceled).
func (s *Server) replaceable(id string) (job.Job, bool) {
	if id == "" {
		return job.Job{}, false
	}
	j, ok := s.store.Get(id)
	if !ok || (j.State != job.StateError && j.State != job.StateCanceled) {
		return job.Job{}, false
	}
	return j, true
}

// removeReplaced entfernt den ersetzten Job; der Zustand wird erneut
// geprüft, weil er zwischenzeitlich per Retry wieder queued sein kann.
func (s *Server) removeReplaced(id string) {
	_, err := s.store.RemoveIf(id, func(j job.Job) bool {
		return j.State == job.StateError || j.State == job.StateCanceled
	})
	if err != nil {
		log.Printf("server: ersetzter Job %s nicht entfernt: %v", id, err)
	}
}

func (s *Server) handleCreateJobs(w http.ResponseWriter, r *http.Request) {
	var req createJobsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "ungültiger Request-Body")
		return
	}
	replaced, canReplace := s.replaceable(req.Replace)
	playlistTitle := req.PlaylistTitle
	if playlistTitle == "" && canReplace {
		playlistTitle = replaced.PlaylistTitle
	}
	var out createOutcome
	switch req.Type {
	case "video":
		out = s.createVideoJob(req, playlistTitle)
	case "direct":
		out = s.createDirectJob(req, playlistTitle)
	case "playlist":
		out = s.createPlaylistJobs(req)
	default:
		writeError(w, http.StatusBadRequest, "type muss video, playlist oder direct sein")
		return
	}
	if out.status != http.StatusCreated {
		writeError(w, out.status, out.errMsg)
		return
	}
	if canReplace && len(out.ids) > 0 {
		s.removeReplaced(req.Replace)
	}
	writeJSON(w, http.StatusCreated, out.body)
}

func (s *Server) createDirectJob(req createJobsRequest, playlistTitle string) createOutcome {
	url := strings.TrimSpace(req.URL)
	if url == "" {
		return failOutcome(http.StatusBadRequest, "url fehlt")
	}
	profile, ok := ytdlp.ProfileByKey(req.Profile)
	if !ok {
		return failOutcome(http.StatusBadRequest, "unbekanntes Profil")
	}
	format, multi := intake.PlaylistFormat(profile)
	if intake.IsDuplicate(s.store, url, format) {
		return failOutcome(http.StatusConflict, "Dieser Download läuft bereits")
	}
	j := job.New(url, "", format, profile.Label, playlistTitle)
	j.MultiAudio = multi
	j.Profile = profile.Key
	j.NeedsProbe = true
	if err := s.store.Add(j); err != nil {
		return failOutcome(http.StatusInternalServerError, err.Error())
	}
	s.queue.Kick()
	return createOutcome{status: http.StatusCreated, ids: []string{j.ID},
		body: map[string]any{"ids": []string{j.ID}}}
}

func (s *Server) createVideoJob(req createJobsRequest, playlistTitle string) createOutcome {
	url := strings.TrimSpace(req.URL)
	if url == "" {
		return failOutcome(http.StatusBadRequest, "url fehlt")
	}
	var format, label, profileKey string
	// ids sind die tatsächlich verwendeten Audiospuren — in Audio-only-
	// Kontexten (Profil "audio" bzw. audio_only=true) auf die erste Spur
	// reduziert, weil dort kein Videoteil existiert, mit dem sich mehrere
	// Spuren kombinieren ließen. Die erste ID ist per RankAudio-Konvention
	// die bevorzugte Sprache. job.AudioFormatIDs/MultiAudio unten leiten
	// sich aus diesem (ggf. reduzierten) ids ab, nicht aus req.AudioFormatIDs
	// direkt — sonst widersprechen sich Job-Zustand und der tatsächlich
	// gebaute yt-dlp-Ausdruck (Controller-Ruling, Fix-Runde 1).
	ids := req.AudioFormatIDs
	if req.Profile != "" {
		profile, ok := ytdlp.ProfileByKey(req.Profile)
		if !ok {
			return failOutcome(http.StatusBadRequest, "unbekanntes Profil")
		}
		profileKey = profile.Key
		format = profile.Expr
		if len(ids) > 0 {
			if profile.VideoExpr == "" {
				// Profil "audio": kein Videoteil zum Kombinieren — Format
				// wird direkt die bevorzugte (erste) Spur.
				ids = ids[:1]
				format = ids[0]
			} else {
				format = profile.VideoExpr + "+" + strings.Join(ids, "+") + "/" + profile.Expr
			}
		}
		label = profile.Label
		// Controller-Ruling (Task 8): das clientseitig angereicherte Label
		// (z. B. "Beste Qualität · de + en (Original)") gilt nur, wenn
		// tatsächlich Audiospuren gewählt wurden — sonst bleibt profile.Label
		// maßgeblich, auch wenn das Formular noch ein altes format_label trägt.
		if req.FormatLabel != "" && len(ids) > 0 {
			label = req.FormatLabel
		}
	} else {
		if req.AudioOnly && len(ids) > 0 {
			ids = ids[:1]
		}
		if len(ids) > 0 {
			format = ytdlp.BuildFormatMulti(req.FormatVideo, ids, req.AudioOnly)
		} else {
			format = ytdlp.BuildFormat(req.FormatVideo, req.FormatAudio, req.AudioOnly)
		}
		label = req.FormatLabel
		if label == "" {
			label = format
		}
	}
	if intake.IsDuplicate(s.store, url, format) {
		return failOutcome(http.StatusConflict, "Dieser Download läuft bereits")
	}
	j := job.New(url, req.Title, format, label, playlistTitle)
	j.AudioFormatIDs = ids
	j.Profile = profileKey
	j.MultiAudio = len(ids) > 1
	if err := s.store.Add(j); err != nil {
		return failOutcome(http.StatusInternalServerError, err.Error())
	}
	s.queue.Kick()
	return createOutcome{status: http.StatusCreated, ids: []string{j.ID},
		body: map[string]any{"ids": []string{j.ID}}}
}

func (s *Server) createPlaylistJobs(req createJobsRequest) createOutcome {
	profile, ok := ytdlp.ProfileByKey(req.Profile)
	if !ok {
		return failOutcome(http.StatusBadRequest, "unbekanntes Profil")
	}
	if len(req.Entries) == 0 {
		return failOutcome(http.StatusBadRequest, "keine Einträge")
	}
	entries := make([]intake.Entry, len(req.Entries))
	for i, e := range req.Entries {
		entries[i] = intake.Entry(e)
	}
	ids, skipped, err := intake.CreatePlaylistJobs(s.store, profile, req.PlaylistTitle, entries)
	if err != nil {
		s.queue.Kick() // bereits angelegte Jobs nicht stranden lassen
		return failOutcome(http.StatusInternalServerError, err.Error())
	}
	s.queue.Kick()
	return createOutcome{status: http.StatusCreated, ids: ids,
		body: map[string]any{"ids": ids, "skipped": skipped}}
}

func (s *Server) handleCancel(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	j, ok := s.store.Get(id)
	if !ok {
		writeError(w, http.StatusNotFound, "Job nicht gefunden")
		return
	}
	if j.State != job.StateQueued && j.State != job.StateRunning {
		writeError(w, http.StatusConflict, "Job läuft nicht")
		return
	}
	s.queue.Cancel(id)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleRetry(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	j, ok := s.store.Get(id)
	if !ok {
		writeError(w, http.StatusNotFound, "Job nicht gefunden")
		return
	}
	if j.State != job.StateError && j.State != job.StateCanceled {
		writeError(w, http.StatusConflict, "nur fehlgeschlagene oder abgebrochene Jobs")
		return
	}
	if err := s.store.Update(id, func(x *job.Job) {
		x.State = job.StateQueued
		x.Error = ""
		x.Progress = job.Progress{}
		x.FinishedAt = nil
		x.Filename = ""
	}); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.queue.Kick()
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	j, ok := s.store.Get(id)
	if !ok {
		writeError(w, http.StatusNotFound, "Job nicht gefunden")
		return
	}
	if j.State == job.StateRunning {
		writeError(w, http.StatusConflict, "laufenden Job zuerst abbrechen")
		return
	}
	removed, err := s.store.RemoveIf(id, func(x job.Job) bool { return x.State != job.StateRunning })
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !removed {
		// Zwischen Get und Entfernen geändert: läuft jetzt oder ist weg.
		if cur, still := s.store.Get(id); still && cur.State == job.StateRunning {
			writeError(w, http.StatusConflict, "laufenden Job zuerst abbrechen")
		} else {
			writeError(w, http.StatusNotFound, "Job nicht gefunden")
		}
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleYtdlpVersion(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	v, err := s.ytdlp.Version(ctx)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"version": v})
}

func (s *Server) handleYtdlpUpdate(w http.ResponseWriter, r *http.Request) {
	// Großzügiges Timeout: -U lädt das komplette Binary neu herunter.
	// Vom Request-Context entkoppelt: ein Tab-Reload oder Proxy-Timeout
	// darf ein einmal angestoßenes Update nicht mehr abbrechen.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 3*time.Minute)
	defer cancel()
	v, err := s.ytdlp.Update(ctx)
	if errors.Is(err, ytdlp.ErrUpdateRunning) {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	if err != nil {
		// Auch serverseitig festhalten — die Antwort erreicht den Client
		// nach einem Abbruch sonst nie.
		log.Printf("yt-dlp-Update fehlgeschlagen: %v", err)
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	log.Printf("yt-dlp aktualisiert: %s", v)
	writeJSON(w, http.StatusOK, map[string]string{"version": v})
}

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	set := s.settings.Get()
	if _, ok := ytdlp.ProfileByKey(set.DefaultProfile); !ok {
		set.DefaultProfile = "best"
	}
	set.Theme = normalizeTheme(set.Theme)
	writeJSON(w, http.StatusOK, set)
}

func (s *Server) handlePutSettings(w http.ResponseWriter, r *http.Request) {
	var set settings.Settings
	if err := json.NewDecoder(r.Body).Decode(&set); err != nil {
		writeError(w, http.StatusBadRequest, "ungültiger Request-Body")
		return
	}
	if _, ok := ytdlp.ProfileByKey(set.DefaultProfile); !ok {
		writeError(w, http.StatusBadRequest, "unbekanntes Profil")
		return
	}
	// Fehlendes theme-Feld (alte Clients) wird als "auto" gespeichert;
	// explizit falsche Werte sind ein Fehler.
	switch set.Theme {
	case "":
		set.Theme = "auto"
	case "auto", "light", "dark":
	default:
		writeError(w, http.StatusBadRequest, "unbekanntes Theme")
		return
	}
	if err := s.settings.Set(set); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
