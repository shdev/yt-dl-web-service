package ytdlp_test

import (
	"testing"

	"ytdlweb/internal/ytdlp"
)

func TestBuildFormat(t *testing.T) {
	cases := []struct {
		name, videoID, audioID string
		audioOnly              bool
		want                   string
	}{
		{"Video+Audio", "303", "251", false, "303+251"},
		{"nur Video-ID", "303", "", false, "303"},
		{"nur Audio-ID ohne audioOnly", "", "251", false, "bv*+251"},
		{"audioOnly mit ID", "303", "251", true, "251"},
		{"audioOnly ohne ID", "", "", true, "ba"},
		{"nichts gewählt = beste Qualität", "", "", false, "bv*+ba/b"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ytdlp.BuildFormat(c.videoID, c.audioID, c.audioOnly); got != c.want {
				t.Fatalf("BuildFormat(%q,%q,%v) = %q, erwartet %q",
					c.videoID, c.audioID, c.audioOnly, got, c.want)
			}
		})
	}
}

func TestProfileByKey(t *testing.T) {
	p, ok := ytdlp.ProfileByKey("1080p")
	if !ok || p.Expr != "bv*[height<=1080]+ba/b[height<=1080]" {
		t.Fatalf("1080p-Profil falsch: %+v (ok=%v)", p, ok)
	}
	if _, ok := ytdlp.ProfileByKey("gibtsnicht"); ok {
		t.Fatal("unbekannter Key muss ok=false liefern")
	}
	for _, key := range []string{"best", "1080p", "720p", "audio"} {
		if _, ok := ytdlp.ProfileByKey(key); !ok {
			t.Fatalf("Profil %s fehlt", key)
		}
	}
}
