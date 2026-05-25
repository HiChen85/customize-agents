package core

import (
	"context"
	"testing"
	"time"

	"github.com/haichen-zhang/customize-agents/llm"
	"github.com/haichen-zhang/customize-agents/memory"
)

func TestSessionManager_GetOrCreate_NewSession(t *testing.T) {
	factory := &SessionFactory{
		Provider:  &mockSessionProv{},
		Tools:     nil,
		Skills:    nil,
		Store:     &mockSessionStore{},
		MaxTokens: 4096,
	}

	mgr := NewSessionManager(SessionConfig{
		MaxSessions:     10,
		TTL:             30 * time.Minute,
		CleanupInterval: 1 * time.Minute,
	}, factory)
	defer mgr.Stop()

	session, created, err := mgr.GetOrCreate("session-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !created {
		t.Error("expected new session to be created")
	}
	if session.ID != "session-1" {
		t.Errorf("expected ID 'session-1', got '%s'", session.ID)
	}
	if session.Agent == nil {
		t.Error("expected agent to be created")
	}
}

func TestSessionManager_GetOrCreate_ExistingSession(t *testing.T) {
	factory := &SessionFactory{
		Provider:  &mockSessionProv{},
		Store:     &mockSessionStore{},
		MaxTokens: 4096,
	}

	mgr := NewSessionManager(SessionConfig{
		MaxSessions: 10, TTL: 30 * time.Minute, CleanupInterval: 1 * time.Minute,
	}, factory)
	defer mgr.Stop()

	mgr.GetOrCreate("session-1")
	session, created, err := mgr.GetOrCreate("session-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if created {
		t.Error("expected existing session, not new")
	}
	if session.ID != "session-1" {
		t.Errorf("expected ID 'session-1', got '%s'", session.ID)
	}
}

func TestSessionManager_MaxSessions(t *testing.T) {
	factory := &SessionFactory{
		Provider:  &mockSessionProv{},
		Store:     &mockSessionStore{},
		MaxTokens: 4096,
	}

	mgr := NewSessionManager(SessionConfig{
		MaxSessions: 2, TTL: 30 * time.Minute, CleanupInterval: 1 * time.Minute,
	}, factory)
	defer mgr.Stop()

	mgr.GetOrCreate("s1")
	mgr.GetOrCreate("s2")
	_, _, err := mgr.GetOrCreate("s3")
	if err == nil {
		t.Fatal("expected error when max sessions exceeded")
	}
}

func TestSessionManager_Delete(t *testing.T) {
	factory := &SessionFactory{
		Provider:  &mockSessionProv{},
		Store:     &mockSessionStore{},
		MaxTokens: 4096,
	}

	mgr := NewSessionManager(SessionConfig{
		MaxSessions: 10, TTL: 30 * time.Minute, CleanupInterval: 1 * time.Minute,
	}, factory)
	defer mgr.Stop()

	mgr.GetOrCreate("session-1")
	err := mgr.Delete("session-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, created, _ := mgr.GetOrCreate("session-1")
	if !created {
		t.Error("expected session to be recreated after deletion")
	}
}

func TestSessionManager_Cleanup(t *testing.T) {
	factory := &SessionFactory{
		Provider:  &mockSessionProv{},
		Store:     &mockSessionStore{},
		MaxTokens: 4096,
	}

	mgr := NewSessionManager(SessionConfig{
		MaxSessions: 10, TTL: 50 * time.Millisecond, CleanupInterval: 20 * time.Millisecond,
	}, factory)
	defer mgr.Stop()

	mgr.GetOrCreate("session-1")
	time.Sleep(100 * time.Millisecond)

	_, created, _ := mgr.GetOrCreate("session-1")
	if !created {
		t.Error("expected expired session to be cleaned up")
	}
}

func TestSessionManager_List(t *testing.T) {
	factory := &SessionFactory{
		Provider:  &mockSessionProv{},
		Store:     &mockSessionStore{},
		MaxTokens: 4096,
	}

	mgr := NewSessionManager(SessionConfig{
		MaxSessions: 10, TTL: 30 * time.Minute, CleanupInterval: 1 * time.Minute,
	}, factory)
	defer mgr.Stop()

	mgr.GetOrCreate("s1")
	mgr.GetOrCreate("s2")

	sessions := mgr.List()
	if len(sessions) != 2 {
		t.Errorf("expected 2 sessions, got %d", len(sessions))
	}
}

func TestSessionManager_Shutdown(t *testing.T) {
	factory := &SessionFactory{
		Provider:  &mockSessionProv{},
		Store:     &mockSessionStore{},
		MaxTokens: 4096,
	}

	mgr := NewSessionManager(SessionConfig{
		MaxSessions: 10, TTL: 30 * time.Minute, CleanupInterval: 1 * time.Minute,
	}, factory)

	mgr.GetOrCreate("s1")
	mgr.GetOrCreate("s2")

	err := mgr.Shutdown(1 * time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	sessions := mgr.List()
	if len(sessions) != 0 {
		t.Errorf("expected 0 sessions after shutdown, got %d", len(sessions))
	}
}

type mockSessionProv struct{}

func (m *mockSessionProv) CreateMessage(ctx context.Context, req llm.Request) (*llm.Response, error) {
	return &llm.Response{Content: []llm.Block{llm.TextBlock{Text: "ok"}}}, nil
}

type mockSessionStore struct{}

func (m *mockSessionStore) Save(ctx context.Context, entry memory.Entry) error { return nil }
func (m *mockSessionStore) Search(ctx context.Context, q string, limit int) ([]memory.Entry, error) {
	return nil, nil
}
func (m *mockSessionStore) List(ctx context.Context) ([]memory.Entry, error) { return nil, nil }
func (m *mockSessionStore) Delete(ctx context.Context, id string) error       { return nil }
