# Spec: Direkt-Download, Neu wählen, URL kopieren, URL leeren

Datum: 2026-10-03 · Status: Design vom Nutzer freigegeben

## Kontext & Ziele

Vier zusammengehörige Erweiterungen der Bedienung, weil sie sich in der
URL-Karte und in den Job-Karten berühren:

1. **Direkt-Download** mit Profil, ohne Analyse-Schritt.
2. **„Neu wählen“** bei gescheiterten oder abgebrochenen Einträgen.
3. **URL-Zeile mit Kopier-Button** an jedem Eintrag.
4. **Button zum Leeren** der URL-Zeile.

Ziel: weniger Klicks bis zum Download, gescheiterte Downloads mit anderen
Einstellungen wiederholen, die verwendete URL sehen und kopieren können.
Zielplattformen: iOS Safari und macOS Chrome, auch wenn die App per
`http://` über eine LAN-IP läuft (dort ist `navigator.clipboard` nicht
verfügbar).

## Entscheidungen

| Frage | Entscheidung |
|---|---|
| Direkt-Download | Neuer Job-Typ `direct`; Analyse läuft als Vorschritt in der Queue |
| Tonspuren im Direkt-Download | Automatisch Deutsch + Original (`playlistFormat`), wie heute bei Playlists |
| Neu wählen | Ersetzt den gescheiterten Eintrag erst nach erfolgreichem Anlegen des neuen Jobs (`replace`) |
| Bindung der Verknüpfung | An die URL: Änderung oder Leeren des Feldes löst sie |
| Kopieren | Eine Funktion `copyText` mit Fallback über `execCommand`; Dateiname nutzt dieselbe |
| Autostart über Teilen-Menü | Unverändert (Analyse) |

## Architektur

### A. Direkt-Download

**UI.** In der URL-Karte neben „Analysieren“: Profil-Select
`#direct-profile` (gleiche Optionen wie die vorhandenen Profil-Selects,
vorbelegt mit dem Standardprofil, sobald die Settings geladen sind, und
aktualisiert, wenn das Standardprofil in den Einstellungen geändert wird)
und Button `#direct-btn` „Download“. Auf schmalen Bildschirmen: Eingabefeld
volle Breite, darunter Select und Buttons in einer umbrechenden Zeile.

**Ablauf.** Klick → `POST /api/jobs` mit `{type:"direct", url, profile,
replace?}`. Bei 201: Eingabefeld leeren, offene Auswahlkarte schließen, Toast
„Download hinzugefügt“, Job-Liste aktualisieren. Fehler (400/409/Netz) an
derselben Stelle anzeigen wie Analyse-Fehler (`#probe-error`). Beide Buttons
(`#probe-btn`, `#direct-btn`) sind während des Requests deaktiviert.

**API `type:"direct"`.** `url` ist Pflicht (400), `profile` ist Pflicht und
muss existieren (400). Der Job wird sofort angelegt mit `Title=""`,
`Format`/`MultiAudio` aus `playlistFormat(profile)`, `FormatLabel` =
Profil-Label, `Profile=<Key>`, `NeedsProbe=true`, Zustand `queued`.
Duplikat-Check wie bisher (gleiche URL + gleiches Format in queued/running →
409). Antwort 201 `{ids:[id]}`.

**Job-Felder.** `Profile string` (`json:"profile,omitempty"`) und
`NeedsProbe bool` (`json:"needs_probe,omitempty"`). `Profile` wird zusätzlich
bei allen bestehenden Wegen gesetzt, die ein Profil verwenden (Video-Job mit
`profile`, Playlist-Jobs). Alte `jobs.json` ohne die Felder bleiben gültig.

**Analyse-Vorschritt in der Queue.** Claimt der Worker einen Job mit
`NeedsProbe`, ruft er vor dem Runner den Prober auf (Timeout 60 s, am
Abbruch-Context des Jobs).

- Ergebnis Video: `Title` setzen, `NeedsProbe=false`, danach im selben
  Durchlauf der normale Download.
- Ergebnis Playlist: je Eintrag ein Job wie in `createPlaylistJobs`
  (Eintrags-URL, Eintrags-Titel, `PlaylistTitle`, Format aus
  `playlistFormat(profile)`, `Profile`); Duplikate (queued/running, gleiche
  URL + Format) werden übersprungen; danach wird der Platzhalter-Job
  entfernt. Reihenfolge: erst Einträge anlegen, dann Platzhalter entfernen,
  damit ein Absturz dazwischen beim Neustart nur zu einer erneuten Analyse
  mit übersprungenen Duplikaten führt.
