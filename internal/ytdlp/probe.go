package ytdlp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

type Format struct {
	ID         string  `json:"format_id"`
	Ext        string  `json:"ext"`
	Resolution string  `json:"resolution"`
	FPS        float64 `json:"fps,omitempty"`
	TBR        float64 `json:"tbr,omitempty"`
	ABR        float64 `json:"abr,omitempty"`
	VCodec     string  `json:"vcodec"`
	ACodec     string  `json:"acodec"`
	Note       string  `json:"format_note,omitempty"`
	Filesize   int64   `json:"filesize,omitempty"`
}

type Video struct {
	ID        string   `json:"id"`
	Title     string   `json:"title"`
	Thumbnail string   `json:"thumbnail,omitempty"`
	Duration  float64  `json:"duration,omitempty"`
	Formats   []Format `json:"formats"`
}

type PlaylistEntry struct {
	URL   string `json:"url"`
	Title string `json:"title"`
}

type Playlist struct {
	Title   string          `json:"title"`
	Entries []PlaylistEntry `json:"entries"`
}

type ProbeResult struct {
	Type     string    `json:"type"`
	Video    *Video    `json:"video,omitempty"`
	Playlist *Playlist `json:"playlist,omitempty"`
}

// rawInfo bildet die yt-dlp -J-Ausgabe ab (nur benötigte Felder).
type rawInfo struct {
	Type      string  `json:"_type"`
	ID        string  `json:"id"`
	Title     string  `json:"title"`
	Thumbnail string  `json:"thumbnail"`
	Duration  float64 `json:"duration"`
	Formats   []struct {
		FormatID       string  `json:"format_id"`
		Ext            string  `json:"ext"`
		Resolution     string  `json:"resolution"`
		FPS            float64 `json:"fps"`
		TBR            float64 `json:"tbr"`
		ABR            float64 `json:"abr"`
		VCodec         string  `json:"vcodec"`
		ACodec         string  `json:"acodec"`
		FormatNote     string  `json:"format_note"`
		Filesize       int64   `json:"filesize"`
		FilesizeApprox int64   `json:"filesize_approx"`
	} `json:"formats"`
	Entries []struct {
		URL   string `json:"url"`
		Title string `json:"title"`
	} `json:"entries"`
}

func ParseProbeJSON(data []byte) (*ProbeResult, error) {
	var raw rawInfo
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("yt-dlp-JSON unlesbar: %w", err)
	}
	if raw.Type == "playlist" {
		pl := &Playlist{Title: raw.Title}
		for _, e := range raw.Entries {
			if strings.TrimSpace(e.URL) == "" {
				continue
			}
			pl.Entries = append(pl.Entries, PlaylistEntry{URL: e.URL, Title: e.Title})
		}
		return &ProbeResult{Type: "playlist", Playlist: pl}, nil
	}
	v := &Video{ID: raw.ID, Title: raw.Title, Thumbnail: raw.Thumbnail, Duration: raw.Duration}
	for _, f := range raw.Formats {
		size := f.Filesize
		if size == 0 {
			size = f.FilesizeApprox
		}
		v.Formats = append(v.Formats, Format{
			ID: f.FormatID, Ext: f.Ext, Resolution: f.Resolution,
			FPS: f.FPS, TBR: f.TBR, ABR: f.ABR,
			VCodec: f.VCodec, ACodec: f.ACodec, Note: f.FormatNote,
			Filesize: size,
		})
	}
	return &ProbeResult{Type: "video", Video: v}, nil
}

type Prober struct {
	Bin string
}

func (p *Prober) Probe(ctx context.Context, rawURL string) (*ProbeResult, error) {
	if strings.HasPrefix(rawURL, "-") {
		return nil, fmt.Errorf("ungültige URL")
	}
	cmd := exec.CommandContext(ctx, p.Bin, "-J", "--flat-playlist", "--no-warnings", "--", rawURL)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("yt-dlp: %s", tailString(stderr.String(), 500))
	}
	return ParseProbeJSON(stdout.Bytes())
}

// tailString liefert höchstens die letzten n Zeichen (für stderr-Auszüge).
func tailString(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}
