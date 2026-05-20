package memory

import (
	"context"
	"time"
)

type LongTermStore interface {
	Save(ctx context.Context, entry Entry) error
	Search(ctx context.Context, query string, limit int) ([]Entry, error)
	List(ctx context.Context) ([]Entry, error)
	Delete(ctx context.Context, id string) error
}

type Entry struct {
	ID        string            `json:"id"`
	Content   string            `json:"content"`
	Tags      []string          `json:"tags"`
	CreatedAt time.Time         `json:"created_at"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}