- Playlist ohne Einträge: Job → `error`, Meldung „Playlist enthält keine
  Einträge“.
- Analyse-Fehler: Job → `error` mit der Fehlermeldung, `NeedsProbe` bleibt
  true; „Erneut“ analysiert dann erneut.
- Abbruch während der Analyse: Job → `canceled`, wie beim Download.
- Neustart des Servers: `running` → `queued` (bestehendes Verhalten),
  `NeedsProbe` bleibt, die Analyse läuft erneut.
- Jobs ohne `NeedsProbe`: Prober wird nicht aufgerufen, Verhalten unverändert.

**Gemeinsame Logik.** Die Playlist-Zerlegung (Format aus Profil, Job je
Eintrag, Duplikat-Prüfung) wird aus dem Handler in eine gemeinsame Stelle
gezogen, die Handler und Queue nutzen. Die Queue importiert das Paket
`server` nicht. Das genaue Paket legt der Implementierungsplan fest.

**Anzeige.** Ein Job mit `state=running` und `needs_probe=true` zeigt die
Pill „Wird analysiert“ und keine Fortschrittswerte. Ist `title` leer, zeigt
die Karte den gedämpften Platzhalter „Ohne Titel“ (nicht mehr die URL, weil
die URL jetzt in der eigenen URL-Zeile steht).

### B. Neu wählen

Bei `error` und `canceled` zusätzlicher Button „Neu wählen“
(`data-action="reselect"`) neben „Erneut“ und „Entfernen“. „Erneut“ bleibt
unverändert.

**Klick:**

- URL des Jobs ins Eingabefeld (über den zentralen Setz-Helfer, siehe D).
- `#direct-profile` auf `job.profile`, falls gesetzt und gültig.
- Offene Auswahlkarte schließen, Analyse-Ergebnis verwerfen.
- Hinweis `#replace-hint` in der URL-Karte: „Ersetzt gescheiterten Eintrag:
  <Titel oder URL>“ mit eigenem ✕ zum Lösen der Verknüpfung.
- Zur URL-Karte scrollen; kein Fokus ins Feld, damit auf iOS nicht die
  Tastatur aufgeht.

**Verknüpfung.** Sie besteht aus Job-ID und URL und ist an die URL gebunden:
Weicht der getrimmte Feldinhalt von der gemerkten URL ab oder wird das Feld
geleert, entfällt sie und der Hinweis verschwindet. Beide Startwege senden bei
bestehender Verknüpfung `replace: <Job-ID>` mit: der Direkt-Download und
`start()` nach der Analyse. Nach erfolgreichem Start wird die Verknüpfung
gelöscht.

**Server.** `replace` (optional, String) gilt für alle Typen (`direct`,
`video`, `playlist`). Erst nach erfolgreichem Anlegen mindestens eines Jobs
wird der referenzierte Job entfernt, und nur, wenn er existiert und in
`error` oder `canceled` ist; andernfalls wird `replace` stillschweigend
ignoriert. Scheitert das Anlegen (400/409), bleibt der alte Job unverändert.
Bei `direct` und `video` übernimmt der neue Job den `PlaylistTitle` des
ersetzten Jobs, wenn der Request selbst keinen setzt.

### C. URL-Zeile mit Kopier-Button

Jede Job-Karte (alle Zustände) bekommt eine einzeilige, per CSS gekürzte
URL-Zeile und daneben einen Icon-Button `data-action="copy-url"` mit
`aria-label="URL kopieren"`. Die URL ist nur dann ein Link
(`target="_blank"`, `rel="noopener noreferrer"`), wenn sie mit `http://` oder
`https://` beginnt; sonst reiner Text. URL und Attribute werden
HTML-escaped (vorhandener Helfer `esc`).

**`copyText(text)`** (gibt Erfolg zurück): Ist `window.isSecureContext` und
`navigator.clipboard` vorhanden, wird `navigator.clipboard.writeText`
genutzt. Sonst läuft ein synchroner Fallback im Klick-Handler: temporäres
`<textarea>` (readonly, `position:fixed`, unsichtbar, `font-size:16px` gegen
iOS-Zoom), `focus()`, `select()`, `setSelectionRange(0, text.length)`,
`document.execCommand('copy')`, Element entfernen. Das vorhandene Kopieren
des Dateinamens nutzt dieselbe Funktion.

