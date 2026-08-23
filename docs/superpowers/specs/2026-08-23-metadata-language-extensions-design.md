# Spec: Job-Metadaten, Verzeichnisstruktur & mehrsprachige Audios

Datum: 2026-08-23 · Status: Design vom Nutzer freigegeben
Quelle der Anforderungen: `docs/backlog.md` (Ideen 1–3)

## Kontext & Ziele

Drei Erweiterungen, gemeinsam entworfen, weil sie sich berühren (Pfade,
Dateinamen, Container):

1. **Job-Karten zeigen Zeitstempel und Dateinamen** — wann ein Job
   aufgenommen und wann er beendet wurde, und wie die Datei heißt.
2. **Downloads sortieren sich nach Quelle und Kanal in Unterverzeichnisse**
   (`youtube/<Kanal>/…`), Short-Domains normalisiert.
3. **Mehrsprachige Audios:** Sprachwahl in der UI plus Default-Regel
   „deutsche Spur bevorzugt, Original immer als zweite Spur dabei“.

## Entscheidungen

| Frage | Entscheidung |
|---|---|
| „Beendet“-Zeitstempel | Generisches `FinishedAt` — gesetzt bei jedem Endzustand (done, error, canceled) |
| Dateinamen-Erfassung | yt-dlp `--print after_move:filepath`; Runner erkennt die Pfadzeile in stdout; gespeichert wird der Pfad relativ zu `/downloads` |
| Zeitanzeige | Relativ in der Karte („vor 2 h“), absolut im Tooltip (`title`-Attribut) |
| Dateiname in der UI | Nur Anzeige + Klick-zum-Kopieren; kein Datei-Serving |
| Verzeichnisstruktur | **Neuer Default** `%(extractor)s/%(channel,uploader|Unbekannt)s/%(title)s [%(id)s].%(ext)s`; Env `OUTPUT_TEMPLATE` bleibt Override |
| Ordnernamen | Schreibweise des yt-dlp-Extractors (`youtube`, `ArteTV`, `Instagram`); ohne Kanal-Metadaten → `Unbekannt/` |
| Audio-Auswahl | **Go-seitig** aus dem Probe-JSON (Ranking-Engine), yt-dlp bekommt konkrete Format-IDs |
| Default-Spuren | Beste de-Spur + Original (dedupliziert, wenn identisch); Audiodeskription strikt abgewertet; Auto-Dubs zweitrangig; Sprachrang de > en > Rest |
| Container | `--audio-multistreams` + `--merge-output-format mp4/mkv` (mp4 wenn Codecs kompatibel, sonst mkv) |
| „Nur Audio“-Modus | Nur die bevorzugte Sprache, kein Original-Zwang |
| Playlists | Kein Einzel-Probe → generischer Fallback-Selektor (siehe unten), Best-Effort |
| Untertitel | Außen vor (YAGNI) |

## Verifizierte Fakten (Proben vom 2026-08-23, via Container-yt-dlp)

- **YouTube** (`AFxNM3RiXzE`): Original-Audiospur trägt
  `language_preference: 10` und `format_note` mit „original (default)“;
  alle anderen Spuren (inkl. Auto-Dubs) `language_preference: -1`. Der
  „dubbed-auto“-Marker steht nur bei den m3u8-Varianten im Note, bei den
  DASH-Varianten nicht — die Preference ist der zuverlässige Marker.
  `channel`/`uploader` gefüllt. `extractor: youtube`.
- **ARTE** (`091157-000-A`): `language_preference` ist bei allen
  Audio-Formaten identisch (110101), `format_note` überall gleich —
  **beide nutzlos zur Original-Erkennung**. Die Kennzeichnung steckt
  ausschließlich in der `format_id`
  (`…-Englisch__Original_`, `…-Deutsch__Audiodeskription_`).
  `channel`/`uploader` sind leer. `extractor: ArteTV`.

Konsequenz: Ein statischer yt-dlp-Selektor kann die Regel nicht robust
abbilden; die Auswahl trifft Go anhand des Probe-JSONs.

