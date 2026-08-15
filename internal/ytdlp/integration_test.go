//go:build integration

package ytdlp_test

import (
	"context"
	"os/exec"
	"testing"
	"time"

	"ytdlweb/internal/ytdlp"
)

// Prüft gegen das reale yt-dlp, dass unser Parsing zum -J-Format passt.
func TestProbeRealYtdlp(t *testing.T) {
	bin, err := exec.LookPath("yt-dlp")
	if err != nil {
		t.Skip("yt-dlp nicht installiert")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	p := &ytdlp.Prober{Bin: bin}
	// "Me at the zoo" — erstes YouTube-Video, sehr stabil verfügbar
	res, err := p.Probe(ctx, "https://www.youtube.com/watch?v=jNQXAC9IVRw")
	if err != nil {
		t.Fatalf("Probe fehlgeschlagen (Netz?): %v", err)
	}
	if res.Type != "video" || res.Video == nil || len(res.Video.Formats) == 0 {
		t.Fatalf("unerwartetes Resultat: %+v", res)
	}
}
