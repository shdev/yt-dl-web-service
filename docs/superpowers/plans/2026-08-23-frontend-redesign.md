# Frontend-Redesign Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Bootstrap durch ein eigenes Design A („Klar & Technisch") ersetzen — Tailwind v4 via Docker, plus UX-Polish (Toasts, Job-Karten, Segmented Control, SVG-Icons).

**Architecture:** Die UI bleibt ein Go-Template (`web/templates/index.html`) + Vanilla-JS (`web/static/app.js`), eingebettet per `go:embed`. Neu: `web/src/input.css` (Tailwind-Einstieg mit Design-Tokens) wird per Docker zu `web/static/app.css` kompiliert — lokal über `make css`, im Image über eine eigene Build-Stage vor dem Go-Build. Das generierte `app.css` wird committet, damit `go test`/`make run` ohne Docker funktionieren.

**Tech Stack:** Go 1.24 (unverändert), Tailwind CSS v4 (`@tailwindcss/cli` 4.3.3, nur in Docker), Vanilla JS.

**Spec:** `docs/superpowers/specs/2026-08-23-frontend-redesign-design.md`

## Global Constraints

- Kein lokales npm/Node: CSS-Kompilierung ausschließlich via Docker (`make css` bzw. Dockerfile-Stage).
- Tailwind-Version überall identisch gepinnt: **4.3.3** (Makefile `TAILWIND_VERSION` und Dockerfile `ARG TAILWIND_VERSION` müssen übereinstimmen).
- Vanilla JS: kein Framework, kein JS-Build, keine neuen JS-Dateien außer den geplanten Änderungen in `app.js`.
- CSS-Klassen in HTML **und** JS nur als vollständige String-Literale (der Tailwind-Scanner findet keine zusammengesetzten Strings).
- UI-Texte auf Deutsch; bestehende IDs/API-Aufrufe/Polling-Logik in `app.js` bleiben unverändert, sofern ein Task nichts anderes sagt.
- Nach jeder Änderung an `index.html`, `app.js` oder `input.css`: `make css` ausführen und das regenerierte `web/static/app.css` mitcommitten.
- Nach jedem Task committen; `make check` (gofmt, vet, go test) muss nach jedem Task grün sein.

---

### Task 1: Tailwind-Build-Pipeline (`input.css`, `make css`, Embed-Test)

**Files:**
- Create: `web/src/input.css`
- Create (generiert): `web/static/app.css`
- Modify: `Makefile` (Variablen oben, neue Targets `css`/`css-watch`, `.PHONY`)
- Test: `web/embed_test.go`

**Interfaces:**
- Consumes: `web/embed.go` (`//go:embed templates static` — bettet alles unter `web/static/` ein, neue Dateien automatisch mit).
- Produces: Design-Token-Utilities (`bg-surface`, `text-muted`, `text-danger`, `text-ok`, `border-edge`, …) und Komponentenklassen (`.card`, `.job-card`, `.btn`, `.btn-primary`, `.btn-ghost`, `.btn-sm`, `.input`, `.select`, `.field-label`, `.pill`, `.pill-dot`, `.pill-wait/-run/-ok/-err/-warn`, `.section-title`, `.seg`, `.seg-label`, `.bar`, `.bar-fill`, `.toast-region`, `.toast`, `.toast-ok/-warn/-err`, `.toast-dot`) — Task 3/4 verwenden exakt diese Namen. `make css` als Build-Kommando.

- [ ] **Step 1: Embed-Test auf die neue Dateiliste umstellen (failing test)**

In `web/embed_test.go` die Dateiliste ersetzen:

```go
	files := []string{
		"templates/index.html",
		"static/app.js",
		"static/app.css",
	}
```

(`static/bootstrap.min.css` und `static/bootstrap.bundle.min.js` fliegen aus der Liste; gelöscht werden die Dateien erst in Task 3.)

- [ ] **Step 2: Test ausführen — muss fehlschlagen**

Run: `go test ./web/`
Expected: FAIL mit `static/app.css fehlt im Embed`

- [ ] **Step 3: `web/src/input.css` anlegen**

Vollständiger Inhalt:

