// Package queue verteilt Jobs aus dem Store auf maximal N parallele Runner.
package queue

import (
	"context"
	"log"
	"sync"
	"time"

	"ytdlweb/internal/job"
	"ytdlweb/internal/store"
	"ytdlweb/internal/ytdlp"
)

type Runner interface {
	Run(ctx context.Context, j job.Job, onProgress func(job.Progress)) error
}

type Queue struct {
	store   *store.Store
	runner  Runner
	sem     chan struct{}
	wake    chan struct{}
	mu      sync.Mutex
	cancels map[string]context.CancelFunc
	root    context.Context
}

func New(st *store.Store, r Runner, maxConcurrent int) *Queue {
	if maxConcurrent < 1 {
		maxConcurrent = 1
	}
	return &Queue{
		store:   st,
		runner:  r,
		sem:     make(chan struct{}, maxConcurrent),
		wake:    make(chan struct{}, 1),
		cancels: map[string]context.CancelFunc{},
	}
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
	err := q.runnerFor(j).Run(ctx, j, func(p job.Progress) { q.store.SetProgress(j.ID, p) })
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
