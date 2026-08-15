// Package store persistiert Jobs als JSON-Datei (atomar via Temp-Datei + Rename).
package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"

	"ytdlweb/internal/job"
)

type Store struct {
	mu   sync.Mutex
	path string
	jobs []job.Job // Einfüge-Reihenfolge
}

func Open(path string) (*Store, error) {
	s := &Store{path: path}
	data, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return s, nil
	case err != nil:
		return nil, err
	}
	if err := json.Unmarshal(data, &s.jobs); err != nil {
		return nil, fmt.Errorf("jobs.json unlesbar: %w", err)
	}
	changed := false
	for i := range s.jobs {
		if s.jobs[i].State == job.StateRunning {
			s.jobs[i].State = job.StateQueued
			changed = true
		}
	}
	if changed {
		if err := s.persistLocked(); err != nil {
			return nil, err
		}
	}
	return s, nil
}

func (s *Store) Add(j job.Job) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.jobs = append(s.jobs, j)
	return s.persistLocked()
}

func (s *Store) Get(id string) (job.Job, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, j := range s.jobs {
		if j.ID == id {
			return j, true
		}
	}
	return job.Job{}, false
}

func (s *Store) List() []job.Job {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]job.Job, len(s.jobs))
	for i, j := range s.jobs {
		out[len(s.jobs)-1-i] = j
	}
	return out
}

func (s *Store) Update(id string, fn func(*job.Job)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.jobs {
		if s.jobs[i].ID == id {
			fn(&s.jobs[i])
			return s.persistLocked()
		}
	}
	return fmt.Errorf("job %s nicht gefunden", id)
}

// SetProgress aktualisiert nur den In-Memory-Zustand — bewusst kein
// Disk-Write pro Progress-Tick; persistiert wird beim nächsten Update.
func (s *Store) SetProgress(id string, p job.Progress) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.jobs {
		if s.jobs[i].ID == id {
			s.jobs[i].Progress = p
			return
		}
	}
}

func (s *Store) Remove(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.jobs {
		if s.jobs[i].ID == id {
			s.jobs = append(s.jobs[:i], s.jobs[i+1:]...)
			return s.persistLocked()
		}
	}
	return fmt.Errorf("job %s nicht gefunden", id)
}

func (s *Store) ClaimNextQueued() (job.Job, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.jobs {
		if s.jobs[i].State == job.StateQueued {
			s.jobs[i].State = job.StateRunning
			if err := s.persistLocked(); err != nil {
				s.jobs[i].State = job.StateQueued
				return job.Job{}, false
			}
			return s.jobs[i], true
		}
	}
	return job.Job{}, false
}

func (s *Store) persistLocked() error {
	data, err := json.MarshalIndent(s.jobs, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
