// Package queue verteilt Jobs aus dem Store auf maximal N parallele Runner.
package queue

import (
	"context"
	"log"
	"sync"
	"time"

	"ytdlweb/internal/job"
	"ytdlweb/internal/store"
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
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-q.wake:
			case <-time.After(time.Second):
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
	err := q.runner.Run(ctx, j, func(p job.Progress) { q.store.SetProgress(j.ID, p) })
	var uerr error
	switch {
	case err == nil:
		uerr = q.store.Update(j.ID, func(x *job.Job) {
			x.State = job.StateDone
			x.Progress = job.Progress{Percent: 100}
		})
	case ctx.Err() != nil:
		uerr = q.store.Update(j.ID, func(x *job.Job) { x.State = job.StateCanceled })
	default:
		uerr = q.store.Update(j.ID, func(x *job.Job) {
			x.State = job.StateError
			x.Error = err.Error()
		})
	}
	if uerr != nil {
		log.Printf("queue: Zustand von Job %s nicht persistiert: %v", j.ID, uerr)
	}
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
		}
	})
}
