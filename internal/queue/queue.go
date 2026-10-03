// Package queue verteilt Jobs aus dem Store auf maximal N parallele Runner.
package queue

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"ytdlweb/internal/intake"
	"ytdlweb/internal/job"
	"ytdlweb/internal/store"
	"ytdlweb/internal/ytdlp"
)

type Runner interface {
	Run(ctx context.Context, j job.Job, onProgress func(job.Progress)) error
}

// probeTimeout begrenzt die Analyse einer URL vor dem Download.
const probeTimeout = 60 * time.Second

// errPlaceholderReplaced meldet, dass der Platzhalter durch Playlist-Einträge ersetzt wurde.
var errPlaceholderReplaced = errors.New("platzhalter durch playlist-einträge ersetzt")

// Prober analysiert eine URL; passt zu (*ytdlp.Prober).Probe.
type Prober interface {
	Probe(ctx context.Context, rawURL string) (*ytdlp.ProbeResult, error)
}

// Option konfiguriert die Queue optional.
type Option func(*Queue)

// WithProber setzt den Prober für Jobs mit NeedsProbe.
func WithProber(p Prober) Option {
	return func(q *Queue) { q.prober = p }
}

type Queue struct {
	store   *store.Store
	runner  Runner
	sem     chan struct{}
	wake    chan struct{}
	mu      sync.Mutex
	cancels map[string]context.CancelFunc
	root    context.Context
	prober  Prober
}

func New(st *store.Store, r Runner, maxConcurrent int, opts ...Option) *Queue {
	if maxConcurrent < 1 {
		maxConcurrent = 1
	}
	q := &Queue{
		store:   st,
		runner:  r,
		sem:     make(chan struct{}, maxConcurrent),
		wake:    make(chan struct{}, 1),
		cancels: map[string]context.CancelFunc{},
	}
	for _, o := range opts {
		o(q)
	}
	return q
}

// Kick stößt den Dispatcher an; blockiert nie.
func (q *Queue) Kick() {
	select {
	case q.wake <- struct{}{}:
	default:
	}
}

func (q *Queue) Start(ctx context.Context) {
	q.root = ctx
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-q.wake:
			case <-ticker.C:
			}
			q.dispatch(ctx)
		}
	}()
}

func (q *Queue) dispatch(ctx context.Context) {
	for {
		select {
		case q.sem <- struct{}{}:
		default:
			return // kein freier Slot
		}
		j, ok := q.store.ClaimNextQueued()
		if !ok {
			<-q.sem
			return
		}
		jobCtx, cancel := context.WithCancel(ctx)
		q.mu.Lock()
		q.cancels[j.ID] = cancel
		q.mu.Unlock()
		go q.runJob(jobCtx, cancel, j)
	}
}

func (q *Queue) runJob(ctx context.Context, cancel context.CancelFunc, j job.Job) {
	defer func() {
		cancel()
		q.mu.Lock()
		delete(q.cancels, j.ID)
		q.mu.Unlock()
		<-q.sem
		q.Kick()
	}()
	var err error
	if j.NeedsProbe {
		err = q.analyze(ctx, &j)
		if errors.Is(err, errPlaceholderReplaced) {
			return // Platzhalter entfernt: kein Update, kein Runner-Aufruf
		}
	}
	if err == nil {
		err = q.runnerFor(j).Run(ctx, j, func(p job.Progress) { q.store.SetProgress(j.ID, p) })
	}
	var uerr error
	switch {
	case err == nil:
		uerr = q.store.Update(j.ID, func(x *job.Job) {
			x.State = job.StateDone
			x.Progress = job.Progress{Percent: 100}
			x.FinishedAt = now()
		})
	case ctx.Err() != nil:
		if q.root != nil && q.root.Err() != nil {
			// Shutdown, kein Nutzer-Abbruch: Zustand bleibt running,
			// die Crash-Recovery reiht den Job beim nächsten Start wieder ein.
			return
		}
		uerr = q.store.Update(j.ID, func(x *job.Job) {
			x.State = job.StateCanceled
			x.FinishedAt = now()
		})
	default:
		uerr = q.store.Update(j.ID, func(x *job.Job) {
			x.State = job.StateError
			x.Error = err.Error()
			x.FinishedAt = now()
		})
	}
	if uerr != nil {
		log.Printf("queue: Zustand von Job %s nicht persistiert: %v", j.ID, uerr)
	}
}

