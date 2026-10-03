# Direkt-Download, Neu wählen, URL kopieren, URL leeren Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development, ein Subagent (Sonnet) je Task. Jeder Subagent liest nur seinen Task-Abschnitt und den Abschnitt „Rahmen für alle Tasks“. Status: Spec vom Nutzer am 2026-10-03 freigegeben (`docs/superpowers/specs/2026-10-03-direkt-download-neu-waehlen-design.md`).

## Goal

Vier Erweiterungen der Bedienung: Direkt-Download mit Profil ohne Analyse-Klick, „Neu wählen“ bei gescheiterten/abgebrochenen Einträgen, URL-Zeile mit Kopier-Button an jedem Eintrag, Button zum Leeren der URL-Zeile.

## Architecture

Neuer Job-Typ `direct`: der Job wird sofort mit `NeedsProbe=true` angelegt, die Queue analysiert vor dem Download (Video: Titel setzen; Playlist: Eintrags-Jobs anlegen, Platzhalter entfernen). Gemeinsame Playlist-Logik liegt im neuen Paket `internal/intake`, das Handler und Queue nutzen. `replace` im Create-Request entfernt den alten Eintrag erst nach erfolgreichem Anlegen. Frontend: Vanilla JS, kein Testrunner.

## Tech Stack

Go 1.23 (`go.mod`), Vanilla JS (`web/static/app.js`), Tailwind v4 (`web/src/input.css` → `web/static/app.css`, per Docker gebaut), `node` v24 lokal vorhanden (`/Users/sh/.nvm/versions/node/v24.21.0/bin/node`).

## Rahmen für alle Tasks

- `make check` (gofmt, vet, `go test ./...`) ist nach jedem Task grün; ein Commit je Task, nur explizit genannte Pfade (`git add <pfade>`, nie `-A`).
- Kommentare im Code und Fehlermeldungen Deutsch, README Englisch.
- Trailer jeder Commit-Message: `Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>`.
- Kein Push, kein Merge. Branch: `feature/direkt-download-neu-waehlen` (vor dem Commit mit `/usr/bin/git rev-parse --abbrev-ref HEAD` prüfen).
- Go-Symbole (Definitionen, Verwender) per LSP (gopls) auflösen, nicht per grep. Falls gopls „no package metadata“ meldet: `go vet ./...` bzw. der Compiler zeigt fehlende Verwender; grep nur als Notbehelf.
- Dateien in Bereichen lesen: `mb-tk go toc <datei>` bzw. `mb-tk md toc <datei>`, dann Read mit offset/limit. `web/static/app.js` (618 Zeilen) und `server_test.go` (1035 Zeilen) nie ganz lesen.
- Zeilenangaben in den Tasks gelten für den Stand vor Task 1; spätere Tasks verschieben sie. Einbaustellen deshalb über den genannten Symbolnamen finden (`mb-tk go toc`), Zeilen nur zur Orientierung.
- Vorhandene Tests dürfen nicht gelöscht oder ersetzt werden; neue Tests werden in den bestehenden Testdateien ergänzt (Funktionen anhängen).
- Nach einem Edit die Datei nicht erneut lesen; das Edit-Ergebnis genügt.
- TDD in Go: erst Test schreiben, `go test` zeigt rot (Kompilierfehler wegen fehlender Symbole zählt als rot), dann Code, dann grün, dann `make check`.
- Frontend-Tasks: kein JS-Testrunner, keiner wird eingeführt. Mindestprüfung `node --check web/static/app.js` plus `make check`.
- CSS-Build (nur Frontend-Tasks, wenn `web/src/input.css`, `web/templates/index.html` oder Klassen in `web/static/app.js` geändert wurden): `make css` im Repo-Root. Das Target startet Docker (`docker run … node:22-alpine`, installiert `@tailwindcss/cli@4.3.3` im Container, Netz beim ersten Lauf nötig, npm-Cache im Volume `ytdlweb-npm-cache`); Docker ist auf diesem Rechner vorhanden (Docker 29.8.1). `web/static/app.css` ist committetes Build-Ergebnis und wird mit committet. Das Dockerfile baut `app.css` im Image selbst neu; die committete Datei zählt für `make run`/`go run` und die Embed-Tests.

## Schnittstellen zwischen den Tasks (Überblick)

| Task | legt an | wird genutzt von |
|---|---|---|
| 1 | `job.Job.Profile string`, `job.Job.NeedsProbe bool` | 2, 3, 4, 6 |
| 2 | Paket `ytdlweb/internal/intake`: `Entry`, `PlaylistFormat`, `IsDuplicate`, `CreatePlaylistJobs` | 3, 4 |
| 3 | API `type:"direct"`, `replace` | 5, 6 |
| 4 | `queue.Prober`, `queue.Option`, `queue.WithProber`, `queue.New(..., opts ...Option)` | main.go |

## Task 1: Job-Felder und Persistenz

**Files:** `internal/job/job.go`, `internal/job/job_test.go`, `internal/store/store_test.go`

**Interface (Struct `Job`, `internal/job/job.go` Z. 27–42, nach `MultiAudio` anhängen):**
```go
Profile    string `json:"profile,omitempty"`
NeedsProbe bool   `json:"needs_probe,omitempty"`
```
`job.New` (Z. 44) behält seine Signatur und setzt die Felder nicht; Aufrufer setzen sie nach `New`. Danach `gofmt -w internal/job/job.go` (Spaltenausrichtung der Tags).

**Steps:**
1. Tests anhängen (rot = Kompilierfehler, Felder fehlen):
   - `internal/job/job_test.go` (Paket `job_test`, importiert bereits `encoding/json`, `strings`, `testing`, `time`, `job`): `TestJobJSONOmitsProfileAndNeedsProbeWhenEmpty` (frischer `job.New(...)`: JSON enthält weder `"profile"` noch `"needs_probe"`); `TestJobJSONRoundTripProfileAndNeedsProbe` (`j.Profile="720p"`, `j.NeedsProbe=true`; JSON enthält `"profile":"720p"` und `"needs_probe":true`; Unmarshal in `job.Job` liefert dieselben Werte).
   - `internal/store/store_test.go` (Paket `store_test`, Hilfsfunktion `openStore(t)` liefert `(*store.Store, pfad)`; Imports `os`, `path/filepath`, `job`, `store` vorhanden): `TestPersistsProfileAndNeedsProbe` (Job mit beiden Feldern per `st.Add`, `store.Open(path)` neu, `Get` liefert beide Werte); `TestOpenOldFileWithoutNewFields` (schreibt per `os.WriteFile` in `filepath.Join(t.TempDir(),"jobs.json")` ein Array mit einem Job-Objekt ohne `profile`/`needs_probe`, z. B. `[{"id":"a1","url":"https://example.com/a","title":"A","format":"ba","format_label":"l","state":"running","progress":{"percent":0,"speed":"","eta":""},"created_at":"2026-10-01T10:00:00Z"}]`; `store.Open` liefert keinen Fehler, `Profile==""`, `NeedsProbe==false`, State `queued` (Crash-Recovery)).