**Toasts:** „URL kopiert“, „Dateiname kopiert“, bei Misserfolg „Kopieren nicht
möglich“. Das Touch-Ziel des Icon-Buttons ist auf Mobilgeräten mindestens
44×44 px.

### D. URL-Zeile leeren

Button `#url-clear` (`type="button"`, `aria-label="URL leeren"`, ✕-Icon)
rechts im Eingabefeld, nur sichtbar, wenn das Feld nicht leer ist, auch nach
programmatischem Setzen (z. B. durch „Neu wählen“ oder eine geteilte URL;
dafür ein zentraler Helfer zum Setzen des Feldwerts, der Sichtbarkeit und
Verknüpfungsprüfung auffrischt).

Klick: Feld leeren, Fokus ins Feld, offene Auswahlkarte schließen und
Analyse-Ergebnis verwerfen, „Ersetzt …“-Verknüpfung lösen.

## Betroffene Stellen (gegen den Code geprüft)

- `internal/server/server.go`: `createJobsRequest` (+ `replace`),
  `createVideoJob`, `createPlaylistJobs`, `playlistFormat`, Duplikat-Check;
  neuer Zweig `direct`; Entfernen des ersetzten Jobs nach Erfolg.
- `internal/job/job.go`: Felder `Profile`, `NeedsProbe`.
- `internal/queue/queue.go`: `runJob` (Z. 87) ist der Einhängepunkt für den
  Analyse-Vorschritt, vor `runnerFor(j).Run` (Z. 96). Die Queue bekommt heute
  nur Store und Runner (`New(st, r, maxConcurrent)`, Z. 29) und kennt keinen
  Prober; er wird als neue Abhängigkeit mit eigenem kleinen Interface
  (`Probe(ctx, url) (*ytdlp.ProbeResult, error)`, passend zu
  `ytdlp.Prober.Probe`, `internal/ytdlp/probe.go:116`) ergänzt. Das
  `Prober`-Interface des Servers (`server.go:22`) bleibt dort. Der
  Abbruch-Context je Job entsteht in `dispatch` (Z. 79) und ist in
  `q.cancels` registriert; `Cancel` (Z. 153) ruft ihn auf. Der Vorschritt
  nutzt denselben Context, die Zuordnung ctx-Fehler → `canceled` und
  Shutdown-Sonderfall (Z. 105–110) gilt damit auch für die Analyse.
- `cmd/server/main.go`: Verdrahtung (`queue.New` Z. 77, Prober wird bereits
  für den Server erzeugt).
- `internal/ytdlp/probe.go`: unverändert; `Probe` liefert Video mit `Title`
  oder Playlist mit `Title` + `Entries[{URL,Title}]`.
- `web/templates/index.html`: URL-Karte (`#url-input`, `#probe-btn`,
  `#probe-error` Z. 82); Profil-`<option>`s entstehen per
  `{{range .Profiles}}<option value="{{.Key}}">{{.Label}}</option>{{end}}`
  (bisher `#default-profile`, `#video-profile`, `#profile-select`); dasselbe
  Muster für `#direct-profile`. Neu: `#direct-btn`, `#url-clear`,
  `#replace-hint`.
- `web/static/app.js`: Probe-Fehler erscheinen in `#probe-error`
  (`probe()`, `textContent` + `show`); `toast()` für Erfolgsmeldungen;
  HTML-Escape-Helfer `esc()` existiert (Z. 42, escapt auch Quotes);
  `renderJobs`, `actionButtons`, delegierter Click-Handler, `start()`,
  `handleSharedUrl`, Kopieren des Dateinamens.
- `web/src/input.css`: Styles für Direkt-Zeile, `#url-clear`, URL-Zeile,
  Icon-Button (44×44 px).
- `README.md` (Englisch).

## Fehlerfälle