// runnerFor liefert den für Job j zu verwendenden Runner. Ist der
// konfigurierte Runner ein *ytdlp.ExecRunner, wird eine flache Kopie mit
// einem auf diesen Job gebundenen OnFilename-Callback zurückgegeben: der
// Callback (Task 5) ist ein Struct-Feld, kein Run()-Parameter — ihn direkt
// auf dem geteilten Runner zu setzen würde bei mehreren gleichzeitig
// laufenden Jobs (MaxConcurrent > 1) Job-übergreifend überschrieben, sobald
// ein zweiter Job dispatcht wird, während der erste noch läuft. Die Kopie
// übernimmt Bin/DownloadDir/OutputTemplate unverändert und bleibt pro Job
// unabhängig; der Callback schreibt den relativen Pfad nach job.Filename
// (letzter Aufruf gewinnt, falls yt-dlp die Zeile theoretisch mehrfach
// ausgibt — s. Task-5-Hinweis).
func (q *Queue) runnerFor(j job.Job) Runner {
	er, ok := q.runner.(*ytdlp.ExecRunner)
	if !ok {
		return q.runner
	}
	clone := *er
	clone.OnFilename = func(rel string) {
		if err := q.store.Update(j.ID, func(x *job.Job) { x.Filename = rel }); err != nil {
			log.Printf("queue: Dateiname von Job %s nicht persistiert: %v", j.ID, err)
		}
	}
	return &clone
}

// Cancel bricht einen laufenden oder wartenden Job ab.
func (q *Queue) Cancel(id string) {
	q.mu.Lock()
	cancel, running := q.cancels[id]
	q.mu.Unlock()
	if running {
		cancel()
		return
	}
	// Nicht (mehr) laufend — nur wartende Jobs direkt abbrechen.
	_ = q.store.Update(id, func(x *job.Job) {
		if x.State == job.StateQueued {
			x.State = job.StateCanceled
			x.FinishedAt = now()
		}
	})
}

// now liefert einen Pointer auf die aktuelle UTC-Zeit — analog zu
// job.New(), das CreatedAt ebenfalls per time.Now().UTC() setzt.
func now() *time.Time {
	t := time.Now().UTC()
	return &t
}

// analyze untersucht die URL eines NeedsProbe-Jobs: Video setzt den Titel,
// Playlist ersetzt den Platzhalter durch Einzel-Jobs.
func (q *Queue) analyze(ctx context.Context, j *job.Job) error {
	if q.prober == nil {
		return errors.New("Analyse nicht verfügbar")
	}
	pctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	res, err := q.prober.Probe(pctx, j.URL)
	if err != nil {
		if ctx.Err() == nil && errors.Is(pctx.Err(), context.DeadlineExceeded) {
			return fmt.Errorf("Analyse: Zeitlimit von %s überschritten", probeTimeout)
		}
		return err
	}
	switch {
	case res != nil && res.Type == "video" && res.Video != nil:
		title := res.Video.Title
		if err := q.store.Update(j.ID, func(x *job.Job) {
			x.Title = title
			x.NeedsProbe = false
		}); err != nil {
			return err
		}
		j.Title, j.NeedsProbe = title, false
		return nil
	case res != nil && res.Type == "playlist" && res.Playlist != nil:
		if len(res.Playlist.Entries) == 0 {
			return errors.New("Playlist enthält keine Einträge")
		}
		profile, ok := ytdlp.ProfileByKey(j.Profile)
		if !ok {
			return errors.New("unbekanntes Profil")
		}
		// Abbruch (Nutzer oder Shutdown) nach erfolgreicher Analyse: keine Einträge anlegen.
		if err := ctx.Err(); err != nil {
			return err
		}
		entries := make([]intake.Entry, 0, len(res.Playlist.Entries))
		for _, e := range res.Playlist.Entries {
			entries = append(entries, intake.Entry{URL: e.URL, Title: e.Title})
		}
		if _, _, err := intake.CreatePlaylistJobs(q.store, profile, res.Playlist.Title, entries); err != nil {
			return err
		}
		// Erst nach dem Anlegen entfernen; bei Fehlern bleibt der Platzhalter für einen Retry.
		if err := q.store.Remove(j.ID); err != nil {
			return err
		}
		return errPlaceholderReplaced
	}
	return errors.New("Analyse lieferte kein Ergebnis")
}
