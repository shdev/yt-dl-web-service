# yt-dl-web-client Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ein Docker-Image (Base `mikenye/youtube-dl`) mit Go-Webservice: URL eingeben, Format per yt-dlp wählen, parallele Downloads mit persistenter Job-Queue, Playlist-Support, Bootstrap-UI.

**Architecture:** Go-Standardbibliothek ohne Framework. Vier Kernpakete (`ytdlp` = Probe/Runner/Formate, `store` = JSON-Persistenz, `queue` = Worker-Pool, `server` = HTTP-API) plus eingebettete Web-Assets. yt-dlp wird per `os/exec` aufgerufen; `-J` liefert die Formatdaten, ein Progress-Template den Fortschritt.

**Tech Stack:** Go ≥ 1.24 (nur stdlib), Bootstrap 5.3.3 (vendored), Vanilla-JS, Docker Multi-Stage, Make.

**Spec:** `docs/superpowers/specs/2026-08-14-yt-dl-web-client-design.md`

## Global Constraints

- Go-Modulname: `ytdlweb`; Imports z.B. `ytdlweb/internal/job`. Go-Version in `go.mod`: `go 1.24`.
- **Keine Third-Party-Dependencies** — ausschließlich Go-Standardbibliothek.
- `make check` (gofmt-Prüfung + `go vet` + Tests) muss nach jedem Task grün sein.
- UI-Texte auf Deutsch („Analysieren", „Download starten", „Abbrechen" …).
- Docker: Base-Image `mikenye/youtube-dl`, Build nur für amd64, yt-dlp-Systempfad im Container: `/usr/local/bin/yt-dlp`.
- Volumes `/downloads` und `/config`; Umgebungsvariablen und Defaults exakt wie Spec §3: `PORT=8080`, `MAX_CONCURRENT=3`, `OUTPUT_TEMPLATE=%(title)s [%(id)s].%(ext)s`, `YTDLP_UPDATE_ON_START=true`, `DOWNLOAD_DIR=/downloads`, `CONFIG_DIR=/config`.
- Progress-Template exakt: `dl:%(progress._percent_str)s|%(progress._speed_str)s|%(progress._eta_str)s`
- Nach jedem Task committen (Commit-Schritt ist Teil jedes Tasks). Commit-Messages auf Deutsch, Format `feat:`/`test:`/`chore:`.
- Test-Hilfsskripte unter `testdata/` brauchen Execute-Bit (`chmod +x`), git übernimmt das Bit.

## Datei-Struktur

```
go.mod                          Modul ytdlweb
Makefile                        Standardaufgaben (Spec §10)
.gitignore                      bin/, tmp/, data/
Dockerfile                      Multi-Stage, Base mikenye/youtube-dl
docker-compose.yml              NAS-Vorlage
README.md                       Kurzanleitung
cmd/server/main.go              Wiring: Config, Store, Queue, HTTP, -healthcheck
internal/config/config.go       Env-Parsing mit Defaults
internal/job/job.go             Job-Typ, Zustände, ID-Erzeugung
internal/store/store.go         JSON-File-Store, atomar, Crash-Recovery
internal/queue/queue.go         Dispatcher + Worker-Pool, Cancel
internal/ytdlp/format.go        Format-Ausdrücke, Playlist-Profile
internal/ytdlp/progress.go      Progress-Template + Parser
internal/ytdlp/probe.go         -J-Aufruf + JSON-Parsing (Video/Playlist)
internal/ytdlp/runner.go        ExecRunner (Download-Prozess)
internal/ytdlp/update.go        EnsureBinary (Kopie nach /config/bin, -U)
web/embed.go                    go:embed FS
web/templates/index.html        Single-Page-UI
web/static/app.js               UI-Logik (Probe, Auswahl, Polling)
web/static/bootstrap.min.css    vendored 5.3.3
web/static/bootstrap.bundle.min.js  vendored 5.3.3
```

Jedes Paket hat genau eine Verantwortung; `ytdlp` kapselt alles, was das externe Binary betrifft. `server` kennt `store`, `queue`, `ytdlp`-Typen; `queue` kennt `store` und das `Runner`-Interface; `store` kennt nur `job`.

---

### Task 1: Projektgerüst (go.mod, Makefile, .gitignore)

**Files:**
- Create: `go.mod`
- Create: `.gitignore`
- Create: `Makefile`
- Create: `cmd/server/main.go` (Platzhalter, wird in Task 13 ersetzt)

**Interfaces:**
- Consumes: —
- Produces: Modulpfad `ytdlweb`; Make-Targets `build`, `test`, `check`, `run`, `image`, `up`, `down`, `clean`, die alle späteren Tasks verwenden.

- [ ] **Step 1: go.mod anlegen**

```
module ytdlweb

go 1.24
```

- [ ] **Step 2: .gitignore anlegen**

```
bin/
tmp/
data/
```

- [ ] **Step 3: Makefile anlegen** (Einrückung: echte Tabs!)

```make
BINARY := bin/app

.PHONY: build test check fmt-check vet run image up down clean

build:
	CGO_ENABLED=0 go build -o $(BINARY) ./cmd/server

test:
	go test ./...

check: fmt-check vet test

fmt-check:
	test -z "$$(gofmt -l .)"

vet:
	go vet ./...

run: build
	mkdir -p tmp/downloads tmp/config
	PORT=8080 DOWNLOAD_DIR=tmp/downloads CONFIG_DIR=tmp/config \
		YTDLP_UPDATE_ON_START=false $(BINARY)

image:
	docker build --platform linux/amd64 -t yt-dl-web-client .

up:
	docker compose up -d --build

down:
	docker compose down

clean:
	rm -rf bin tmp
```

- [ ] **Step 4: Platzhalter-main anlegen** (`cmd/server/main.go`)

```go
package main

import "fmt"

func main() {
	fmt.Println("yt-dl-web-client: Platzhalter — Server folgt in Task 13")
}
```

- [ ] **Step 5: Verifizieren**

Run: `make build && make check`
Expected: Build ok, gofmt/vet/Tests ohne Fehler („no test files" ist ok).

- [ ] **Step 6: Commit**

```bash
git add go.mod .gitignore Makefile cmd/server/main.go
git commit -m "chore: Projektgerüst mit Makefile und Go-Modul"
```

---

### Task 2: Job-Typ (`internal/job`)

**Files:**
- Create: `internal/job/job.go`
- Test: `internal/job/job_test.go`

**Interfaces:**
- Consumes: —
- Produces (alle späteren Tasks bauen darauf):
  - `type State string` mit Konstanten `StateQueued`, `StateRunning`, `StateDone`, `StateError`, `StateCanceled` (JSON-Werte: `"queued"`, `"running"`, `"done"`, `"error"`, `"canceled"`)
  - `type Progress struct { Percent float64; Speed string; ETA string }` (JSON: `percent`, `speed`, `eta`)
  - `type Job struct { ID, URL, Title, Format, FormatLabel, PlaylistTitle string; State State; Progress Progress; Error string; CreatedAt time.Time }` (JSON: `id`, `url`, `title`, `format`, `format_label`, `playlist_title`, `state`, `progress`, `error`, `created_at`)
  - `func New(url, title, format, formatLabel, playlistTitle string) Job` — Zustand `StateQueued`, eindeutige hex-ID, `CreatedAt` UTC

- [ ] **Step 1: Failing Test schreiben** (`internal/job/job_test.go`)

```go
package job_test

import (
	"encoding/json"
	"testing"

	"ytdlweb/internal/job"
)

func TestNewSetsFields(t *testing.T) {
	j := job.New("https://example.com/v", "Titel", "303+251", "1080p", "Meine Liste")
	if j.URL != "https://example.com/v" || j.Title != "Titel" {
		t.Fatalf("Felder nicht übernommen: %+v", j)
	}
	if j.Format != "303+251" || j.FormatLabel != "1080p" || j.PlaylistTitle != "Meine Liste" {
		t.Fatalf("Format-Felder nicht übernommen: %+v", j)
	}
	if j.State != job.StateQueued {
		t.Fatalf("neuer Job muss queued sein, war %s", j.State)
	}
	if j.ID == "" || j.CreatedAt.IsZero() {
		t.Fatalf("ID/CreatedAt fehlen: %+v", j)
	}
}

func TestNewUniqueIDs(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		id := job.New("u", "t", "f", "l", "").ID
		if seen[id] {
			t.Fatalf("doppelte ID: %s", id)
		}
		seen[id] = true
	}
}

func TestJobJSONRoundTrip(t *testing.T) {
	j := job.New("https://example.com/v", "Titel", "ba", "Nur Audio", "")
	j.Progress = job.Progress{Percent: 42.5, Speed: "1.25MiB/s", ETA: "00:35"}
	data, err := json.Marshal(j)
	if err != nil {
		t.Fatal(err)
	}
	var back job.Job
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}
	if back.ID != j.ID || back.Progress.Percent != 42.5 || back.State != job.StateQueued {
		t.Fatalf("Round-Trip verändert Daten: %+v", back)
	}
}
```

- [ ] **Step 2: Test laufen lassen — muss fehlschlagen**

Run: `go test ./internal/job/`
Expected: FAIL (Paket `job` existiert nicht)

- [ ] **Step 3: Implementierung** (`internal/job/job.go`)

```go
// Package job definiert den Download-Job und seine Zustände.
package job

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

type State string

const (
	StateQueued   State = "queued"
	StateRunning  State = "running"
	StateDone     State = "done"
	StateError    State = "error"
	StateCanceled State = "canceled"
)

type Progress struct {
	Percent float64 `json:"percent"`
	Speed   string  `json:"speed"`
	ETA     string  `json:"eta"`
}

type Job struct {
	ID            string    `json:"id"`
	URL           string    `json:"url"`
	Title         string    `json:"title"`
	Format        string    `json:"format"`
	FormatLabel   string    `json:"format_label"`
	PlaylistTitle string    `json:"playlist_title,omitempty"`
	State         State     `json:"state"`
	Progress      Progress  `json:"progress"`
	Error         string    `json:"error,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
}

func New(url, title, format, formatLabel, playlistTitle string) Job {
	return Job{
		ID:            newID(),
		URL:           url,
		Title:         title,
		Format:        format,
		FormatLabel:   formatLabel,
		PlaylistTitle: playlistTitle,
		State:         StateQueued,
		CreatedAt:     time.Now().UTC(),
	}
}

func newID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}
```

- [ ] **Step 4: Tests laufen lassen — müssen bestehen**

Run: `go test ./internal/job/ -v`
Expected: PASS (3 Tests)

- [ ] **Step 5: Commit**

```bash
git add internal/job/
git commit -m "feat: Job-Typ mit Zuständen und ID-Erzeugung"
```

---

### Task 3: JSON-File-Store (`internal/store`)

**Files:**
- Create: `internal/store/store.go`
- Test: `internal/store/store_test.go`

**Interfaces:**
- Consumes: `ytdlweb/internal/job` (Task 2)
- Produces (verwendet von `queue` Task 10 und `server` Task 12):
  - `func Open(path string) (*Store, error)` — lädt Datei falls vorhanden; **Recovery:** Jobs im Zustand `running` werden zu `queued` (Spec §4.2); korrupte Datei → Fehler
  - `func (s *Store) Add(j job.Job) error` — anhängen + persistieren
  - `func (s *Store) Get(id string) (job.Job, bool)` — Kopie
  - `func (s *Store) List() []job.Job` — Kopien, **neueste zuerst**
  - `func (s *Store) Update(id string, fn func(*job.Job)) error` — Mutation unter Lock + persistieren; unbekannte ID → Fehler
  - `func (s *Store) SetProgress(id string, p job.Progress)` — **nur in-memory** (kein Disk-Write pro Progress-Tick; Zustand wird beim nächsten `Update` mitgeschrieben; unbekannte ID → no-op)
  - `func (s *Store) Remove(id string) error` — Eintrag entfernen + persistieren
  - `func (s *Store) ClaimNextQueued() (job.Job, bool)` — ältesten `queued`-Job atomar auf `running` setzen + persistieren
- Persistenz: `json.MarshalIndent` → `<path>.tmp` → `os.Rename` (atomar, Spec §4.2)

- [ ] **Step 1: Failing Tests schreiben** (`internal/store/store_test.go`)

```go
package store_test

