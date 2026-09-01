//go:build darwin || linux

package tunnel

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStartLocalhostRunReadsURL(t *testing.T) {
	script := filepath.Join(t.TempDir(), "ssh")
	content := "#!/bin/sh\necho 'demo123.lhr.life tunneled with tls termination, https://demo123.lhr.life' >&2\nexec sleep 30\n"
	if err := os.WriteFile(script, []byte(content), 0o700); err != nil {
		t.Fatal(err)
	}
	process, err := StartLocalhostRun(context.Background(), script, "http://127.0.0.1:4321", nil, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if process.URL != "https://demo123.lhr.life" || process.Provider != "localhost.run" {
		t.Fatalf("process = %#v", process)
	}
	process.StopAndWait(2 * time.Second)
}

func TestStartLocalhostRunRejectsNonLoopbackTarget(t *testing.T) {
	_, err := StartLocalhostRun(context.Background(), "/usr/bin/ssh", "http://example.com:80", nil, time.Second)
	if err == nil {
		t.Fatal("non-loopback target was accepted")
	}
}
