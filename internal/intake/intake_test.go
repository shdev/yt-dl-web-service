package intake_test

import (
	"path/filepath"
	"testing"

	"ytdlweb/internal/intake"
	"ytdlweb/internal/job"
	"ytdlweb/internal/store"
	"ytdlweb/internal/ytdlp"
)

func openStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "jobs.json"))
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func mustProfile(t *testing.T, key string) ytdlp.Profile {
	t.Helper()
	p, ok := ytdlp.ProfileByKey(key)
	if !ok {
		t.Fatalf("Profil %q fehlt", key)
	}
	return p
}

func TestPlaylistFormatAudioProfile(t *testing.T) {
	p := mustProfile(t, "audio")
	format, multi := intake.PlaylistFormat(p)
	if format != p.Expr || multi {
		t.Fatalf("format=%q multi=%v", format, multi)
	}
}

func TestPlaylistFormatVideoProfile(t *testing.T) {
	p := mustProfile(t, "best")
	format, multi := intake.PlaylistFormat(p)
	want := p.VideoExpr + "+ba[language^=de]+ba[format_note*=original][language!^=de]/" +
		p.VideoExpr + "+ba[language^=de]/" + p.Expr
	if format != want || !multi {
		t.Fatalf("format=%q multi=%v", format, multi)
	}
}

func TestIsDuplicate(t *testing.T) {
	st := openStore(t)
	const url = "https://example.com/a"
	add := func(state job.State) {
		j := job.New(url, "A", "ba", "l", "")
		j.State = state
		if err := st.Add(j); err != nil {
			t.Fatal(err)
		}
	}
	add(job.StateDone)
	add(job.StateError)
	if intake.IsDuplicate(st, url, "ba") {
		t.Fatal("done/error zählen nicht als Duplikat")
	}
	add(job.StateQueued)
	if !intake.IsDuplicate(st, url, "ba") {
		t.Fatal("queued muss Duplikat sein")
	}
	if intake.IsDuplicate(st, url, "bv") {
		t.Fatal("anderes Format ist kein Duplikat")
	}
	if intake.IsDuplicate(st, "https://example.com/b", "ba") {
		t.Fatal("andere URL ist kein Duplikat")
	}
	st2 := openStore(t)
	j := job.New(url, "A", "ba", "l", "")
	j.State = job.StateRunning
	if err := st2.Add(j); err != nil {
		t.Fatal(err)
	}
	if !intake.IsDuplicate(st2, url, "ba") {
		t.Fatal("running muss Duplikat sein")
	}
}

func TestCreatePlaylistJobs(t *testing.T) {
	st := openStore(t)
	p := mustProfile(t, "best")
	format, multi := intake.PlaylistFormat(p)
	existing := job.New("https://example.com/dup", "D", format, p.Label, "")
	if err := st.Add(existing); err != nil {
		t.Fatal(err)
	}
	entries := []intake.Entry{
		{URL: "https://example.com/1", Title: "Eins"},
		{URL: "  ", Title: "leer"},
		{URL: "https://example.com/dup", Title: "Dup"},
		{URL: " https://example.com/2 ", Title: "Zwei"},
	}
	ids, skipped, err := intake.CreatePlaylistJobs(st, p, "PL", entries)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 || skipped != 2 {
		t.Fatalf("ids=%v skipped=%d", ids, skipped)
	}
	titles := []string{"Eins", "Zwei"}
	for i, id := range ids {
		j, ok := st.Get(id)
		if !ok {
			t.Fatalf("Job %s fehlt", id)
		}
		if j.State != job.StateQueued || j.Title != titles[i] || j.PlaylistTitle != "PL" ||
			j.Format != format || j.FormatLabel != p.Label || j.MultiAudio != multi ||
			j.Profile != p.Key || j.NeedsProbe {
			t.Fatalf("Job falsch: %+v", j)
		}
	}
	empty, _, err := intake.CreatePlaylistJobs(st, p, "PL", nil)
	if err != nil || empty == nil || len(empty) != 0 {
		t.Fatalf("ids muss leer, nicht nil sein: %v %v", empty, err)
	}
}
