# Frontend-Redesign: Bootstrap → eigenes Design (Tailwind v4 via Docker)

Datum: 2026-08-23 · Status: abgestimmt (Design + Ansatz vom Nutzer freigegeben)

## Kontext & Ziel

Die Web-UI (ein Template `web/templates/index.html`, ein `web/static/app.js`)
nutzt Bootstrap 5 im Dark-Mode und wirkt altbacken. Sie wird auf ein eigenes,
modernes Design umgestellt — Richtung **„Klar & Technisch“** (ruhig, präzise,
feine Linien, dezenter Blau-Akzent, im Stil moderner Dev-Tools wie
Linear/Vercel). Das Design wurde als Browser-Mockup abgestimmt und komplett
freigegeben (`.superpowers/brainstorm/…/content/full-design-a.html`).

**Rahmenbedingungen (vom Nutzer gesetzt):**

- Vanilla JS bleibt; kein React, kein Frontend-Framework, kein JS-Build.
- CSS-Tooling ausschließlich in Docker — lokal wird nichts installiert (kein npm).
- Gewählter CSS-Ansatz: **Tailwind CSS v4**, kompiliert in einem
  Docker-Build-Schritt.

## Entscheidungen

| Frage | Entscheidung |
|---|---|
| Umfang | Umstyling **plus** UX-Polish (Toasts, Job-Karten, Segmented Control, SVG-Icons) |
| Theme | Dunkel + Hell automatisch über `prefers-color-scheme`, kein manueller Umschalter |
| Design-Richtung | A — „Klar & Technisch“ |
| CSS-Technik | Tailwind v4 via Docker; generiertes `app.css` wird committet |
| Bootstrap | `bootstrap.min.css` und `bootstrap.bundle.min.js` werden gelöscht (JS-Bundle war ohnehin ungenutzt) |

## Nicht-Ziele

- Keine Backend-/API-Änderungen, keine neuen Features, keine Verhaltensänderung
  jenseits der vier UX-Punkte.
- Kein manueller Theme-Umschalter, keine Authentifizierung, kein Icon-Font.
- Kein JS-Bundling/-Minifying — `app.js` bleibt eine handgeschriebene Datei.

## Architektur

### Build-Pipeline

`web/static` wird per `go:embed` (in `web/embed.go`) ins Binary eingebettet —
das kompilierte CSS muss also **vor** dem Go-Build existieren.

1. **`web/src/input.css`** (neu): Tailwind-Einstiegspunkt.
   - `@import "tailwindcss";`
   - `@source`-Direktiven explizit auf `../templates/index.html` und
     `../static/app.js` (nur diese zwei Dateien erzeugen Klassen).
   - Semantische Design-Tokens als CSS-Variablen in `:root`, geflippt über
     `@media (prefers-color-scheme: light)`; Anbindung an Utilities über
     `@theme inline` (z. B. `--color-surface: var(--surface)` →
     `bg-surface`).
2. **Dockerfile, neue Stage `css`** (vor der Go-Stage):
   - `FROM node:22-alpine`, `@tailwindcss/cli` **versionsgepinnt** installieren
     (gleiches Prinzip wie das deno-Pinning: reproduzierbar, Versions-Bump
     invalidiert den Layer-Cache). Konkrete 4.x-Version wird bei der
     Implementierung per `npm view @tailwindcss/cli version` ermittelt und
     als `ARG` im Dockerfile wie im Makefile identisch gepinnt.
   - Kompiliert `web/src/input.css` → `/out/app.css` (`--minify`).
   - Die Go-Stage kopiert `/out/app.css` nach `web/static/app.css` **bevor**
     `go build` läuft — das Image baut sein CSS immer selbst frisch.
3. **Makefile:**
   - `make css` — einmalige Kompilierung via `docker run` (node-Image, gleiche
     gepinnte CLI-Version wie im Dockerfile).
   - `make css-watch` — Watch-Modus für die Entwicklung (Datei speichern →
     CSS wird neu erzeugt → Browser neu laden).
