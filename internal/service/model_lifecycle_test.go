package service

import (
	"sync/atomic"
	"testing"
	"time"
)

type mockReleaser struct {
	released atomic.Int32
}

func (m *mockReleaser) ReleaseIfIdle(timeout time.Duration) bool {
	m.released.Add(1)
	return true
}

func (m *mockReleaser) Close() error { return nil }

func TestModelLifecycle_ReleaseOnTimeout(t *testing.T) {
	mock := &mockReleaser{}
	lc := NewModelLifecycle(mock, 50*time.Millisecond)
	lc.checkInterval = 50 * time.Millisecond
	go lc.Run()
	defer lc.Stop()

	time.Sleep(200 * time.Millisecond)
	if mock.released.Load() == 0 {
		t.Fatal("expected model to be released after idle timeout")
	}
}

func TestModelLifecycle_StopIsIdempotent(t *testing.T) {
	mock := &mockReleaser{}
	lc := NewModelLifecycle(mock, time.Minute)
	lc.Stop()
	lc.Stop()
}

type noopReleaser struct {
	closed atomic.Int32
}

func (n *noopReleaser) ReleaseIfIdle(timeout time.Duration) bool { return false }
func (n *noopReleaser) Close() error {
	n.closed.Add(1)
	return nil
}

func TestModelLifecycle_StopCallsClose(t *testing.T) {
	mock := &noopReleaser{}
	lc := NewModelLifecycle(mock, time.Minute)
	go lc.Run()
	time.Sleep(50 * time.Millisecond)
	lc.Stop()
	if mock.closed.Load() != 1 {
		t.Fatal("expected Close to be called on Stop")
	}
}
