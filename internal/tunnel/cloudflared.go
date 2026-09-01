package tunnel

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

const releasesURL = "https://api.github.com/repos/cloudflare/cloudflared/releases/latest"
const maxDownloadBytes = 100 << 20

var errNotInstalled = errors.New("cloudflared is not installed")

var quickTunnelPattern = regexp.MustCompile(`https://[a-z0-9-]+\.trycloudflare\.com`)

func StartCloudflare(parent context.Context, binary, localURL string, timeout time.Duration) (*Process, error) {
	return startProcess(parent, binary, []string{"tunnel", "--no-autoupdate", "--url", localURL}, nil, quickTunnelPattern, "cloudflare", timeout)
}

func FindBinary() (string, error) {
	if configured := os.Getenv("AGENT_PREVIEW_CLOUDFLARED"); configured != "" {
		if info, err := os.Stat(configured); err == nil && !info.IsDir() {
			return configured, nil
		}
		return "", fmt.Errorf("AGENT_PREVIEW_CLOUDFLARED does not point to a file: %s", configured)
	}
	if path, err := exec.LookPath("cloudflared"); err == nil {
		return path, nil
	}
	path, err := CachedBinaryPath()
	if err != nil {
		return "", err
	}
	if info, err := os.Stat(path); err == nil && !info.IsDir() {
		return path, nil
	}
	return "", fmt.Errorf("%w; run `agent-preview setup --accept-cloudflare-terms`", errNotInstalled)
}

func CachedBinaryPath() (string, error) {
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("locate user cache directory: %w", err)
	}
	name := "cloudflared"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return filepath.Join(cache, "agent-preview", "bin", name), nil
}

type release struct {
	TagName string  `json:"tag_name"`
	Assets  []asset `json:"assets"`
}

type asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Digest             string `json:"digest"`
}

func Install(ctx context.Context, accepted bool) (string, string, error) {
	if !accepted {
		return "", "", errors.New("Cloudflare terms were not accepted; review the Cloudflare License, Terms, and Privacy Policy before retrying with --accept-cloudflare-terms")
	}
	if existing, err := FindBinary(); err == nil {
		return existing, "existing", nil
	} else if !errors.Is(err, errNotInstalled) {
		return "", "", err
	}

	client := &http.Client{Timeout: 2 * time.Minute}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, releasesURL, nil)
	if err != nil {
		return "", "", err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("User-Agent", "agent-preview")
	response, err := client.Do(request)
	if err != nil {
		return "", "", fmt.Errorf("query latest cloudflared release: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("query latest cloudflared release: GitHub returned %s", response.Status)
	}

	var latest release
	if err := json.NewDecoder(response.Body).Decode(&latest); err != nil {
		return "", "", fmt.Errorf("decode latest cloudflared release: %w", err)
	}
	selected, err := selectAsset(latest.Assets)
	if err != nil {
		return "", "", err
	}

	downloadRequest, err := http.NewRequestWithContext(ctx, http.MethodGet, selected.BrowserDownloadURL, nil)
	if err != nil {
		return "", "", err
	}
	downloadRequest.Header.Set("User-Agent", "agent-preview")
	download, err := client.Do(downloadRequest)
	if err != nil {
		return "", "", fmt.Errorf("download cloudflared: %w", err)
	}
	defer download.Body.Close()
	if download.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("download cloudflared: GitHub returned %s", download.Status)
	}

	data, err := io.ReadAll(io.LimitReader(download.Body, maxDownloadBytes+1))
	if err != nil {
		return "", "", fmt.Errorf("read cloudflared download: %w", err)
	}
	if len(data) > maxDownloadBytes {
		return "", "", errors.New("cloudflared download exceeded the 100 MiB safety limit")
	}
	if err := verifyDigest(data, selected.Digest); err != nil {
		return "", "", err
	}
	if strings.HasSuffix(selected.Name, ".tgz") {
		data, err = extractBinary(data)
		if err != nil {
			return "", "", err
		}
	}

	destination, err := CachedBinaryPath()
	if err != nil {
		return "", "", err
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return "", "", fmt.Errorf("create cloudflared cache directory: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(destination), "cloudflared-*")
	if err != nil {
		return "", "", fmt.Errorf("create cloudflared temporary file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return "", "", fmt.Errorf("write cloudflared: %w", err)
	}
	if err := temporary.Chmod(0o755); err != nil {
		_ = temporary.Close()
		return "", "", fmt.Errorf("make cloudflared executable: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return "", "", fmt.Errorf("close cloudflared temporary file: %w", err)
	}
	if err := os.Rename(temporaryPath, destination); err != nil {
		return "", "", fmt.Errorf("install cloudflared: %w", err)
	}
	return destination, latest.TagName, nil
}

func selectAsset(assets []asset) (asset, error) {
	var name string
	switch runtime.GOOS + "/" + runtime.GOARCH {
	case "darwin/arm64":
		name = "cloudflared-darwin-arm64.tgz"
	case "darwin/amd64":
		name = "cloudflared-darwin-amd64.tgz"
	case "linux/arm64":
		name = "cloudflared-linux-arm64"
	case "linux/amd64":
		name = "cloudflared-linux-amd64"
	case "windows/amd64":
		name = "cloudflared-windows-amd64.exe"
	default:
		return asset{}, fmt.Errorf("cloudflared automatic setup does not support %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	for _, candidate := range assets {
		if candidate.Name == name {
			if candidate.Digest == "" {
				return asset{}, fmt.Errorf("GitHub did not provide a digest for %s", name)
			}
			return candidate, nil
		}
	}
	return asset{}, fmt.Errorf("latest cloudflared release does not contain %s", name)
}

func verifyDigest(data []byte, digest string) error {
	algorithm, expected, found := strings.Cut(digest, ":")
	if !found || algorithm != "sha256" {
		return fmt.Errorf("unsupported cloudflared digest %q", digest)
	}
	actual := sha256.Sum256(data)
	if !strings.EqualFold(hex.EncodeToString(actual[:]), expected) {
		return errors.New("cloudflared SHA-256 verification failed")
	}
	return nil
}

func extractBinary(data []byte) ([]byte, error) {
	gzipReader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("open cloudflared archive: %w", err)
	}
	defer gzipReader.Close()
	tarReader := tar.NewReader(gzipReader)
	for {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read cloudflared archive: %w", err)
		}
		if filepath.Base(header.Name) == "cloudflared" && header.Typeflag == tar.TypeReg {
			if header.Size < 0 || header.Size > maxDownloadBytes {
				return nil, errors.New("cloudflared binary exceeded the 100 MiB safety limit")
			}
			binary, err := io.ReadAll(io.LimitReader(tarReader, maxDownloadBytes+1))
			if err != nil {
				return nil, fmt.Errorf("extract cloudflared binary: %w", err)
			}
			if len(binary) > maxDownloadBytes {
				return nil, errors.New("cloudflared binary exceeded the 100 MiB safety limit")
			}
			return binary, nil
		}
	}
	return nil, errors.New("cloudflared archive did not contain the cloudflared binary")
}
