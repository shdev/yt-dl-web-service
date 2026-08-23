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

func TestBuildFormatMulti(t *testing.T) {
	cases := []struct {
		name      string
		videoID   string
		audioIDs  []string
		audioOnly bool
		want      string
	}{
		{"mehrere Audio-IDs ohne Video-ID", "", []string{"140-0", "140-7"}, false, "bv*+140-0+140-7"},
		{"Video-ID + mehrere Audio-IDs", "137", []string{"140-0", "140-7"}, false, "137+140-0+140-7"},
		{"audioOnly mit mehreren IDs nimmt nur die erste", "", []string{"140-0"}, true, "140-0"},
		{"audioOnly ohne IDs", "", nil, true, "ba"},
		{"keine Audio-IDs, nur Video-ID", "303", nil, false, "303"},
		{"nichts gewählt = beste Qualität", "", nil, false, "bv*+ba/b"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ytdlp.BuildFormatMulti(c.videoID, c.audioIDs, c.audioOnly); got != c.want {
				t.Fatalf("BuildFormatMulti(%q,%v,%v) = %q, erwartet %q",
					c.videoID, c.audioIDs, c.audioOnly, got, c.want)
			}
		})
	}
}

func TestProfileVideoExpr(t *testing.T) {
	cases := map[string]string{
		"best":      "bv*",
		"1080p-mp4": "bv*[height<=1080][ext=mp4]",
		"1080p":     "bv*[height<=1080]",
		"720p":      "bv*[height<=720]",
		"audio":     "",
	}
	for key, want := range cases {
		p, ok := ytdlp.ProfileByKey(key)
		if !ok {
			t.Fatalf("Profil %s fehlt", key)
		}
		if p.VideoExpr != want {
			t.Fatalf("VideoExpr für %s = %q, erwartet %q", key, p.VideoExpr, want)
		}
	}
}

func TestProfileByKey(t *testing.T) {
	p, ok := ytdlp.ProfileByKey("1080p")
	if !ok || p.Expr != "bv*[height<=1080]+ba/b[height<=1080]" {
		t.Fatalf("1080p-Profil falsch: %+v (ok=%v)", p, ok)
	}
	mp4, ok := ytdlp.ProfileByKey("1080p-mp4")
	if !ok || mp4.Expr != "bv*[height<=1080][ext=mp4]+ba[ext=m4a]/b[ext=mp4][height<=1080]/b[height<=1080]" {
		t.Fatalf("1080p-mp4-Profil falsch: %+v (ok=%v)", mp4, ok)
	}
	if _, ok := ytdlp.ProfileByKey("gibtsnicht"); ok {
		t.Fatal("unbekannter Key muss ok=false liefern")
	}
	for _, key := range []string{"best", "1080p-mp4", "1080p", "720p", "audio"} {
		if _, ok := ytdlp.ProfileByKey(key); !ok {
			t.Fatalf("Profil %s fehlt", key)
		}
	}
}
