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
	if got.Theme != "auto" {
		t.Fatalf("Default-Theme = %q, erwartet \"auto\"", got.Theme)
	}
}

// TestOpenLegacyFileWithoutTheme: eine bestehende settings.json ohne
// theme-Feld darf den Theme-Default nicht überschreiben.
func TestOpenLegacyFileWithoutTheme(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte(`{"default_profile":"720p"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	st, err := settings.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	got := st.Get()
	if got.DefaultProfile != "720p" || got.Theme != "auto" {
		t.Fatalf("Legacy-Load liefert %+v", got)
	}
}

func TestSetPersistsAndReloads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	st, err := settings.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Set(settings.Settings{DefaultProfile: "1080p-mp4", Theme: "dark"}); err != nil {
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
	if got.DefaultProfile != "1080p-mp4" || got.Theme != "dark" {
		t.Fatalf("Reload liefert %+v", got)
	}
}

// TestSetKeepsOldValueOnPersistError: schlägt das Schreiben fehl (hier:
// Zielverzeichnis existiert nicht), darf der neue Wert nicht im Speicher
// hängen bleiben — Get muss weiter den letzten persistierten Stand liefern.
func TestSetKeepsOldValueOnPersistError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fehlt", "settings.json")
	st, err := settings.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Set(settings.Settings{DefaultProfile: "720p", Theme: "dark"}); err == nil {
		t.Fatal("Set muss bei fehlendem Verzeichnis fehlschlagen")
	}
	got := st.Get()
	if got.DefaultProfile != "best" || got.Theme != "auto" {
		t.Fatalf("nach Persist-Fehler darf der Wert nicht wechseln: %+v", got)
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