import (
	"errors"
	"os"
	"path/filepath"
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
```

- [ ] **Step 2: Tests laufen lassen — müssen fehlschlagen**

Run: `go test ./internal/store/`
Expected: FAIL (Paket existiert nicht)

- [ ] **Step 3: Implementierung** (`internal/store/store.go`)

```go
// Package store persistiert Jobs als JSON-Datei (atomar via Temp-Datei + Rename).
package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"

	"ytdlweb/internal/job"
)

type Store struct {
	mu   sync.Mutex
	path string
	jobs []job.Job // Einfüge-Reihenfolge
}

func Open(path string) (*Store, error) {
	s := &Store{path: path}
	data, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return s, nil
	case err != nil:
		return nil, err
	}
	if err := json.Unmarshal(data, &s.jobs); err != nil {
		return nil, fmt.Errorf("jobs.json unlesbar: %w", err)
	}
	changed := false
	for i := range s.jobs {
		if s.jobs[i].State == job.StateRunning {
			s.jobs[i].State = job.StateQueued
			changed = true
		}
	}
	if changed {
		if err := s.persistLocked(); err != nil {
			return nil, err
		}
	}
	return s, nil
}

func (s *Store) Add(j job.Job) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.jobs = append(s.jobs, j)
	return s.persistLocked()
}

func (s *Store) Get(id string) (job.Job, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, j := range s.jobs {
		if j.ID == id {
			return j, true
		}
	}
	return job.Job{}, false
}

func (s *Store) List() []job.Job {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]job.Job, len(s.jobs))
	for i, j := range s.jobs {
		out[len(s.jobs)-1-i] = j
	}
	return out
}

func (s *Store) Update(id string, fn func(*job.Job)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.jobs {
		if s.jobs[i].ID == id {
			fn(&s.jobs[i])
			return s.persistLocked()
		}
	}
	return fmt.Errorf("job %s nicht gefunden", id)
}

// SetProgress aktualisiert nur den In-Memory-Zustand — bewusst kein
// Disk-Write pro Progress-Tick; persistiert wird beim nächsten Update.
func (s *Store) SetProgress(id string, p job.Progress) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.jobs {
		if s.jobs[i].ID == id {
			s.jobs[i].Progress = p
			return
		}
	}
}

func (s *Store) Remove(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.jobs {
		if s.jobs[i].ID == id {
			s.jobs = append(s.jobs[:i], s.jobs[i+1:]...)
			return s.persistLocked()
		}
	}
	return fmt.Errorf("job %s nicht gefunden", id)
}

func (s *Store) ClaimNextQueued() (job.Job, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.jobs {
		if s.jobs[i].State == job.StateQueued {
			s.jobs[i].State = job.StateRunning
			if err := s.persistLocked(); err != nil {
				s.jobs[i].State = job.StateQueued
				return job.Job{}, false
			}
			return s.jobs[i], true
		}
	}
	return job.Job{}, false
}

