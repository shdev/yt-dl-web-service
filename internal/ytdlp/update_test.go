package ytdlp_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ytdlweb/internal/ytdlp"
)

func TestEnsureBinaryCopies(t *testing.T) {
	dir := t.TempDir()
	system := filepath.Join(dir, "yt-dlp-system")
	if err := os.WriteFile(system, []byte("#!/bin/sh\necho system\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfgDir := filepath.Join(dir, "config")
	got := ytdlp.EnsureBinary(system, cfgDir, false, t.Logf)
	want := filepath.Join(cfgDir, "bin", "yt-dlp")
	if got != want {
		t.Fatalf("Pfad %q, erwartet %q", got, want)
	}
	data, err := os.ReadFile(want)
	if err != nil || !strings.Contains(string(data), "system") {
		t.Fatalf("Kopie fehlt/falsch: %v %q", err, data)
	}
	info, _ := os.Stat(want)
	if info.Mode()&0o111 == 0 {
		t.Fatal("Kopie muss ausführbar sein")
	}
}

func TestEnsureBinaryKeepsExisting(t *testing.T) {
	dir := t.TempDir()
	system := filepath.Join(dir, "yt-dlp-system")
	_ = os.WriteFile(system, []byte("neu"), 0o755)
	cfgDir := filepath.Join(dir, "config")
	dst := filepath.Join(cfgDir, "bin", "yt-dlp")
	_ = os.MkdirAll(filepath.Dir(dst), 0o755)
	_ = os.WriteFile(dst, []byte("vorhanden"), 0o755)
	got := ytdlp.EnsureBinary(system, cfgDir, false, t.Logf)
	data, _ := os.ReadFile(got)
	if string(data) != "vorhanden" {
		t.Fatal("vorhandene Kopie darf nicht überschrieben werden")
	}
}

func TestEnsureBinaryFallsBackToSystem(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "gibtsnicht")
	got := ytdlp.EnsureBinary(missing, filepath.Join(dir, "config"), false, t.Logf)
	if got != missing {
		t.Fatalf("bei Kopierfehler muss der Systempfad zurückkommen, war %q", got)
	}
}
