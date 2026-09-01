package tunnel

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"time"
)

var localhostRunPattern = regexp.MustCompile(`https://[a-z0-9-]+\.lhr\.life`)

func StartLocalhostRun(parent context.Context, binary, localURL string, inheritedFiles []*os.File, timeout time.Duration) (*Process, error) {
	parsed, err := url.Parse(localURL)
	if err != nil || parsed.Scheme != "http" || parsed.Hostname() != "127.0.0.1" || parsed.Port() == "" {
		return nil, errors.New("localhost.run requires an http://127.0.0.1:<port> target")
	}
	knownHosts, err := localhostRunKnownHostsPath()
	if err != nil {
		return nil, err
	}
	arguments := []string{
		"-o", "BatchMode=yes",
		"-o", "StrictHostKeyChecking=accept-new",
		"-o", "UserKnownHostsFile=" + knownHosts,
		"-o", "ExitOnForwardFailure=yes",
		"-o", "ServerAliveInterval=30",
		"-o", "ServerAliveCountMax=3",
		"-T",
		"-R", "80:127.0.0.1:" + parsed.Port(),
		"nokey@localhost.run",
	}
	return startProcess(parent, binary, arguments, inheritedFiles, localhostRunPattern, "localhost.run", timeout)
}

func FindSSH() (string, error) {
	path, err := exec.LookPath("ssh")
	if err != nil {
		return "", errors.New("ssh is not installed; localhost.run fallback is unavailable")
	}
	return path, nil
}

func localhostRunKnownHostsPath() (string, error) {
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("locate user cache directory: %w", err)
	}
	directory := filepath.Join(cache, "agent-preview", "ssh")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return "", fmt.Errorf("create agent-preview SSH directory: %w", err)
	}
	return filepath.Join(directory, "known_hosts"), nil
}