func (s *Store) persistLocked() error {
	data, err := json.MarshalIndent(s.jobs, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
```

- [ ] **Step 4: Tests laufen lassen — müssen bestehen**

Run: `go test ./internal/store/ -v`
Expected: PASS (8 Tests)

- [ ] **Step 5: Race-Detector**

Run: `go test ./internal/store/ -race`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/store/
git commit -m "feat: atomarer JSON-Store mit Crash-Recovery"
```

---

### Task 4: Format-Ausdrücke & Playlist-Profile (`internal/ytdlp/format.go`)

**Files:**
- Create: `internal/ytdlp/format.go`
- Test: `internal/ytdlp/format_test.go`

**Interfaces:**
- Consumes: —
- Produces (verwendet von `server` Task 12):
  - `func BuildFormat(videoID, audioID string, audioOnly bool) string` — yt-dlp `-f`-Ausdruck (Spec §5)
  - `type Profile struct { Key, Label, Expr string }`
  - `var Profiles []Profile` — Keys `best`, `1080p`, `720p`, `audio`
  - `func ProfileByKey(key string) (Profile, bool)`

- [ ] **Step 1: Failing Tests schreiben** (`internal/ytdlp/format_test.go`)

```go
package ytdlp_test

import (
	"testing"

	"ytdlweb/internal/ytdlp"
)

func TestBuildFormat(t *testing.T) {
	cases := []struct {
		name, videoID, audioID string
		audioOnly              bool
		want                   string
	}{
		{"Video+Audio", "303", "251", false, "303+251"},
		{"nur Video-ID", "303", "", false, "303"},
		{"nur Audio-ID ohne audioOnly", "", "251", false, "bv*+251"},
		{"audioOnly mit ID", "303", "251", true, "251"},
		{"audioOnly ohne ID", "", "", true, "ba"},
		{"nichts gewählt = beste Qualität", "", "", false, "bv*+ba/b"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ytdlp.BuildFormat(c.videoID, c.audioID, c.audioOnly); got != c.want {
				t.Fatalf("BuildFormat(%q,%q,%v) = %q, erwartet %q",
					c.videoID, c.audioID, c.audioOnly, got, c.want)
			}
		})
	}
}

func TestProfileByKey(t *testing.T) {
	p, ok := ytdlp.ProfileByKey("1080p")
	if !ok || p.Expr != "bv*[height<=1080]+ba/b[height<=1080]" {
		t.Fatalf("1080p-Profil falsch: %+v (ok=%v)", p, ok)
	}
	if _, ok := ytdlp.ProfileByKey("gibtsnicht"); ok {
		t.Fatal("unbekannter Key muss ok=false liefern")
	}
	for _, key := range []string{"best", "1080p", "720p", "audio"} {
		if _, ok := ytdlp.ProfileByKey(key); !ok {
			t.Fatalf("Profil %s fehlt", key)
		}
	}
}
```

- [ ] **Step 2: Tests laufen lassen — müssen fehlschlagen**

Run: `go test ./internal/ytdlp/`
Expected: FAIL (Paket existiert nicht)

- [ ] **Step 3: Implementierung** (`internal/ytdlp/format.go`)

```go
// Package ytdlp kapselt alle Aufrufe des externen yt-dlp-Binaries
// sowie das Parsen seiner Ausgaben.
package ytdlp

// BuildFormat baut den yt-dlp -f-Ausdruck aus der UI-Auswahl.
// Leere IDs fallen auf "beste Qualität" zurück.
func BuildFormat(videoID, audioID string, audioOnly bool) string {
	switch {
	case audioOnly && audioID != "":
		return audioID
	case audioOnly:
		return "ba"
	case videoID != "" && audioID != "":
		return videoID + "+" + audioID
	case videoID != "":
		return videoID
	case audioID != "":
		return "bv*+" + audioID
	default:
		return "bv*+ba/b"
	}
}

// Profile sind die pauschalen Qualitätsstufen für Playlist-Downloads (Spec §5).
type Profile struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Expr  string `json:"-"`
}

var Profiles = []Profile{
	{Key: "best", Label: "Beste Qualität", Expr: "bv*+ba/b"},
	{Key: "1080p", Label: "Beste ≤1080p", Expr: "bv*[height<=1080]+ba/b[height<=1080]"},
	{Key: "720p", Label: "Beste ≤720p", Expr: "bv*[height<=720]+ba/b[height<=720]"},
	{Key: "audio", Label: "Nur Audio", Expr: "ba"},
}

func ProfileByKey(key string) (Profile, bool) {
	for _, p := range Profiles {
		if p.Key == key {
			return p, true
		}
	}
	return Profile{}, false
}
```

- [ ] **Step 4: Tests laufen lassen — müssen bestehen**

Run: `go test ./internal/ytdlp/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/ytdlp/
git commit -m "feat: Format-Ausdrücke und Playlist-Profile"
```

---

### Task 5: Progress-Parser (`internal/ytdlp/progress.go`)

**Files:**
- Create: `internal/ytdlp/progress.go`
- Test: `internal/ytdlp/progress_test.go`

**Interfaces:**
- Consumes: `ytdlweb/internal/job` (Task 2)
- Produces (verwendet vom Runner Task 7):
  - `const ProgressTemplate = "dl:%(progress._percent_str)s|%(progress._speed_str)s|%(progress._eta_str)s"`
  - `func ParseProgress(line string) (job.Progress, bool)` — `false` für alle Nicht-Progress-Zeilen; `"NA"` in Speed/ETA wird zu `""`

- [ ] **Step 1: Failing Tests schreiben** (`internal/ytdlp/progress_test.go`)

```go
package ytdlp_test

import (
	"testing"

	"ytdlweb/internal/ytdlp"
)

func TestParseProgress(t *testing.T) {
	cases := []struct {
		name, line string
		wantOK     bool
		percent    float64
		speed, eta string
	}{
		{"normale Zeile", "dl:  45.3%|1.25MiB/s|00:35", true, 45.3, "1.25MiB/s", "00:35"},
		{"100 Prozent", "dl: 100.0%|1.10MiB/s|00:00", true, 100, "1.10MiB/s", "00:00"},
		{"NA-Werte", "dl:  10.0%|NA|NA", true, 10, "", ""},
		{"fremde Zeile", "[download] Destination: video.mp4", false, 0, "", ""},
		{"kaputte Zeile", "dl:abc|def", false, 0, "", ""},
		{"leere Zeile", "", false, 0, "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p, ok := ytdlp.ParseProgress(c.line)
			if ok != c.wantOK {
				t.Fatalf("ok = %v, erwartet %v", ok, c.wantOK)
			}
			if !ok {
				return
			}
			if p.Percent != c.percent || p.Speed != c.speed || p.ETA != c.eta {
				t.Fatalf("Parse(%q) = %+v", c.line, p)
			}
		})
	}
}
```

- [ ] **Step 2: Tests laufen lassen — müssen fehlschlagen**

Run: `go test ./internal/ytdlp/`
Expected: FAIL (`ParseProgress` undefiniert)

- [ ] **Step 3: Implementierung** (`internal/ytdlp/progress.go`)

```go
package ytdlp

import (
	"strconv"
	"strings"

	"ytdlweb/internal/job"
)

// ProgressTemplate erzeugt maschinenlesbare Fortschrittszeilen (Spec §4.4).
const ProgressTemplate = "dl:%(progress._percent_str)s|%(progress._speed_str)s|%(progress._eta_str)s"

func ParseProgress(line string) (job.Progress, bool) {
	rest, ok := strings.CutPrefix(strings.TrimSpace(line), "dl:")
	if !ok {
		return job.Progress{}, false
	}
	parts := strings.Split(rest, "|")
	if len(parts) != 3 {
		return job.Progress{}, false
	}
	pctStr := strings.TrimSuffix(strings.TrimSpace(parts[0]), "%")
	pct, err := strconv.ParseFloat(pctStr, 64)
	if err != nil {
		return job.Progress{}, false
	}
	speed := strings.TrimSpace(parts[1])
	eta := strings.TrimSpace(parts[2])
	if speed == "NA" {
		speed = ""
	}
	if eta == "NA" {
		eta = ""
	}
	return job.Progress{Percent: pct, Speed: speed, ETA: eta}, true
}
```

- [ ] **Step 4: Tests laufen lassen — müssen bestehen**

Run: `go test ./internal/ytdlp/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/ytdlp/
git commit -m "feat: Progress-Template und Parser"
```

---

### Task 6: Probe — URL analysieren (`internal/ytdlp/probe.go`)

**Files:**
- Create: `internal/ytdlp/probe.go`
- Create: `internal/ytdlp/testdata/video.json`
- Create: `internal/ytdlp/testdata/playlist.json`
- Create: `internal/ytdlp/testdata/fake-yt-dlp.sh`
- Create: `internal/ytdlp/testdata/probe-fail.sh`
- Test: `internal/ytdlp/probe_test.go`

**Interfaces:**
- Consumes: —
- Produces (verwendet von `server` Task 12; JSON-Tags gehen 1:1 an die UI):
  - `type Format struct { ID, Ext, Resolution string; FPS, TBR, ABR float64; VCodec, ACodec, Note string; Filesize int64 }` (JSON: `format_id`, `ext`, `resolution`, `fps`, `tbr`, `abr`, `vcodec`, `acodec`, `format_note`, `filesize`)
  - `type Video struct { ID, Title, Thumbnail string; Duration float64; Formats []Format }` (JSON: `id`, `title`, `thumbnail`, `duration`, `formats`)
  - `type PlaylistEntry struct { URL, Title string }` (JSON: `url`, `title`)
  - `type Playlist struct { Title string; Entries []PlaylistEntry }` (JSON: `title`, `entries`)
  - `type ProbeResult struct { Type string; Video *Video; Playlist *Playlist }` (JSON: `type` = `"video"`/`"playlist"`, `video`, `playlist`)
  - `func ParseProbeJSON(data []byte) (*ProbeResult, error)`
  - `type Prober struct { Bin string }` mit `func (p *Prober) Probe(ctx context.Context, rawURL string) (*ProbeResult, error)` — ruft `<Bin> -J --flat-playlist --no-warnings <url>`; bei Exit ≠ 0 Fehler mit stderr-Auszug (Spec §8)

- [ ] **Step 1: Fixtures anlegen**

`internal/ytdlp/testdata/video.json` (gekürzter realer `yt-dlp -J`-Dump):

```json
{
  "id": "dQw4w9WgXcQ",
  "title": "Test Video",
  "thumbnail": "https://i.ytimg.com/vi/dQw4w9WgXcQ/maxresdefault.jpg",
  "duration": 212.0,
  "formats": [
    {"format_id": "251", "ext": "webm", "resolution": "audio only", "fps": null, "vcodec": "none", "acodec": "opus", "abr": 132.4, "filesize": 3405231, "format_note": "medium"},
    {"format_id": "136", "ext": "mp4", "resolution": "1280x720", "fps": 25, "vcodec": "avc1.4d401f", "acodec": "none", "tbr": 1153.1, "filesize": 30528975, "format_note": "720p"},
    {"format_id": "303", "ext": "webm", "resolution": "1920x1080", "fps": 50, "vcodec": "vp9", "acodec": "none", "tbr": 2205.4, "filesize_approx": 58342123, "format_note": "1080p50"}
  ]
}
```

`internal/ytdlp/testdata/playlist.json`:

```json
{
  "_type": "playlist",
  "id": "PLtest",
  "title": "Test Playlist",
  "entries": [
    {"_type": "url", "url": "https://www.youtube.com/watch?v=aaaaaaaaaaa", "title": "Erstes Video"},
    {"_type": "url", "url": "https://www.youtube.com/watch?v=bbbbbbbbbbb", "title": "Zweites Video"},
    {"_type": "url", "url": "", "title": "Kaputter Eintrag ohne URL"}
  ]
}
```

`internal/ytdlp/testdata/fake-yt-dlp.sh` (gibt die per `FIXTURE` benannte Datei aus):

```sh
#!/bin/sh
cat "$FIXTURE"
```

`internal/ytdlp/testdata/probe-fail.sh`:

```sh
#!/bin/sh
echo "ERROR: Unsupported URL: https://example.invalid" >&2
exit 1
```

Danach: `chmod +x internal/ytdlp/testdata/*.sh`

- [ ] **Step 2: Failing Tests schreiben** (`internal/ytdlp/probe_test.go`)

```go
package ytdlp_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"ytdlweb/internal/ytdlp"
)

func TestParseProbeJSONVideo(t *testing.T) {
	data, err := os.ReadFile("testdata/video.json")
	if err != nil {
		t.Fatal(err)
	}
	res, err := ytdlp.ParseProbeJSON(data)
	if err != nil {
		t.Fatal(err)
	}
	if res.Type != "video" || res.Video == nil || res.Playlist != nil {
		t.Fatalf("kein Video-Resultat: %+v", res)
	}
	v := res.Video
	if v.Title != "Test Video" || v.ID != "dQw4w9WgXcQ" || len(v.Formats) != 3 {
		t.Fatalf("Video-Metadaten falsch: %+v", v)
	}
	// Format 303 hat nur filesize_approx — muss in Filesize landen
	var f303 ytdlp.Format
	for _, f := range v.Formats {
		if f.ID == "303" {
			f303 = f
		}
	}
	if f303.Filesize != 58342123 || f303.VCodec != "vp9" || f303.FPS != 50 {
		t.Fatalf("Format 303 falsch geparst: %+v", f303)
	}
}

func TestParseProbeJSONPlaylist(t *testing.T) {
	data, err := os.ReadFile("testdata/playlist.json")
	if err != nil {
		t.Fatal(err)
	}
	res, err := ytdlp.ParseProbeJSON(data)
	if err != nil {
		t.Fatal(err)
	}
	if res.Type != "playlist" || res.Playlist == nil || res.Video != nil {
		t.Fatalf("kein Playlist-Resultat: %+v", res)
	}
	pl := res.Playlist
	if pl.Title != "Test Playlist" || len(pl.Entries) != 2 {
		t.Fatalf("Einträge ohne URL müssen übersprungen werden: %+v", pl)
	}
	if pl.Entries[0].Title != "Erstes Video" || !strings.Contains(pl.Entries[0].URL, "watch?v=a") {
		t.Fatalf("erster Eintrag falsch: %+v", pl.Entries[0])
	}
}

func TestParseProbeJSONInvalid(t *testing.T) {
	if _, err := ytdlp.ParseProbeJSON([]byte("kein json")); err == nil {
		t.Fatal("ungültiges JSON muss Fehler liefern")
	}
}

func TestProberRunsBinary(t *testing.T) {
	t.Setenv("FIXTURE", "testdata/video.json")
	p := &ytdlp.Prober{Bin: "testdata/fake-yt-dlp.sh"}
	res, err := p.Probe(context.Background(), "https://example.com/v")
	if err != nil {
		t.Fatal(err)
	}
	if res.Type != "video" || res.Video.Title != "Test Video" {
		t.Fatalf("Probe-Resultat falsch: %+v", res)
	}
}

func TestProberSurfacesStderr(t *testing.T) {
	p := &ytdlp.Prober{Bin: "testdata/probe-fail.sh"}
	_, err := p.Probe(context.Background(), "https://example.invalid")
	if err == nil || !strings.Contains(err.Error(), "Unsupported URL") {
		t.Fatalf("stderr muss im Fehler auftauchen, war: %v", err)
	}
}
```

- [ ] **Step 3: Tests laufen lassen — müssen fehlschlagen**

Run: `go test ./internal/ytdlp/`
Expected: FAIL (`ParseProbeJSON`, `Prober` undefiniert)

- [ ] **Step 4: Implementierung** (`internal/ytdlp/probe.go`)

```go
package ytdlp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

type Format struct {
	ID         string  `json:"format_id"`
	Ext        string  `json:"ext"`
	Resolution string  `json:"resolution"`
	FPS        float64 `json:"fps,omitempty"`
	TBR        float64 `json:"tbr,omitempty"`
	ABR        float64 `json:"abr,omitempty"`
	VCodec     string  `json:"vcodec"`
	ACodec     string  `json:"acodec"`
	Note       string  `json:"format_note,omitempty"`
	Filesize   int64   `json:"filesize,omitempty"`
}

type Video struct {
	ID        string   `json:"id"`
	Title     string   `json:"title"`
	Thumbnail string   `json:"thumbnail,omitempty"`
	Duration  float64  `json:"duration,omitempty"`
	Formats   []Format `json:"formats"`
}

type PlaylistEntry struct {
	URL   string `json:"url"`
	Title string `json:"title"`
}

type Playlist struct {
	Title   string          `json:"title"`
	Entries []PlaylistEntry `json:"entries"`
}

type ProbeResult struct {
	Type     string    `json:"type"`
	Video    *Video    `json:"video,omitempty"`
	Playlist *Playlist `json:"playlist,omitempty"`
}

// rawInfo bildet die yt-dlp -J-Ausgabe ab (nur benötigte Felder).
type rawInfo struct {
	Type      string  `json:"_type"`
	ID        string  `json:"id"`
	Title     string  `json:"title"`
	Thumbnail string  `json:"thumbnail"`
	Duration  float64 `json:"duration"`
	Formats   []struct {
		FormatID       string  `json:"format_id"`
		Ext            string  `json:"ext"`
		Resolution     string  `json:"resolution"`
		FPS            float64 `json:"fps"`
		TBR            float64 `json:"tbr"`
		ABR            float64 `json:"abr"`
		VCodec         string  `json:"vcodec"`
		ACodec         string  `json:"acodec"`
		FormatNote     string  `json:"format_note"`
		Filesize       int64   `json:"filesize"`
		FilesizeApprox int64   `json:"filesize_approx"`
	} `json:"formats"`
	Entries []struct {
		URL   string `json:"url"`
		Title string `json:"title"`
	} `json:"entries"`
}

func ParseProbeJSON(data []byte) (*ProbeResult, error) {
	var raw rawInfo
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("yt-dlp-JSON unlesbar: %w", err)
	}
	if raw.Type == "playlist" {
		pl := &Playlist{Title: raw.Title}
		for _, e := range raw.Entries {
			if strings.TrimSpace(e.URL) == "" {
				continue
			}
			pl.Entries = append(pl.Entries, PlaylistEntry{URL: e.URL, Title: e.Title})
		}
		return &ProbeResult{Type: "playlist", Playlist: pl}, nil
	}
	v := &Video{ID: raw.ID, Title: raw.Title, Thumbnail: raw.Thumbnail, Duration: raw.Duration}
	for _, f := range raw.Formats {
		size := f.Filesize
		if size == 0 {
			size = f.FilesizeApprox
		}
		v.Formats = append(v.Formats, Format{
			ID: f.FormatID, Ext: f.Ext, Resolution: f.Resolution,
			FPS: f.FPS, TBR: f.TBR, ABR: f.ABR,
			VCodec: f.VCodec, ACodec: f.ACodec, Note: f.FormatNote,
			Filesize: size,
		})
	}
	return &ProbeResult{Type: "video", Video: v}, nil
}

type Prober struct {
	Bin string
}

func (p *Prober) Probe(ctx context.Context, rawURL string) (*ProbeResult, error) {
	if strings.HasPrefix(rawURL, "-") {
		return nil, fmt.Errorf("ungültige URL")
	}
	cmd := exec.CommandContext(ctx, p.Bin, "-J", "--flat-playlist", "--no-warnings", "--", rawURL)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("yt-dlp: %s", tailString(stderr.String(), 500))
	}
	return ParseProbeJSON(stdout.Bytes())
}

// tailString liefert höchstens die letzten n Zeichen (für stderr-Auszüge).
func tailString(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}
```

- [ ] **Step 5: Tests laufen lassen — müssen bestehen**

Run: `go test ./internal/ytdlp/ -v`
Expected: PASS (alle Probe-, Format- und Progress-Tests)

- [ ] **Step 6: Commit**

```bash
git add internal/ytdlp/
git commit -m "feat: Probe mit -J-Parsing für Video und Playlist"
```

---

### Task 7: ExecRunner — Download-Prozess (`internal/ytdlp/runner.go`)

**Files:**
- Create: `internal/ytdlp/runner.go`
- Create: `internal/ytdlp/testdata/progress.sh`
- Create: `internal/ytdlp/testdata/dl-fail.sh`
- Create: `internal/ytdlp/testdata/dl-sleep.sh`
- Test: `internal/ytdlp/runner_test.go`

**Interfaces:**
- Consumes: `ParseProgress`, `ProgressTemplate` (Task 5), `ytdlweb/internal/job` (Task 2)
- Produces (erfüllt das `queue.Runner`-Interface aus Task 10):
  - `type ExecRunner struct { Bin, DownloadDir, OutputTemplate string }`
  - `func (r *ExecRunner) Run(ctx context.Context, j job.Job, onProgress func(job.Progress)) error` — Aufruf: `<Bin> -f <j.Format> -o <DownloadDir>/<OutputTemplate> --newline --progress-template <ProgressTemplate> --continue --no-playlist --no-warnings -- <j.URL>` (das `--`-Sentinel verhindert Argument-Injection über die URL); eigene Prozessgruppe, Cancel = SIGTERM an die Gruppe (Spec §4.3); bei ctx-Abbruch → `ctx.Err()`; bei Exit ≠ 0 → Fehler mit stderr-Auszug

- [ ] **Step 1: Test-Skripte anlegen**

`internal/ytdlp/testdata/progress.sh` (simuliert yt-dlp-Ausgabe; `%%` ist printf-Escaping):

```sh
#!/bin/sh
printf 'dl:   1.2%%|500.00KiB/s|01:40\n'
printf 'dl:  50.0%%|1.25MiB/s|00:35\n'
printf '[download] irrelevante Zeile\n'
printf 'dl: 100.0%%|1.10MiB/s|00:00\n'
exit 0
```

`internal/ytdlp/testdata/dl-fail.sh`:

```sh
#!/bin/sh
echo "ERROR: HTTP Error 403: Forbidden" >&2
exit 1
```

`internal/ytdlp/testdata/dl-sleep.sh`:

```sh
#!/bin/sh
sleep 10
```

Danach: `chmod +x internal/ytdlp/testdata/*.sh`

- [ ] **Step 2: Failing Tests schreiben** (`internal/ytdlp/runner_test.go`)

```go
package ytdlp_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"ytdlweb/internal/job"
	"ytdlweb/internal/ytdlp"
)

func testJob() job.Job {
	return job.New("https://example.com/v", "Titel", "ba", "Nur Audio", "")
}

func TestExecRunnerReportsProgress(t *testing.T) {
	r := &ytdlp.ExecRunner{
		Bin: "testdata/progress.sh", DownloadDir: t.TempDir(),
		OutputTemplate: "%(title)s.%(ext)s",
	}
	var mu sync.Mutex
	var got []job.Progress
	err := r.Run(context.Background(), testJob(), func(p job.Progress) {
		mu.Lock()
		got = append(got, p)
		mu.Unlock()
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("3 Progress-Updates erwartet, %d bekommen: %+v", len(got), got)
	}
	if got[2].Percent != 100 || got[1].Speed != "1.25MiB/s" {
		t.Fatalf("Progress falsch geparst: %+v", got)
	}
}

func TestExecRunnerSurfacesStderr(t *testing.T) {
	r := &ytdlp.ExecRunner{
		Bin: "testdata/dl-fail.sh", DownloadDir: t.TempDir(),
		OutputTemplate: "x.%(ext)s",
	}
	err := r.Run(context.Background(), testJob(), func(job.Progress) {})
	if err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("stderr muss im Fehler auftauchen, war: %v", err)
	}
}

func TestExecRunnerCancel(t *testing.T) {
	r := &ytdlp.ExecRunner{
		Bin: "testdata/dl-sleep.sh", DownloadDir: t.TempDir(),
		OutputTemplate: "x.%(ext)s",
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- r.Run(ctx, testJob(), func(job.Progress) {}) }()
	time.Sleep(200 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("context.Canceled erwartet, war: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Runner hat auf Cancel nicht reagiert")
	}
}
```

- [ ] **Step 3: Tests laufen lassen — müssen fehlschlagen**

Run: `go test ./internal/ytdlp/`
Expected: FAIL (`ExecRunner` undefiniert)

- [ ] **Step 4: Implementierung** (`internal/ytdlp/runner.go`)

```go
package ytdlp

import (
	"bufio"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"ytdlweb/internal/job"
)

// ExecRunner führt yt-dlp als Kindprozess in eigener Prozessgruppe aus,
// damit Cancel auch von yt-dlp gestartete ffmpeg-Prozesse beendet.
type ExecRunner struct {
	Bin            string
	DownloadDir    string
	OutputTemplate string
}

func (r *ExecRunner) Run(ctx context.Context, j job.Job, onProgress func(job.Progress)) error {
	cmd := exec.CommandContext(ctx, r.Bin,
		"-f", j.Format,
		"-o", filepath.Join(r.DownloadDir, r.OutputTemplate),
		"--newline",
		"--progress-template", ProgressTemplate,
		"--continue",
		"--no-playlist",
		"--no-warnings",
		"--", j.URL,
	)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
	}
	cmd.WaitDelay = 5 * time.Second // SIGKILL-Fallback, falls SIGTERM ignoriert wird

	stderr := &tailBuffer{max: 4096}
	cmd.Stderr = stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	sc := bufio.NewScanner(stdout)
	for sc.Scan() {
		if p, ok := ParseProgress(sc.Text()); ok {
			onProgress(p)
		}
	}
	err = cmd.Wait()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		return fmt.Errorf("yt-dlp: %w: %s", err, stderr.String())
	}
	return nil
}

// tailBuffer behält die letzten max Bytes — genug für Fehlermeldungen,
// ohne bei langen Downloads unbegrenzt zu wachsen.
type tailBuffer struct {
	max int
	buf []byte
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.buf = append(t.buf, p...)
	if len(t.buf) > t.max {
		t.buf = t.buf[len(t.buf)-t.max:]
	}
	return len(p), nil
}

func (t *tailBuffer) String() string {
	return strings.TrimSpace(string(t.buf))
}
```

- [ ] **Step 5: Tests laufen lassen — müssen bestehen**

Run: `go test ./internal/ytdlp/ -v -race`
Expected: PASS (inkl. Cancel-Test deutlich unter 10 s)

- [ ] **Step 6: Commit**

```bash
git add internal/ytdlp/
git commit -m "feat: ExecRunner mit Prozessgruppen-Cancel und Progress-Streaming"
```

---

### Task 8: yt-dlp-Update-Mechanismus (`internal/ytdlp/update.go`)

**Files:**
- Create: `internal/ytdlp/update.go`
- Test: `internal/ytdlp/update_test.go`

**Interfaces:**
- Consumes: `tailString` (Task 6)
- Produces (verwendet von `main` Task 13):
  - `func EnsureBinary(systemBin, configDir string, update bool, logf func(format string, args ...any)) string` — kopiert `systemBin` einmalig nach `<configDir>/bin/yt-dlp` (0755), führt bei `update` dort `-U` aus; **niemals fatal**: bei jedem Fehler wird geloggt und der beste verfügbare Pfad zurückgegeben (Spec §3, Update-Mechanismus)

- [ ] **Step 1: Failing Tests schreiben** (`internal/ytdlp/update_test.go`)

```go
package ytdlp_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ytdlweb/internal/ytdlp"
)

func TestEnsureBinaryCopies(t *testing.T) {
	dir := t.TempDir()
	system := filepath.Join(dir, "yt-dlp-system")
	if err := os.WriteFile(system, []byte("#!/bin/sh\necho system\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfgDir := filepath.Join(dir, "config")
	got := ytdlp.EnsureBinary(system, cfgDir, false, t.Logf)
	want := filepath.Join(cfgDir, "bin", "yt-dlp")
	if got != want {
		t.Fatalf("Pfad %q, erwartet %q", got, want)
	}
	data, err := os.ReadFile(want)
	if err != nil || !strings.Contains(string(data), "system") {
		t.Fatalf("Kopie fehlt/falsch: %v %q", err, data)
	}
	info, _ := os.Stat(want)
	if info.Mode()&0o111 == 0 {
		t.Fatal("Kopie muss ausführbar sein")
	}
}

func TestEnsureBinaryKeepsExisting(t *testing.T) {
	dir := t.TempDir()
	system := filepath.Join(dir, "yt-dlp-system")
	_ = os.WriteFile(system, []byte("neu"), 0o755)
	cfgDir := filepath.Join(dir, "config")
	dst := filepath.Join(cfgDir, "bin", "yt-dlp")
	_ = os.MkdirAll(filepath.Dir(dst), 0o755)
	_ = os.WriteFile(dst, []byte("vorhanden"), 0o755)
	got := ytdlp.EnsureBinary(system, cfgDir, false, t.Logf)
	data, _ := os.ReadFile(got)
	if string(data) != "vorhanden" {
		t.Fatal("vorhandene Kopie darf nicht überschrieben werden")
	}
}

func TestEnsureBinaryFallsBackToSystem(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "gibtsnicht")
	got := ytdlp.EnsureBinary(missing, filepath.Join(dir, "config"), false, t.Logf)
	if got != missing {
		t.Fatalf("bei Kopierfehler muss der Systempfad zurückkommen, war %q", got)
	}
}
```

- [ ] **Step 2: Tests laufen lassen — müssen fehlschlagen**

Run: `go test ./internal/ytdlp/`
Expected: FAIL (`EnsureBinary` undefiniert)

- [ ] **Step 3: Implementierung** (`internal/ytdlp/update.go`)

```go
package ytdlp

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
)

// EnsureBinary stellt eine schreibbare yt-dlp-Kopie unter <configDir>/bin
// bereit, damit -U auch als Nicht-root funktioniert (Spec §3).
// Fehler sind nie fatal — es wird geloggt und der beste Pfad zurückgegeben.
func EnsureBinary(systemBin, configDir string, update bool, logf func(format string, args ...any)) string {
	dst := filepath.Join(configDir, "bin", "yt-dlp")
	if _, err := os.Stat(dst); errors.Is(err, os.ErrNotExist) {
		if err := copyBinary(systemBin, dst); err != nil {
			logf("yt-dlp-Kopie fehlgeschlagen, nutze %s: %v", systemBin, err)
			return systemBin
		}
	}
	if update {
		if out, err := exec.Command(dst, "-U").CombinedOutput(); err != nil {
			logf("yt-dlp-Update fehlgeschlagen (weiter mit vorhandener Version): %v: %s",
				err, tailString(string(out), 300))
		}
	}
	return dst
}

func copyBinary(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o755)
}
```

- [ ] **Step 4: Tests laufen lassen — müssen bestehen**

Run: `go test ./internal/ytdlp/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/ytdlp/
git commit -m "feat: yt-dlp-Kopie nach /config/bin mit optionalem Update"
```

---

### Task 9: Konfiguration (`internal/config`)

**Files:**
- Create: `internal/config/config.go`
- Test: `internal/config/config_test.go`

**Interfaces:**
- Consumes: —
- Produces (verwendet von `main` Task 13):
  - `type Config struct { Port, MaxConcurrent int; OutputTemplate, DownloadDir, ConfigDir string; UpdateOnStart bool }`
  - `func FromEnv(getenv func(string) string) Config` — Defaults per Spec §3; ungültige Zahlen fallen auf den Default zurück; `YTDLP_UPDATE_ON_START`: `false`/`0`/`no` (case-insensitiv) → `false`, alles andere → `true`

- [ ] **Step 1: Failing Tests schreiben** (`internal/config/config_test.go`)

```go
package config_test

import (
	"testing"

	"ytdlweb/internal/config"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestFromEnvDefaults(t *testing.T) {
	cfg := config.FromEnv(env(nil))
	if cfg.Port != 8080 || cfg.MaxConcurrent != 3 {
		t.Fatalf("Defaults falsch: %+v", cfg)
	}
	if cfg.OutputTemplate != "%(title)s [%(id)s].%(ext)s" {
		t.Fatalf("OUTPUT_TEMPLATE-Default falsch: %q", cfg.OutputTemplate)
	}
	if cfg.DownloadDir != "/downloads" || cfg.ConfigDir != "/config" || !cfg.UpdateOnStart {
		t.Fatalf("Defaults falsch: %+v", cfg)
	}
}

func TestFromEnvOverrides(t *testing.T) {
	cfg := config.FromEnv(env(map[string]string{
		"PORT":                  "9090",
		"MAX_CONCURRENT":        "5",
		"OUTPUT_TEMPLATE":       "%(id)s.%(ext)s",
		"DOWNLOAD_DIR":          "/mnt/media",
		"CONFIG_DIR":            "/mnt/cfg",
		"YTDLP_UPDATE_ON_START": "false",
	}))
	if cfg.Port != 9090 || cfg.MaxConcurrent != 5 || cfg.UpdateOnStart {
		t.Fatalf("Overrides nicht übernommen: %+v", cfg)
	}
	if cfg.OutputTemplate != "%(id)s.%(ext)s" || cfg.DownloadDir != "/mnt/media" || cfg.ConfigDir != "/mnt/cfg" {
		t.Fatalf("Overrides nicht übernommen: %+v", cfg)
	}
}

func TestFromEnvInvalidNumbersFallBack(t *testing.T) {
	cfg := config.FromEnv(env(map[string]string{"PORT": "abc", "MAX_CONCURRENT": "-2"}))
	if cfg.Port != 8080 || cfg.MaxConcurrent != 3 {
		t.Fatalf("ungültige Zahlen müssen auf Defaults fallen: %+v", cfg)
	}
}

func TestFromEnvUpdateFlagVariants(t *testing.T) {
	for _, v := range []string{"false", "FALSE", "0", "no"} {
		if config.FromEnv(env(map[string]string{"YTDLP_UPDATE_ON_START": v})).UpdateOnStart {
			t.Fatalf("%q muss Update deaktivieren", v)
		}
	}
	if !config.FromEnv(env(map[string]string{"YTDLP_UPDATE_ON_START": "true"})).UpdateOnStart {
		t.Fatal("true muss Update aktivieren")
	}
}
```

- [ ] **Step 2: Tests laufen lassen — müssen fehlschlagen**

Run: `go test ./internal/config/`
Expected: FAIL (Paket existiert nicht)

- [ ] **Step 3: Implementierung** (`internal/config/config.go`)

```go
// Package config liest die Laufzeit-Konfiguration aus Umgebungsvariablen (Spec §3).
package config

import (
	"strconv"
	"strings"
)

type Config struct {
	Port           int
	MaxConcurrent  int
	OutputTemplate string
	DownloadDir    string
	ConfigDir      string
	UpdateOnStart  bool
}

func FromEnv(getenv func(string) string) Config {
	cfg := Config{
		Port:           8080,
		MaxConcurrent:  3,
		OutputTemplate: "%(title)s [%(id)s].%(ext)s",
		DownloadDir:    "/downloads",
		ConfigDir:      "/config",
		UpdateOnStart:  true,
	}
	if n, err := strconv.Atoi(getenv("PORT")); err == nil && n > 0 {
		cfg.Port = n
	}
	if n, err := strconv.Atoi(getenv("MAX_CONCURRENT")); err == nil && n > 0 {
		cfg.MaxConcurrent = n
	}
	if v := getenv("OUTPUT_TEMPLATE"); v != "" {
		cfg.OutputTemplate = v
	}
	if v := getenv("DOWNLOAD_DIR"); v != "" {
		cfg.DownloadDir = v
	}
	if v := getenv("CONFIG_DIR"); v != "" {
		cfg.ConfigDir = v
	}
	switch strings.ToLower(getenv("YTDLP_UPDATE_ON_START")) {
	case "false", "0", "no":
		cfg.UpdateOnStart = false
	}
	return cfg
}
```

- [ ] **Step 4: Tests laufen lassen — müssen bestehen**

Run: `go test ./internal/config/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/config/
git commit -m "feat: Env-Konfiguration mit Spec-Defaults"
```

---

### Task 10: Queue — Dispatcher & Worker (`internal/queue`)

**Files:**
- Create: `internal/queue/queue.go`
- Test: `internal/queue/queue_test.go`

**Interfaces:**
- Consumes: `store.Store` (Task 3), `job` (Task 2)
- Produces (verwendet von `server` Task 12 und `main` Task 13):
  - `type Runner interface { Run(ctx context.Context, j job.Job, onProgress func(job.Progress)) error }` — von `ytdlp.ExecRunner` (Task 7) erfüllt
  - `func New(st *store.Store, r Runner, maxConcurrent int) *Queue`
  - `func (q *Queue) Start(ctx context.Context)` — startet den Dispatcher (Goroutine)
  - `func (q *Queue) Kick()` — Dispatcher anstoßen, blockiert nie, auch vor `Start` gefahrlos
  - `func (q *Queue) Cancel(id string)` — laufender Job: Context-Cancel (Runner beendet Prozessgruppe); wartender Job: direkt auf `canceled`
- Zustandsübergänge (Spec §4.3): Runner-Ende ohne Fehler → `done` (Percent 100); `ctx` abgebrochen → `canceled`; sonst → `error` mit `err.Error()`

- [ ] **Step 1: Failing Tests schreiben** (`internal/queue/queue_test.go`)

```go
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
```

- [ ] **Step 2: Tests laufen lassen — müssen fehlschlagen**

Run: `go test ./internal/queue/`
Expected: FAIL (Paket existiert nicht)

- [ ] **Step 3: Implementierung** (`internal/queue/queue.go`)

```go
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
		go q.runJob(jobCtx, j)
	}
}

func (q *Queue) runJob(ctx context.Context, j job.Job) {
	defer func() {
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
```

- [ ] **Step 4: Tests laufen lassen — müssen bestehen**

Run: `go test ./internal/queue/ -v -race`
Expected: PASS (5 Tests)

- [ ] **Step 5: Commit**

```bash
git add internal/queue/
git commit -m "feat: Worker-Queue mit MAX_CONCURRENT und Cancel"
```

---

### Task 11: Web-Assets & Embedding (`web/`)

**Files:**
- Create: `web/embed.go`
- Create: `web/templates/index.html`
- Create: `web/static/app.js`
- Create: `web/static/bootstrap.min.css` (Download, vendored)
- Create: `web/static/bootstrap.bundle.min.js` (Download, vendored)
- Test: `web/embed_test.go`

**Interfaces:**
- Consumes: HTTP-API-Formate aus Task 12 (JSON-Felder wie in Task 6/2 definiert: `format_id`, `vcodec`, `format_label`, `state`, `progress.percent` …)
- Produces (verwendet von `server` Task 12):
  - `package web` mit `var FS embed.FS`, enthält `templates/index.html` und `static/*`
- UI-Verhalten (Spec §7): Eingabe-Card → Probe; Auswahl-Card (Video: Modus „Beste Qualität"/„Formate wählen"/„Nur Audio" + zwei Selects; Playlist: Profil-Select); Jobs-Tabelle mit Polling alle 1,5 s

- [ ] **Step 1: Bootstrap 5.3.3 vendoren**

```bash
mkdir -p web/static web/templates
curl -fsSL -o web/static/bootstrap.min.css https://cdn.jsdelivr.net/npm/bootstrap@5.3.3/dist/css/bootstrap.min.css
curl -fsSL -o web/static/bootstrap.bundle.min.js https://cdn.jsdelivr.net/npm/bootstrap@5.3.3/dist/js/bootstrap.bundle.min.js
```

Verifizieren: beide Dateien > 50 KB (`ls -la web/static/`).

- [ ] **Step 2: Failing Test schreiben** (`web/embed_test.go`)

```go
package web_test

import (
	"testing"

	"ytdlweb/web"
)

func TestEmbeddedAssets(t *testing.T) {
	files := []string{
		"templates/index.html",
		"static/app.js",
		"static/bootstrap.min.css",
		"static/bootstrap.bundle.min.js",
	}
	for _, f := range files {
		data, err := web.FS.ReadFile(f)
		if err != nil {
			t.Fatalf("%s fehlt im Embed: %v", f, err)
		}
		if len(data) == 0 {
			t.Fatalf("%s ist leer", f)
		}
	}
}
```

Run: `go test ./web/` → FAIL (Paket existiert nicht)

- [ ] **Step 3: Embed-Datei anlegen** (`web/embed.go`)

```go
// Package web bettet alle UI-Assets ins Binary ein — die Oberfläche
// funktioniert ohne Internetzugang (Spec §3).
package web

import "embed"

//go:embed templates static
var FS embed.FS
```

- [ ] **Step 4: index.html anlegen** (`web/templates/index.html`)

```html
<!doctype html>
<html lang="de" data-bs-theme="dark">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>yt-dl Web</title>
  <link rel="stylesheet" href="/static/bootstrap.min.css">
</head>
<body>
  <div class="container py-4" style="max-width: 960px">
    <h1 class="mb-4">yt-dl Web</h1>

    <div class="card mb-4">
      <div class="card-body">
        <label for="url-input" class="form-label">Video- oder Playlist-URL</label>
        <div class="input-group">
          <input type="url" class="form-control" id="url-input" placeholder="https://…" autofocus>
          <button class="btn btn-primary" id="probe-btn">Analysieren</button>
        </div>
        <div class="alert alert-danger mt-3 d-none" id="probe-error"></div>
      </div>
    </div>

    <div class="card mb-4 d-none" id="select-card">
      <div class="card-body">
        <div class="d-flex gap-3 mb-3">
          <img id="video-thumb" src="" alt="" class="rounded d-none"
               style="width:160px;height:auto;object-fit:cover">
          <div>
            <h5 id="select-title" class="mb-1"></h5>
            <div id="select-subtitle" class="text-body-secondary"></div>
          </div>
        </div>

        <div id="video-options">
          <div class="mb-3">
            <div class="form-check form-check-inline">
              <input class="form-check-input" type="radio" name="mode" id="mode-best" value="best" checked>
              <label class="form-check-label" for="mode-best">Beste Qualität</label>
            </div>
            <div class="form-check form-check-inline">
              <input class="form-check-input" type="radio" name="mode" id="mode-manual" value="manual">
              <label class="form-check-label" for="mode-manual">Formate wählen</label>
            </div>
            <div class="form-check form-check-inline">
              <input class="form-check-input" type="radio" name="mode" id="mode-audio" value="audio">
              <label class="form-check-label" for="mode-audio">Nur Audio</label>
            </div>
          </div>
          <div class="row g-3 d-none" id="format-selects">
            <div class="col-md-6" id="video-col">
              <label class="form-label" for="video-format">Videoformat</label>
              <select class="form-select" id="video-format"></select>
            </div>
            <div class="col-md-6">
              <label class="form-label" for="audio-format">Audioformat</label>
              <select class="form-select" id="audio-format"></select>
            </div>
          </div>
        </div>

        <div id="playlist-options" class="d-none">
          <label class="form-label" for="profile-select">Qualitätsprofil</label>
          <select class="form-select" id="profile-select">
            <option value="best">Beste Qualität</option>
            <option value="1080p">Beste ≤1080p</option>
            <option value="720p">Beste ≤720p</option>
            <option value="audio">Nur Audio</option>
          </select>
        </div>

        <button class="btn btn-success mt-3" id="start-btn">Download starten</button>
        <div class="alert alert-danger mt-3 d-none" id="start-error"></div>
      </div>
    </div>

    <div class="card">
      <div class="card-header">Downloads</div>
      <div class="table-responsive">
        <table class="table align-middle mb-0">
          <thead>
            <tr>
              <th>Titel</th>
              <th>Format</th>
              <th>Status</th>
              <th style="min-width:160px">Fortschritt</th>
              <th>Tempo</th>
              <th>ETA</th>
              <th></th>
            </tr>
          </thead>
          <tbody id="jobs-tbody"></tbody>
        </table>
      </div>
    </div>
  </div>
  <script src="/static/bootstrap.bundle.min.js"></script>
  <script src="/static/app.js"></script>
</body>
</html>
```

- [ ] **Step 5: app.js anlegen** (`web/static/app.js`)

```js
"use strict";

const $ = (id) => document.getElementById(id);

let probeResult = null;

async function api(path, options = {}) {
  const res = await fetch(path, {
    headers: { "Content-Type": "application/json" },
    ...options,
  });
  if (res.status === 204) return null;
  const body = await res.json().catch(() => ({}));
  if (!res.ok) throw new Error(body.error || `HTTP ${res.status}`);
  return body;
}

function show(el) { el.classList.remove("d-none"); }
function hide(el) { el.classList.add("d-none"); }

function esc(s) {
  // Escapt auch Quotes — der Wert landet teils in Attribut-Kontexten.
  return String(s ?? "").replace(/[&<>"'`]/g, (c) => ({
    "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;", "`": "&#96;",
  }[c]));
}