## Architektur

### A. Job-Modell & Runner (Idee 1)

- `internal/job/job.go`: neue Felder
  `FinishedAt *time.Time \`json:"finished_at,omitempty"\`` und
  `Filename string \`json:"filename,omitempty"\``. `FinishedAt` wird beim
  Übergang in einen Endzustand gesetzt (dort, wo die Queue heute
  `State` setzt).
- `internal/ytdlp/runner.go`: Argument `--print after_move:filepath`
  ergänzen. Der Runner liest stdout zeilenweise (Progress-Template);
  eine Zeile, die mit dem absoluten `DownloadDir`-Pfad beginnt, ist der
  finale Dateipfad → relativ zu `DownloadDir` machen und am Job
  speichern. Verhalten mit `--progress-template` gemeinsam verifizieren
  (Integration-Test-Fixture). Bei Playlist-Jobs (ein Job pro Video) gilt
  dasselbe.
- Alt-Jobs ohne die Felder: JSON-omitempty, UI blendet Leeres aus.

### B. Job-Karten-UI (Idee 1)

- Meta-Zeile ergänzt: „hinzugefügt <relativ>“ immer; bei Endzuständen
  zusätzlich „beendet <relativ>“ (bei done: „fertig <relativ>“).
  Relativzeit-Helfer in `app.js` (< 1 min „gerade eben“, Minuten,
  Stunden, ab 24 h Datum); absoluter Zeitpunkt im `title`-Attribut.
- Bei `done` mit `filename`: eigene Zeile mit dem relativen Pfad
  (Ellipsis bei Überlänge), Klick kopiert per
  `navigator.clipboard.writeText` und quittiert mit Toast.

### C. Verzeichnis-Default (Idee 2)

- `internal/config/config.go`: Default-`OutputTemplate` wird
  `%(extractor)s/%(channel,uploader|Unbekannt)s/%(title)s [%(id)s].%(ext)s`.
- Kein `--restrict-filenames` (Umlaute bleiben); yt-dlp sanitisiert
  Pfadzeichen selbst.
- README: neuer Default dokumentiert, Hinweis auf Env-Override und
  darauf, dass unterbrochene Alt-Jobs nach dem Update unter dem neuen
  Pfad neu laden (verwaiste `.part`-Dateien im alten Pfad möglich).

### D. Audio-Ranking-Engine (Idee 3, Kern)

Neues Modul `internal/ytdlp/audiorank.go`:

- Eingabe: `[]Format` (Audio-Formate aus dem Probe-JSON, mit den neu
  durchgereichten Feldern `language`, `language_preference`).
- Klassifikation je Spur:
  - **Audiodeskription:** `format_id` oder `format_note` enthält
    (case-insensitiv) „audiodeskription“, „audio description“ oder
    „audio_desc“ → Rang ganz hinten, nie automatisch gewählt.
  - **Original:** Wenn `language_preference` innerhalb des Videos
    variiert → Spuren mit dem Maximum sind Original (YouTube-Fall).
    Sonst: `format_id`/`format_note` enthält „original“ (ARTE-Fall).
    Keine Kennung → kein Original bekannt.
  - **Sprache:** Prefix-Match auf `language` (`de*` > `en*` > Rest).
- Innerhalb gleicher Klasse entscheidet die Audio-Qualität (abr/tbr).
- Ausgabe: Liste der Sprachen mit je bester Spur (`format_id`, Sprache,
  Original-Flag, Label) plus die Default-Auswahl nach der Regel:
  beste de-Spur + Original; fehlt de → Original allein; kein Original
  erkennbar → beste de-, sonst beste en-, sonst beste Spur.
- `BuildFormat` wird erweitert: mehrere Audio-IDs →
  `bv*+<id1>+<id2>`-Ausdruck; ein Audio-only-Download nutzt genau eine ID.

### E. API & Sprachwahl-UI (Idee 3)

