package memory

import (
	"context"
	"testing"
)

func TestFileStore_SaveAndSearch(t *testing.T) {
	dir := t.TempDir()
	store, err := NewFileStore(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ctx := context.Background()

	err = store.Save(ctx, Entry{
		ID:      "entry1",
		Content: "User prefers tab indentation",
		Tags:    []string{"preference", "editor"},
	})
	if err != nil {
		t.Fatalf("save error: %v", err)
	}

	err = store.Save(ctx, Entry{
		ID:      "entry2",
		Content: "Project uses Go 1.22 on AWS",
		Tags:    []string{"project", "infra"},
	})
	if err != nil {
		t.Fatalf("save error: %v", err)
	}

	results, err := store.Search(ctx, "tab", 5)
	if err != nil {
		t.Fatalf("search error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].ID != "entry1" {
		t.Errorf("expected entry1, got %s", results[0].ID)
	}

	results, err = store.Search(ctx, "preference", 5)
	if err != nil {
		t.Fatalf("search error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result for tag search, got %d", len(results))
	}
}

func TestFileStore_ListAndDelete(t *testing.T) {
	dir := t.TempDir()
	store, err := NewFileStore(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ctx := context.Background()
	store.Save(ctx, Entry{ID: "a", Content: "first", Tags: []string{"test"}})
	store.Save(ctx, Entry{ID: "b", Content: "second", Tags: []string{"test"}})

	entries, err := store.List(ctx)
	if err != nil {
		t.Fatalf("list error: %v", err)
	}
	if len(entries) != 2 {
		t.Errorf("expected 2 entries, got %d", len(entries))
	}

	err = store.Delete(ctx, "a")
	if err != nil {
		t.Fatalf("delete error: %v", err)
	}

	entries, err = store.List(ctx)
	if err != nil {
		t.Fatalf("list error: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("expected 1 entry after delete, got %d", len(entries))
	}
	if entries[0].ID != "b" {
		t.Errorf("expected remaining entry 'b', got '%s'", entries[0].ID)
	}
}
