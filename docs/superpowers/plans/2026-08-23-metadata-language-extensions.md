# Metadaten & Mehrsprachigkeit Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Job-Karten zeigen Zeitstempel und Dateiname; Downloads sortieren sich nach Quelle/Kanal; mehrsprachige Videos laden per Default de-Spur + Original, wählbar über die UI.

**Architecture:** Go-seitige Audio-Ranking-Engine (`internal/ytdlp/audiorank.go`) wählt Spuren aus dem Probe-JSON und liefert sie der UI als Chips; der Server baut daraus Format-Ausdrücke mit konkreten IDs. Der Runner erfasst den finalen Dateipfad per `--print after_move:filepath`. Verzeichnisse entstehen über das neue Default-Output-Template.

**Tech Stack:** Go 1.24, Vanilla JS, Tailwind v4 via Docker (`make css`), yt-dlp im Container.

**Spec:** `docs/superpowers/specs/2026-08-23-metadata-language-extensions-design.md`

## Global Constraints

- Neues Default-Template exakt: `%(extractor)s/%(channel,uploader|Unbekannt)s/%(title)s [%(id)s].%(ext)s`
- Merge-Optionen bei ≥ 2 Audio-IDs exakt: `--audio-multistreams`, `--merge-output-format mp4/mkv`
- Dateiname-Erfassung exakt: `--print after_move:filepath`; gespeichert relativ zu `DownloadDir`
- CSS-Klassen in HTML/JS nur als vollständige Literale; nach HTML/JS-Änderungen `make css` + `app.css` mitcommitten
- UI-Texte Deutsch; `make check` muss nach jedem Task grün sein; nach jedem Task committen
- Bestehende IDs/Endpunkte nicht umbenennen; neue JSON-Felder mit `omitempty`

---

### Task 1: Probe-Formate um Sprachfelder erweitern

**Files:**
- Modify: `internal/ytdlp/probe.go` (Format-Struct)
- Test: `internal/ytdlp/probe_test.go`

**Interfaces:**
- Produces: `Format.Language string` (`json:"language,omitempty"`), `Format.LanguagePreference *int` (`json:"language_preference,omitempty"`) — Task 2 und die Probe-API-Antwort nutzen genau diese Felder/Namen.

- [ ] **Step 1: Failing Test** — in `probe_test.go` einen Testfall ergänzen (an bestehende Parse-Tests anlehnen), der ein Format-JSON mit `"language": "de-DE", "language_preference": 10` parst und beide Felder prüft; plus ein Format ohne die Felder → `Language == ""`, `LanguagePreference == nil`.
- [ ] **Step 2: Test läuft rot** — `go test ./internal/ytdlp/`
- [ ] **Step 3: Felder ergänzen** im Format-Struct:

```go
	Language           string `json:"language,omitempty"`
	LanguagePreference *int   `json:"language_preference,omitempty"`
```

- [ ] **Step 4: Test grün** — `go test ./internal/ytdlp/`
- [ ] **Step 5: Commit** — `feat: Probe reicht language/language_preference durch`

---

### Task 2: Audio-Ranking-Engine

**Files:**
- Create: `internal/ytdlp/audiorank.go`
- Test: `internal/ytdlp/audiorank_test.go`

**Interfaces:**
- Consumes: `Format` mit `Language`/`LanguagePreference` aus Task 1; vorhandene Felder `FormatID`, `FormatNote`, `ACodec`, `VCodec`, `ABR`, `TBR`.
- Produces (Task 5/6/7 verlassen sich exakt hierauf):

```go
// AudioTrack beschreibt eine wählbare Audiospur eines Einzelvideos.
type AudioTrack struct {
	FormatID string `json:"format_id"`
	Language string `json:"language"` // Kurzform, z. B. "de", "en"; "" wenn unbekannt
	Label    string `json:"label"`    // z. B. "de", "en (Original)", "de (Audiodeskription)"
	Original bool   `json:"original"`
	Selected bool   `json:"selected"` // Default-Auswahl nach Spec-Regel
}

// RankAudio liefert je Sprache die beste Audiospur samt Default-Auswahl.
// Rückgabe sortiert: de, en, Rest (stabil); leere Liste, wenn keine
// Audio-Formate Sprachinfos tragen.
func RankAudio(formats []Format) []AudioTrack
```

**Algorithmus (verbindlich):**