function humanSize(bytes) {
  if (!bytes) return "";
  const units = ["B", "KiB", "MiB", "GiB"];
  let i = 0;
  let n = bytes;
  while (n >= 1024 && i < units.length - 1) { n /= 1024; i++; }
  return `${n.toFixed(n >= 10 ? 0 : 1)} ${units[i]}`;
}

// --- Analyse ---------------------------------------------------------------

$("probe-btn").addEventListener("click", probe);
$("url-input").addEventListener("keydown", (e) => { if (e.key === "Enter") probe(); });

async function probe() {
  const url = $("url-input").value.trim();
  hide($("probe-error"));
  hide($("select-card"));
  if (!url) return;
  $("probe-btn").disabled = true;
  $("probe-btn").textContent = "Analysiere…";
  try {
    probeResult = await api("/api/probe", { method: "POST", body: JSON.stringify({ url }) });
    renderSelectCard();
  } catch (err) {
    $("probe-error").textContent = err.message;
    show($("probe-error"));
  } finally {
    $("probe-btn").disabled = false;
    $("probe-btn").textContent = "Analysieren";
  }
}

function renderSelectCard() {
  hide($("start-error"));
  if (probeResult.type === "playlist") {
    const pl = probeResult.playlist;
    $("select-title").textContent = pl.title || "Playlist";
    $("select-subtitle").textContent = `Playlist · ${pl.entries.length} Videos`;
    hide($("video-thumb"));
    hide($("video-options"));
    show($("playlist-options"));
  } else {
    const v = probeResult.video;
    $("select-title").textContent = v.title;
    $("select-subtitle").textContent = "";
    if (v.thumbnail) {
      $("video-thumb").src = v.thumbnail;
      show($("video-thumb"));
    } else {
      hide($("video-thumb"));
    }
    fillFormatSelects(v.formats || []);
    show($("video-options"));
    hide($("playlist-options"));
    updateModeVisibility();
  }
  show($("select-card"));
}

