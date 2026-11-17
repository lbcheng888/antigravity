// Package sessionstore provides persistent JSON-backed storage for Cortex conversation sessions.
package sessionstore

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"antigravity.dev/agy-sdk/pkg/aicode"
	"antigravity.dev/agy-sdk/pkg/cortex"
)

// ErrSessionNotFound is returned when a requested session cannot be located.
var ErrSessionNotFound = errors.New("session not found")

// SessionRecord is the full persisted state of a single conversation session.
type SessionRecord struct {
	ID        string            `json:"id"`
	Title     string            `json:"title,omitempty"`
	Cwd       string            `json:"cwd"`
	Model     string            `json:"model,omitempty"`
	Mode      cortex.Mode       `json:"mode,omitempty"`
	CreatedAt time.Time         `json:"created_at"`
	UpdatedAt time.Time         `json:"updated_at"`
	History   []*aicode.Content `json:"history,omitempty"`
}

// SessionSummary describes a persisted session without loading its full conversation history.
type SessionSummary struct {
	ID           string      `json:"id"`
	Title        string      `json:"title,omitempty"`
	Cwd          string      `json:"cwd"`
	Model        string      `json:"model,omitempty"`
	Mode         cortex.Mode `json:"mode,omitempty"`
	CreatedAt    time.Time   `json:"created_at"`
	UpdatedAt    time.Time   `json:"updated_at"`
	MessageCount int         `json:"message_count"`
}

// Store persists SessionRecords as individual JSON files under a directory.
type Store struct {
	dir string
}

// NewStore creates a Store rooted at dir.
func NewStore(dir string) *Store {
	return &Store{dir: dir}
}

// Dir returns the root directory of the store.
func (s *Store) Dir() string {
	if s == nil {
		return ""
	}
	return s.dir
}

// NewSession constructs a new unsaved SessionRecord with a unique ID and timestamps.
func (s *Store) NewSession(cwd, model string, mode cortex.Mode) *SessionRecord {
	now := time.Now().UTC()
	if mode == "" {
		mode = cortex.ModeAgent
	}
	return &SessionRecord{
		ID:        newSessionID(now),
		Cwd:       normalizeCwd(cwd),
		Model:     model,
		Mode:      mode,
		CreatedAt: now,
		UpdatedAt: now,
	}
}

// Save atomically writes rec to <dir>/<id>.json, updating UpdatedAt.
func (s *Store) Save(rec *SessionRecord) error {
	if s == nil || strings.TrimSpace(s.dir) == "" {
		return fmt.Errorf("sessionstore: store directory is empty")
	}
	if rec == nil {
		return fmt.Errorf("sessionstore: session record is nil")
	}

	now := time.Now().UTC()
	if rec.ID == "" {
		rec.ID = newSessionID(now)
	}
	if err := validateSessionID(rec.ID); err != nil {
		return err
	}
	if rec.CreatedAt.IsZero() {
		rec.CreatedAt = now
	}
	rec.UpdatedAt = now
	rec.Cwd = normalizeCwd(rec.Cwd)

	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return fmt.Errorf("sessionstore: create dir: %w", err)
	}

	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return fmt.Errorf("sessionstore: marshal session: %w", err)
	}
	data = append(data, '\n')

	tmp, err := os.CreateTemp(s.dir, ".session-*.tmp")
	if err != nil {
		return fmt.Errorf("sessionstore: create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sessionstore: write temp file: %w", err)
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sessionstore: chmod temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("sessionstore: close temp file: %w", err)
	}

	target := filepath.Join(s.dir, rec.ID+".json")
	if err := os.Rename(tmpPath, target); err != nil {
		return fmt.Errorf("sessionstore: rename session file: %w", err)
	}
	cleanup = false
	return nil
}

// Load reads and decodes the session with the given ID.
func (s *Store) Load(id string) (*SessionRecord, error) {
	if s == nil || strings.TrimSpace(s.dir) == "" {
		return nil, fmt.Errorf("sessionstore: store directory is empty")
	}
	if err := validateSessionID(id); err != nil {
		return nil, err
	}

	path := filepath.Join(s.dir, id+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%w: %s", ErrSessionNotFound, id)
		}
		return nil, fmt.Errorf("sessionstore: read session %q: %w", id, err)
	}

	var rec SessionRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		return nil, fmt.Errorf("sessionstore: decode session %q: %w", id, err)
	}
	return &rec, nil
}

// Latest returns the most recently updated session matching cwd (or any session if cwd is empty).
func (s *Store) Latest(cwd string) (*SessionRecord, error) {
	summaries, err := s.List(cwd)
	if err != nil {
		return nil, err
	}
	if len(summaries) == 0 {
		return nil, ErrSessionNotFound
	}
	return s.Load(summaries[0].ID)
}

// List returns summaries of all stored sessions matching cwd (or all if cwd is empty), sorted newest first.
func (s *Store) List(cwd string) ([]SessionSummary, error) {
	if s == nil || strings.TrimSpace(s.dir) == "" {
		return nil, fmt.Errorf("sessionstore: store directory is empty")
	}

	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("sessionstore: read dir: %w", err)
	}

	wantCwd := normalizeCwd(cwd)
	summaries := make([]SessionSummary, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ".json")
		rec, err := s.Load(id)
		if err != nil {
			continue
		}
		if wantCwd != "" && normalizeCwd(rec.Cwd) != wantCwd {
			continue
		}
		summaries = append(summaries, SessionSummary{
			ID:           rec.ID,
			Title:        rec.Title,
			Cwd:          rec.Cwd,
			Model:        rec.Model,
			Mode:         rec.Mode,
			CreatedAt:    rec.CreatedAt,
			UpdatedAt:    rec.UpdatedAt,
			MessageCount: len(rec.History),
		})
	}

	sort.Slice(summaries, func(i, j int) bool {
		if !summaries[i].UpdatedAt.Equal(summaries[j].UpdatedAt) {
			return summaries[i].UpdatedAt.After(summaries[j].UpdatedAt)
		}
		if !summaries[i].CreatedAt.Equal(summaries[j].CreatedAt) {
			return summaries[i].CreatedAt.After(summaries[j].CreatedAt)
		}
		return summaries[i].ID > summaries[j].ID
	})
	return summaries, nil
}

func newSessionID(now time.Time) string {
	var buf [6]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return fmt.Sprintf("sess-%d", now.UnixNano())
	}
	return fmt.Sprintf("sess-%s-%s", now.UTC().Format("20060102T150405"), hex.EncodeToString(buf[:]))
}

func validateSessionID(id string) error {
	if id == "" || strings.ContainsAny(id, `/\`) || strings.Contains(id, "..") {
		return fmt.Errorf("sessionstore: invalid session id %q", id)
	}
	return nil
}

func normalizeCwd(cwd string) string {
	trimmed := strings.TrimSpace(cwd)
	if trimmed == "" {
		return ""
	}
	cleaned := filepath.Clean(trimmed)
	if resolved, err := filepath.EvalSymlinks(cleaned); err == nil {
		return resolved
	}
	return cleaned
}