4. **`web/static/app.css` wird committet.** Begründung: `go:embed` bricht die
   Kompilierung ab, wenn die Datei fehlt — `go test` / `make run` müssen auf
   einem frischen Checkout ohne Docker funktionieren. Der Docker-Build
   überschreibt die Datei ohnehin immer mit frisch Kompiliertem. Nach jeder
   Frontend-Änderung gehört ein aktualisiertes `app.css` mit in den Commit.

### Design-Tokens

Alle Werte stammen aus dem freigegebenen Mockup. Semantische Namen; Utilities
referenzieren ausschließlich Tokens, nie Rohfarben.

| Token | Dunkel | Hell |
|---|---|---|
| `bg` (Seite) | `#0f1116` | `#f6f7f9` |
| `surface` (Karten) | `#151821` | `#ffffff` |
| `edge` (Karten-Rand) | `#232834` | `#e7eaef` |
| `edge-strong` (Inputs/Buttons) | `#2a303c` | `#d6dbe3` |
| `text` | `#e2e5ea` | `#1d2129` |
| `muted` | `#8b919d` | `#667085` |
| `dim` (Section-Titel, Platzhalter) | `#6f7683` | `#98a2b3` |
| `accent` | `#6c8cff` | `#4c6cf5` |
| `on-accent` (Text auf Akzent) | `#0d0f14` | `#ffffff` |
| `accent-soft` (aktives Segment, Hintergrund) | `#232c47` | `#e3e9fe` |
| `accent-soft-text` | `#aebfff` | `#3652c8` |
| `ok` | `#4ade80` | `#12924f` |
| `ok-edge` | `#1e4230` | `#b8e6cd` |
| `danger` | `#f87171` | `#b42318` |
| `danger-edge` | `#4a2626` | `#f4b6b0` |
| `warn` (Abgebrochen) | `#fbbf24` | `#b45309` |
| `warn-edge` | `#4a3a16` | `#f2d9a7` |
| `run-edge` (Pill „Lädt“) | `#2f3b63` | `#c9d4fd` |

Weitere Konstanten: Radius Karten `10px`, Inputs/Buttons `8px`, Pills rund;
Schrift `system-ui`-Stack; Zahlen (Tempo/ETA/Größe) mit
`font-variant-numeric: tabular-nums`; helle Karten mit dezentem Schatten
(`0 1px 2px rgba(16,24,40,0.04)`), dunkle ohne.

### Komponenten (index.html)

Struktur bleibt wie heute (gleiche IDs, gleiche Reihenfolge), nur Markup/Optik:

- **Header:** „yt-dl web“ (Akzent auf „web“), rechts Zahnrad-Button mit
  Inline-SVG statt ⚙️-Emoji.
- **Einstellungs-Karte** (auf-/zuklappbar wie bisher via `hidden`):
  Standard-Profil-Select, Trennlinie, yt-dlp-Versionszeile mit `code`-Badge
  und Ghost-Button „Jetzt aktualisieren“.
- **URL-Karte:** Label, Eingabefeld + Primär-Button „Analysieren“; auf Mobil
  bricht die Zeile um (Button volle Breite).
- **Auswahl-Karte:** Thumbnail (16:9, gerundet), Titel, Untertitel;
  **Segmented Control** für „Profil / Formate wählen / Nur Audio“ — die
  bestehenden Radio-Inputs (`name="mode"`) bleiben im DOM und funktional
  (Tastatur!), werden visuell versteckt (`sr-only`-Technik, **nicht**
  `display:none`), die Labels bilden die Segmente; darunter je nach Modus
  Profil-Select bzw. Format-Selects (zweispaltig, mobil einspaltig);
  Primär-Button „Download starten“.