function fillFormatSelects(formats) {
  const isVideo = (f) => f.vcodec && f.vcodec !== "none";
  const isAudio = (f) => f.acodec && f.acodec !== "none" && (!f.vcodec || f.vcodec === "none");
  const byRate = (a, b) => (b.tbr || b.abr || 0) - (a.tbr || a.abr || 0);

  const vids = formats.filter(isVideo).sort(byRate);
  const auds = formats.filter(isAudio).sort(byRate);

  $("video-format").innerHTML = vids.map((f) => {
    const label = [f.resolution, f.fps ? `${f.fps}fps` : "", f.vcodec, f.ext, humanSize(f.filesize)]
      .filter(Boolean).join(" · ");
    return `<option value="${esc(f.format_id)}">${esc(label)}</option>`;
  }).join("");
  $("audio-format").innerHTML = auds.map((f) => {
    const label = [f.acodec, f.abr ? `${Math.round(f.abr)} kbit/s` : "", f.ext, humanSize(f.filesize)]
      .filter(Boolean).join(" · ");
    return `<option value="${esc(f.format_id)}">${esc(label)}</option>`;
  }).join("");
}

document.querySelectorAll('input[name="mode"]').forEach((el) =>
  el.addEventListener("change", updateModeVisibility)
);

function currentMode() {
  return document.querySelector('input[name="mode"]:checked').value;
}

