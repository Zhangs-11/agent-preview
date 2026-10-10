package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Zhangs-11/agent-preview/internal/preview"
)

func TestMonitorTunnelReportsConsecutiveFailures(t *testing.T) {
	var healthy atomic.Bool
	healthy.Store(true)
	var probes atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != preview.HealthPath {
			t.Errorf("unexpected probe path %q", r.URL.Path)
		}
		probes.Add(1)
		if healthy.Load() {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		http.Error(w, "no tunnel", http.StatusNotFound)
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	unreachable := monitorTunnel(ctx, server.URL, 10*time.Millisecond, 3)

	deadline := time.Now().Add(2 * time.Second)
	for probes.Load() < 3 {
		if time.Now().After(deadline) {
			t.Fatal("monitor did not probe the healthy URL")
		}
		time.Sleep(5 * time.Millisecond)
	}
	select {
	case err := <-unreachable:
		t.Fatalf("healthy URL was reported unreachable: %v", err)
	default:
	}

	healthy.Store(false)
	select {
	case err := <-unreachable:
		if err == nil {
			t.Fatal("monitor reported a nil error")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("monitor did not report the unreachable URL")
	}
}

func TestMonitorTunnelStopsWithContext(t *testing.T) {
	var probes atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		probes.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	monitorTunnel(ctx, server.URL, 10*time.Millisecond, 3)
	time.Sleep(50 * time.Millisecond)
	cancel()
	time.Sleep(30 * time.Millisecond)
	stopped := probes.Load()
	time.Sleep(50 * time.Millisecond)
	if probes.Load() != stopped {
		t.Fatal("monitor kept probing after its context was cancelled")
	}
}
