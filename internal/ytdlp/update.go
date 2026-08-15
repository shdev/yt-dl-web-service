package ytdlp

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
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
