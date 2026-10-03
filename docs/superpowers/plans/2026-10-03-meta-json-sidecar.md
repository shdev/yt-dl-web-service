# Metadaten-Sidecar (`.meta.json`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) oder superpowers:executing-plans, Task für Task. Status: Entwurf, wartet auf Freigabe durch den Nutzer.

## Goal

Neben jeder fertig heruntergeladenen Datei liegen die vollen yt-dlp-Metadaten als JSON, Dateiname = Mediendateiname + `.meta.json`.

## Architecture

yt-dlp schreibt die Metadaten selbst (`--write-info-json`), der Go-Runner benennt die Datei nach erfolgreichem Lauf um. Kein zweiter Netzwerkabruf, keine API- oder UI-Änderung.

## Tech Stack

Go 1.24, yt-dlp im Container (geprüft mit 2026.08.19).

## Entscheidungen

1. **Dateiname:** Voller Mediendateiname plus `.meta.json`, Beispiel `Me at the zoo [jNQXAC9IVRw].mp4` → `Me at the zoo [jNQXAC9IVRw].mp4.meta.json`. Annahme aus der Formulierung „angehängt"; die Alternative `…[jNQXAC9IVRw].meta.json` wurde nicht gewählt, weil Audio- und Video-Download desselben Videos sonst dieselbe Metadatei teilen würden.

2. **Inhalt:** Die unveränderte `info.json` von yt-dlp (von yt-dlp bereinigt, keine eigene Filterung). Enthält u. a. `description`, `channel`, `upload_date`, `view_count`, `like_count`, `tags`, `categories`, `chapters`, `subtitles`, `automatic_captions`, `thumbnails`, `formats`.

3. **Aktivierung:** Immer aktiv, kein Schalter in den Einstellungen (wie beim Poster).

4. **Fehlerbehandlung:** Fehler beim Schreiben oder Umbenennen der Metadatei lassen den Job nicht fehlschlagen; es wird nur geloggt (wie beim fehlenden Poster).

5. **Job-Feld und UI:** Kein neues Job-Feld, keine UI-Anzeige (YAGNI).

6. **Löschen:** Löschen eines Jobs fasst weiterhin keine Dateien an (heutiges Verhalten von `handleDelete`).

## Verifizierte Fakten

Probe vom 2026-10-03, yt-dlp 2026.08.19 in python:3.12-alpine.

- `--write-info-json` mit dem Default-Template `%(extractor)s/%(channel,uploader|Unbekannt)s/%(title)s [%(id)s].%(ext)s` schreibt `youtube/jawed/Me at the zoo [jNQXAC9IVRw].info.json` neben `…[jNQXAC9IVRw].mp4`, also Stamm ohne Medien-Extension plus `.info.json`. Die Endung `.info.json` ist bei yt-dlp nicht änderbar, deshalb die Umbenennung in Go.

- Die Datei wird vor dem Download geschrieben; bei Abbruch oder Fehler bleibt sie als `.info.json` liegen (wie `.part`-Dateien) und wird beim Retry überschrieben.

- Größe im Test: 82 KB, 70 Schlüssel auf oberster Ebene, davon etwa 57 KB für 24 Einträge in `formats` (enthalten signierte, ablaufende Stream-URLs).

- Verworfen: `--print-to-file "after_move:%()j" "%(filepath)s.meta.json"`. yt-dlp ersetzt im Ziel-Template die Pfadtrenner (`/` wird zu `⧸`), die Datei landet mit verstümmeltem Namen im Arbeitsverzeichnis. Außerdem hängt die Option an statt zu überschreiben und liefert 25 interne Zusatzfelder (`__finaldir`, `_filename`, `formats_table` …).

- Generischer Extraktor (direkte mp4-URL) liefert nur 26 Schlüssel; Umfang hängt also von der Quelle ab.

## Global Constraints

- TDD: erst roter Test, dann Implementierung; `make check` nach jedem Task grün; nach jedem Task ein Commit.
- Kommentare und Fehlermeldungen Deutsch, wie im Bestand.
- Bestehende Argumente und deren Reihenfolge in `ExecRunner.Run` nicht ändern, nur ergänzen.
- Merge am Ende mit `--no-ff` nach main.

