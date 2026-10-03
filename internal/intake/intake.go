// Paket intake enthält die gemeinsame Job-Anlage-Logik für HTTP-Handler und
// Queue (Playlist-Format, Duplikatprüfung, Anlage von Playlist-Jobs).
package intake

import (
	"strings"

	"ytdlweb/internal/job"
	"ytdlweb/internal/store"
	"ytdlweb/internal/ytdlp"
)

// Entry ist ein Playlist-Eintrag (URL und Titel).
type Entry struct {
	URL   string
	Title string
}

// PlaylistFormat baut den Format-Ausdruck für Playlist-Jobs: eine
// Sprach-Fallback-Kette, die die deutsche Synchro plus fremdsprachige
// Originalspur bevorzugt, ersatzweise irgendeine deutschsprachige Spur,
// sonst der bisherige Profil-Ausdruck ("Beste Qualität · de + en
// (Original)"-Regel aus Backlog-Idee 3, angewandt auf Playlists). Das
// zweite Kettenglied (Original-Zweitspur) filtert zusätzlich
// [language!^=de] — sonst würde bei Quellen ohne language_preference eine
// zweite deutsche Spur (z. B. eine zweite de-Synchro) fälschlich als
// "Original" mitgewählt und die de-Spur landet doppelt im Ausgabefile
// (gegen echtes YouTube/ARTE verifiziert, Final-Review-Fund 1). MultiAudio
// ist true, weil die Kette bis zu zwei Audiospuren kombinieren kann
// (Runner setzt dann --audio-multistreams). Beim Profil "audio" (kein
// VideoExpr) bleibt alles wie bisher — dort existiert kein Videoteil, mit
// dem sich mehrere Spuren kombinieren ließen (Ausnahme aus dem Brief).
func PlaylistFormat(profile ytdlp.Profile) (format string, multiAudio bool) {
	if profile.VideoExpr == "" {
		return profile.Expr, false
	}
	format = profile.VideoExpr + "+ba[language^=de]+ba[format_note*=original][language!^=de]/" +
		profile.VideoExpr + "+ba[language^=de]/" + profile.Expr
	return format, true
}

// IsDuplicate meldet, ob für URL und Format schon ein wartender oder
// laufender Job existiert.
func IsDuplicate(st *store.Store, url, format string) bool {
	for _, j := range st.List() {
		if j.URL == url && j.Format == format &&
			(j.State == job.StateQueued || j.State == job.StateRunning) {
			return true
		}
	}
	return false
}

// CreatePlaylistJobs legt für jeden Eintrag einen Job an. Leere URLs und
// Duplikate werden übersprungen (skipped). Bei einem Speicherfehler bleiben
// die bereits angelegten ids erhalten. Kicken ist Sache des Aufrufers.
func CreatePlaylistJobs(st *store.Store, profile ytdlp.Profile, playlistTitle string, entries []Entry) (ids []string, skipped int, err error) {
	format, multiAudio := PlaylistFormat(profile)
	ids = []string{}
	for _, e := range entries {
		url := strings.TrimSpace(e.URL)
		if url == "" || IsDuplicate(st, url, format) {
			skipped++
			continue
		}
		j := job.New(url, e.Title, format, profile.Label, playlistTitle)
		j.MultiAudio = multiAudio
		j.Profile = profile.Key
		if err := st.Add(j); err != nil {
			return ids, skipped, err
		}
		ids = append(ids, j.ID)
	}
	return ids, skipped, nil
}
