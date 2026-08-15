package store_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"ytdlweb/internal/job"
	"ytdlweb/internal/store"
)

func openStore(t *testing.T) (*store.Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "jobs.json")
	st, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	return st, path
}

func TestAddPersistsAndReloads(t *testing.T) {
	st, path := openStore(t)
	j := job.New("https://example.com/a", "A", "ba", "l", "")
	if err := st.Add(j); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path + ".tmp"); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("Temp-Datei darf nach persist nicht liegen bleiben")
	}
	st2, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	got := st2.List()
	if len(got) != 1 || got[0].ID != j.ID {
		t.Fatalf("Reload liefert %+v", got)
	}
}

func TestListNewestFirst(t *testing.T) {
	st, _ := openStore(t)
	a := job.New("https://example.com/a", "A", "ba", "l", "")
	b := job.New("https://example.com/b", "B", "ba", "l", "")
	_ = st.Add(a)
	_ = st.Add(b)
	got := st.List()
	if got[0].ID != b.ID || got[1].ID != a.ID {
		t.Fatalf("Reihenfolge falsch: %+v", got)
	}
}

func TestRecoveryRequeuesRunning(t *testing.T) {
	st, path := openStore(t)
	j := job.New("https://example.com/a", "A", "ba", "l", "")
	_ = st.Add(j)
	if err := st.Update(j.ID, func(x *job.Job) { x.State = job.StateRunning }); err != nil {
		t.Fatal(err)
	}
	st2, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := st2.Get(j.ID)
	if got.State != job.StateQueued {
		t.Fatalf("running muss nach Neustart queued sein, war %s", got.State)
	}
}

func TestSetProgressIsInMemoryOnly(t *testing.T) {
	st, path := openStore(t)
	j := job.New("https://example.com/a", "A", "ba", "l", "")
	_ = st.Add(j)
	st.SetProgress(j.ID, job.Progress{Percent: 50})
	got, _ := st.Get(j.ID)
	if got.Progress.Percent != 50 {
		t.Fatalf("Progress in-memory fehlt: %+v", got)
	}
	st2, _ := store.Open(path)
	reloaded, _ := st2.Get(j.ID)
	if reloaded.Progress.Percent != 0 {
		t.Fatal("SetProgress darf nicht auf Disk landen")
	}
	st.SetProgress("unbekannt", job.Progress{Percent: 1}) // no-op, kein Panic
}

func TestClaimNextQueued(t *testing.T) {
	st, _ := openStore(t)
	a := job.New("https://example.com/a", "A", "ba", "l", "")
	b := job.New("https://example.com/b", "B", "ba", "l", "")
	_ = st.Add(a)
	_ = st.Add(b)
	first, ok := st.ClaimNextQueued()
	if !ok || first.ID != a.ID || first.State != job.StateRunning {
		t.Fatalf("Claim muss ältesten Job liefern: %+v", first)
	}
	second, ok := st.ClaimNextQueued()
	if !ok || second.ID != b.ID {
		t.Fatalf("zweiter Claim falsch: %+v", second)
	}
	if _, ok := st.ClaimNextQueued(); ok {
		t.Fatal("dritter Claim muss false liefern")
	}
}

func TestRemove(t *testing.T) {
	st, _ := openStore(t)
	j := job.New("https://example.com/a", "A", "ba", "l", "")
	_ = st.Add(j)
	if err := st.Remove(j.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok := st.Get(j.ID); ok {
		t.Fatal("Job muss entfernt sein")
	}
	if err := st.Remove("unbekannt"); err == nil {
		t.Fatal("Remove unbekannter ID muss Fehler liefern")
	}
}

func TestUpdateUnknownID(t *testing.T) {
	st, _ := openStore(t)
	if err := st.Update("unbekannt", func(*job.Job) {}); err == nil {
		t.Fatal("Update unbekannter ID muss Fehler liefern")
	}
}

func TestOpenCorruptFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jobs.json")
	if err := os.WriteFile(path, []byte("kein json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Open(path); err == nil {
		t.Fatal("korrupte Datei muss Fehler liefern")
	}
}

func TestConcurrentAccess(t *testing.T) {
	st, _ := openStore(t)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			j := job.New(fmt.Sprintf("https://example.com/%d", i), "t", "ba", "l", "")
			if err := st.Add(j); err != nil {
				t.Error(err)
				return
			}
			st.SetProgress(j.ID, job.Progress{Percent: float64(i)})
			_, _ = st.ClaimNextQueued()
			_ = st.List()
			if _, ok := st.Get(j.ID); !ok {
				t.Errorf("job %s fehlt", j.ID)
			}
		}(i)
	}
	wg.Wait()
	if got := len(st.List()); got != 8 {
		t.Fatalf("8 Jobs erwartet, %d vorhanden", got)
	}
}
