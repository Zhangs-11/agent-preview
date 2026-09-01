package app

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Zhangs-11/agent-preview/internal/preview"
)

func TestBuildRequestSupportsFileDirectoryAndPort(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "page.html")
	if err := os.WriteFile(file, []byte("page"), 0o600); err != nil {
		t.Fatal(err)
	}

	request, err := buildRequest([]string{file}, 0, 24*time.Hour, true)
	if err != nil {
		t.Fatal(err)
	}
	if request.Kind != preview.KindFile || request.TTLSeconds != int64((24*time.Hour).Seconds()) {
		t.Fatalf("file request = %#v", request)
	}

	directory := filepath.Join(root, "site")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "index.html"), []byte("site"), 0o600); err != nil {
		t.Fatal(err)
	}
	request, err = buildRequest([]string{directory}, 0, time.Hour, false)
	if err != nil {
		t.Fatal(err)
	}
	if request.Kind != preview.KindDirectory {
		t.Fatalf("directory kind = %q", request.Kind)
	}

	request, err = buildRequest(nil, 3000, time.Hour, false)
	if err != nil {
		t.Fatal(err)
	}
	if request.Kind != preview.KindProxy || request.Target != "http://127.0.0.1:3000" {
		t.Fatalf("proxy request = %#v", request)
	}
}

func TestBuildRequestRejectsUnsafeOrAmbiguousInputs(t *testing.T) {
	root := t.TempDir()
	unsupported := filepath.Join(root, "secret.key")
	if err := os.WriteFile(unsupported, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := buildRequest([]string{unsupported}, 0, time.Hour, false); err == nil {
		t.Fatal("unsupported file was accepted")
	}
	if _, err := buildRequest([]string{root}, 0, time.Hour, false); err == nil {
		t.Fatal("directory without index file was accepted")
	}
	if _, err := buildRequest([]string{unsupported}, 3000, time.Hour, false); err == nil {
		t.Fatal("path and port were accepted together")
	}
}

func TestBuildRequestRejectsDirectoryIndexSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "index.html")
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "index.html")); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}
	if _, err := buildRequest([]string{root}, 0, time.Hour, false); err == nil {
		t.Fatal("directory index symlink outside preview root was accepted")
	}
}

func TestParseTTL(t *testing.T) {
	for input, expected := range map[string]time.Duration{"24h": 24 * time.Hour, "72h": 72 * time.Hour, "0": 0} {
		actual, err := parseTTL(input)
		if err != nil || actual != expected {
			t.Fatalf("parseTTL(%q) = %s, %v", input, actual, err)
		}
	}
	for _, input := range []string{"", "500ms", "-1h", "tomorrow"} {
		if _, err := parseTTL(input); err == nil {
			t.Fatalf("parseTTL(%q) succeeded", input)
		}
	}
}

func TestLocalWorkerLifecycle(t *testing.T) {
	stateDirectory := t.TempDir()
	t.Setenv("AGENT_PREVIEW_STATE_DIR", stateDirectory)
	target := filepath.Join(t.TempDir(), "index.html")
	if err := os.WriteFile(target, []byte("<h1>worker preview</h1>"), 0o600); err != nil {
		t.Fatal(err)
	}
	request := workerRequest{
		ID: "0123456789abcdef", Kind: preview.KindFile, Target: target, Token: "worker-secret",
		TTLSeconds: 1, LocalOnly: true, CreatedAt: time.Now().UTC(),
	}
	requestFile, err := requestPath(request.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeJSON(requestFile, request); err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() { done <- runWorker([]string{"--request", requestFile}) }()
	state, err := waitForReady(request.ID, 3*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if state.Status != "ready" || !strings.Contains(state.URL, "token=worker-secret") {
		t.Fatalf("ready state = %#v", state)
	}

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Jar: jar}
	response, err := client.Get(state.URL)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK || !strings.Contains(string(body), "worker preview") {
		t.Fatalf("preview response = %d %q", response.StatusCode, body)
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("worker did not expire")
	}
	state, err = readState(request.ID)
	if err != nil {
		t.Fatal(err)
	}
	if state.Status != "expired" {
		t.Fatalf("final status = %q", state.Status)
	}
}

func TestLocalWorkerStopsThroughItsOwnControlFile(t *testing.T) {
	stateDirectory := t.TempDir()
	t.Setenv("AGENT_PREVIEW_STATE_DIR", stateDirectory)
	target := filepath.Join(t.TempDir(), "index.html")
	if err := os.WriteFile(target, []byte("worker"), 0o600); err != nil {
		t.Fatal(err)
	}
	request := workerRequest{
		ID: "fedcba9876543210", Kind: preview.KindFile, Target: target, Token: "worker-secret",
		LocalOnly: true, CreatedAt: time.Now().UTC(),
	}
	requestFile, err := requestPath(request.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeJSON(requestFile, request); err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() { done <- runWorker([]string{"--request", requestFile}) }()
	if _, err := waitForReady(request.ID, 3*time.Second); err != nil {
		t.Fatal(err)
	}
	if err := requestStop(request.ID); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("worker did not honor stop control file")
	}
	state, err := readState(request.ID)
	if err != nil {
		t.Fatal(err)
	}
	if state.Status != "stopped" {
		t.Fatalf("final status = %q", state.Status)
	}
	if err := requestStop(request.ID); err == nil {
		t.Fatal("already stopped preview accepted another stop request")
	}
}

func TestPreviewIDCannotEscapeStateRoot(t *testing.T) {
	t.Setenv("AGENT_PREVIEW_STATE_DIR", t.TempDir())
	if _, err := previewDirectory("../../outside"); err == nil {
		t.Fatal("path-like preview ID was accepted")
	}
}

func TestJSONStateOutput(t *testing.T) {
	state := State{ID: "abc", Status: "ready", URL: "http://example.test", CreatedAt: time.Now().UTC()}
	var output strings.Builder
	if err := printState(&output, state, true); err != nil {
		t.Fatal(err)
	}
	var decoded State
	if err := json.Unmarshal([]byte(output.String()), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.ID != state.ID || decoded.URL != state.URL {
		t.Fatalf("decoded state = %#v", decoded)
	}
}
