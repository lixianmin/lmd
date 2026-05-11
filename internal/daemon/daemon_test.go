package daemon

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestPidPath_UnderCacheDir(t *testing.T) {
	p := PidPath()
	home, _ := os.UserHomeDir()
	expected := filepath.Join(home, ".cache", "lmd", "daemon.pid")
	if p != expected {
		t.Fatalf("expected %s, got %s", expected, p)
	}
}

func TestAcquireLock(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "daemon.pid")
	orig := PidPath
	PidPath = func() string { return pidFile }
	defer func() { PidPath = orig }()

	if err := acquireLockFile(); err != nil {
		t.Fatalf("acquireLock failed: %v", err)
	}
	defer releaseLockFile()

	data, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatalf("read pid file failed: %v", err)
	}
	if string(data) != strconv.Itoa(os.Getpid()) {
		t.Fatalf("expected pid %d, got %s", os.Getpid(), string(data))
	}
}

func TestAcquireLock_DoubleFails(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "daemon.pid")
	orig := PidPath
	PidPath = func() string { return pidFile }
	defer func() { PidPath = orig }()

	if err := acquireLockFile(); err != nil {
		t.Fatalf("first acquireLock failed: %v", err)
	}
	defer releaseLockFile()

	err := acquireLockFile()
	if err == nil {
		t.Fatal("expected second acquireLock to fail")
	}
}

func TestIsRunning_NoLockFile(t *testing.T) {
	dir := t.TempDir()
	orig := PidPath
	PidPath = func() string { return filepath.Join(dir, "nonexistent.pid") }
	defer func() { PidPath = orig }()

	if IsRunning() {
		t.Fatal("expected IsRunning false with no lock file")
	}
}

func TestIsRunning_WithLock(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "daemon.pid")
	orig := PidPath
	PidPath = func() string { return pidFile }
	defer func() { PidPath = orig }()

	if err := acquireLockFile(); err != nil {
		t.Fatalf("acquireLock failed: %v", err)
	}
	defer releaseLockFile()

	if !IsRunning() {
		t.Fatal("expected IsRunning true when lock is held")
	}
}

func TestDaemon_Stop_WithoutStart(t *testing.T) {
	d := &Daemon{
		cfg: nil,
	}
	err := d.Stop()
	if err != nil {
		t.Fatalf("Stop on unstarted daemon should not error: %v", err)
	}
}

func TestRegisterRoutes_HealthEndpoint(t *testing.T) {
	d := &Daemon{
		cfg: nil,
	}
	handler := registerRoutes(d)

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp map[string]string
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["status"] != "ok" {
		t.Fatalf("expected status ok, got %v", resp["status"])
	}
}

func TestRebuildMu_RLockAllowsConcurrent(t *testing.T) {
	d := &Daemon{}
	done := make(chan struct{})

	// 第一个 RLock
	d.rebuildMu.RLock()
	go func() {
		// 第二个 RLock 应能并发获取（RLock 不互斥）
		d.rebuildMu.RLock()
		defer d.rebuildMu.RUnlock()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("RLock should not block another RLock")
	}
	d.rebuildMu.RUnlock()
}

func TestRebuildMu_LockBlocksRLock(t *testing.T) {
	d := &Daemon{}

	d.rebuildMu.Lock()

	blocked := make(chan struct{})
	go func() {
		d.rebuildMu.RLock()
		defer d.rebuildMu.RUnlock()
		close(blocked)
	}()

	// RLock 应被 Lock 阻塞
	select {
	case <-blocked:
		t.Fatal("RLock should be blocked by Lock")
	case <-time.After(50 * time.Millisecond):
	}

	d.rebuildMu.Unlock()

	// Lock 释放后 RLock 应获取
	select {
	case <-blocked:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("RLock should acquire after Lock released")
	}
}
