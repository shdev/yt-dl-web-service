# Backlog — gesammelte Erweiterungsideen

Sammelphase; Planung folgt gesondert (erst auf Zuruf).

## 1. Zeitstempel und Dateiname in der Job-Karte

**Idee (2026-08-23):** In der UI sieht man pro Eintrag, wann er aufgenommen
wurde bzw. wann er fertig heruntergeladen war. Außerdem sieht man den
Dateinamen der heruntergeladenen Datei.

**Ausgangslage (Kurz-Recherche):**

- `Job.CreatedAt` existiert bereits und kommt schon heute über `/api/jobs`
  mit — die UI zeigt es nur nicht an. Anzeige ist reines Frontend.
- Ein „fertig seit“-Zeitstempel (`CompletedAt` o. ä.) fehlt im Modell —
  kleines Backend-Feld plus Setzen beim Zustandswechsel nach `done`.
- Der tatsächliche Dateiname fehlt komplett. yt-dlp kennt ihn; der Runner
  müsste ihn erfassen (z. B. `--print after_move:filepath` oder aus dem
  Fortschritts-/Output-Parsing) und im Job persistieren.

**Offene Fragen für die Planung:**

- Zeitformat: absolut („23.08. 15:12“) oder relativ („vor 2 h“)?
- Dateiname: nur anzeigen oder auch kopierbar/verlinkt (Downloads-Ordner
  ist serverseitig — Link ginge nur, wenn wir Dateien ausliefern wollen)?
- Gilt „fertig seit“ auch für Fehler/Abbruch (generisches `FinishedAt`)?
- Bestandsjobs in `jobs.json` haben die neuen Felder nicht — Anzeige muss
  leere Werte verkraften.

## 2. Verzeichnisstruktur aus Metadaten: Quelle/Kanal

**Idee (2026-08-23):** Downloads anhand von Metadaten in Unterverzeichnisse
sortieren: pro Quelle ein Ordner (alle YouTube-Videos unter `youtube/`,
Short-Domains wie `youtu.be` normalisiert auf dieselbe Quelle), darunter ein
Unterverzeichnis je Kanal.

**Ausgangslage (Kurz-Recherche):**

- Der Runner übergibt heute `-o DownloadDir/OUTPUT_TEMPLATE` mit Default
  `%(title)s [%(id)s].%(ext)s` (Env `OUTPUT_TEMPLATE` überschreibt).
- yt-dlp kann das fast allein: `%(extractor)s` liefert die normalisierte
  Quelle („youtube“ — egal ob youtube.com oder youtu.be geteilt wurde,
  gleicher Extractor), `%(channel)s`/`%(uploader)s` den Kanal. Ein Template
  wie `%(extractor)s/%(channel,uploader|Unbekannt)s/%(title)s [%(id)s].%(ext)s`
  erledigt Quelle + Kanal + Fallback ohne eigenes TLD-Parsing.

**Offene Fragen für die Planung:**

- Neuer Default fürs Template oder zusätzlich in den UI-Einstellungen
  konfigurierbar (z. B. Schalter „nach Quelle/Kanal sortieren“)?
- Kanal-Feld: `channel` vs. `uploader` (unterscheiden sich z. T.); Fallback
  für Quellen ohne Kanalbegriff.
- Groß-/Kleinschreibung und Sonderzeichen der Ordnernamen: yt-dlp
  sanitisiert selbst; reicht das fürs NAS (SMB), oder `--restrict-filenames`?
- Wechselwirkung mit `--continue`: Template-Wechsel während laufender/
  unterbrochener Jobs — Teildateien liegen dann unter dem alten Pfad.
- Bestandsdateien werden nicht umsortiert (nur neue Downloads).
- Zusammenspiel mit Idee 1: der erfasste Dateiname enthielte dann den
  relativen Pfad inkl. Quelle/Kanal.

**Ergänzung (2026-08-23):** Soll analog für Instagram und Facebook gelten.
Der Extractor-Ansatz deckt das automatisch ab — yt-dlp hat eigene
Extractors für beide (`instagram/`, `facebook/`), Short-/Teil-Domains wie
`fb.watch` oder `instagr.am` normalisieren auf denselben Extractor. Als
„Kanal“ greift dort der `uploader`-Fallback (Instagram-Account bzw.
Facebook-Seite). Zu verifizieren bei der Umsetzung: wie die Felder bei
beiden konkret gefüllt sind (Login-/Cookie-Pflicht bei Instagram kann
Metadaten beeinflussen).

## 3. Mehrsprachige Audios: Sprachwahl + kluge Defaults

**Idee (2026-08-23):** Manche Videos haben mehrere Audiospuren — echte
Mehrsprachigkeit (z. B. ARTE: Deutsch, Französisch, Englisch-Original,
Audiodeskriptions-Varianten) oder Auto-Dubs (YouTube: „dubbed-auto“ in
vielen Sprachen plus „original (default)“). Gewünscht:

1. **UI-Auswahl:** Bei Multi-Language-Videos eine Auswahl anbieten, welche
   Sprache(n) heruntergeladen werden.
2. **Default-Regel (gilt für alle Videos):** Immer die Original-Version
   bevorzugen. Sprachordnung de vor en — ein deutsches Original schlägt ein
   englisches Original. Dubbed-Versionen sind zweitrangig (dort dieselbe
   Ordnung de vor en). Alle anderen Sprachen können ignoriert werden.
   Ergibt als Rangfolge: original-de > original-en > dubbed-de > dubbed-en.

**Ausgangslage (Kurz-Recherche):**

- Der Probe-Parser (`internal/ytdlp/probe.go`) reicht `language`/
  `language_preference` heute **nicht** durch — die UI kann Sprachen weder
  anzeigen noch auswählen. Felder ergänzen ist klein.
- Die Format-Labels im „Formate wählen“-Audio-Select müssten die Sprache
  zeigen (z. B. „[de] Original · opus · 129 kbit/s“).
- Profile nutzen `ba` ohne Sprachlogik; yt-dlp wählt dann nach eigener
  Präferenz. Werkzeuge für die Default-Regel: Selektor-Filter wie
  `ba[language^=de]` (Prefix-Match wegen de-DE), Fallback-Ketten mit `/`,
  Format-Sortierung `-S lang`, Original-Erkennung über `format_note`
  („original“) bzw. `language_preference`. Genaue Mechanik pro Extractor
  (ARTE vs. YouTube kodieren Original/Dub unterschiedlich) beim Planen
  verifizieren.

**Offene Fragen für die Planung:**

- Klarstellung der Regel am ARTE-Beispiel: Dort ist das Original Englisch,
  Deutsch ist Synchro. Strikt „Original zuerst“ ⇒ englisches Original vor
  deutscher Synchro. Gewollt? Oder de-Synchro vor fremdsprachigem Original?
- Audiodeskription (ARTE „Deutsch (Audiodeskription)“) darf trotz „de“
  nicht vor normalem Deutsch/Original landen — explizit abwerten.
- Mehrere Sprachen gleichzeitig: getrennte Dateien oder eine Datei mit
  mehreren Tonspuren (`--audio-multistreams`, erzwingt i. d. R. mkv)?
- Gilt die Sprachauswahl auch für Playlist-Profile oder nur für
  Einzelvideos mit Formatwahl?
- Untertitel gleich mitdenken (gleiche Sprachlogik) oder bewusst außen vor?
