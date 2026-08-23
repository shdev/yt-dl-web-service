package ytdlp

import (
	"sort"
	"strings"
)

// AudioTrack beschreibt eine wählbare Audiospur eines Einzelvideos.
type AudioTrack struct {
	FormatID string `json:"format_id"`
	Language string `json:"language"` // Kurzform, z. B. "de", "en"
	Label    string `json:"label"`    // z. B. "de", "en (Original)", "de (Audiodeskription)"
	Original bool   `json:"original"`
	Selected bool   `json:"selected"` // Default-Auswahl nach Spec-Regel
}

// audioDescMarkers erkennen Audiodeskriptions-Spuren; ARTE benennt sie nur im
// Text der format_id, andere Extraktoren in der format_note.
var audioDescMarkers = []string{"audiodeskription", "audio description", "audio_desc"}

// audioCandidate ist eine Tonspur samt der aus der Heuristik abgeleiteten
// Einordnung.
type audioCandidate struct {
	format      Format
	lang        string // Sprach-Kurzform, z. B. "de"
	description bool
	original    bool
}

// RankAudio liefert je Sprache die beste Audiospur samt Default-Auswahl.
// Rückgabe sortiert: de, en, Rest (stabil); leere Liste, wenn keine
// Audio-Formate Sprachinfos tragen.
func RankAudio(formats []Format) []AudioTrack {
	cands := audioCandidates(formats)
	if len(cands) == 0 {
		return nil
	}
	markOriginals(cands)

	// Je Sprache bleibt nur die beste Spur; langOrder hält fest, in welcher
	// Reihenfolge die Sprachen in der Eingabe zuerst auftauchen.
	best := make(map[string]*audioCandidate, len(cands))
	var langOrder []string
	for _, c := range cands {
		cur, seen := best[c.lang]
		if !seen {
			best[c.lang] = c
			langOrder = append(langOrder, c.lang)
			continue
		}
		if betterAudio(c, cur) {
			best[c.lang] = c
		}
	}
	// de zuerst, dann en, dann der Rest in Eingabereihenfolge.
	sort.SliceStable(langOrder, func(i, j int) bool {
		return langRank(langOrder[i]) < langRank(langOrder[j])
	})

	selected := selectDefaults(best, langOrder)
	tracks := make([]AudioTrack, 0, len(langOrder))
	for _, lang := range langOrder {
		c := best[lang]
		tracks = append(tracks, AudioTrack{
			FormatID: c.format.ID,
			Language: c.lang,
			Label:    audioLabel(c),
			Original: c.original,
			Selected: selected[c],
		})
	}
	return tracks
}

// audioCandidates filtert die reinen Tonspuren mit bekannter Sprache heraus
// und markiert Audiodeskriptionen (Regeln 1, 2 und 4).
func audioCandidates(formats []Format) []*audioCandidate {
	var cands []*audioCandidate
	for _, f := range formats {
		if !isAudioOnly(f) {
			continue
		}
		lang := langShort(f.Language)
		if lang == "" {
			continue
		}
		cands = append(cands, &audioCandidate{
			format:      f,
			lang:        lang,
			description: hasMarker(f, audioDescMarkers...),
		})
	}
	return cands
}

// isAudioOnly erkennt reine Tonspuren. Primäre Kennung ist ein ausdrückliches
// vcodec "none" — dann zählt die Spur auch dann als Audio, wenn der Audio-Codec
// unbekannt ist: ARTE liefert für seine HLS-Tonspuren `"acodec": null`. Fehlt
// die vcodec-Angabe ganz, braucht es umgekehrt einen positiven Audio-Codec.
// Storyboards (beides "none") bleiben in beiden Fällen draußen.
func isAudioOnly(f Format) bool {
	if f.VCodec == "none" {
		return f.ACodec != "none"
	}
	return f.VCodec == "" && f.ACodec != "" && f.ACodec != "none"
}

