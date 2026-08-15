// Package ytdlp kapselt alle Aufrufe des externen yt-dlp-Binaries
// sowie das Parsen seiner Ausgaben.
package ytdlp

// BuildFormat baut den yt-dlp -f-Ausdruck aus der UI-Auswahl.
// Leere IDs fallen auf "beste Qualität" zurück.
func BuildFormat(videoID, audioID string, audioOnly bool) string {
	switch {
	case audioOnly && audioID != "":
		return audioID
	case audioOnly:
		return "ba"
	case videoID != "" && audioID != "":
		return videoID + "+" + audioID
	case videoID != "":
		return videoID
	case audioID != "":
		return "bv*+" + audioID
	default:
		return "bv*+ba/b"
	}
}

// Profile sind die pauschalen Qualitätsstufen für Playlist-Downloads (Spec §5).
type Profile struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Expr  string `json:"-"`
}

var Profiles = []Profile{
	{Key: "best", Label: "Beste Qualität", Expr: "bv*+ba/b"},
	{Key: "1080p", Label: "Beste ≤1080p", Expr: "bv*[height<=1080]+ba/b[height<=1080]"},
	{Key: "720p", Label: "Beste ≤720p", Expr: "bv*[height<=720]+ba/b[height<=720]"},
	{Key: "audio", Label: "Nur Audio", Expr: "ba"},
}

func ProfileByKey(key string) (Profile, bool) {
	for _, p := range Profiles {
		if p.Key == key {
			return p, true
		}
	}
	return Profile{}, false
}
