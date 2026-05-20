package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type FileStore struct {
	dir       string
	indexPath string
}

func NewFileStore(dir string) (*FileStore, error) {
	entriesDir := filepath.Join(dir, "entries")
	if err := os.MkdirAll(entriesDir, 0755); err != nil {
		return nil, fmt.Errorf("create entries dir: %w", err)
	}

	return &FileStore{
		dir:       dir,
		indexPath: filepath.Join(dir, "index.json"),
	}, nil
}

func (f *FileStore) Save(ctx context.Context, entry Entry) error {
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = time.Now()
	}

	entryPath := filepath.Join(f.dir, "entries", entry.ID+".json")
	data, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal entry: %w", err)
	}

	if err := os.WriteFile(entryPath, data, 0644); err != nil {
		return fmt.Errorf("write entry: %w", err)
	}

	return f.rebuildIndex()
}

func (f *FileStore) Search(ctx context.Context, query string, limit int) ([]Entry, error) {
	entries, err := f.List(ctx)
	if err != nil {
		return nil, err
	}

	query = strings.ToLower(query)
	var results []Entry
	for _, entry := range entries {
		if f.matches(entry, query) {
			results = append(results, entry)
			if len(results) >= limit {
				break
			}
		}
	}
	return results, nil
}

func (f *FileStore) List(ctx context.Context) ([]Entry, error) {
	entriesDir := filepath.Join(f.dir, "entries")
	files, err := os.ReadDir(entriesDir)
	if err != nil {
		return nil, fmt.Errorf("read entries dir: %w", err)
	}

	var entries []Entry
	for _, file := range files {
		if !strings.HasSuffix(file.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(entriesDir, file.Name()))
		if err != nil {
			continue
		}
		var entry Entry
		if err := json.Unmarshal(data, &entry); err != nil {
			continue
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

func (f *FileStore) Delete(ctx context.Context, id string) error {
	entryPath := filepath.Join(f.dir, "entries", id+".json")
	if err := os.Remove(entryPath); err != nil {
		return fmt.Errorf("delete entry: %w", err)
	}
	return f.rebuildIndex()
}

func (f *FileStore) matches(entry Entry, query string) bool {
	if strings.Contains(strings.ToLower(entry.Content), query) {
		return true
	}
	for _, tag := range entry.Tags {
		if strings.Contains(strings.ToLower(tag), query) {
			return true
		}
	}
	return false
}

func (f *FileStore) rebuildIndex() error {
	entries, err := f.List(context.Background())
	if err != nil {
		return err
	}

	type indexEntry struct {
		ID   string   `json:"id"`
		Tags []string `json:"tags"`
	}

	index := make([]indexEntry, 0, len(entries))
	for _, e := range entries {
		index = append(index, indexEntry{ID: e.ID, Tags: e.Tags})
	}

	data, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(f.indexPath, data, 0644)
}