```css
@import "tailwindcss";

@source "../templates/index.html";
@source "../static/app.js";

/* Design-Tokens (Spec §Design-Tokens): dunkel ist Default, hell folgt der
   Systemeinstellung. Utilities referenzieren ausschließlich diese Tokens. */
:root {
  --bg: #0f1116;
  --surface: #151821;
  --edge: #232834;
  --edge-strong: #2a303c;
  --text: #e2e5ea;
  --muted: #8b919d;
  --dim: #6f7683;
  --accent: #6c8cff;
  --on-accent: #0d0f14;
  --accent-soft: #232c47;
  --accent-soft-text: #aebfff;
  --ok: #4ade80;
  --ok-edge: #1e4230;
  --danger: #f87171;
  --danger-edge: #4a2626;
  --warn: #fbbf24;
  --warn-edge: #4a3a16;
  --run-edge: #2f3b63;
  --card-shadow: none;
}

@media (prefers-color-scheme: light) {
  :root {
    --bg: #f6f7f9;
    --surface: #ffffff;
    --edge: #e7eaef;
    --edge-strong: #d6dbe3;
    --text: #1d2129;
    --muted: #667085;
    --dim: #98a2b3;
    --accent: #4c6cf5;
    --on-accent: #ffffff;
    --accent-soft: #e3e9fe;
    --accent-soft-text: #3652c8;
    --ok: #12924f;
    --ok-edge: #b8e6cd;
    --danger: #b42318;
    --danger-edge: #f4b6b0;
    --warn: #b45309;
    --warn-edge: #f2d9a7;
    --run-edge: #c9d4fd;
    --card-shadow: 0 1px 2px rgba(16, 24, 40, 0.04);
  }
}

@theme inline {
  --color-bg: var(--bg);
  --color-surface: var(--surface);
  --color-edge: var(--edge);
  --color-edge-strong: var(--edge-strong);
  --color-text: var(--text);
  --color-muted: var(--muted);
  --color-dim: var(--dim);
  --color-accent: var(--accent);
  --color-on-accent: var(--on-accent);
  --color-accent-soft: var(--accent-soft);
  --color-accent-soft-text: var(--accent-soft-text);
  --color-ok: var(--ok);
  --color-ok-edge: var(--ok-edge);
  --color-danger: var(--danger);
  --color-danger-edge: var(--danger-edge);
  --color-warn: var(--warn);
  --color-warn-edge: var(--warn-edge);
  --color-run-edge: var(--run-edge);
  --font-sans: system-ui, -apple-system, "Segoe UI", sans-serif;
}

@layer base {
  /* show()/hide() in app.js nutzen das hidden-Attribut — muss auch gegen
     Display-Utilities (flex/grid) gewinnen. */
  [hidden] {
    display: none !important;
  }
  body {
    background: var(--bg);
    color: var(--text);
    -webkit-font-smoothing: antialiased;
  }
  button {
    cursor: pointer;
  }
  :focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 1px;
  }
}

@layer components {
  .card {
    @apply rounded-[10px] border border-edge bg-surface p-[18px];
    box-shadow: var(--card-shadow);
  }
  .job-card {
    @apply rounded-[10px] border border-edge bg-surface px-4 py-3;
    box-shadow: var(--card-shadow);
  }
  .field-label {
    @apply mb-1.5 block text-xs font-medium text-muted;
  }
  .input,
  .select {
    @apply w-full rounded-lg border border-edge-strong bg-bg px-3 py-2 text-sm text-text;
  }
  .input::placeholder {
    @apply text-dim;
  }
  .select {
    @apply appearance-none bg-no-repeat pr-9;
    background-image: url("data:image/svg+xml;charset=utf-8,%3Csvg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 16 16' fill='none'%3E%3Cpath d='M4 6l4 4 4-4' stroke='%23798093' stroke-width='1.5' stroke-linecap='round' stroke-linejoin='round'/%3E%3C/svg%3E");
    background-position: right 0.6rem center;
  }
  .btn {
    @apply inline-flex items-center justify-center gap-1.5 rounded-lg px-4 py-2 text-sm font-semibold whitespace-nowrap;
  }
  .btn:disabled {
    opacity: 0.6;
    pointer-events: none;
  }
  .btn-primary {
    @apply bg-accent text-on-accent;
  }
  .btn-primary:hover {
    filter: brightness(1.08);
  }
  .btn-ghost {
    @apply border border-edge-strong bg-transparent font-medium text-muted;
  }
  .btn-ghost:hover {
    @apply text-text;
  }
  .btn-sm {
    @apply rounded-[7px] px-2.5 py-1 text-xs;
  }
  .pill {
    @apply inline-flex items-center gap-1.5 rounded-full border px-2.5 py-0.5 text-[11.5px] font-medium whitespace-nowrap;
  }
  .pill-dot {
    @apply size-1.5 rounded-full bg-current;
  }
  .pill-wait {
    @apply border-edge-strong text-muted;
  }
  .pill-run {
    @apply border-run-edge text-accent-soft-text;
  }
  .pill-ok {
    @apply border-ok-edge text-ok;
  }
  .pill-err {
    @apply border-danger-edge text-danger;
  }
  .pill-warn {
    @apply border-warn-edge text-warn;
  }
  .section-title {
    @apply text-[11.5px] font-semibold tracking-[0.09em] uppercase text-dim;
  }
  .seg {
    @apply inline-flex rounded-lg border border-edge-strong bg-bg p-[3px];
  }
  .seg-label {
    @apply cursor-pointer rounded-md px-3.5 py-1.5 text-[12.5px] font-medium text-muted select-none;
  }
  .seg input:checked + .seg-label {
    background: var(--accent-soft);
    color: var(--accent-soft-text);
  }
  .seg input:focus-visible + .seg-label {
    outline: 2px solid var(--accent);
    outline-offset: 1px;
  }
  .bar {
    @apply h-1 w-full overflow-hidden rounded-full bg-edge;
  }
  .bar-fill {
    @apply block h-full rounded-full bg-accent;
  }
  .toast-region {
    @apply fixed right-5 bottom-5 z-50 flex flex-col items-end gap-2;
  }
  .toast {
    @apply flex items-center gap-2 rounded-[9px] border border-edge-strong bg-surface px-3.5 py-2.5 text-[12.5px] text-text shadow-lg;
    animation: toast-in 200ms ease-out;
  }
  .toast-dot {
    @apply size-[7px] shrink-0 rounded-full;
  }
  .toast-ok .toast-dot {
    background: var(--ok);
  }
  .toast-warn .toast-dot {
    background: var(--warn);
  }
  .toast-err .toast-dot {
    background: var(--danger);
  }
}

@keyframes toast-in {
  from {
    opacity: 0;
    transform: translateY(4px);
  }
}
```

