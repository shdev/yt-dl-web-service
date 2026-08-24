// Package settings persistiert nutzerkonfigurierbare Einstellungen als JSON-Datei
// (atomar via Temp-Datei + Rename, analog internal/store).
package settings

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
)

// Settings sind die über die UI konfigurierbaren Einstellungen.
type Settings struct {
	DefaultProfile string `json:"default_profile"`
	// Theme: "auto" (folgt der Systemeinstellung), "light" oder "dark".
	Theme string `json:"theme"`
}

// defaults sind die Werte, mit denen ein frischer Store startet.
func defaults() Settings {
	return Settings{DefaultProfile: "best", Theme: "auto"}
}

type Store struct {
	mu    sync.Mutex
	path  string
	value Settings
}

// Open lädt die Einstellungen aus path. Fehlt die Datei, startet der Store
// mit Defaults. Eine vorhandene, aber kaputte Datei ist ein Fehler.
func Open(path string) (*Store, error) {
	s := &Store{path: path, value: defaults()}
	data, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return s, nil
	case err != nil:
		return nil, err
	}
	if err := json.Unmarshal(data, &s.value); err != nil {
		return nil, fmt.Errorf("settings.json unlesbar: %w", err)
	}
	return s, nil
}

// Get liefert eine Kopie der aktuellen Einstellungen.
func (s *Store) Get() Settings {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.value
}

// Set setzt die Einstellungen und persistiert sie atomar. Schlägt das
// Persistieren fehl, bleibt der alte Wert auch im Speicher aktiv — sonst
// würden Reads bis zum Neustart einen nie gespeicherten Zustand liefern.
func (s *Store) Set(v Settings) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	old := s.value
	s.value = v
	if err := s.persistLocked(); err != nil {
		s.value = old
		return err
	}
	return nil
}

func (s *Store) persistLocked() error {
	data, err := json.MarshalIndent(s.value, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