1. Audio-Formate filtern: `ACodec` weder leer noch `"none"`, `VCodec` leer oder `"none"`.
2. Audiodeskription: `FormatID` oder `FormatNote` enthält case-insensitiv `audiodeskription`, `audio description` oder `audio_desc`.
3. Original: Wenn die Nicht-Deskriptions-Spuren mindestens zwei
   verschiedene Nicht-nil-`LanguagePreference`-Werte haben → Original =
   Spuren mit dem Maximum (YouTube-Fall). Sonst → Original = `FormatID`
   oder `FormatNote` enthält case-insensitiv `original` (ARTE-Fall).
4. Sprach-Kurzform: `strings.ToLower` vom Teil vor `-` (aus `de-DE` wird `de`); Formate ohne `Language` werden ignoriert.
5. Je Sprach-Kurzform die beste Spur wählen: Original schlägt Nicht-Original schlägt Audiodeskription; innerhalb dessen höchste `ABR` (Fallback `TBR`).
6. Default-Auswahl (`Selected`): beste de-Spur, sofern sie keine Audiodeskription ist, **plus** die Original-Spur (falls erkannt, auch fremdsprachig). Gleiches Format nur einmal. Kein de → Original allein. Kein Original erkannt → de, sonst en, sonst erste Spur.
7. Label: Kurzform + ` (Original)` bzw. ` (Audiodeskription)` falls zutreffend.
8. Sortierung der Rückgabe: de zuerst, dann en, dann Rest in Eingabereihenfolge.

- [ ] **Step 1: Failing Tests** mit Fixtures aus den realen Proben (Spec §Verifizierte Fakten). ARTE-Fixture:

```go
func intp(i int) *int { return &i }

var arteAudio = []Format{
	{FormatID: "VA-STA-audio_0-Deutsch__Audiodeskription_", ACodec: "mp4a", Language: "de", LanguagePreference: intp(110101)},
	{FormatID: "VA-STA-audio_0-Englisch__Original_", ACodec: "mp4a", Language: "en", LanguagePreference: intp(110101)},
	{FormatID: "VA-STA-audio_0-Französisch", ACodec: "mp4a", Language: "fr", LanguagePreference: intp(110101)},
	{FormatID: "VA-STA-audio_0-Deutsch", ACodec: "mp4a", Language: "de", LanguagePreference: intp(110101)},
}
```

Erwartung ARTE: Tracks [de (id `…-Deutsch`), en mit Original=true, fr]; Selected = de + en; die Audiodeskription ist NICHT der de-Track.

YouTube-Fixture (`Language "de-DE"/"en-US"`, Preferences -1/10, ABR 129 „medium“ vs. 49 „low“):

```go
var ytAudio = []Format{
	{FormatID: "139-0", ACodec: "mp4a.40.5", Language: "de-DE", LanguagePreference: intp(-1), ABR: 49},
	{FormatID: "139-7", ACodec: "mp4a.40.5", Language: "en-US", LanguagePreference: intp(10), ABR: 49, FormatNote: "English (US) original (default), low"},
	{FormatID: "140-0", ACodec: "mp4a.40.2", Language: "de-DE", LanguagePreference: intp(-1), ABR: 129},
	{FormatID: "140-7", ACodec: "mp4a.40.2", Language: "en-US", LanguagePreference: intp(10), ABR: 129, FormatNote: "English (US) original (default), medium"},
}
```

Erwartung YouTube: de-Track = `140-0` (beste ABR), en-Track = `140-7` mit Original=true; Selected = beide.

Weitere Fälle: (a) einsprachig en-Original → ein Track, Selected; (b) de nur als Audiodeskription + en-Original → Selected = nur en, de-Track trägt Label `de (Audiodeskription)` und Selected=false; (c) keine Language-Infos → leere Liste; (d) de ist selbst Original → Selected = genau ein Track.

- [ ] **Step 2: rot** — `go test ./internal/ytdlp/ -run TestRankAudio`
- [ ] **Step 3: Implementieren** gemäß Algorithmus.
- [ ] **Step 4: grün** — `go test ./internal/ytdlp/`
- [ ] **Step 5: Commit** — `feat: audiorank — Spurauswahl de+Original mit ARTE/YouTube-Heuristik`

---

### Task 3: BuildFormat für mehrere Audiospuren

**Files:**
- Modify: `internal/ytdlp/format.go`
- Test: `internal/ytdlp/format_test.go`