- [ ] **Step 4: Makefile erweitern**

Oben bei den Variablen (nach `TAG ?= latest`):

```make
# Muss mit ARG TAILWIND_VERSION im Dockerfile übereinstimmen.
TAILWIND_VERSION := 4.3.3
```

`.PHONY`-Zeile um `css css-watch` ergänzen. Nach dem `clean`-Target anfügen:

```make
# Tailwind-CSS via Docker kompilieren — kein lokales npm nötig.
# Das npm-Cache-Volume erspart den CLI-Download bei jedem Aufruf.
# Der Symlink nach /node_modules ist nötig, weil der v4-Resolver das Paket
# "tailwindcss" vom Verzeichnis der Input-Datei aufwärts sucht.
TAILWIND_RUN = npm install -g @tailwindcss/cli@$(TAILWIND_VERSION) >/dev/null \
	&& ln -s /usr/local/lib/node_modules/@tailwindcss/cli/node_modules /node_modules \
	&& cd /work && tailwindcss -i web/src/input.css -o web/static/app.css

css:
	docker run --rm -v $(CURDIR):/work -v ytdlweb-npm-cache:/root/.npm \
		node:22-alpine sh -c "$(TAILWIND_RUN) --minify"

css-watch:
	docker run --rm -it -v $(CURDIR):/work -v ytdlweb-npm-cache:/root/.npm \
		node:22-alpine sh -c "$(TAILWIND_RUN) --watch"
```

- [ ] **Step 5: CSS erzeugen und prüfen**

Run: `make css`
Expected: `web/static/app.css` existiert; `grep -c "color-mix\|--bg" web/static/app.css` bzw. `grep -c "0f1116" web/static/app.css` liefert ≥ 1 (Token angekommen). Hinweis: Utilities wie `bg-surface` tauchen erst auf, wenn HTML/JS sie benutzen (Task 3) — in diesem Task genügen Tokens + Komponentenklassen (`.card` usw. sind immer im Output).

- [ ] **Step 6: Test ausführen — muss bestehen**

Run: `go test ./web/`
Expected: PASS

- [ ] **Step 7: Sicherstellen, dass `app.css` nicht ignoriert wird**

Run: `git check-ignore web/static/app.css || echo "ok: wird getrackt"`
Expected: `ok: wird getrackt`

- [ ] **Step 8: Commit**

```bash
git add web/src/input.css web/static/app.css web/embed_test.go Makefile
git commit -m "feat: Tailwind-v4-Pipeline — input.css mit Design-Tokens, make css via Docker"
```

---

### Task 2: Dockerfile — css-Stage vor dem Go-Build

**Files:**
- Modify: `Dockerfile` (neue Stage `css` an den Anfang; Go-Stage kopiert das Ergebnis)

**Interfaces:**
- Consumes: `web/src/input.css` aus Task 1.
- Produces: Docker-Image, dessen eingebettetes `app.css` immer frisch im Image gebaut wurde (committeter Stand egal).

- [ ] **Step 1: css-Stage einfügen**

Ganz an den Anfang des `Dockerfile` (vor „Stage 1: Go-Build“):

```dockerfile
# Stage 0: Tailwind-CSS-Build — erzeugt app.css frisch im Image, damit ein
# veralteter committeter Stand nie ins Binary gelangt.
# Gepinnt (wie deno weiter unten): reproduzierbar, Bump invalidiert den Cache.
# Muss mit TAILWIND_VERSION im Makefile übereinstimmen.
FROM node:22-alpine AS css
ARG TAILWIND_VERSION=4.3.3
# Symlink: der v4-Resolver sucht das Paket "tailwindcss" vom Verzeichnis der
# Input-Datei aufwärts — /node_modules liegt auf diesem Pfad.
RUN npm install -g @tailwindcss/cli@${TAILWIND_VERSION} \
 && ln -s /usr/local/lib/node_modules/@tailwindcss/cli/node_modules /node_modules
WORKDIR /src
COPY web/ web/
RUN tailwindcss -i web/src/input.css -o /out/app.css --minify
```

