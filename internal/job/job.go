// Package job definiert den Download-Job und seine Zustände.
package job

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

type State string

const (
	StateQueued   State = "queued"
	StateRunning  State = "running"
	StateDone     State = "done"
	StateError    State = "error"
	StateCanceled State = "canceled"
)

type Progress struct {
	Percent float64 `json:"percent"`
	Speed   string  `json:"speed"`
	ETA     string  `json:"eta"`
}

type Job struct {
	ID             string     `json:"id"`
	URL            string     `json:"url"`
	Title          string     `json:"title"`
	Format         string     `json:"format"`
	FormatLabel    string     `json:"format_label"`
	PlaylistTitle  string     `json:"playlist_title,omitempty"`
	State          State      `json:"state"`
	Progress       Progress   `json:"progress"`
	Error          string     `json:"error,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	FinishedAt     *time.Time `json:"finished_at,omitempty"`
	Filename       string     `json:"filename,omitempty"`
	AudioFormatIDs []string   `json:"audio_format_ids,omitempty"`
	MultiAudio     bool       `json:"multi_audio,omitempty"`
	Profile        string     `json:"profile,omitempty"`
	NeedsProbe     bool       `json:"needs_probe,omitempty"`
}

func New(url, title, format, formatLabel, playlistTitle string) Job {
	return Job{
		ID:            newID(),
		URL:           url,
		Title:         title,
		Format:        format,
		FormatLabel:   formatLabel,
		PlaylistTitle: playlistTitle,
		State:         StateQueued,
		CreatedAt:     time.Now().UTC(),
	}
}

func newID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}