**Interfaces:**
- Produces: `func BuildFormatMulti(videoID string, audioIDs []string, audioOnly bool) string`. Bestehendes `BuildFormat(videoID, audioID string, audioOnly bool)` bleibt und delegiert (`BuildFormatMulti(videoID, []string{audioID}, audioOnly)` bzw. leere Liste bei `""`).
- Zusätzlich: `Profile` erhält Feld `VideoExpr string` (nur Videoteil, z. B. `bv*`, `bv*[height<=1080]`, `bv*[height<=1080][ext=mp4]`, `bv*[height<=720]`; beim Profil `audio` leer). Task 6 nutzt es für Chips+Profil-Kombination.

**Regeln BuildFormatMulti:**

- `audioOnly` und ≥ 1 ID → genau die erste ID. `audioOnly` ohne IDs → `ba`.
- Video-Teil: `videoID` falls gesetzt, sonst `bv*`. Audio-IDs mit `+` anhängen: `bv*+140-0+140-7`.
- Keine Audio-IDs: Verhalten wie bisher (`videoID` allein bzw. `bv*+ba/b`).

- [ ] **Step 1: Failing Tests** — Tabelle: (`""`, `["140-0","140-7"]`, false) → `bv*+140-0+140-7`; (`"137"`, `["140-0","140-7"]`, false) → `137+140-0+140-7`; (`""`, `["140-0"]`, true) → `140-0`; Delegation von `BuildFormat` unverändert zu heutigen Fällen (bestehende Tests bleiben grün).
- [ ] **Step 2: rot** → **Step 3: implementieren** (inkl. `VideoExpr` in `Profiles` befüllen) → **Step 4: grün** (`go test ./internal/ytdlp/`)
- [ ] **Step 5: Commit** — `feat: BuildFormatMulti — Formatausdrücke mit mehreren Audiospuren`

---

### Task 4: Job-Modell — FinishedAt, Filename, AudioFormatIDs

**Files:**
- Modify: `internal/job/job.go`, `internal/queue/queue.go` (Endzustands-Übergänge)
- Test: `internal/job/job_test.go`, `internal/queue/queue_test.go`

**Interfaces:**
- Produces (Task 5/6/7 nutzen exakt diese Namen):

```go
	FinishedAt     *time.Time `json:"finished_at,omitempty"`
	Filename       string     `json:"filename,omitempty"`
	AudioFormatIDs []string   `json:"audio_format_ids,omitempty"`
	MultiAudio     bool       `json:"multi_audio,omitempty"`
```

`MultiAudio` steuert die Multistream-Flags des Runners (Task 5); gesetzt
wird es vom Server (Video-Jobs mit ≥ 2 IDs, Task 6) und vom
Playlist-Pfad (Task 8).

- `FinishedAt` wird überall dort gesetzt (UTC), wo die Queue einen Job in `done`, `error` oder `canceled` überführt; bei `retry` (zurück nach `queued`) wird es auf `nil` zurückgesetzt und `Filename`/`Error` geleert (Filename-Leeren nur bei retry, sonst unangetastet).

- [ ] **Step 1: Failing Tests** — Queue-Tests: nach erfolgreichem Lauf `FinishedAt != nil`; nach Fehler `!= nil`; nach Cancel `!= nil`; nach Retry wieder `nil`. Job-Serialisierung: leere Felder erscheinen nicht im JSON (`json.Marshal`-Check auf Abwesenheit von `finished_at`).
- [ ] **Step 2: rot** → **Step 3: implementieren** (alle Übergangsstellen in `queue.go` finden: nach Runner-Ende, Cancel-Pfad, Fehler-Pfad) → **Step 4: grün** (`go test ./internal/job/ ./internal/queue/`)
- [ ] **Step 5: Commit** — `feat: Job — FinishedAt, Filename und AudioFormatIDs im Modell`

---

### Task 5: Runner — Dateipfad erfassen, Multistream-Argumente

**Files:**
- Modify: `internal/ytdlp/runner.go`
- Test: `internal/ytdlp/runner_test.go` (+ ggf. `progress_test.go`-Muster wiederverwenden)

**Interfaces:**
- Consumes: `Job.AudioFormatIDs` (Task 4).
- Produces: Der Runner meldet den erfassten relativen Dateipfad über einen neuen optionalen Callback `OnFilename func(rel string)` im Runner-Struct (analog zum vorhandenen Progress-Callback-Muster; die Queue verdrahtet ihn in Task 6 auf `job.Filename`).

**Verhalten:**