In der Go-Build-Stage nach `COPY . .` und vor `RUN CGO_ENABLED=0 go build …` einfügen:

```dockerfile
COPY --from=css /out/app.css web/static/app.css
```

- [ ] **Step 2: Image bauen — muss durchlaufen**

Run: `make image-native`
Expected: Build erfolgreich; im Log erscheint die css-Stage mit `tailwindcss -i web/src/input.css`.

- [ ] **Step 3: Stichprobe — ausgeliefertes CSS kommt aus dem Image-Build**

```bash
docker run --rm -d -p 18080:8080 -e YTDLP_UPDATE_ON_START=false \
  -v "$(mktemp -d)":/downloads -v "$(mktemp -d)":/config \
  --name css-smoke yt-dl-web-service
sleep 2
curl -fsS http://localhost:18080/static/app.css | head -c 200
docker rm -f css-smoke
```

Expected: CSS-Inhalt (beginnt mit Tailwind-Preamble/Minified-CSS), HTTP 200.

- [ ] **Step 4: Commit**

```bash
git add Dockerfile
git commit -m "feat: Dockerfile — Tailwind-css-Stage baut app.css frisch vor dem Go-Build"
```

---

### Task 3: UI-Umbau — index.html neu, app.js-Darstellung, Bootstrap raus

Ein Task, weil Markup und sein JS-Renderer nur zusammen funktionieren; committet wird erst am Ende des Tasks, wenn beides konsistent ist.

**Files:**
- Modify: `web/templates/index.html` (kompletter Ersatz)
- Modify: `web/static/app.js` (nur Darstellungs-Stellen; API/Polling unverändert)
- Delete: `web/static/bootstrap.min.css`, `web/static/bootstrap.bundle.min.js`
- Modify (generiert): `web/static/app.css` (via `make css`)

**Interfaces:**
- Consumes: Komponentenklassen/Token-Utilities aus Task 1 (exakte Namen siehe dort).
- Produces: neue Element-IDs `jobs-list`, `jobs-empty`, `toast-region` (Task 4 nutzt `toast-region`); alle übrigen IDs unverändert (`settings-btn`, `settings-card`, `default-profile`, `settings-saved`, `ytdlp-version`, `ytdlp-update-btn`, `ytdlp-update-status`, `url-input`, `probe-btn`, `probe-error`, `select-card`, `video-thumb`, `select-title`, `select-subtitle`, `video-options`, `mode-profile`, `mode-manual`, `mode-audio`, `video-profile-wrap`, `video-profile`, `format-selects`, `video-col`, `video-format`, `audio-format`, `playlist-options`, `profile-select`, `start-btn`, `start-error`).

- [ ] **Step 1: `web/templates/index.html` komplett ersetzen**

Vollständiger neuer Inhalt (Go-Template-Blöcke `{{range .Profiles}}` bleiben exakt wie gehabt):

