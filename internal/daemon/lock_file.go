package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
)

var lockFile *os.File

func acquireLockFile() error {
	path := PidPath()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return err
	}

	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return fmt.Errorf("daemon already running (lock held on %s)", path)
	}

	if err := f.Truncate(0); err != nil {
		return fmt.Errorf("truncate pid file failed: %w", err)
	}
	if _, err := f.Seek(0, 0); err != nil {
		return fmt.Errorf("seek pid file failed: %w", err)
	}
	if _, err := f.WriteString(strconv.Itoa(os.Getpid())); err != nil {
		return fmt.Errorf("write pid file failed: %w", err)
	}
	f.Sync()

	lockFile = f
	return nil
}

func releaseLockFile() {
	if lockFile != nil {
		syscall.Flock(int(lockFile.Fd()), syscall.LOCK_UN)
		lockFile.Close()
		lockFile = nil
		os.Remove(PidPath())
	}
}