- Argumente immer ergänzen: `--print`, `after_move:filepath` (nach den bestehenden Argumenten).
- Wenn `job.MultiAudio`: zusätzlich `--audio-multistreams`, `--merge-output-format`, `mp4/mkv`.
- stdout-Zeilen, die mit dem absoluten `DownloadDir` beginnen (nach `filepath.Abs`-Normalisierung), sind der finale Pfad: relativieren (`filepath.Rel`), per Callback melden. Alle anderen Zeilen wie bisher als Progress parsen.

- [ ] **Step 1: Failing Tests** — (a) Argumentliste enthält die Multistream-Flags nur bei `MultiAudio`, `--print after_move:filepath` immer; (b) Fixture-stdout mit gemischten Zeilen (Progress-Template-Zeilen + eine Pfadzeile `/downloads/youtube/Kanal/Titel [id].mkv`) → Callback erhält `youtube/Kanal/Titel [id].mkv`, Progress-Parsing unverändert. Bestehende Runner-Tests als Muster für das Prozess-Faking nutzen.
- [ ] **Step 2: rot** → **Step 3: implementieren** → **Step 4: grün** (`go test ./internal/ytdlp/`)
- [ ] **Step 5: Verifikation gegen echtes yt-dlp (macht der Orchestrator):** einmaliger Docker-Lauf, der bestätigt, dass `--print after_move:filepath` gemeinsam mit `--progress-template` genau eine Pfadzeile auf stdout liefert.
- [ ] **Step 6: Commit** — `feat: Runner — after_move-Dateipfad und Multistream-Merge-Optionen`

---

### Task 6: Server-API & Config — audio_languages, audio_format_ids, neues Template

**Files:**
- Modify: `internal/server/server.go`, `internal/config/config.go`, `internal/queue/queue.go` (OnFilename-Verdrahtung)
- Test: `internal/server/server_test.go`, `internal/config/config_test.go`

**Interfaces:**
- Consumes: `RankAudio` (Task 2), `BuildFormatMulti`/`Profile.VideoExpr` (Task 3), Job-Felder (Task 4), `OnFilename` (Task 5).
- Produces:
  - Probe-Antwort `video` erhält `audio_languages []ytdlp.AudioTrack` (`json:"audio_languages,omitempty"`, nur bei Einzelvideos, Ergebnis von `RankAudio(video.Formats)`).
  - `POST /api/jobs` (type video) akzeptiert zusätzlich `audio_format_ids []string`. Wenn gesetzt: Format-Ausdruck je Modus —
    Profil-Modus: `ProfileByKey(profile).VideoExpr + "+" + join(ids, "+") + "/" + Profile.Expr` (Fallback auf den bisherigen Profilausdruck);
    manueller Modus: `BuildFormatMulti(format_video, ids, audio_only)`.
    IDs landen in `Job.AudioFormatIDs`.
  - Config-Default `OutputTemplate` = `%(extractor)s/%(channel,uploader|Unbekannt)s/%(title)s [%(id)s].%(ext)s` (Env-Override unverändert).
  - Queue setzt `job.Filename` über den Runner-Callback.

- [ ] **Step 1: Failing Tests** — (a) Probe-Handler-Test mit gemocktem Probe-Ergebnis (mehrsprachige Formate) → Antwort enthält `audio_languages` mit Selected-Flags; einsprachig → Feld fehlt oder Länge ≤ 1. (b) Jobs-POST mit `audio_format_ids` → gespeicherter Job hat den erwarteten Format-Ausdruck und die IDs. (c) Config-Test: neuer Default; Env-Override greift weiter.
- [ ] **Step 2: rot** → **Step 3: implementieren** → **Step 4: grün** (`go test ./...`)
- [ ] **Step 5: Commit** — `feat: API — audio_languages im Probe, audio_format_ids beim Job, Quelle/Kanal-Template`

---

### Task 7: UI — Sprach-Chips, Zeitstempel, Dateiname

**Files:**
- Modify: `web/templates/index.html`, `web/static/app.js`
- Modify (generiert): `web/static/app.css` (`make css`)

**Interfaces:**
- Consumes: `audio_languages` aus der Probe-Antwort, `finished_at`/`filename`/`created_at` aus `/api/jobs`; sendet `audio_format_ids` beim Start.

**Verhalten (verbindlich):**

