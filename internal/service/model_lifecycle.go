package service

import (
	"time"

	"github.com/lixianmin/logo"
)

type idleReleaser interface {
	ReleaseIfIdle(timeout time.Duration) bool
	Close() error
}

type ModelLifecycle struct {
	releaser      idleReleaser
	timeout       time.Duration
	checkInterval time.Duration
	done          chan struct{}
}

func NewModelLifecycle(releaser idleReleaser, timeout time.Duration) *ModelLifecycle {
	return &ModelLifecycle{
		releaser:      releaser,
		timeout:       timeout,
		checkInterval: 30 * time.Second,
		done:          make(chan struct{}),
	}
}

func (my *ModelLifecycle) Run() {
	ticker := time.NewTicker(my.checkInterval)
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
