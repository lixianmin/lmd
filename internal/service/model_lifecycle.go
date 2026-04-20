package service

import (
	"sync"
	"time"

	"github.com/lixianmin/logo"
)

type idleReleaser interface {
	ReleaseIfIdle(timeout time.Duration) bool
	Close() error
}

type ModelLifecycle struct {
	releaser   idleReleaser
	timeout    time.Duration
	done       chan struct{}
	mu         sync.Mutex
	lastActive time.Time
}

func NewModelLifecycle(releaser idleReleaser, timeout time.Duration) *ModelLifecycle {
	return &ModelLifecycle{
		releaser: releaser,
		timeout:  timeout,
		done:     make(chan struct{}),
	}
}

func (my *ModelLifecycle) Touch() {
	my.mu.Lock()
	defer my.mu.Unlock()
	my.lastActive = time.Now()
}

func (my *ModelLifecycle) Run() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-my.done:
			return
		case <-ticker.C:
			my.releaser.ReleaseIfIdle(my.timeout)
		}
	}
}

func (my *ModelLifecycle) Stop() {
	select {
	case <-my.done:
	default:
		close(my.done)
		my.releaser.Close()
		logo.Info("ModelLifecycle: stopped")
	}
}
