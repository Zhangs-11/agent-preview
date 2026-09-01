//go:build !darwin && !linux

package app

import (
	"errors"
	"os"
)

type providerLock struct{}

func acquireLocalhostRunLock() (*providerLock, error) {
	return nil, errors.New("localhost.run fallback is currently supported only on macOS and Linux")
}

func (lock *providerLock) release() {}

func (lock *providerLock) closeWithoutUnlock() {}

func (lock *providerLock) inheritedFiles() []*os.File { return nil }