// hasMarker prüft format_id und format_note case-insensitiv auf einen der
// Textmarker.
func hasMarker(f Format, markers ...string) bool {
	for _, field := range [...]string{strings.ToLower(f.ID), strings.ToLower(f.Note)} {
		for _, m := range markers {
			if strings.Contains(field, m) {
				return true
			}
		}
	}
	return false
}

// langShort kürzt die Sprachangabe auf den Teil vor dem Bindestrich: aus
// "de-DE" wird "de".
func langShort(lang string) string {
	if i := strings.Index(lang, "-"); i >= 0 {
		lang = lang[:i]
	}
	return strings.ToLower(lang)
}

// markOriginals bestimmt die Originalspuren (Regel 3). Unterscheiden sich die
// language_preference-Werte der Nicht-Deskriptions-Spuren, gilt das Maximum als
// Original — so markiert YouTube die Originalspur (10) gegenüber
// Synchronfassungen (-1). Sind alle Werte gleich oder fehlen sie, entscheidet
// der Text in format_id/format_note, wie bei ARTE. Audiodeskriptionen gelten
// nie als Original.
func markOriginals(cands []*audioCandidate) {
	prefs := make(map[int]struct{})
	maxPref := 0
	for _, c := range cands {
		if c.description || c.format.LanguagePreference == nil {
			continue
		}
		p := *c.format.LanguagePreference
		if len(prefs) == 0 || p > maxPref {
			maxPref = p
		}
		prefs[p] = struct{}{}
	}
	for _, c := range cands {
		if c.description {
			continue
		}
		if len(prefs) >= 2 {
			c.original = c.format.LanguagePreference != nil && *c.format.LanguagePreference == maxPref
			continue
		}
		c.original = hasMarker(c.format, "original")
	}
}

// betterAudio entscheidet, ob a die bislang beste Spur b einer Sprache ablöst
// (Regel 5): Original schlägt normale Spur schlägt Audiodeskription, danach
// zählt die höhere Bitrate.
func betterAudio(a, b *audioCandidate) bool {
	if ra, rb := audioTier(a), audioTier(b); ra != rb {
		return ra > rb
	}
	return audioRate(a.format) > audioRate(b.format)
}

// audioTier ist die Vorzugsstufe einer Spur: Original vor normaler Spur vor
// Audiodeskription.
func audioTier(c *audioCandidate) int {
	switch {
	case c.original:
		return 2
	case c.description:
		return 0
	default:
		return 1
	}
}

// audioRate ist das Qualitätsmaß einer Tonspur: die Audio-Bitrate, ersatzweise
// die Gesamtbitrate.
func audioRate(f Format) float64 {
	if f.ABR > 0 {
		return f.ABR
	}
	return f.TBR
}

// audioLabel beschriftet eine Spur mit ihrer Sprach-Kurzform und weist auf
// Original bzw. Audiodeskription hin (Regel 7).
func audioLabel(c *audioCandidate) string {
	switch {
	case c.original:
		return c.lang + " (Original)"
	case c.description:
		return c.lang + " (Audiodeskription)"
	default:
		return c.lang
	}
}

// langRank sortiert Deutsch nach vorne, dann Englisch, dann den Rest.
func langRank(lang string) int {
	switch lang {
	case "de":
		return 0
	case "en":
		return 1
	default:
		return 2
	}
}

// selectDefaults bestimmt die vorausgewählten Spuren (Regel 6): die beste
// deutsche Spur, sofern sie keine Audiodeskription ist, plus die — notfalls
// fremdsprachige — Originalspur. Ist danach nichts gewählt, greift Englisch,
// zuletzt die erste Spur der sortierten Liste.
func selectDefaults(best map[string]*audioCandidate, langOrder []string) map[*audioCandidate]bool {
	selected := make(map[*audioCandidate]bool, 2)
	if de, ok := best["de"]; ok && !de.description {
		selected[de] = true
	}
	for _, lang := range langOrder {
		if best[lang].original {
			selected[best[lang]] = true
			break
		}
	}
	if len(selected) > 0 {
		return selected
	}
	if en, ok := best["en"]; ok {
		selected[en] = true
	} else {
		selected[best[langOrder[0]]] = true
	}
	return selected
}