- **Playlist-Variante:** wie heute (Profil-Select statt Formatwahl).
- **Downloads:** Section-Titel in Versalien; pro Job eine **Karte** statt
  Tabellenzeile. Grid: Titel | Status-Pill | Aktionen; darunter Meta-Zeile
  (Format · Tempo · ETA · %), optional Fehlerzeile (rot) + 403-Tipp,
  optional Fortschrittsbalken (4 px, volle Breite, nur bei `running`
  sichtbar mit Prozent in der Meta-Zeile statt im Balken).
- **Status-Pills** mit Farbpunkt: Wartet (muted), Lädt (accent), Fertig (ok),
  Fehler (danger), Abgebrochen (warn).
- **Toasts:** Container unten rechts (`role="status"`, `aria-live="polite"`),
  Einblendung mit kurzer Transition, Auto-Dismiss nach ~4 s, stapelbar.

### app.js-Änderungen

Logik, API-Aufrufe und Polling bleiben unangetastet. Es ändern sich nur
Darstellungs-Stellen:

- `show()`/`hide()` nutzen das native `hidden`-Attribut statt der
  Bootstrap-Klasse `d-none`. Damit das auch auf Elementen mit
  Display-Utilities (`flex`, `grid`) zuverlässig greift, kommt in
  `input.css` eine Regel `[hidden] { display: none !important; }`.
- `STATE_BADGES` → neue Pill-Klassen als **vollständige String-Literale**
  (Tailwind-Scanner findet nur Literale, keine zusammengesetzten Strings).
- `renderJobs()` erzeugt Job-Karten (`<div>`-Grid) statt `<tr>`-Zeilen;
  `actionButtons()` liefert Ghost-Buttons.
- Neues Toast-Modul (~20 Zeilen): `toast(text, kind)`; ersetzt die drei
  `alert()`-Aufrufe (Playlist-Skip-Hinweis, Settings-Fehler, Job-Aktions-Fehler).
- Der 403-Tipp-Text verweist statt „⚙️“ auf „Einstellungen“.

### Gelöschte Dateien

- `web/static/bootstrap.min.css`
- `web/static/bootstrap.bundle.min.js` (wurde von keinem Code genutzt)

## Fehlerbehandlung

Alle bestehenden Fehlerpfade bleiben erhalten:

- Probe-/Start-Fehler: rote Inline-Meldung in der jeweiligen Karte (wie
  bisher, neues Styling).
- Job-Fehler: rote Fehlerzeile in der Job-Karte, inkl. 403-Tipp.
- Aktions-/Settings-Fehler: Toast (Ersatz für `alert()`).
- Polling-Fehler: weiterhin still ignorieren.

## Verifikation

1. `make check` (gofmt, vet, go test) — muss unverändert grün sein; das
   Backend wird nicht angefasst.
2. `make css` erzeugt `app.css` ohne Fehler; Ergebnis committet.
3. `docker build` läuft durch (css-Stage + Go-Stage).
4. UI-Abnahme gegen das freigegebene Mockup: Analyse Video + Playlist, alle
   drei Modi der Segmented Control, alle fünf Job-Zustände, 403-Tipp,
   Toasts, Einstellungs-Panel inkl. Update-Button, hell/dunkel
   (Systemeinstellung), Mobil-Breite (~375 px).
5. README: Bootstrap-Erwähnung ersetzen, `make css`/`css-watch`
   dokumentieren, Hinweis dass `app.css` generiert ist.

## Risiken & Hinweise

- **Netzzugriff im Docker-Build:** Die css-Stage lädt `@tailwindcss/cli` aus
  dem npm-Registry (wie die deno-Stage von GitHub lädt) — offline baut das
  Image nicht.
- **Klassen-Scanning:** Dynamisch zusammengesetzte Klassennamen in JS würden
  im Build fehlen; Regel: Klassen immer als vollständige Literale schreiben.
- **`app.css` veraltet im Repo:** Wenn jemand HTML/JS ändert, ohne
  `make css` auszuführen, stimmt das committete CSS nicht mehr — im Image
  ist es trotzdem korrekt (wird dort frisch gebaut). Merkregel in README.