| Fall | Verhalten |
|---|---|
| `direct` ohne `url` | 400 |
| `direct` ohne oder mit unbekanntem `profile` | 400 |
| Gleiche URL + gleiches Format in queued/running | 409, alter Job (bei `replace`) bleibt |
| `replace` verweist auf unbekannte ID oder Job in queued/running/done | stillschweigend ignoriert, neuer Job wird normal angelegt |
| Anlegen scheitert (400/409) | `replace` wirkungslos, alter Job unverändert |
| Analyse-Fehler in der Queue | Job `error` mit Meldung, `NeedsProbe` bleibt, „Erneut“ analysiert erneut |
| Playlist ohne Einträge | Job `error`: „Playlist enthält keine Einträge“ |
| Abbruch während Analyse | Job `canceled` |
| Server-Neustart während Analyse | Job wieder `queued`, Analyse läuft erneut |
| Netzfehler beim Direkt-Download | Meldung in `#probe-error`, Buttons wieder aktiv |
| Kopieren scheitert (kein Clipboard, `execCommand` false oder wirft) | Toast „Kopieren nicht möglich“ |

## Tests

**Go, per TDD** (`make check`):

- Server (`internal/server/server_test.go`): Direkt-Job (201, Felder
  `profile`, `needs_probe`, Format aus `playlistFormat`); 400 ohne URL; 400
  bei unbekanntem oder fehlendem Profil; 409 bei Duplikat; `replace` entfernt
  Job in `error` bzw. `canceled`; `replace` ignoriert bei
  queued/running/done/unbekannter ID; alter Job bleibt bei 400/409;
  Übernahme von `PlaylistTitle`; `Profile` wird bei profilbasierten Video-
  und Playlist-Jobs gespeichert.
- Queue (`internal/queue/queue_test.go`): Video-Analyse setzt Titel und
  löscht `NeedsProbe`, Runner läuft; Playlist erzeugt Eintrags-Jobs,
  überspringt Duplikate, entfernt Platzhalter; leere Playlist → `error`;
  Analyse-Fehler → `error`, `NeedsProbe` bleibt, Retry analysiert erneut;
  Abbruch während Analyse → `canceled`; Job ohne `NeedsProbe` ruft den
  Prober nicht auf.
- Job/Store: JSON-Roundtrip der neuen Felder, `omitempty`, alte Datei ohne
  die Felder lädt.
- Template: Index enthält `#direct-profile`, `#direct-btn`, `#url-clear`,
  `#replace-hint`.

**Frontend:** Es gibt keinen JS-Testrunner, und er wird nicht eingeführt.
Prüfung im Browser gegen den lokalen Server mit Fake-yt-dlp anhand dieser
Checkliste:

- [ ] Direkt-Download Video: Eintrag erscheint, „Wird analysiert“, danach
      Titel und Fortschritt.
- [ ] Direkt-Download Playlist: Platzhalter verschwindet, Einträge erscheinen.
- [ ] Direkt-Download Fehler (ungültige URL, Fake-yt-dlp scheitert): Meldung
      in `#probe-error` bzw. Job `error`, „Erneut“ analysiert neu.
- [ ] Neu wählen über Direkt-Download: alter Eintrag verschwindet erst nach
      erfolgreichem Anlegen.
- [ ] Neu wählen über „Analysieren“ + `start()`: ebenso.
- [ ] Verknüpfung löst sich bei URL-Änderung, beim Leeren und über das ✕ im
      Hinweis; danach wird kein `replace` gesendet.
- [ ] URL-Zeile: gekürzt, Link nur bei `http(s)://`, sonst Text.
- [ ] Kopieren im sicheren Kontext (`localhost`) und im Fallback über
      `http://<LAN-IP>` (URL und Dateiname).
- [ ] Leeren-Button: nur sichtbar bei gefülltem Feld, auch nach
      „Neu wählen“ und geteilter URL; Fokus im Feld nach Klick.
- [ ] Schmale Breite ca. 390 px: Layout der URL-Karte, Touch-Ziele 44×44 px.

**iOS Safari:** manueller Test durch den Nutzer nach dem Deploy (Kopieren,
Leeren, Layout).

## Doku

`README.md` (Englisch) wird um die neuen Bedienmöglichkeiten ergänzt:
Direkt-Download, „Neu wählen“, URL kopieren.

## Nicht enthalten

- Autostart über das Teilen-Menü (`?url=…&start=1`) läuft weiter über die
  Analyse.
- „Neu wählen“ gilt für den einzelnen Eintrag, nicht für eine ganze Playlist.
- „Erneut“ und `handleDelete` bleiben unverändert.
- Kein JS-Testframework.
