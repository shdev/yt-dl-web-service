package ytdlp_test

import (
	"testing"

	"ytdlweb/internal/ytdlp"
)

func TestParseProgress(t *testing.T) {
	cases := []struct {
		name, line string
		wantOK     bool
		percent    float64
		speed, eta string
	}{
		{"normale Zeile", "dl:  45.3%|1.25MiB/s|00:35", true, 45.3, "1.25MiB/s", "00:35"},
		{"100 Prozent", "dl: 100.0%|1.10MiB/s|00:00", true, 100, "1.10MiB/s", "00:00"},
		{"NA-Werte", "dl:  10.0%|NA|NA", true, 10, "", ""},
		{"fremde Zeile", "[download] Destination: video.mp4", false, 0, "", ""},
		{"kaputte Zeile", "dl:abc|def", false, 0, "", ""},
		{"leere Zeile", "", false, 0, "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p, ok := ytdlp.ParseProgress(c.line)
			if ok != c.wantOK {
				t.Fatalf("ok = %v, erwartet %v", ok, c.wantOK)
			}
			if !ok {
				return
			}
			if p.Percent != c.percent || p.Speed != c.speed || p.ETA != c.eta {
				t.Fatalf("Parse(%q) = %+v", c.line, p)
			}
		})
	}
}