2. `go test ./internal/job/ ./internal/store/` rot, Felder in `job.go` ergänzen, grün.
3. `make check`.
4. Commit: `git add internal/job/job.go internal/job/job_test.go internal/store/store_test.go`, Message `feat: Job-Felder Profile und NeedsProbe` (+ Trailer).

## Task 2: Paket `intake` und Handler-Umbau

**Setzt voraus (Task 1 gemergt):** `job.Job` hat `Profile string` und `NeedsProbe bool`.

**Files:** neu `internal/intake/intake.go`, neu `internal/intake/intake_test.go`; ändern `internal/server/server.go`, `internal/server/server_test.go`.

**Neues Paket `ytdlweb/internal/intake`** (importiert `job`, `store`, `ytdlp`; **nicht** `server`, **nicht** `queue`). Paketkommentar Deutsch: gemeinsame Job-Anlage-Logik für Handler und Queue. Exakte Signaturen:
```go
// Entry ist ein Playlist-Eintrag (URL und Titel).
type Entry struct {
	URL   string
	Title string
}

func PlaylistFormat(profile ytdlp.Profile) (format string, multiAudio bool)
func IsDuplicate(st *store.Store, url, format string) bool
func CreatePlaylistJobs(st *store.Store, profile ytdlp.Profile, playlistTitle string, entries []Entry) (ids []string, skipped int, err error)
```
- `PlaylistFormat`: Inhalt von `playlistFormat` aus `internal/server/server.go` Z. 311–332 **wortgleich** umziehen (samt Kommentarblock, Z. 311–324). In `server.go` entfällt die Funktion.
- `IsDuplicate`: Inhalt von `(*Server).isDuplicate` (Z. 334–342): `st.List()` durchlaufen, Treffer wenn `j.URL == url && j.Format == format` und State `job.StateQueued` oder `job.StateRunning`. In `server.go` entfällt die Methode; Aufrufer rufen `intake.IsDuplicate(s.store, url, format)`.
- `CreatePlaylistJobs`: Schleife aus `createPlaylistJobs` (Z. 289–308) mit `format, multiAudio := PlaylistFormat(profile)`; je Eintrag `url := strings.TrimSpace(e.URL)`; leer oder `IsDuplicate(st, url, format)` → `skipped++; continue`; sonst `j := job.New(url, e.Title, format, profile.Label, playlistTitle)`, `j.MultiAudio = multiAudio`, `j.Profile = profile.Key`, `st.Add(j)`; bei Add-Fehler `return ids, skipped, err` (bereits angelegte ids bleiben in `ids`); sonst `ids = append(ids, j.ID)`. `ids` ist nie `nil` (`ids := []string{}`), damit das JSON `[]` bleibt. Kein `Kick` im Paket (Aufrufer kicken).

**Umbau `internal/server/server.go`:**
1. Import `ytdlweb/internal/intake` ergänzen.
2. `createVideoJob` (Z. 209–277): `isDuplicate` → `intake.IsDuplicate(s.store, url, format)`; im Profil-Zweig (`req.Profile != ""`, Z. 225–249) den Profil-Key merken (`profileKey = profile.Key`, Variable vor dem `if` deklarieren) und nach `job.New` (Z. 268) `j.Profile = profileKey` setzen (bei manuellen Formaten bleibt es leer).
3. `createPlaylistJobs` (Z. 279–309): Validierung (unbekanntes Profil → 400 „unbekanntes Profil“, leere Einträge → 400 „keine Einträge“) bleibt; danach `entries := make([]intake.Entry, len(req.Entries))` aus `req.Entries` (`entryPayload` hat dieselben Felder `URL`, `Title`; Typkonvertierung `intake.Entry(e)` ist erlaubt), `ids, skipped, err := intake.CreatePlaylistJobs(s.store, profile, req.PlaylistTitle, entries)`; bei `err != nil` erst `s.queue.Kick()`, dann `writeError(w, 500, err.Error())`; sonst `s.queue.Kick()` und `writeJSON(w, 201, map[string]any{"ids": ids, "skipped": skipped})`. Antwortformat unverändert.
4. `playlistFormat` und `isDuplicate` aus `server.go` löschen. Verwender dieser Symbole laut Stand: nur `server.go` selbst (Z. 264, 289, 294); `server_test.go` erwähnt `playlistFormat` nur in einem Kommentar (Z. 565).

**Steps:**
1. Tests zuerst:
   - `internal/intake/intake_test.go` (Paket `intake_test`, Store per `store.Open(filepath.Join(t.TempDir(),"jobs.json"))`): `TestPlaylistFormatAudioProfile` (Profil `audio` via `ytdlp.ProfileByKey("audio")`: Format == `profile.Expr`, `multiAudio == false`); `TestPlaylistFormatVideoProfile` (`best`: `multiAudio == true`, Format == `VideoExpr+"+ba[language^=de]+ba[format_note*=original][language!^=de]/"+VideoExpr+"+ba[language^=de]/"+Expr`); `TestIsDuplicate` (Job queued → true; running → true; done/error → false; anderes Format → false; andere URL → false); `TestCreatePlaylistJobs` (2 gültige Einträge, 1 mit leerer URL, 1 Duplikat eines vorhandenen queued-Jobs gleicher URL und `PlaylistFormat`-Format: `len(ids)==2`, `skipped==2`; jeder neue Job: State queued, `Title`, `PlaylistTitle`, `Format`, `FormatLabel == profile.Label`, `MultiAudio`, `Profile == profile.Key`, `NeedsProbe == false`).
   - `internal/server/server_test.go` anhängen (Helfer `newServer(t, fakeProber{})` → `(h, st, set)`, `do(t, h, method, path, body)` → `*httptest.ResponseRecorder`, `videoProbe()` vorhanden): `TestCreateVideoJobWithProfileStoresProfile` (POST `{"type":"video","url":"https://example.com/a","profile":"720p"}` → 201, `st.List()[0].Profile == "720p"`); `TestCreateVideoJobManualHasNoProfile` (`audio_only` ohne Profil → `Profile == ""`); `TestCreatePlaylistJobsStoresProfile` (POST playlist mit `profile:"best"` und 2 Einträgen → beide Jobs `Profile == "best"`).
2. `go test ./internal/intake/ ./internal/server/` rot (Paket fehlt; neue Server-Tests rot).
3. Paket anlegen, Server umbauen. Alle **bestehenden** Tests in `server_test.go` bleiben unverändert und müssen grün sein.
4. `make check`.
5. Commit: `git add internal/intake internal/server/server.go internal/server/server_test.go`, Message `refactor: Playlist-Logik in Paket intake, Profile an Jobs speichern` (+ Trailer).

## Task 3: API `direct` und `replace`

**Setzt voraus (Task 1 und 2 gemergt):** `job.Job.Profile`, `job.Job.NeedsProbe`; `intake.PlaylistFormat(profile ytdlp.Profile) (format string, multiAudio bool)`, `intake.IsDuplicate(st *store.Store, url, format string) bool`, `intake.CreatePlaylistJobs(...)`; `server.go` ruft diese bereits.

**Files:** `internal/server/server.go`, `internal/server/server_test.go`.

