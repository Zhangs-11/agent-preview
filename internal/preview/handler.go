package preview

import (
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

type Kind string

const (
	KindFile      Kind = "file"
	KindDirectory Kind = "directory"
	KindProxy     Kind = "proxy"
)

const HealthPath = "/_agent-preview/health"

type Config struct {
	Kind         Kind
	Target       string
	Token        string
	SecureCookie bool
}

func NewHandler(config Config) (http.Handler, error) {
	if config.Token == "" {
		return nil, errors.New("preview token is required")
	}

	var content http.Handler
	switch config.Kind {
	case KindFile:
		target, err := filepath.EvalSymlinks(config.Target)
		if err != nil {
			return nil, fmt.Errorf("resolve preview file symlinks: %w", err)
		}
		info, err := os.Stat(target)
		if err != nil || !info.Mode().IsRegular() {
			return nil, errors.New("preview file must resolve to a regular file")
		}
		content = exactFileHandler(target)
	case KindDirectory:
		root, err := filepath.Abs(config.Target)
		if err != nil {
			return nil, fmt.Errorf("resolve preview directory: %w", err)
		}
		root, err = filepath.EvalSymlinks(root)
		if err != nil {
			return nil, fmt.Errorf("resolve preview directory symlinks: %w", err)
		}
		content = safeDirectoryHandler(root)
	case KindProxy:
		target, err := url.Parse(config.Target)
		if err != nil {
			return nil, fmt.Errorf("parse proxy target: %w", err)
		}
		if target.Scheme != "http" || target.Hostname() != "127.0.0.1" {
			return nil, errors.New("proxy target must be an http://127.0.0.1 address")
		}
		content = httputil.NewSingleHostReverseProxy(target)
	default:
		return nil, fmt.Errorf("unsupported preview kind %q", config.Kind)
	}

	return securityHeaders(authenticate(config.Token, config.SecureCookie, content)), nil
}

func authenticate(token string, secureCookie bool, next http.Handler) http.Handler {
	digest := sha256.Sum256([]byte(token))
	cookieName := fmt.Sprintf("agent_preview_%x", digest[:6])
	if secureCookie {
		cookieName = "__Host-" + cookieName
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		queryToken := r.URL.Query().Get("token")
		if secureEqual(queryToken, token) {
			http.SetCookie(w, &http.Cookie{
				Name:     cookieName,
				Value:    token,
				Path:     "/",
				Secure:   secureCookie,
				HttpOnly: true,
				SameSite: http.SameSiteLaxMode,
			})

			cleanURL := *r.URL
			query := cleanURL.Query()
			query.Del("token")
			cleanURL.RawQuery = query.Encode()
			http.Redirect(w, r, cleanURL.String(), http.StatusSeeOther)
			return
		}

		cookie, err := r.Cookie(cookieName)
		if err != nil || !secureEqual(cookie.Value, token) {
			http.Error(w, "A valid agent-preview access link is required.", http.StatusUnauthorized)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if r.URL.Path == HealthPath {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func secureEqual(left, right string) bool {
	if len(left) != len(right) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(left), []byte(right)) == 1
}

func exactFileHandler(path string) http.Handler {
	name := filepath.Base(path)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" && r.URL.Path != "/"+name {
			http.NotFound(w, r)
			return
		}
		http.ServeFile(w, r, path)
	})
}

func safeDirectoryHandler(root string) http.Handler {
	fileServer := http.FileServer(safeDir{root: root})
	indexHTM := filepath.Join(root, "index.htm")
	_, indexHTMError := os.Stat(indexHTM)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" && indexHTMError == nil {
			indexHTML := filepath.Join(root, "index.html")
			if _, err := os.Stat(indexHTML); errors.Is(err, os.ErrNotExist) {
				request := r.Clone(r.Context())
				request.URL.Path = "/index.htm"
				fileServer.ServeHTTP(w, request)
				return
			}
		}
		fileServer.ServeHTTP(w, r)
	})
}

type safeDir struct {
	root string
}

func (directory safeDir) Open(name string) (http.File, error) {
	clean := filepath.Clean("/" + name)
	for _, part := range strings.Split(strings.TrimPrefix(clean, string(filepath.Separator)), string(filepath.Separator)) {
		if isSensitiveName(part) {
			return nil, fs.ErrPermission
		}
	}

	relativeName := strings.TrimPrefix(clean, string(filepath.Separator))
	candidate := filepath.Join(directory.root, relativeName)
	realPath, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return nil, err
	}
	relative, err := filepath.Rel(directory.root, realPath)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return nil, fs.ErrPermission
	}
	file, err := os.Open(realPath)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	if info.IsDir() {
		return noListFile{File: file}, nil
	}
	return file, nil
}

type noListFile struct {
	http.File
}

func (file noListFile) Readdir(int) ([]fs.FileInfo, error) {
	return nil, fs.ErrPermission
}

func isSensitiveName(name string) bool {
	lower := strings.ToLower(name)
	if strings.HasPrefix(lower, ".") {
		return true
	}
	switch lower {
	case "id_rsa", "id_ed25519", "credentials", "credentials.json", "secrets.json":
		return true
	}
	return strings.HasSuffix(lower, ".pem") ||
		strings.HasSuffix(lower, ".key") ||
		strings.HasSuffix(lower, ".p12") ||
		strings.HasSuffix(lower, ".pfx")
}
