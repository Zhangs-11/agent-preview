//go:build darwin || linux

package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

type providerLock struct {
	file *os.File
}

func acquireLocalhostRunLock() (*providerLock, error) {
	root, err := stateRoot()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, fmt.Errorf("create preview state directory: %w", err)
	}
	path := filepath.Join(root, "localhost-run.lock")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open localhost.run provider lock: %w", err)
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = file.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, errors.New("another localhost.run preview is active; stop it before publishing another fallback preview")
		}
		return nil, fmt.Errorf("lock localhost.run provider: %w", err)
	}
	return &providerLock{file: file}, nil
}

func (lock *providerLock) release() {
	if lock == nil || lock.file == nil {
		return
	}
	_ = syscall.Flock(int(lock.file.Fd()), syscall.LOCK_UN)
	_ = lock.file.Close()
	lock.file = nil
}

func (lock *providerLock) closeWithoutUnlock() {
	if lock == nil || lock.file == nil {
		return
	}
	_ = lock.file.Close()
	lock.file = nil
}

func (lock *providerLock) inheritedFiles() []*os.File {
	if lock == nil || lock.file == nil {
		return nil
	}
	return []*os.File{lock.file}
}