**Verhalten (Spec A „API“ und B „Server“):**
1. Request `createJobsRequest` (Z. 179–191) bekommt `Replace string` (`json:"replace"`).
2. `handleCreateJobs` (Z. 193–207) bekommt den Zweig `case "direct"`; Default-Meldung des 400 wird `type muss video, playlist oder direct sein` (kein bestehender Test prüft den Text, per `grep -n 'oder playlist' internal/server/server_test.go` bestätigen).
3. `direct`: `url` getrimmt, leer → 400 `url fehlt`; `profile` leer oder unbekannt (`ytdlp.ProfileByKey`) → 400 `unbekanntes Profil`; `format, multi := intake.PlaylistFormat(profile)`; Duplikat (`intake.IsDuplicate`) → 409 `Dieser Download läuft bereits`; sonst `j := job.New(url, "", format, profile.Label, playlistTitle)`, `j.MultiAudio = multi`, `j.Profile = profile.Key`, `j.NeedsProbe = true`, `s.store.Add(j)` (Fehler → 500), `s.queue.Kick()`, Antwort 201 `{"ids":[id]}`.
4. `replace` (alle Typen): ein Job gilt als ersetzbar, wenn die ID existiert und State `error` oder `canceled` ist. Der Job wird erst nach erfolgreichem Anlegen mindestens eines Jobs entfernt (Status 201 und `len(ids) > 0`); unbekannte ID oder State queued/running/done → `replace` stillschweigend ignoriert; Scheitern des Anlegens (400/409/500) lässt den alten Job unverändert. Vor dem Entfernen den Job erneut per `s.store.Get` lesen und State erneut prüfen (er kann zwischenzeitlich per Retry wieder `queued` sein); Fehler von `s.store.Remove` nur per `log.Printf("server: ersetzter Job %s nicht entfernt: %v", id, err)` melden, Antwort bleibt 201.
5. `PlaylistTitle`-Übernahme: Bei `direct` und `video` gilt `playlistTitle = req.PlaylistTitle`; ist er leer und der ersetzbare Job hat einen `PlaylistTitle`, wird dieser übernommen. (Achtung: `createVideoJob` übergibt heute `""` an `job.New` und ignoriert `req.PlaylistTitle`; neu wird `playlistTitle` übergeben.) Bei `playlist` ändert sich nichts (jeder Eintrag bekommt `req.PlaylistTitle`).

**Umsetzungsgerüst (damit alle Zweige dieselbe Stelle für `replace` nutzen):** die drei `create*`-Funktionen schreiben nicht mehr selbst die Antwort, sondern liefern
```go
type createOutcome struct {
	status int            // 201 bei Erfolg, sonst Fehlercode
	errMsg string         // Meldung bei status != 201
	ids    []string       // angelegte Job-IDs
	body   map[string]any // JSON-Antwort bei 201
}
```
Signaturen neu: `createVideoJob(req createJobsRequest, playlistTitle string) createOutcome`, `createDirectJob(req createJobsRequest, playlistTitle string) createOutcome`, `createPlaylistJobs(req createJobsRequest) createOutcome`. `handleCreateJobs` ermittelt vorher `replaced, canReplace := s.replaceable(req.Replace)` (neue Methode `func (s *Server) replaceable(id string) (job.Job, bool)`), bestimmt `playlistTitle`, ruft die Funktion, schreibt bei `status != 201` `writeError(w, out.status, out.errMsg)`, sonst `removeReplaced` (neue Methode `func (s *Server) removeReplaced(id string)`, siehe Punkt 4) und `writeJSON(w, 201, out.body)`. Die Antwortkörper bleiben byte-identisch zu heute (`{"ids":[…]}` bzw. `{"ids":[…],"skipped":n}`); bestehende Tests bleiben unverändert grün.

**Steps:**
1. Tests anhängen (Helfer wie in Task 2; Jobs mit Zustand per `st.Add(j)` und `st.Update(j.ID, func(x *job.Job){ x.State = job.StateError })` vorbereiten; die Queue ist im Test nicht gestartet, Jobs bleiben `queued`):
   - `TestCreateDirectJob`: POST `{"type":"direct","url":"https://example.com/v","profile":"best"}` → 201, `ids` Länge 1; Job: `Title==""`, `NeedsProbe`, `Profile=="best"`, `Format`/`MultiAudio` == `intake.PlaylistFormat(profile)`, `FormatLabel == profile.Label`, State queued. `GET /api/jobs` enthält `"needs_probe":true` und `"profile":"best"`.
   - `TestCreateDirectJobAudioProfile`: Profil `audio` → `Format == profile.Expr`, `MultiAudio == false`.
   - `TestCreateDirectJobValidation` (Tabelle): ohne `url` → 400; `url` nur Leerzeichen → 400; ohne `profile` → 400; `profile:"gibtsnicht"` → 400.
   - `TestCreateDirectJobDuplicate`: zweiter identischer POST → 409.
   - `TestReplaceRemovesErrorAndCanceledJob`: alter Job (State `error`, dann Wiederholung mit `canceled`), POST direct mit `replace:<alt-id>` und anderer URL → 201, `st.Get(alt)` nicht mehr vorhanden, neuer Job vorhanden.
   - `TestReplaceIgnoredWhenNotReplaceable`: Tabelle: Job queued, running, done, unbekannte ID `"gibtsnicht"`: Antwort 201, der alte Job existiert weiter, der neue Job ist angelegt.
   - `TestReplaceKeepsOldJobOnFailure`: alter Job `error`; (a) POST direct ohne `profile` + `replace` → 400; (b) POST direct, dessen URL+Format ein anderer queued-Job schon hat → 409; in beiden Fällen existiert der alte Job weiter.
   - `TestReplaceAdoptsPlaylistTitle`: alter Job `error` mit `PlaylistTitle:"Meine Liste"`; direct mit `replace` → neuer Job `PlaylistTitle=="Meine Liste"`; dasselbe mit `type:"video"` (`profile:"best"`); mit gesetztem `playlist_title:"Andere"` im Request gewinnt der Request-Wert.
   - `TestReplaceWorksForVideoAndPlaylist`: `type:"video"` mit `replace` entfernt den Job; `type:"playlist"` (2 Einträge) mit `replace` entfernt ihn; `type:"playlist"` mit ausschließlich leeren/dupliziertem Einträgen (`ids` leer, 201) lässt den alten Job bestehen.
2. `go test ./internal/server/` rot.
3. Implementieren (Gerüst oben). 
4. `go test ./internal/server/` grün, `make check`.
5. Commit: `git add internal/server/server.go internal/server/server_test.go`, Message `feat: API type direct und replace beim Anlegen von Jobs` (+ Trailer).

## Task 4: Queue-Analyse-Vorschritt

**Setzt voraus (Task 1–3 gemergt):** `job.Job.Profile`, `job.Job.NeedsProbe`; `ytdlweb/internal/intake` mit `intake.Entry{URL, Title string}`, `intake.CreatePlaylistJobs(st *store.Store, profile ytdlp.Profile, playlistTitle string, entries []intake.Entry) (ids []string, skipped int, err error)`, `intake.PlaylistFormat(profile ytdlp.Profile) (string, bool)`.

**Files:** `internal/queue/queue.go`, `internal/queue/queue_test.go`, `cmd/server/main.go`.

