package ytdlp_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"ytdlweb/internal/ytdlp"
)

func TestParseProbeJSONVideo(t *testing.T) {
	data, err := os.ReadFile("testdata/video.json")
	if err != nil {
		t.Fatal(err)
	}
	res, err := ytdlp.ParseProbeJSON(data)
	if err != nil {
		t.Fatal(err)
	}
	if res.Type != "video" || res.Video == nil || res.Playlist != nil {
		t.Fatalf("kein Video-Resultat: %+v", res)
	}
	v := res.Video
	if v.Title != "Test Video" || v.ID != "dQw4w9WgXcQ" || len(v.Formats) != 3 {
		t.Fatalf("Video-Metadaten falsch: %+v", v)
	}
	// Format 303 hat nur filesize_approx — muss in Filesize landen
	var f303 ytdlp.Format
	for _, f := range v.Formats {
		if f.ID == "303" {
			f303 = f
		}
	}
	if f303.Filesize != 58342123 || f303.VCodec != "vp9" || f303.FPS != 50 {
		t.Fatalf("Format 303 falsch geparst: %+v", f303)
	}
}

func TestParseProbeJSONPlaylist(t *testing.T) {
	data, err := os.ReadFile("testdata/playlist.json")
	if err != nil {
		t.Fatal(err)
	}
	res, err := ytdlp.ParseProbeJSON(data)
	if err != nil {
		t.Fatal(err)
	}
	if res.Type != "playlist" || res.Playlist == nil || res.Video != nil {
		t.Fatalf("kein Playlist-Resultat: %+v", res)
	}
	pl := res.Playlist
	if pl.Title != "Test Playlist" || len(pl.Entries) != 2 {
		t.Fatalf("Einträge ohne URL müssen übersprungen werden: %+v", pl)
	}
	if pl.Entries[0].Title != "Erstes Video" || !strings.Contains(pl.Entries[0].URL, "watch?v=a") {
		t.Fatalf("erster Eintrag falsch: %+v", pl.Entries[0])
	}
}

func TestParseProbeJSONInvalid(t *testing.T) {
	if _, err := ytdlp.ParseProbeJSON([]byte("kein json")); err == nil {
		t.Fatal("ungültiges JSON muss Fehler liefern")
	}
}

func TestProberRunsBinary(t *testing.T) {
	t.Setenv("FIXTURE", "testdata/video.json")
	p := &ytdlp.Prober{Bin: "testdata/fake-yt-dlp.sh"}
	res, err := p.Probe(context.Background(), "https://example.com/v")
	if err != nil {
		t.Fatal(err)
	}
	if res.Type != "video" || res.Video.Title != "Test Video" {
		t.Fatalf("Probe-Resultat falsch: %+v", res)
	}
}

func TestProberSurfacesStderr(t *testing.T) {
	p := &ytdlp.Prober{Bin: "testdata/probe-fail.sh"}
	_, err := p.Probe(context.Background(), "https://example.invalid")
	if err == nil || !strings.Contains(err.Error(), "Unsupported URL") {
		t.Fatalf("stderr muss im Fehler auftauchen, war: %v", err)
	}
}
