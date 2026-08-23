package ytdlp

import "testing"

// posterTemplate leitet aus dem Video-Output-Template den Dateinamen fürs
// Poster-Bild ab — Details siehe Tabelle unten und posterTemplate selbst.
func TestPosterTemplate(t *testing.T) {
	cases := []struct {
		name string
		tpl  string
		want string
	}{
		{
			name: "Default-Template mit .%(ext)s-Suffix",
			tpl:  "%(extractor)s/%(channel,uploader|Unbekannt)s/%(title)s [%(id)s].%(ext)s",
			want: "%(extractor)s/%(channel,uploader|Unbekannt)s/%(title)s [%(id)s]-poster.%(ext)s",
		},
		{
			name: "Exotisches Template ohne .%(ext)s-Suffix",
			tpl:  "%(title)s",
			want: "%(title)s-poster.%(ext)s",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := posterTemplate(c.tpl)
			if got != c.want {
				t.Fatalf("posterTemplate(%q) = %q, want %q", c.tpl, got, c.want)
			}
		})
	}
}