**Interface in `queue.go`:**
```go
// Prober analysiert eine URL; passt zu (*ytdlp.Prober).Probe.
type Prober interface {
	Probe(ctx context.Context, rawURL string) (*ytdlp.ProbeResult, error)
}

// Option konfiguriert die Queue optional.
type Option func(*Queue)

func WithProber(p Prober) Option

func New(st *store.Store, r Runner, maxConcurrent int, opts ...Option) *Queue
```
Struct `Queue` (Z. 19–27) bekommt das Feld `prober Prober`. Alle bestehenden Aufrufer (`queue.New(st, r, n)` in `cmd/server/main.go:77`, `server_test.go:63` und `:877`, `queue_test.go:40,154,191,224,258`) kompilieren unverändert. Konstante `probeTimeout = 60 * time.Second`. Import `errors`, `fmt`, `ytdlweb/internal/intake` ergänzen (`queue` importiert `server` nicht).

**Verhalten in `runJob` (Z. 87–125):** Der `defer` bleibt. Die Zeile `err := q.runnerFor(j).Run(...)` (Z. 96) wird ersetzt durch:
```go
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
```
Der folgende `switch` bleibt unverändert, damit Analyse-Fehler → `error`, Abbruch (ctx-Fehler) → `canceled` und der Shutdown-Sonderfall (Z. 105–110: bei beendetem Root-Context bleibt der Zustand `running`) auch für die Analyse gelten.

**Wie der Platzhalter-Durchlauf endet (entschieden):** `store.Update` auf eine entfernte ID liefert den Fehler `job … nicht gefunden` (`internal/store/store.go` Z. 75–85), und `store.SetProgress` ignoriert unbekannte IDs lautlos. Deshalb darf `runJob` nach `store.Remove` keinen Update mehr aufrufen, sonst entstünde pro Playlist eine Log-Zeile „Zustand nicht persistiert“. `analyze` meldet das Ende per Sentinel `var errPlaceholderReplaced = errors.New("platzhalter durch playlist-einträge ersetzt")`; `runJob` kehrt dafür sofort zurück (nur der `defer` läuft: `cancel()`, `q.cancels`-Eintrag löschen, Semaphor freigeben, `Kick()`).

**`analyze` (neue Methode, `func (q *Queue) analyze(ctx context.Context, j *job.Job) error`):**
1. `q.prober == nil` → `errors.New("Analyse nicht verfügbar")`.
2. `pctx, cancel := context.WithTimeout(ctx, probeTimeout)`, `defer cancel()`; `res, err := q.prober.Probe(pctx, j.URL)`.
3. `err != nil`: war `ctx.Err() == nil` und `errors.Is(pctx.Err(), context.DeadlineExceeded)`, Fehler `fmt.Errorf("Analyse: Zeitlimit von %s überschritten", probeTimeout)`; sonst `err` unverändert zurück (bei Nutzer-Abbruch/Shutdown trägt `ctx.Err()` im `switch` die Zuordnung).
4. `res.Type == "video"` und `res.Video != nil`: `q.store.Update(j.ID, func(x *job.Job){ x.Title = res.Video.Title; x.NeedsProbe = false })` (Fehler zurückgeben); danach lokal `j.Title`, `j.NeedsProbe` setzen (der Runner bekommt die aktualisierte Kopie); `return nil`.
5. `res.Type == "playlist"` und `res.Playlist != nil`:
   - `len(Entries) == 0` → `errors.New("Playlist enthält keine Einträge")`.
   - `profile, ok := ytdlp.ProfileByKey(j.Profile)`; `!ok` → `errors.New("unbekanntes Profil")`.
   - Einträge zu `[]intake.Entry` (`intake.Entry{URL: e.URL, Title: e.Title}`), `intake.CreatePlaylistJobs(q.store, profile, res.Playlist.Title, entries)`; Fehler zurückgeben (Job wird `error`, Platzhalter bleibt, `NeedsProbe` bleibt; ein Retry analysiert neu, Duplikate werden übersprungen).
   - **Erst danach** `q.store.Remove(j.ID)`; Fehler zurückgeben; Erfolg → `return errPlaceholderReplaced`. Nur Duplikate (alle Einträge übersprungen) entfernt den Platzhalter trotzdem.
6. Sonstiges Ergebnis (`res == nil` oder unbekannter Typ) → `errors.New("Analyse lieferte kein Ergebnis")`.
In allen Fehlerfällen bleibt `NeedsProbe == true` (kein Update dafür).

**Verdrahtung `cmd/server/main.go`:** vor `queue.New` (Z. 77) `prober := &ytdlp.Prober{Bin: bin}` anlegen; `q := queue.New(st, runner, cfg.MaxConcurrent, queue.WithProber(prober))`; in `server.New(...)` (Z. 86) statt `&ytdlp.Prober{Bin: bin}` die Variable `prober` übergeben.

**Test-Muster (`internal/queue/queue_test.go`, Paket `queue_test`; Imports `intake`, `sync`, `ytdlp` ergänzen, `ytdlp`/`job`/`store`/`queue` sind vorhanden):** Vorhanden sind `fakeRunner{started chan string, release chan struct{}, fail error}` (blockiert bis `release` geschlossen oder ctx beendet), `newQueue(t, fr, n)`, `addJob(t, st, i)`, `waitState(t, st, id, want)` (pollt bis 3 s). Nicht ändern. Neu anhängen:
```go
// fakeProber zählt Aufrufe und kann blockieren oder scheitern.
type fakeProber struct {
	mu     sync.Mutex
	res    *ytdlp.ProbeResult
	err    error
	calls  int
	block  chan struct{} // gesetzt: Probe wartet auf close oder ctx.Done
	called chan struct{} // gesetzt: jeder Aufruf meldet sich (gepuffert, Kapazität >= 4)
}
func (f *fakeProber) Probe(ctx context.Context, url string) (*ytdlp.ProbeResult, error) {
	f.mu.Lock(); f.calls++; res, err := f.res, f.err; f.mu.Unlock()
	if f.called != nil { f.called <- struct{}{} }
	if f.block != nil {
		select { case <-f.block: case <-ctx.Done(): return nil, ctx.Err() }
	}
	return res, err
}
func (f *fakeProber) set(res *ytdlp.ProbeResult, err error) { /* unter mu setzen */ }
func (f *fakeProber) count() int { /* calls unter mu lesen */ }

// recordingRunner meldet erfolgreich zurück und merkt sich alle gelaufenen Job-IDs.
type recordingRunner struct { mu sync.Mutex; ids []string }
func (r *recordingRunner) Run(_ context.Context, j job.Job, _ func(job.Progress)) error { /* ID anhängen, nil */ }
func (r *recordingRunner) ran(id string) bool { /* unter mu prüfen */ }

func newQueueProber(t *testing.T, r queue.Runner, p queue.Prober) (*queue.Queue, *store.Store)
// wie newQueue, aber queue.New(st, r, 1, queue.WithProber(p)) (p == nil: ohne Option).
func addProbeJob(t *testing.T, st *store.Store, url, profileKey string) job.Job
// profile, _ := ytdlp.ProfileByKey(profileKey); format, multi := intake.PlaylistFormat(profile);
// j := job.New(url, "", format, profile.Label, ""); j.MultiAudio = multi; j.Profile = profile.Key;
// j.NeedsProbe = true; per st.Add anlegen und j zurückgeben.
```
Hilfsfunktion `waitGone(t, st, id)` (pollt bis `st.Get(id)` nicht mehr ok, 3 s). Videoergebnis für Tests: `&ytdlp.ProbeResult{Type:"video", Video:&ytdlp.Video{ID:"x", Title:"Probe-Titel"}}`; Playlist: `&ytdlp.ProbeResult{Type:"playlist", Playlist:&ytdlp.Playlist{Title:"PL", Entries:[]ytdlp.PlaylistEntry{{URL:"https://example.com/e1", Title:"E1"}, {URL:"https://example.com/e2", Title:"E2"}}}}`. Wartepunkte immer mit Timeout (`select` mit `time.After(3*time.Second)`), keine festen Sleeps außer „es darf nichts passieren“ (200 ms).