1. **Chips:** In der Auswahl-Karte unter der Segmented Control ein Block `id="audio-langs"` (hidden by default): Label „Audiosprachen“ + eine Checkbox je `audio_languages`-Eintrag (Chip-Optik; Klassen als `@layer components` `.chip` in `web/src/input.css` ergänzen — Checkbox `sr-only`, Label-Optik analog `.seg-label`, checked-Zustand wie aktives Segment). Sichtbar nur, wenn ≥ 2 Einträge UND Modus „Profil“ (in `updateModeVisibility()` mitschalten). Vorauswahl = `selected`-Flags; Beschriftung = `label`.
2. **Start-Payload:** Im Profil-Modus mit sichtbaren Chips: `audio_format_ids` = IDs der angehakten Chips (Reihenfolge der Liste). Keine Chips → Feld weglassen (Server-Fallback). `format_label` clientseitig um die Sprachlabels ergänzen (z. B. `Beste Qualität · de + en (Original)`).
3. **Zeiten:** Helfer `relTime(iso)` → „gerade eben“ (< 60 s), „vor N min“, „vor N h“, sonst lokales Datum (`toLocaleDateString("de-DE")` + Uhrzeit). In der Job-Meta-Zeile: `hinzugefügt <relTime(created_at)>`; bei Endzuständen zusätzlich `fertig/beendet <relTime(finished_at)>` (done → „fertig“, error/canceled → „beendet“). Absolutwert in `title`-Attribut des jeweiligen `<span>`.
4. **Dateiname:** Bei `done` mit `filename`: eigene Zeile in der Job-Karte (Klasse-Literale, `truncate`, `text-xs text-muted`, `cursor-pointer`, `font-mono`), `title`-Attribut = voller Pfad. Klick → `navigator.clipboard.writeText(filename)` → Toast „Dateiname kopiert“; im Fehlerfall Toast „Kopieren nicht möglich“ (kind error). Event-Delegation über das bestehende `jobs-list`-Click-Handling (`data-action="copy"`, `data-filename`-Attribut escapen mit `esc()`).
5. **Audio-Select-Labels** („Formate wählen“): Sprach-Präfix `[de] ` vor dem bisherigen Label, wenn `language` am Format hängt.

- [ ] **Step 1: HTML + input.css ergänzen** (Chips-Block, `.chip`-Komponente)
- [ ] **Step 2: app.js umsetzen** (Chips-Rendering in `renderSelectCard`, Payload, relTime, Dateiname-Zeile + Copy, Select-Labels)
- [ ] **Step 3: `make css`** — neue Klassen im Output verifizieren (`grep -c "chip" web/static/app.css` ≥ 1)
- [ ] **Step 4: `make check`** grün; lokaler Smoke (`make build` + curl wie im Redesign-Plan Task 3 Step 9)
- [ ] **Step 5: Commit** — `feat: UI — Sprach-Chips, relative Zeitstempel, kopierbarer Dateiname`

---

### Task 8: Playlist-Fallback, README, E2E-Verifikation

**Files:**
- Modify: `internal/queue/queue.go` oder `internal/server/server.go` (wo Playlist-Jobs ihren Format-Ausdruck erhalten), `README.md`, `docs/backlog.md`
- Test: bestehende Playlist-Tests erweitern

**Interfaces:**
- Consumes: alles Vorherige.

- [ ] **Step 1: Playlist-Fallback** — Playlist-Jobs (Profilausdruck) erhalten stattdessen die Kette `<VideoExpr>+ba[language^=de]+ba[language_preference>0]/<VideoExpr>+ba[language^=de]/<bisheriger Expr>` und `MultiAudio = true` (Feld aus Task 4; beim Profil `audio` bleibt alles wie bisher). Test: Playlist-Job hat die Fallback-Kette im Format-Ausdruck und `MultiAudio` gesetzt.
- [ ] **Step 2: README** — Abschnitte: neuer Verzeichnis-Default (+ Env-Override, `.part`-Hinweis), Mehrsprachigkeit (Default-Regel, mp4/mkv), Zeitstempel/Dateiname in der UI.
- [ ] **Step 3: Backlog** — Ideen 1–3 als „geplant/umgesetzt am 2026-08-23“ markieren (kurzer Vermerk mit Spec-/Plan-Link, Einträge nicht löschen).
- [ ] **Step 4: E2E (macht der Orchestrator):** `make start`; echte Probe der ARTE-URL über die UI → Chips „de“ + „en (Original)“ vorausgewählt; Download starten → Datei landet unter `ArteTV/Unbekannt/…`, Job zeigt Zeiten + Dateiname; DevTools-Fixtures für alle Kartenzustände; `make check`.
- [ ] **Step 5: Commit** — `feat: Playlist-Sprachfallback, Doku, Backlog-Abschluss`