```html
<!doctype html>
<html lang="de">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>yt-dl Web</title>
  <link rel="stylesheet" href="/static/app.css">
</head>
<body>
  <div class="mx-auto max-w-[860px] px-4 py-7 sm:px-6">
    <header class="mb-6 flex items-center justify-between">
      <h1 class="text-[17px] font-semibold tracking-tight">yt-dl <span class="text-accent">web</span></h1>
      <button class="btn btn-ghost size-8 rounded-[7px] p-0" id="settings-btn" type="button" title="Einstellungen" aria-label="Einstellungen">
        <svg class="size-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
          <circle cx="12" cy="12" r="3"/>
          <path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 1 1-4 0v-.09a1.65 1.65 0 0 0-1-1.51 1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 1 1 0-4h.09a1.65 1.65 0 0 0 1.51-1 1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06a1.65 1.65 0 0 0 1.82.33h0a1.65 1.65 0 0 0 1-1.51V3a2 2 0 1 1 4 0v.09a1.65 1.65 0 0 0 1 1.51h0a1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82v0a1.65 1.65 0 0 0 1.51 1H21a2 2 0 1 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z"/>
        </svg>
      </button>
    </header>

    <section class="card mb-4" id="settings-card" hidden>
      <div class="grid gap-4">
        <div>
          <label class="field-label" for="default-profile">Standard-Profil (Vorauswahl nach jeder Analyse)</label>
          <select class="select" id="default-profile">
            {{range .Profiles}}<option value="{{.Key}}">{{.Label}}</option>
            {{end}}</select>
          <div class="mt-1.5 text-xs text-ok" id="settings-saved" hidden>Gespeichert ✓</div>
        </div>
        <div class="h-px bg-edge"></div>
        <div class="flex flex-wrap items-center gap-2.5 text-sm text-muted">
          <span>yt-dlp-Version: <code class="rounded-md border border-edge-strong bg-bg px-2 py-0.5 font-mono text-xs text-text" id="ytdlp-version">…</code></span>
          <button class="btn btn-ghost btn-sm" id="ytdlp-update-btn" type="button">Jetzt aktualisieren</button>
        </div>
        <div class="text-xs text-muted" id="ytdlp-update-status" hidden></div>
      </div>
    </section>

    <section class="card mb-4">
      <label class="field-label" for="url-input">Video- oder Playlist-URL</label>
      <div class="flex flex-col gap-2 sm:flex-row">
        <input type="url" class="input" id="url-input" placeholder="https://…" autofocus>
        <button class="btn btn-primary" id="probe-btn">Analysieren</button>
      </div>
      <div class="mt-3 text-sm text-danger" id="probe-error" hidden></div>
    </section>

    <section class="card mb-4" id="select-card" hidden>
      <div class="mb-4 flex gap-3.5">
        <img id="video-thumb" src="" alt="" class="aspect-video w-[148px] self-start rounded-lg object-cover" hidden>
        <div class="min-w-0">
          <h2 class="text-[14.5px] font-semibold" id="select-title"></h2>
          <div class="text-[12.5px] text-muted" id="select-subtitle"></div>
        </div>
      </div>

      <div id="video-options">
        <div class="seg mb-4" role="radiogroup" aria-label="Formatwahl-Modus">
          <input class="sr-only" type="radio" name="mode" id="mode-profile" value="profile" checked>
          <label class="seg-label" for="mode-profile">Profil</label>
          <input class="sr-only" type="radio" name="mode" id="mode-manual" value="manual">
          <label class="seg-label" for="mode-manual">Formate wählen</label>
          <input class="sr-only" type="radio" name="mode" id="mode-audio" value="audio">
          <label class="seg-label" for="mode-audio">Nur Audio</label>
        </div>
        <div class="mb-4" id="video-profile-wrap">
          <label class="field-label" for="video-profile">Qualitätsprofil</label>
          <select class="select" id="video-profile">
            {{range .Profiles}}<option value="{{.Key}}">{{.Label}}</option>
            {{end}}</select>
        </div>
        <div class="grid gap-4 sm:grid-cols-2" id="format-selects" hidden>
          <div id="video-col">
            <label class="field-label" for="video-format">Videoformat</label>
            <select class="select" id="video-format"></select>
          </div>
          <div>
            <label class="field-label" for="audio-format">Audioformat</label>
            <select class="select" id="audio-format"></select>
          </div>
        </div>
      </div>

      <div id="playlist-options" hidden>
        <label class="field-label" for="profile-select">Qualitätsprofil</label>
        <select class="select" id="profile-select">
          {{range .Profiles}}<option value="{{.Key}}">{{.Label}}</option>
          {{end}}</select>
      </div>

      <button class="btn btn-primary mt-4" id="start-btn">Download starten</button>
      <div class="mt-3 text-sm text-danger" id="start-error" hidden></div>
    </section>

    <h2 class="section-title mx-0.5 mt-6 mb-2.5">Downloads</h2>
    <div class="text-sm text-dim" id="jobs-empty">Noch keine Downloads</div>
    <div class="grid gap-2" id="jobs-list"></div>
  </div>

  <div class="toast-region" id="toast-region" role="status" aria-live="polite"></div>
  <script src="/static/app.js"></script>
</body>
</html>
```

- [ ] **Step 2: `app.js` — `show`/`hide` auf das `hidden`-Attribut umstellen**

Ersetzen:

```js
function show(el) { el.classList.remove("d-none"); }
function hide(el) { el.classList.add("d-none"); }
```

durch:

```js
function show(el) { el.hidden = false; }
function hide(el) { el.hidden = true; }
```

- [ ] **Step 3: `app.js` — Settings-Toggle ohne `d-none`**

Ersetzen:

```js
$("settings-btn").addEventListener("click", () => {
  $("settings-card").classList.toggle("d-none");
  if (!$("settings-card").classList.contains("d-none")) loadYtdlpVersion();
});
```

durch:

```js
$("settings-btn").addEventListener("click", () => {
  const card = $("settings-card");
  card.hidden = !card.hidden;
  if (!card.hidden) loadYtdlpVersion();
});
```

- [ ] **Step 4: `app.js` — Statusfarbe des Update-Status als vollständige Klassen-Literale**

In `loadYtdlpVersion()` ersetzen:

```js
    const status = $("ytdlp-update-status");
    status.classList.add("text-danger");
```

durch:

```js
    const status = $("ytdlp-update-status");
    status.className = "text-xs text-danger";
```

Im Click-Handler von `ytdlp-update-btn` ersetzen:

