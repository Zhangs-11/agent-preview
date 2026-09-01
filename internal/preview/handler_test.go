package preview

import (
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestFilePreviewRequiresCapabilityLink(t *testing.T) {
	target := filepath.Join(t.TempDir(), "index.html")
	if err := os.WriteFile(target, []byte("<h1>preview works</h1>"), 0o600); err != nil {
		t.Fatal(err)
	}
	handler, err := NewHandler(Config{Kind: KindFile, Target: target, Token: "secret", SecureCookie: false})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()

	response, err := http.Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status without token = %d, want %d", response.StatusCode, http.StatusUnauthorized)
	}

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Jar: jar}
	response, err = client.Get(server.URL + "/?token=secret")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK || !strings.Contains(string(body), "preview works") {
		t.Fatalf("authenticated response = %d %q", response.StatusCode, body)
	}
	if response.Request.URL.RawQuery != "" {
		t.Fatalf("token remained in final URL: %s", response.Request.URL)
	}
	if got := response.Header.Get("Referrer-Policy"); got != "no-referrer" {
		t.Fatalf("Referrer-Policy = %q", got)
	}
}

func TestHealthEndpointDoesNotExposeContentOrRequireToken(t *testing.T) {
	target := filepath.Join(t.TempDir(), "index.html")
	writeTestFile(t, target, "private page")
	handler, err := NewHandler(Config{Kind: KindFile, Target: target, Token: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "http://preview.example"+HealthPath, nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	response := recorder.Result()
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusNoContent || len(body) != 0 {
		t.Fatalf("health response = %d %q", response.StatusCode, body)
	}
}

func TestPublicPreviewSetsHostOnlySecureCookie(t *testing.T) {
	target := filepath.Join(t.TempDir(), "index.html")
	writeTestFile(t, target, "page")
	handler, err := NewHandler(Config{Kind: KindFile, Target: target, Token: "secret", SecureCookie: true})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "https://preview.example/?token=secret", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	response := recorder.Result()
	defer response.Body.Close()
	if response.StatusCode != http.StatusSeeOther {
		t.Fatalf("status = %d", response.StatusCode)
	}
	cookies := response.Cookies()
	if len(cookies) != 1 || !strings.HasPrefix(cookies[0].Name, "__Host-agent_preview_") || !cookies[0].Secure || !cookies[0].HttpOnly || cookies[0].Path != "/" || cookies[0].Domain != "" {
		t.Fatalf("unexpected public preview cookie: %#v", cookies)
	}
}

func TestLocalPreviewsDoNotOverwriteEachOthersCookies(t *testing.T) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Jar: jar}
	servers := make([]*httptest.Server, 0, 2)
	for index, token := range []string{"first-secret", "second-secret"} {
		target := filepath.Join(t.TempDir(), "index.html")
		writeTestFile(t, target, string(rune('1'+index)))
		handler, err := NewHandler(Config{Kind: KindFile, Target: target, Token: token})
		if err != nil {
			t.Fatal(err)
		}
		server := httptest.NewServer(handler)
		servers = append(servers, server)
		defer server.Close()
		response, err := client.Get(server.URL + "/?token=" + token)
		if err != nil {
			t.Fatal(err)
		}
		_ = response.Body.Close()
		if response.StatusCode != http.StatusOK {
			t.Fatalf("preview %d authentication status = %d", index, response.StatusCode)
		}
	}

	for index, server := range servers {
		response, err := client.Get(server.URL)
		if err != nil {
			t.Fatal(err)
		}
		_ = response.Body.Close()
		if response.StatusCode != http.StatusOK {
			t.Fatalf("preview %d lost authentication after opening another preview: %d", index, response.StatusCode)
		}
	}
}

func TestDirectoryPreviewBlocksSensitiveFilesAndListings(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "index.html"), "home")
	writeTestFile(t, filepath.Join(root, ".env"), "TOKEN=secret")
	writeTestFile(t, filepath.Join(root, "credentials.json"), "secret")
	if err := os.Mkdir(filepath.Join(root, "assets"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(root, "assets", "app.js"), "ok")
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	file, err := (safeDir{root: realRoot}).Open("/assets/app.js")
	if err != nil {
		t.Fatalf("safe directory rejected ordinary asset: %v", err)
	}
	_ = file.Close()

	handler, err := NewHandler(Config{Kind: KindDirectory, Target: root, Token: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	server := authenticatedTestServer(t, handler, "secret")
	defer server.Close()

	for _, path := range []string{"/.env", "/credentials.json", "/assets/"} {
		response, err := server.Client().Get(server.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		_ = response.Body.Close()
		if response.StatusCode == http.StatusOK {
			t.Fatalf("sensitive or listable path %s returned 200", path)
		}
	}

	response, err := server.Client().Get(server.URL + "/assets/app.js")
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("asset status = %d, want 200", response.StatusCode)
	}
}

func TestDirectoryPreviewBlocksSymlinkEscape(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink permissions vary on Windows")
	}
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.txt")
	writeTestFile(t, filepath.Join(root, "index.html"), "home")
	writeTestFile(t, outside, "outside")
	if err := os.Symlink(outside, filepath.Join(root, "outside.txt")); err != nil {
		t.Fatal(err)
	}

	handler, err := NewHandler(Config{Kind: KindDirectory, Target: root, Token: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	server := authenticatedTestServer(t, handler, "secret")
	defer server.Close()
	response, err := server.Client().Get(server.URL + "/outside.txt")
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode == http.StatusOK {
		t.Fatal("symlink outside preview root returned 200")
	}
}

func TestDirectoryPreviewSupportsIndexHTM(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "index.htm"), "htm home")
	handler, err := NewHandler(Config{Kind: KindDirectory, Target: root, Token: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	server := authenticatedTestServer(t, handler, "secret")
	defer server.Close()
	response, err := server.Client().Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK || string(body) != "htm home" {
		t.Fatalf("index.htm response = %d %q", response.StatusCode, body)
	}
}

func TestFilePreviewPinsSymlinkTarget(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink permissions vary on Windows")
	}
	directory := t.TempDir()
	first := filepath.Join(directory, "first.html")
	second := filepath.Join(directory, "second.html")
	link := filepath.Join(directory, "page.html")
	writeTestFile(t, first, "first")
	writeTestFile(t, second, "second")
	if err := os.Symlink(first, link); err != nil {
		t.Fatal(err)
	}
	handler, err := NewHandler(Config{Kind: KindFile, Target: link, Token: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(second, link); err != nil {
		t.Fatal(err)
	}
	server := authenticatedTestServer(t, handler, "secret")
	defer server.Close()
	response, err := server.Client().Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "first" {
		t.Fatalf("preview followed a replaced symlink: %q", body)
	}
}

func TestProxyRejectsNonLoopbackTarget(t *testing.T) {
	_, err := NewHandler(Config{Kind: KindProxy, Target: "http://example.com", Token: "secret"})
	if err == nil {
		t.Fatal("non-loopback proxy target was accepted")
	}
}

func authenticatedTestServer(t *testing.T, handler http.Handler, token string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(handler)
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	server.Client().Jar = jar
	response, err := server.Client().Get(server.URL + "/?token=" + token)
	if err != nil {
		server.Close()
		t.Fatal(err)
	}
	_ = response.Body.Close()
	return server
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