**Tests (alle einzeln benannt):**
- `TestQueueProbeVideoSetsTitleAndRuns`: Job mit NeedsProbe (Profil `best`), Prober liefert Video, `recordingRunner`: Zustand `done`; `Title == "Probe-Titel"`, `NeedsProbe == false`; `ran(id)`; `count() == 1`.
- `TestQueuePlaylistCreatesEntryJobsAndRemovesPlaceholder`: Prober liefert Playlist mit 2 Einträgen; Platzhalter verschwindet (`waitGone`); in `st.List()` zwei Jobs mit den Eintrags-URLs, `PlaylistTitle == "PL"`, `Format` == `intake.PlaylistFormat(profile)`, `Profile == "best"`, `Title` == Eintragstitel; beide erreichen `done`; `ran(platzhalterID) == false`; im Log keine Pflicht.
- `TestQueuePlaylistSkipsDuplicates`: vorab ein Job mit `State: job.StateRunning` (direkt per `st.Add` mit gesetztem State, wird nie dispatcht) mit URL von Eintrag 1 und dem `intake.PlaylistFormat`-Format; nach dem Lauf existiert nur ein neuer Job (Eintrag 2), Platzhalter weg.
- `TestQueueEmptyPlaylistFails`: Playlist ohne Einträge → Platzhalter `error`, `Error == "Playlist enthält keine Einträge"`, `NeedsProbe` bleibt true, Runner lief nicht.
- `TestQueueProbeErrorKeepsNeedsProbeAndRetryProbesAgain`: Prober liefert Fehler `errors.New("kaputt")` → `error`, `Error == "kaputt"`, `NeedsProbe == true`, Runner lief nicht, `count()==1`; dann `fp.set(videoResult, nil)`, `st.Update(id, func(x){ x.State = job.StateQueued; x.Error = ""; x.FinishedAt = nil })`, `q.Kick()` → `done`, `count()==2`, Titel gesetzt.
- `TestQueueCancelDuringProbe`: Prober mit `block` und `called`; nach `<-called` `q.Cancel(id)` → `canceled`, `FinishedAt != nil`, `NeedsProbe` bleibt true, Runner lief nicht.
- `TestQueueSkipsProbeWithoutNeedsProbe`: normaler Job (`addJob`) mit Prober → `done`, `count() == 0`.
- `TestQueueNeedsProbeWithoutProberFails`: Queue ohne `WithProber`, Job NeedsProbe → `error`, `Error == "Analyse nicht verfügbar"`, Runner lief nicht.
- `TestQueueShutdownDuringProbeKeepsRunning`: wie `TestQueueShutdownKeepsRunningState` (Z. 148–169, eigener Root-Context mit `cancel()`), aber Prober blockiert; nach `cancel()` bleibt der Zustand `running`.

**Steps:**
1. Tests schreiben (rot: `queue.WithProber` fehlt).
2. `go test ./internal/queue/` rot; implementieren; grün.
3. `main.go` verdrahten; `go build ./...`, `make check`.
4. Commit: `git add internal/queue/queue.go internal/queue/queue_test.go cmd/server/main.go`, Message `feat: Queue analysiert Direkt-Download-Jobs vor dem Download` (+ Trailer).

## Task 5: Frontend URL-Karte

**Setzt voraus (Task 3 gemergt):** `POST /api/jobs` akzeptiert `{type:"direct", url, profile, replace?}` (201 `{ids:[id]}`) und `replace` bei `video`/`playlist`.

**Files:** `web/templates/index.html`, `web/static/app.js`, `web/src/input.css`, `web/static/app.css` (gebaut), `internal/server/server_test.go`.

**Template `web/templates/index.html`, URL-Karte Z. 76–83** (ersetzen; `#url-input`, `#probe-btn`, `#probe-error` behalten ihre IDs):
```html
<section class="card mb-4">
  <label class="field-label" for="url-input">Video- oder Playlist-URL</label>
  <div class="url-field">
    <input type="url" class="input" id="url-input" placeholder="https://…" autofocus>
    <button type="button" class="url-clear" id="url-clear" aria-label="URL leeren" hidden>
      <svg viewBox="0 0 16 16" width="14" height="14" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" aria-hidden="true"><path d="M4 4l8 8M12 4l-8 8"/></svg>
    </button>
  </div>
  <div class="mt-2 flex flex-wrap items-center gap-2">
    <button class="btn btn-primary" id="probe-btn">Analysieren</button>
    <select class="select w-auto" id="direct-profile" aria-label="Profil für Direkt-Download">
      {{range .Profiles}}<option value="{{.Key}}">{{.Label}}</option>
      {{end}}</select>
    <button type="button" class="btn btn-ghost" id="direct-btn">Download</button>
  </div>
  <div class="replace-hint" id="replace-hint" hidden>
    <span class="min-w-0 truncate" id="replace-hint-text"></span>
    <button type="button" class="icon-btn" id="replace-hint-clear" aria-label="Verknüpfung lösen">
      <svg viewBox="0 0 16 16" width="14" height="14" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" aria-hidden="true"><path d="M4 4l8 8M12 4l-8 8"/></svg>
    </button>
  </div>
  <div class="mt-3 text-sm text-danger" id="probe-error" hidden></div>
</section>
```
Der `{{range .Profiles}}`-Block ist dasselbe Muster wie bei `#default-profile` (Z. 47–50). Die schmale Anordnung ergibt sich aus Flex-Wrap: das Eingabefeld hat volle Breite, Buttons und Select umbrechen darunter.