```js
  btn.disabled = true;
  status.classList.remove("text-danger");
```

durch:

```js
  btn.disabled = true;
  status.className = "text-xs text-muted";
```

und im `catch` dort ersetzen:

```js
    status.classList.add("text-danger");
```

durch:

```js
    status.className = "text-xs text-danger";
```

- [ ] **Step 5: `app.js` — Job-Rendering auf Karten umstellen**

Den Block ab `const STATE_BADGES = {` bis zum Ende von `actionButtons(j)` (inklusive) ersetzen durch:

```js
const STATE_PILLS = {
  queued:   ["pill pill-wait", "Wartet"],
  running:  ["pill pill-run", "Lädt"],
  done:     ["pill pill-ok", "Fertig"],
  error:    ["pill pill-err", "Fehler"],
  canceled: ["pill pill-warn", "Abgebrochen"],
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
  $("jobs-empty").hidden = jobs.length > 0;
  $("jobs-list").innerHTML = jobs.map((j) => {
    const [pill, label] = STATE_PILLS[j.state] || ["pill pill-wait", esc(j.state)];
    const pct = Math.round(j.progress?.percent || 0);
    let title = esc(j.title || j.url);
    if (j.playlist_title) {
      title += ` <span class="font-normal text-muted">(${esc(j.playlist_title)})</span>`;
    }
    const meta = [
      j.format_label,
      j.progress?.speed,
      j.progress?.eta ? `ETA ${j.progress.eta}` : "",
      j.state === "running" ? `${pct} %` : "",
    ].filter(Boolean).map(esc).join(" · ");
    let extra = j.error
      ? `<div class="col-span-full text-xs text-danger">${esc(j.error)}</div>` : "";
    // Bekanntes Muster (z.B. yt-dlp#17456): 403 heißt fast immer, dass
    // yt-dlp veraltet ist — direkt zur Abhilfe verlinken.
    if (j.error && /HTTP Error 403|403: Forbidden/i.test(j.error)) {
      extra += `<div class="col-span-full text-xs text-muted">Tipp: yt-dlp über
        Einstellungen → „Jetzt aktualisieren“ auf den neuesten Stand bringen und den Job erneut starten.</div>`;
    }
    if (j.state === "running") {
      extra += `<div class="col-span-full bar" role="progressbar" aria-valuenow="${pct}"
        aria-valuemin="0" aria-valuemax="100"><span class="bar-fill" style="width:${pct}%"></span></div>`;
    }
    return `<div class="job-card grid grid-cols-[minmax(0,1fr)_auto] items-center gap-x-3.5 gap-y-1.5 sm:grid-cols-[minmax(0,1fr)_auto_auto]">
      <div class="truncate text-sm font-medium">${title}</div>
      <span class="${pill} justify-self-end"><i class="pill-dot"></i>${label}</span>
      <div class="col-span-2 flex gap-1.5 justify-self-start sm:col-span-1 sm:justify-self-end">${actionButtons(j)}</div>
      ${meta ? `<div class="col-span-full text-xs text-muted tabular-nums">${meta}</div>` : ""}
      ${extra}
    </div>`;
  }).join("");
}

function actionButtons(j) {
  const btn = (action, label) =>
    `<button class="btn btn-ghost btn-sm" data-action="${action}" data-id="${j.id}">${label}</button>`;
  if (j.state === "queued" || j.state === "running") {
    return btn("cancel", "Abbrechen");
  }
  const parts = [];
  if (j.state === "error" || j.state === "canceled") {
    parts.push(btn("retry", "Erneut"));
  }
  parts.push(btn("delete", "Entfernen"));
  return parts.join(" ");
}
```

(Hinweis: `refreshJobs` bleibt inhaltlich unverändert und steht nur deshalb im Block, weil er zwischen den ersetzten Funktionen liegt.)

- [ ] **Step 6: `app.js` — Event-Delegation auf `jobs-list` umziehen**

Ersetzen:

```js
$("jobs-tbody").addEventListener("click", async (e) => {
```

durch:

```js
$("jobs-list").addEventListener("click", async (e) => {
```

- [ ] **Step 7: Bootstrap-Dateien löschen**

```bash
git rm web/static/bootstrap.min.css web/static/bootstrap.bundle.min.js
```

- [ ] **Step 8: CSS regenerieren**

Run: `make css`
Expected: läuft fehlerfrei; `grep -c "job-card" web/static/app.css` ≥ 1, `grep -c "max-w-\[860px\]" web/static/app.css` ≥ 1 (Utilities aus HTML/JS angekommen).

- [ ] **Step 9: Backend-Checks und Smoke-Test**

Run: `make check`
Expected: PASS (gofmt, vet, alle Go-Tests — inkl. Embed-Test ohne Bootstrap).