function updateModeVisibility() {
  const mode = currentMode();
  if (mode === "manual") {
    show($("format-selects"));
    show($("video-col"));
  } else if (mode === "audio") {
    show($("format-selects"));
    hide($("video-col"));
  } else {
    hide($("format-selects"));
  }
}

// --- Download starten ------------------------------------------------------

$("start-btn").addEventListener("click", start);

async function start() {
  hide($("start-error"));
  try {
    if (probeResult.type === "playlist") {
      await api("/api/jobs", {
        method: "POST",
        body: JSON.stringify({
          type: "playlist",
          profile: $("profile-select").value,
          playlist_title: probeResult.playlist.title,
          entries: probeResult.playlist.entries,
        }),
      });
    } else {
      const mode = currentMode();
      await api("/api/jobs", {
        method: "POST",
        body: JSON.stringify({
          type: "video",
          url: $("url-input").value.trim(),
          title: probeResult.video.title,
          audio_only: mode === "audio",
          format_video: mode === "manual" ? $("video-format").value : "",
          format_audio: mode !== "best" ? $("audio-format").value : "",
          format_label: formatLabel(mode),
        }),
      });
    }
    hide($("select-card"));
    $("url-input").value = "";
    await refreshJobs();
  } catch (err) {
    $("start-error").textContent = err.message;
    show($("start-error"));
  }
}

function formatLabel(mode) {
  if (mode === "best") return "Beste Qualität";
  const audioText = $("audio-format").selectedOptions[0]?.textContent.trim() || "beste";
  if (mode === "audio") return `Nur Audio (${audioText})`;
  const videoText = $("video-format").selectedOptions[0]?.textContent.trim() || "";
  return `${videoText} + ${audioText}`;
}

// --- Jobs-Tabelle ----------------------------------------------------------

const STATE_BADGES = {
  queued:   ["text-bg-secondary", "Wartet"],
  running:  ["text-bg-primary", "Lädt"],
  done:     ["text-bg-success", "Fertig"],
  error:    ["text-bg-danger", "Fehler"],
  canceled: ["text-bg-warning", "Abgebrochen"],
};

async function refreshJobs() {
  try {
    const body = await api("/api/jobs");
    renderJobs(body.jobs || []);
  } catch {
    // Polling-Fehler ignorieren; der nächste Tick versucht es erneut.
  }
}

function renderJobs(jobs) {
  $("jobs-tbody").innerHTML = jobs.map((j) => {
    const [badge, label] = STATE_BADGES[j.state] || ["text-bg-secondary", esc(j.state)];
    const pct = Math.round(j.progress?.percent || 0);
    let title = esc(j.title || j.url);
    if (j.playlist_title) {
      title += ` <small class="text-body-secondary">(${esc(j.playlist_title)})</small>`;
    }
    const errorHTML = j.error
      ? `<div class="small text-danger">${esc(j.error)}</div>` : "";
    const animated = j.state === "running" ? " progress-bar-striped progress-bar-animated" : "";
    return `<tr>
      <td>${title}${errorHTML}</td>
      <td class="small">${esc(j.format_label)}</td>
      <td><span class="badge ${badge}">${label}</span></td>
      <td>
        <div class="progress" role="progressbar" aria-valuenow="${pct}"
             aria-valuemin="0" aria-valuemax="100">
          <div class="progress-bar${animated}" style="width:${pct}%">${pct}%</div>
        </div>
      </td>
      <td class="small">${esc(j.progress?.speed || "")}</td>
      <td class="small">${esc(j.progress?.eta || "")}</td>
      <td class="text-nowrap">${actionButtons(j)}</td>
    </tr>`;
  }).join("");
}

function actionButtons(j) {
  const btn = (action, label, cls) =>
    `<button class="btn btn-sm ${cls}" data-action="${action}" data-id="${j.id}">${label}</button>`;
  if (j.state === "queued" || j.state === "running") {
    return btn("cancel", "Abbrechen", "btn-outline-warning");
  }
  const parts = [];
  if (j.state === "error" || j.state === "canceled") {
    parts.push(btn("retry", "Erneut", "btn-outline-primary"));
  }
  parts.push(btn("delete", "Entfernen", "btn-outline-danger"));
  return parts.join(" ");
}

$("jobs-tbody").addEventListener("click", async (e) => {
  const btn = e.target.closest("button[data-action]");
  if (!btn) return;
  const { action, id } = btn.dataset;
  try {
    if (action === "delete") {
      await api(`/api/jobs/${id}`, { method: "DELETE" });
    } else {
      await api(`/api/jobs/${id}/${action}`, { method: "POST" });
    }
    await refreshJobs();
  } catch (err) {
    alert(err.message);
  }
});

refreshJobs();
setInterval(refreshJobs, 1500);
```

- [ ] **Step 6: Test laufen lassen — muss bestehen**

Run: `go test ./web/ -v`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add web/
git commit -m "feat: Bootstrap-UI mit Formatauswahl und Job-Tabelle (embedded)"
```

---

### Task 12: HTTP-Server & API (`internal/server`)

**Files:**
- Create: `internal/server/server.go`
- Test: `internal/server/server_test.go`

**Interfaces:**
- Consumes: `store.Store` (Task 3), `queue.Queue` (Task 10), `ytdlp.ProbeResult`/`BuildFormat`/`ProfileByKey` (Tasks 4/6), `web.FS` (Task 11), `job` (Task 2)
- Produces (verwendet von `main` Task 13):
  - `type Prober interface { Probe(ctx context.Context, url string) (*ytdlp.ProbeResult, error) }` — von `ytdlp.Prober` erfüllt
  - `func New(st *store.Store, q *queue.Queue, p Prober) http.Handler`
- Routen und Statuscodes (Spec §6, §8): siehe Tests unten; Fehler immer als `{"error": "..."}`

- [ ] **Step 1: Failing Tests schreiben** (`internal/server/server_test.go`)

```go
package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"ytdlweb/internal/job"
	"ytdlweb/internal/queue"
	"ytdlweb/internal/server"
	"ytdlweb/internal/store"
	"ytdlweb/internal/ytdlp"
)

type nopRunner struct{}

func (nopRunner) Run(context.Context, job.Job, func(job.Progress)) error { return nil }

type fakeProber struct {
	res *ytdlp.ProbeResult
	err error
}

func (f fakeProber) Probe(context.Context, string) (*ytdlp.ProbeResult, error) {
	return f.res, f.err
}

// newServer baut den Handler mit nicht gestarteter Queue — Jobs bleiben queued.
func newServer(t *testing.T, p server.Prober) (http.Handler, *store.Store) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "jobs.json"))
	if err != nil {
		t.Fatal(err)
	}
	q := queue.New(st, nopRunner{}, 1)
	return server.New(st, q, p), st
}

func do(t *testing.T, h http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func videoProbe() *ytdlp.ProbeResult {
	return &ytdlp.ProbeResult{
		Type: "video",
		Video: &ytdlp.Video{
			ID: "abc", Title: "Test Video",
			Formats: []ytdlp.Format{{ID: "303", VCodec: "vp9", ACodec: "none"}},
		},
	}
}

func TestHealthz(t *testing.T) {
	h, _ := newServer(t, fakeProber{})
	rec := do(t, h, "GET", "/healthz", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("healthz: %d", rec.Code)
	}
}

func TestIndexAndStatic(t *testing.T) {
	h, _ := newServer(t, fakeProber{})
	rec := do(t, h, "GET", "/", nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "yt-dl Web") {
		t.Fatalf("Index fehlt: %d", rec.Code)
	}
	rec = do(t, h, "GET", "/static/app.js", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("Static-Route: %d", rec.Code)
	}
}

func TestProbeReturnsResult(t *testing.T) {
	h, _ := newServer(t, fakeProber{res: videoProbe()})
	rec := do(t, h, "POST", "/api/probe", map[string]string{"url": "https://example.com/v"})
	if rec.Code != http.StatusOK {
		t.Fatalf("Code %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"format_id":"303"`) {
		t.Fatalf("Formatliste fehlt: %s", rec.Body.String())
	}
}

func TestProbeValidation(t *testing.T) {
	h, _ := newServer(t, fakeProber{res: videoProbe()})
	if rec := do(t, h, "POST", "/api/probe", map[string]string{"url": "  "}); rec.Code != http.StatusBadRequest {
		t.Fatalf("leere URL: %d", rec.Code)
	}
}

