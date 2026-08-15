// Der Serverprozess: Konfiguration lesen, Store laden, Queue starten,
// HTTP bedienen. Mit -healthcheck prüft sich der Container selbst.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"ytdlweb/internal/config"
	"ytdlweb/internal/queue"
	"ytdlweb/internal/server"
	"ytdlweb/internal/store"
	"ytdlweb/internal/ytdlp"
)

// systemYtdlp ist der Pfad im mikenye/youtube-dl-Base-Image (Spec §2).
const systemYtdlp = "/usr/local/bin/yt-dlp"

func main() {
	healthcheck := flag.Bool("healthcheck", false, "Healthcheck ausführen und beenden")
	flag.Parse()

	cfg := config.FromEnv(os.Getenv)

	if *healthcheck {
		os.Exit(runHealthcheck(cfg.Port))
	}
	if err := run(cfg); err != nil {
		log.Fatal(err)
	}
}

func runHealthcheck(port int) int {
	client := &http.Client{Timeout: 3 * time.Second}
	res, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/healthz", port))
	if err != nil || res.StatusCode != http.StatusOK {
		return 1
	}
	return 0
}

func run(cfg config.Config) error {
	for _, dir := range []string{cfg.DownloadDir, cfg.ConfigDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}

	bin := ytdlp.EnsureBinary(systemYtdlp, cfg.ConfigDir, cfg.UpdateOnStart, log.Printf)
	log.Printf("verwende yt-dlp: %s", bin)

	st, err := store.Open(filepath.Join(cfg.ConfigDir, "jobs.json"))
	if err != nil {
		return err
	}

	runner := &ytdlp.ExecRunner{
		Bin:            bin,
		DownloadDir:    cfg.DownloadDir,
		OutputTemplate: cfg.OutputTemplate,
	}
	q := queue.New(st, runner, cfg.MaxConcurrent)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	q.Start(ctx)
	q.Kick() // durch Recovery wieder eingereihte Jobs sofort anstoßen

	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.Port),
		Handler:           server.New(st, q, &ytdlp.Prober{Bin: bin}),
		ReadHeaderTimeout: 10 * time.Second,
	}
	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe() }()
	log.Printf("yt-dl-web-client läuft auf Port %d", cfg.Port)

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
		return nil
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}
