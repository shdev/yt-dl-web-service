// Package server stellt die HTTP-API und die eingebettete UI bereit (Spec §6).
package server

import (
	"context"
	"encoding/json"
	"html/template"
	"log"
	"net/http"
	"strings"
	"time"

	"ytdlweb/internal/job"
	"ytdlweb/internal/queue"
	"ytdlweb/internal/store"
	"ytdlweb/internal/ytdlp"
	"ytdlweb/web"
)

type Prober interface {
	Probe(ctx context.Context, url string) (*ytdlp.ProbeResult, error)
}

type Server struct {
	store     *store.Store
	queue     *queue.Queue
	prober    Prober
	indexTmpl *template.Template
}

func New(st *store.Store, q *queue.Queue, p Prober) http.Handler {
	tmpl := template.Must(template.ParseFS(web.FS, "templates/index.html"))
	s := &Server{store: st, queue: q, prober: p, indexTmpl: tmpl}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.handleIndex)
	mux.Handle("GET /static/", http.FileServerFS(web.FS))
	mux.HandleFunc("GET /healthz", s.handleHealthz)
	mux.HandleFunc("POST /api/probe", s.handleProbe)
	mux.HandleFunc("GET /api/jobs", s.handleListJobs)
	mux.HandleFunc("POST /api/jobs", s.handleCreateJobs)
	mux.HandleFunc("POST /api/jobs/{id}/cancel", s.handleCancel)
	mux.HandleFunc("POST /api/jobs/{id}/retry", s.handleRetry)
	mux.HandleFunc("DELETE /api/jobs/{id}", s.handleDelete)
	return mux
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.indexTmpl.Execute(w, map[string]any{"Profiles": ytdlp.Profiles}); err != nil {
		log.Printf("index-template: %v", err)
	}
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	_, _ = w.Write([]byte("ok"))
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
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleListJobs(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"jobs": s.store.List()})
}

type entryPayload struct {
	URL   string `json:"url"`
	Title string `json:"title"`
}

type createJobsRequest struct {
	Type          string         `json:"type"`
	URL           string         `json:"url"`
	Title         string         `json:"title"`
	FormatVideo   string         `json:"format_video"`
	FormatAudio   string         `json:"format_audio"`
	AudioOnly     bool           `json:"audio_only"`
	FormatLabel   string         `json:"format_label"`
	Profile       string         `json:"profile"`
	PlaylistTitle string         `json:"playlist_title"`
	Entries       []entryPayload `json:"entries"`
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
	format := ytdlp.BuildFormat(req.FormatVideo, req.FormatAudio, req.AudioOnly)
	label := req.FormatLabel
	if label == "" {
		label = format
	}
	if s.isDuplicate(url, format) {
		writeError(w, http.StatusConflict, "Dieser Download läuft bereits")
		return
	}
	j := job.New(url, req.Title, format, label, "")
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
	ids := []string{}
	skipped := 0
	for _, e := range req.Entries {
		url := strings.TrimSpace(e.URL)
		if url == "" || s.isDuplicate(url, profile.Expr) {
			skipped++
			continue
		}
		j := job.New(url, e.Title, profile.Expr, profile.Label, req.PlaylistTitle)
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