func TestProbeErrorBecomes502(t *testing.T) {
	h, _ := newServer(t, fakeProber{err: errors.New("yt-dlp: Unsupported URL")})
	rec := do(t, h, "POST", "/api/probe", map[string]string{"url": "https://example.invalid"})
	if rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), "Unsupported URL") {
		t.Fatalf("Code %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCreateVideoJob(t *testing.T) {
	h, st := newServer(t, fakeProber{})
	rec := do(t, h, "POST", "/api/jobs", map[string]any{
		"type": "video", "url": "https://example.com/v", "title": "Test",
		"format_video": "303", "format_audio": "251", "format_label": "1080p · vp9",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("Code %d: %s", rec.Code, rec.Body.String())
	}
	jobs := st.List()
	if len(jobs) != 1 || jobs[0].Format != "303+251" || jobs[0].FormatLabel != "1080p · vp9" {
		t.Fatalf("Job falsch angelegt: %+v", jobs)
	}
}

func TestCreateVideoJobDuplicate(t *testing.T) {
	h, _ := newServer(t, fakeProber{})
	body := map[string]any{"type": "video", "url": "https://example.com/v", "format_video": "303"}
	if rec := do(t, h, "POST", "/api/jobs", body); rec.Code != http.StatusCreated {
		t.Fatalf("erster: %d", rec.Code)
	}
	if rec := do(t, h, "POST", "/api/jobs", body); rec.Code != http.StatusConflict {
		t.Fatalf("Duplikat muss 409 liefern, war %d", rec.Code)
	}
}

func TestCreatePlaylistJobs(t *testing.T) {
	h, st := newServer(t, fakeProber{})
	rec := do(t, h, "POST", "/api/jobs", map[string]any{
		"type": "playlist", "profile": "720p", "playlist_title": "Liste",
		"entries": []map[string]string{
			{"url": "https://example.com/1", "title": "Eins"},
			{"url": "https://example.com/2", "title": "Zwei"},
		},
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("Code %d: %s", rec.Code, rec.Body.String())
	}
	jobs := st.List()
	if len(jobs) != 2 {
		t.Fatalf("2 Jobs erwartet: %+v", jobs)
	}
	for _, j := range jobs {
		if j.Format != "bv*[height<=720]+ba/b[height<=720]" || j.PlaylistTitle != "Liste" {
			t.Fatalf("Playlist-Job falsch: %+v", j)
		}
	}
}

func TestCreatePlaylistValidation(t *testing.T) {
	h, _ := newServer(t, fakeProber{})
	rec := do(t, h, "POST", "/api/jobs", map[string]any{
		"type": "playlist", "profile": "gibtsnicht",
		"entries": []map[string]string{{"url": "https://example.com/1"}},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unbekanntes Profil: %d", rec.Code)
	}
	rec = do(t, h, "POST", "/api/jobs", map[string]any{"type": "playlist", "profile": "best"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("leere Einträge: %d", rec.Code)
	}
}

func TestListJobs(t *testing.T) {
	h, st := newServer(t, fakeProber{})
	_ = st.Add(job.New("https://example.com/v", "Test", "ba", "l", ""))
	rec := do(t, h, "GET", "/api/jobs", nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"state":"queued"`) {
		t.Fatalf("Code %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCancelQueuedJob(t *testing.T) {
	h, st := newServer(t, fakeProber{})
	j := job.New("https://example.com/v", "Test", "ba", "l", "")
	_ = st.Add(j)
	if rec := do(t, h, "POST", "/api/jobs/"+j.ID+"/cancel", nil); rec.Code != http.StatusNoContent {
		t.Fatalf("Cancel: %d", rec.Code)
	}
	got, _ := st.Get(j.ID)
	if got.State != job.StateCanceled {
		t.Fatalf("Zustand %s", got.State)
	}
	// done-Job kann nicht abgebrochen werden
	if rec := do(t, h, "POST", "/api/jobs/"+j.ID+"/cancel", nil); rec.Code != http.StatusConflict {
		t.Fatalf("Cancel auf canceled muss 409 sein: %d", rec.Code)
	}
}

func TestRetryJob(t *testing.T) {
	h, st := newServer(t, fakeProber{})
	j := job.New("https://example.com/v", "Test", "ba", "l", "")
	_ = st.Add(j)
	_ = st.Update(j.ID, func(x *job.Job) {
		x.State = job.StateError
		x.Error = "kaputt"
		x.Progress = job.Progress{Percent: 33}
	})
	if rec := do(t, h, "POST", "/api/jobs/"+j.ID+"/retry", nil); rec.Code != http.StatusNoContent {
		t.Fatalf("Retry: %d", rec.Code)
	}
	got, _ := st.Get(j.ID)
	if got.State != job.StateQueued || got.Error != "" || got.Progress.Percent != 0 {
		t.Fatalf("Retry muss zurücksetzen: %+v", got)
	}
	// queued-Job kann nicht erneut versucht werden
	if rec := do(t, h, "POST", "/api/jobs/"+j.ID+"/retry", nil); rec.Code != http.StatusConflict {
		t.Fatalf("Retry auf queued muss 409 sein: %d", rec.Code)
	}
}

func TestDeleteJob(t *testing.T) {
	h, st := newServer(t, fakeProber{})
	j := job.New("https://example.com/v", "Test", "ba", "l", "")
	_ = st.Add(j)
	_ = st.Update(j.ID, func(x *job.Job) { x.State = job.StateRunning })
	if rec := do(t, h, "DELETE", "/api/jobs/"+j.ID, nil); rec.Code != http.StatusConflict {
		t.Fatalf("laufender Job darf nicht gelöscht werden: %d", rec.Code)
	}
	_ = st.Update(j.ID, func(x *job.Job) { x.State = job.StateDone })
	if rec := do(t, h, "DELETE", "/api/jobs/"+j.ID, nil); rec.Code != http.StatusNoContent {
		t.Fatalf("Delete: %d", rec.Code)
	}
	if _, ok := st.Get(j.ID); ok {
		t.Fatal("Job muss entfernt sein")
	}
}

func TestUnknownJobID(t *testing.T) {
	h, _ := newServer(t, fakeProber{})
	for _, c := range []struct{ method, path string }{
		{"POST", "/api/jobs/nix/cancel"},
		{"POST", "/api/jobs/nix/retry"},
		{"DELETE", "/api/jobs/nix"},
	} {
		if rec := do(t, h, c.method, c.path, nil); rec.Code != http.StatusNotFound {
			t.Fatalf("%s %s: %d", c.method, c.path, rec.Code)
		}
	}
}
```

- [ ] **Step 2: Tests laufen lassen — müssen fehlschlagen**

Run: `go test ./internal/server/`
Expected: FAIL (Paket existiert nicht)

- [ ] **Step 3: Implementierung** (`internal/server/server.go`)

```go
// Package server stellt die HTTP-API und die eingebettete UI bereit (Spec §6).
package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

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
	store  *store.Store
	queue  *queue.Queue
	prober Prober
}

func New(st *store.Store, q *queue.Queue, p Prober) http.Handler {
	s := &Server{store: st, queue: q, prober: p}
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
	data, err := web.FS.ReadFile("templates/index.html")
	if err != nil {
		http.Error(w, "Template fehlt", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(data)
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
	res, err := s.prober.Probe(r.Context(), strings.TrimSpace(req.URL))
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
```

- [ ] **Step 4: Tests laufen lassen — müssen bestehen**

Run: `go test ./internal/server/ -v -race`
Expected: PASS (alle Handler-Tests)

- [ ] **Step 5: Gesamtcheck + Commit**

Run: `make check`
Expected: alles grün

```bash
git add internal/server/
git commit -m "feat: HTTP-API mit Probe, Job-Verwaltung und statischer UI"
```

---

### Task 13: main — Wiring & Healthcheck (`cmd/server/main.go`)

**Files:**
- Modify: `cmd/server/main.go` (Platzhalter aus Task 1 komplett ersetzen)

**Interfaces:**
- Consumes: `config.FromEnv` (Task 9), `store.Open` (Task 3), `queue.New/Start/Kick` (Task 10), `ytdlp.EnsureBinary/ExecRunner/Prober` (Tasks 6–8), `server.New` (Task 12)
- Produces: Binary mit zwei Betriebsarten: normal (Server) und `-healthcheck` (Exit 0/1, Spec §3)

- [ ] **Step 1: Implementierung** (`cmd/server/main.go`, kompletter Inhalt)

```go
// Der Serverprozess: Konfiguration lesen, Store laden, Queue starten,
// HTTP bedienen. Mit -healthcheck prüft sich der Container selbst.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"ytdlweb/internal/config"
	"ytdlweb/internal/queue"
	"ytdlweb/internal/server"
	"ytdlweb/internal/store"
	"ytdlweb/internal/ytdlp"
)

// systemYtdlp ist der Pfad im mikenye/youtube-dl-Base-Image (Spec §2).
const systemYtdlp = "/usr/local/bin/yt-dlp"

func main() {
	healthcheck := flag.Bool("healthcheck", false, "Healthcheck ausführen und beenden")
	flag.Parse()

	cfg := config.FromEnv(os.Getenv)

	if *healthcheck {
		os.Exit(runHealthcheck(cfg.Port))
	}
	if err := run(cfg); err != nil {
		log.Fatal(err)
	}
}

func runHealthcheck(port int) int {
	client := &http.Client{Timeout: 3 * time.Second}
	res, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/healthz", port))
	if err != nil || res.StatusCode != http.StatusOK {
		return 1
	}
	return 0
}

func run(cfg config.Config) error {
	for _, dir := range []string{cfg.DownloadDir, cfg.ConfigDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}

	bin := ytdlp.EnsureBinary(systemYtdlp, cfg.ConfigDir, cfg.UpdateOnStart, log.Printf)
	log.Printf("verwende yt-dlp: %s", bin)

	st, err := store.Open(filepath.Join(cfg.ConfigDir, "jobs.json"))
	if err != nil {
		return err
	}

	runner := &ytdlp.ExecRunner{
		Bin:            bin,
		DownloadDir:    cfg.DownloadDir,
		OutputTemplate: cfg.OutputTemplate,
	}
	q := queue.New(st, runner, cfg.MaxConcurrent)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	q.Start(ctx)
	q.Kick() // durch Recovery wieder eingereihte Jobs sofort anstoßen

	srv := &http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.Port),
		Handler: server.New(st, q, &ytdlp.Prober{Bin: bin}),
	}
	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe() }()
	log.Printf("yt-dl-web-client läuft auf Port %d", cfg.Port)

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
		return nil
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}
```

- [ ] **Step 2: Build und Smoke-Test**

```bash
make build
mkdir -p tmp/downloads tmp/config
PORT=18080 DOWNLOAD_DIR=tmp/downloads CONFIG_DIR=tmp/config \
  YTDLP_UPDATE_ON_START=false ./bin/app &
sleep 1
curl -fsS http://127.0.0.1:18080/healthz          # erwartet: ok
PORT=18080 ./bin/app -healthcheck; echo "exit=$?" # erwartet: exit=0
curl -fsS http://127.0.0.1:18080/ | head -c 200   # erwartet: HTML mit "yt-dl Web"
kill %1
PORT=18080 ./bin/app -healthcheck; echo "exit=$?" # erwartet: exit=1
```

Hinweis: `EnsureBinary` loggt lokal einen Kopier-Fehlschlag (kein `/usr/local/bin/yt-dlp` auf dem Mac) — das ist der vorgesehene Fallback, kein Fehler.

- [ ] **Step 3: Gesamtcheck**

Run: `make check`
Expected: alles grün

- [ ] **Step 4: Commit**

```bash
git add cmd/server/main.go
git commit -m "feat: Server-Wiring mit Graceful Shutdown und -healthcheck"
```

---

### Task 14: Docker-Image, Compose & README

**Files:**
- Create: `Dockerfile`
- Create: `docker-compose.yml`

**Interfaces:**
- Consumes: Make-Targets (Task 1), Binary (Task 13)
- Produces: lauffähiges amd64-Image `yt-dl-web-client`, NAS-Compose-Vorlage

- [ ] **Step 1: Dockerfile anlegen** (Spec §3)

```dockerfile
# Stage 1: Go-Build
FROM golang:1.24 AS build
WORKDIR /src
COPY go.mod ./
COPY . .
RUN CGO_ENABLED=0 go build -o /app ./cmd/server

# Stage 2: Runtime — mikenye/youtube-dl als Base (Spec §2).
# yt-dlp + ffmpeg sind enthalten; das s6-Init (/init) wird bewusst
# durch unseren Webservice ersetzt.
FROM mikenye/youtube-dl
COPY --from=build /app /usr/local/bin/app
ENTRYPOINT ["/usr/local/bin/app"]
HEALTHCHECK --interval=30s --timeout=5s --start-period=15s \
  CMD ["/usr/local/bin/app", "-healthcheck"]
```

- [ ] **Step 2: docker-compose.yml anlegen** (NAS-Vorlage, Spec §3)

```yaml
services:
  yt-dl-web:
    build: .
    image: yt-dl-web-client
    container_name: yt-dl-web
    # UID:GID des NAS-Nutzers, dem die Downloads gehören sollen
    user: "1000:1000"
    ports:
      - "8080:8080"
    environment:
      MAX_CONCURRENT: "3"
      YTDLP_UPDATE_ON_START: "true"
      # OUTPUT_TEMPLATE: "%(title)s [%(id)s].%(ext)s"
    volumes:
      - ./data/downloads:/downloads
      - ./data/config:/config
    restart: unless-stopped
```

- [ ] **Step 3: README — entfällt hier**

Die README entsteht als eigener Task 16 (englischsprachige GitHub-README + LICENSE, Nutzer-Anforderung vom 2026-08-15). Dieser Schritt entfällt ersatzlos.

<!-- ehemaliger deutscher README-Entwurf, ersetzt durch Task 16:
````markdown
# yt-dl-web-client

Web-Oberfläche für yt-dlp auf dem NAS: URL eingeben, Format wählen,
parallele Downloads mit persistenter Warteschlange. Downloads landen
im gemounteten `/downloads`-Volume.

## Schnellstart (NAS / Docker)

```bash
make up        # baut das Image und startet den Container
# UI: http://<nas>:8080
make down      # stoppt den Container
```

Vor dem Start in `docker-compose.yml` anpassen:

- `user:` — UID:GID des Nutzers, dem die Downloads gehören sollen
- `volumes:` — Zielordner für Downloads und Konfiguration

## Konfiguration (Umgebungsvariablen)

| Variable | Default | Zweck |
|---|---|---|
| `PORT` | `8080` | HTTP-Port |
| `MAX_CONCURRENT` | `3` | parallele Downloads |
| `OUTPUT_TEMPLATE` | `%(title)s [%(id)s].%(ext)s` | yt-dlp-Dateinamensschema |
| `YTDLP_UPDATE_ON_START` | `true` | yt-dlp beim Start aktualisieren |

## Entwicklung

```bash
make check     # gofmt + go vet + Tests
make run       # lokal starten (nutzt ./tmp als Volumes-Ersatz)
make image     # Docker-Image bauen (amd64)
```

Design-Spec: `docs/superpowers/specs/2026-08-14-yt-dl-web-client-design.md`
````
-->

- [ ] **Step 4: Image bauen und Smoke-Test**

```bash
make image
docker run -d --rm --name yt-dl-smoke -p 18081:8080 \
  -e YTDLP_UPDATE_ON_START=false yt-dl-web-client
sleep 2
curl -fsS http://127.0.0.1:18081/healthz   # erwartet: ok
docker exec yt-dl-smoke /usr/local/bin/app -healthcheck && echo healthy
docker stop yt-dl-smoke
```

Falls Docker lokal nicht verfügbar ist: Schritt dokumentiert überspringen und im Abschlussbericht vermerken — der Image-Build muss dann auf dem NAS/CI verifiziert werden.

- [ ] **Step 5: Commit**

```bash
git add Dockerfile docker-compose.yml
git commit -m "feat: Docker-Image auf mikenye-Base mit Compose-Vorlage"
```

---

### Task 15 (optional): Integrationstest gegen echtes yt-dlp

Nur lokal sinnvoll, läuft nie in CI (Spec §9). Überspringen, wenn kein
yt-dlp installiert ist oder keine Netzverbindung besteht.

**Files:**
- Create: `internal/ytdlp/integration_test.go`

**Interfaces:**
- Consumes: `Prober` (Task 6)
- Produces: Absicherung, dass das reale `-J`-Format zu `ParseProbeJSON` passt

- [ ] **Step 1: Test schreiben** (`internal/ytdlp/integration_test.go`)

```go
//go:build integration

package ytdlp_test

import (
	"context"
	"os/exec"
	"testing"
	"time"

	"ytdlweb/internal/ytdlp"
)

// Prüft gegen das reale yt-dlp, dass unser Parsing zum -J-Format passt.
func TestProbeRealYtdlp(t *testing.T) {
	bin, err := exec.LookPath("yt-dlp")
	if err != nil {
		t.Skip("yt-dlp nicht installiert")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	p := &ytdlp.Prober{Bin: bin}
	// "Me at the zoo" — erstes YouTube-Video, sehr stabil verfügbar
	res, err := p.Probe(ctx, "https://www.youtube.com/watch?v=jNQXAC9IVRw")
	if err != nil {
		t.Fatalf("Probe fehlgeschlagen (Netz?): %v", err)
	}
	if res.Type != "video" || res.Video == nil || len(res.Video.Formats) == 0 {
		t.Fatalf("unerwartetes Resultat: %+v", res)
	}
}
```

- [ ] **Step 2: Ausführen (nur lokal)**

Run: `go test ./internal/ytdlp/ -tags integration -run TestProbeRealYtdlp -v`
Expected: PASS (oder SKIP ohne yt-dlp)

- [ ] **Step 3: Commit**

```bash
git add internal/ytdlp/integration_test.go
git commit -m "test: optionaler Integrationstest gegen reales yt-dlp"
```

---

### Task 16: GitHub-README (englisch) & LICENSE

Nutzer-Anforderung (2026-08-15): Das Repo wird auf GitHub veröffentlicht; andere
sollen von der Arbeit profitieren, und der Nutzer will selbst nachlesen können,
wie man das Projekt aufruft. Sprache: Englisch. Lizenz: PolyForm Noncommercial
1.0.0 (kommerzielle Nutzung ausgeschlossen — bewusst kein OSI-Open-Source).

**Files:**
- Create: `README.md`
- Create: `LICENSE.md`

**Interfaces:**
- Consumes: Make-Targets (Task 1), Compose/Dockerfile (Task 14), Env-Defaults (Task 9)
- Produces: öffentliche Projekt-Dokumentation; keine Code-Schnittstellen

- [ ] **Step 1: LICENSE.md anlegen**

Offiziellen Lizenztext der PolyForm Noncommercial 1.0.0 beschaffen (Quelle:
https://polyformproject.org/licenses/noncommercial/1.0.0/ — den reinen
Lizenztext übernehmen, keine Website-Navigation). Verifizieren, dass die Datei
die Phrasen „PolyForm Noncommercial License 1.0.0" und „noncommercial purposes"
(case-insensitiv) enthält und mindestens 3 KB groß ist.

- [ ] **Step 2: README.md anlegen** (Inhalt verbatim)

````markdown
# yt-dl-web-client

A self-hosted web UI for [yt-dlp](https://github.com/yt-dlp/yt-dlp), built to
run on a NAS. Paste a video or playlist URL, pick the exact video/audio format
(or a quality profile), and let the server download in the background — with a
persistent job queue, live progress, and parallel downloads. Finished files
land directly in a mounted folder (e.g. your media library).

## Features

- Paste a URL, inspect title, thumbnail and every available format (via `yt-dlp -J`)
- Pick exact video + audio formats, best quality, or audio-only
- Playlist support: one job per video, with quality profiles (best / ≤1080p / ≤720p / audio only)
- Parallel downloads (configurable), with live progress, speed and ETA
- Persistent job queue: survives container restarts, interrupted downloads resume (`--continue`)
- Retry, cancel and remove jobs from the UI (removing a job never deletes files)
- Single container: one Go binary with an embedded Bootstrap UI, no CDN, UI works offline
- Optional yt-dlp self-update on container start

## Quick start

Requires Docker with Compose v2. Clone the repository, then:

```bash
make up        # builds the image and starts the container
```

Open `http://<your-host>:8080`, paste a URL, choose a format, hit
"Download starten". Finished files appear in `./data/downloads/`.

```bash
make down      # stops the container
```

Before the first start, adjust `docker-compose.yml`:

- `user:` — UID:GID that should own the downloaded files
- `volumes:` — where downloads and the queue state are stored
- `ports:` — host port (default 8080)

> **Note:** The web UI has no authentication. Run it on a trusted network
> (LAN), or put a reverse proxy with auth in front of it.

## Configuration

| Environment variable | Default | Purpose |
|---|---|---|
| `PORT` | `8080` | HTTP port of the web service |
| `MAX_CONCURRENT` | `3` | Number of parallel downloads |
| `OUTPUT_TEMPLATE` | `%(title)s [%(id)s].%(ext)s` | yt-dlp output filename template |
| `YTDLP_UPDATE_ON_START` | `true` | Update yt-dlp when the container starts |

| Volume mount | Purpose |
|---|---|
| `/downloads` | Target directory for finished downloads |
| `/config` | Persistent state: job queue (`jobs.json`) and the updated yt-dlp binary |

## How it works

- A small Go server (standard library only) shells out to yt-dlp: `-J` to
  probe formats, a worker pool with a machine-readable progress template for
  downloads.
- Jobs are persisted to `/config/jobs.json` on every state change (atomic
  temp-file + rename). After a restart, interrupted jobs are re-queued and
  resume from their `.part` files.
- The UI (Bootstrap 5, vanilla JS, German) polls `/api/jobs` every 1.5 s.
- The Docker image is based on `mikenye/youtube-dl` (ships yt-dlp + ffmpeg),
  built for amd64.

## Development

Requires Go ≥ 1.23; there are no external Go dependencies.

```bash
make check     # gofmt + go vet + tests
make run       # run locally on :8080 (uses ./tmp as volume substitute;
               # real downloads need a yt-dlp binary at /usr/local/bin/yt-dlp)
make image     # build the Docker image (amd64)
```

The design spec and implementation plan live in `docs/superpowers/`.

## License

[PolyForm Noncommercial 1.0.0](LICENSE.md) — you may use, modify and share
this software for any noncommercial purpose; commercial use is not permitted.

Bundled components keep their own licenses: Bootstrap (MIT, vendored),
yt-dlp (Unlicense, pulled at image build/start time).
````

- [ ] **Step 3: Referenzen prüfen**

Verifizieren, dass jede README-Aussage stimmt: genannte Make-Targets existieren im Makefile
(`up`, `down`, `check`, `run`, `image`), Env-Defaults stimmen mit `internal/config/config.go`
überein, Volume-Pfade mit `docker-compose.yml`, Port mit dem Compose-Mapping.
Abweichungen an der README korrigieren (Code bleibt unangetastet).

- [ ] **Step 4: make check**

Run: `make check`
Expected: grün (Docs-only-Änderung, Sicherheitsnetz).

- [ ] **Step 5: Commit**

```bash
git add README.md LICENSE.md
git commit -m "docs: englische GitHub-README und PolyForm-Noncommercial-Lizenz"
```

---

## Abschluss-Verifikation (nach allen Tasks)

- [ ] `make check` — komplett grün
- [ ] `make image` — Image baut
- [ ] Manueller E2E-Test: `make up`, `http://localhost:8080` öffnen, ein Video analysieren, Format wählen, Download starten, Fortschritt beobachten, Datei in `./data/downloads/` prüfen, Container neu starten (`docker compose restart`) und prüfen, dass die Job-Liste erhalten bleibt.
- [ ] `superpowers:requesting-code-review`-Skill für die Gesamtabnahme verwenden

