// Package ytdlp kapselt alle Aufrufe des externen yt-dlp-Binaries
// sowie das Parsen seiner Ausgaben.
package ytdlp

// BuildFormat baut den yt-dlp -f-Ausdruck aus der UI-Auswahl.
// Leere IDs fallen auf "beste Qualität" zurück.
// Delegiert an BuildFormatMulti (eine Audiospur bzw. keine, falls audioID leer ist).
func BuildFormat(videoID, audioID string, audioOnly bool) string {
	var audioIDs []string
	if audioID != "" {
		audioIDs = []string{audioID}
	}
	return BuildFormatMulti(videoID, audioIDs, audioOnly)
}

// BuildFormatMulti baut den yt-dlp -f-Ausdruck für eine Video-ID und mehrere
// Audiospuren (z. B. mehrsprachige Original-/Synchronfassungen). Die Audio-IDs
// werden mit "+" an den Video-Teil angehängt (bv*+140-0+140-7).
func BuildFormatMulti(videoID string, audioIDs []string, audioOnly bool) string {
	switch {
	case audioOnly && len(audioIDs) > 0:
		return audioIDs[0]
	case audioOnly:
		return "ba"
	case len(audioIDs) == 0 && videoID != "":
		return videoID
	case len(audioIDs) == 0:
		return "bv*+ba/b"
	}

	video := videoID
	if video == "" {
		video = "bv*"
	}
	expr := video
	for _, id := range audioIDs {
		expr += "+" + id
	}
	return expr
}

// Profile sind die pauschalen Qualitätsstufen für Playlist-Downloads (Spec §5).
type Profile struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Expr  string `json:"-"`
	// VideoExpr ist nur der Videoteil des Profil-Ausdrucks (ohne Audio-Fallback),
	// z. B. "bv*[height<=1080]". Leer beim Profil "audio". Wird von der Chip-Auswahl
	// mehrerer Audiospuren mit dem Profil kombiniert (siehe BuildFormatMulti).
	VideoExpr string `json:"-"`
}

var Profiles = []Profile{
	{Key: "best", Label: "Beste Qualität", Expr: "bv*+ba/b", VideoExpr: "bv*"},
	{Key: "1080p-mp4", Label: "Beste ≤1080p (MP4/M4A)", Expr: "bv*[height<=1080][ext=mp4]+ba[ext=m4a]/b[ext=mp4][height<=1080]/b[height<=1080]", VideoExpr: "bv*[height<=1080][ext=mp4]"},
	{Key: "1080p", Label: "Beste ≤1080p", Expr: "bv*[height<=1080]+ba/b[height<=1080]", VideoExpr: "bv*[height<=1080]"},
	{Key: "720p", Label: "Beste ≤720p", Expr: "bv*[height<=720]+ba/b[height<=720]", VideoExpr: "bv*[height<=720]"},
	{Key: "audio", Label: "Nur Audio", Expr: "ba", VideoExpr: ""},
}

func ProfileByKey(key string) (Profile, bool) {
	for _, p := range Profiles {
		if p.Key == key {
			return p, true
		}
	}
	return Profile{}, false
}
