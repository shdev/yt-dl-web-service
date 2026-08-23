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
