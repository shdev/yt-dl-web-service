package queue_test

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"ytdlweb/internal/job"
	"ytdlweb/internal/queue"
	"ytdlweb/internal/store"
)

// fakeRunner meldet gestartete Jobs und blockiert bis release geschlossen wird.
type fakeRunner struct {
	started chan string
	release chan struct{}
	fail    error
}

func (f *fakeRunner) Run(ctx context.Context, j job.Job, onProgress func(job.Progress)) error {
	f.started <- j.ID
	select {
	case <-f.release:
	case <-ctx.Done():
		return ctx.Err()
	}
	return f.fail
}

func newQueue(t *testing.T, fr *fakeRunner, maxConcurrent int) (*queue.Queue, *store.Store) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "jobs.json"))
	if err != nil {
		t.Fatal(err)
	}
	q := queue.New(st, fr, maxConcurrent)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	q.Start(ctx)
	return q, st
}

func addJob(t *testing.T, st *store.Store, i int) job.Job {
	t.Helper()
	j := job.New(fmt.Sprintf("https://example.com/%d", i), "t", "ba", "l", "")
	if err := st.Add(j); err != nil {
		t.Fatal(err)
	}
	return j
}

func waitState(t *testing.T, st *store.Store, id string, want job.State) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if j, ok := st.Get(id); ok && j.State == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	j, _ := st.Get(id)
	t.Fatalf("job %s: Zustand %s, erwartet %s", id, j.State, want)
}

func TestQueueRunsJobToDone(t *testing.T) {
	fr := &fakeRunner{started: make(chan string, 1), release: make(chan struct{})}
	q, st := newQueue(t, fr, 1)
	j := addJob(t, st, 0)
	q.Kick()
	<-fr.started
	close(fr.release)
	waitState(t, st, j.ID, job.StateDone)
	got, _ := st.Get(j.ID)
	if got.Progress.Percent != 100 {
		t.Fatalf("fertiger Job muss 100%% haben: %+v", got)
	}
}

func TestQueueRespectsMaxConcurrent(t *testing.T) {
	fr := &fakeRunner{started: make(chan string, 3), release: make(chan struct{})}
	q, st := newQueue(t, fr, 2)
	for i := 0; i < 3; i++ {
		addJob(t, st, i)
	}
	q.Kick()
	for i := 0; i < 2; i++ {
		select {
		case <-fr.started:
		case <-time.After(3 * time.Second):
			t.Fatal("Worker nicht gestartet")
		}
	}
	select {
	case id := <-fr.started:
		t.Fatalf("dritter Job %s lief zu früh", id)
	case <-time.After(200 * time.Millisecond):
	}
	close(fr.release)
	select {
	case <-fr.started:
	case <-time.After(3 * time.Second):
		t.Fatal("dritter Job startete nie")
	}
	time.Sleep(100 * time.Millisecond) // Job-Goroutine cleanup abwarten
}

func TestQueueSetsErrorState(t *testing.T) {
	fr := &fakeRunner{
		started: make(chan string, 1), release: make(chan struct{}),
		fail: errors.New("kaputt"),
	}
	q, st := newQueue(t, fr, 1)
	j := addJob(t, st, 0)
	q.Kick()
	<-fr.started
	close(fr.release)
	waitState(t, st, j.ID, job.StateError)
	got, _ := st.Get(j.ID)
	if got.Error != "kaputt" {
		t.Fatalf("Fehlermeldung fehlt: %+v", got)
	}
}

func TestQueueCancelRunning(t *testing.T) {
	fr := &fakeRunner{started: make(chan string, 1), release: make(chan struct{})}
	q, st := newQueue(t, fr, 1)
	j := addJob(t, st, 0)
	q.Kick()
	<-fr.started
	q.Cancel(j.ID)
	waitState(t, st, j.ID, job.StateCanceled)
}

func TestQueueShutdownKeepsRunningState(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "jobs.json"))
	if err != nil {
		t.Fatal(err)
	}
	fr := &fakeRunner{started: make(chan string, 1), release: make(chan struct{})}
	q := queue.New(st, fr, 1)
	ctx, cancel := context.WithCancel(context.Background())
	q.Start(ctx)
	j := job.New("https://example.com/shutdown", "t", "ba", "l", "")
	if err := st.Add(j); err != nil {
		t.Fatal(err)
	}
	q.Kick()
	<-fr.started
	cancel() // Shutdown (Root-Context), kein Nutzer-Cancel
	time.Sleep(200 * time.Millisecond)
	got, _ := st.Get(j.ID)
	if got.State != job.StateRunning {
		t.Fatalf("Shutdown darf running nicht überschreiben, war %s", got.State)
	}
}

func TestQueueCancelQueued(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "jobs.json"))
	if err != nil {
		t.Fatal(err)
	}
	// Queue absichtlich nicht gestartet — Job bleibt queued
	q := queue.New(st, &fakeRunner{started: make(chan string, 1), release: make(chan struct{})}, 1)
	j := addJob(t, st, 0)
	q.Cancel(j.ID)
	got, _ := st.Get(j.ID)
	if got.State != job.StateCanceled {
		t.Fatalf("wartender Job muss canceled sein, war %s", got.State)
	}
}
