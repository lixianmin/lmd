package daemon

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPidPath_UnderCacheDir(t *testing.T) {
	p := pidPath()
	home, _ := os.UserHomeDir()
	expected := filepath.Join(home, ".cache", "lmd", "daemon.pid")
	if p != expected {
		t.Fatalf("expected %s, got %s", expected, p)
	}
}

func TestWritePid_AndReadPid(t *testing.T) {
	dir := t.TempDir()
	oldCache := os.Getenv("XDG_CACHE_HOME")
	os.Setenv("XDG_CACHE_HOME", dir)
	defer os.Setenv("XDG_CACHE_HOME", oldCache)

	originalPidPath := pidPath
	pidPath = func() string { return filepath.Join(dir, "daemon.pid") }
	defer func() { pidPath = originalPidPath }()

	if err := writePid(); err != nil {
		t.Fatalf("writePid failed: %v", err)
	}

	pid, err := readPid()
	if err != nil {
		t.Fatalf("readPid failed: %v", err)
	}
	if pid != os.Getpid() {
		t.Fatalf("expected pid %d, got %d", os.Getpid(), pid)
	}
}

func TestIsProcessAlive_CurrentProcess(t *testing.T) {
	if !isProcessAlive(os.Getpid()) {
		t.Fatal("current process should be alive")
	}
}

func TestIsProcessAlive_NonExistent(t *testing.T) {
	if isProcessAlive(999999999) {
		t.Fatal("non-existent pid should not be alive")
	}
}

func TestIsRunning_NoPidFile(t *testing.T) {
	dir := t.TempDir()
	originalPidPath := pidPath
	pidPath = func() string { return filepath.Join(dir, "nonexistent.pid") }
	defer func() { pidPath = originalPidPath }()

	if IsRunning() {
		t.Fatal("expected IsRunning false with no pid file")
	}
}

func TestIsRunning_DeadPid(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "daemon.pid")
	os.WriteFile(pidFile, []byte("999999999"), 0644)

	originalPidPath := pidPath
	pidPath = func() string { return pidFile }
	defer func() { pidPath = originalPidPath }()

	if IsRunning() {
		t.Fatal("expected IsRunning false with dead pid")
	}
}

func TestDaemon_Stop_WithoutStart(t *testing.T) {
	d := &Daemon{
		cfg:  nil,
		done: make(chan struct{}),
	}
	err := d.Stop()
	if err != nil {
		t.Fatalf("Stop on unstarted daemon should not error: %v", err)
	}
}

func TestDaemon_TouchActivity(t *testing.T) {
	d := &Daemon{
		done: make(chan struct{}),
	}
	before := d.lastActive
	time.Sleep(10 * time.Millisecond)
	d.touchActivity()
	if !d.lastActive.After(before) {
		t.Fatal("touchActivity should update lastActive")
	}
}

func TestRegisterRoutes_HealthEndpoint(t *testing.T) {
	d := &Daemon{
		cfg:  nil,
		done: make(chan struct{}),
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
