package queue_test

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"ytdlweb/internal/intake"
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

// fakeProber zählt Aufrufe und kann blockieren oder scheitern.
type fakeProber struct {
	mu     sync.Mutex
	res    *ytdlp.ProbeResult
	err    error
	calls  int
	block  chan struct{} // gesetzt: Probe wartet auf close oder ctx.Done
	called chan struct{} // gesetzt: jeder Aufruf meldet sich (gepuffert)
	// ignoreCtx: Probe wartet nur auf block und liefert das Ergebnis auch bei
	// abgebrochenem Context (Abbruch trifft genau nach erfolgreicher Analyse).
	ignoreCtx bool
}

func (f *fakeProber) Probe(ctx context.Context, url string) (*ytdlp.ProbeResult, error) {
	f.mu.Lock()
	f.calls++
	res, err := f.res, f.err
	f.mu.Unlock()
	if f.called != nil {
		f.called <- struct{}{}
	}
	if f.block != nil {
		if f.ignoreCtx {
			<-f.block
		} else {
			select {
			case <-f.block:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
	}
	return res, err
}

func (f *fakeProber) set(res *ytdlp.ProbeResult, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.res, f.err = res, err
}

func (f *fakeProber) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// recordingRunner meldet erfolgreich zurück und merkt sich alle gelaufenen Job-IDs.
type recordingRunner struct {
	mu  sync.Mutex
	ids []string
}

func (r *recordingRunner) Run(_ context.Context, j job.Job, _ func(job.Progress)) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ids = append(r.ids, j.ID)
	return nil
}

func (r *recordingRunner) ran(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, x := range r.ids {
		if x == id {
			return true
		}
	}
	return false
}

// newQueueProber wie newQueue, aber mit optionalem Prober (nil: ohne Option).
func newQueueProber(t *testing.T, r queue.Runner, p queue.Prober) (*queue.Queue, *store.Store) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "jobs.json"))
	if err != nil {
		t.Fatal(err)
	}
	var opts []queue.Option
	if p != nil {
		opts = append(opts, queue.WithProber(p))
	}
	q := queue.New(st, r, 1, opts...)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	q.Start(ctx)
	return q, st
}

// addProbeJob legt einen Platzhalter-Job mit NeedsProbe an.
func addProbeJob(t *testing.T, st *store.Store, url, profileKey string) job.Job {
	t.Helper()
	profile, ok := ytdlp.ProfileByKey(profileKey)
	if !ok {
		t.Fatalf("Profil %q unbekannt", profileKey)
	}
	format, multi := intake.PlaylistFormat(profile)
	j := job.New(url, "", format, profile.Label, "")
	j.MultiAudio = multi
	j.Profile = profile.Key
	j.NeedsProbe = true
	if err := st.Add(j); err != nil {
		t.Fatal(err)
	}
	return j
}