**CSS `web/src/input.css`**, im `@layer components`-Block (Z. 153 ff., nach `.btn-sm` Z. 205–207) ergänzen:
- `.url-field { @apply relative; }` und `.url-field .input { @apply pr-10; }` (Platz für das ✕).
- `.url-clear { @apply absolute right-1 top-1/2 inline-flex size-8 -translate-y-1/2 items-center justify-center rounded-md text-dim; }` plus `.url-clear:hover { @apply text-text; }`; auf Touch-Geräten (`@media (pointer: coarse)`) `width: 44px; height: 44px;` und `right: 0` (Touch-Ziel mindestens 44×44 px; das Eingabefeld hat dort ca. 38 px Höhe, `top-1/2`-Zentrierung gleicht aus).
- `.icon-btn { @apply inline-flex size-8 shrink-0 items-center justify-center rounded-md text-dim; }`, `.icon-btn:hover { @apply text-text; }`, `@media (pointer: coarse) { .icon-btn { width: 44px; height: 44px; } }` (wird in Task 6 auch für das Kopier-Icon genutzt).
- `.replace-hint { @apply mt-3 flex items-center gap-2 rounded-lg border border-edge-strong bg-bg px-3 py-1.5 text-xs text-muted; }`.
Alle `@apply`-Tokens (`text-dim`, `text-text`, `border-edge-strong`, `bg-bg`, `text-muted`) existieren im Bestand (siehe `.input`, `.field-label`).

**`web/static/app.js`**, Funktionen und Einbaustellen (Zeilen vor Task 5):
1. Neuer Zustand nach `let probeResult = null;` (Z. 5): `let replaceLink = null; // { id, url, label } oder null`.
2. Neuer Abschnitt „URL-Feld“ direkt vor „Analyse“ (vor Z. 251) mit:
   - `function setUrl(value)`: setzt `$("url-input").value = value` und ruft `refreshUrlUi()`. **Einziger** Weg, den Wert programmatisch zu ändern.
   - `function refreshUrlUi()`: `$("url-clear").hidden = $("url-input").value === ""`; ist `replaceLink` gesetzt und `$("url-input").value.trim() !== replaceLink.url`, `clearReplaceLink()`.
   - `function setReplaceLink(link)` (setzt `replaceLink = link`, zeigt `#replace-hint`, `#replace-hint-text`.textContent = `Ersetzt Eintrag: ${link.label}`), `function clearReplaceLink()` (setzt `null`, versteckt den Hinweis). Der Aufrufer in Task 6 ruft `setUrl(job.url)` **vor** `setReplaceLink(...)`.
   - `function closeSelection()`: `probeResult = null; hide($("select-card")); hide($("start-error"));`.
   - Listener: `$("url-input").addEventListener("input", refreshUrlUi);` `$("replace-hint-clear").addEventListener("click", clearReplaceLink);` `$("url-clear").addEventListener("click", () => { setUrl(""); closeSelection(); clearReplaceLink(); $("url-input").focus(); });`
3. Alle drei bisherigen Zuweisungen von `$("url-input").value` auf `setUrl(...)` umstellen: in `start()` Z. 420 (`= ""`) und Z. 459 (`= ""`), in `handleSharedUrl` Z. 605 (`= shared`). Lesende Zugriffe bleiben.
4. `probe()` (Z. 259–279): `probe-btn` **und** `direct-btn` während des Requests `disabled`, im `finally` wieder aktiv; nach dem `await api(...)` verwerfen, wenn `$("url-input").value.trim() !== url` (Feld wurde zwischenzeitlich geändert/geleert): `return null`, ohne `probeResult` zu setzen oder `renderSelectCard()` aufzurufen.
5. `start()` (Z. 406–467): Bei `replaceLink && $("url-input").value.trim() === replaceLink.url` wird `replace: replaceLink.id` in den Request-Body mitgesendet (Playlist-Zweig Z. 412–417 und beide Video-Payloads Z. 430 bzw. 447). Nach erfolgreichem Start `clearReplaceLink()` (zusammen mit `setUrl("")`, vor `refreshJobs()`).
6. Neue Funktion `async function startDirect()` + `$("direct-btn").addEventListener("click", startDirect);`: (a) `url = $("url-input").value.trim()`; leer → `return`; (b) `hide($("probe-error"))`, beide Buttons `disabled`; (c) `body = { type: "direct", url, profile: $("direct-profile").value }`, plus `replace: replaceLink.id`, wenn `replaceLink && replaceLink.url === url`; (d) `await api("/api/jobs", { method: "POST", body: JSON.stringify(body) })`; bei Erfolg `setUrl("")`, `closeSelection()`, `clearReplaceLink()`, `await refreshJobs()`, `toast("Download hinzugefügt")`; (e) bei Fehler `$("probe-error").textContent = err.message; show($("probe-error"))`; (f) `finally`: Buttons wieder aktiv.
7. `#direct-profile`-Vorbelegung: in `loadSettings()` (Z. 132–141) nach `$("default-profile").value = …` auch `$("direct-profile").value = currentSettings.default_profile`; im `change`-Handler von `#default-profile` (Z. 240–249) bei Änderung `$("direct-profile").value = $("default-profile").value`, im Fehlerzweig (Rollback Z. 247) ebenso `$("direct-profile").value = currentSettings.default_profile`.
8. `handleSharedUrl()` bleibt ansonsten unverändert (Autostart läuft über die Analyse).

**Verhaltensaussagen (jede im Browser prüfbar, siehe Task 8):**
1. `#url-clear` ist sichtbar genau dann, wenn das Feld nicht leer ist, auch nach programmatischem Setzen (geteilte URL, Neu wählen).
2. Klick auf `#url-clear` leert das Feld, setzt den Fokus ins Feld, schließt `#select-card`, verwirft das Analyse-Ergebnis, löst die Verknüpfung.
3. Ändern des Feldtexts (getrimmt ungleich der Verknüpfungs-URL) oder Leeren löst die Verknüpfung, `#replace-hint` verschwindet; das ✕ im Hinweis löst sie ebenfalls.
4. Bei bestehender Verknüpfung senden `startDirect()` und `start()` `replace`; ohne Verknüpfung kein `replace`-Feld.
5. Direkt-Download (201): Feld leer, Auswahlkarte zu, Toast „Download hinzugefügt“, Liste aktualisiert. Fehler: Text in `#probe-error`, beide Buttons wieder aktiv.
6. `#direct-profile` startet mit dem Standardprofil und folgt Änderungen des Standardprofils.
7. Bei schmaler Breite (390 px) liegt das Feld über volle Breite, Analysieren/Select/Download umbrechen darunter; Touch-Ziele von `#url-clear` und Hinweis-✕ sind ≥ 44×44 px (nur bei `pointer: coarse`).

**Template-Test** (TDD, `internal/server/server_test.go` anhängen): `TestIndexRendersDirectDownloadUI`: `GET /` (Helfer `newServer`, `do`) enthält `id="direct-profile"`, `id="direct-btn"`, `id="url-clear"`, `id="replace-hint"`; zwischen `id="direct-profile"` und dem nächsten `</select>` steht `value="1080p-mp4"`.

**Steps:**
1. Template-Test schreiben, `go test ./internal/server/ -run TestIndexRendersDirectDownloadUI` rot.
2. `index.html` ändern → Test grün.
3. `app.js` und `input.css` ändern.
4. `command -v node` (vorhanden: `/Users/sh/.nvm/versions/node/v24.21.0/bin/node`), `node --check web/static/app.js`.
5. CSS bauen: `make css` (Docker, siehe Rahmen); `git status` zeigt `web/static/app.css` geändert.
6. `make check`.
7. Commit: `git add web/templates/index.html web/static/app.js web/src/input.css web/static/app.css internal/server/server_test.go`, Message `feat: URL-Karte mit Direkt-Download, Leeren-Button und Ersetzt-Hinweis` (+ Trailer).

