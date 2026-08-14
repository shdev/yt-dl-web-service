# yt-dl-web-client — Design-Spezifikation

**Datum:** 2026-08-14
**Status:** Vom Nutzer abgenommenes Design (Brainstorming-Phase abgeschlossen)

## 1. Ziel

Ein einzelnes Docker-Image für den Betrieb auf einem NAS (x86_64/amd64). Es enthält
einen in Go geschriebenen Webservice, über den Videos per yt-dlp heruntergeladen
werden: URL eingeben, Video-/Audioformat und Größe auswählen (Daten aus dem
yt-dlp-Format-Listing), Download starten. Mehrere Downloads laufen parallel.
Fertige Dateien landen auf einem gemounteten NAS-Volume.

## 2. Entscheidungen (geklärt mit dem Nutzer)

| Thema | Entscheidung |
|---|---|
| Download-Ziel | NAS-Volume (`/downloads`), kein Browser-Download |
| Plattform | x86_64 (amd64), Single-Arch-Build |
| Job-Persistenz | Ja — Warteschlange, Fortschritt und Historie überleben Container-Neustarts |
| Zugriffsschutz | Keiner — Dienst ist nur im LAN erreichbar |
| Playlists | Ja — Playlist-URLs werden unterstützt |
| Base-Image | `mikenye/youtube-dl` (Wunsch des Nutzers; Alternative „eigenes Image" wurde besprochen und verworfen) |
| Styling | Bootstrap 5.3, lokal im Binary eingebettet (kein CDN) |

**Hinweise zum Base-Image:** `mikenye/youtube-dl` ist auf Docker Hub als
deprecated markiert, basiert auf `debian:sid-slim` und installiert intern
**yt-dlp** (nicht mehr das alte youtube-dl). Das Binary heißt
`/usr/local/bin/yt-dlp`. ffmpeg ist enthalten. Die eingefrorene yt-dlp-Version
wird durch `YTDLP_UPDATE_ON_START` (siehe unten) ausgeglichen.

## 3. Container & Konfiguration

### Image-Aufbau (Multi-Stage)

```dockerfile
# Stage 1: Go-Build
FROM golang:1.24 AS build
COPY . /src
WORKDIR /src
RUN CGO_ENABLED=0 go build -o /app ./cmd/server

# Stage 2: Runtime — mikenye als Base
FROM mikenye/youtube-dl
# yt-dlp + ffmpeg bereits enthalten; s6-Init wird durch unseren Service ersetzt
COPY --from=build /app /usr/local/bin/app
ENTRYPOINT ["/usr/local/bin/app"]
```

- Alle Web-Assets (Bootstrap CSS/JS, eigenes JS, HTML-Templates) werden per
  `go:embed` in das statische Go-Binary eingebettet. Die UI funktioniert ohne
  Internetzugang.
- Der `ENTRYPOINT` des Base-Images (`/init`, s6) wird bewusst ersetzt.

### Volumes

| Mount | Zweck |
|---|---|
| `/downloads` | Zielverzeichnis für fertige Videos (NAS-Ordner) |
| `/config` | Persistenz: `jobs.json` (Warteschlange + Historie) |

### Umgebungsvariablen

| Variable | Zweck | Default |
|---|---|---|
| `PORT` | HTTP-Port des Webservice | `8080` |
| `MAX_CONCURRENT` | Anzahl paralleler Downloads (Worker) | `3` |
| `OUTPUT_TEMPLATE` | yt-dlp-Dateinamensschema unterhalb `/downloads` | `%(title)s [%(id)s].%(ext)s` |
| `YTDLP_UPDATE_ON_START` | Beim Containerstart yt-dlp aktualisieren (Ablauf siehe unten) | `true` |

**yt-dlp-Update-Mechanismus:** Der Container läuft als Nicht-root
(`user:` im Compose), daher kann `yt-dlp -U` das root-eigene Binary unter
`/usr/local/bin` nicht ersetzen. Stattdessen kopiert der Go-Service beim
Start das Binary einmalig nach `/config/bin/yt-dlp` (beschreibbares Volume)
und führt dort `yt-dlp -U` aus. Der Runner verwendet immer
`/config/bin/yt-dlp`; schlägt das Update fehl (z.B. kein Internet), läuft
die vorhandene Version weiter — Start bricht nie am Update ab.

### Auslieferung

- `docker-compose.yml`-Vorlage für das NAS wird mitgeliefert, inkl.
  `user: "1000:1000"` (Dateien gehören nicht root).
- Docker-Healthcheck über das eigene Binary: `app -healthcheck` ruft
  intern `GET /healthz` auf und liefert Exit-Code 0/1 (`curl` ist im
  Runtime-Image nicht garantiert).

## 4. Go-Service: Komponenten

Nur Go-Standardbibliothek (`net/http`, `html/template`, `os/exec`, `encoding/json`) —
kein Web-Framework. Vier Komponenten mit klaren Schnittstellen:

### 4.1 Prober

- Eingabe: URL. Führt `yt-dlp -J --flat-playlist <url>` aus, um Video vs.
  Playlist zu erkennen.
- Einzelvideo: `yt-dlp -J <url>` liefert die Formatliste als JSON — dieselben
  Daten wie `-F`, aber maschinenlesbar: `format_id`, Auflösung, fps, Container
  (`ext`), `vcodec`/`acodec`, Dateigröße (`filesize`/`filesize_approx`),
  Bitrate. Dazu Titel, Thumbnail, Dauer.
- Playlist: Titel, Anzahl und URLs der Einträge (aus dem Flat-Dump).

### 4.2 Store

- Hält alle Jobs im Speicher; jede Zustandsänderung wird atomar
  (Temp-Datei + `rename`) nach `/config/jobs.json` geschrieben. Mutex-geschützt.
- Beim Start wird `jobs.json` geladen. **Crash-Recovery:** Jobs, die beim
  letzten Stopp `queued` oder `running` waren, werden automatisch wieder
  eingereiht. yt-dlp setzt dank `--continue` auf vorhandenen `.part`-Dateien auf.

### 4.3 Queue

- Worker-Pool mit `MAX_CONCURRENT` Workern.
- Job-Zustände: `queued → running → done | error | canceled`.
- Abbrechen beendet den yt-dlp-Prozess über die Prozessgruppe (SIGTERM).

### 4.4 Runner

- Startet pro Job:
  `yt-dlp -f <formatausdruck> -o /downloads/<OUTPUT_TEMPLATE> --newline --progress-template "dl:%(progress._percent_str)s|%(progress.speed)s|%(progress.eta)s" --continue --no-playlist <url>`
- Parst Fortschritt (%, Geschwindigkeit, ETA) zeilenweise aus stdout
  (Zeilenpräfix `dl:`, Felder `|`-getrennt).
- Liegt hinter einem Go-Interface, damit Tests einen Fake-Runner verwenden können.

## 5. Datenfluss

**Einzelvideo:**
1. Nutzer gibt URL ein → `POST /api/probe`.
2. UI zeigt Formatauswahl (Video- und Audioformat aus der Formatliste).
3. `POST /api/jobs` mit gewählten format_ids → yt-dlp-Ausdruck wie `303+251`.
   Schnellwahl „Beste Qualität" → `bv*+ba/b`; „Nur Audio" → `<audio format_id>`
   bzw. `ba` als Voreinstellung.
4. Job läuft durch die Queue; UI pollt den Fortschritt.

**Playlist:**
1. Probe erkennt Playlist → UI zeigt Titel, Anzahl und ein Qualitätsprofil-Dropdown
   („Beste Qualität", „Beste ≤1080p", „Beste ≤720p", „Nur Audio").
2. Beim Einreihen wird die Playlist in **einzelne Video-Jobs expandiert**
   (ein Job = ein Video). Das Profil wird zum Format-Ausdruck, z.B.
   `bv*[height<=1080]+ba/b[height<=1080]`.
3. Jeder Job hat eigenen Fortschritt und ist einzeln abbrechbar. Jobs tragen
   den Playlist-Titel als Metadatum.

## 6. HTTP-API

| Methode & Pfad | Zweck |
|---|---|
| `POST /api/probe` | URL analysieren → Video-Metadaten + Formatliste oder Playlist-Info |
| `POST /api/jobs` | Job(s) anlegen (Einzelvideo mit format_ids oder Playlist mit Profil) |
| `GET /api/jobs` | Alle Jobs mit Status/Fortschritt (Polling-Endpunkt der UI, ~1,5 s) |
| `POST /api/jobs/{id}/cancel` | Laufenden/wartenden Job abbrechen |
| `POST /api/jobs/{id}/retry` | Fehlgeschlagenen/abgebrochenen Job neu einreihen |
| `DELETE /api/jobs/{id}` | Listeneintrag entfernen (Datei bleibt erhalten) |
| `GET /healthz` | Healthcheck |
| `GET /` | Die Single-Page-UI |

## 7. UI

Eine Seite, Bootstrap 5.3 (lokal eingebettet), Vanilla-JS mit `fetch`:

1. **Eingabe-Card:** URL-Feld + Button „Analysieren".
2. **Auswahl-Card** (nach Probe):
   - Einzelvideo: Titel + Thumbnail; Dropdown Videoformat (Auflösung, fps,
     Codec, Größe), Dropdown Audioformat (Codec, Bitrate); Schnellwahl
     „Beste Qualität"; Option „Nur Audio".
   - Playlist: Titel, Anzahl Videos, Qualitätsprofil-Dropdown.
   - Button „Download starten".
3. **Jobs-Card:** Tabelle mit Titel, Format, Status-Badge, Progressbar,
   Geschwindigkeit, ETA, Aktionen (Abbrechen / Wiederholen / Entfernen).
   Aktualisierung per Polling (`GET /api/jobs`). Nach Browser-Reload bleibt
   alles sichtbar, da der Zustand vom Server kommt.

## 8. Fehlerbehandlung

- **Probe-Fehler** (ungültige URL, Geo-Block, gelöschtes Video): yt-dlp-
  stderr-Meldung als Bootstrap-Alert in der UI.
- **Download-Fehler:** Job → `error` mit stderr-Auszug (in der UI einsehbar),
  Retry-Button.
- **Container-Absturz:** Crash-Recovery aus 4.2 (Re-Enqueue + `--continue`).
- **Duplikate:** Gleiche URL + Format bereits `queued`/`running` → HTTP 409
  mit Hinweis in der UI.
- **Entfernen löscht nie Dateien** — `DELETE` betrifft nur den Listeneintrag.

## 9. Testing

Umsetzung folgt TDD. Testebenen:

- **Unit:** Format-Ausdruck-Builder, Progress-Parser, Probe-JSON-Parsing —
  jeweils mit echten yt-dlp-Output-Fixtures.
- **Store:** atomares Persistieren/Laden, Crash-Recovery (running → re-enqueued).
- **Handler:** `httptest` gegen die API mit Fake-Runner (Interface aus 4.4).
- **Integration (optional, lokal):** echter yt-dlp-Aufruf hinter einem
  Build-Tag, läuft nicht in CI.

## 10. Build & Entwicklungs-Workflow (Make)

Alle Standardaufgaben laufen über ein `Makefile` im Projekt-Root — es ist die
zentrale, dokumentierte Stelle für wiederkehrende Kommandos:

| Target | Zweck |
|---|---|
| `make build` | Go-Binary lokal bauen (`CGO_ENABLED=0`, nach `bin/app`) |
| `make test` | Alle Go-Tests ausführen |
| `make check` | `gofmt`-Prüfung + `go vet` + `make test` |
| `make run` | Service lokal starten (mit `./tmp/downloads` und `./tmp/config` als Volumes-Ersatz) |
| `make image` | Docker-Image für amd64 bauen (`docker build`) |
| `make up` / `make down` | Container per `docker compose` starten/stoppen |
| `make clean` | Build-Artefakte und `./tmp` entfernen |

`make image` ist der einzige unterstützte Weg, das Image zu bauen; CI oder
NAS-Deployment rufen dieselben Targets auf. Neue Standardaufgaben werden als
weitere Targets ergänzt, nicht als lose Shell-Kommandos dokumentiert.

## 11. Bewusst weggelassen (YAGNI)

- Kein Auth (LAN-only; bei Bedarf später Reverse-Proxy).
- Kein Transcoding/mp3-Konvertierung — „Nur Audio" lädt das Quellformat
  (m4a/opus); Konvertierung wäre ein späteres Feature.
- Kein Browser-Download der fertigen Dateien.
- Kein Multi-Arch-Build (nur amd64).
- Keine WebSockets/SSE — Polling reicht für den LAN-Anwendungsfall.

## 12. Nächster Schritt

Implementierungsplan über den `writing-plans`-Skill erstellen
(nach Nutzer-Review dieser Spec).