func waitGone(t *testing.T, st *store.Store, id string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, ok := st.Get(id); !ok {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("job %s existiert noch, erwartet entfernt", id)
}

func probeVideo() *ytdlp.ProbeResult {
	return &ytdlp.ProbeResult{Type: "video", Video: &ytdlp.Video{ID: "x", Title: "Probe-Titel"}}
}

func probePlaylist() *ytdlp.ProbeResult {
	return &ytdlp.ProbeResult{Type: "playlist", Playlist: &ytdlp.Playlist{Title: "PL", Entries: []ytdlp.PlaylistEntry{
		{URL: "https://example.com/e1", Title: "E1"},
		{URL: "https://example.com/e2", Title: "E2"},
	}}}
}

func TestQueueProbeVideoSetsTitleAndRuns(t *testing.T) {
	fp := &fakeProber{res: probeVideo()}
	rr := &recordingRunner{}
	q, st := newQueueProber(t, rr, fp)
	j := addProbeJob(t, st, "https://example.com/v", "best")
	q.Kick()
	waitState(t, st, j.ID, job.StateDone)
	got, _ := st.Get(j.ID)
	if got.Title != "Probe-Titel" || got.NeedsProbe {
		t.Fatalf("Titel/NeedsProbe falsch: %+v", got)
	}
	if !rr.ran(j.ID) {
		t.Fatal("Runner lief nicht")
	}
	if fp.count() != 1 {
		t.Fatalf("Probe-Aufrufe %d, erwartet 1", fp.count())
	}
}

func TestQueuePlaylistCreatesEntryJobsAndRemovesPlaceholder(t *testing.T) {
	fp := &fakeProber{res: probePlaylist()}
	rr := &recordingRunner{}
	q, st := newQueueProber(t, rr, fp)
	j := addProbeJob(t, st, "https://example.com/pl", "best")
	q.Kick()
	waitGone(t, st, j.ID)
	profile, _ := ytdlp.ProfileByKey("best")
	format, _ := intake.PlaylistFormat(profile)
	want := map[string]string{"https://example.com/e1": "E1", "https://example.com/e2": "E2"}
	jobs := st.List()
	if len(jobs) != 2 {
		t.Fatalf("erwartet 2 Jobs, war %d: %+v", len(jobs), jobs)
	}
	for _, x := range jobs {
		title, ok := want[x.URL]
		if !ok {
			t.Fatalf("unerwartete URL %s", x.URL)
		}
		if x.PlaylistTitle != "PL" || x.Format != format || x.Profile != "best" || x.Title != title {
			t.Fatalf("Job falsch angelegt: %+v", x)
		}
		waitState(t, st, x.ID, job.StateDone)
	}
	if rr.ran(j.ID) {
		t.Fatal("Runner darf für den Platzhalter nicht laufen")
	}
}

func TestQueuePlaylistSkipsDuplicates(t *testing.T) {
	fp := &fakeProber{res: probePlaylist()}
	rr := &recordingRunner{}
	q, st := newQueueProber(t, rr, fp)
	profile, _ := ytdlp.ProfileByKey("best")
	format, _ := intake.PlaylistFormat(profile)
	dup := job.New("https://example.com/e1", "E1", format, profile.Label, "")
	dup.State = job.StateRunning
	if err := st.Add(dup); err != nil {
		t.Fatal(err)
	}
	j := addProbeJob(t, st, "https://example.com/pl", "best")
	q.Kick()
	waitGone(t, st, j.ID)
	jobs := st.List()
	if len(jobs) != 2 {
		t.Fatalf("erwartet Duplikat plus 1 neuer Job, war %d: %+v", len(jobs), jobs)
	}
	n := 0
	for _, x := range jobs {
		if x.URL == "https://example.com/e2" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("Eintrag 2 muss genau einmal existieren, war %d", n)
	}
}

func TestQueueEmptyPlaylistFails(t *testing.T) {
	fp := &fakeProber{res: &ytdlp.ProbeResult{Type: "playlist", Playlist: &ytdlp.Playlist{Title: "PL"}}}
	rr := &recordingRunner{}
	q, st := newQueueProber(t, rr, fp)
	j := addProbeJob(t, st, "https://example.com/pl", "best")
	q.Kick()
	waitState(t, st, j.ID, job.StateError)
	got, _ := st.Get(j.ID)
	if got.Error != "Playlist enthält keine Einträge" || !got.NeedsProbe {
		t.Fatalf("Fehler/NeedsProbe falsch: %+v", got)
	}
	if rr.ran(j.ID) {
		t.Fatal("Runner darf nicht laufen")
	}
}

func TestQueueProbeErrorKeepsNeedsProbeAndRetryProbesAgain(t *testing.T) {
	fp := &fakeProber{err: errors.New("kaputt")}
	rr := &recordingRunner{}
	q, st := newQueueProber(t, rr, fp)
	j := addProbeJob(t, st, "https://example.com/v", "best")
	q.Kick()
	waitState(t, st, j.ID, job.StateError)
	got, _ := st.Get(j.ID)
	if got.Error != "kaputt" || !got.NeedsProbe {
		t.Fatalf("Fehler/NeedsProbe falsch: %+v", got)
	}
	if rr.ran(j.ID) || fp.count() != 1 {
		t.Fatalf("Runner lief oder Probe-Aufrufe %d != 1", fp.count())
	}
	fp.set(probeVideo(), nil)
	if err := st.Update(j.ID, func(x *job.Job) {
		x.State = job.StateQueued
		x.Error = ""
		x.FinishedAt = nil
	}); err != nil {
		t.Fatal(err)
	}
	q.Kick()
	waitState(t, st, j.ID, job.StateDone)
	got, _ = st.Get(j.ID)
	if fp.count() != 2 || got.Title != "Probe-Titel" {
		t.Fatalf("Probe-Aufrufe %d, Titel %q", fp.count(), got.Title)
	}
}

func TestQueueCancelDuringProbe(t *testing.T) {
	fp := &fakeProber{res: probeVideo(), block: make(chan struct{}), called: make(chan struct{}, 4)}
	rr := &recordingRunner{}
	q, st := newQueueProber(t, rr, fp)
	j := addProbeJob(t, st, "https://example.com/v", "best")
	q.Kick()
	select {
	case <-fp.called:
	case <-time.After(3 * time.Second):
		t.Fatal("Probe wurde nicht aufgerufen")
	}
	q.Cancel(j.ID)
	waitState(t, st, j.ID, job.StateCanceled)
	got, _ := st.Get(j.ID)
	if got.FinishedAt == nil || !got.NeedsProbe {
		t.Fatalf("FinishedAt/NeedsProbe falsch: %+v", got)
	}
	if rr.ran(j.ID) {
		t.Fatal("Runner darf nicht laufen")
	}
}

func TestQueueCancelAfterPlaylistProbeCreatesNoEntries(t *testing.T) {
	fp := &fakeProber{res: probePlaylist(), block: make(chan struct{}), called: make(chan struct{}, 4), ignoreCtx: true}
	rr := &recordingRunner{}
	q, st := newQueueProber(t, rr, fp)
	j := addProbeJob(t, st, "https://example.com/pl", "best")
	q.Kick()
	select {
	case <-fp.called:
	case <-time.After(3 * time.Second):
		t.Fatal("Probe wurde nicht aufgerufen")
	}
	q.Cancel(j.ID)
	close(fp.block) // Probe liefert jetzt trotz abgebrochenem Context die Playlist
	waitState(t, st, j.ID, job.StateCanceled)
	got, _ := st.Get(j.ID)
	if !got.NeedsProbe {
		t.Fatalf("NeedsProbe muss true bleiben: %+v", got)
	}
	if n := len(st.List()); n != 1 {
		t.Fatalf("%d Jobs, erwartet nur den Platzhalter", n)
	}
	if rr.ran(j.ID) {
		t.Fatal("Runner darf nicht laufen")
	}
}

func TestQueueSkipsProbeWithoutNeedsProbe(t *testing.T) {
	fp := &fakeProber{res: probeVideo()}
	rr := &recordingRunner{}
	q, st := newQueueProber(t, rr, fp)
	j := addJob(t, st, 0)
	q.Kick()
	waitState(t, st, j.ID, job.StateDone)
	if fp.count() != 0 {
		t.Fatalf("Probe-Aufrufe %d, erwartet 0", fp.count())
	}
}

func TestQueueNeedsProbeWithoutProberFails(t *testing.T) {
	rr := &recordingRunner{}
	q, st := newQueueProber(t, rr, nil)
	j := addProbeJob(t, st, "https://example.com/v", "best")
	q.Kick()
	waitState(t, st, j.ID, job.StateError)
	got, _ := st.Get(j.ID)
	if got.Error != "Analyse nicht verfügbar" {
		t.Fatalf("Fehler %q", got.Error)
	}
	if rr.ran(j.ID) {
		t.Fatal("Runner darf nicht laufen")
	}
}

func TestQueueShutdownDuringProbeKeepsRunning(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "jobs.json"))
	if err != nil {
		t.Fatal(err)
	}
	fp := &fakeProber{res: probeVideo(), block: make(chan struct{}), called: make(chan struct{}, 4)}
	rr := &recordingRunner{}
	q := queue.New(st, rr, 1, queue.WithProber(fp))
	ctx, cancel := context.WithCancel(context.Background())
	q.Start(ctx)
	j := addProbeJob(t, st, "https://example.com/v", "best")
	q.Kick()
	select {
	case <-fp.called:
	case <-time.After(3 * time.Second):
		t.Fatal("Probe wurde nicht aufgerufen")
	}
	cancel() // Shutdown (Root-Context), kein Nutzer-Cancel
	time.Sleep(200 * time.Millisecond)
	got, _ := st.Get(j.ID)
	if got.State != job.StateRunning {
		t.Fatalf("Shutdown darf running nicht überschreiben, war %s", got.State)
	}
}
