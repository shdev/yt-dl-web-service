package settings_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"ytdlweb/internal/settings"
)

func TestOpenWithoutFileUsesDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	st, err := settings.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	got := st.Get()
	if got.DefaultProfile != "best" {
		t.Fatalf("Default-Profil = %q, erwartet \"best\"", got.DefaultProfile)
	}
}

func TestSetPersistsAndReloads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	st, err := settings.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Set(settings.Settings{DefaultProfile: "1080p-mp4"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path + ".tmp"); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("Temp-Datei darf nach persist nicht liegen bleiben")
	}
	st2, err := settings.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	got := st2.Get()
	if got.DefaultProfile != "1080p-mp4" {
		t.Fatalf("Reload liefert %+v", got)
	}
}

func TestOpenCorruptFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte("kein json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := settings.Open(path); err == nil {
		t.Fatal("korrupte Datei muss Fehler liefern")
	}
}
