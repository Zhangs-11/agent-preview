//go:build darwin || linux

package app

import (
	"os/exec"
	"testing"
	"time"
)

func TestLocalhostRunProviderLockIsExclusive(t *testing.T) {
	t.Setenv("AGENT_PREVIEW_STATE_DIR", t.TempDir())
	first, err := acquireLocalhostRunLock()
	if err != nil {
		t.Fatal(err)
	}
	defer first.release()
	if _, err := acquireLocalhostRunLock(); err == nil {
		t.Fatal("second localhost.run lock was acquired")
	}
	first.release()
	third, err := acquireLocalhostRunLock()
	if err != nil {
		t.Fatalf("lock was not reusable after release: %v", err)
	}
	third.release()
}

func TestLocalhostRunLockSurvivesInInheritedChild(t *testing.T) {
	t.Setenv("AGENT_PREVIEW_STATE_DIR", t.TempDir())
	lock, err := acquireLocalhostRunLock()
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command("sleep", "1")
	command.ExtraFiles = lock.inheritedFiles()
	if err := command.Start(); err != nil {
		lock.release()
		t.Fatal(err)
	}
	// Closing without LOCK_UN simulates the worker being killed while the SSH
	// child still owns an inherited descriptor.
	lock.closeWithoutUnlock()
	if _, err := acquireLocalhostRunLock(); err == nil {
		_ = command.Process.Kill()
		t.Fatal("lock was released while inherited child was still running")
	}
	if err := command.Wait(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond)
	after, err := acquireLocalhostRunLock()
	if err != nil {
		t.Fatalf("lock was not released after inherited child exited: %v", err)
	}
	after.release()
}
