package ytdlp_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
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

func TestExecRunnerSurvivesOversizedLine(t *testing.T) {
	r := &ytdlp.ExecRunner{
		Bin: "testdata/dl-hugeline.sh", DownloadDir: t.TempDir(),
		OutputTemplate: "x.%(ext)s",
	}
	done := make(chan error, 1)
	go func() { done <- r.Run(context.Background(), testJob(), func(job.Progress) {}) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Scan-Fehler darf den Job nicht scheitern lassen: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Runner hängt bei überlanger stdout-Zeile")
	}
}

func TestExecRunnerArgsAlwaysPrintFilepath(t *testing.T) {
	dir := t.TempDir()
	argsFile := filepath.Join(dir, "args.txt")
	t.Setenv("ARGS_FILE", argsFile)
	r := &ytdlp.ExecRunner{
		Bin: "testdata/echo-args.sh", DownloadDir: dir,
		OutputTemplate: "x.%(ext)s",
	}
	if err := r.Run(context.Background(), testJob(), func(job.Progress) {}); err != nil {
		t.Fatal(err)
	}
	args := readArgs(t, argsFile)
	if !containsSeq(args, "--print", "after_move:filepath") {
		t.Fatalf("--print after_move:filepath fehlt in Argumenten: %v", args)
	}
	if containsAny(args, "--audio-multistreams", "--merge-output-format") {
		t.Fatalf("Multistream-Flags dürfen ohne MultiAudio nicht gesetzt sein: %v", args)
	}
}

func TestExecRunnerArgsMultistreamWhenMultiAudio(t *testing.T) {
	dir := t.TempDir()
	argsFile := filepath.Join(dir, "args.txt")
	t.Setenv("ARGS_FILE", argsFile)
	r := &ytdlp.ExecRunner{
		Bin: "testdata/echo-args.sh", DownloadDir: dir,
		OutputTemplate: "x.%(ext)s",
	}
	j := testJob()
	j.MultiAudio = true
	if err := r.Run(context.Background(), j, func(job.Progress) {}); err != nil {
		t.Fatal(err)
	}
	args := readArgs(t, argsFile)
	if !containsSeq(args, "--print", "after_move:filepath") {
		t.Fatalf("--print after_move:filepath fehlt in Argumenten: %v", args)
	}
	if !containsSeq(args, "--audio-multistreams") {
		t.Fatalf("--audio-multistreams fehlt bei MultiAudio: %v", args)
	}
	if !containsSeq(args, "--merge-output-format", "mp4/mkv") {
		t.Fatalf("--merge-output-format mp4/mkv fehlt bei MultiAudio: %v", args)
	}
}

func TestExecRunnerReportsFilename(t *testing.T) {
	dir := t.TempDir()
	absDir, err := filepath.Abs(dir)
	if err != nil {
		t.Fatal(err)
	}
	rel := filepath.Join("youtube", "Kanal", "Titel [id].mkv")
	t.Setenv("PRINT_LINE", filepath.Join(absDir, rel))

	r := &ytdlp.ExecRunner{
		Bin: "testdata/filepath.sh", DownloadDir: dir,
		OutputTemplate: "%(title)s.%(ext)s",
	}
	var muFn sync.Mutex
	var filenameCalls int
	var gotFilename string
	r.OnFilename = func(rel string) {
		muFn.Lock()
		filenameCalls++
		gotFilename = rel
		muFn.Unlock()
	}

	var progressCalls int
	err = r.Run(context.Background(), testJob(), func(job.Progress) {
		progressCalls++
	})
	if err != nil {
		t.Fatal(err)
	}
	if filenameCalls != 1 {
		t.Fatalf("OnFilename muss genau 1x aufgerufen werden, war %d", filenameCalls)
	}
	if gotFilename != rel {
		t.Fatalf("relativer Pfad falsch: got %q want %q", gotFilename, rel)
	}
	if progressCalls != 2 {
		t.Fatalf("Progress-Parsing muss unverändert bleiben (2 Updates erwartet): %d", progressCalls)
	}
}

// Bei relativ konfiguriertem DownloadDir muss der Runner die Pfadzeile
// trotzdem erkennen — dafür normalisiert er DownloadDir per filepath.Abs,
// bevor er stdout-Zeilen darauf prüft.
func TestExecRunnerReportsFilenameWithRelativeDownloadDir(t *testing.T) {
	relDir, err := os.MkdirTemp(".", "downloaddir-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(relDir) })

	absDir, err := filepath.Abs(relDir)
	if err != nil {
		t.Fatal(err)
	}
	rel := filepath.Join("youtube", "Kanal", "Titel [id].mkv")
	t.Setenv("PRINT_LINE", filepath.Join(absDir, rel))

	r := &ytdlp.ExecRunner{
		Bin: "testdata/filepath.sh", DownloadDir: relDir,
		OutputTemplate: "%(title)s.%(ext)s",
	}
	var gotFilename string
	r.OnFilename = func(rel string) { gotFilename = rel }

	if err := r.Run(context.Background(), testJob(), func(job.Progress) {}); err != nil {
		t.Fatal(err)
	}
	if gotFilename != rel {
		t.Fatalf("relativer Pfad falsch bei relativem DownloadDir: got %q want %q", gotFilename, rel)
	}
}

func readArgs(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("Args-Datei nicht lesbar: %v", err)
	}
	trimmed := strings.TrimRight(string(data), "\n")
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "\n")
}

func containsSeq(args []string, seq ...string) bool {
	for i := 0; i+len(seq) <= len(args); i++ {
		match := true
		for j, s := range seq {
			if args[i+j] != s {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

func containsAny(args []string, vals ...string) bool {
	for _, a := range args {
		for _, v := range vals {
			if a == v {
				return true
			}
		}
	}
	return false
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
