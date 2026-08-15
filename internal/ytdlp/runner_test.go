package ytdlp_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"ytdlweb/internal/job"
	"ytdlweb/internal/ytdlp"
)

func testJob() job.Job {
	return job.New("https://example.com/v", "Titel", "ba", "Nur Audio", "")
}

func TestExecRunnerReportsProgress(t *testing.T) {
	r := &ytdlp.ExecRunner{
		Bin: "testdata/progress.sh", DownloadDir: t.TempDir(),
		OutputTemplate: "%(title)s.%(ext)s",
	}
	var mu sync.Mutex
	var got []job.Progress
	err := r.Run(context.Background(), testJob(), func(p job.Progress) {
		mu.Lock()
		got = append(got, p)
		mu.Unlock()
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("3 Progress-Updates erwartet, %d bekommen: %+v", len(got), got)
	}
	if got[2].Percent != 100 || got[1].Speed != "1.25MiB/s" {
		t.Fatalf("Progress falsch geparst: %+v", got)
	}
}

func TestExecRunnerSurfacesStderr(t *testing.T) {
	r := &ytdlp.ExecRunner{
		Bin: "testdata/dl-fail.sh", DownloadDir: t.TempDir(),
		OutputTemplate: "x.%(ext)s",
	}
	err := r.Run(context.Background(), testJob(), func(job.Progress) {})
	if err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("stderr muss im Fehler auftauchen, war: %v", err)
	}
}

func TestExecRunnerCancel(t *testing.T) {
	r := &ytdlp.ExecRunner{
		Bin: "testdata/dl-sleep.sh", DownloadDir: t.TempDir(),
		OutputTemplate: "x.%(ext)s",
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- r.Run(ctx, testJob(), func(job.Progress) {}) }()
	time.Sleep(200 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("context.Canceled erwartet, war: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Runner hat auf Cancel nicht reagiert")
	}
}
