package sessionstore

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"antigravity.dev/agy-sdk/pkg/aicode"
	"antigravity.dev/agy-sdk/pkg/cortex"
)

func TestStoreSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	rec := store.NewSession("/tmp/ws-a", "gemini-3-flash", cortex.ModeAgent)
	rec.Title = "Fix build"
	rec.History = []*aicode.Content{
		{Role: "user", Parts: []*aicode.Part{{Text: "hello"}}},
		{Role: "model", Parts: []*aicode.Part{{Text: "hi there"}}},
	}

	if err := store.Save(rec); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, rec.ID+".json")); err != nil {
		t.Fatalf("expected session file to exist: %v", err)
	}

	loaded, err := store.Load(rec.ID)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if loaded.ID != rec.ID || loaded.Title != "Fix build" || loaded.Model != "gemini-3-flash" || loaded.Mode != cortex.ModeAgent {
		t.Fatalf("unexpected loaded metadata: %+v", loaded)
	}
	if len(loaded.History) != 2 || loaded.History[0].Parts[0].Text != "hello" || loaded.History[1].Parts[0].Text != "hi there" {
		t.Fatalf("unexpected loaded history: %+v", loaded.History)
	}
}

func TestStoreLatestAndListSortingByCwd(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	wsA := filepath.Join(t.TempDir(), "ws-a")
	wsB := filepath.Join(t.TempDir(), "ws-b")
	if err := os.MkdirAll(wsA, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(wsB, 0o755); err != nil {
		t.Fatal(err)
	}

	s1 := store.NewSession(wsA, "model-1", cortex.ModePlan)
	s1.Title = "oldest-a"
	if err := store.Save(s1); err != nil {
		t.Fatal(err)
	}

	time.Sleep(5 * time.Millisecond)

	s2 := store.NewSession(wsB, "model-2", cortex.ModeAgent)
	s2.Title = "latest-b"
	if err := store.Save(s2); err != nil {
		t.Fatal(err)
	}

	time.Sleep(5 * time.Millisecond)

	s3 := store.NewSession(wsA, "model-3", cortex.ModeAgent)
	s3.Title = "newest-a"
	s3.History = []*aicode.Content{{Role: "user", Parts: []*aicode.Part{{Text: "one"}}}}
	if err := store.Save(s3); err != nil {
		t.Fatal(err)
	}

	listA, err := store.List(wsA)
	if err != nil {
		t.Fatalf("List(wsA) failed: %v", err)
	}
	if len(listA) != 2 {
		t.Fatalf("expected 2 sessions for wsA, got %d", len(listA))
	}
	if listA[0].ID != s3.ID || listA[1].ID != s1.ID {
		t.Fatalf("expected newest-first [%s, %s], got [%s, %s]", s3.ID, s1.ID, listA[0].ID, listA[1].ID)
	}
	if listA[0].MessageCount != 1 {
		t.Fatalf("expected MessageCount 1, got %d", listA[0].MessageCount)
	}

	latestA, err := store.Latest(wsA)
	if err != nil {
		t.Fatalf("Latest(wsA) failed: %v", err)
	}
	if latestA.ID != s3.ID {
		t.Fatalf("expected Latest(wsA) = %s, got %s", s3.ID, latestA.ID)
	}

	latestAll, err := store.Latest("")
	if err != nil {
		t.Fatalf("Latest(\"\") failed: %v", err)
	}
	if latestAll.ID != s3.ID {
		t.Fatalf("expected Latest(\"\") = %s, got %s", s3.ID, latestAll.ID)
	}
}

func TestStoreErrors(t *testing.T) {
	store := NewStore(t.TempDir())

	if _, err := store.Load("missing"); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("expected ErrSessionNotFound, got %v", err)
	}
	if _, err := store.Latest(""); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("expected ErrSessionNotFound on empty store, got %v", err)
	}
	if _, err := store.Load("../escape"); err == nil {
		t.Fatalf("expected error for path traversal id")
	}
}
