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
}

func (s *Server) handleCreateJobs(w http.ResponseWriter, r *http.Request) {
	var req createJobsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "ungültiger Request-Body")
		return
	}
	switch req.Type {
	case "video":
		s.createVideoJob(w, req)
	case "playlist":
		s.createPlaylistJobs(w, req)
	default:
		writeError(w, http.StatusBadRequest, "type muss video oder playlist sein")
	}
}

func (s *Server) createVideoJob(w http.ResponseWriter, req createJobsRequest) {
	url := strings.TrimSpace(req.URL)
	if url == "" {
		writeError(w, http.StatusBadRequest, "url fehlt")
		return
	}
	var format, label string
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
			writeError(w, http.StatusBadRequest, "unbekanntes Profil")
			return
		}
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
	if s.isDuplicate(url, format) {
		writeError(w, http.StatusConflict, "Dieser Download läuft bereits")
		return
	}
	j := job.New(url, req.Title, format, label, "")
	j.AudioFormatIDs = ids
	j.MultiAudio = len(ids) > 1
	if err := s.store.Add(j); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.queue.Kick()
	writeJSON(w, http.StatusCreated, map[string]any{"ids": []string{j.ID}})
}

func (s *Server) createPlaylistJobs(w http.ResponseWriter, req createJobsRequest) {
	profile, ok := ytdlp.ProfileByKey(req.Profile)
	if !ok {
		writeError(w, http.StatusBadRequest, "unbekanntes Profil")
		return
	}
	if len(req.Entries) == 0 {
		writeError(w, http.StatusBadRequest, "keine Einträge")
		return
	}
	format, multiAudio := playlistFormat(profile)
	ids := []string{}
	skipped := 0
	for _, e := range req.Entries {
		url := strings.TrimSpace(e.URL)
		if url == "" || s.isDuplicate(url, format) {
			skipped++
			continue
		}
		j := job.New(url, e.Title, format, profile.Label, req.PlaylistTitle)
		j.MultiAudio = multiAudio
		if err := s.store.Add(j); err != nil {
			s.queue.Kick() // bereits angelegte Jobs nicht stranden lassen
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		ids = append(ids, j.ID)
	}
	s.queue.Kick()
	writeJSON(w, http.StatusCreated, map[string]any{"ids": ids, "skipped": skipped})
}

// playlistFormat baut den Format-Ausdruck für Playlist-Jobs: eine
// Sprach-Fallback-Kette, die die deutsche Synchro plus fremdsprachige
// Originalspur bevorzugt, ersatzweise irgendeine deutschsprachige Spur,
// sonst der bisherige Profil-Ausdruck ("Beste Qualität · de + en
// (Original)"-Regel aus Backlog-Idee 3, angewandt auf Playlists). Das
// zweite Kettenglied (Original-Zweitspur) filtert zusätzlich
// [language!^=de] — sonst würde bei Quellen ohne language_preference eine
// zweite deutsche Spur (z. B. eine zweite de-Synchro) fälschlich als
// "Original" mitgewählt und die de-Spur landet doppelt im Ausgabefile
// (gegen echtes YouTube/ARTE verifiziert, Final-Review-Fund 1). MultiAudio
// ist true, weil die Kette bis zu zwei Audiospuren kombinieren kann
// (Runner setzt dann --audio-multistreams). Beim Profil "audio" (kein
// VideoExpr) bleibt alles wie bisher — dort existiert kein Videoteil, mit
// dem sich mehrere Spuren kombinieren ließen (Ausnahme aus dem Brief).
func playlistFormat(profile ytdlp.Profile) (format string, multiAudio bool) {
	if profile.VideoExpr == "" {
		return profile.Expr, false
	}
	format = profile.VideoExpr + "+ba[language^=de]+ba[format_note*=original][language!^=de]/" +
		profile.VideoExpr + "+ba[language^=de]/" + profile.Expr
	return format, true
}

func (s *Server) isDuplicate(url, format string) bool {
	for _, j := range s.store.List() {
		if j.URL == url && j.Format == format &&
			(j.State == job.StateQueued || j.State == job.StateRunning) {
			return true
		}
	}
	return false
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
	if err := s.store.Remove(id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
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