## Task 6: Frontend Job-Liste

**Setzt voraus (Task 5 gemergt):** in `web/static/app.js` existieren `let replaceLink`, `setUrl(value)`, `setReplaceLink({id, url, label})`, `clearReplaceLink()`, `closeSelection()`; im Template `#direct-profile`, `#url-input`, `#replace-hint`; in `web/src/input.css` die Klasse `.icon-btn`. Der Server liefert Jobs mit `profile` und `needs_probe` (Task 1–3).

**Files:** `web/static/app.js`, `web/src/input.css`, `web/static/app.css` (gebaut).

**Einbaustellen (`app.js`, Zeilen vor Task 5; per Symbol finden):** `STATE_PILLS` (Z. 480), `refreshJobs` (488), `renderJobs` (497–541), `actionButtons` (543–555), Click-Handler `$("jobs-list").addEventListener("click", …)` (557–581).

**Änderungen:**
1. **`copyText(text)`** (neu, vor „Jobs-Tabelle“): `async function copyText(text)` gibt `true`/`false` zurück. Ist `window.isSecureContext && navigator.clipboard`, `await navigator.clipboard.writeText(text)` in try/catch (Fehler → weiter zum Fallback). Sonst/danach synchroner Fallback `legacyCopy(text)`: `<textarea>` mit `readOnly = true`, `value = text`, Stil `position:fixed; top:0; left:0; opacity:0; font-size:16px` (16 px gegen iOS-Zoom), `document.body.append(ta)`, `ta.focus()`, `ta.select()`, `ta.setSelectionRange(0, text.length)`, `ok = document.execCommand("copy")` in try/catch (Wurf → `false`), `ta.remove()`, `return ok`. Im Nicht-Secure-Context darf **kein** `await` vor `legacyCopy` stehen (Nutzergeste bleibt erhalten).
2. **Dateiname kopieren** (Handler `[data-action="copy"]`, Z. 558–567): `const ok = await copyText(copyEl.dataset.filename); toast(ok ? "Dateiname kopiert" : "Kopieren nicht möglich", ok ? "ok" : "error");` statt `navigator.clipboard`.
3. **`cardHtml(j)`**: den Inhalt des `map`-Callbacks aus `renderJobs` in `function cardHtml(j)` auslagern, mit diesen Änderungen:
   - Titel: `j.title ? esc(j.title) : '<span class="font-normal text-dim">Ohne Titel</span>'` (nicht mehr `j.url`); `playlist_title`-Anhang wie bisher.
   - Pill: `j.state === "running" && j.needs_probe` → `["pill pill-run", "Wird analysiert"]`; in diesem Fall keine Fortschrittswerte (`speed`, `eta`, `${pct} %` entfallen in `metaParts`) und kein Fortschrittsbalken; Format-Label, „hinzugefügt“ bleiben.
   - Kartenwurzel bekommt `data-job-id="${esc(j.id)}"`.
   - Neue URL-Zeile `urlRow(j)` direkt nach der Meta-Zeile (vor `extra`): `<div class="col-span-full flex min-w-0 items-center gap-1.5">` mit (a) dem URL-Element und (b) `<button type="button" class="icon-btn" data-action="copy-url" data-url="${esc(j.url)}" aria-label="URL kopieren">` mit einem Kopier-SVG (z. B. zwei überlappende Rechtecke, `viewBox="0 0 16 16"`, `aria-hidden="true"`). URL-Element: beginnt `j.url` mit `http://` oder `https://` (`/^https?:\/\//i`), dann `<a class="min-w-0 flex-1 truncate text-xs text-muted hover:text-text" href="${esc(j.url)}" target="_blank" rel="noopener noreferrer" title="${esc(j.url)}">${esc(j.url)}</a>`, sonst `<span class="min-w-0 flex-1 truncate text-xs text-muted" title="${esc(j.url)}">${esc(j.url)}</span>`. Die URL wird in Text und Attributen ausschließlich per `esc()` eingesetzt.
4. **`actionButtons(j)`**: bei `error`/`canceled` die Reihenfolge `Erneut`, `Neu wählen` (`data-action="reselect"`, Label „Neu wählen“), `Entfernen`. Bestehende Buttons und ihr Markup bleiben unverändert.
5. **Klick-Handler**: Reihenfolge der Zweige: (1) `[data-action="copy"]` (Dateiname, siehe 2), (2) neu `[data-action="copy-url"]`: `const ok = await copyText(el.dataset.url)` → Toast „URL kopiert“ bzw. „Kopieren nicht möglich“ (error), `return`; (3) neu `button[data-action="reselect"]`: Job aus `lastJobs` (siehe 6) per `data-id` holen, fehlt er → `return`; dann `setUrl(job.url)`; hat `job.profile` und existiert eine `<option value=job.profile>` in `#direct-profile`, `$("direct-profile").value = job.profile`; `closeSelection()`; `setReplaceLink({ id: job.id, url: job.url, label: job.title || job.url })`; `$("url-input").closest("section").scrollIntoView({ behavior: "smooth", block: "start" })`; **kein** `focus()` (iOS-Tastatur); `return`. (4) bisheriger Pfad für `cancel`/`retry`/`delete`.
6. **Karten-weises Rendern** (`renderJobs`, Vorgabe: Klicks dürfen beim 1,5-s-Polling nicht verloren gehen): Modulzustand `let lastJobs = []; let renderedIds = []; let renderedHtml = new Map();`. `renderJobs(jobs)`: `lastJobs = jobs`; `$("jobs-empty").hidden = jobs.length > 0`; `const ids = jobs.map(j => j.id)`; `const htmls = jobs.map(cardHtml)`. Ist `ids` elementweise gleich `renderedIds` und gleich lang: für jede Position `i`, an der `htmls[i] !== renderedHtml.get(ids[i])`, die Karte ersetzen (`const t = document.createElement("template"); t.innerHTML = htmls[i].trim(); list.replaceChild(t.content.firstElementChild, list.children[i])`). Sonst: `list.innerHTML = htmls.join("")` wie bisher. Danach `renderedIds = ids`, `renderedHtml = new Map(ids.map((id, i) => [id, htmls[i]]))`. Die Zeitangaben („vor 5 s“) ändern das Karten-HTML und führen so nur zum Ersetzen der betroffenen Karte.
7. **Verhaltensaussagen:**
   1. Jede Karte (alle Zustände) zeigt unter der Meta-Zeile eine einzeilige, per `truncate` gekürzte URL und daneben den Kopier-Button.
   2. `http(s)://`-URLs sind Links mit `target="_blank"` und `rel="noopener noreferrer"`; andere Werte sind reiner Text.
   3. URL und Attribute sind HTML-escaped (Test: Job-URL `javascript:alert(1)"><b>` erscheint als Text, kein Link, kein Markup).
   4. Klick auf das Icon kopiert die URL und zeigt „URL kopiert“; im Nicht-Secure-Context (`http://<LAN-IP>`) greift der `execCommand`-Fallback; schlägt beides fehl, „Kopieren nicht möglich“. Dateiname kopieren nutzt dieselbe Funktion („Dateiname kopiert“).
   5. `error` und `canceled` zeigen „Erneut“, „Neu wählen“, „Entfernen“; „Neu wählen“ setzt URL, Profil, Hinweis „Ersetzt Eintrag: <Titel oder URL>“, schließt die Auswahlkarte, scrollt zur URL-Karte ohne Fokus.
   6. `running` + `needs_probe`: Pill „Wird analysiert“, keine Prozentangabe/kein Balken. Leerer `title`: gedämpftes „Ohne Titel“.
   7. Ändert sich zwischen zwei Polls nichts an der Job-ID-Reihenfolge, werden nur Karten mit geändertem HTML ersetzt; ein gedrückter Button auf einer unveränderten Karte bleibt erhalten.