- Probe-Antwort (`/api/probe`): Formate reichen `language`,
  `language_preference` mit durch; zusätzlich liefert der Server
  `video.audio_languages`: Liste `{format_id, language, label, original,
  selected}` — die UI rendert ohne eigene Ranking-Logik.
- Auswahl-Karte: Bei ≥ 2 Sprachen erscheint „Audiosprachen“ als
  Checkbox-Chips (vorausgewählt = Default-Auswahl, Original markiert,
  z. B. „en (Original)“). Bei einer Sprache: keine neue UI.
- Job-Anlage (`POST /api/jobs`, type video): neues Feld
  `audio_format_ids []string` (ersetzt für den Mehrspur-Fall das
  bisherige einzelne `format_audio`; das bleibt für „Formate wählen“/
  „Nur Audio“ gültig). Der Server baut den Format-Ausdruck.
- `format_label` des Jobs nennt die Spuren, z. B.
  „1080p av01 + de + en (Original)“.
- Audio-Select in „Formate wählen“ zeigt die Sprache im Label
  („[de] opus · 129 kbit/s · …“).
- Runner-Argumente bei ≥ 2 Audio-IDs: `--audio-multistreams` und
  `--merge-output-format mp4/mkv`.

### F. Playlists & Fallback

Playlist-Jobs (Profil-basiert, ohne Einzel-Probe) nutzen als
Format-Ausdruck-Anhang die Best-Effort-Kette:
`bv*+ba[language^=de]+ba[language_preference>0]/bv*+ba[language^=de]/bv*+ba/b`
mit denselben Multistream-/Merge-Optionen. Profile mit Höhen-Filter
behalten ihre `[height<=…]`-Bedingungen auf dem Videoteil. Quellen ohne
verwertbare Kennzeichnung (ARTE-Playlists) fallen damit kontrolliert auf
yt-dlp-Default zurück.

## Fehlerfälle

- Kein `filename` erfassbar (yt-dlp-Abbruch, altes yt-dlp): Feld bleibt
  leer, UI zeigt nichts — kein Fehler.
- Clipboard-API nicht verfügbar (http ohne TLS auf Fremd-Host):
  Kopieren scheitert still → Toast „Kopieren nicht möglich“.
- Probe ohne `language`-Felder (Extractor liefert nichts): keine
  Sprach-Chips, Verhalten wie heute.
- `audio_format_ids` mit unbekannter ID: Server validiert nicht gegen die
  Probe (zustandslos wie bisher bei `format_video`); yt-dlp meldet den
  Fehler, der Job endet als `error` (Status quo).

## Tests

- `audiorank`: TDD mit Fixtures aus den beiden realen Proben (ARTE- und
  YouTube-JSON, eingedampft auf die Audio-Formate) — Default-Auswahl,
  Audiodeskriptions-Abwertung, Dedupe de==Original, fehlendes de,
  fehlende Original-Kennung.
- `BuildFormat`: Mehrfach-Audio-Ausdrücke.
- Job/Queue: `FinishedAt` bei allen Endzuständen.
- Runner: Argumentliste (Print/Multistreams/Merge) und
  Pfadzeilen-Erkennung mit Fixture-Output.
- Server: Probe-Antwort mit `audio_languages`; Job-Anlage mit
  `audio_format_ids`.
- UI-Abnahme per DevTools-Fixtures (Chips, Zeiten, Dateiname, Kopieren).

## Risiken & Hinweise

- yt-dlp-Feldsemantik kann sich je Extractor ändern — die Heuristik ist
  bewusst mehrstufig; Fixtures konservieren den heutigen Stand.
- `--merge-output-format mp4/mkv`: mp4-Merge scheitert bei inkompatiblen
  Codecs nicht, sondern fällt auf mkv — im Integrationstest verifizieren.
- Wechselwirkung `--print after_move:filepath` ↔ `--progress-template`
  wird im Plan mit echtem yt-dlp einmal verifiziert, bevor der Runner
  darauf baut.
