package core

import (
	"context"
	"testing"
	"time"

	"github.com/HiChen85/customize-agents/llm"
	"github.com/HiChen85/customize-agents/memory"
)

func TestLifecycle_InitialState(t *testing.T) {
	lc := NewLifecycle()
	if lc.State() != StateIdle {
		t.Errorf("expected StateIdle, got %d", lc.State())
	}
}

func TestLifecycle_TransitionToRunning(t *testing.T) {
	lc := NewLifecycle()
	err := lc.Transition(StateRunning)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if lc.State() != StateRunning {
		t.Errorf("expected StateRunning, got %d", lc.State())
	}
}

func TestLifecycle_TransitionFromStoppedFails(t *testing.T) {
	lc := NewLifecycle()
	lc.Stop()
	err := lc.Transition(StateRunning)
	if err == nil {
		t.Fatal("expected error transitioning from stopped state")
	}
}

func TestLifecycle_Stop(t *testing.T) {
	lc := NewLifecycle()
	err := lc.Stop()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if lc.State() != StateStopped {
		t.Errorf("expected StateStopped, got %d", lc.State())
	}
}

func TestLifecycle_PauseAndResume(t *testing.T) {
	lc := NewLifecycle()
	lc.Transition(StateRunning)

	err := lc.Pause()
	if err != nil {
		t.Fatalf("unexpected pause error: %v", err)
	}
	if lc.State() != StatePaused {
		t.Errorf("expected StatePaused, got %d", lc.State())
	}

	err = lc.Resume()
	if err != nil {
		t.Fatalf("unexpected resume error: %v", err)
	}
	if lc.State() != StateIdle {
		t.Errorf("expected StateIdle after resume, got %d", lc.State())
	}
}

func TestLifecycle_ResumeFromNonPausedFails(t *testing.T) {
	lc := NewLifecycle()
	err := lc.Resume()
	if err == nil {
		t.Fatal("expected error resuming from non-paused state")
	}
}

func TestLifecycle_WaitIfPaused_NotPaused(t *testing.T) {
	lc := NewLifecycle()
	lc.Transition(StateRunning)
	err := lc.WaitIfPaused(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLifecycle_WaitIfPaused_BlocksUntilResume(t *testing.T) {
	lc := NewLifecycle()
	lc.Transition(StateRunning)
	lc.Pause()

	done := make(chan struct{})
	go func() {
		lc.WaitIfPaused(context.Background())
		close(done)
	}()

	time.Sleep(20 * time.Millisecond)
	select {
	case <-done:
		t.Fatal("should still be blocked")
	default:
	}

	lc.Resume()
	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for resume")
	}
}

func TestLifecycle_WaitIfPaused_ContextCancelled(t *testing.T) {
	lc := NewLifecycle()
	lc.Transition(StateRunning)
	lc.Pause()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := lc.WaitIfPaused(ctx)
	if err == nil {
		t.Fatal("expected context cancelled error")
	}
}

func TestLifecycle_WaitDone(t *testing.T) {
	lc := NewLifecycle()
	lc.Transition(StateRunning)

	go func() {
		time.Sleep(20 * time.Millisecond)
		lc.MarkDone()
	}()

	err := lc.WaitDone(1 * time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLifecycle_WaitDone_Timeout(t *testing.T) {
	lc := NewLifecycle()
	lc.Transition(StateRunning)

	err := lc.WaitDone(20 * time.Millisecond)
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestAgent_RunRespectsLifecycle(t *testing.T) {
	lc := NewLifecycle()
	lc.Stop()

	mm := memory.NewMemoryManager(&mockLifecycleStore{}, 4096)
	agent := NewAgent(&mockLifecycleProv{}, mm, nil, nil)
	agent.SetLifecycle(lc)

	_, err := agent.Run(context.Background(), "hello")
	if err == nil {
		t.Fatal("expected error running stopped agent")
	}
}

func TestAgent_RunStreamRespectsLifecycle(t *testing.T) {
	lc := NewLifecycle()
	lc.Stop()

	mm := memory.NewMemoryManager(&mockLifecycleStore{}, 4096)
	agent := NewAgent(&mockLifecycleProv{}, mm, nil, nil)
	agent.SetLifecycle(lc)

	_, err := agent.RunStream(context.Background(), "hello", func(llm.StreamEvent) {})
	if err == nil {
		t.Fatal("expected error running stopped agent")
	}
}

type mockLifecycleProv struct{}

func (m *mockLifecycleProv) CreateMessage(ctx context.Context, req llm.Request) (*llm.Response, error) {
	return &llm.Response{Content: []llm.Block{llm.TextBlock{Text: "ok"}}}, nil
}

type mockLifecycleStore struct{}

func (m *mockLifecycleStore) Save(ctx context.Context, entry memory.Entry) error { return nil }
func (m *mockLifecycleStore) Search(ctx context.Context, q string, limit int) ([]memory.Entry, error) {
	return nil, nil
}
func (m *mockLifecycleStore) List(ctx context.Context) ([]memory.Entry, error) { return nil, nil }
func (m *mockLifecycleStore) Delete(ctx context.Context, id string) error       { return nil }
