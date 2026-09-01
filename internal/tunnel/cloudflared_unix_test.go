//go:build darwin || linux

package tunnel

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStartReadsQuickTunnelURL(t *testing.T) {
	script := filepath.Join(t.TempDir(), "cloudflared")
	content := "#!/bin/sh\necho 'https://tiny-preview.trycloudflare.com' >&2\nexec sleep 30\n"
	if err := os.WriteFile(script, []byte(content), 0o700); err != nil {
		t.Fatal(err)
	}
	process, err := StartCloudflare(context.Background(), script, "http://127.0.0.1:1234", 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if process.URL != "https://tiny-preview.trycloudflare.com" {
		t.Fatalf("URL = %q", process.URL)
	}
	if process.Provider != "cloudflare" {
		t.Fatalf("provider = %q", process.Provider)
	}
	process.Stop()
	select {
	case <-process.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("fake cloudflared process did not stop")
	}
}

func TestStartReturnsRecentOutputOnFailure(t *testing.T) {
	script := filepath.Join(t.TempDir(), "cloudflared")
	content := "#!/bin/sh\necho 'configuration conflict' >&2\nexit 1\n"
	if err := os.WriteFile(script, []byte(content), 0o700); err != nil {
		t.Fatal(err)
	}
	_, err := StartCloudflare(context.Background(), script, "http://127.0.0.1:1234", 2*time.Second)
	if err == nil || !strings.Contains(err.Error(), "configuration conflict") {
		t.Fatalf("failure did not include recent output: %v", err)
	}
}
