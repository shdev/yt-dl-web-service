package ytdlp_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

// writeScript legt ein ausführbares Shell-Skript als yt-dlp-Ersatz an.
func writeScript(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "yt-dlp")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestManagerVersion(t *testing.T) {
	bin := writeScript(t, `[ "$1" = "--version" ] && { echo 2026.08.19; exit 0; }; exit 1`)
	m := &ytdlp.Manager{Bin: bin}
	v, err := m.Version(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if v != "2026.08.19" {
		t.Fatalf("Version %q, erwartet 2026.08.19", v)
	}
}

func TestManagerVersionError(t *testing.T) {
	bin := writeScript(t, `echo kaputt >&2; exit 1`)
	m := &ytdlp.Manager{Bin: bin}
	if _, err := m.Version(context.Background()); err == nil ||
		!strings.Contains(err.Error(), "kaputt") {
		t.Fatalf("Fehler mit stderr erwartet, war: %v", err)
	}
}

func TestManagerUpdate(t *testing.T) {
	bin := writeScript(t, `case "$1" in
-U) echo "Updated yt-dlp to 2026.08.19"; exit 0;;
--version) echo 2026.08.19; exit 0;;
esac; exit 1`)
	m := &ytdlp.Manager{Bin: bin}
	v, err := m.Update(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if v != "2026.08.19" {
		t.Fatalf("Version nach Update %q, erwartet 2026.08.19", v)
	}
}

func TestManagerUpdateFailure(t *testing.T) {
	bin := writeScript(t, `echo "ERROR: unable to update" >&2; exit 1`)
	m := &ytdlp.Manager{Bin: bin}
	if _, err := m.Update(context.Background()); err == nil ||
		!strings.Contains(err.Error(), "unable to update") {
		t.Fatalf("Fehler mit yt-dlp-Ausgabe erwartet, war: %v", err)
	}
}

// Ein zweites Update während ein erstes läuft muss sofort mit
// ErrUpdateRunning abgewiesen werden (kein Warten, kein Doppel-Update).
func TestManagerUpdateConcurrent(t *testing.T) {
	dir := t.TempDir()
	started := filepath.Join(dir, "started")
	release := filepath.Join(dir, "release")
	bin := writeScript(t, `case "$1" in
-U) touch `+started+`
    while [ ! -f `+release+` ]; do sleep 0.02; done
    exit 0;;
--version) echo 2026.08.19; exit 0;;
esac; exit 1`)
	m := &ytdlp.Manager{Bin: bin}

	done := make(chan error, 1)
	go func() {
		_, err := m.Update(context.Background())
		done <- err
	}()

	// Warten, bis das erste Update nachweislich läuft (Lock gehalten).
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(started); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("erstes Update ist nie gestartet")
		}
		time.Sleep(5 * time.Millisecond)
	}

	if _, err := m.Update(context.Background()); !errors.Is(err, ytdlp.ErrUpdateRunning) {
		t.Fatalf("ErrUpdateRunning erwartet, war: %v", err)
	}

	if err := os.WriteFile(release, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatalf("erstes Update muss durchlaufen: %v", err)
	}
}
