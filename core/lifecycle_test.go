package core

import (
	"context"
	"testing"
	"time"
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