**CSS:** Kopier-Icon nutzt `.icon-btn` aus Task 5; weitere Klassen sind Tailwind-Utilities in `app.js`. Trotzdem neu bauen, weil neue Utility-Klassen in `app.js` dazukommen (`min-w-0`, `flex-1`, `hover:text-text`, `text-dim` u. a.).

**Steps:**
1. Änderungen 1–6 umsetzen.
2. `node --check web/static/app.js`.
3. `make css` (Docker, siehe Rahmen).
4. `make check`.
5. Commit: `git add web/static/app.js web/src/input.css web/static/app.css` (`input.css` nur, falls geändert), Message `feat: Job-Liste mit URL-Zeile, Kopier-Button, Neu wählen und karten-weisem Rendern` (+ Trailer).

## Task 7: README

**Files:** `README.md` (Englisch).

**Einbaustellen** (Gliederung per `mb-tk md toc README.md`; Stand: `## Features` Z. 9–29, `## Poster images` Z. 94–103, `## Metadata sidecar` Z. 104–113, `## Multi-language audio` Z. 114–136, `## Sharing links from other apps` Z. 137–170):
1. In `## Features` (Z. 9–29) nach der Zeile „Pick exact video + audio formats…“ (Z. 12) einen Punkt: one-click **Download** with a quality profile, skipping the analysis step.
2. Neuer Abschnitt `## Quick download, re-picking and copying URLs` **vor** `## Multi-language audio` (also nach `## Metadata sidecar`, Ende Z. 113). Inhalt (englisch, kurze Absätze):
   - **Quick download:** The profile dropdown next to *Analyze* (preset to your default profile) and the **Download** button queue the URL right away; analysis happens in the queue (card shows "Analyzing"). For a playlist the placeholder is replaced by one job per video; audio tracks are chosen automatically (German + original, as for playlists).
   - **Re-pick:** failed or canceled entries have a **Re-pick** button next to *Retry*: it puts the URL back into the field and, when you start the new download (quick or after analysis), replaces the old entry; the link is dropped when you edit or clear the field or click ✕ in the hint. The old entry stays untouched if creating the new job fails.
   - **Copy URL:** every job card shows its URL (a link only for http/https) with a copy button; works over plain `http://<LAN-IP>` too (fallback without the Clipboard API). Clicking the filename copies it the same way.
   - **Clear:** the ✕ in the URL field empties it.
   - API: `POST /api/jobs` accepts `{"type":"direct","url":"…","profile":"best"}` and an optional `"replace":"<job id>"` for all types.
3. In `## Sharing links from other apps` (Z. 137–170) keine Änderung (Autostart läuft weiter über die Analyse).

**Steps:**
1. `mb-tk md toc README.md` und Z. 9–29, 104–114 lesen, Einfügestellen bestätigen.
2. Texte einfügen.
3. `make check` (unverändert grün; README hat keinen Test).
4. Commit: `git add README.md`, Message `docs: README — Direkt-Download, Neu wählen, URL kopieren` (+ Trailer).

## Task 8: Browser-Prüfung (Orchestrator)

Kein Subagent-Task; der Orchestrator prüft im Browser (chrome-devtools-MCP oder manuell) und trägt Befunde ein. Nur lesen, kein Produktcode ändern; Befunde als Fix-Tasks zurück an Subagenten.

**Instanz starten:**
- Echte Instanz: `make up` (baut das Image inkl. frischem `app.css`, Port **8080 fest** in `docker-compose.yml`; `make start` öffnet zusätzlich den Browser). Stoppen: `make down`.
- Alternative mit Fake-yt-dlp (falls gescheiterte Downloads gezielt provoziert werden sollen; Port ebenfalls 8080, nicht gleichzeitig mit `make up`): ein Shell-Skript als `tmp/config/bin/yt-dlp` ablegen (`EnsureBinary` nutzt vorhandene Datei, siehe `internal/ytdlp/update.go` Z. 18–35), das für `-J` je nach URL JSON für Video, Playlist oder Fehler (Exit 1) ausgibt und beim Download den `after_move:filepath`-Pfad druckt; dann `make run`.
- Fallback-Test des Kopierens: App über `http://<LAN-IP>:8080` (LAN-IP des Rechners, z. B. `ipconfig getifaddr en0`) öffnen; dort ist `navigator.clipboard` nicht verfügbar.

**Checkliste (abhaken):**
- [ ] Direkt-Download Video: Eintrag erscheint, „Wird analysiert“, danach Titel und Fortschritt.
- [ ] Direkt-Download Playlist: Platzhalter verschwindet, Einträge erscheinen.
- [ ] Direkt-Download Fehler (ungültige URL, Fake-yt-dlp scheitert): Meldung in `#probe-error` bzw. Job `error`, „Erneut“ analysiert neu.
- [ ] Neu wählen über Direkt-Download: alter Eintrag verschwindet erst nach erfolgreichem Anlegen.
- [ ] Neu wählen über „Analysieren“ + `start()`: ebenso.
- [ ] Verknüpfung löst sich bei URL-Änderung, beim Leeren und über das ✕ im Hinweis; danach wird kein `replace` gesendet (Netzwerk-Tab: Body von `POST /api/jobs`).
- [ ] URL-Zeile: gekürzt, Link nur bei `http(s)://`, sonst Text.
- [ ] Kopieren im sicheren Kontext (`http://localhost:8080`) und im Fallback über `http://<LAN-IP>:8080` (URL und Dateiname).
- [ ] Leeren-Button: nur sichtbar bei gefülltem Feld, auch nach „Neu wählen“ und geteilter URL (`/?url=…`); Fokus im Feld nach Klick.
- [ ] Schmale Breite ca. 390 px: Layout der URL-Karte, Touch-Ziele 44×44 px (Emulation mit `pointer: coarse`).
- [ ] Klicks auf Buttons gehen beim 1,5-s-Polling nicht verloren (mehrere Jobs laufen, Entfernen/Abbrechen/Neu wählen sofort wirksam).
- [ ] iOS Safari: manueller Test durch den Nutzer nach dem Deploy (Kopieren, Leeren, Layout).

## Offene Punkte

- Autostart über das Teilen-Menü bleibt bei der Analyse; „Neu wählen“ gilt je Eintrag, nicht je Playlist; `handleRetry`/`handleDelete` bleiben unverändert (alle laut Spec).
- Bei `type:"video"` wird nun `playlist_title` aus dem Request übernommen (bisher ignoriert); Frontend sendet es dort nicht.