## Task 1: Runner schreibt und benennt die Metadatei

**Files:** `internal/ytdlp/runner.go`, `internal/ytdlp/runner_test.go`, neues Test-Fake `internal/ytdlp/testdata/infojson.sh`

**Interface:**
- Konstante `metaSuffix = ".meta.json"`.
- `func infoJSONCandidates(mediaPath string) []string` liefert in dieser Reihenfolge: (a) `strings.TrimSuffix(mediaPath, filepath.Ext(mediaPath)) + ".info.json"`, (b) `mediaPath + ".info.json"` (Fall: Template ohne `.%(ext)s`).
- `func moveInfoJSON(mediaPath string) error` benennt den ersten existierenden Kandidaten per `os.Rename` nach `mediaPath + metaSuffix` um (überschreibt eine vorhandene Zieldatei); existiert kein Kandidat, Fehler `keine info.json gefunden`.

**Verhalten in `Run`:**
- Argument `--write-info-json` direkt nach `"--print", "after_move:filepath"` einfügen.
- In der Scan-Schleife den absoluten Pfad der zuletzt gemeldeten `after_move:filepath`-Zeile merken (zusätzlich zum bestehenden `OnFilename`-Aufruf).
- Nach erfolgreichem `cmd.Wait()` und nur wenn ein Pfad gemerkt wurde: `moveInfoJSON(pfad)` aufrufen; ein Fehler wird mit `log.Printf("yt-dlp: Metadatei nicht abgelegt: %v", err)` geloggt, `Run` gibt trotzdem `nil` zurück.
- Bei Fehler oder Abbruch des Laufs keine Umbenennung.

**Steps:**
1. Failing Tests: (a) Argumentliste enthält `--write-info-json` (Muster des bestehenden Tests mit `testdata/echo-args.sh`); (b) Fake `infojson.sh` legt `$PRINT_LINE` als Mediendatei und daneben `<Stamm>.info.json` an und gibt `$PRINT_LINE` aus (Aufbau wie `testdata/filepath.sh`): danach existiert `<Mediendatei>.meta.json` mit dem Inhalt der info.json, die `.info.json` ist weg; (c) Fake ohne info.json: `Run` gibt `nil` zurück, keine `.meta.json`; (d) fehlschlagender Lauf (`testdata/dl-fail.sh`): keine Umbenennung; (e) Tabellentest für `infoJSONCandidates` mit `a/b/Titel [id].mp4` und `a/b/Titel` (ohne Extension).
2. Test rot: `go test ./internal/ytdlp/ -run 'TestRun|TestInfoJSON'`
3. Implementieren.
4. Test grün: `go test ./internal/ytdlp/` und `make check`.
5. Commit: `feat: Runner legt volle yt-dlp-Metadaten als <datei>.meta.json ab`

## Task 2: README und Backlog

**Files:** `README.md`, `docs/backlog.md`

- **README:** Neuer Abschnitt `## Metadata sidecar` direkt nach dem Abschnitt `## Poster images` (Zeile 94 ff.), Englisch wie der Rest der README. Namensschema mit dem Beispiel `Me at the zoo [jNQXAC9IVRw].mp4.meta.json`, Inhalt = unveränderte yt-dlp-info.json, Hinweis auf liegenbleibende `.info.json` bei abgebrochenen Downloads, Hinweis, dass ein Fehler beim Ablegen den Job nicht scheitern lässt.
- **Backlog:** Kurzer Vermerk „umgesetzt am <Datum der Umsetzung>".
- **Commit:** `docs: README — Metadaten-Sidecar .meta.json`

## Task 3: E2E-Verifikation (Orchestrator)

- `make start`, einen echten YouTube-Download und einen Instagram-Download über die UI anstoßen.
- Prüfen: `<datei>.meta.json` liegt neben Mediendatei und `-poster.jpg`, ist gültiges JSON (`jq 'keys | length'`), keine `.info.json` übrig.
- Retry-Fall: Job abbrechen, erneut starten, danach genau eine `.meta.json` und keine `.info.json`.

## Offene Punkte

- Bestätigung der Namensform aus Entscheidung 1.
- Bestehende Downloads erhalten keine Metadatei nachträglich (nicht Teil dieses Plans).
