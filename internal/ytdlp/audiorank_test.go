package ytdlp_test

import (
	"testing"

	"ytdlweb/internal/ytdlp"
)

func intp(i int) *int { return &i }

// arteAudio bildet eine reale ARTE-Probe ab: überall identische
// language_preference; Original und Audiodeskription stehen nur im Text der
// format_id.
var arteAudio = []ytdlp.Format{
	{ID: "VA-STA-audio_0-Deutsch__Audiodeskription_", ACodec: "mp4a", Language: "de", LanguagePreference: intp(110101)},
	{ID: "VA-STA-audio_0-Englisch__Original_", ACodec: "mp4a", Language: "en", LanguagePreference: intp(110101)},
	{ID: "VA-STA-audio_0-Französisch", ACodec: "mp4a", Language: "fr", LanguagePreference: intp(110101)},
	{ID: "VA-STA-audio_0-Deutsch", ACodec: "mp4a", Language: "de", LanguagePreference: intp(110101)},
}

// ytAudio bildet eine reale YouTube-Probe ab: Originalspur über
// language_preference=10, Synchronfassung über -1; je Sprache zwei Bitraten.
var ytAudio = []ytdlp.Format{
	{ID: "139-0", ACodec: "mp4a.40.5", Language: "de-DE", LanguagePreference: intp(-1), ABR: 49},
	{ID: "139-7", ACodec: "mp4a.40.5", Language: "en-US", LanguagePreference: intp(10), ABR: 49, Note: "English (US) original (default), low"},
	{ID: "140-0", ACodec: "mp4a.40.2", Language: "de-DE", LanguagePreference: intp(-1), ABR: 129},
	{ID: "140-7", ACodec: "mp4a.40.2", Language: "en-US", LanguagePreference: intp(10), ABR: 129, Note: "English (US) original (default), medium"},
}

// wantTrack ist die Erwartung an eine Spur an einer festen Listenposition.
type wantTrack struct {
	formatID string
	language string
	label    string
	original bool
	selected bool
}

func checkTracks(t *testing.T, got []ytdlp.AudioTrack, want []wantTrack) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%d Spuren, erwartet %d: %+v", len(got), len(want), got)
	}
	for i, w := range want {
		g := got[i]
		if g.FormatID != w.formatID || g.Language != w.language || g.Label != w.label ||
			g.Original != w.original || g.Selected != w.selected {
			t.Errorf("Spur %d = %+v, erwartet %+v", i, g, w)
		}
	}
}

func TestRankAudioARTE(t *testing.T) {
	checkTracks(t, ytdlp.RankAudio(arteAudio), []wantTrack{
		{formatID: "VA-STA-audio_0-Deutsch", language: "de", label: "de", selected: true},
		{formatID: "VA-STA-audio_0-Englisch__Original_", language: "en", label: "en (Original)", original: true, selected: true},
		{formatID: "VA-STA-audio_0-Französisch", language: "fr", label: "fr"},
	})
}

func TestRankAudioYouTube(t *testing.T) {
	checkTracks(t, ytdlp.RankAudio(ytAudio), []wantTrack{
		{formatID: "140-0", language: "de", label: "de", selected: true},
		{formatID: "140-7", language: "en", label: "en (Original)", original: true, selected: true},
	})
}

// (a) Einsprachiges Video, dessen einzige Spur als Original ausgewiesen ist.
func TestRankAudioSingleOriginalTrack(t *testing.T) {
	formats := []ytdlp.Format{
		{ID: "140-7", ACodec: "mp4a.40.2", Language: "en-US", LanguagePreference: intp(10), ABR: 129, Note: "English (US) original (default), medium"},
	}
	checkTracks(t, ytdlp.RankAudio(formats), []wantTrack{
		{formatID: "140-7", language: "en", label: "en (Original)", original: true, selected: true},
	})
}

// (b) Deutsch existiert nur als Audiodeskription — dann bleibt nur das
// englische Original ausgewählt. Regel 2 nennt drei Textmarker (auch
// case-insensitiv), die alle greifen müssen.
func TestRankAudioGermanOnlyAudioDescription(t *testing.T) {
	for _, marker := range []string{"Audiodeskription", "Audio Description", "audio_desc"} {
		t.Run(marker, func(t *testing.T) {
			formats := []ytdlp.Format{
				{ID: "audio-de", ACodec: "mp4a", Language: "de", Note: marker, ABR: 128},
				{ID: "audio-en", ACodec: "mp4a", Language: "en", Note: "Original", ABR: 128},
			}
			checkTracks(t, ytdlp.RankAudio(formats), []wantTrack{
				{formatID: "audio-de", language: "de", label: "de (Audiodeskription)"},
				{formatID: "audio-en", language: "en", label: "en (Original)", original: true, selected: true},
			})
		})
	}
}

