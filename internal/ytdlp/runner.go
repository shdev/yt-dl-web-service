package ytdlp

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"ytdlweb/internal/job"
)

// ExecRunner führt yt-dlp als Kindprozess in eigener Prozessgruppe aus,
// damit Cancel auch von yt-dlp gestartete ffmpeg-Prozesse beendet.
type ExecRunner struct {
	Bin            string
	DownloadDir    string
	OutputTemplate string

	// OnFilename wird mit dem relativen (zu DownloadDir) Pfad der final
	// heruntergeladenen Datei aufgerufen, sobald yt-dlp ihn via
	// --print after_move:filepath meldet. Optional — nil ist erlaubt.
	OnFilename func(rel string)
}

func (r *ExecRunner) Run(ctx context.Context, j job.Job, onProgress func(job.Progress)) error {
	args := []string{
		"-f", j.Format,
		"-o", filepath.Join(r.DownloadDir, r.OutputTemplate),
		"--newline",
		// --print impliziert bei yt-dlp --quiet und unterdrückt sonst die
		// --progress-template-Ausgabe komplett (Regression vom 2026-08-23).
		"--progress",
		"--progress-template", ProgressTemplate,
		"--continue",
		"--no-playlist",
		"--no-warnings",
		"--print", "after_move:filepath",
		"--write-thumbnail",
		"--convert-thumbnails", "jpg",
		"-o", "thumbnail:" + filepath.Join(r.DownloadDir, posterTemplate(r.OutputTemplate)),
	}
	if j.MultiAudio {
		args = append(args, "--audio-multistreams", "--merge-output-format", "mp4/mkv")
	}
	args = append(args, "--", j.URL)
	cmd := exec.CommandContext(ctx, r.Bin, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
	}
	cmd.WaitDelay = 5 * time.Second // SIGKILL-Fallback, falls SIGTERM ignoriert wird

	stderr := &tailBuffer{max: 4096}
	cmd.Stderr = stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	// absDownloadDir dient dazu, die after_move:filepath-Zeile unter den
	// stdout-Zeilen zu erkennen: yt-dlp gibt dort den absoluten Zielpfad
	// aus, alles andere ist Progress-Template-Output.
	absDownloadDir, absErr := filepath.Abs(r.DownloadDir)
	if absErr != nil {
		absDownloadDir = r.DownloadDir
	}
	sc := bufio.NewScanner(stdout)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, absDownloadDir+string(filepath.Separator)) {
			if r.OnFilename != nil {
				if rel, relErr := filepath.Rel(absDownloadDir, line); relErr == nil {
					r.OnFilename(rel)
				}
			}
			continue
		}
		if p, ok := ParseProgress(line); ok {
			onProgress(p)
		}
	}
	if serr := sc.Err(); serr != nil {
		// Pipe leeren, sonst kann yt-dlp am vollen stdout blockieren und
		// cmd.Wait() hängt für immer (Worker-Slot ginge verloren).
		log.Printf("yt-dlp: stdout-Scan-Fehler, leere Pipe: %v", serr)
		_, _ = io.Copy(io.Discard, stdout)
	}
	err = cmd.Wait()
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("yt-dlp: %w: %s", err, stderr.String())
	}
	return nil
}

// posterTemplate leitet aus dem Video-Output-Template das Output-Template
// fürs Poster-Bild ab: das Suffix "-poster" landet vor der Extension, damit
// Poster und Video denselben Basisnamen teilen (Jellyfin/Plex/Kodi-Konvention
// für Artwork, z. B. "Titel [id]-poster.jpg" neben "Titel [id].webm").
// Endet tpl nicht auf ".%(ext)s" (exotisches Template), wird "-poster.%(ext)s"
// stattdessen angehängt statt eingefügt.
const extSuffix = ".%(ext)s"

func posterTemplate(tpl string) string {
	if strings.HasSuffix(tpl, extSuffix) {
		return strings.TrimSuffix(tpl, extSuffix) + "-poster" + extSuffix
	}
	return tpl + "-poster" + extSuffix
}

// tailBuffer behält die letzten max Bytes — genug für Fehlermeldungen,
// ohne bei langen Downloads unbegrenzt zu wachsen.
type tailBuffer struct {
	max int
	buf []byte
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.buf = append(t.buf, p...)
	if len(t.buf) > t.max {
		t.buf = t.buf[len(t.buf)-t.max:]
	}
	return len(p), nil
}

func (t *tailBuffer) String() string {
	return strings.TrimSpace(string(t.buf))
}
