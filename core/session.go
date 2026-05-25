package core

import (
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/HiChen85/customize-agents/llm"
	"github.com/HiChen85/customize-agents/memory"
	"github.com/HiChen85/customize-agents/skill"
)

type Session struct {
	ID        string
	Agent     *Agent
	Lifecycle *Lifecycle
	CreatedAt time.Time
	LastUsed  time.Time
	mu        sync.Mutex
}

func (s *Session) Lock()   { s.mu.Lock() }
func (s *Session) Unlock() { s.mu.Unlock() }

type SessionConfig struct {
	MaxSessions     int           `yaml:"max_sessions"`
	TTL             time.Duration `yaml:"ttl"`
	CleanupInterval time.Duration `yaml:"cleanup_interval"`
}

type SessionFactory struct {
	Provider  llm.Provider
	Tools     []Tool
	Skills    []*skill.Skill
	Store     memory.LongTermStore
	Hooks     *HookRegistry
	MaxTokens int
}

func (f *SessionFactory) Create(id string) *Session {
	mm := memory.NewMemoryManager(f.Store, f.MaxTokens)
	agent := NewAgent(f.Provider, mm, f.Tools, f.Skills)
	if f.Hooks != nil {
		agent.SetHookRegistry(f.Hooks)
	}
	lc := NewLifecycle()
	agent.SetLifecycle(lc)

	return &Session{
		ID:        id,
		Agent:     agent,
		Lifecycle: lc,
		CreatedAt: time.Now(),
		LastUsed:  time.Now(),
	}
}

type SessionManager struct {
	sessions map[string]*Session
	mu       sync.RWMutex
	config   SessionConfig
	factory  *SessionFactory
	stopCh   chan struct{}
}

func NewSessionManager(config SessionConfig, factory *SessionFactory) *SessionManager {
	if config.MaxSessions <= 0 {
		config.MaxSessions = 100
	}
	if config.TTL <= 0 {
		config.TTL = 30 * time.Minute
	}
	if config.CleanupInterval <= 0 {
		config.CleanupInterval = 1 * time.Minute
	}

	mgr := &SessionManager{
		sessions: make(map[string]*Session),
		config:   config,
		factory:  factory,
		stopCh:   make(chan struct{}),
	}

	go mgr.cleanupLoop()
	return mgr
}

func (m *SessionManager) GetOrCreate(id string) (*Session, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if s, ok := m.sessions[id]; ok {
		s.LastUsed = time.Now()
		return s, false, nil
	}

	if len(m.sessions) >= m.config.MaxSessions {
		return nil, false, fmt.Errorf("max sessions reached (%d)", m.config.MaxSessions)
	}

	s := m.factory.Create(id)
	m.sessions[id] = s
	return s, true, nil
}

func (m *SessionManager) Delete(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	s, ok := m.sessions[id]
	if !ok {
		return fmt.Errorf("session not found: %s", id)
	}

	s.Lifecycle.Stop()
	delete(m.sessions, id)
	return nil
}

func (m *SessionManager) List() []*Session {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]*Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		result = append(result, s)
	}
	return result
}

func (m *SessionManager) Shutdown(timeout time.Duration) error {
	m.Stop()

	m.mu.Lock()
	sessions := make([]*Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		sessions = append(sessions, s)
	}
	m.mu.Unlock()

	for _, s := range sessions {
		s.Lifecycle.Stop()
		s.Lifecycle.WaitDone(timeout)
	}

	m.mu.Lock()
	m.sessions = make(map[string]*Session)
	m.mu.Unlock()

	return nil
}

func (m *SessionManager) Stop() {
	select {
	case <-m.stopCh:
	default:
		close(m.stopCh)
	}
}

func (m *SessionManager) cleanupLoop() {
	ticker := time.NewTicker(m.config.CleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-m.stopCh:
			return
		case <-ticker.C:
			m.cleanup()
		}
	}
}

func (m *SessionManager) cleanup() {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	for id, s := range m.sessions {
		if now.Sub(s.LastUsed) > m.config.TTL {
			slog.Info("cleaning up expired session", "id", id)
			s.Lifecycle.Stop()
			delete(m.sessions, id)
		}
	}
}
