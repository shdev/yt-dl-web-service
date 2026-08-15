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
}

func (r *ExecRunner) Run(ctx context.Context, j job.Job, onProgress func(job.Progress)) error {
	cmd := exec.CommandContext(ctx, r.Bin,
		"-f", j.Format,
		"-o", filepath.Join(r.DownloadDir, r.OutputTemplate),
		"--newline",
		"--progress-template", ProgressTemplate,
		"--continue",
		"--no-playlist",
		"--no-warnings",
		"--", j.URL,
	)
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
	sc := bufio.NewScanner(stdout)
	for sc.Scan() {
		if p, ok := ParseProgress(sc.Text()); ok {
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
