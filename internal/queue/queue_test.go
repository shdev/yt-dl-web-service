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
	"ytdlweb/internal/ytdlp"
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
	if got.FinishedAt == nil {
		t.Fatalf("done-Job muss FinishedAt gesetzt haben: %+v", got)
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
	if got.FinishedAt == nil {
		t.Fatalf("error-Job muss FinishedAt gesetzt haben: %+v", got)
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
	got, _ := st.Get(j.ID)
	if got.FinishedAt == nil {
		t.Fatalf("canceled (running) Job muss FinishedAt gesetzt haben: %+v", got)
	}
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

// TestQueueSetsFilenameViaRunnerCallback verdrahtet den echten ExecRunner
// (Task 5) — sein OnFilename-Callback muss den relativen Pfad nach
// job.Filename schreiben, sobald der Job fertig ist.
func TestQueueSetsFilenameViaRunnerCallback(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "jobs.json"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	absDir, err := filepath.Abs(dir)
	if err != nil {
		t.Fatal(err)
	}
	rel := filepath.Join("youtube", "Kanal", "Titel [id].mkv")
	t.Setenv("PRINT_LINE", filepath.Join(absDir, rel))

	runner := &ytdlp.ExecRunner{
		Bin: "../ytdlp/testdata/filepath.sh", DownloadDir: dir,
		OutputTemplate: "%(title)s.%(ext)s",
	}
	q := queue.New(st, runner, 1)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	q.Start(ctx)

	j := job.New("https://example.com/v", "t", "ba", "l", "")
	if err := st.Add(j); err != nil {
		t.Fatal(err)
	}
	q.Kick()
	waitState(t, st, j.ID, job.StateDone)
	got, _ := st.Get(j.ID)
	if got.Filename != rel {
		t.Fatalf("Filename falsch: got %q want %q", got.Filename, rel)
	}
}

// TestQueueAttributesFilenamesToCorrectConcurrentJob stellt sicher, dass bei
// mehreren gleichzeitig laufenden Jobs (MaxConcurrent > 1) jeder Job seinen
// eigenen Dateipfad bekommt — nicht den eines anderen Jobs. Der geteilte
// *ytdlp.ExecRunner darf dafür nicht als gemeinsam genutztes OnFilename-Feld
// verdrahtet werden, da das bei Überlappung Job-übergreifend überschreibt.
func TestQueueAttributesFilenamesToCorrectConcurrentJob(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "jobs.json"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir() // t.TempDir() liefert einen absoluten Pfad.

	runner := &ytdlp.ExecRunner{
		Bin: "testdata/slow-filepath.sh", DownloadDir: dir,
		OutputTemplate: "%(title)s.%(ext)s",
	}
	q := queue.New(st, runner, 2)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	q.Start(ctx)

	relA := filepath.Join("youtube", "KanalA", "A [a].mkv")
	relB := filepath.Join("youtube", "KanalB", "B [b].mkv")
	jA := job.New("https://example.com/a", "a", "ba", "l", "")
	jB := job.New("https://example.com/b", "b", "ba", "l", "")
	if err := st.Add(jA); err != nil {
		t.Fatal(err)
	}
	if err := st.Add(jB); err != nil {
		t.Fatal(err)
	}
	q.Kick()
	waitState(t, st, jA.ID, job.StateDone)
	waitState(t, st, jB.ID, job.StateDone)
	gotA, _ := st.Get(jA.ID)
	gotB, _ := st.Get(jB.ID)
	if gotA.Filename != relA {
		t.Fatalf("Job A falsch zugeordnet: got %q want %q", gotA.Filename, relA)
	}
	if gotB.Filename != relB {
		t.Fatalf("Job B falsch zugeordnet: got %q want %q", gotB.Filename, relB)
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
	if got.FinishedAt == nil {
		t.Fatalf("canceled (queued) Job muss FinishedAt gesetzt haben: %+v", got)
	}
}
