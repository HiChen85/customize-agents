package core

import (
	"context"
	"fmt"
	"sync"
	"time"
)

type AgentState int

const (
	StateIdle AgentState = iota
	StateRunning
	StatePaused
	StateStopped
)

func (s AgentState) String() string {
	switch s {
	case StateIdle:
		return "idle"
	case StateRunning:
		return "running"
	case StatePaused:
		return "paused"
	case StateStopped:
		return "stopped"
	default:
		return "unknown"
	}
}

type Lifecycle struct {
	state    AgentState
	mu       sync.RWMutex
	resumeCh chan struct{}
	doneCh   chan struct{}
}

func NewLifecycle() *Lifecycle {
	return &Lifecycle{
		state:  StateIdle,
		doneCh: make(chan struct{}),
	}
}

func (l *Lifecycle) State() AgentState {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.state
}

func (l *Lifecycle) Transition(to AgentState) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.state == StateStopped {
		return fmt.Errorf("cannot transition from stopped state")
	}

	l.state = to
	return nil
}

func (l *Lifecycle) Pause() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.state == StateStopped {
		return fmt.Errorf("cannot pause stopped agent")
	}

	l.state = StatePaused
	l.resumeCh = make(chan struct{})
	return nil
}

func (l *Lifecycle) Resume() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.state != StatePaused {
		return fmt.Errorf("cannot resume: agent is not paused (state: %s)", l.state)
	}

	l.state = StateIdle
	if l.resumeCh != nil {
		close(l.resumeCh)
		l.resumeCh = nil
	}
	return nil
}

func (l *Lifecycle) Stop() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.state == StatePaused && l.resumeCh != nil {
		close(l.resumeCh)
		l.resumeCh = nil
	}
	l.state = StateStopped
	return nil
}

func (l *Lifecycle) WaitIfPaused(ctx context.Context) error {
	l.mu.RLock()
	if l.state != StatePaused {
		l.mu.RUnlock()
		return nil
	}
	ch := l.resumeCh
	l.mu.RUnlock()

	if ch == nil {
		return nil
	}

	select {
	case <-ch:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (l *Lifecycle) MarkDone() {
	select {
	case <-l.doneCh:
	default:
		close(l.doneCh)
	}
}

func (l *Lifecycle) WaitDone(timeout time.Duration) error {
	select {
	case <-l.doneCh:
		return nil
	case <-time.After(timeout):
		return fmt.Errorf("wait done timed out after %v", timeout)
	}
}
