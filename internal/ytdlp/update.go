package ytdlp

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// EnsureBinary stellt eine schreibbare yt-dlp-Kopie unter <configDir>/bin
// bereit, damit -U auch als Nicht-root funktioniert (Spec §3).
// Fehler sind nie fatal — es wird geloggt und der beste Pfad zurückgegeben.
func EnsureBinary(systemBin, configDir string, update bool, logf func(format string, args ...any)) string {
	dst := filepath.Join(configDir, "bin", "yt-dlp")
	if _, err := os.Stat(dst); errors.Is(err, os.ErrNotExist) {
		if err := copyBinary(systemBin, dst); err != nil {
			logf("yt-dlp-Kopie fehlgeschlagen, nutze %s: %v", systemBin, err)
			return systemBin
		}
	}
	if update {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		if out, err := exec.CommandContext(ctx, dst, "-U").CombinedOutput(); err != nil {
			logf("yt-dlp-Update fehlgeschlagen (weiter mit vorhandener Version): %v: %s",
				err, tailString(string(out), 300))
		}
	}
	return dst
}

// ErrUpdateRunning: ein Update läuft bereits — der zweite Aufruf wird
// abgewiesen statt zu warten, damit die UI sofort Rückmeldung geben kann.
var ErrUpdateRunning = errors.New("yt-dlp-Update läuft bereits")

// Manager kapselt Versionsabfrage und Selfupdate (-U) der yt-dlp-Kopie.
// Ein laufender Download ist dabei unkritisch: -U ersetzt die Datei per
// rename, gestartete Prozesse laufen auf dem alten Inode weiter.
type Manager struct {
	Bin string
	mu  sync.Mutex
}

func (m *Manager) Version(ctx context.Context) (string, error) {
	out, err := exec.CommandContext(ctx, m.Bin, "--version").Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) && len(ee.Stderr) > 0 {
			return "", fmt.Errorf("yt-dlp --version: %w: %s", err, tailString(string(ee.Stderr), 300))
		}
		return "", fmt.Errorf("yt-dlp --version: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// Update führt yt-dlp -U aus und liefert die danach installierte Version.
func (m *Manager) Update(ctx context.Context) (string, error) {
	if !m.mu.TryLock() {
		return "", ErrUpdateRunning
	}
	defer m.mu.Unlock()
	if out, err := exec.CommandContext(ctx, m.Bin, "-U").CombinedOutput(); err != nil {
		return "", fmt.Errorf("yt-dlp -U: %w: %s", err, tailString(string(out), 300))
	}
	return m.Version(ctx)
}

func copyBinary(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o755)
}