```bash
make build && mkdir -p tmp/downloads tmp/config
PORT=18081 DOWNLOAD_DIR=tmp/downloads CONFIG_DIR=tmp/config \
  YTDLP_UPDATE_ON_START=false ./bin/app & APP_PID=$!
sleep 1
curl -fsS http://localhost:18081/ | grep -c "app.css"          # ≥ 1
curl -fsS http://localhost:18081/ | grep -ci "bootstrap" || true # 0 Treffer
curl -fsS -o /dev/null -w "%{http_code}\n" http://localhost:18081/static/app.css  # 200
kill $APP_PID
```

- [ ] **Step 10: Commit**

```bash
git add web/templates/index.html web/static/app.js web/static/app.css
git commit -m "feat: UI-Redesign — Design A statt Bootstrap (Karten, Pills, Segmented Control)"
```

---

### Task 4: Toast-Modul statt alert()

**Files:**
- Modify: `web/static/app.js`
- Modify (generiert): `web/static/app.css` (via `make css`)

**Interfaces:**
- Consumes: `#toast-region` aus Task 3; CSS-Klassen `.toast`, `.toast-ok`, `.toast-warn`, `.toast-err`, `.toast-dot` aus Task 1.
- Produces: `toast(text, kind)` mit `kind ∈ "ok" | "warn" | "error"` (Default `"ok"`).

- [ ] **Step 1: Toast-Funktion einfügen**

In `app.js` direkt nach `function esc(…) {…}` einfügen:

```js
// Dezente Rückmeldung unten rechts statt alert(); verschwindet nach 4 s.
function toast(text, kind = "ok") {
  const cls = { ok: "toast toast-ok", warn: "toast toast-warn", error: "toast toast-err" };
  const el = document.createElement("div");
  el.className = cls[kind] || cls.ok;
  const dot = document.createElement("i");
  dot.className = "toast-dot";
  el.append(dot, document.createTextNode(text));
  $("toast-region").append(el);
  setTimeout(() => el.remove(), 4000);
}
```

- [ ] **Step 2: Die drei `alert()`-Aufrufe ersetzen und Start-Feedback ergänzen**

1. Settings-Fehler — ersetzen:

```js
  } catch (err) {
    alert(err.message);
    $("default-profile").value = currentSettings.default_profile;
  }
```

durch:

```js
  } catch (err) {
    toast(err.message, "error");
    $("default-profile").value = currentSettings.default_profile;
  }
```

2. Playlist-Start — ersetzen:

```js
      hide($("select-card"));
      $("url-input").value = "";
      await refreshJobs();
      if (res && res.skipped > 0) {
        alert(`${res.skipped} Eintrag/Einträge übersprungen (bereits in der Warteschlange oder ohne URL).`);
      }
```

durch:

```js
      hide($("select-card"));
      $("url-input").value = "";
      await refreshJobs();
      toast("Download gestartet");
      if (res && res.skipped > 0) {
        toast(`${res.skipped} Eintrag/Einträge übersprungen (bereits in der Warteschlange oder ohne URL).`, "warn");
      }
```

3. Einzelvideo-Start — ersetzen:

```js
      await api("/api/jobs", { method: "POST", body: JSON.stringify(payload) });
      hide($("select-card"));
      $("url-input").value = "";
      await refreshJobs();
```

durch:

```js
      await api("/api/jobs", { method: "POST", body: JSON.stringify(payload) });
      hide($("select-card"));
      $("url-input").value = "";
      await refreshJobs();
      toast("Download gestartet");
```

4. Job-Aktions-Fehler — ersetzen:

```js
  } catch (err) {
    alert(err.message);
  }
```

durch:

```js
  } catch (err) {
    toast(err.message, "error");
  }
```

- [ ] **Step 3: Kein alert() mehr vorhanden**

Run: `grep -n "alert(" web/static/app.js`
Expected: keine Treffer.

- [ ] **Step 4: CSS regenerieren, Checks**

Run: `make css && make check`
Expected: beides PASS.

- [ ] **Step 5: Commit**

```bash
git add web/static/app.js web/static/app.css
git commit -m "feat: Toasts statt alert() — Start-Feedback, Warnungen und Fehler unten rechts"
```

---

### Task 5: README aktualisieren

**Files:**
- Modify: `README.md`

**Interfaces:**
- Consumes: `make css`/`make css-watch` aus Task 1.
- Produces: —

- [ ] **Step 1: Bootstrap-Erwähnungen ersetzen**

1. Feature-Liste — ersetzen:

```markdown
- Single container: one Go binary with an embedded Bootstrap UI, no CDN, UI works offline
```

durch:

```markdown
- Single container: one Go binary with an embedded UI (custom Tailwind-built CSS), no CDN, UI works offline
```

2. Architektur-Abschnitt — ersetzen:

```markdown
- The UI (Bootstrap 5, vanilla JS, German) polls `/api/jobs` every 1.5 s.
```

durch:

```markdown
- The UI (vanilla JS, Tailwind CSS, German) polls `/api/jobs` every 1.5 s.
```