// (c) Ohne Sprachangaben ist keine Spurauswahl möglich.
func TestRankAudioWithoutLanguageInfo(t *testing.T) {
	formats := []ytdlp.Format{
		{ID: "140", ACodec: "mp4a.40.2", ABR: 129},
		{ID: "251", ACodec: "opus", ABR: 132},
	}
	if got := ytdlp.RankAudio(formats); len(got) != 0 {
		t.Fatalf("erwartet leere Liste, bekam %+v", got)
	}
}

// (d) Ist Deutsch selbst das Original, darf es nur einmal ausgewählt sein.
func TestRankAudioGermanIsOriginal(t *testing.T) {
	formats := []ytdlp.Format{
		{ID: "140-0", ACodec: "mp4a.40.2", Language: "de-DE", LanguagePreference: intp(10), ABR: 129},
		{ID: "140-1", ACodec: "mp4a.40.2", Language: "en-US", LanguagePreference: intp(-1), ABR: 129},
	}
	got := ytdlp.RankAudio(formats)
	checkTracks(t, got, []wantTrack{
		{formatID: "140-0", language: "de", label: "de (Original)", original: true, selected: true},
		{formatID: "140-1", language: "en", label: "en"},
	})
	selected := 0
	for _, tr := range got {
		if tr.Selected {
			selected++
		}
	}
	if selected != 1 {
		t.Fatalf("%d Spuren ausgewählt, erwartet genau 1", selected)
	}
}

// Regel 1: Nur reine Tonspuren zählen — Video-only und gemuxte Formate nicht.
func TestRankAudioIgnoresNonAudioFormats(t *testing.T) {
	formats := []ytdlp.Format{
		{ID: "137", ACodec: "none", VCodec: "avc1.640028", Resolution: "1920x1080", Language: "de"},
		{ID: "18", ACodec: "mp4a.40.2", VCodec: "avc1.42001E", Resolution: "640x360", Language: "de"},
		{ID: "140-0", ACodec: "mp4a.40.2", VCodec: "none", Language: "de-DE", ABR: 129},
	}
	checkTracks(t, ytdlp.RankAudio(formats), []wantTrack{
		{formatID: "140-0", language: "de", label: "de", selected: true},
	})
}

// Regel 6: Ohne wählbare de-Spur und ohne erkanntes Original greift Englisch,
// zuletzt die erste Spur.
func TestRankAudioSelectionFallback(t *testing.T) {
	t.Run("englische Spur, wenn de nur Audiodeskription ist", func(t *testing.T) {
		formats := []ytdlp.Format{
			{ID: "audio-de", ACodec: "mp4a", Language: "de", Note: "Audiodeskription", ABR: 128},
			{ID: "audio-en", ACodec: "mp4a", Language: "en", ABR: 128},
			{ID: "audio-fr", ACodec: "mp4a", Language: "fr", ABR: 128},
		}
		checkTracks(t, ytdlp.RankAudio(formats), []wantTrack{
			{formatID: "audio-de", language: "de", label: "de (Audiodeskription)"},
			{formatID: "audio-en", language: "en", label: "en", selected: true},
			{formatID: "audio-fr", language: "fr", label: "fr"},
		})
	})
	t.Run("erste Spur, wenn weder de noch en vorhanden ist", func(t *testing.T) {
		formats := []ytdlp.Format{
			{ID: "audio-fr", ACodec: "mp4a", Language: "fr", ABR: 128},
			{ID: "audio-es", ACodec: "mp4a", Language: "es", ABR: 128},
		}
		checkTracks(t, ytdlp.RankAudio(formats), []wantTrack{
			{formatID: "audio-fr", language: "fr", label: "fr", selected: true},
			{formatID: "audio-es", language: "es", label: "es"},
		})
	})
}

// Regel 8: de zuerst, dann en, der Rest in Eingabereihenfolge.
func TestRankAudioSortsGermanThenEnglishThenRest(t *testing.T) {
	formats := []ytdlp.Format{
		{ID: "audio-fr", ACodec: "mp4a", Language: "fr", ABR: 128},
		{ID: "audio-es", ACodec: "mp4a", Language: "es", ABR: 128},
		{ID: "audio-en", ACodec: "mp4a", Language: "en-US", ABR: 128},
		{ID: "audio-de", ACodec: "mp4a", Language: "de-DE", ABR: 128},
	}
	checkTracks(t, ytdlp.RankAudio(formats), []wantTrack{
		{formatID: "audio-de", language: "de", label: "de", selected: true},
		{formatID: "audio-en", language: "en", label: "en"},
		{formatID: "audio-fr", language: "fr", label: "fr"},
		{formatID: "audio-es", language: "es", label: "es"},
	})
}

// Regel 5: Ohne ABR entscheidet die Gesamtbitrate über die beste Spur.
func TestRankAudioFallsBackToTBR(t *testing.T) {
	formats := []ytdlp.Format{
		{ID: "audio-lo", ACodec: "mp4a", Language: "de", TBR: 96},
		{ID: "audio-hi", ACodec: "mp4a", Language: "de", TBR: 192},
	}
	checkTracks(t, ytdlp.RankAudio(formats), []wantTrack{
		{formatID: "audio-hi", language: "de", label: "de", selected: true},
	})
}