3. Lizenz-Abschnitt — ersetzen:

```markdown
Bundled components keep their own licenses: Bootstrap (MIT, vendored),
yt-dlp (Unlicense, pulled at image build/start time).
```

durch:

```markdown
Bundled components keep their own licenses: Tailwind CSS (MIT, generated
stylesheet), yt-dlp (Unlicense, pulled at image build/start time).
```

- [ ] **Step 2: Development-Abschnitt ergänzen**

Vor dem `## License`-Abschnitt einfügen:

```markdown
## Development

`web/static/app.css` is generated from `web/src/input.css` with Tailwind CSS —
do not edit it by hand. After changing HTML, JS or `input.css`, rebuild it:

    make css        # one-shot build (runs in Docker, no local npm needed)
    make css-watch  # rebuild on every change while styling

The generated file is committed so `go test` and `make run` work without
Docker. The Docker image builds its own fresh CSS in a dedicated build stage,
so a stale committed `app.css` never ends up in the image.
```

- [ ] **Step 3: Kein Bootstrap-Rest mehr**

Run: `grep -rn -i bootstrap README.md web/`
Expected: keine Treffer.

- [ ] **Step 4: Commit**

```bash
git add README.md
git commit -m "docs: README — Tailwind-Pipeline statt Bootstrap, make css dokumentiert"
```

---

### Task 6: End-to-End-Verifikation gegen das freigegebene Mockup

**Files:**
- Keine Änderungen erwartet; nur Fixes, falls die Verifikation Abweichungen zeigt (dann jeweils `make css` + Commit).

**Interfaces:**
- Consumes: alles Vorherige; `make start` (baut Image, öffnet Browser).

- [ ] **Step 1: Frischer Container**

Run: `make start`
Expected: Build ok, Browser öffnet `http://localhost:8080`, neue Optik sichtbar (dunkler Hintergrund `#0f1116`, keine Bootstrap-Reste).

- [ ] **Step 2: Job-Zustände mit Fixtures prüfen (Chrome DevTools MCP)**

Auf `http://localhost:8080` per `evaluate_script` ausführen — stoppt das Polling und rendert alle fünf Zustände:

```js
window.refreshJobs = async () => {};
renderJobs([
  { id: "1", state: "running", title: "Kurzgesagt – Black Holes Explained", format_label: "1080p av01 + opus", progress: { percent: 58, speed: "4.2MiB/s", eta: "01:12" } },
  { id: "2", state: "queued", title: "Lo-fi beats to relax/study to", playlist_title: "Chill Mix", format_label: "≤720p" },
  { id: "3", state: "error", title: "Some old concert recording", format_label: "best", error: "ERROR: unable to download video data: HTTP Error 403: Forbidden" },
  { id: "4", state: "done", title: "How Trains Actually Work", format_label: "2160p vp9 + opus" },
  { id: "5", state: "canceled", title: "Abgebrochenes Video", format_label: "best" },
]);
```

Screenshot; prüfen: fünf Karten, Pills (Lädt/Wartet/Fehler/Fertig/Abgebrochen) farblich korrekt, Fortschrittsbalken nur beim laufenden Job, 403-Tipp unter dem Fehler.

- [ ] **Step 3: Auswahl-Karte mit Fixture prüfen**

```js
probeResult = { type: "video", video: { title: "Demo-Video für die Abnahme", thumbnail: "", formats: [
  { format_id: "137", vcodec: "avc1", ext: "mp4", resolution: "1920x1080", fps: 30, tbr: 4000, filesize: 250000000 },
  { format_id: "140", acodec: "mp4a", vcodec: "none", abr: 128, ext: "m4a", tbr: 128 },
] } };
renderSelectCard();
```

Screenshot; prüfen: Segmented Control („Profil“ aktiv), Wechsel auf „Formate wählen“ zeigt zwei Selects, „Nur Audio“ nur das Audio-Select; Tastatur: Pfeiltasten wechseln den Modus.

- [ ] **Step 4: Einstellungen, Toast, Mobil, Hell**

1. Zahnrad klicken → Einstellungs-Karte mit Versions-Badge und „Jetzt aktualisieren“.
2. `toast("Download gestartet"); toast("Testwarnung", "warn"); toast("Testfehler", "error");` → drei gestapelte Toasts unten rechts, verschwinden nach 4 s.
3. Fenster auf 375 px Breite → URL-Zeile bricht um, Job-Karten einspaltig, kein horizontales Scrollen.
4. Helles Schema (DevTools-Emulation `prefers-color-scheme: light` oder macOS-Systemeinstellung) → helle Palette gemäß Spec-Tokens.

- [ ] **Step 5: Abschluss**

Run: `make check`
Expected: PASS. Danach Abnahme durch den Nutzer (Design-Vergleich mit Mockup); Abweichungen fixen (`make css`, Commit), sonst fertig.
